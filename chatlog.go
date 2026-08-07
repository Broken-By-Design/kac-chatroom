package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const chatlogsDir = "chatlogs"

var (
	logMu          sync.Mutex
	currentLogFile string

	// chicagoLoc is the timezone the chatlog day rolls over at (America/Chicago).
	chicagoLoc = func() *time.Location {
		loc, err := time.LoadLocation("America/Chicago")
		if err != nil {
			return time.Local
		}
		return loc
	}()

	imageHashMu sync.Mutex
	imageHashes = map[string]string{} // sha256 hex -> image id
)

// ChatlogEntry matches the JSON shape written by utils/helpers.add_chatlog_entry.
type ChatlogEntry struct {
	Nickname  string `json:"nickname"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Message   string `json:"message,omitempty"`
	ID        string `json:"id,omitempty"`
	Recipient string `json:"recipient,omitempty"`
}

// chicagoNow returns the current time in America/Chicago.
func chicagoNow() time.Time {
	return time.Now().In(chicagoLoc)
}

// todayKey returns today's date (YYYY-MM-DD) in America/Chicago.
func todayKey() string {
	return chicagoNow().Format("2006-01-02")
}

// chatlogPath returns the log file path for the given day key.
func chatlogPath(day string) string {
	return filepath.Join(chatlogsDir, day+".json")
}

func ensureChatlogDirs() {
	_ = os.MkdirAll(chatlogsDir, 0755)
	_ = os.MkdirAll(filepath.Join(chatlogsDir, "images"), 0755)
}

// selectTodayLog makes today's log (in Chicago time) the active one, creating
// it if it does not exist yet. Call once at startup and after any rollover.
func selectTodayLog() {
	logMu.Lock()
	defer logMu.Unlock()
	ensureChatlogDirs()
	currentLogFile = chatlogPath(todayKey())
	if _, err := os.Stat(currentLogFile); os.IsNotExist(err) {
		_ = os.WriteFile(currentLogFile, nil, 0644)
	}
	fmt.Printf("Active chatlog: %s\n", currentLogFile)
}

func addChatlogEntry(message, nickname, timestamp, typ, recipient string) {
	entry := ChatlogEntry{Nickname: nickname, Timestamp: timestamp, Type: typ}
	if typ == "image" {
		entry.ID = message
	} else {
		entry.Message = message
	}
	if recipient != "" {
		entry.Recipient = recipient
	}
	appendToChatlog(entry)
}

// appendToChatlog appends a single entry as one JSON line (JSONL format).
func appendToChatlog(entry ChatlogEntry) {
	logMu.Lock()
	defer logMu.Unlock()
	if currentLogFile == "" {
		currentLogFile = chatlogPath(todayKey())
		_ = os.MkdirAll(filepath.Dir(currentLogFile), 0755)
	}
	f, err := os.OpenFile(currentLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open chatlog for append: %v\n", err)
		return
	}
	defer f.Close()
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// readChatlogs reads and returns the active day's chatlog entries.
func readChatlogs() []ChatlogEntry {
	logMu.Lock()
	defer logMu.Unlock()
	if currentLogFile == "" {
		return nil
	}
	return readChatlogFile(currentLogFile)
}

func readChatlogFile(path string) []ChatlogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var logs []ChatlogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e ChatlogEntry
		if json.Unmarshal(line, &e) == nil {
			logs = append(logs, e)
		}
	}
	return logs
}

func loadRecentChatContext(num int) []ChatlogEntry {
	logs := readChatlogs()
	if len(logs) == 0 {
		return nil
	}
	start := 0
	if len(logs) > num {
		start = len(logs) - num
	}
	return logs[start:]
}

// clearChatlogs wipes all stored JSON logs and starts a fresh log for today.
// Previous days' logs are not read anywhere, so removing them keeps the disk
// clean. Images are left untouched.
func clearChatlogs() {
	logMu.Lock()
	defer logMu.Unlock()
	ensureChatlogDirs()
	entries, _ := os.ReadDir(chatlogsDir)
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			_ = os.Remove(filepath.Join(chatlogsDir, e.Name()))
		}
	}
	currentLogFile = chatlogPath(todayKey())
	_ = os.WriteFile(currentLogFile, nil, 0644)
	fmt.Printf("Chatlog cleared. New log: %s\n", currentLogFile)
}

// findImageIDByHash returns the id of a stored image whose content matches the
// given hash, or "" if none. A memory cache avoids rescanning the directory
// for every image upload.
func findImageIDByHash(sum [32]byte) string {
	key := hex.EncodeToString(sum[:])
	imageHashMu.Lock()
	defer imageHashMu.Unlock()
	if id, ok := imageHashes[key]; ok {
		return id
	}
	dir := filepath.Join(chatlogsDir, "images")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		h := sha256.Sum256(b)
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		imageHashes[hex.EncodeToString(h[:])] = id
		if hex.EncodeToString(h[:]) == key {
			return id
		}
	}
	return ""
}

func cacheImageID(sum [32]byte, id string) {
	imageHashMu.Lock()
	defer imageHashMu.Unlock()
	imageHashes[hex.EncodeToString(sum[:])] = id
}

// saveImage writes an image to the images directory using the given base id
// and the extension taken from the original filename.
func saveImage(id, name string, data []byte) error {
	dir := filepath.Join(chatlogsDir, "images")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		ext = ".png"
	}
	return os.WriteFile(filepath.Join(dir, id+ext), data, 0644)
}
