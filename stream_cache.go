package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Stream URL cache. Resolved playback URLs are time-limited by YouTube
// (verified ~5.8h), so entries are safe to reuse for a long window. The cache
// is file-backed so it survives container restarts, with a small in-memory LRU
// mirror so memory stays flat under the container's soft limit.

const (
	// streamCacheTTL is how long a resolved URL set is considered fresh.
	// URLs expire in ~6h; 4h leaves a safety margin while making most
	// resolutions cache hits.
	streamCacheTTL = 4 * time.Hour

	// streamSingleTTL is how long a 360p-only (no DASH pair) resolve is kept.
	// YouTube rotates which player client gets served, so a pair may become
	// fetchable minutes after a single-only resolve. A short TTL lets the
	// next click re-resolve and pick up the higher quality.
	streamSingleTTL = 10 * time.Minute

	// streamFailureTTL is how long a failed resolve is remembered, so the
	// frontend's ?fresh=1 retries don't hammer yt-dlp on already-broken
	// videos.
	streamFailureTTL = 60 * time.Second

	// streamCacheMax bounds the in-memory mirror (LRU eviction). ~256 entries
	// of small URL maps keeps resident memory well under a megabyte.
	streamCacheMax = 256

	// streamCacheSaveDelay debounces writes to disk.
	streamCacheSaveDelay = 2 * time.Second

	// streamCacheDir/file are the default persistence location. Override the
	// directory with YT_STREAM_CACHE_DIR.
	streamCacheDir = "cache"
	streamCacheFile = "video_streams.json"
)

type streamCacheEntry struct {
	Info    map[string]any `json:"info,omitempty"`
	Failed  bool           `json:"failed,omitempty"`
	Expires int64          `json:"expires"`
}

type streamCache struct {
	mu       sync.Mutex
	path     string
	entries  map[string]streamCacheEntry
	order    []string // insertion/LRU order, front = oldest
	saveTimer *time.Timer
	dir      string
}

var streamCacheInstance = newStreamCache()

func newStreamCache() *streamCache {
	dir := os.Getenv("YT_STREAM_CACHE_DIR")
	if dir == "" {
		dir = streamCacheDir
	}
	sc := &streamCache{
		path:    filepath.Join(dir, streamCacheFile),
		entries: map[string]streamCacheEntry{},
		dir:     dir,
	}
	sc.load()
	return sc
}

// streamCacheGet returns the cached entry for videoID, treating expired
// entries as a miss. ok is false when absent or expired.
func streamCacheGet(videoID string) (streamCacheEntry, bool) {
	sc := streamCacheInstance
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e, ok := sc.entries[videoID]
	if !ok || time.Now().Unix() >= e.Expires {
		return streamCacheEntry{}, false
	}
	sc.touch(videoID)
	return e, true
}

// streamCachePut stores a successful resolve. Pair (1080p DASH) entries live
// the full TTL; single-only entries expire quickly so a later re-resolve can
// catch a live pair when Google's client rotation allows one.
func streamCachePut(videoID string, info map[string]any) {
	ttl := streamCacheTTL
	if _, ok := info["video"].(string); !ok {
		ttl = streamSingleTTL
	}
	sc := streamCacheInstance
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.insert(videoID, streamCacheEntry{
		Info:    info,
		Expires: time.Now().Add(ttl).Unix(),
	})
	sc.scheduleSave()
}

// streamCachePutFailure remembers a failed resolve for a short window.
func streamCachePutFailure(videoID string) {
	sc := streamCacheInstance
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.insert(videoID, streamCacheEntry{
		Failed:  true,
		Expires: time.Now().Add(streamFailureTTL).Unix(),
	})
	sc.scheduleSave()
}

// streamCacheDelete forgets an entry (used by ?fresh=1).
func streamCacheDelete(videoID string) {
	sc := streamCacheInstance
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.entries, videoID)
	sc.rebuildOrder()
}

// insert adds/updates an entry and enforces the LRU bound.
func (sc *streamCache) insert(videoID string, e streamCacheEntry) {
	_, existed := sc.entries[videoID]
	sc.entries[videoID] = e
	if existed {
		sc.touch(videoID)
	} else {
		sc.order = append(sc.order, videoID)
	}
	for len(sc.order) > streamCacheMax {
		oldest := sc.order[0]
		sc.order = sc.order[1:]
		delete(sc.entries, oldest)
	}
}

// touch moves videoID to the back of the LRU order (most recently used).
func (sc *streamCache) touch(videoID string) {
	idx := -1
	for i, id := range sc.order {
		if id == videoID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	if idx == len(sc.order)-1 {
		return
	}
	copy(sc.order[idx:], sc.order[idx+1:])
	sc.order[len(sc.order)-1] = videoID
}

func (sc *streamCache) rebuildOrder() {
	// Rebuild order from entries (map iteration order, close enough for LRU
	// bookkeeping after a delete).
	sc.order = sc.order[:0]
	for id := range sc.entries {
		sc.order = append(sc.order, id)
	}
}

func (sc *streamCache) scheduleSave() {
	if sc.saveTimer != nil {
		sc.saveTimer.Stop()
	}
	sc.saveTimer = time.AfterFunc(streamCacheSaveDelay, func() {
		sc.mu.Lock()
		defer sc.mu.Unlock()
		sc.save()
	})
}

func (sc *streamCache) save() {
	if err := os.MkdirAll(sc.dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(sc.entries)
	if err != nil {
		return
	}
	tmp := sc.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, sc.path)
}

func (sc *streamCache) load() {
	data, err := os.ReadFile(sc.path)
	if err != nil {
		return
	}
	var entries map[string]streamCacheEntry
	if json.Unmarshal(data, &entries) != nil {
		return
	}
	now := time.Now().Unix()
	sc.entries = map[string]streamCacheEntry{}
	for id, e := range entries {
		if now < e.Expires {
			sc.entries[id] = e
		}
	}
	sc.rebuildOrder()
}

// streamCacheFlush persists any pending changes immediately (e.g. on shutdown).
func streamCacheFlush() {
	sc := streamCacheInstance
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.saveTimer != nil {
		sc.saveTimer.Stop()
		sc.saveTimer = nil
	}
	sc.save()
}
