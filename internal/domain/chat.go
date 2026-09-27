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
// context. ID is kept (even though it's never shown to the model as text)
// because it's what OrderProposal resolution needs to reference a real item.
type MenuItemSummary struct {
	ID          string
	Name        string
	Description string
	PriceCents  int64
	Category    string
	Available   bool
}

// ToolCall is a function-call request from the model — never executed as-is:
// the application layer resolves it against real data (the actual menu,
// never whatever price the model may have guessed) before it becomes an
// OrderProposal.
type ToolCall struct {
	Name      string
	Arguments string // raw JSON, as returned by the model
}

// CompletionResult is either a plain-text reply, or a ToolCall — never both.
type CompletionResult struct {
	Content  string
	ToolCall *ToolCall
}

// ProposedItem is one line of an OrderProposal, resolved against the real
// menu — MenuItemID and UnitPriceCents are never taken from the model.
type ProposedItem struct {
	MenuItemID     string
	MenuItemName   string
	Quantity       int
	UnitPriceCents int64
}

// OrderProposal is never executed by assistant-service itself — it is
// returned to the front so the customer can review and confirm it, and the
// front then places it through order-service's normal checkout flow, exactly
// as if it had been built by hand from the cart.
type OrderProposal struct {
	RestaurantID     string
	Items            []ProposedItem
	DeliveryAddress  string
	TotalAmountCents int64
	PaymentMethod    string // e.g. "Visa •••• 4242" — informational only
}
