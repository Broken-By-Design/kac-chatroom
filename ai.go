package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	genai "google.golang.org/genai"
)

var aiClient *genai.Client
var aiPersonality string

var aiModel = "gemini-3.5-flash-lite"

const aiFallbackModel = "gemini-3.1-flash-lite"

func newGenAIClient(apiKey string) *genai.Client {
	if apiKey == "" {
		return nil
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		fmt.Printf("Failed to create Gemini client: %v\n", err)
		return nil
	}
	return client
}

func loadAIPersonality() {
	b, err := os.ReadFile("ai_personality.txt")
	if err != nil {
		aiPersonality = ""
		return
	}
	aiPersonality = string(b)
}

// generateResponse mirrors main.py generate_response.
func generateResponse(message, user string, enableGoogleSearch, image bool, imageID string) string {
	if aiClient == nil {
		return "The bot is not available right now."
	}

	contents := state.promptHistory()

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{genai.NewPartFromText(aiPersonality)},
		},
		ResponseModalities: []string{"TEXT"},
	}
	if enableGoogleSearch {
		config.Tools = []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}
	}

	if image {
		if imageID == "" {
			return "I need an image to look at."
		}
		imagePath := fmt.Sprintf("http://0.0.0.0:5000/get_image/%s", imageID)
		resp, err := http.Get(imagePath)
		if err != nil {
			return "I couldn't fetch that image."
		}
		imageBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		mimeType := http.DetectContentType(imageBytes)

		parts := []*genai.Part{
			genai.NewPartFromText(fmt.Sprintf("%s: %s", user, message)),
			genai.NewPartFromBytes(imageBytes, mimeType),
		}
		contents = append(contents, &genai.Content{Role: "user", Parts: parts})
	} else {
		contents = append(contents, &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{genai.NewPartFromText(fmt.Sprintf("%s: %s", user, message))},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	response, err := aiClient.Models.GenerateContent(ctx, aiModel, contents, config)
	if err != nil {
		var apiErr genai.APIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests {
			if aiModel != aiFallbackModel {
				fmt.Printf("Rate limit hit on %s, falling back to %s\n", aiModel, aiFallbackModel)
				aiModel = aiFallbackModel
			}
			response, err = aiClient.Models.GenerateContent(ctx, aiModel, contents, config)
		}
		if err != nil && enableGoogleSearch {
			fmt.Printf("Request with Google Search failed (%v), retrying without it\n", err)
			config.Tools = nil
			response, err = aiClient.Models.GenerateContent(ctx, aiModel, contents, config)
		}
		if err != nil {
			fmt.Printf("Gemini error: %v\n", err)
			return "I ran into an error thinking about that."
		}
	}

	var text strings.Builder
	if len(response.Candidates) > 0 && response.Candidates[0].Content != nil {
		for _, part := range response.Candidates[0].Content.Parts {
			if part.Text != "" {
				text.WriteString(part.Text)
			}
		}
	}

	out := strings.TrimSpace(text.String())
	out = strings.ReplaceAll(out, "\n", " ")

	if image {
		addImageToPromptHistorySafe("user", fmt.Sprintf("%s sent an image and asked %s", user, message), imageID)
	} else {
		addToPromptHistorySafe("user", fmt.Sprintf("%s: %s", user, message))
	}
	addToPromptHistorySafe("model", out)
	return out
}

// initializeAIHistoryFromLog mirrors main.py initialize_ai_history_from_log.
func initializeAIHistoryFromLog() {
	if state.promptHistoryLen() > 0 {
		return
	}
	logs := loadRecentChatContext(100)
	if len(logs) == 0 {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.aiPromptHistory = []*genai.Content{}
	for _, log := range logs {
		role := "user"
		if log.Nickname == "KAC-Bot" {
			role = "model"
		}
		if log.Type == "text" {
			state.aiPromptHistory = append(state.aiPromptHistory, &genai.Content{
				Role:  role,
				Parts: []*genai.Part{genai.NewPartFromText(log.Message)},
			})
		} else if log.Type == "image" {
			content := &genai.Content{
				Role:  role,
				Parts: []*genai.Part{genai.NewPartFromText(fmt.Sprintf("%s sent an image.", log.Nickname))},
			}
			if b, mime := loadImageByID(log.ID); len(b) > 0 && mime != "" {
				content.Parts = append(content.Parts, genai.NewPartFromBytes(b, mime))
			}
			state.aiPromptHistory = append(state.aiPromptHistory, content)
		}
	}
	trimHistoryImagesLocked(maxAIHistoryImages)
}
