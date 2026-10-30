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
	"net/url"
	"strings"
	"sync/atomic"
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
		baseURL: strings.TrimSuffix(azureOpenAIBase(baseURL), "/"),
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
	// Reasoning models (OpenAI o-series, GPT-5) turn down max_tokens and any
	// temperature but their own. What the endpoint refused is learned from its
	// first 400 and kept for the next calls.
	completionTokens   atomic.Bool
	defaultTemperature atomic.Bool
}

// azureOpenAIBase points an Azure OpenAI / AI Foundry address (the resource,
// or a project URL copied from the portal) at its OpenAI-compatible v1 API:
// the only one that needs no api-version query parameter.
func azureOpenAIBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{".openai.azure.com", ".services.ai.azure.com", ".cognitiveservices.azure.com"} {
		if strings.HasSuffix(host, suffix) {
			if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/openai/v1") {
				return raw
			}
			return u.Scheme + "://" + u.Host + "/openai/v1"
		}
	}
	return raw
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
	Model               string        `json:"model"`
	Messages            []chatMessage `json:"messages"`
	Temperature         *float64      `json:"temperature,omitempty"`
	MaxTokens           int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Tools               []tool        `json:"tools,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
		Param   string `json:"param"`
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

// maxReasoningTokens is the same cap for a reasoning model, whose budget also
// pays for its hidden reasoning.
const maxReasoningTokens = 2500

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

	// At most one retry per parameter the endpoint can turn down.
	var out chatResponse
	for attempt := 0; ; attempt++ {
		status, err := c.send(ctx, c.request(msgs, temperature, tools), &out)
		if err != nil {
			return domain.CompletionResult{}, err
		}
		if status == http.StatusBadRequest && attempt < 2 && c.learn(out.Error.Param) {
			out = chatResponse{}
			continue
		}
		if status >= 300 {
			message := out.Error.Message
			if message == "" {
				message = fmt.Sprintf("AI endpoint returned %d", status)
			}
			return domain.CompletionResult{}, fmt.Errorf("%s", message)
		}
		break
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

func (c *Client) request(msgs []chatMessage, temperature float64, tools []domain.ToolDefinition) chatRequest {
	r := chatRequest{Model: c.model, Messages: msgs, Tools: toRequestTools(tools)}
	if !c.defaultTemperature.Load() {
		r.Temperature = &temperature
	}
	if c.completionTokens.Load() {
		r.MaxCompletionTokens = maxReasoningTokens
	} else {
		r.MaxTokens = maxAnswerTokens
	}
	return r
}

// learn records a parameter the endpoint refused; false when there is nothing
// left to change, so the error goes back to the caller.
func (c *Client) learn(param string) bool {
	switch param {
	case "max_tokens":
		return c.completionTokens.CompareAndSwap(false, true)
	case "temperature":
		return c.defaultTemperature.CompareAndSwap(false, true)
	}
	return false
}

func (c *Client) send(ctx context.Context, payload chatRequest, out *chatResponse) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("AI endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, fmt.Errorf("invalid AI endpoint response: %w", err)
	}
	return resp.StatusCode, nil
}

const unreadableToolCallMessage = "Désolé, je n'ai pas bien compris ta demande. Peux-tu la reformuler ?"

// noRedirect stops a redirect from forwarding the API key to another host.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
