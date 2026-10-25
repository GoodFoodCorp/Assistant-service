package llmclient

import (
	"context"
	"sort"
	"sync"

	"goodfood/assistant-service/internal/domain"
)

// Switchable is the provider the chat talks through: the admin can replace
// what is behind it while requests are in flight.
type Switchable struct {
	mu      sync.RWMutex
	current domain.LLMProvider
}

func NewSwitchable(p domain.LLMProvider) *Switchable { return &Switchable{current: p} }

func (s *Switchable) Swap(p domain.LLMProvider) {
	s.mu.Lock()
	s.current = p
	s.mu.Unlock()
}

func (s *Switchable) Complete(ctx context.Context, m []domain.Message, t []domain.ToolDefinition) (domain.CompletionResult, error) {
	s.mu.RLock()
	p := s.current
	s.mu.RUnlock()
	return p.Complete(ctx, m, t)
}

// PresetInfo describes a selectable provider, for the admin form.
type PresetInfo struct {
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	NeedsKey bool   `json:"needs_key"`
}

// Presets lists the known providers, plus the generic OpenAI-compatible one.
func Presets() []PresetInfo {
	out := make([]PresetInfo, 0, len(presets)+1)
	for n, p := range presets {
		out = append(out, PresetInfo{Name: n, BaseURL: p.baseURL, Model: p.model, NeedsKey: p.needsKey})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return append(out, PresetInfo{Name: "openai-compatible", Model: "", NeedsKey: false})
}
