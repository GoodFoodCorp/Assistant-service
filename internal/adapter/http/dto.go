package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"goodfood/assistant-service/internal/domain"
)

type chatMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sendMessageRequest struct {
	Messages     []chatMessageRequest `json:"messages"`
	RestaurantID string               `json:"restaurant_id"`
}

func (r sendMessageRequest) toMessages() []domain.Message {
	out := make([]domain.Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, domain.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

type sendMessageResponse struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	reqID, _ := r.Context().Value(ctxKeyRequestID).(string)
	writeJSON(w, status, errorResponse{Error: msg, RequestID: reqID})
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var derr *domain.Error
	if errors.As(err, &derr) {
		status := map[domain.ErrorCode]int{
			domain.ErrCodeValidation: http.StatusBadRequest,
			domain.ErrCodeForbidden:  http.StatusForbidden,
			domain.ErrCodeUpstream:   http.StatusBadGateway,
		}[derr.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, r, status, derr.Message)
		return
	}
	writeError(w, r, http.StatusInternalServerError, "internal server error")
}
