package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	genai "google.golang.org/genai"
)

func generateRandomString() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// isoNow mirrors Python's datetime.datetime.now().isoformat() (naive local time).
func isoNow() string {
	return time.Now().Format("2006-01-02T15:04:05.999999")
}

func getRealIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := r.Header.Get("Cf-Connecting-Ip"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	if r.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return host
		}
	}
	return r.RemoteAddr
}

// handleVideoRelay proxies googlevideo playback URLs through this server so
// the browser can fetch them for client-side MediaSource (googlevideo sends no
// CORS headers). Restricted to googlevideo/videoplayback URLs. Implemented as a
// plain net/http handler because fiber's SendStream misbehaves under the
// net/http adaptor.
// videoRelaySem caps concurrent video relays so a classroom full of viewers
// can't saturate the VPS's bandwidth all at once. Excess requests get a 503.
// Increased from 6 to 60 to allow native browser <video> and <audio> tags
// to comfortably fetch segments without hitting 503s on seek.
var videoRelaySem = make(chan struct{}, 60)

func handleVideoRelay(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if !strings.Contains(u, "googlevideo.com/videoplayback") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}
	select {
	case videoRelaySem <- struct{}{}:
		defer func() { <-videoRelaySem }()
	default:
		http.Error(w, "busy, try again", http.StatusServiceUnavailable)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", u, nil)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	// These headers mirror a real browser's media request. YouTube/Google
	// rejects bare UA+Referer fetches of googlevideo streams (403), so the
	// Sec-Fetch-* / Origin / Accept headers are required to be accepted.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.youtube.com/")
	req.Header.Set("Origin", "https://www.youtube.com")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Sec-Fetch-Dest", "video")
	req.Header.Set("Sec-Ch-Ua", `"Not;A=Brand";v="24", "Chromium";v="138", "Google Chrome";v="138"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Accept-Encoding", "identity")
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	} else {
		req.Header.Set("Range", "bytes=0-")
	}
	// Clone DefaultTransport so we keep Go's HTTP/2 + ALPN negotiation that
	// mirrors a browser request fingerprint; the server's bare Transport was
	// rejected by Google's bot detection on googlevideo playback URLs.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// processRSSMB returns the current resident set size (physical RAM used) of
// this process in MB, read from /proc/self/status on Linux.
func processRSSMB() float64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			var kb int
			if _, err := fmt.Sscanf(line, "VmRSS:%d", &kb); err == nil {
				return float64(kb) / 1024
			}
		}
	}
	return 0
}

// addToPromptHistorySafe mirrors utils/helpers.add_to_prompt_history_safe.
func addToPromptHistorySafe(role, text string) {
	state.mu.Lock()
	defer state.mu.Unlock()
	content := &genai.Content{
		Role:  role,
		Parts: []*genai.Part{genai.NewPartFromText(text)},
	}
	if len(state.aiPromptHistory) <= 100 {
		state.aiPromptHistory = append(state.aiPromptHistory, content)
	} else {
		state.aiPromptHistory = state.aiPromptHistory[1:]
		state.aiPromptHistory = append(state.aiPromptHistory, content)
	}
}

// imageExts are the file extensions recognized for stored chat images. Keep in
// sync with the /get_image route.
var imageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".tiff", ".bmp", ".psd", ".raw", ".svg", ".heif", ".jp2", ".jpx", ".jpm", ".j2k", ".mj2"}

// maxAIHistoryImages caps how many actual image parts are kept in the bot's
// prompt history. Images cost far more input tokens than text and hold large
// byte slices in memory, so older images beyond this limit degrade to their
// text caption instead of being sent to the model.
const maxAIHistoryImages = 10

// loadImageByID reads a stored chat image by its base id (no extension),
// returning the raw bytes and the detected MIME type. Returns empty bytes if
// the image is missing or the id looks like a path traversal.
func loadImageByID(id string) ([]byte, string) {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return nil, ""
	}
	basePath := filepath.Join(chatlogsDir, "images", id)
	for _, e := range imageExts {
		full := basePath + e
		if _, err := os.Stat(full); err == nil {
			b, err := os.ReadFile(full)
			if err == nil {
				return b, http.DetectContentType(b)
			}
		}
	}
	return nil, ""
}

// countHistoryImagesLocked counts the image (inline data) parts currently in
// the prompt history. Callers must hold state.mu.
func countHistoryImagesLocked() int {
	n := 0
	for _, c := range state.aiPromptHistory {
		for _, p := range c.Parts {
			if p.InlineData != nil {
				n++
			}
		}
	}
	return n
}

// trimHistoryImagesLocked degrades the oldest image-bearing entries in the
// prompt history down to text-only until at most max inline images remain.
// Callers must hold state.mu.
func trimHistoryImagesLocked(max int) {
	for countHistoryImagesLocked() > max {
		stripped := false
		for i, c := range state.aiPromptHistory {
			if c == nil {
				continue
			}
			hasImage := false
			for _, p := range c.Parts {
				if p.InlineData != nil {
					hasImage = true
					break
				}
			}
			if !hasImage {
				continue
			}
			var textParts []*genai.Part
			for _, p := range c.Parts {
				if p.InlineData == nil {
					textParts = append(textParts, p)
				}
			}
			if len(textParts) == 0 {
				textParts = []*genai.Part{genai.NewPartFromText("(an image was shared here)")}
			}
			state.aiPromptHistory[i] = &genai.Content{Role: c.Role, Parts: textParts}
			stripped = true
			break
		}
		if !stripped {
			break
		}
	}
}

// addImageToPromptHistorySafe records a chat image in the bot's memory with its
// actual pixel data so the model can reference it later, subject to the image
// cap. The image is loaded from disk by id.
func addImageToPromptHistorySafe(role, text, id string) {
	b, mime := loadImageByID(id)
	parts := []*genai.Part{genai.NewPartFromText(text)}
	if len(b) > 0 && mime != "" {
		parts = append(parts, genai.NewPartFromBytes(b, mime))
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	content := &genai.Content{Role: role, Parts: parts}
	if len(state.aiPromptHistory) <= 100 {
		state.aiPromptHistory = append(state.aiPromptHistory, content)
	} else {
		state.aiPromptHistory = state.aiPromptHistory[1:]
		state.aiPromptHistory = append(state.aiPromptHistory, content)
	}
	trimHistoryImagesLocked(maxAIHistoryImages)
}
