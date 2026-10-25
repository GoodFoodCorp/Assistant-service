package llmclient

import (
	"encoding/json"
	"testing"

	"goodfood/assistant-service/internal/domain"
)

var orderTool = []domain.ToolDefinition{{
	Name: "propose_order",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items":            map[string]any{"type": "array"},
			"delivery_address": map[string]any{"type": "string"},
		},
		"required": []string{"items", "delivery_address"},
	},
}}

type orderArgs struct {
	Items []struct {
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
	} `json:"items"`
	DeliveryAddress string `json:"delivery_address"`
}

func decode(t *testing.T, tc *domain.ToolCall) orderArgs {
	t.Helper()
	var a orderArgs
	if err := json.Unmarshal([]byte(tc.Arguments), &a); err != nil {
		t.Fatalf("recovered arguments are not valid JSON: %v (%s)", err, tc.Arguments)
	}
	return a
}

// The three shapes observed from qwen2.5:7b on Ollama when it writes the call
// as plain text instead of emitting a structured tool_calls entry.
func TestRecoverTextToolCall_ObservedShapes(t *testing.T) {
	const addr = "37 Rue Mazagran, 76320, CAUDEBEC-LÈS-ELBEUF"
	cases := map[string]string{
		"named args":      `Propose_order("items": [{"name": "Burger Deluxe", "quantity": 1}], "delivery_address": "` + addr + `")`,
		"json object":     `Propose_order({"delivery_address": "` + addr + `", "items": [{"name": "Burger Deluxe", "quantity": 1}]})`,
		"positional":      `Propose_order("` + addr + `", [{"name": "Burger Deluxe", "quantity": 1}])`,
		"trailing prose":  `propose_order("items": [{"name": "Burger Deluxe", "quantity": 1}], "delivery_address": "` + addr + `") voilà`,
		"in a code fence": "```\npropose_order({\"items\": [{\"name\": \"Burger Deluxe\", \"quantity\": 1}], \"delivery_address\": \"" + addr + "\"})\n```",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			tc := recoverTextToolCall(content, orderTool)
			if tc == nil {
				t.Fatalf("expected a recovered tool call from %q", content)
			}
			if tc.Name != "propose_order" {
				t.Fatalf("name = %q", tc.Name)
			}
			a := decode(t, tc)
			if a.DeliveryAddress != addr {
				t.Errorf("address = %q", a.DeliveryAddress)
			}
			if len(a.Items) != 1 || a.Items[0].Name != "Burger Deluxe" || a.Items[0].Quantity != 1 {
				t.Errorf("items = %+v", a.Items)
			}
		})
	}
}

func TestRecoverTextToolCall_IgnoresOrdinaryText(t *testing.T) {
	for _, content := range []string{
		"Bonjour ! Comment puis-je vous aider ?",
		"Je peux préparer une commande avec propose_order si tu me donnes le plat.",
		"",
	} {
		if tc := recoverTextToolCall(content, orderTool); tc != nil {
			t.Errorf("unexpected tool call from %q: %+v", content, tc)
		}
	}
}

func TestRecoverTextToolCall_OnlyOfferedTools(t *testing.T) {
	if tc := recoverTextToolCall(`delete_everything({"a":1})`, orderTool); tc != nil {
		t.Errorf("must never recover a tool that was not offered: %+v", tc)
	}
	if tc := recoverTextToolCall(`propose_order({"items":[]})`, nil); tc != nil {
		t.Errorf("no tools offered, nothing to recover: %+v", tc)
	}
}

func TestLooksLikeToolCall(t *testing.T) {
	if !looksLikeToolCall(`Propose_order(garbage that cannot be parsed`, orderTool) {
		t.Error("an unparseable pseudo call must still be flagged so it is never shown raw")
	}
	if looksLikeToolCall("Je peux commander pour toi.", orderTool) {
		t.Error("ordinary text flagged")
	}
}

// Llama-family models emit their native call format as the message content:
// {"name": "...", "parameters": {...}}.
func TestRecoverTextToolCall_LlamaNativeJSON(t *testing.T) {
	content := `{"name": "propose_order", "parameters": {"items": [{"name": "Burger Deluxe", "quantity": 2}], "delivery_address": "8 rue de la Paix"}}`
	tc := recoverTextToolCall(content, orderTool)
	if tc == nil || tc.Name != "propose_order" {
		t.Fatalf("got %+v", tc)
	}
	a := decode(t, tc)
	if a.DeliveryAddress != "8 rue de la Paix" || len(a.Items) != 1 || a.Items[0].Quantity != 2 {
		t.Errorf("args = %+v", a)
	}
	// "arguments" is the other spelling seen in the wild.
	if tc := recoverTextToolCall(`{"name":"propose_order","arguments":{"items":[],"delivery_address":"x"}}`, orderTool); tc == nil {
		t.Error("the arguments spelling should be recovered too")
	}
}

func TestLooksLikeToolCall_JSONForAnUnofferedTool(t *testing.T) {
	// Observed: a refusal wrapped in a made-up "support" call. Never show it raw.
	content := `{"name": "support", "parameters": {"text": "Je ne peux pas répondre."}}`
	if !looksLikeToolCall(content, orderTool) {
		t.Error("a JSON call shape must be flagged even for a tool that was not offered")
	}
	if recoverTextToolCall(content, orderTool) != nil {
		t.Error("but a tool that was not offered must never be recovered")
	}
	if looksLikeToolCall(`{"merci": "beaucoup"}`, orderTool) || looksLikeToolCall(`{"name": "Burger"}`, orderTool) {
		t.Error("ordinary JSON-looking text flagged")
	}
	if looksLikeToolCall(content, nil) {
		t.Error("nothing to protect when no tool is offered")
	}
}

// Observed with qwen2.5:7b after a click on a restaurant button: the model
// narrates the order and pastes the call's arguments as JSON inside the prose.
func TestRecoverTextToolCall_ArgumentsObjectEmbeddedInProse(t *testing.T) {
	content := "Parfait ! Un Burger Deluxe sera livré à 8 rue de la Paix. Voici votre commande :\n\n{\n  \"restaurant\": \"Good Food Montparnasse\",\n  \"items\": [\n    {\"name\": \"Burger Deluxe\", \"quantity\": 1}\n  ],\n  \"delivery_address\": \"8 rue de la Paix\"\n}\n\nVoulez-vous confirmer cette commande ?"
	tools := []domain.ToolDefinition{{
		Name: "propose_order",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"restaurant":       map[string]any{"type": "string"},
				"items":            map[string]any{"type": "array"},
				"delivery_address": map[string]any{"type": "string"},
			},
			"required": []string{"restaurant", "items", "delivery_address"},
		},
	}}

	tc := recoverTextToolCall(content, tools)
	if tc == nil || tc.Name != "propose_order" {
		t.Fatalf("got %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil || args["restaurant"] != "Good Food Montparnasse" {
		t.Errorf("args = %s (%v)", tc.Arguments, err)
	}
	if !looksLikeToolCall(content, tools) {
		t.Error("must also be flagged, so it is never shown raw if it cannot be used")
	}
}

func TestRecoverTextToolCall_NativeCallAfterJunkText(t *testing.T) {
	content := "Ronaldolement\n{\"name\": \"propose_order\", \"arguments\": {\"items\": [{\"name\": \"Burger Deluxe\", \"quantity\": 1}], \"delivery_address\": \"8 rue de la Paix\"}}"
	tc := recoverTextToolCall(content, orderTool)
	if tc == nil || tc.Name != "propose_order" {
		t.Fatalf("got %+v", tc)
	}
	if a := decode(t, tc); a.DeliveryAddress != "8 rue de la Paix" || len(a.Items) != 1 {
		t.Errorf("args = %+v", a)
	}
}

func TestRecoverTextToolCall_EmbeddedJSONThatIsNotACall(t *testing.T) {
	// Missing a required argument: not a call, so ordinary text is left alone.
	content := `Voici le détail : {"items": [{"name": "Burger", "quantity": 1}]} bonne journée`
	if tc := recoverTextToolCall(content, orderTool); tc != nil {
		t.Errorf("incomplete arguments must not become a call: %+v", tc)
	}
	if looksLikeToolCall(content, orderTool) {
		t.Error("an unrelated JSON snippet is not a tool call")
	}
	if recoverTextToolCall(`{"horaires": {"ouverture": "11h"}} {"delivery_address": "x"}`, orderTool) != nil {
		t.Error("only a complete argument set counts")
	}
}
