package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

type memStore struct {
	saved *AISettings
	fail  error
}

func (m *memStore) Load() (*AISettings, error) { return m.saved, nil }
func (m *memStore) Save(s AISettings) error {
	if m.fail != nil {
		return m.fail
	}
	m.saved = &s
	return nil
}
func (m *memStore) Clear() error { m.saved = nil; return nil }

type namedLLM struct{ name string }

func (n *namedLLM) Complete(context.Context, []domain.Message, []domain.ToolDefinition) (domain.CompletionResult, error) {
	return domain.CompletionResult{Content: n.name}, nil
}

type switcher struct{ current domain.LLMProvider }

func (s *switcher) Swap(p domain.LLMProvider) { s.current = p }
func (s *switcher) Complete(ctx context.Context, m []domain.Message, t []domain.ToolDefinition) (domain.CompletionResult, error) {
	return s.current.Complete(ctx, m, t)
}

var admin = Actor{UserID: "a1", RoleSlugs: []string{"admin"}, Token: "t"}

func newAIConfig(env AISettings) (*AIConfig, *memStore, *switcher) {
	store, sw := &memStore{}, &switcher{}
	factory := func(s AISettings) (domain.LLMProvider, error) {
		if s.Provider == "bad" {
			return nil, errors.New("unknown provider")
		}
		if s.Provider == "anthropic" && s.APIKey == "" {
			return nil, errors.New("api key required")
		}
		return &namedLLM{s.Provider + "/" + s.Model + "/" + s.APIKey}, nil
	}
	return NewAIConfig(env, store, sw, factory), store, sw
}

func TestAIConfig_OnlyAdminsCanReadOrChangeIt(t *testing.T) {
	c, _, _ := newAIConfig(AISettings{})
	for _, a := range []Actor{customer, {UserID: "m", RoleSlugs: []string{"manager"}}} {
		_, err := c.View(a)
		var de *domain.Error
		require.ErrorAs(t, err, &de)
		assert.Equal(t, domain.ErrCodeForbidden, de.Code)
		_, err = c.Update(a, AISettings{Provider: "ollama"})
		require.ErrorAs(t, err, &de)
		assert.Equal(t, domain.ErrCodeForbidden, de.Code)
	}
}

func TestAIConfig_StartsFromEnvAndNeverExposesTheKey(t *testing.T) {
	c, _, sw := newAIConfig(AISettings{Provider: "anthropic", APIKey: "sk-ant-secret-1234", Model: "m"})
	require.NoError(t, c.Init())
	require.NotNil(t, sw.current)

	v, err := c.View(admin)
	require.NoError(t, err)
	assert.Equal(t, "anthropic", v.Provider)
	assert.Equal(t, "env", v.Source)
	assert.True(t, v.HasKey)
	assert.Equal(t, "1234", v.KeyHint)
}

func TestAIConfig_UpdateSwitchesLiveAndPersists(t *testing.T) {
	c, store, sw := newAIConfig(AISettings{Provider: "ollama", Model: "qwen"})
	require.NoError(t, c.Init())

	v, err := c.Update(admin, AISettings{Provider: "anthropic", APIKey: "sk-ant-abcd", Model: "claude"})
	require.NoError(t, err)
	assert.Equal(t, "admin", v.Source)
	res, _ := sw.current.Complete(context.Background(), nil, nil)
	assert.Equal(t, "anthropic/claude/sk-ant-abcd", res.Content, "the new provider answers immediately")
	require.NotNil(t, store.saved)
	assert.Equal(t, "anthropic", store.saved.Provider)
}

func TestAIConfig_EmptyKeyKeepsTheCurrentOneForTheSameProvider(t *testing.T) {
	c, _, sw := newAIConfig(AISettings{})
	_, err := c.Update(admin, AISettings{Provider: "anthropic", APIKey: "sk-ant-keep", Model: "a"})
	require.NoError(t, err)

	_, err = c.Update(admin, AISettings{Provider: "anthropic", Model: "b"})
	require.NoError(t, err)
	res, _ := sw.current.Complete(context.Background(), nil, nil)
	assert.Equal(t, "anthropic/b/sk-ant-keep", res.Content)

	// another provider must not inherit a key meant for the first one
	_, err = c.Update(admin, AISettings{Provider: "bad"})
	require.Error(t, err)
}

func TestAIConfig_InvalidSettingsAreRejectedAndChangeNothing(t *testing.T) {
	c, store, sw := newAIConfig(AISettings{Provider: "ollama", Model: "qwen"})
	require.NoError(t, c.Init())
	before := sw.current

	_, err := c.Update(admin, AISettings{Provider: "bad"})
	var de *domain.Error
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrCodeValidation, de.Code)
	assert.Same(t, before, sw.current)
	assert.Nil(t, store.saved)

	_, err = c.Update(admin, AISettings{Provider: "anthropic"})
	require.ErrorAs(t, err, &de, "missing key")
}

func TestAIConfig_SavedSettingsWinOverEnvAtStartup(t *testing.T) {
	c, store, sw := newAIConfig(AISettings{Provider: "ollama", Model: "qwen"})
	store.saved = &AISettings{Provider: "anthropic", APIKey: "sk-ant-zzzz", Model: "claude"}
	require.NoError(t, c.Init())
	res, _ := sw.current.Complete(context.Background(), nil, nil)
	assert.Equal(t, "anthropic/claude/sk-ant-zzzz", res.Content)
	v, _ := c.View(admin)
	assert.Equal(t, "admin", v.Source)
}

func TestAIConfig_ResetGoesBackToEnv(t *testing.T) {
	c, store, sw := newAIConfig(AISettings{Provider: "ollama", Model: "qwen"})
	_, err := c.Update(admin, AISettings{Provider: "anthropic", APIKey: "sk-ant-abcd"})
	require.NoError(t, err)

	v, err := c.Reset(admin)
	require.NoError(t, err)
	assert.Equal(t, "env", v.Source)
	assert.Nil(t, store.saved)
	res, _ := sw.current.Complete(context.Background(), nil, nil)
	assert.Equal(t, "ollama/qwen/", res.Content)
}

func TestAIConfig_TestReportsSuccessAndFailure(t *testing.T) {
	c, _, sw := newAIConfig(AISettings{Provider: "ollama"})
	require.NoError(t, c.Init())
	r, err := c.Test(context.Background(), admin)
	require.NoError(t, err)
	assert.True(t, r.OK)

	sw.current = &failingLLM{}
	r, err = c.Test(context.Background(), admin)
	require.NoError(t, err)
	assert.False(t, r.OK)
	assert.Contains(t, r.Message, "boom")
}

type failingLLM struct{}

func (failingLLM) Complete(context.Context, []domain.Message, []domain.ToolDefinition) (domain.CompletionResult, error) {
	return domain.CompletionResult{}, errors.New("boom")
}
