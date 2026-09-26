package application

import (
	"context"
	"fmt"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

const (
	maxHistoryMessages = 20
	maxMessageChars    = 4000

	baseSystemPrompt = `Tu es l'assistant Good Food, un service de livraison de repas.
Réponds en français, de façon brève, chaleureuse et utile.
Tu peux aider sur : le menu d'un restaurant, le statut d'une commande en cours, les codes promo, ou toute question générale sur le service.
Si tu ne sais pas répondre, dis-le simplement et propose de contacter le support.`
)

type SendMessageInput struct {
	Messages     []domain.Message
	RestaurantID string
}

// SendMessage answers the customer's latest message, enriched with their own
// recent orders and — if a restaurant is in view — its current menu. Context
// fetches are best-effort: a failure there degrades the answer, it never
// fails the chat itself.
func (uc *UseCases) SendMessage(ctx context.Context, actor Actor, in SendMessageInput) (string, error) {
	if len(in.Messages) == 0 {
		return "", domain.NewValidationError("messages must not be empty")
	}
	last := in.Messages[len(in.Messages)-1]
	if last.Role != domain.RoleUser {
		return "", domain.NewValidationError("the last message must be from the user")
	}
	if strings.TrimSpace(last.Content) == "" {
		return "", domain.NewValidationError("message content must not be empty")
	}
	if len(last.Content) > maxMessageChars {
		return "", domain.NewValidationError("message is too long")
	}

	history := in.Messages
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}

	system := domain.Message{Role: domain.RoleSystem, Content: uc.buildContext(ctx, actor, in.RestaurantID)}
	full := append([]domain.Message{system}, history...)

	reply, err := uc.llm.Complete(ctx, full)
	if err != nil {
		return "", domain.NewUpstreamError(err.Error())
	}
	return reply, nil
}

// buildContext is best-effort: an order-service/menu-service hiccup degrades
// the answer's context, it never fails the chat itself.
func (uc *UseCases) buildContext(ctx context.Context, actor Actor, restaurantID string) string {
	var b strings.Builder
	b.WriteString(baseSystemPrompt)

	if orders, err := uc.orders.RecentOrders(ctx, actor.Token); err == nil && len(orders) > 0 {
		b.WriteString("\n\nCommandes récentes du client :\n")
		for _, o := range orders {
			fmt.Fprintf(&b, "- #%s : %s, %s, total %.2f€\n",
				shortID(o.ID), o.Status, strings.Join(o.ItemNames, ", "), float64(o.TotalAmountCents)/100)
		}
	}

	if restaurantID != "" {
		if items, err := uc.menu.MenuForRestaurant(ctx, restaurantID); err == nil && len(items) > 0 {
			b.WriteString("\n\nMenu du restaurant consulté :\n")
			for _, m := range items {
				if !m.Available {
					continue
				}
				fmt.Fprintf(&b, "- %s (%s) : %.2f€ — %s\n", m.Name, m.Category, float64(m.PriceCents)/100, m.Description)
			}
		}
	}

	return b.String()
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
