package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Port of utils/command_helper.py
const commandAPI = "https://api.killallchickens.org"
const jokeAPIKey = "9813a54654f81bcc3f69fe1489f05e016d944c0b7d85df43feec77bf89ae97e7"
const lyricsAPI = "https://lrclib.net/api/search"

// innertubeWebKey is the public API key embedded in youtube.com's web client,
// used for keyless search scraping.
const innertubeWebKey = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"

// innertubePost sends a request to a youtubei/v1 endpoint and returns the
// decoded JSON payload as an arbitrary tree. Returns nil on any failure.
func innertubePost(endpoint string, body map[string]any) any {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("POST", "https://www.youtube.com/youtubei/v1/"+endpoint+"?key="+innertubeWebKey, bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var root any
	if json.NewDecoder(resp.Body).Decode(&root) != nil {
		return nil
	}
	return root
}

// collectContinuation walks the innertube response looking for the first
// continuation token used to fetch the next page of results.
func collectContinuation(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if cmd, ok := v["continuationCommand"].(map[string]any); ok {
			if token, ok := cmd["token"].(string); ok && token != "" {
				return token
			}
		}
		for _, child := range v {
			if t := collectContinuation(child); t != "" {
				return t
			}
		}
	case []any:
		for _, child := range v {
			if t := collectContinuation(child); t != "" {
				return t
			}
		}
	}
	return ""
}

// videoPage runs an innertube query and returns deduped videos plus the
// continuation token for the next page, if any.
func videoPage(endpoint string, body map[string]any) ([]VideoSearchResult, string) {
	root := innertubePost(endpoint, body)
	if root == nil {
		return nil, ""
	}
	var results []VideoSearchResult
	collectVideos(root, &results)
	seen := map[string]bool{}
	deduped := results[:0]
	for _, r := range results {
		if !seen[r.VideoID] {
			seen[r.VideoID] = true
			deduped = append(deduped, r)
		}
	}
	return deduped, collectContinuation(root)
}

// searchVideos returns one page of search results. Pass a continuation token
// (from a prior call) to fetch the next page; otherwise pass the query.
func searchVideos(query, continuation string) ([]VideoSearchResult, string) {
	body := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName": "WEB", "clientVersion": "2.20240801.00.00", "hl": "en", "gl": "US",
			},
		},
	}
	if continuation != "" {
		body["continuation"] = continuation
	} else {
		body["query"] = query
	}
	return videoPage("search", body)
}

// defaultRecommendQueries seed the homepage when the logged-out home feed is
// unavailable (YouTube gates it behind a sign-in). Override with the
// RECOMMEND_QUERIES env var (comma-separated) to influence what gets surfaced.
var defaultRecommendQueries = []string{
	"viral videos 2026",
	"trending music",
	"best of gaming",
	"science explained",
	"comedy skits",
	"top songs 2026",
	"documentary",
	"cooking recipes",
}

// recommendQueries is the active query pool, seeded from the env var if set.
var recommendQueries = defaultRecommendQueries

func initRecommendQueries() {
	raw := os.Getenv("RECOMMEND_QUERIES")
	if raw == "" {
		return
	}
	var out []string
	for _, q := range strings.Split(raw, ",") {
		q = strings.TrimSpace(q)
		if q != "" {
			out = append(out, q)
		}
	}
	if len(out) > 0 {
		recommendQueries = out
	}
}

// shuffledQueries returns the recommendation pool in random order so the
// homepage isn't the same every load.
func shuffledQueries() []string {
	out := make([]string, len(recommendQueries))
	copy(out, recommendQueries)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// recommendVideos returns one page of home-feed recommendations. Pass a
// continuation token to fetch the next page. If the home feed is empty
// (auth-gated), it falls back to a merged set of curated searches.
func recommendVideos(continuation string) ([]VideoSearchResult, string) {
	body := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName": "WEB", "clientVersion": "2.20240801.00.00", "hl": "en", "gl": "US",
			},
		},
	}
	if continuation != "" {
		body["continuation"] = continuation
	} else {
		body["browseId"] = "FEwhat_to_watch"
	}
	results, next := videoPage("browse", body)
	if len(results) > 0 {
		return results, next
	}
	if continuation != "" {
		return nil, ""
	}
	seen := map[string]bool{}
	var (
		mu     sync.Mutex
		perQ   [][]VideoSearchResult
		wg     sync.WaitGroup
		queries = shuffledQueries()
	)
	perQ = make([][]VideoSearchResult, len(queries))
	for i, q := range queries {
		q := q
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rs, _ := searchVideos(q, "")
			mu.Lock()
			defer mu.Unlock()
			for _, r := range rs {
				if !seen[r.VideoID] {
					seen[r.VideoID] = true
					perQ[idx] = append(perQ[idx], r)
				}
			}
		}(i)
	}
	wg.Wait()

	var merged []VideoSearchResult
	for round := 0; len(merged) < 20; round++ {
		added := 0
		for _, list := range perQ {
			if round < len(list) {
				merged = append(merged, list[round])
				added++
			}
			if len(merged) >= 20 {
				break
			}
		}
		if added == 0 {
			break
		}
	}
	return merged, ""
}

func collectVideos(node any, out *[]VideoSearchResult) {
	switch v := node.(type) {
	case map[string]any:
		if vr, ok := v["videoRenderer"].(map[string]any); ok {
			if id, ok := vr["videoId"].(string); ok && id != "" {
				result := VideoSearchResult{VideoID: id}
				if title, ok := vr["title"].(map[string]any); ok {
					result.Title = runsText(title["runs"])
				}
				if byline, ok := vr["shortBylineText"].(map[string]any); ok {
					result.Channel = runsText(byline["runs"])
				}
				if dms, ok := vr["detailedMetadataSnippets"].([]any); ok && len(dms) > 0 {
					if first, ok := dms[0].(map[string]any); ok {
						if st, ok := first["snippetText"].(map[string]any); ok {
							result.Description = runsText(st["runs"])
						}
					}
				}
				if result.Description == "" {
					if snippet, ok := vr["descriptionSnippet"].(map[string]any); ok {
						result.Description = runsText(snippet["runs"])
					}
				}
				*out = append(*out, result)
			}
		}
		for _, child := range v {
			collectVideos(child, out)
		}
	case []any:
		for _, child := range v {
			collectVideos(child, out)
		}
	}
}

func runsText(node any) string {
	runs, ok := node.([]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	for _, r := range runs {
		if rm, ok := r.(map[string]any); ok {
			if t, ok := rm["text"].(string); ok {
				sb.WriteString(t)
			}
		}
	}
	return sb.String()
}

// streamResolveSem caps concurrent yt-dlp subprocesses to protect the host's
// RAM/CPU from spikes when several video streams resolve at once.
var streamResolveSem = make(chan struct{}, 2)

// videoStreamCache avoids re-running yt-dlp (a heavy Python process) for the
// same video within the TTL, so frontend retries are cheap. The cached URLs
// are time-limited by YouTube (~6h); 30 minutes is a safe refresh window.
var (
	videoStreamCacheMu sync.Mutex
	videoStreamCache   = map[string]videoStreamCacheEntry{}
)

type videoStreamCacheEntry struct {
	info    map[string]any
	expires time.Time
}

// resolveVideoStreamInfo uses yt-dlp in simulate mode to fetch direct,
// time-limited stream URLs for a video. Nothing is downloaded or stored.
//
// It prefers a 1080p H.264 video-only DASH stream paired with the best m4a
// audio, which the client muxes via MediaSource. YouTube no longer offers
// combined (video+audio) formats above 360p, so a "single" 360p URL is
// returned as a fallback when no DASH pair is available.
func resolveVideoStreamInfo(videoID string, fresh bool) map[string]any {
	videoStreamCacheMu.Lock()
	if fresh {
		delete(videoStreamCache, videoID)
	} else if e, ok := videoStreamCache[videoID]; ok && time.Now().Before(e.expires) {
		videoStreamCacheMu.Unlock()
		return e.info
	}
	videoStreamCacheMu.Unlock()

	streamResolveSem <- struct{}{}
	defer func() { <-streamResolveSem }()

	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ytdlp, "-j", "--no-warnings",
		"-f", "bestvideo[height<=1080][ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=720]/best",
		"https://www.youtube.com/watch?v="+videoID)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var info struct {
		URL              string `json:"url"`
		Title            string `json:"title"`
		Channel          string `json:"channel"`
		Description      string `json:"description"`
		RequestedFormats []struct {
			URL    string `json:"url"`
			VCodec string `json:"vcodec"`
			ACodec string `json:"acodec"`
		} `json:"requested_formats"`
		Formats []struct {
			URL    string `json:"url"`
			VCodec string `json:"vcodec"`
			ACodec string `json:"acodec"`
			Height int    `json:"height"`
		} `json:"formats"`
	}
	if json.Unmarshal(out, &info) != nil {
		return nil
	}
	result := map[string]any{}
	if info.Title != "" {
		result["title"] = info.Title
	}
	if info.Channel != "" {
		result["channel"] = info.Channel
	}
	if info.Description != "" {
		result["description"] = info.Description
	}
	if len(info.RequestedFormats) == 2 {
		v, a := info.RequestedFormats[0], info.RequestedFormats[1]
		if v.VCodec == "none" && a.VCodec != "none" {
			v, a = a, v
		}
		if v.VCodec != "none" && a.ACodec != "none" && strings.Contains(v.URL, "googlevideo.com") && strings.Contains(a.URL, "googlevideo.com") {
			result["video"] = v.URL
			result["audio"] = a.URL
			result["vcodec"] = v.VCodec
			result["acodec"] = a.ACodec
		}
	}
	if info.URL != "" && strings.Contains(info.URL, "googlevideo.com") {
		result["single"] = info.URL
	}
	var bestSingleH int
	for _, f := range info.Formats {
		if f.URL != "" && f.VCodec != "none" && f.ACodec != "none" && strings.Contains(f.URL, "googlevideo.com") {
			if best, ok := result["single"].(string); !ok || best == "" {
				result["single"] = f.URL
				bestSingleH = f.Height
			} else if f.Height > bestSingleH {
				result["single"] = f.URL
				bestSingleH = f.Height
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	videoStreamCacheMu.Lock()
	videoStreamCache[videoID] = videoStreamCacheEntry{info: result, expires: time.Now().Add(30 * time.Minute)}
	videoStreamCacheMu.Unlock()
	return result
}

func parseCommand(message, nickname, timestamp string) {
	if strings.HasPrefix(message, "/whitman") {
		data, err := os.ReadFile("Whitman/JAMES!!!.png")
		if err != nil {
			msg := html.EscapeString("I couldn't find the Whitman image!")
			emitChatMessage(map[string]any{"message": msg, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "KAC-Bot", timestamp, "system", "")
			return
		}
		const imageID = "whitman"
		if err := saveImage(imageID, "JAMES!!!.png", data); err == nil {
			sio.Sockets().Emit("add_image", map[string]any{"id": imageID, "nickname": "KAC-Bot", "timestamp": timestamp})
			addChatlogEntry(imageID, "KAC-Bot", timestamp, "image", "")
		}
		return
	}

	if strings.HasPrefix(message, "/lyrics ") {
		query := strings.TrimPrefix(message, "/lyrics ")
		if query == "" {
			msg := html.EscapeString(nickname) + ", Please include a song name"
			emitChatMessage(map[string]any{"message": msg, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "KAC-Bot", timestamp, "system", "")
			return
		}
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", lyricsAPI, nil)
		if err != nil {
			return
		}
		q := req.URL.Query()
		q.Set("q", query)
		req.URL.RawQuery = q.Encode()
		req.Header.Set("User-Agent", "KAC-Chatroom/1.0 (chat.killallchickens.org)")
		resp, err := client.Do(req)
		if err != nil {
			msg := "I couldn't fetch lyrics! check the server status: https://status.killallchickens.org"
			emitChatMessage(map[string]any{"message": msg, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "KAC-Bot", timestamp, "system", "")
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var results []struct {
			Name        string `json:"name"`
			ArtistName  string `json:"artistName"`
			PlainLyrics string `json:"plainLyrics"`
		}
		if json.Unmarshal(body, &results) != nil || len(results) == 0 || results[0].PlainLyrics == "" {
			msg := html.EscapeString(fmt.Sprintf("I couldn't find lyrics for %s", query))
			emitChatMessage(map[string]any{"message": msg, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "KAC-Bot", timestamp, "system", "")
			return
		}
		track := results[0]
		header := html.EscapeString(track.Name + " - " + track.ArtistName)
		lyrics := strings.ReplaceAll(html.EscapeString(strings.TrimSpace(track.PlainLyrics)), "\n", "<br>")
		msg := header + "<br><br>" + lyrics
		emitChatMessage(map[string]any{"message": msg, "nickname": "KAC-Bot", "timestamp": timestamp, "system": true})
		addChatlogEntry(msg, "KAC-Bot", timestamp, "system", "")
		return
	}
	if strings.HasPrefix(message, "/8ball ") {
		question := strings.TrimPrefix(message, "/8ball ")
		if question == "" {
			msg := html.EscapeString(nickname) + ", Please include a question"
			emitChatMessage(map[string]any{"message": msg, "nickname": "8-Ball", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "8-Ball", timestamp, "system", "")
			return
		}
		resp, err := http.Get(commandAPI + "/fun/8ball")
		if err == nil {
			defer resp.Body.Close()
			var data struct {
				Response string `json:"response"`
			}
			if json.NewDecoder(resp.Body).Decode(&data) == nil {
				msg := html.EscapeString(question) + " → " + data.Response
				emitChatMessage(map[string]any{"message": msg, "nickname": "8-Ball", "timestamp": timestamp, "system": true})
				addChatlogEntry(msg, "8-Ball", timestamp, "system", "")
			}
		}
		return
	}

	if strings.HasPrefix(message, "/joke") {
		client := &http.Client{Timeout: 3 * time.Second}
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/fun/joke?api_key=%s", commandAPI, jokeAPIKey), nil)
		req.Header.Set("Origin", "https://chat.killallchickens.org")
		req.Header.Set("Referer", "https://chat.killallchickens.org")
		resp, err := client.Do(req)
		if err != nil {
			msg := "I couldn't fetch a joke! check the server status: https://status.killallchickens.org/report/uptime/a03d13d0a05da5a94df473ae71f8d648/"
			emitChatMessage(map[string]any{"message": msg, "nickname": "Joke-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "Joke-Bot", timestamp, "system", "")
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var data struct {
			Type     string `json:"type"`
			Joke     string `json:"joke"`
			Setup    string `json:"setup"`
			Delivery string `json:"delivery"`
		}
		if json.Unmarshal(body, &data) != nil {
			return
		}
		if data.Type == "single" {
			emitChatMessage(map[string]any{"message": data.Joke, "nickname": "Joke-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(data.Joke, "Joke-Bot", timestamp, "system", "")
			addToPromptHistorySafe("user", data.Joke)
			return
		}
		if data.Type == "twopart" {
			msg := data.Setup + " → " + data.Delivery
			emitChatMessage(map[string]any{"message": msg, "nickname": "Joke-Bot", "timestamp": timestamp, "system": true})
			addChatlogEntry(msg, "Joke-Bot", timestamp, "system", "")
			addToPromptHistorySafe("user", msg)
			return
		}
	}
}
