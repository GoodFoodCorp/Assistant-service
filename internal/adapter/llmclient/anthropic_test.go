package llmclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodfood/assistant-service/internal/domain"
)

// anthropicServer records the request it received and replies with `reply`.
func anthropicServer(t *testing.T, status int, reply any, got *map[string]any, hdr *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		*hdr = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, got)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(reply)
	}))
}

func newAnthropic(srv *httptest.Server) *anthropicClient {
	return &anthropicClient{baseURL: srv.URL, apiKey: "sk-ant-secret", model: "claude-test", http: srv.Client()}
}

func TestAnthropic_RequestShapeAndTextReply(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := anthropicServer(t, 200, map[string]any{"content": []any{map[string]any{"type": "text", "text": "Bonjour !"}}}, &got, &hdr)
	defer srv.Close()

	res, err := newAnthropic(srv).Complete(context.Background(), []domain.Message{
		{Role: domain.RoleSystem, Content: "Tu es Good Food."},
		{Role: domain.RoleUser, Content: "salut"},
		{Role: domain.RoleUser, Content: "tu es là ?"}, // consecutive user turns must be merged
		{Role: domain.RoleAssistant, Content: "oui"},
		{Role: domain.RoleUser, Content: "parfait"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "Bonjour !" || res.ToolCall != nil {
		t.Fatalf("res = %+v", res)
	}
	if hdr.Get("x-api-key") != "sk-ant-secret" || hdr.Get("anthropic-version") == "" {
		t.Errorf("headers = %v", hdr)
	}
	if hdr.Get("Authorization") != "" {
		t.Error("Anthropic must not receive a Bearer header")
	}
	if got["system"] != "Tu es Good Food." || got["model"] != "claude-test" {
		t.Errorf("body = %v", got)
	}
	if got["max_tokens"] == nil {
		t.Error("max_tokens is mandatory for Anthropic")
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", msgs)
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "user" || !strings.Contains(first["content"].(string), "salut") || !strings.Contains(first["content"].(string), "tu es là ?") {
		t.Errorf("merged first turn = %v", first)
	}
	if _, hasTools := got["tools"]; hasTools {
		t.Error("no tools were offered")
	}
}

func TestAnthropic_ToolUseBecomesToolCall(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := anthropicServer(t, 200, map[string]any{"content": []any{
		map[string]any{"type": "text", "text": "Je prépare ça."},
		map[string]any{"type": "tool_use", "id": "tu_1", "name": "propose_order",
			"input": map[string]any{"delivery_address": "8 rue de la Paix", "items": []any{map[string]any{"name": "Burger Deluxe", "quantity": 2}}}},
	}}, &got, &hdr)
	defer srv.Close()

	res, err := newAnthropic(srv).Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "2 burgers chez moi"}}, orderTool)
	if err != nil {
		t.Fatal(err)
	}
	if res.ToolCall == nil || res.ToolCall.Name != "propose_order" {
		t.Fatalf("res = %+v", res)
	}
	a := decode(t, res.ToolCall)
	if a.DeliveryAddress != "8 rue de la Paix" || len(a.Items) != 1 || a.Items[0].Quantity != 2 {
		t.Errorf("args = %+v", a)
	}

	tools := got["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["name"] != "propose_order" || tool["input_schema"] == nil {
		t.Errorf("tool definition = %v", tool)
	}
}

func TestAnthropic_ErrorsAreSurfacedWithoutLeakingTheKey(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := anthropicServer(t, 401, map[string]any{"type": "error", "error": map[string]any{"type": "authentication_error", "message": "invalid x-api-key"}}, &got, &hdr)
	defer srv.Close()

	_, err := newAnthropic(srv).Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sk-ant-secret") {
		t.Error("the API key leaked into the error")
	}
}

func TestAnthropic_EmptyContentIsAnError(t *testing.T) {
	var got map[string]any
	var hdr http.Header
	srv := anthropicServer(t, 200, map[string]any{"content": []any{}}, &got, &hdr)
	defer srv.Close()

	if _, err := newAnthropic(srv).Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: "x"}}, nil); err == nil {
		t.Fatal("expected an error")
	}
}
