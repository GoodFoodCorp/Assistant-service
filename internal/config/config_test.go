package config

import (
	"testing"
	"time"
)

func TestLoad_AIDefaultsKeepTheDemoProvider(t *testing.T) {
	t.Setenv("JWT_SECRET", "s")
	for _, k := range []string{"AI_PROVIDER", "AI_BASE_URL", "AI_API_KEY", "AI_MODEL", "AI_TIMEOUT_SECONDS"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "" || cfg.AIBaseURL != "" || cfg.AIModel != "" || cfg.AITimeout != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoad_ReadsAIProviderSettings(t *testing.T) {
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("AI_PROVIDER", "anthropic")
	t.Setenv("AI_API_KEY", "sk-ant-x")
	t.Setenv("AI_MODEL", "claude-haiku-5-5")
	t.Setenv("AI_TIMEOUT_SECONDS", "120")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "anthropic" || cfg.AIAPIKey != "sk-ant-x" || cfg.AIModel != "claude-haiku-5-5" || cfg.AITimeout != 120*time.Second {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoad_RejectsBadTimeout(t *testing.T) {
	t.Setenv("JWT_SECRET", "s")
	for _, v := range []string{"abc", "0", "-5"} {
		t.Setenv("AI_TIMEOUT_SECONDS", v)
		if _, err := Load(); err == nil {
			t.Errorf("AI_TIMEOUT_SECONDS=%q should be rejected", v)
		}
	}
}
