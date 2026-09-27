package domain

import "context"

// ToolDefinition describes a function the model may call, in the same shape
// every OpenAI-compatible endpoint expects (JSON Schema parameters).
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// LLMProvider is the outbound port to the language model — a real
// OpenAI-compatible HTTP endpoint (local or cloud) or, when none is
// configured, a canned FakeProvider so the app stays demoable offline.
type LLMProvider interface {
	Complete(ctx context.Context, messages []Message, tools []ToolDefinition) (CompletionResult, error)
}

// OrdersProvider fetches a customer's recent orders from order-service, so
// the assistant can answer "où en est ma commande ?" without the frontend
// having to assemble that context itself.
type OrdersProvider interface {
	RecentOrders(ctx context.Context, token string) ([]OrderSummary, error)
}

// MenuProvider fetches a restaurant's menu from menu-service, so the
// assistant can answer questions about what's available to order, and so an
// OrderProposal can be resolved against real items and real prices.
type MenuProvider interface {
	MenuForRestaurant(ctx context.Context, restaurantID string) ([]MenuItemSummary, error)
}

// PaymentMethodsProvider fetches the customer's saved card from
// payment-service — informational only, so the assistant can name it in an
// OrderProposal exactly like the cart's checkout summary would.
type PaymentMethodsProvider interface {
	DefaultPaymentMethod(ctx context.Context, token string) (string, error)
}

// AddressProvider fetches the customer's default saved address from
// user-service, so "chez moi" / "mon adresse habituelle" resolves to a real
// address instead of being passed through to order-service as-is.
type AddressProvider interface {
	DefaultAddress(ctx context.Context, token string) (string, error)
}
