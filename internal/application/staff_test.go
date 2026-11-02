package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goodfood/assistant-service/internal/domain"
)

func TestStaffGetHelpWithTheirOwnSpace(t *testing.T) {
	cases := []struct {
		roles []string
		about string
	}{
		{[]string{"manager"}, "portail franchisé"},
		{[]string{"livreur"}, "espace livreur"},
		{[]string{"admin"}, "portail siège"},
	}
	for _, c := range cases {
		uc, llm, orders, menu, _, _ := setup()
		orders.orders = []domain.OrderSummary{{ID: "11111111-0000-0000-0000-000000000000", Status: "DELIVERED"}}
		menu.items = []domain.MenuItemSummary{{ID: "d1", Name: "Burger", PriceCents: 1000}}

		out, err := uc.SendMessage(context.Background(), Actor{UserID: "staff", RoleSlugs: c.roles, Token: "tok"},
			SendMessageInput{RestaurantID: "r1", Messages: []domain.Message{{Role: domain.RoleUser, Content: "Comment ça marche ?"}}})
		require.NoError(t, err, c.roles)
		assert.Equal(t, "réponse", out.Content)

		system := llm.lastMessages[0].Content
		assert.Contains(t, system, c.about, c.roles)
		assert.NotContains(t, system, "Commandes récentes du client", "no customer orders for staff")
		assert.NotContains(t, system, "Menu du restaurant consulté")
		assert.Empty(t, llm.lastTools, "staff never order through the assistant")
	}
}

func TestCustomersKeepOrdering(t *testing.T) {
	uc, llm, _, menu, _, _ := setup()
	menu.items = []domain.MenuItemSummary{{ID: "d1", Name: "Burger", PriceCents: 1000}}

	_, err := uc.SendMessage(context.Background(), customer,
		SendMessageInput{RestaurantID: "r1", Messages: []domain.Message{{Role: domain.RoleUser, Content: "Je voudrais un burger"}}})
	require.NoError(t, err)
	assert.NotEmpty(t, llm.lastTools)
	assert.Equal(t, "", staffRole(customer))
	assert.Equal(t, "", staffRole(Actor{RoleSlugs: []string{"manager", "user"}}), "a manager who orders as a customer is a customer")
}
