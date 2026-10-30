package llmclient

import (
	"strings"
	"testing"
	"time"
)

func TestNewProvider_BackwardCompatibleDefaults(t *testing.T) {
	// Nothing configured -> offline demo provider.
	p, d, err := NewProvider(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*FakeProvider); !ok || d.Provider != "demo" {
		t.Fatalf("got %T / %+v", p, d)
	}

	// Only a base URL (the pre-existing configuration) -> generic OpenAI-compatible client.
	p, d, err = NewProvider(Settings{BaseURL: "http://host.docker.internal:11434/v1/", Model: "qwen2.5:7b"})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := p.(*Client)
	if !ok {
		t.Fatalf("got %T", p)
	}
	if c.baseURL != "http://host.docker.internal:11434/v1" || c.model != "qwen2.5:7b" || d.Provider != "openai-compatible" {
		t.Errorf("client = %+v / %+v", c, d)
	}
}

func TestNewProvider_Presets(t *testing.T) {
	cases := []struct {
		provider, key, wantBase, wantModel string
	}{
		{"ollama", "", "http://host.docker.internal:11434/v1", "qwen2.5:7b"},
		{"lmstudio", "", "http://host.docker.internal:1234/v1", "local-model"},
		{"openai", "sk-x", "https://api.openai.com/v1", "gpt-4o-mini"},
		{"openrouter", "k", "https://openrouter.ai/api/v1", "openai/gpt-4o-mini"},
		{"groq", "k", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile"},
		{"mistral", "k", "https://api.mistral.ai/v1", "mistral-small-latest"},
		{"deepseek", "k", "https://api.deepseek.com/v1", "deepseek-chat"},
		{"together", "k", "https://api.together.xyz/v1", "meta-llama/Llama-3.3-70B-Instruct-Turbo"},
		{"gemini", "k", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.0-flash"},
		{"  OpenAI ", "sk-x", "https://api.openai.com/v1", "gpt-4o-mini"}, // trimmed, case-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			p, _, err := NewProvider(Settings{Provider: tc.provider, APIKey: tc.key})
			if err != nil {
				t.Fatal(err)
			}
			c, ok := p.(*Client)
			if !ok {
				t.Fatalf("got %T", p)
			}
			if c.baseURL != tc.wantBase || c.model != tc.wantModel {
				t.Errorf("base=%q model=%q, want %q / %q", c.baseURL, c.model, tc.wantBase, tc.wantModel)
			}
		})
	}
}

func TestNewProvider_OverridesBeatPresets(t *testing.T) {
	p, _, err := NewProvider(Settings{Provider: "ollama", BaseURL: "http://ollama:11434/v1", Model: "llama3.2", Timeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*Client)
	if c.baseURL != "http://ollama:11434/v1" || c.model != "llama3.2" || c.http.Timeout != 90*time.Second {
		t.Errorf("client = %+v timeout=%v", c, c.http.Timeout)
	}
}

func TestNewProvider_Anthropic(t *testing.T) {
	p, d, err := NewProvider(Settings{Provider: "anthropic", APIKey: "sk-ant-x"})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := p.(*anthropicClient)
	if !ok {
		t.Fatalf("got %T", p)
	}
	if a.baseURL != "https://api.anthropic.com" || a.model != "claude-haiku-5-5" || d.Provider != "anthropic" {
		t.Errorf("client = %+v / %+v", a, d)
	}
}

func TestNewProvider_RejectsBadConfiguration(t *testing.T) {
	for name, s := range map[string]Settings{
		"unknown provider":      {Provider: "skynet"},
		"openai without key":    {Provider: "openai"},
		"anthropic without key": {Provider: "anthropic"},
		"openrouter blank key":  {Provider: "openrouter", APIKey: "   "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NewProvider(s); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	_, _, err := NewProvider(Settings{Provider: "skynet"})
	if !strings.Contains(err.Error(), "ollama") || !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("the error should list the supported providers: %v", err)
	}
}

func TestNewProvider_DemoAlias(t *testing.T) {
	p, d, err := NewProvider(Settings{Provider: "demo", BaseURL: "http://ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*FakeProvider); !ok || d.Provider != "demo" {
		t.Fatalf("got %T / %+v", p, d)
	}
}

func TestNewProvider_Azure(t *testing.T) {
	p, desc, err := NewProvider(Settings{Provider: "azure", BaseURL: "https://cesi.services.ai.azure.com/api/projects/hack", APIKey: "k", Model: "gpt-5-mini"})
	if err != nil {
		t.Fatal(err)
	}
	c := p.(*Client)
	if desc.BaseURL != "https://cesi.services.ai.azure.com/openai/v1" || c.model != "gpt-5-mini" || !c.completionTokens.Load() {
		t.Errorf("desc=%+v model=%q", desc, c.model)
	}
	for _, s := range []Settings{
		{Provider: "azure", APIKey: "k", Model: "m"},
		{Provider: "azure", APIKey: "k", BaseURL: "https://cesi.openai.azure.com"},
	} {
		if _, _, err := NewProvider(s); err == nil {
			t.Errorf("%+v: want an error (address and deployment are required)", s)
		}
	}
}
