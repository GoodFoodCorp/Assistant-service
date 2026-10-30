package application

import "goodfood/assistant-service/internal/domain"

// Actor is the authenticated caller, extracted from the JWT by the HTTP
// adapter. Token is the raw JWT, forwarded to order-service so the assistant
// only ever sees the calling customer's own orders — never a service account.
type Actor struct {
	UserID    string
	RoleSlugs []string
	Token     string
	ClientIP  string
}

type UseCases struct {
	llm         domain.LLMProvider
	orders      domain.OrdersProvider
	menu        domain.MenuProvider
	payments    domain.PaymentMethodsProvider
	addresses   domain.AddressProvider
	restaurants domain.RestaurantsProvider
	delivery    domain.DeliveryChecker // nil: proposals are not checked (tests)
	signer      *HistorySigner         // nil: history is not verified (tests)
}

// WithDeliveryChecks makes every order proposal checked with the restaurant
// (open, taking orders, address in its zone, minimum order) first.
func (uc *UseCases) WithDeliveryChecks(d domain.DeliveryChecker) *UseCases {
	uc.delivery = d
	return uc
}

// WithHistorySigner signs the assistant's replies and drops, from the history
// the browser sends back, any assistant turn this service did not write.
func (uc *UseCases) WithHistorySigner(s *HistorySigner) *UseCases {
	uc.signer = s
	return uc
}

func NewUseCases(llm domain.LLMProvider, orders domain.OrdersProvider, menu domain.MenuProvider, payments domain.PaymentMethodsProvider, addresses domain.AddressProvider, restaurants domain.RestaurantsProvider) *UseCases {
	return &UseCases{llm: llm, orders: orders, menu: menu, payments: payments, addresses: addresses, restaurants: restaurants}
}
