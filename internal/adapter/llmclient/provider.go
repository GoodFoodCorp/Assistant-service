package llmclient

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"goodfood/assistant-service/internal/domain"
)

// Settings is everything needed to pick and configure a language-model
// backend. Only Provider is needed for a known provider; BaseURL and Model
// override that provider's defaults (e.g. a remote Ollama, another model).
type Settings struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

// Description says what was selected, for the startup log. It never contains
// the API key.
type Description struct {
	Provider string
	BaseURL  string
	Model    string
}

const defaultTimeout = 60 * time.Second

type preset struct {
	baseURL  string
	model    string
	needsKey bool
	native   string // non-empty: a dedicated adapter instead of the OpenAI-compatible client
}

// Every provider except Anthropic speaks the OpenAI "chat completions"
// dialect (including function calling), so one client covers them all; Gemini
// is reached through Google's OpenAI-compatibility endpoint.
var presets = map[string]preset{
	"ollama":     {baseURL: "http://host.docker.internal:11434/v1", model: "qwen2.5:7b"},
	"lmstudio":   {baseURL: "http://host.docker.internal:1234/v1", model: "local-model"},
	"openai":     {baseURL: "https://api.openai.com/v1", model: "gpt-4o-mini", needsKey: true},
	"openrouter": {baseURL: "https://openrouter.ai/api/v1", model: "openai/gpt-4o-mini", needsKey: true},
	"groq":       {baseURL: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile", needsKey: true},
	"mistral":    {baseURL: "https://api.mistral.ai/v1", model: "mistral-small-latest", needsKey: true},
	"deepseek":   {baseURL: "https://api.deepseek.com/v1", model: "deepseek-chat", needsKey: true},
	"together":   {baseURL: "https://api.together.xyz/v1", model: "meta-llama/Llama-3.3-70B-Instruct-Turbo", needsKey: true},
	"gemini":     {baseURL: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-2.0-flash", needsKey: true},
	"anthropic":  {baseURL: "https://api.anthropic.com", model: "claude-haiku-5-5", needsKey: true, native: "anthropic"},
}

// NewProvider builds the configured backend.
//
//   - Provider empty and BaseURL empty  -> offline demo provider.
//   - Provider empty and BaseURL set    -> generic OpenAI-compatible client
//     (the original configuration, unchanged).
//   - Provider set                      -> that provider's preset, with
//     BaseURL / Model overriding its defaults.
//
// A misconfiguration (unknown provider, missing key) is an error rather than a
// silent fallback to the demo provider, so it is noticed at startup.
func NewProvider(s Settings) (domain.LLMProvider, Description, error) {
	name := strings.ToLower(strings.TrimSpace(s.Provider))
	baseURL := strings.TrimSpace(s.BaseURL)
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	apiKey := strings.TrimSpace(s.APIKey)

	if name == "demo" || (name == "" && baseURL == "") {
		return NewFakeProvider(), Description{Provider: "demo"}, nil
	}

	if name == "" || name == "openai-compatible" {
		model := strings.TrimSpace(s.Model)
		if model == "" {
			model = "gpt-4o-mini"
		}
		if baseURL == "" {
			return nil, Description{}, fmt.Errorf("AI_BASE_URL is required for AI_PROVIDER=openai-compatible")
		}
		c := newClient(baseURL, apiKey, model, timeout)
		return c, Description{Provider: "openai-compatible", BaseURL: c.baseURL, Model: model}, nil
	}

	p, ok := presets[name]
	if !ok {
		return nil, Description{}, fmt.Errorf("unknown AI_PROVIDER %q (supported: %s)", s.Provider, supportedProviders())
	}
	if p.needsKey && apiKey == "" {
		return nil, Description{}, fmt.Errorf("AI_API_KEY is required for AI_PROVIDER=%s", name)
	}
	if baseURL == "" {
		baseURL = p.baseURL
	}
	model := strings.TrimSpace(s.Model)
	if model == "" {
		model = p.model
	}

	if p.native == "anthropic" {
		a := &anthropicClient{
			baseURL: strings.TrimSuffix(baseURL, "/"), apiKey: apiKey, model: model,
			http: &http.Client{Timeout: timeout, CheckRedirect: noRedirect},
		}
		return a, Description{Provider: name, BaseURL: a.baseURL, Model: model}, nil
	}
	c := newClient(baseURL, apiKey, model, timeout)
	return c, Description{Provider: name, BaseURL: c.baseURL, Model: model}, nil
}

func supportedProviders() string {
	names := make([]string, 0, len(presets)+2)
	for n := range presets {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(append(names, "openai-compatible", "demo"), ", ")
}
