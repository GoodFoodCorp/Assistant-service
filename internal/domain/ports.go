package domain

import "context"

// LLMProvider is the outbound port to the language model — a real
// OpenAI-compatible HTTP endpoint (local or cloud) or, when none is
// configured, a canned FakeProvider so the app stays demoable offline.
type LLMProvider interface {
	Complete(ctx context.Context, messages []Message) (string, error)
}

// OrdersProvider fetches a customer's recent orders from order-service, so
// the assistant can answer "où en est ma commande ?" without the frontend
// having to assemble that context itself.
type OrdersProvider interface {
	RecentOrders(ctx context.Context, token string) ([]OrderSummary, error)
}

// MenuProvider fetches a restaurant's menu from menu-service, so the
// assistant can answer questions about what's available to order.
type MenuProvider interface {
	MenuForRestaurant(ctx context.Context, restaurantID string) ([]MenuItemSummary, error)
}
