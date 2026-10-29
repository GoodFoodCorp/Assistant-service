package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/adapter/llmclient"
	"goodfood/assistant-service/internal/application"
	"goodfood/assistant-service/internal/domain"
)

type memStore struct{ s *application.AISettings }

func (m *memStore) Load() (*application.AISettings, error) { return m.s, nil }
func (m *memStore) Save(s application.AISettings) error    { m.s = &s; return nil }
func (m *memStore) Clear() error                           { m.s = nil; return nil }

func adminRouter(t *testing.T) http.Handler {
	t.Helper()
	sw := llmclient.NewSwitchable(llmclient.NewFakeProvider())
	cfg := application.NewAIConfig(application.AISettings{Provider: "demo"}, &memStore{}, sw,
		func(s application.AISettings) (domain.LLMProvider, error) {
			p, _, err := llmclient.NewProvider(llmclient.Settings{Provider: s.Provider, BaseURL: s.BaseURL, APIKey: s.APIKey, Model: s.Model, Timeout: time.Second})
			return p, err
		})
	require.NoError(t, cfg.Init())
	return NewRouter(NewChatHandler(application.NewUseCases(sw, nil, nil, nil, nil, nil)), NewAdminHandler(cfg), JWTConfig{Secret: "secret", Issuer: "goodfood-auth", Audience: "goodfood-api"}, zerolog.Nop())
}

func call(t *testing.T, h http.Handler, method, path, role, body string) *httptest.ResponseRecorder {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u1", "role_slugs": []string{role}, "typ": "access", "iss": "goodfood-auth", "aud": "goodfood-api", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("secret"))
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminSettings_NonAdminsAreRefused(t *testing.T) {
	h := adminRouter(t)
	for _, role := range []string{"user", "manager", "livreur"} {
		assert.Equal(t, http.StatusForbidden, call(t, h, "GET", "/api/chat/admin/settings/", role, "").Code, role)
		assert.Equal(t, http.StatusForbidden, call(t, h, "PUT", "/api/chat/admin/settings/", role, `{"provider":"ollama"}`).Code, role)
	}
}

func TestAdminSettings_SaveNeverEchoesTheKey(t *testing.T) {
	h := adminRouter(t)
	rec := call(t, h, "PUT", "/api/chat/admin/settings/", "admin", `{"provider":"anthropic","api_key":"sk-ant-supersecret-9876","model":"claude-haiku-5-5"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "supersecret")

	rec = call(t, h, "GET", "/api/chat/admin/settings/", "admin", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "supersecret")
	var out struct {
		Settings  aiSettingsResponse
		Providers []llmclient.PresetInfo
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.True(t, out.Settings.HasKey)
	assert.Equal(t, "9876", out.Settings.KeyHint)
	assert.Equal(t, "admin", out.Settings.Source)
	assert.NotEmpty(t, out.Providers)
}

func TestAdminSettings_InvalidChoiceIsA400(t *testing.T) {
	h := adminRouter(t)
	rec := call(t, h, "PUT", "/api/chat/admin/settings/", "admin", `{"provider":"anthropic"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
