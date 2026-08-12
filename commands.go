package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Port of utils/command_helper.py
const commandAPI = "https://api.killallchickens.org"
const jokeAPIKey = "9813a54654f81bcc3f69fe1489f05e016d944c0b7d85df43feec77bf89ae97e7"
const lyricsAPI = "https://lrclib.net/api/search"

// innertubeWebKey is the public API key embedded in youtube.com's web client,
// used for keyless search scraping.
const innertubeWebKey = "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8"

func searchVideos(query string) []VideoSearchResult {
	body := fmt.Sprintf(`{"context":{"client":{"clientName":"WEB","clientVersion":"2.20240801.00.00","hl":"en","gl":"US"}},"query":%s}`, strconv.Quote(query))
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("POST", "https://www.youtube.com/youtubei/v1/search?key="+innertubeWebKey, strings.NewReader(body))
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
	return deduped
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

// resolveVideoStreamURL uses yt-dlp in simulate mode to fetch a direct,
// time-limited stream URL for a video. Nothing is downloaded or stored.
func resolveVideoStreamURL(videoID string) string {
	streamResolveSem <- struct{}{}
	defer func() { <-streamResolveSem }()

	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ytdlp, "-g", "--no-warnings",
		"-f", "best[height<=1080]/best",
		"https://www.youtube.com/watch?v="+videoID)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if url == "" || strings.HasPrefix(url, "ERROR:") || !strings.Contains(url, "googlevideo.com") {
		return ""
	}
	return url
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
