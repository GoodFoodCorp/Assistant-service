package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

type fakeLLM struct {
	lastMessages []domain.Message
	lastTools    []domain.ToolDefinition
	result       domain.CompletionResult
	err          error
}

func (f *fakeLLM) Complete(_ context.Context, messages []domain.Message, tools []domain.ToolDefinition) (domain.CompletionResult, error) {
	f.lastMessages = messages
	f.lastTools = tools
	if f.err != nil {
		return domain.CompletionResult{}, f.err
	}
	if f.result.Content != "" || f.result.ToolCall != nil {
		return f.result, nil
	}
	return domain.CompletionResult{Content: "réponse"}, nil
}

type fakeOrders struct {
	orders []domain.OrderSummary
	err    error
}

func (f *fakeOrders) RecentOrders(_ context.Context, _ string) ([]domain.OrderSummary, error) {
	return f.orders, f.err
}

type fakeMenu struct {
	items []domain.MenuItemSummary
	err   error
}

func (f *fakeMenu) MenuForRestaurant(_ context.Context, _ string) ([]domain.MenuItemSummary, error) {
	return f.items, f.err
}

type fakePayments struct {
	method string
	err    error
}

func (f *fakePayments) DefaultPaymentMethod(_ context.Context, _ string) (string, error) {
	return f.method, f.err
}

type fakeAddresses struct {
	address string
	err     error
}

func (f *fakeAddresses) DefaultAddress(_ context.Context, _ string) (string, error) {
	return f.address, f.err
}

func setup() (*UseCases, *fakeLLM, *fakeOrders, *fakeMenu, *fakePayments, *fakeAddresses) {
	llm := &fakeLLM{}
	orders := &fakeOrders{}
	menu := &fakeMenu{}
	payments := &fakePayments{}
	addresses := &fakeAddresses{}
	return NewUseCases(llm, orders, menu, payments, addresses), llm, orders, menu, payments, addresses
}

var customer = Actor{UserID: "cust-1", RoleSlugs: []string{"user"}, Token: "tok"}

func toolCallArgs(t *testing.T, args proposeOrderArgs) string {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return string(raw)
}

func TestSendMessageRejectsEmptyHistory(t *testing.T) {
	uc, _, _, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsWhenLastMessageIsNotFromUser(t *testing.T) {
	uc, _, _, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleAssistant, Content: "salut"}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsEmptyContent(t *testing.T) {
	uc, _, _, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "   "}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsOversizedContent(t *testing.T) {
	uc, _, _, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("a", maxMessageChars+1)}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageHappyPath(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	llm.result = domain.CompletionResult{Content: "Bonjour !"}
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Bonjour"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Bonjour !", out.Content)
	assert.Nil(t, out.Proposal)
	require.Len(t, llm.lastMessages, 2, "system context message + the user message")
	assert.Equal(t, domain.RoleSystem, llm.lastMessages[0].Role)
	assert.Equal(t, domain.RoleUser, llm.lastMessages[1].Role)
}

func TestSendMessageTruncatesLongHistory(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	messages := make([]domain.Message, 0, maxHistoryMessages+10)
	for i := 0; i < maxHistoryMessages+9; i++ {
		messages = append(messages, domain.Message{Role: domain.RoleUser, Content: "msg"})
	}
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: "dernier message"})

	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: messages})
	require.NoError(t, err)
	assert.Len(t, llm.lastMessages, maxHistoryMessages+1, "system message + capped history")
}

func TestSendMessageIncludesOrderContext(t *testing.T) {
	uc, llm, orders, _, _, _ := setup()
	orders.orders = []domain.OrderSummary{
		{ID: "order-123456789", Status: "CONFIRMED", TotalAmountCents: 1299, ItemNames: []string{"Burger"}},
	}
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "où en est ma commande ?"}},
	})
	require.NoError(t, err)
	systemMsg := llm.lastMessages[0].Content
	assert.Contains(t, systemMsg, "CONFIRMED")
	assert.Contains(t, systemMsg, "Burger")
}

func TestSendMessageIncludesMenuContextWhenRestaurantGiven(t *testing.T) {
	uc, llm, _, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{
		{ID: "item-1", Name: "Pizza Margherita", Category: "Pizzas", PriceCents: 1499, Available: true},
		{ID: "item-2", Name: "Plat retiré", Category: "Pizzas", PriceCents: 999, Available: false},
	}
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "qu'y a-t-il au menu ?"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	systemMsg := llm.lastMessages[0].Content
	assert.Contains(t, systemMsg, "Pizza Margherita")
	assert.NotContains(t, systemMsg, "Plat retiré", "unavailable items are not offered to the customer")
}

func TestSendMessageOffersOrderToolOnlyWithAMenu(t *testing.T) {
	uc, llm, _, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{{ID: "item-1", Name: "Burger", PriceCents: 1000, Available: true}}

	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	require.Len(t, llm.lastTools, 1)
	assert.Equal(t, proposeOrderTool, llm.lastTools[0].Name)
	assert.Contains(t, llm.lastMessages[0].Content, "propose_order",
		"the ordering instructions must be in the prompt exactly when the tool is offered")

	_, err = uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
	})
	require.NoError(t, err)
	assert.Empty(t, llm.lastTools, "no restaurant in view — nothing to order from")
}

// Regression test: a real conversation once had the model fabricate a fake
// propose_order(...) call as plain text when no restaurant was in view,
// because the prompt mentioned the tool unconditionally. The prompt must now
// explicitly tell the model ordering is unavailable instead of staying
// silent about it.
func TestSendMessagePromptForbidsFakingToolCallsWhenOrderingUnavailable(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "commande-moi un burger"}},
	})
	require.NoError(t, err)
	systemMsg := llm.lastMessages[0].Content
	assert.Contains(t, systemMsg, "Tu ne peux PAS préparer de commande")
	assert.NotContains(t, systemMsg, "propose_order",
		"the tool must not even be named in the prompt when it isn't actually offered")
}

// Regression test: a customer asked to order while viewing a restaurant,
// then navigated to their address book mid-conversation — the assistant kept
// asking for an address that was already saved, because the address was
// only being fetched when a restaurant (and so the order tool) was also in
// view. The address must be available regardless: it's useful context (and
// avoids repeating a question the customer already answered) whether or not
// an order can actually be placed on this exact request.
func TestSendMessageIncludesDefaultAddressEvenWithoutARestaurantInView(t *testing.T) {
	uc, llm, _, menu, _, addresses := setup()
	addresses.address = "12 rue de Paris, 75001 Paris"

	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
	})
	require.NoError(t, err)
	assert.Contains(t, llm.lastMessages[0].Content, "12 rue de Paris, 75001 Paris",
		"the address is known context even when there's no restaurant to order from yet")

	menu.items = []domain.MenuItemSummary{{ID: "item-1", Name: "Burger", PriceCents: 1000, Available: true}}
	_, err = uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	assert.Contains(t, llm.lastMessages[0].Content, "12 rue de Paris, 75001 Paris")
}

func TestSendMessageDegradesGracefullyWhenContextFetchFails(t *testing.T) {
	uc, _, orders, menu, _, _ := setup()
	orders.err = errors.New("order-service unreachable")
	menu.err = errors.New("menu-service unreachable")
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err, "a context-fetch failure must not fail the chat itself")
	assert.NotEmpty(t, out.Content)
}

func TestSendMessageWrapsLLMFailureAsUpstreamError(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	llm.err = errors.New("boom")
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeUpstream, derr.Code)
}

// Regression test: a real conversation had the model narrate a full fake
// order confirmation — "commande confirmée", plus an order number lifted
// straight from the customer's real order history — entirely in plain text,
// without ever calling propose_order. No proposal means no confirm button,
// so the customer was told they'd ordered something that never happened.
// The prompt now forbids this explicitly, but since that's best-effort, this
// is caught here too regardless of what the model actually says.
func TestSendMessageCatchesHallucinatedOrderIDInPlainText(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	llm.result = domain.CompletionResult{
		Content: "La commande est confirmée et sera livrée rapidement ! Numéro de commande : #8bb387ec",
	}
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "oui c'est ça"}},
	})
	require.NoError(t, err)
	assert.Nil(t, out.Proposal)
	assert.Equal(t, noFakeConfirmationMessage, out.Content)
	assert.NotContains(t, out.Content, "8bb387ec")
}

func TestSendMessageCatchesHallucinatedConfirmationPhraseWithoutAnID(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	llm.result = domain.CompletionResult{Content: "Très bien, votre commande a été confirmée !"}
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "oui"}},
	})
	require.NoError(t, err)
	assert.Nil(t, out.Proposal)
	assert.Equal(t, noFakeConfirmationMessage, out.Content)
}

func TestSendMessageDoesNotFlagOrdinaryReplies(t *testing.T) {
	uc, llm, _, _, _, _ := setup()
	llm.result = domain.CompletionResult{
		Content: "Votre commande sera livrée sous 30 à 40 minutes après confirmation du restaurant.",
	}
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "quel est le délai de livraison ?"}},
	})
	require.NoError(t, err)
	assert.Equal(t, llm.result.Content, out.Content, "an ordinary FAQ-style answer must not be flagged")
}

// ── propose_order tool handling ─────────────────────────────

func TestSendMessageResolvesOrderProposalAgainstRealMenu(t *testing.T) {
	uc, llm, _, menu, payments, _ := setup()
	menu.items = []domain.MenuItemSummary{
		{ID: "item-1", Name: "Burger Deluxe", PriceCents: 1299, Available: true},
	}
	payments.method = "Visa •••• 4242"
	llm.result = domain.CompletionResult{ToolCall: &domain.ToolCall{
		Name: proposeOrderTool,
		Arguments: toolCallArgs(t, proposeOrderArgs{
			Items:           []proposedItemArgs{{Name: "Burger Deluxe", Quantity: 2}},
			DeliveryAddress: "12 rue de Paris",
		}),
	}}

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "commande-moi 2 burgers deluxe"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "resto-1", out.Proposal.RestaurantID)
	assert.Equal(t, "12 rue de Paris", out.Proposal.DeliveryAddress)
	assert.Equal(t, "Visa •••• 4242", out.Proposal.PaymentMethod)
	require.Len(t, out.Proposal.Items, 1)
	assert.Equal(t, "item-1", out.Proposal.Items[0].MenuItemID, "the real menu item id, never invented by the model")
	assert.Equal(t, 2, out.Proposal.Items[0].Quantity)
	assert.Equal(t, int64(1299), out.Proposal.Items[0].UnitPriceCents, "the real price, never trusted from the model")
	assert.Equal(t, int64(2598), out.Proposal.TotalAmountCents)
	assert.Contains(t, out.Content, "Je confirme ?")
}

func TestSendMessageRefusesProposalForItemNotOnMenu(t *testing.T) {
	uc, llm, _, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{{ID: "item-1", Name: "Burger Deluxe", PriceCents: 1299, Available: true}}
	llm.result = domain.CompletionResult{ToolCall: &domain.ToolCall{
		Name: proposeOrderTool,
		Arguments: toolCallArgs(t, proposeOrderArgs{
			Items:           []proposedItemArgs{{Name: "Licorne Flambée", Quantity: 1}},
			DeliveryAddress: "12 rue de Paris",
		}),
	}}

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "commande-moi une licorne flambée"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err, "an unresolvable item degrades to a clarifying message, not an error")
	assert.Nil(t, out.Proposal, "never a proposal for an item that isn't really on the menu")
	assert.Contains(t, out.Content, "Licorne Flambée")
}

func TestSendMessageRefusesProposalWithoutDeliveryAddress(t *testing.T) {
	uc, llm, _, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{{ID: "item-1", Name: "Burger Deluxe", PriceCents: 1299, Available: true}}
	llm.result = domain.CompletionResult{ToolCall: &domain.ToolCall{
		Name: proposeOrderTool,
		Arguments: toolCallArgs(t, proposeOrderArgs{
			Items: []proposedItemArgs{{Name: "Burger Deluxe", Quantity: 1}},
		}),
	}}

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "commande-moi un burger"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	assert.Nil(t, out.Proposal)
	assert.Contains(t, strings.ToLower(out.Content), "adresse")
}

func TestSendMessageProposalNeverExecutesAnOrder(t *testing.T) {
	// resolveProposal only ever talks to the menu and payments providers to
	// build an OrderProposal — it must never call anything that creates an
	// order. There is no OrdersProvider.Create in this port at all, so this
	// test mainly documents the guarantee: assert the fake order repository
	// (RecentOrders-only) was the sole orders interaction.
	uc, llm, orders, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{{ID: "item-1", Name: "Burger Deluxe", PriceCents: 1299, Available: true}}
	llm.result = domain.CompletionResult{ToolCall: &domain.ToolCall{
		Name: proposeOrderTool,
		Arguments: toolCallArgs(t, proposeOrderArgs{
			Items:           []proposedItemArgs{{Name: "Burger Deluxe", Quantity: 1}},
			DeliveryAddress: "12 rue de Paris",
		}),
	}}

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "commande-moi un burger"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err)
	require.NotNil(t, out.Proposal)
	assert.Empty(t, orders.orders, "resolving a proposal never creates an order")
}
