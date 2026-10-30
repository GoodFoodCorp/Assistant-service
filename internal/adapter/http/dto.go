package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"goodfood/assistant-service/internal/domain"
)

type chatMessageRequest struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Signature string `json:"signature,omitempty"`
}

type sendMessageRequest struct {
	Messages     []chatMessageRequest `json:"messages"`
	RestaurantID string               `json:"restaurant_id"`
}

func (r sendMessageRequest) toMessages() []domain.Message {
	out := make([]domain.Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		out = append(out, domain.Message{Role: m.Role, Content: m.Content, Signature: m.Signature})
	}
	return out
}

type proposedItemResponse struct {
	MenuItemID     string `json:"menu_item_id"`
	MenuItemName   string `json:"menu_item_name"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type orderProposalResponse struct {
	RestaurantID     string                 `json:"restaurant_id"`
	RestaurantName   string                 `json:"restaurant_name,omitempty"`
	Items            []proposedItemResponse `json:"items"`
	DeliveryAddress  string                 `json:"delivery_address"`
	TotalAmountCents int64                  `json:"total_amount_cents"`
	PaymentMethod    string                 `json:"payment_method,omitempty"`
	DeliveryFeeCents int64                  `json:"delivery_fee_cents"`
	EstimatedMinutes int                    `json:"estimated_minutes,omitempty"`
	NeedsCard        bool                   `json:"needs_card,omitempty"`
}

func toProposalResponse(p *domain.OrderProposal) *orderProposalResponse {
	if p == nil {
		return nil
	}
	items := make([]proposedItemResponse, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, proposedItemResponse{
			MenuItemID: it.MenuItemID, MenuItemName: it.MenuItemName,
			Quantity: it.Quantity, UnitPriceCents: it.UnitPriceCents,
		})
	}
	return &orderProposalResponse{
		RestaurantID: p.RestaurantID, RestaurantName: p.RestaurantName, Items: items, DeliveryAddress: p.DeliveryAddress,
		TotalAmountCents: p.TotalAmountCents, PaymentMethod: p.PaymentMethod,
		DeliveryFeeCents: p.DeliveryFeeCents, EstimatedMinutes: p.EstimatedMinutes, NeedsCard: p.NeedsCard,
	}
}

type restaurantChoiceResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	City string `json:"city,omitempty"`
}

type sendMessageResponse struct {
	Role      string                     `json:"role"`
	Content   string                     `json:"content"`
	Proposal  *orderProposalResponse     `json:"proposal,omitempty"`
	Choices   []restaurantChoiceResponse `json:"choices,omitempty"`
	Dishes    []dishCardResponse         `json:"dishes,omitempty"`
	Signature string                     `json:"signature,omitempty"`
}

type dishCardResponse struct {
	ID             string `json:"id"`
	RestaurantID   string `json:"restaurant_id"`
	RestaurantName string `json:"restaurant_name,omitempty"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Category       string `json:"category,omitempty"`
	PriceCents     int64  `json:"price_cents"`
}

func toDishesResponse(list []domain.DishCard) []dishCardResponse {
	if len(list) == 0 {
		return nil
	}
	out := make([]dishCardResponse, 0, len(list))
	for _, d := range list {
		out = append(out, dishCardResponse{
			ID: d.ID, RestaurantID: d.RestaurantID, RestaurantName: d.RestaurantName, Name: d.Name,
			Description: d.Description, Category: d.Category, PriceCents: d.PriceCents,
		})
	}
	return out
}

func toChoicesResponse(list []domain.RestaurantSummary) []restaurantChoiceResponse {
	if len(list) == 0 {
		return nil
	}
	out := make([]restaurantChoiceResponse, 0, len(list))
	for _, r := range list {
		out = append(out, restaurantChoiceResponse{ID: r.ID, Name: r.Name, City: r.City})
	}
	return out
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
