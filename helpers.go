package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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
func handleVideoRelay(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if !strings.Contains(u, "googlevideo.com/videoplayback") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.youtube.com/")
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	} else {
		req.Header.Set("Range", "bytes=0-")
	}
	client := &http.Client{}
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
