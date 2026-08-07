package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// Port of utils/command_helper.py
const commandAPI = "https://api.killallchickens.org"
const jokeAPIKey = "9813a54654f81bcc3f69fe1489f05e016d944c0b7d85df43feec77bf89ae97e7"

func parseCommand(message, nickname, timestamp string) {
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
