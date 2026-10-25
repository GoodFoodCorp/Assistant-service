package llmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

// anthropicClient talks to Anthropic's Messages API, whose dialect differs
// from the OpenAI one: the system prompt is a top-level field, auth is an
// x-api-key header, turns must alternate user/assistant, and a function call
// comes back as a "tool_use" content block.
type anthropicClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

const (
	anthropicVersion   = "2023-06-01"
	anthropicMaxTokens = maxAnswerTokens
)

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

type anthropicBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type anthropicResponse struct {
	Content []anthropicBlock `json:"content"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *anthropicClient) Complete(ctx context.Context, messages []domain.Message, tools []domain.ToolDefinition) (domain.CompletionResult, error) {
	var system []string
	var turns []anthropicMessage
	for _, m := range messages {
		if m.Role == domain.RoleSystem {
			system = append(system, m.Content)
			continue
		}
		// Anthropic rejects two consecutive turns with the same role: merge them.
		if n := len(turns); n > 0 && turns[n-1].Role == m.Role {
			turns[n-1].Content += "\n\n" + m.Content
			continue
		}
		turns = append(turns, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	temperature := chatTemperature
	var reqTools []anthropicTool
	if len(tools) > 0 {
		temperature = toolTemperature
		for _, t := range tools {
			reqTools = append(reqTools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
		}
	}

	body, err := json.Marshal(anthropicRequest{
		Model: c.model, MaxTokens: anthropicMaxTokens, System: strings.Join(system, "\n\n"),
		Messages: turns, Temperature: temperature, Tools: reqTools,
	})
	if err != nil {
		return domain.CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return domain.CompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return domain.CompletionResult{}, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return domain.CompletionResult{}, fmt.Errorf("invalid AI endpoint response: %w", err)
	}
	if resp.StatusCode >= 300 {
		message := out.Error.Message
		if message == "" {
			message = fmt.Sprintf("AI endpoint returned %d", resp.StatusCode)
		}
		return domain.CompletionResult{}, fmt.Errorf("%s", message)
	}

	var text []string
	for _, b := range out.Content {
		switch b.Type {
		case "tool_use":
			args, err := json.Marshal(b.Input)
			if err != nil {
				return domain.CompletionResult{}, err
			}
			return domain.CompletionResult{ToolCall: &domain.ToolCall{Name: b.Name, Arguments: string(args)}}, nil
		case "text":
			text = append(text, b.Text)
		}
	}
	if len(text) == 0 {
		return domain.CompletionResult{}, fmt.Errorf("AI endpoint returned no content")
	}
	return domain.CompletionResult{Content: strings.Join(text, "\n")}, nil
}
