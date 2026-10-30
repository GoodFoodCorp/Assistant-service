package llmclient

import (
	"context"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

// FakeProvider stands in for a real AI endpoint when none is configured
// (no provider set, by the environment or the admin), so the chat widget stays demoable offline. It gives a
// canned but context-aware-looking reply based on simple keyword matching —
// no real language understanding, purely a demo stand-in.
type FakeProvider struct{}

func NewFakeProvider() *FakeProvider { return &FakeProvider{} }

// Complete never emits a domain.ToolCall — placing an order via chat needs a
// real model that actually supports function calling.
func (f *FakeProvider) Complete(_ context.Context, messages []domain.Message, _ []domain.ToolDefinition) (domain.CompletionResult, error) {
	var lastUser string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			lastUser = strings.ToLower(messages[i].Content)
			break
		}
	}

	// Customers read these: no configuration talk, the admin sets the AI up
	// from the back-office.
	var content string
	switch {
	case strings.Contains(lastUser, "veux") || strings.Contains(lastUser, "commande-moi") || strings.Contains(lastUser, "achète"):
		content = "La commande par le chat n'est pas encore disponible. Choisissez un restaurant, ajoutez vos plats au panier et validez : c'est rapide !"
	case strings.Contains(lastUser, "commande"):
		content = "Vous suivez vos commandes en temps réel sur la page « Mes commandes » : statut, livreur et détail de chaque commande."
	case strings.Contains(lastUser, "menu") || strings.Contains(lastUser, "plat"):
		content = "Ouvrez la page d'un restaurant pour voir sa carte complète, avec les prix et les plats disponibles en ce moment."
	case strings.Contains(lastUser, "promo") || strings.Contains(lastUser, "code"):
		content = "Les codes promo se saisissent dans le panier, dans le champ « Code promo »."
	default:
		content = "Bonjour ! Je suis l'assistant Good Food. Je peux vous orienter vers vos commandes, les menus des restaurants ou les codes promo. Que puis-je faire pour vous ?"
	}
	return domain.CompletionResult{Content: content}, nil
}
