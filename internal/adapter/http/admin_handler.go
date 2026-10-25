package http

import (
	"encoding/json"
	"net/http"

	"goodfood/assistant-service/internal/adapter/llmclient"
	"goodfood/assistant-service/internal/application"
)

// AdminHandler lets an administrator choose the AI provider, model and key.
type AdminHandler struct{ cfg *application.AIConfig }

func NewAdminHandler(cfg *application.AIConfig) *AdminHandler { return &AdminHandler{cfg: cfg} }

type aiSettingsResponse struct {
	Provider       string `json:"provider"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HasKey         bool   `json:"has_key"`
	KeyHint        string `json:"key_hint"`
	Source         string `json:"source"`
}

type aiSettingsRequest struct {
	Provider       string `json:"provider"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func toSettingsResponse(v application.AISettingsView) aiSettingsResponse {
	return aiSettingsResponse{v.Provider, v.BaseURL, v.Model, v.TimeoutSeconds, v.HasKey, v.KeyHint, v.Source}
}

// GET /api/chat/admin/settings
func (h *AdminHandler) Get(w http.ResponseWriter, r *http.Request) {
	v, err := h.cfg.View(actorFrom(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": toSettingsResponse(v), "providers": llmclient.Presets()})
}

// PUT /api/chat/admin/settings
func (h *AdminHandler) Put(w http.ResponseWriter, r *http.Request) {
	var req aiSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	v, err := h.cfg.Update(actorFrom(r), application.AISettings{
		Provider: req.Provider, BaseURL: req.BaseURL, APIKey: req.APIKey, Model: req.Model, TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsResponse(v))
}

// DELETE /api/chat/admin/settings — back to the environment configuration.
func (h *AdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	v, err := h.cfg.Reset(actorFrom(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettingsResponse(v))
}

// POST /api/chat/admin/settings/test
func (h *AdminHandler) Test(w http.ResponseWriter, r *http.Request) {
	res, err := h.cfg.Test(r.Context(), actorFrom(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "message": res.Message, "latency_ms": res.LatencyMS})
}
