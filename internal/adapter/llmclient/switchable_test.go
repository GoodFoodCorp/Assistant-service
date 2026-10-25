package llmclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"goodfood/assistant-service/internal/domain"
)

type stub string

func (s stub) Complete(context.Context, []domain.Message, []domain.ToolDefinition) (domain.CompletionResult, error) {
	return domain.CompletionResult{Content: string(s)}, nil
}

func TestSwitchable_SwapChangesTheAnswerer(t *testing.T) {
	s := NewSwitchable(stub("a"))
	r, _ := s.Complete(context.Background(), nil, nil)
	assert.Equal(t, "a", r.Content)
	s.Swap(stub("b"))
	r, _ = s.Complete(context.Background(), nil, nil)
	assert.Equal(t, "b", r.Content)
}

func TestPresets_ListsKnownProvidersAndTheGenericOne(t *testing.T) {
	names := map[string]PresetInfo{}
	for _, p := range Presets() {
		names[p.Name] = p
	}
	assert.True(t, names["anthropic"].NeedsKey)
	assert.False(t, names["ollama"].NeedsKey)
	assert.Contains(t, names, "openai-compatible")
}
