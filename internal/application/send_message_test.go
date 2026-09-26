package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

type fakeLLM struct {
	lastMessages []domain.Message
	reply        string
	err          error
}

func (f *fakeLLM) Complete(_ context.Context, messages []domain.Message) (string, error) {
	f.lastMessages = messages
	if f.err != nil {
		return "", f.err
	}
	if f.reply != "" {
		return f.reply, nil
	}
	return "réponse", nil
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

func setup() (*UseCases, *fakeLLM, *fakeOrders, *fakeMenu) {
	llm := &fakeLLM{}
	orders := &fakeOrders{}
	menu := &fakeMenu{}
	return NewUseCases(llm, orders, menu), llm, orders, menu
}

var customer = Actor{UserID: "cust-1", RoleSlugs: []string{"user"}, Token: "tok"}

func TestSendMessageRejectsEmptyHistory(t *testing.T) {
	uc, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsWhenLastMessageIsNotFromUser(t *testing.T) {
	uc, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleAssistant, Content: "salut"}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsEmptyContent(t *testing.T) {
	uc, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "   "}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageRejectsOversizedContent(t *testing.T) {
	uc, _, _, _ := setup()
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("a", maxMessageChars+1)}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeValidation, derr.Code)
}

func TestSendMessageHappyPath(t *testing.T) {
	uc, llm, _, _ := setup()
	llm.reply = "Bonjour !"
	reply, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "Bonjour"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Bonjour !", reply)
	require.Len(t, llm.lastMessages, 2, "system context message + the user message")
	assert.Equal(t, domain.RoleSystem, llm.lastMessages[0].Role)
	assert.Equal(t, domain.RoleUser, llm.lastMessages[1].Role)
}

func TestSendMessageTruncatesLongHistory(t *testing.T) {
	uc, llm, _, _ := setup()
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
	uc, llm, orders, _ := setup()
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
	uc, llm, _, menu := setup()
	menu.items = []domain.MenuItemSummary{
		{Name: "Pizza Margherita", Category: "Pizzas", PriceCents: 1499, Available: true},
		{Name: "Plat retiré", Category: "Pizzas", PriceCents: 999, Available: false},
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

func TestSendMessageDegradesGracefullyWhenContextFetchFails(t *testing.T) {
	uc, llm, orders, menu := setup()
	orders.err = errors.New("order-service unreachable")
	menu.err = errors.New("menu-service unreachable")
	reply, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
		RestaurantID: "resto-1",
	})
	require.NoError(t, err, "a context-fetch failure must not fail the chat itself")
	assert.NotEmpty(t, reply)
	_ = llm.lastMessages
}

func TestSendMessageWrapsLLMFailureAsUpstreamError(t *testing.T) {
	uc, llm, _, _ := setup()
	llm.err = errors.New("boom")
	_, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages: []domain.Message{{Role: domain.RoleUser, Content: "salut"}},
	})
	var derr *domain.Error
	require.ErrorAs(t, err, &derr)
	assert.Equal(t, domain.ErrCodeUpstream, derr.Code)
}
