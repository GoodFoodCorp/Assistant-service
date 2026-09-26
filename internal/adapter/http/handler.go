package http

import (
	"encoding/json"
	"net/http"

	"goodfood/assistant-service/internal/application"
	"goodfood/assistant-service/internal/domain"
)

type ChatHandler struct {
	uc *application.UseCases
}

func NewChatHandler(uc *application.UseCases) *ChatHandler {
	return &ChatHandler{uc: uc}
}

// POST /api/chat/messages
func (h *ChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	reply, err := h.uc.SendMessage(r.Context(), actorFrom(r), application.SendMessageInput{
		Messages:     req.toMessages(),
		RestaurantID: req.RestaurantID,
	})
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sendMessageResponse{Role: domain.RoleAssistant, Content: reply})
}
