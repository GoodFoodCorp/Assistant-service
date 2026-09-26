package llmclient

import (
	"context"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

// FakeProvider stands in for a real AI endpoint when none is configured
// (AI_BASE_URL empty), so the chat widget stays demoable offline. It gives a
// canned but context-aware-looking reply based on simple keyword matching —
// no real language understanding, purely a demo stand-in.
type FakeProvider struct{}

func NewFakeProvider() *FakeProvider { return &FakeProvider{} }

func (f *FakeProvider) Complete(_ context.Context, messages []domain.Message) (string, error) {
	var lastUser string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			lastUser = strings.ToLower(messages[i].Content)
			break
		}
	}

	switch {
	case strings.Contains(lastUser, "commande"):
		return "Je n'ai pas d'accès à un vrai modèle d'IA pour l'instant (mode démo), mais je vois le contexte de vos commandes récentes ci-dessus si vous en avez. Pour un suivi précis, consultez la page « Mes commandes ».", nil
	case strings.Contains(lastUser, "menu") || strings.Contains(lastUser, "plat"):
		return "Je suis en mode démo (pas d'IA connectée), mais le menu du restaurant que vous consultez est listé dans mon contexte. Je vous invite aussi à parcourir la page du restaurant pour tous les détails.", nil
	case strings.Contains(lastUser, "promo") || strings.Contains(lastUser, "code"):
		return "Les codes promo sont saisissables directement dans le panier, dans le champ « Code promo ». Je suis en mode démo, donc je ne peux pas en générer un pour vous ici.", nil
	default:
		return "Bonjour ! Je suis l'assistant Good Food, actuellement en mode démo (aucune IA connectée — configurez AI_BASE_URL pour brancher un vrai modèle, local ou cloud). Comment puis-je vous aider ?", nil
	}
}
