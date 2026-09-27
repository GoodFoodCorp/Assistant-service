package application

import "goodfood/assistant-service/internal/domain"

// Actor is the authenticated caller, extracted from the JWT by the HTTP
// adapter. Token is the raw JWT, forwarded to order-service so the assistant
// only ever sees the calling customer's own orders — never a service account.
type Actor struct {
	UserID    string
	RoleSlugs []string
	Token     string
}

type UseCases struct {
	llm      domain.LLMProvider
	orders   domain.OrdersProvider
	menu     domain.MenuProvider
	payments domain.PaymentMethodsProvider
}

func NewUseCases(llm domain.LLMProvider, orders domain.OrdersProvider, menu domain.MenuProvider, payments domain.PaymentMethodsProvider) *UseCases {
	return &UseCases{llm: llm, orders: orders, menu: menu, payments: payments}
}
