package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

// scriptedLLM answers with each result in turn and records every call.
type scriptedLLM struct {
	results []domain.CompletionResult
	calls   [][]domain.Message
}

func (s *scriptedLLM) Complete(_ context.Context, messages []domain.Message, _ []domain.ToolDefinition) (domain.CompletionResult, error) {
	s.calls = append(s.calls, messages)
	i := len(s.calls) - 1
	if i >= len(s.results) {
		i = len(s.results) - 1
	}
	return s.results[i], nil
}

// Regression: a 7B model ignored a reminder appended to the system prompt and
// wrote the recap twice; the same reminder next to the customer's last message
// makes it call the tool (measured 6/6 vs 0/6).
func TestRecapRetryPutsReminderNextToTheCustomersMessage(t *testing.T) {
	uc, _, _ := multiRestaurantSetup()
	recap := domain.CompletionResult{Content: "Voici le récapitulatif :\n- 3× Burger Deluxe — 38.97€\nLivraison à : 12 rue de Paris\nTotal : 38.97€"}
	llm := &scriptedLLM{results: []domain.CompletionResult{recap, orderToolCall(t, "Good Food République")}}
	uc.llm = llm

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: []domain.Message{
		{Role: "user", Content: "un burger deluxe chez moi à République"},
		{Role: "assistant", Content: proposalHeader + "\nRestaurant : Good Food République\n- 1× Burger Deluxe — 12.99€\n\nLivraison à : 12 rue de Paris\nTotal : 12.99€\n\nJe confirme ?"},
		{Role: "user", Content: "finalement 3 burgers"},
	}})
	require.NoError(t, err)
	require.Len(t, llm.calls, 2)
	retry := llm.calls[1]
	assert.Contains(t, retry[len(retry)-1].Content, "finalement 3 burgers")
	assert.Contains(t, retry[len(retry)-1].Content, proposeOrderTool)
	assert.NotNil(t, out.Proposal, "the retry's tool call becomes a proposal")
	// the first call is untouched
	assert.NotContains(t, llm.calls[0][len(llm.calls[0])-1].Content, proposeOrderTool)
}

// Regression: with a saved address in the prompt, a 7B model refused an order
// delivered to another address.
func TestPromptAllowsAnotherDeliveryAddress(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: []domain.Message{
		{Role: "user", Content: "un burger deluxe livré au 5 rue de la Paix 75002 Paris"},
	}})
	require.NoError(t, err)
	assert.Contains(t, llm.lastMessages[0].Content, "autre adresse de livraison")
}
