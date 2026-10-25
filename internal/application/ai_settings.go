package application

import (
	"context"
	"net/url"
	"sync"
	"time"

	"goodfood/assistant-service/internal/domain"
)

// AISettings is the language-model backend an admin can choose.
type AISettings struct {
	Provider       string
	BaseURL        string
	APIKey         string
	Model          string
	TimeoutSeconds int
}

// AISettingsView is what an admin may see: never the key itself.
type AISettingsView struct {
	Provider       string
	BaseURL        string
	Model          string
	TimeoutSeconds int
	HasKey         bool
	KeyHint        string // last 4 characters
	Source         string // "admin" (saved from the UI) or "env"
}

type AITestResult struct {
	OK        bool
	Message   string
	LatencyMS int64
}

// AISettingsStore persists the admin's choice across restarts.
type AISettingsStore interface {
	Load() (*AISettings, error) // nil when nothing was saved
	Save(AISettings) error
	Clear() error
}

// LLMSwitch is the live provider the chat talks through.
type LLMSwitch interface {
	domain.LLMProvider
	Swap(domain.LLMProvider)
}

type ProviderFactory func(AISettings) (domain.LLMProvider, error)

// AIConfig lets an admin pick the provider, model and API key at runtime. The
// environment stays the default; a saved choice overrides it.
type AIConfig struct {
	mu      sync.Mutex
	env     AISettings
	store   AISettingsStore
	sw      LLMSwitch
	factory ProviderFactory
	current AISettings
	source  string
}

func NewAIConfig(env AISettings, store AISettingsStore, sw LLMSwitch, factory ProviderFactory) *AIConfig {
	return &AIConfig{env: env, store: store, sw: sw, factory: factory, current: env, source: "env"}
}

// Init applies the saved choice if any, else the environment. An invalid saved
// choice falls back to the environment rather than breaking the chat.
func (c *AIConfig) Init() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if saved, err := c.store.Load(); err == nil && saved != nil {
		if p, err := c.factory(*saved); err == nil {
			c.sw.Swap(p)
			c.current, c.source = *saved, "admin"
			return nil
		}
	}
	return c.applyEnv()
}

func (c *AIConfig) applyEnv() error {
	p, err := c.factory(c.env)
	if err != nil {
		return err
	}
	c.sw.Swap(p)
	c.current, c.source = c.env, "env"
	return nil
}

func isAdmin(a Actor) bool {
	for _, r := range a.RoleSlugs {
		if r == "admin" {
			return true
		}
	}
	return false
}

func forbidden() error { return domain.NewForbiddenError("réservé aux administrateurs") }

func (c *AIConfig) view() AISettingsView {
	v := AISettingsView{
		Provider: c.current.Provider, BaseURL: c.current.BaseURL, Model: c.current.Model,
		TimeoutSeconds: c.current.TimeoutSeconds, Source: c.source, HasKey: c.current.APIKey != "",
	}
	if k := c.current.APIKey; len(k) >= 4 {
		v.KeyHint = k[len(k)-4:]
	}
	return v
}

func (c *AIConfig) View(a Actor) (AISettingsView, error) {
	if !isAdmin(a) {
		return AISettingsView{}, forbidden()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view(), nil
}

// Update validates by building the provider first: an invalid choice changes
// nothing. An empty key keeps the current one, but only for the same provider.
func (c *AIConfig) Update(a Actor, in AISettings) (AISettingsView, error) {
	if !isAdmin(a) {
		return AISettingsView{}, forbidden()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := validateBaseURL(in.BaseURL); err != nil {
		return AISettingsView{}, err
	}
	// The saved key only stays with the destination it was entered for: keeping
	// it for another URL would let it be sent wherever the form points.
	if in.APIKey == "" && in.Provider == c.current.Provider && in.BaseURL == c.current.BaseURL {
		in.APIKey = c.current.APIKey
	}
	p, err := c.factory(in)
	if err != nil {
		return AISettingsView{}, domain.NewValidationError(err.Error())
	}
	if err := c.store.Save(in); err != nil {
		return AISettingsView{}, domain.NewUpstreamError("enregistrement impossible : " + err.Error())
	}
	c.sw.Swap(p)
	c.current, c.source = in, "admin"
	return c.view(), nil
}

func (c *AIConfig) Reset(a Actor) (AISettingsView, error) {
	if !isAdmin(a) {
		return AISettingsView{}, forbidden()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.store.Clear(); err != nil {
		return AISettingsView{}, domain.NewUpstreamError(err.Error())
	}
	if err := c.applyEnv(); err != nil {
		return AISettingsView{}, domain.NewValidationError(err.Error())
	}
	return c.view(), nil
}

// Test sends a tiny prompt through the live provider.
func (c *AIConfig) Test(ctx context.Context, a Actor) (AITestResult, error) {
	if !isAdmin(a) {
		return AITestResult{}, forbidden()
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.sw.Complete(ctx, []domain.Message{{Role: domain.RoleUser, Content: "Réponds uniquement : ok"}}, nil)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if r := []rune(msg); len(r) > 200 {
			msg = string(r[:200]) + "…"
		}
		return AITestResult{Message: msg, LatencyMS: ms}, nil
	}
	return AITestResult{OK: true, Message: "Le modèle répond.", LatencyMS: ms}, nil
}

// validateBaseURL only allows http(s) endpoints. Private addresses stay
// allowed on purpose: local runtimes (Ollama, LM Studio) live there, and only
// an admin can set this.
func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return domain.NewValidationError("l'URL de l'API doit commencer par http:// ou https://")
	}
	return nil
}
