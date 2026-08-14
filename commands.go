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

// streamResolveSem caps concurrent yt-dlp subprocesses. Kept at 1 so the
// Python process never stacks with itself under the container's soft memory
// limit; the persistent cache + singleflight make a single slot sufficient.
var streamResolveSem = make(chan struct{}, 1)

// ytCredProfile is one optional set of YouTube credentials (cookies + optional
// PO token). Profiles are loaded once at startup from env vars.
type ytCredProfile struct {
	label       string
	cookiesFile string
	poToken     string
	visitorData string
}

// hasFiles reports whether this profile has at least a usable cookie file on
// disk or a PO token. A configured-but-missing cookie file is treated as absent
// so yt-dlp never fails on a phantom --cookies path.
func (p ytCredProfile) hasFiles() bool {
	if p.poToken != "" {
		return true
	}
	if p.cookiesFile == "" {
		return false
	}
	if _, err := os.Stat(p.cookiesFile); err == nil {
		return true
	}
	return false
}

// ytProfiles holds the optional A→B credential sets used for failover.
var ytProfiles []ytCredProfile

// ytCredHealth tracks the live status of each credential profile so the admin
// panel can flag cookies/tokens that stopped working and swap them out.
type ytCredHealthStatus struct {
	Label       string    `json:"label"`
	Enabled     bool      `json:"enabled"`
	CookiesFile string    `json:"cookies_file,omitempty"`
	HasPoToken  bool      `json:"has_po_token"`
	Status      string    `json:"status"` // "ok" | "failed" | "untested"
	Flagged     bool      `json:"flagged"`
	LastError   string    `json:"last_error,omitempty"`
	LastTested  time.Time `json:"last_tested,omitempty"`
	FailStreak  int       `json:"fail_streak"`
}

var (
	ytCredHealthMu sync.Mutex
	ytCredHealth   = map[string]*ytCredHealthStatus{}
)

// recordCredResult marks a profile as having failed or succeeded on a resolve.
func recordCredResult(label string, ok bool, errStr string) {
	if label == "" {
		return
	}
	ytCredHealthMu.Lock()
	defer ytCredHealthMu.Unlock()
	h := ytCredHealth[label]
	if h == nil {
		return
	}
	h.LastTested = time.Now()
	h.LastError = errStr
	if ok {
		h.Status = "ok"
		h.Flagged = false
		h.FailStreak = 0
	} else {
		h.Status = "failed"
		h.FailStreak++
		if h.FailStreak >= 2 {
			h.Flagged = true
		}
	}
}

// ytCredHealthList returns a snapshot of all configured profiles.
func ytCredHealthList() []ytCredHealthStatus {
	ytCredHealthMu.Lock()
	defer ytCredHealthMu.Unlock()
	out := make([]ytCredHealthStatus, 0, len(ytCredHealth))
	for _, h := range ytCredHealth {
		c := *h
		out = append(out, c)
	}
	return out
}

// ytCredHealthReset clears flagging/failure state (call after swapping creds).
func ytCredHealthReset() {
	ytCredHealthMu.Lock()
	defer ytCredHealthMu.Unlock()
	for _, h := range ytCredHealth {
		h.Status = "untested"
		h.Flagged = false
		h.LastError = ""
		h.FailStreak = 0
		h.LastTested = time.Time{}
	}
}

// ytCredProbeVideo is a short, stable public video used to validate that a
// credential set can still obtain playback on YouTube.
const ytCredProbeVideo = "dQw4w9WgXcQ"

// testYTCredentials actively probes each configured credential profile
// against a known-good video and updates their health so the admin panel can
// confirm fresh credentials work after swapping them out.
func testYTCredentials() []ytCredHealthStatus {
	for _, p := range ytProfiles {
		attempt := ytResolveAttempt{
			profileLabel: p.label,
			cookiesFile:  p.cookiesFile,
			extractorArg: "youtube:player_client=web",
			format:       "best[height<=720]/best",
		}
		if p.poToken != "" {
			attempt.extractorArg = "youtube:player_client=web;po_token=web+" + p.poToken
		}
		// runYtdlpResolve already updates this profile's health from its own
		// success/failure outcome; we just run it and then read the snapshot.
		_ = runYtdlpResolve(ytCredProbeVideo, attempt)
	}
	return ytCredHealthList()
}

// loadYTCredentials reads optional YouTube credentials from the environment.
// All vars are off-by-default; empty profile means credential-free operation.
func loadYTCredentials() {
	profile := func(label, cookiesEnv, potEnv, visEnv string) ytCredProfile {
		pot := os.Getenv(potEnv)
		if i := strings.Index(pot, "&"); i >= 0 {
			pot = pot[:i] // strip &ump=1&srfvp=1 trailing params
		}
		return ytCredProfile{
			label:       label,
			cookiesFile: os.Getenv(cookiesEnv),
			poToken:     pot,
			visitorData: os.Getenv(visEnv),
		}
	}
	var profs []ytCredProfile
	if p := profile("Account A", "YT_COOKIES_FILE", "YT_PO_TOKEN", "YT_VISITOR_DATA"); p.hasFiles() {
		profs = append(profs, p)
	}
	if p := profile("Account B", "YT_COOKIES_FILE2", "YT_PO_TOKEN2", "YT_VISITOR_DATA2"); p.hasFiles() {
		profs = append(profs, p)
	}
	ytProfiles = profs

	ytCredHealthMu.Lock()
	ytCredHealth = map[string]*ytCredHealthStatus{}
	for _, p := range ytProfiles {
		ytCredHealth[p.label] = &ytCredHealthStatus{
			Label:       p.label,
			Enabled:     true,
			CookiesFile: p.cookiesFile,
			HasPoToken:  p.poToken != "",
			Status:      "untested",
		}
	}
	ytCredHealthMu.Unlock()
}

// ytResolveAttempt is one yt-dlp invocation configuration (credentials +
// player client), tried in order until one returns a usable stream.
type ytResolveAttempt struct {
	profileLabel string
	cookiesFile  string
	extractorArg string
	format       string // if empty, a default strict DASH-preferring selector is used
}

// resolveFlight dedups concurrent resolutions of the same videoID so a
// classroom full of users clicking the same video shares one yt-dlp call.
var (
	resolveFlightMu sync.Mutex
	resolveFlight   = map[string]chan map[string]any{}
)

// singleResolve runs fn once per videoID; concurrent callers wait for and
// share its result.
func singleResolve(videoID string, fn func() map[string]any) map[string]any {
	resolveFlightMu.Lock()
	if ch, ok := resolveFlight[videoID]; ok {
		resolveFlightMu.Unlock()
		return <-ch
	}
	ch := make(chan map[string]any, 1)
	resolveFlight[videoID] = ch
	resolveFlightMu.Unlock()

	defer func() {
		resolveFlightMu.Lock()
		delete(resolveFlight, videoID)
		resolveFlightMu.Unlock()
	}()
	res := fn()
	ch <- res
	return res
}

// buildResolveAttempts returns the ordered list of yt-dlp invocations:
// anonymous with yt-dlp's default client first (fast and reliably yields a
// playable DASH pair), then anonymous cross-client fallbacks, then optionally
// credentialed accounts (cookies + PO token) as a last resort if anonymous is
// bot-blocked. Credentialed "web" playback only exposes combined formats and is
// slower, so it's tried after the anonymous paths.
func buildResolveAttempts() []ytResolveAttempt {
	var attempts []ytResolveAttempt

	// 1) Anonymous, yt-dlp default player client (fastest, best formats).
	attempts = append(attempts, ytResolveAttempt{extractorArg: ""})

	// 2) Anonymous cross-client fallbacks.
	for _, client := range []string{"web_embedded", "tv"} {
		attempts = append(attempts, ytResolveAttempt{extractorArg: "youtube:player_client=" + client})
	}

	// Credentialed "web" + PO-token sessions are rate-limited to combined
	// formats only (no separate video/audio), so a strict DASH selector would
	// error "Requested format is not available". Use a lenient selector that
	// accepts whatever YouTube exposes for these sessions.
	const credFormat = "best[height<=720]/best"

	// 3) Credentialed accounts (PO token / cookies) as a last resort.
	for _, p := range ytProfiles {
		ea := "youtube:player_client=web"
		if p.poToken != "" {
			ea += ";po_token=web+" + p.poToken
			// PO tokens for logged-in sessions are bound to the account and
			// must NOT carry visitor_data. Only use visitor data when there
			// are no cookies (anonymous session).
			if p.visitorData != "" && p.cookiesFile == "" {
				ea += ";visitor_data=" + p.visitorData
			}
		}
		attempts = append(attempts, ytResolveAttempt{profileLabel: p.label, cookiesFile: p.cookiesFile, extractorArg: ea, format: credFormat})
	}

	// 4) Credentialed web without a PO token (in case the token expired).
	for _, p := range ytProfiles {
		attempts = append(attempts, ytResolveAttempt{
			profileLabel: p.label,
			cookiesFile:  p.cookiesFile,
			extractorArg: "youtube:player_client=web",
			format:       credFormat,
		})
	}

	return attempts
}

// ytdlpInfo is the subset of yt-dlp's -j JSON output we consume.
type ytdlpInfo struct {
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

// runYtdlpResolve runs one yt-dlp -j invocation and returns the usable stream
// info, or nil if nothing playable came back.
func runYtdlpResolve(videoID string, attempt ytResolveAttempt) map[string]any {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"-j", "--no-warnings"}
	if attempt.cookiesFile != "" {
		args = append(args, "--cookies", attempt.cookiesFile)
	}
	if attempt.extractorArg != "" {
		args = append(args, "--extractor-args", attempt.extractorArg)
	}
	format := attempt.format
	if format == "" {
		// Prefer a 1080p H.264 DASH pair + best m4a audio, then fall back.
		format = "bestvideo[height<=1080][ext=mp4][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=720]/best"
	}
	args = append(args,
		"-f", format,
		"https://www.youtube.com/watch?v="+videoID)
	cmd := exec.CommandContext(ctx, ytdlp, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Capture a meaningful error line for the admin panel. yt-dlp's stderr
		// ends with "ERROR: ..." which usually explains the failure.
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "ERROR:"); i >= 0 {
			msg = strings.TrimSpace(msg[i+len("ERROR:"):])
		}
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		if msg != "" && attempt.profileLabel != "" {
			recordCredResult(attempt.profileLabel, false, msg)
		}
		return nil
	}
	var info ytdlpInfo
	if json.Unmarshal(out, &info) != nil {
		if attempt.profileLabel != "" {
			recordCredResult(attempt.profileLabel, false, "unparseable yt-dlp output")
		}
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
	if info.URL != "" && strings.Contains(info.URL, "googlevideo.com/videoplayback") {
		result["single"] = info.URL
	}
	var bestSingleH int
	for _, f := range info.Formats {
		// Only direct videoplayback URLs are playable by the browser.
		// HLS manifests (manifest.googlevideo.com) are excluded because
		// <video> cannot play them natively outside Safari.
		if f.URL != "" && f.VCodec != "none" && f.ACodec != "none" && strings.Contains(f.URL, "googlevideo.com/videoplayback") {
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
		if attempt.profileLabel != "" {
			recordCredResult(attempt.profileLabel, false, "no playable stream in yt-dlp output")
		}
		return nil
	}
	if attempt.profileLabel != "" {
		recordCredResult(attempt.profileLabel, true, "")
	}
	return result
}

// resolveVideoStreamInfo uses yt-dlp in simulate mode to fetch direct,
// time-limited stream URLs for a video. Nothing is downloaded or stored.
//
// It prefers a 1080p H.264 video-only DASH stream paired with the best m4a
// audio, which the client muxes via MediaSource. YouTube no longer offers
// combined (video+audio) formats above 360p, so a "single" 360p URL is
// returned as a fallback when no DASH pair is available.
func resolveVideoStreamInfo(videoID string, fresh bool) map[string]any {
	if !fresh {
		if e, ok := streamCacheGet(videoID); ok {
			if e.Failed {
				return nil
			}
			return e.Info
		}
	} else {
		streamCacheDelete(videoID)
	}

	return singleResolve(videoID, func() map[string]any {
		streamResolveSem <- struct{}{}
		defer func() { <-streamResolveSem }()

		var result map[string]any
		for _, attempt := range buildResolveAttempts() {
			if result = runYtdlpResolve(videoID, attempt); result != nil {
				break
			}
		}
		if result == nil {
			streamCachePutFailure(videoID)
		} else {
			streamCachePut(videoID, result)
		}
		return result
	})
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
