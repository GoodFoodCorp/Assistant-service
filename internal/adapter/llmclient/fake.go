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

	var content string
	switch {
	case strings.Contains(lastUser, "veux") || strings.Contains(lastUser, "commande-moi") || strings.Contains(lastUser, "achète"):
		content = "Je suis en mode démo (pas d'IA connectée), donc je ne peux pas préparer de commande pour vous ici — configurez AI_BASE_URL avec un modèle qui gère les function calls (ex. llama3.2 via Ollama) pour activer la commande par chat. En attendant, utilisez le panier."
	case strings.Contains(lastUser, "commande"):
		content = "Je n'ai pas d'accès à un vrai modèle d'IA pour l'instant (mode démo), mais je vois le contexte de vos commandes récentes ci-dessus si vous en avez. Pour un suivi précis, consultez la page « Mes commandes »."
	case strings.Contains(lastUser, "menu") || strings.Contains(lastUser, "plat"):
		content = "Je suis en mode démo (pas d'IA connectée), mais le menu du restaurant que vous consultez est listé dans mon contexte. Je vous invite aussi à parcourir la page du restaurant pour tous les détails."
	case strings.Contains(lastUser, "promo") || strings.Contains(lastUser, "code"):
		content = "Les codes promo sont saisissables directement dans le panier, dans le champ « Code promo ». Je suis en mode démo, donc je ne peux pas en générer un pour vous ici."
	default:
		content = "Bonjour ! Je suis l'assistant Good Food, actuellement en mode démo (aucune IA connectée — configurez AI_BASE_URL pour brancher un vrai modèle, local ou cloud). Comment puis-je vous aider ?"
	}
	return domain.CompletionResult{Content: content}, nil
}
