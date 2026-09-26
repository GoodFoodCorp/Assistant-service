// Package llmclient talks to any OpenAI-compatible "chat completions" HTTP
// endpoint — this single client works for a local runtime (e.g. Ollama at
// http://ollama:11434/v1) and for cloud providers (e.g. OpenAI at
// https://api.openai.com/v1) alike, since they share the same request and
// response shape. Only the base URL, API key and model name change.
package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"goodfood/assistant-service/internal/domain"
)

// New returns a real client against baseURL, or — when baseURL is empty — a
// FakeProvider so the chat stays demoable without any AI endpoint or key
// configured, mirroring payment-service's offline FakeGateway.
func New(baseURL, apiKey, model string) domain.LLMProvider {
	if baseURL == "" {
		return NewFakeProvider()
	}
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) Complete(ctx context.Context, messages []domain.Message) (string, error) {
	msgs := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	body, err := json.Marshal(chatRequest{Model: c.model, Messages: msgs, Temperature: 0.7})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("invalid AI endpoint response: %w", err)
	}
	if resp.StatusCode >= 300 {
		message := out.Error.Message
		if message == "" {
			message = fmt.Sprintf("AI endpoint returned %d", resp.StatusCode)
		}
		return "", fmt.Errorf("%s", message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("AI endpoint returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}
