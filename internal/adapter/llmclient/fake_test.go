package llmclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

func TestFakeProvider_RepliesWithoutConfigurationTalk(t *testing.T) {
	for _, ask := range []string{"Bonjour", "Je veux un burger", "Où est ma commande ?", "Le menu ?", "Un code promo ?"} {
		got, err := NewFakeProvider().Complete(context.Background(), []domain.Message{{Role: domain.RoleUser, Content: ask}}, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, got.Content, ask)
		assert.Nil(t, got.ToolCall, ask)
		for _, jargon := range []string{"AI_", "Ollama", "function call", "mode démo"} {
			assert.NotContains(t, got.Content, jargon, ask)
		}
	}
}
