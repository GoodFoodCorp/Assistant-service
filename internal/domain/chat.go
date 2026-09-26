package domain

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Role    string
	Content string
}

// OrderSummary is the slice of an order relevant to the assistant's context —
// never the full order-service record.
type OrderSummary struct {
	ID               string
	Status           string
	TotalAmountCents int64
	ItemNames        []string
	PlacedAt         string
}

// MenuItemSummary is the slice of a menu item relevant to the assistant's
// context.
type MenuItemSummary struct {
	Name         string
	Description  string
	PriceCents   int64
	Category     string
	Available    bool
}
