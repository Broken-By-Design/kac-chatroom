package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// hlsCacheDir is where HLS segment directories live on disk.
const hlsCacheDir = "hls_cache"

// hlsTranscodeSem caps concurrent ffmpeg processes to 2 on the 1-vCPU VPS.
var hlsTranscodeSem = make(chan struct{}, 2)

// hlsEntry tracks an active or completed transcode.
type hlsEntry struct {
	mu       sync.Mutex
	ready    bool       // true once at least the first segment is written
	err      error      // set if transcode failed fatally
	done     chan struct{} // closed when transcode goroutine exits (success or fail)
	lastUsed time.Time
	cancel   context.CancelFunc
}

var (
	hlsMu      sync.Mutex
	hlsEntries = map[string]*hlsEntry{}
)

func init() {
	os.MkdirAll(hlsCacheDir, 0o755)
	go hlsCleanupLoop()
}

// hlsDir returns the directory for a given videoID's HLS segments.
func hlsDir(videoID string) string {
	return filepath.Join(hlsCacheDir, videoID)
}

// startHLSTranscode kicks off (or reuses) an ffmpeg transcode for videoID.
// It blocks until the playlist file exists (first segment written) or fails.
// Returns the relative path to the m3u8, e.g. "/hls/dQw4w9WgXcQ/index.m3u8"
func startHLSTranscode(videoID string, videoURL, audioURL string) (string, error) {
	hlsMu.Lock()
	entry, exists := hlsEntries[videoID]
	if exists {
		entry.lastUsed = time.Now()
		hlsMu.Unlock()
		return "/hls/" + videoID + "/index.m3u8", nil
	}

	// Create new entry
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	entry = &hlsEntry{
		done:     make(chan struct{}),
		lastUsed: time.Now(),
		cancel:   cancel,
	}
	hlsEntries[videoID] = entry
	hlsMu.Unlock()

	go func() {
		defer close(entry.done)
		defer cancel()

		// Acquire transcode semaphore
		hlsTranscodeSem <- struct{}{}
		defer func() { <-hlsTranscodeSem }()

		dir := hlsDir(videoID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			entry.mu.Lock()
			entry.err = err
			entry.mu.Unlock()
			return
		}

		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			entry.mu.Lock()
			entry.err = fmt.Errorf("ffmpeg not found")
			entry.mu.Unlock()
			return
		}

		m3u8 := filepath.Join(dir, "index.m3u8")
		segPattern := filepath.Join(dir, "seg%05d.ts")

		args := []string{
			"-y",
			"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
			"-referer", "https://www.youtube.com/",
			"-i", videoURL,
			"-i", audioURL,
			"-map", "0:v:0",
			"-map", "1:a:0",
			"-c:v", "copy",
			"-c:a", "copy",
			"-f", "hls",
			"-hls_time", "6",
			"-hls_list_size", "0",         // keep all segments in playlist
			"-hls_flags", "append_list",   // append to playlist as segments are written
			"-hls_segment_filename", segPattern,
			m3u8,
		}

		cmd := exec.CommandContext(ctx, ffmpeg, args...)
		cmd.Stdout = nil
		cmd.Stderr = nil // suppress ffmpeg output

		if err := cmd.Start(); err != nil {
			entry.mu.Lock()
			entry.err = fmt.Errorf("ffmpeg start: %w", err)
			entry.mu.Unlock()
			return
		}

		// Poll until the m3u8 file has at least one segment line (i.e. first
		// segment written), then mark ready so the HTTP handler can respond.
		for {
			if ctx.Err() != nil {
				entry.mu.Lock()
				entry.err = fmt.Errorf("transcode timeout")
				entry.mu.Unlock()
				cmd.Process.Kill()
				return
			}
			if data, err := os.ReadFile(m3u8); err == nil {
				// Look for at least one .ts segment line
				content := string(data)
				hasSegment := false
				for _, line := range splitLines(content) {
					if len(line) > 0 && line[0] != '#' {
						hasSegment = true
						break
					}
				}
				if hasSegment {
					entry.mu.Lock()
					entry.ready = true
					entry.mu.Unlock()
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
		}

		// Wait for ffmpeg to finish
		cmd.Wait()
	}()

	// Wait for the first segment to be ready or for a fatal error, with a
	// generous timeout. ffmpeg keeps transcoding in the background afterwards,
	// so the client can start playing and seeking almost immediately.
	deadline := time.Now().Add(30 * time.Second)
	for {
		entry.mu.Lock()
		ready := entry.ready
		err := entry.err
		entry.mu.Unlock()
		if err != nil {
			hlsMu.Lock()
			delete(hlsEntries, videoID)
			hlsMu.Unlock()
			return "", err
		}
		if ready {
			return "/hls/" + videoID + "/index.m3u8", nil
		}
		if time.Now().After(deadline) {
			entry.cancel()
			hlsMu.Lock()
			delete(hlsEntries, videoID)
			hlsMu.Unlock()
			return "", fmt.Errorf("transcode timed out waiting for first segment")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// handleHLSFile serves .m3u8 playlists and .ts segment files from hls_cache.
func handleHLSFile(w http.ResponseWriter, r *http.Request) {
	// Path is /hls/<videoID>/<file>
	rel := r.URL.Path[len("/hls/"):]
	clean := filepath.Clean(rel)
	// Security: must not escape hls_cache
	full := filepath.Join(hlsCacheDir, clean)
	if !isSubPath(hlsCacheDir, full) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Update last-used time
	parts := splitN(clean, string(filepath.Separator), 2)
	if len(parts) >= 1 {
		hlsMu.Lock()
		if e, ok := hlsEntries[parts[0]]; ok {
			e.lastUsed = time.Now()
		}
		hlsMu.Unlock()
	}

	ext := filepath.Ext(full)
	switch ext {
	case ".m3u8":
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	case ".ts":
		w.Header().Set("Content-Type", "video/MP2T")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, full)
}

func isSubPath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if len(child) < len(parent) {
		return false
	}
	if child == parent {
		return true
	}
	return child[len(parent)] == filepath.Separator && child[:len(parent)] == parent
}

func splitN(s, sep string, n int) []string {
	var result []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// hlsCleanupLoop deletes HLS directories that haven't been accessed in 30 min.
func hlsCleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		hlsMu.Lock()
		now := time.Now()
		for id, e := range hlsEntries {
			if now.Sub(e.lastUsed) > 30*time.Minute {
				e.cancel()
				delete(hlsEntries, id)
				go os.RemoveAll(hlsDir(id))
			}
		}
		hlsMu.Unlock()
	}
}

