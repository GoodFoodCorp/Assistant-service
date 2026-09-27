package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

const (
	maxHistoryMessages = 20
	maxMessageChars    = 4000
	maxProposedItems   = 20
	maxItemQuantity    = 20

	proposeOrderTool = "propose_order"

	corePrompt = `Tu es l'assistant Good Food, un service de livraison de repas.
Réponds en français, de façon brève, chaleureuse et utile.

Ton périmètre est strictement limité à Good Food : le menu d'un restaurant,
le statut ou l'historique d'une commande, les codes promo, la livraison, ou
le fonctionnement du service.
Pour toute question hors de ce périmètre (recette de cuisine, actualité,
culture générale, aide en programmation, etc.), décline poliment en une
phrase et recentre la conversation sur ce que tu peux faire — ne réponds
jamais à la question hors-sujet elle-même, même partiellement.
Si tu ne sais pas répondre à une question qui relève bien de ton périmètre,
dis-le simplement et propose de contacter le support.`

	// Appended only when the propose_order tool is actually offered to the
	// model for this request (a restaurant's menu is in view) — the model
	// must never be told it can order when the tool isn't really available,
	// or it fabricates a fake function call as plain text instead.
	orderingAvailablePrompt = `

Tu peux préparer une commande pour le client avec l'outil propose_order —
mais UNIQUEMENT avec des plats qui apparaissent réellement dans le menu
ci-dessus (jamais un plat inventé).

Si le client dit "chez moi", "mon adresse habituelle" ou équivalent, et
qu'une adresse par défaut est indiquée ci-dessus, c'est une information
SUFFISANTE — ne redemande jamais cette adresse, et n'annonce pas non plus au
client quelle adresse tu vas utiliser : utilise-la en silence, directement
comme argument de l'outil, exactement comme si le client l'avait tapée lui-
même. Ne l'écris JAMAIS dans ta réponse en texte, sous quelque forme que ce
soit (ni en phrase, ni en JSON, ni en code) — appelle l'outil directement.

Dès que tu connais à la fois le ou les plats ET l'adresse de livraison
exacte, appelle IMMÉDIATEMENT l'outil propose_order — n'écris PAS de message
demandant "voulez-vous confirmer ?", ne récapitule pas la commande toi-même,
et n'écris JAMAIS de texte ressemblant à un appel de fonction ou à du code
(ex. propose_order("...")) : l'appel réel de l'outil affiche déjà au client
un récapitulatif avec un bouton de confirmation, c'est lui, pas toi, qui gère
cette étape. Ton seul rôle avant d'appeler l'outil est de réunir les
informations manquantes (plat, adresse) ; dès qu'elles sont réunies, appelle
l'outil au lieu de répondre par du texte.`

	// Appended when no restaurant is in view — without this, a model that has
	// seen propose_order mentioned earlier in the conversation (or knows the
	// concept from training) may try to "call" it anyway as plain text, even
	// though it was never actually offered as a tool for this request.
	orderingUnavailablePrompt = `

Tu ne peux PAS préparer de commande dans cette conversation : aucun outil de
commande ne t'est proposé ici (le client n'a pas de restaurant ouvert). Si le
client demande à commander quelque chose, dis-lui simplement d'ouvrir la page
d'un restaurant d'abord — n'écris jamais de texte ressemblant à un appel de
fonction ou à du code, et ne prétends jamais avoir préparé une commande.`
)

type SendMessageInput struct {
	Messages     []domain.Message
	RestaurantID string
}

type SendMessageOutput struct {
	Content  string
	Proposal *domain.OrderProposal
}

// SendMessage answers the customer's latest message, enriched with their own
// recent orders and — if a restaurant is in view — its current menu. Context
// fetches are best-effort: a failure there degrades the answer, it never
// fails the chat itself. When the model calls the propose_order tool, its
// arguments are resolved against the real menu (never trusted as-is) into an
// OrderProposal that the front must still get the customer to confirm —
// SendMessage itself never places an order.
func (uc *UseCases) SendMessage(ctx context.Context, actor Actor, in SendMessageInput) (*SendMessageOutput, error) {
	if len(in.Messages) == 0 {
		return nil, domain.NewValidationError("messages must not be empty")
	}
	last := in.Messages[len(in.Messages)-1]
	if last.Role != domain.RoleUser {
		return nil, domain.NewValidationError("the last message must be from the user")
	}
	if strings.TrimSpace(last.Content) == "" {
		return nil, domain.NewValidationError("message content must not be empty")
	}
	if len(last.Content) > maxMessageChars {
		return nil, domain.NewValidationError("message is too long")
	}

	history := in.Messages
	if len(history) > maxHistoryMessages {
		history = history[len(history)-maxHistoryMessages:]
	}

	menu, systemPrompt, orderingEnabled := uc.buildContext(ctx, actor, in.RestaurantID)
	system := domain.Message{Role: domain.RoleSystem, Content: systemPrompt}
	full := append([]domain.Message{system}, history...)

	var tools []domain.ToolDefinition
	if orderingEnabled {
		tools = []domain.ToolDefinition{orderProposalTool()}
	}

	result, err := uc.llm.Complete(ctx, full, tools)
	if err != nil {
		return nil, domain.NewUpstreamError(err.Error())
	}

	if result.ToolCall != nil && result.ToolCall.Name == proposeOrderTool {
		return uc.resolveProposal(ctx, actor, in.RestaurantID, menu, result.ToolCall.Arguments)
	}
	return &SendMessageOutput{Content: result.Content}, nil
}

// buildContext is best-effort: an order-service/menu-service/user-service
// hiccup degrades the answer's context, it never fails the chat itself. It
// also returns the fetched menu (possibly nil, for OrderProposal resolution
// without a second round-trip) and whether ordering is actually possible for
// this request — the ordering instructions and the propose_order tool itself
// are only ever included when this is true, so the model is never told it
// can order when it can't.
func (uc *UseCases) buildContext(ctx context.Context, actor Actor, restaurantID string) ([]domain.MenuItemSummary, string, bool) {
	var b strings.Builder
	b.WriteString(corePrompt)

	if orders, err := uc.orders.RecentOrders(ctx, actor.Token); err == nil && len(orders) > 0 {
		b.WriteString("\n\nCommandes récentes du client :\n")
		for _, o := range orders {
			fmt.Fprintf(&b, "- #%s : %s, %s, total %.2f€\n",
				shortID(o.ID), o.Status, strings.Join(o.ItemNames, ", "), float64(o.TotalAmountCents)/100)
		}
	}

	var menu []domain.MenuItemSummary
	if restaurantID != "" {
		if items, err := uc.menu.MenuForRestaurant(ctx, restaurantID); err == nil && len(items) > 0 {
			menu = items
			b.WriteString("\n\nMenu du restaurant consulté :\n")
			for _, m := range items {
				if !m.Available {
					continue
				}
				fmt.Fprintf(&b, "- %s (%s) : %.2f€ — %s\n", m.Name, m.Category, float64(m.PriceCents)/100, m.Description)
			}
		}
	}

	orderingEnabled := len(menu) > 0
	if orderingEnabled {
		if address, err := uc.addresses.DefaultAddress(ctx, actor.Token); err == nil && address != "" {
			fmt.Fprintf(&b, "\n\nAdresse de livraison par défaut du client : %s\n", address)
		}
		b.WriteString(orderingAvailablePrompt)
	} else {
		b.WriteString(orderingUnavailablePrompt)
	}

	return menu, b.String(), orderingEnabled
}

func orderProposalTool() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name: proposeOrderTool,
		Description: "Prépare une commande à confirmer par le client — n'exécute rien, affiche juste un " +
			"récapitulatif à valider. N'appelle cette fonction que lorsque tu connais le ou les plats (qui " +
			"doivent exister dans le menu ci-dessus) ET l'adresse de livraison exacte.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":     map[string]any{"type": "string", "description": "Nom du plat, tel qu'il apparaît dans le menu"},
							"quantity": map[string]any{"type": "integer", "minimum": 1},
						},
						"required": []string{"name", "quantity"},
					},
				},
				"delivery_address": map[string]any{"type": "string", "description": "Adresse de livraison complète"},
			},
			"required": []string{"items", "delivery_address"},
		},
	}
}

type proposedItemArgs struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type proposeOrderArgs struct {
	Items           []proposedItemArgs `json:"items"`
	DeliveryAddress string             `json:"delivery_address"`
}

// resolveProposal turns the model's raw tool-call arguments into a real
// OrderProposal — every item is matched against the actual menu fetched for
// this request, so neither the item's existence nor its price ever comes
// from the model. If an item can't be matched, no proposal is returned: the
// customer sees a message asking them to clarify instead of a silently
// wrong order.
func (uc *UseCases) resolveProposal(ctx context.Context, actor Actor, restaurantID string, menu []domain.MenuItemSummary, rawArgs string) (*SendMessageOutput, error) {
	var args proposeOrderArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return &SendMessageOutput{Content: "Désolé, je n'ai pas réussi à préparer cette commande. Peux-tu reformuler ?"}, nil
	}
	if strings.TrimSpace(args.DeliveryAddress) == "" {
		return &SendMessageOutput{Content: "Il me manque l'adresse de livraison — à quelle adresse dois-je faire livrer ?"}, nil
	}
	if len(args.Items) == 0 {
		return &SendMessageOutput{Content: "Je n'ai pas compris quel(s) plat(s) tu veux commander — peux-tu préciser ?"}, nil
	}
	if len(args.Items) > maxProposedItems {
		return &SendMessageOutput{Content: "C'est beaucoup trop de plats différents pour une seule commande — peux-tu réduire la liste ?"}, nil
	}

	items := make([]domain.ProposedItem, 0, len(args.Items))
	var totalCents int64
	for _, req := range args.Items {
		match := findMenuItem(menu, req.Name)
		if match == nil {
			return &SendMessageOutput{
				Content: fmt.Sprintf("Je ne trouve pas « %s » dans le menu de ce restaurant — peux-tu vérifier le nom exact du plat ?", req.Name),
			}, nil
		}
		qty := req.Quantity
		if qty < 1 {
			qty = 1
		}
		if qty > maxItemQuantity {
			qty = maxItemQuantity
		}
		items = append(items, domain.ProposedItem{
			MenuItemID: match.ID, MenuItemName: match.Name, Quantity: qty, UnitPriceCents: match.PriceCents,
		})
		totalCents += match.PriceCents * int64(qty)
	}

	paymentMethod, _ := uc.payments.DefaultPaymentMethod(ctx, actor.Token) // best-effort, "" is fine

	proposal := &domain.OrderProposal{
		RestaurantID: restaurantID, Items: items, DeliveryAddress: strings.TrimSpace(args.DeliveryAddress),
		TotalAmountCents: totalCents, PaymentMethod: paymentMethod,
	}
	return &SendMessageOutput{Content: formatProposal(proposal), Proposal: proposal}, nil
}

// formatProposal is built deterministically in Go, never by the model —
// there is no risk of a hallucinated total or item in what the customer
// reads just before confirming.
func formatProposal(p *domain.OrderProposal) string {
	var b strings.Builder
	b.WriteString("Voici ce que je te propose :\n")
	for _, it := range p.Items {
		fmt.Fprintf(&b, "- %d× %s — %.2f€\n", it.Quantity, it.MenuItemName, float64(it.UnitPriceCents*int64(it.Quantity))/100)
	}
	fmt.Fprintf(&b, "\nLivraison à : %s\n", p.DeliveryAddress)
	fmt.Fprintf(&b, "Total : %.2f€ (hors frais de livraison)\n", float64(p.TotalAmountCents)/100)
	if p.PaymentMethod != "" {
		fmt.Fprintf(&b, "Paiement : %s\n", p.PaymentMethod)
	}
	b.WriteString("\nJe confirme ?")
	return b.String()
}

// findMenuItem matches case-insensitively, exact first, then either name
// containing the other — good enough for a model naming a dish close to how
// it appears on the menu, never for guessing an item that isn't there.
func findMenuItem(menu []domain.MenuItemSummary, name string) *domain.MenuItemSummary {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return nil
	}
	for i := range menu {
		if !menu[i].Available {
			continue
		}
		if strings.ToLower(menu[i].Name) == needle {
			return &menu[i]
		}
	}
	for i := range menu {
		if !menu[i].Available {
			continue
		}
		hay := strings.ToLower(menu[i].Name)
		if strings.Contains(hay, needle) || strings.Contains(needle, hay) {
			return &menu[i]
		}
	}
	return nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
