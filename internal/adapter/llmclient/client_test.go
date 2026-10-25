package llmclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodfood/assistant-service/internal/domain"
)

// fakeEndpoint answers every chat completion with one fixed message.
func fakeEndpoint(t *testing.T, message map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}})
	}))
}

func complete(t *testing.T, srv *httptest.Server, tools []domain.ToolDefinition) domain.CompletionResult {
	t.Helper()
	c := New(srv.URL, "", "test-model")
	res, err := c.Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "salut"}}, tools)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return res
}

func TestComplete_RecoversTextualToolCall(t *testing.T) {
	srv := fakeEndpoint(t, map[string]any{
		"role":    "assistant",
		"content": `Propose_order("items": [{"name": "Burger Deluxe", "quantity": 2}], "delivery_address": "8 rue de la Paix")`,
	})
	defer srv.Close()

	res := complete(t, srv, orderTool)
	if res.ToolCall == nil || res.ToolCall.Name != "propose_order" {
		t.Fatalf("expected a recovered propose_order call, got %+v", res)
	}
	if !strings.Contains(res.ToolCall.Arguments, "8 rue de la Paix") {
		t.Errorf("arguments lost the address: %s", res.ToolCall.Arguments)
	}
}

func TestComplete_NeverShowsUnparseablePseudoCall(t *testing.T) {
	srv := fakeEndpoint(t, map[string]any{"role": "assistant", "content": `propose_order("oups, pas fermé`})
	defer srv.Close()

	res := complete(t, srv, orderTool)
	if res.ToolCall != nil {
		t.Fatalf("nothing to recover, got %+v", res.ToolCall)
	}
	if strings.Contains(strings.ToLower(res.Content), "propose_order") {
		t.Errorf("raw pseudo call leaked to the customer: %q", res.Content)
	}
	if res.Content == "" {
		t.Error("expected a human-readable fallback message")
	}
}

func TestComplete_StructuredToolCallStillWins(t *testing.T) {
	srv := fakeEndpoint(t, map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{map[string]any{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "propose_order", "arguments": `{"items":[],"delivery_address":"x"}`},
		}},
	})
	defer srv.Close()

	res := complete(t, srv, orderTool)
	if res.ToolCall == nil || res.ToolCall.Arguments != `{"items":[],"delivery_address":"x"}` {
		t.Fatalf("structured call must pass through untouched, got %+v", res)
	}
}

func TestComplete_PlainAnswerUntouchedWhenNoTools(t *testing.T) {
	srv := fakeEndpoint(t, map[string]any{"role": "assistant", "content": `propose_order("a", [])`})
	defer srv.Close()

	// No tool is offered for this request, so the text is not a tool call.
	res := complete(t, srv, nil)
	if res.ToolCall != nil || res.Content != `propose_order("a", [])` {
		t.Fatalf("got %+v", res)
	}
}

// Regression: asked for its system prompt, a local model generated until the
// proxy gave up (504 after 60 s). Every request is capped.
func TestComplete_CapsTheLengthOfTheAnswer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	defer srv.Close()

	complete(t, srv, nil)
	max, ok := got["max_tokens"].(float64)
	if !ok || max <= 0 || max > 1000 {
		t.Errorf("max_tokens = %v, want a sensible cap", got["max_tokens"])
	}
}

// Security: a redirect must not carry the API key to another host.
func TestClients_DoNotFollowRedirects(t *testing.T) {
	var hit bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	for _, provider := range []string{"openai-compatible", "anthropic"} {
		p, _, err := NewProvider(Settings{Provider: provider, BaseURL: redirector.URL, APIKey: "k", Model: "m"})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "hi"}}, nil)
		if hit {
			t.Fatalf("%s followed a redirect", provider)
		}
	}
}
