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

// Regression: "livre-moi un burger deluxe chez moi" typed on a page with no
// restaurant open (home, account…) only got "go open a restaurant page". With
// several restaurants the assistant now chooses one with the customer.

type fakeRestaurants struct {
	list []domain.RestaurantSummary
	err  error
}

func (f *fakeRestaurants) ListRestaurants(_ context.Context) ([]domain.RestaurantSummary, error) {
	return f.list, f.err
}

// menusByRestaurant serves a different menu per restaurant id.
type menusByRestaurant struct {
	menus     map[string][]domain.MenuItemSummary
	requested []string
}

func (m *menusByRestaurant) MenuForRestaurant(_ context.Context, id string) ([]domain.MenuItemSummary, error) {
	m.requested = append(m.requested, id)
	items, ok := m.menus[id]
	if !ok {
		return nil, errors.New("menu-service unreachable")
	}
	return items, nil
}

var (
	republique   = domain.RestaurantSummary{ID: "r-rep", Name: "Good Food République", City: "Paris"}
	montparnasse = domain.RestaurantSummary{ID: "r-mon", Name: "Good Food Montparnasse", City: "Paris"}
	burger       = domain.MenuItemSummary{ID: "item-1", Name: "Burger Deluxe", PriceCents: 1299, Available: true}
)

func multiRestaurantSetup() (*UseCases, *fakeLLM, *menusByRestaurant) {
	uc, llm, _, _, payments, addresses := setup()
	payments.method = "Visa •••• 4242"
	addresses.address = "12 rue de Paris"
	uc.restaurants = &fakeRestaurants{list: []domain.RestaurantSummary{republique, montparnasse}}
	menus := &menusByRestaurant{menus: map[string][]domain.MenuItemSummary{
		"r-rep": {burger},
		"r-mon": {{ID: "item-9", Name: "Burger Deluxe", PriceCents: 1399, Available: true}},
	}}
	uc.menu = menus
	return uc, llm, menus
}

func orderToolCall(t *testing.T, restaurant string) domain.CompletionResult {
	t.Helper()
	return domain.CompletionResult{ToolCall: &domain.ToolCall{
		Name: proposeOrderTool,
		Arguments: toolCallArgs(t, proposeOrderArgs{
			Restaurant:      restaurant,
			Items:           []proposedItemArgs{{Name: "Burger Deluxe", Quantity: 1}},
			DeliveryAddress: "12 rue de Paris",
		}),
	}}
}

func ask(t *testing.T, uc *UseCases, restaurantID, text string) *SendMessageOutput {
	t.Helper()
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{
		Messages:     []domain.Message{{Role: domain.RoleUser, Content: text}},
		RestaurantID: restaurantID,
	})
	require.NoError(t, err)
	return out
}

func TestOrderingAcrossRestaurants_OffersToolAndListsRestaurants(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	ask(t, uc, "", "livre moi un burger deluxe chez moi")

	system := llm.lastMessages[0].Content
	assert.Contains(t, system, "Good Food République")
	assert.Contains(t, system, "Good Food Montparnasse")
	assert.NotContains(t, system, "Tu ne peux PAS préparer de commande")

	require.Len(t, llm.lastTools, 1)
	props := llm.lastTools[0].Parameters["properties"].(map[string]any)
	assert.Contains(t, props, "restaurant", "the model must say which restaurant")
	assert.Contains(t, llm.lastTools[0].Parameters["required"], "restaurant")
}

func TestOrderingAcrossRestaurants_ResolvesTheRestaurantAndItsOwnMenu(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	llm.result = orderToolCall(t, "Montparnasse")

	out := ask(t, uc, "", "un burger deluxe à Montparnasse")
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "r-mon", out.Proposal.RestaurantID)
	assert.Equal(t, "item-9", out.Proposal.Items[0].MenuItemID, "the chosen restaurant's own item, not another's")
	assert.Equal(t, int64(1399), out.Proposal.TotalAmountCents, "that restaurant's price")
	assert.Equal(t, "r-mon", menus.requested[len(menus.requested)-1], "resolution loads the chosen restaurant's menu")
}

func TestOrderingAcrossRestaurants_MatchesNamesLeniently(t *testing.T) {
	for _, said := range []string{"république", "REPUBLIQUE", "Good Food République", "good food republique"} {
		t.Run(said, func(t *testing.T) {
			uc, llm, _ := multiRestaurantSetup()
			llm.result = orderToolCall(t, said)
			out := ask(t, uc, "", "un burger chez "+said)
			require.NotNil(t, out.Proposal, "should have matched %q", said)
			assert.Equal(t, "r-rep", out.Proposal.RestaurantID)
		})
	}
}

func TestOrderingAcrossRestaurants_UnknownRestaurantListsTheRealOnes(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	llm.result = orderToolCall(t, "Chez Fantôme")

	out := ask(t, uc, "", "un burger chez Fantôme")
	assert.Nil(t, out.Proposal)
	assert.Contains(t, out.Content, "Chez Fantôme")
	assert.Contains(t, out.Content, "République")
	assert.Contains(t, out.Content, "Montparnasse")
	assert.Len(t, menus.requested, 2, "only the context's own lookups, none for a restaurant that does not exist")
}

func TestOrderingAcrossRestaurants_MissingOrAmbiguousRestaurantAsksWhich(t *testing.T) {
	for name, said := range map[string]string{"missing": "", "ambiguous": "Good Food"} {
		t.Run(name, func(t *testing.T) {
			uc, llm, menus := multiRestaurantSetup()
			llm.result = orderToolCall(t, said)
			out := ask(t, uc, "", "un burger")
			assert.Nil(t, out.Proposal, "never guess between two restaurants")
			assert.Contains(t, strings.ToLower(out.Content), "quel restaurant")
			assert.Contains(t, out.Content, "République")
			assert.Len(t, menus.requested, 2, "no lookup beyond the context's")
		})
	}
}

func TestOrderingAcrossRestaurants_UnreachableMenuIsAClearMessage(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	delete(menus.menus, "r-rep")
	llm.result = orderToolCall(t, "République")

	out := ask(t, uc, "", "un burger à République")
	assert.Nil(t, out.Proposal)
	assert.Contains(t, out.Content, "République")
}

func TestOrderingAcrossRestaurants_ItemMustExistInTheChosenRestaurant(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	menus.menus["r-rep"] = []domain.MenuItemSummary{{ID: "p1", Name: "Pizza Regina", PriceCents: 1599, Available: true}}
	llm.result = orderToolCall(t, "République") // asks for a Burger Deluxe

	out := ask(t, uc, "", "un burger deluxe à République")
	assert.Nil(t, out.Proposal)
	assert.Contains(t, out.Content, "Burger Deluxe")
}

func TestOrderingAcrossRestaurants_SingleRestaurantIsSelectedAutomatically(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	uc.restaurants = &fakeRestaurants{list: []domain.RestaurantSummary{republique}}
	llm.result = orderToolCall(t, "") // the model has nothing to choose between

	out := ask(t, uc, "", "livre moi un burger deluxe chez moi")
	require.NotNil(t, out.Proposal, "one restaurant: no need to ask which")
	assert.Equal(t, "r-rep", out.Proposal.RestaurantID)
	assert.Contains(t, llm.lastMessages[0].Content, "Burger Deluxe", "its menu is in the prompt like on its own page")
	assert.NotContains(t, llm.lastMessages[0].Content, "Tu ne peux PAS préparer de commande")
	assert.Equal(t, "r-rep", menus.requested[0])
	props := llm.lastTools[0].Parameters["properties"].(map[string]any)
	assert.NotContains(t, props, "restaurant", "nothing to choose, so no restaurant parameter")
}

func TestOrderingAcrossRestaurants_FallsBackWhenRestaurantsCannotBeListed(t *testing.T) {
	for name, r := range map[string]*fakeRestaurants{
		"error": {err: errors.New("franchise-service down")},
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			uc, llm, _ := multiRestaurantSetup()
			uc.restaurants = r
			ask(t, uc, "", "livre moi un burger")
			assert.Contains(t, llm.lastMessages[0].Content, "Tu ne peux PAS préparer de commande")
			assert.Empty(t, llm.lastTools, "no tool when ordering is impossible")
		})
	}
}

func TestOrderingInARestaurantPageIsUnchanged(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	menus.menus["r-rep"] = []domain.MenuItemSummary{burger}
	llm.result = orderToolCall(t, "") // the page already says which restaurant

	out := ask(t, uc, "r-rep", "un burger deluxe")
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "r-rep", out.Proposal.RestaurantID)
	props := llm.lastTools[0].Parameters["properties"].(map[string]any)
	assert.NotContains(t, props, "restaurant", "on a restaurant page the restaurant is already known")
}

func TestOrderingInARestaurantPageIgnoresARestaurantTheModelMadeUp(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "Montparnasse") // the page is République: the page wins

	out := ask(t, uc, "r-rep", "un burger deluxe")
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "r-rep", out.Proposal.RestaurantID, "the viewed restaurant is never overridden by the model")
}

// Regression: with nothing open the model silently picked a restaurant for the
// customer (République in 6 of 8 live runs) and the recap did not even name it.
func TestOrderingAcrossRestaurants_ModelCannotPickARestaurantTheCustomerDidNotName(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	llm.result = orderToolCall(t, "République")

	out := ask(t, uc, "", "livre moi un burger deluxe chez moi")
	assert.Nil(t, out.Proposal, "the customer never said which restaurant")
	assert.Contains(t, strings.ToLower(out.Content), "quel restaurant")
	assert.Contains(t, out.Content, "Montparnasse", "lists the real choices")
	assert.Len(t, menus.requested, 2, "no lookup beyond the context's")
}

func TestOrderingAcrossRestaurants_OnlyTheCustomersWordsCountAsNamingARestaurant(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "République")

	// The assistant's own question lists both names — that must not count.
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: []domain.Message{
		{Role: domain.RoleUser, Content: "livre moi un burger deluxe chez moi"},
		{Role: domain.RoleAssistant, Content: "Dans quel restaurant ? Good Food République ou Good Food Montparnasse ?"},
		{Role: domain.RoleUser, Content: "peu importe"},
	}})
	require.NoError(t, err)
	assert.Nil(t, out.Proposal)
}

func TestOrderingAcrossRestaurants_RestaurantNamedInAnEarlierCustomerTurnIsEnough(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "République")

	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: []domain.Message{
		{Role: domain.RoleUser, Content: "je suis au restaurant République ce soir"},
		{Role: domain.RoleAssistant, Content: "Super, je peux t'aider à commander."},
		{Role: domain.RoleUser, Content: "livre moi un burger deluxe chez moi"},
	}})
	require.NoError(t, err)
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "r-rep", out.Proposal.RestaurantID)
}

func TestOrderingAcrossRestaurants_RecapNamesTheRestaurant(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "République")

	out := ask(t, uc, "", "un burger deluxe à République")
	require.NotNil(t, out.Proposal)
	assert.Equal(t, "Good Food République", out.Proposal.RestaurantName)
	assert.Contains(t, out.Content, "Restaurant : Good Food République")
}

// The model often asks "tu veux commander chez Good Food République ?" rather
// than listing both: a plain "oui" to that single suggestion is the customer
// choosing it — but only a plain yes, never "oui, plutôt Montparnasse".
func conversationEndingWith(suggestion, reply string) []domain.Message {
	return []domain.Message{
		{Role: domain.RoleUser, Content: "livre moi un burger deluxe chez moi"},
		{Role: domain.RoleAssistant, Content: suggestion},
		{Role: domain.RoleUser, Content: reply},
	}
}

func proposalFor(t *testing.T, restaurantChosenByModel string, history []domain.Message) *SendMessageOutput {
	t.Helper()
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, restaurantChosenByModel)
	out, err := uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: history})
	require.NoError(t, err)
	return out
}

func TestOrderingAcrossRestaurants_YesToASingleSuggestionCountsAsChoosing(t *testing.T) {
	suggestion := "Pouvez-vous me confirmer que vous souhaitez commander chez Good Food République ?"
	for _, yes := range []string{"oui", "Oui !", "ok", "d'accord", "oui merci", "Oui, parfait", "c'est bon"} {
		t.Run(yes, func(t *testing.T) {
			out := proposalFor(t, "République", conversationEndingWith(suggestion, yes))
			require.NotNil(t, out.Proposal, "%q answers the suggestion", yes)
			assert.Equal(t, "r-rep", out.Proposal.RestaurantID)
		})
	}
}

func TestOrderingAcrossRestaurants_YesDoesNotChooseWhenSeveralWereOffered(t *testing.T) {
	both := "Dans quel restaurant ? Good Food Montparnasse ou Good Food République ?"
	out := proposalFor(t, "République", conversationEndingWith(both, "oui"))
	assert.Nil(t, out.Proposal, "a yes to a choice between two chooses neither")
}

func TestOrderingAcrossRestaurants_YesWithSomethingElseIsNotAPlainYes(t *testing.T) {
	suggestion := "Pouvez-vous me confirmer que vous souhaitez commander chez Good Food République ?"
	// Said Montparnasse, but the model still answers République: must not slip through.
	out := proposalFor(t, "République", conversationEndingWith(suggestion, "oui mais plutôt Montparnasse"))
	assert.Nil(t, out.Proposal)
	out = proposalFor(t, "République", conversationEndingWith(suggestion, "non"))
	assert.Nil(t, out.Proposal)
	out = proposalFor(t, "République", conversationEndingWith(suggestion, "peu importe"))
	assert.Nil(t, out.Proposal)
}

func TestOrderingAcrossRestaurants_YesOnlyCountsRightAfterTheSuggestion(t *testing.T) {
	out, err := func() (*SendMessageOutput, error) {
		uc, llm, _ := multiRestaurantSetup()
		llm.result = orderToolCall(t, "République")
		return uc.SendMessage(context.Background(), customer, SendMessageInput{Messages: []domain.Message{
			{Role: domain.RoleUser, Content: "livre moi un burger deluxe chez moi"},
			{Role: domain.RoleAssistant, Content: "Vous souhaitez commander chez Good Food République ?"},
			{Role: domain.RoleUser, Content: "attends, c'est quoi vos horaires ?"},
			{Role: domain.RoleAssistant, Content: "Nous sommes ouverts de 11h à 23h."},
			{Role: domain.RoleUser, Content: "ok"},
		}})
	}()
	require.NoError(t, err)
	assert.Nil(t, out.Proposal, "the 'ok' answers the opening hours, not the restaurant suggestion")
}

// ── Clickable restaurant choices ────────────────────────────

func names(list []domain.RestaurantSummary) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, r.Name)
	}
	return out
}

func TestChoices_WhenTheAssistantMustAskWhichRestaurant(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "") // the model has no idea which one

	out := ask(t, uc, "", "livre moi un burger deluxe chez moi")
	assert.Nil(t, out.Proposal)
	assert.ElementsMatch(t, []string{"Good Food République", "Good Food Montparnasse"}, names(out.Choices))
	assert.NotEmpty(t, out.Choices[0].ID)
}

func TestChoices_WhenTheRestaurantIsUnknown(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = orderToolCall(t, "Chez Fantôme")

	out := ask(t, uc, "", "un burger chez Fantôme")
	assert.Len(t, out.Choices, 2, "offer the real ones to pick from")
}

func TestChoices_WhenTheModelCustomerAnswersInPlainTextAboutRestaurants(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = domain.CompletionResult{Content: "Dans quel restaurant ? Nous avons Good Food Montparnasse et Good Food République."}

	out := ask(t, uc, "", "livre moi un burger deluxe")
	assert.ElementsMatch(t, []string{"Good Food République", "Good Food Montparnasse"}, names(out.Choices))
}

// Regression: the model suggested only "Good Food République", and so did the
// buttons — the customer had no way to pick Montparnasse. Whenever the reply is
// about choosing a restaurant, every restaurant is offered, not just the one(s)
// the model happened to name.
func TestChoices_AllRestaurantsEvenWhenTheReplyNamesOnlyOne(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = domain.CompletionResult{Content: "Pouvez-vous me confirmer que vous souhaitez commander chez Good Food République ?"}

	out := ask(t, uc, "", "livre moi un burger deluxe")
	assert.ElementsMatch(t, []string{"Good Food République", "Good Food Montparnasse"}, names(out.Choices))
}

func TestChoices_AccentsAndCaseDoNotMatter(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	llm.result = domain.CompletionResult{Content: "Préférez-vous GOOD FOOD REPUBLIQUE ?"}

	out := ask(t, uc, "", "livre moi un burger deluxe")
	assert.ElementsMatch(t, []string{"Good Food République", "Good Food Montparnasse"}, names(out.Choices))
}

func TestChoices_NoneWhenNothingToChooseOrNothingMentioned(t *testing.T) {
	// Ordinary reply, no restaurant named.
	uc, llm, _ := multiRestaurantSetup()
	llm.result = domain.CompletionResult{Content: "Bonjour ! Comment puis-je t'aider ?"}
	assert.Empty(t, ask(t, uc, "", "salut").Choices)

	// A proposal is itself the answer.
	uc, llm, _ = multiRestaurantSetup()
	llm.result = orderToolCall(t, "République")
	out := ask(t, uc, "", "un burger à République")
	require.NotNil(t, out.Proposal)
	assert.Empty(t, out.Choices)

	// On a restaurant's own page there is nothing to choose.
	uc, llm, _ = multiRestaurantSetup()
	llm.result = domain.CompletionResult{Content: "Ici c'est Good Food République, un burger ?"}
	assert.Empty(t, ask(t, uc, "r-rep", "salut").Choices)

	// Ordering impossible: no list to offer.
	uc, llm, _ = multiRestaurantSetup()
	uc.restaurants = &fakeRestaurants{}
	llm.result = domain.CompletionResult{Content: "Good Food République est super."}
	assert.Empty(t, ask(t, uc, "", "salut").Choices)
}

// ── Menus in the prompt when nothing is open ────────────────

// Regression: asked "dis-moi en plus sur le burger deluxe" with no restaurant
// open, the model had no menu and invented the ingredients ("lardons,
// mayonnaise"). With few restaurants it now gets each one's menu.
func TestAcrossRestaurants_PromptHoldsEachRestaurantsMenu(t *testing.T) {
	uc, llm, _ := multiRestaurantSetup()
	ask(t, uc, "", "dis moi en plus sur le burger deluxe")

	system := llm.lastMessages[0].Content
	assert.Contains(t, system, "Menu de Good Food République")
	assert.Contains(t, system, "Menu de Good Food Montparnasse")
	assert.Contains(t, system, "12.99€", "République's price")
	assert.Contains(t, system, "13.99€", "Montparnasse's own price")
	assert.NotContains(t, system, "Tu ne connais pas les menus", "it does now")
	assert.Contains(t, system, "n'invente", "and must not go beyond them")
}

func TestAcrossRestaurants_NoMenusWhenThereAreTooManyRestaurants(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	var many []domain.RestaurantSummary
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, domain.RestaurantSummary{ID: id, Name: "Good Food " + strings.ToUpper(id)})
		menus.menus[id] = []domain.MenuItemSummary{burger}
	}
	uc.restaurants = &fakeRestaurants{list: many}
	ask(t, uc, "", "dis moi en plus sur le burger deluxe")

	system := llm.lastMessages[0].Content
	assert.NotContains(t, system, "Menu de Good Food")
	assert.Contains(t, system, "Tu ne connais pas les menus")
	assert.Empty(t, menus.requested, "a prompt full of menus would drown the model: none are fetched")
}

func TestAcrossRestaurants_ARestaurantWhoseMenuFailsIsSkipped(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	delete(menus.menus, "r-mon")
	ask(t, uc, "", "dis moi en plus sur le burger deluxe")

	system := llm.lastMessages[0].Content
	assert.Contains(t, system, "Menu de Good Food République")
	assert.NotContains(t, system, "Menu de Good Food Montparnasse")
}

func TestAcrossRestaurants_AllMenusFailingFallsBackToTheNoMenuInstruction(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	menus.menus = map[string][]domain.MenuItemSummary{}
	ask(t, uc, "", "dis moi en plus sur le burger deluxe")

	assert.Contains(t, llm.lastMessages[0].Content, "Tu ne connais pas les menus")
}

func TestAcrossRestaurants_UnavailableDishesAreNotListed(t *testing.T) {
	uc, llm, menus := multiRestaurantSetup()
	menus.menus["r-rep"] = []domain.MenuItemSummary{burger, {ID: "p", Name: "Pizza épuisée", PriceCents: 1500, Available: false}}
	ask(t, uc, "", "qu'avez-vous ?")

	assert.NotContains(t, llm.lastMessages[0].Content, "Pizza épuisée")
}
