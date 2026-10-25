// Package llmclient talks to any OpenAI-compatible "chat completions" HTTP
// endpoint — this single client works for a local runtime (e.g. Ollama at
// http://ollama:11434/v1) and for cloud providers (e.g. OpenAI at
// https://api.openai.com/v1) alike, since they share the same request and
// response shape, including function/tool calling. Only the base URL, API
// key and model name change.
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
	return newClient(baseURL, apiKey, model, 30*time.Second)
}

func newClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: timeout, CheckRedirect: noRedirect},
	}
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type tool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Tools       []tool        `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func toRequestTools(tools []domain.ToolDefinition) []tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, tool{
			Type: "function",
			Function: toolFunction{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			},
		})
	}
	return out
}

// Temperature: 0.7 for ordinary conversation (varied, natural replies), but
// much lower whenever a tool is offered — a low temperature is what makes
// small/local models reliably emit well-formed tool-call JSON instead of
// garbling the schema, verified empirically against llama3.2 and qwen2.5.
// maxAnswerTokens caps every answer: an unbounded one (a model asked for its
// own prompt, or looping) outlives the proxy's timeout and the customer just
// sees a gateway error. 700 tokens is far more than any reply here needs.
const maxAnswerTokens = 700

const (
	chatTemperature = 0.7
	toolTemperature = 0.1
)

func (c *Client) Complete(ctx context.Context, messages []domain.Message, tools []domain.ToolDefinition) (domain.CompletionResult, error) {
	msgs := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	temperature := chatTemperature
	if len(tools) > 0 {
		temperature = toolTemperature
	}
	body, err := json.Marshal(chatRequest{
		Model: c.model, Messages: msgs, Temperature: temperature, MaxTokens: maxAnswerTokens, Tools: toRequestTools(tools),
	})
	if err != nil {
		return domain.CompletionResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return domain.CompletionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return domain.CompletionResult{}, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
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
	if len(out.Choices) == 0 {
		return domain.CompletionResult{}, fmt.Errorf("AI endpoint returned no choices")
	}

	msg := out.Choices[0].Message
	if len(msg.ToolCalls) > 0 {
		tc := msg.ToolCalls[0]
		return domain.CompletionResult{
			ToolCall: &domain.ToolCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
		}, nil
	}

	// Small local models sometimes write the call as text instead of emitting
	// tool_calls (see recover.go): recover it, or at least never relay it raw.
	if tc := recoverTextToolCall(msg.Content, tools); tc != nil {
		return domain.CompletionResult{ToolCall: tc}, nil
	}
	if looksLikeToolCall(msg.Content, tools) {
		return domain.CompletionResult{Content: unreadableToolCallMessage}, nil
	}
	return domain.CompletionResult{Content: msg.Content}, nil
}

const unreadableToolCallMessage = "Désolé, je n'ai pas bien compris ta demande. Peux-tu la reformuler ?"

// noRedirect stops a redirect from forwarding the API key to another host.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
