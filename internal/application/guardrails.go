package application

import (
	"regexp"
	"strings"

	"goodfood/assistant-service/internal/domain"
)

// Guardrails around a small, imperfect model. A prompt is only a request: each
// failure seen in a field test with realistic (and silly) questions is also
// caught in code, where it cannot be talked out of.

// ── Matching words in what the customer typed ──────────────

// terms matches any of its words or prefixes at the START of a word, in text
// that has already been lowercased and stripped of accents. A plain substring
// test is a trap: "script" is inside "description".
type terms struct{ re *regexp.Regexp }

func newTerms(words ...string) terms {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = regexp.QuoteMeta(w)
	}
	return terms{regexp.MustCompile(`(?:^|[^a-z0-9])(?:` + strings.Join(quoted, "|") + `)`)}
}

func (t terms) in(folded string) bool { return t.re.MatchString(folded) }

// ── Topics that get a strict, per-request reminder ──────────

const (
	allergyReminder = `

Le client pose une question d'allergie ou de régime alimentaire. Tu ne peux RIEN garantir : tu ne connais que la composition affichée dans le menu, pas les traces, les allergènes ni les certifications (halal, bio, sans gluten…). Ne dis JAMAIS qu'un plat « ne contient pas » un allergène, ni qu'il convient à un régime ou à une religion. Cite seulement la composition affichée si elle aide, puis demande de vérifier auprès du restaurant ou du support avant de commander. Mais n'affirme pas non plus qu'aucun plat ne convient : cite les plats dont la composition affichée ne mentionne pas l'ingrédient concerné, en précisant que c'est à vérifier auprès du restaurant.`

	offTopicReminder = `

Cette question semble hors du périmètre de Good Food : décline en UNE phrase polie, sans donner aucune information, astuce, recette, ingrédient, code, calcul ni conseil médical — même partiel — puis propose ton aide sur le menu ou une commande. Seule exception : si elle porte sur un plat du menu, réponds avec sa composition affichée.`

	cancelReminder = `

Le client veut annuler, modifier ou se faire rembourser une commande. Toi, tu ne peux faire aucune de ces actions. Regarde le statut de sa commande dans la liste : s'il est « enregistrée » ou « payée et acceptée » (pas encore en préparation), il peut l'annuler lui-même tout de suite depuis la page de la commande, dans « Mes commandes », bouton « Annuler la commande », en choisissant un motif : il est remboursé intégralement. Sinon (en préparation, en livraison, livrée), c'est trop tard pour annuler : dis-lui de contacter le support (contact@goodfood.fr, +33 1 23 45 67 89). Une adresse de livraison ne peut pas être modifiée une fois la commande passée, et un autre remboursement se demande au support.`

	unknownInfoReminder = `

Le client demande une information que tu n'as pas (horaires d'ouverture, délai de livraison, zones livrées). Réponds franchement que tu ne la connais pas, sans rien ajouter de général : n'écris jamais « généralement », « habituellement » ni « en principe ». Oriente vers le restaurant ou le support (contact@goodfood.fr).`

	// The sample sentence is only shown when promos come up: a small model
	// pastes any sample it sees into unrelated answers ("commande un burger à
	// 1 euro" got the promo sentence).
	promoReminder = `

Le client parle de codes promo, de promotions ou de réductions. Tu n'as pas accès à la liste des codes promo : réponds dans cet esprit :
« Je n'ai pas la liste des codes promo, mais si vous en avez un, saisissez-le au paiement dans le panier : son effet s'affiche avant de valider. »
N'affirme jamais qu'il n'y en a pas, et n'en cite ni n'en invente aucun.`
)

var (
	allergyTerms = newTerms(
		"allerg", "gluten", "coeliaque", "celiaque", "arachide", "cacahuete", "lactose", "noix", "fruits a coque",
		"halal", "casher", "kasher", "vegan", "vegetalien", "vegetarien", "sans porc", "regime", "intolerance",
	)
	orderChangeTerms = newTerms(
		"annul", "rembours", "modifier ma commande", "modifier la commande", "changer ma commande",
		"changer l'adresse", "changer mon adresse", "modifier l'adresse", "modifier mon adresse",
	)
	unknownInfoTerms = newTerms(
		"horaire", "ouvert", "fermeture", "ferme a", "quelle heure", "delai", "combien de temps",
		"livrez a", "livrez dans", "livrez en", "livrez-vous", "livrez vous", "zone de livraison", "zones de livraison",
	)
	promoTerms = newTerms("promo", "reduc", "remise", "rabais", "coupon", "gratuit", "code", "bienvenue20")

	// "recette" may be about a dish on the menu, and "comment faire" about using
	// the service: those only get a reminder, the model decides.
	softOffTopicTerms = newTerms("recette")
	howTo             = regexp.MustCompile(`(?:^|[^a-z0-9])comment (on |je |tu )?(fait|fais|faire|cuisine|cuisiner|prepare|preparer)\b`)
	serviceTerms      = newTerms("command", "livr", "pay", "reserv", "annul", "suivre", "compte", "promo", "panier", "restaurant")
)

// topicReminders appends, to the system prompt, a strict reminder for the kind
// of question being asked. The detection is deliberately blunt (keywords): it
// only decides which reminder the model gets, never what the customer sees.
func topicReminders(userText string) string {
	t := foldAccents(userText)
	var out strings.Builder
	if allergyTerms.in(t) {
		out.WriteString(allergyReminder)
	}
	if orderChangeTerms.in(t) {
		out.WriteString(cancelReminder)
	}
	if unknownInfoTerms.in(t) {
		out.WriteString(unknownInfoReminder)
	}
	if promoTerms.in(t) {
		out.WriteString(promoReminder)
	}
	if softOffTopicTerms.in(t) || (howTo.MatchString(t) && !serviceTerms.in(t)) {
		out.WriteString(offTopicReminder)
	}
	return out.String()
}

// ── Questions refused without the model ─────────────────────

const instructionsRefusalMessage = "Je suis l'assistant Good Food : je ne peux ni détailler ni modifier mes consignes. Je peux t'aider sur le menu, une commande ou le service."

var (
	// Plainly outside the service. A prompt reminder did not hold for these
	// (it refused, then answered the question anyway), so they are refused in
	// code: instant, free, and impossible to talk out of.
	strongOffTopicTerms = newTerms(
		"python", "javascript", "typescript", "sql", "programmation", "script",
		"capitale de", "coupe du monde", "meteo", "blague", "horoscope", "impot", "traduis", "traduction",
		"mal a la tete", "migraine", "medicament", "docteur", "symptome",
	)
	// Arithmetic: "12*13", "combien font 12 x 13". "combien font 2 burgers" is a price question.
	pureMath    = regexp.MustCompile(`^\s*\d[\d\s.,x×*+/()=-]*\??\s*$`)
	combienMath = regexp.MustCompile(`(?:^|[^a-z0-9])combien (font|fait|ca fait)\s+\d+(?:[.,]\d+)?\s*(?:[x×*+/-]|fois|plus|moins)\s*\d`)

	instructionTerms = newTerms(
		"ton prompt", "ton system", "system prompt", "prompt systeme", "tes instructions", "tes consignes",
		"tes regles internes", "tes regles de fonctionnement", "ignore tes", "ignore les instructions", "ignore previous", "system message",
		"your prompt", "your instructions",
		"oublie tes", "oublie les instructions", "oublie toutes", "oublie tout ce", "forget your", "forget all", "forget previous",
		"nouvelles instructions", "nouvelle consigne", "new instructions", "ignore all", "ignore the above", "disregard",
		"jailbreak", "mode developpeur", "developer mode", "dan mode", "mode dan", "mode sans restriction", "sans restrictions",
		"agis comme", "act as", "pretend", "fais semblant", "joue le role", "role play", "roleplay",
		"tu n'es plus", "you are now", "repete tout", "repeat everything", "texte ci-dessus", "above text",
	)
)

// offTopicRefusal answers, without the model, what is plainly outside the
// service or an attempt to read its instructions.
func offTopicRefusal(userText string) (string, bool) {
	t := foldAccents(userText)
	if instructionTerms.in(t) {
		return instructionsRefusalMessage, true
	}
	if strongOffTopicTerms.in(t) || pureMath.MatchString(t) || combienMath.MatchString(t) {
		return scopeRefusalMessage, true
	}
	return "", false
}

// ── Is the customer asking about an order they already have? ─

var statusQuestionTerms = newTerms(
	"ou en est", "statut", "suivi", "suivre", "derniere commande", "ma commande", "mes commandes",
	"annul", "livraison", "livree", "recu", "rembours",
)

// isOrderStatusQuestion tells reporting an existing order's status ("ta
// commande est confirmée et payée" — real data) apart from announcing a new
// order that was never placed. The fake-confirmation phrases only apply to the
// latter: they are not checked when the customer is asking about their orders.
func isOrderStatusQuestion(userText string) bool {
	return statusQuestionTerms.in(foldAccents(userText))
}

// ── "Où êtes-vous ?" ────────────────────────────────────────

// The model answered "vous êtes où ?" with the customer's own saved address in
// every run, whatever the prompt said. The restaurants' addresses are data, so
// this question is answered from the list, without the model.
var (
	locationTerms = newTerms(
		"vous etes ou", "ou etes", "vous etes situe", "etes-vous situe", "ou se trouvent vos", "ou sont vos", "ou se situent",
		"adresse du restaurant", "adresse des restaurants", "adresse de vos restaurants", "adresse de vos resto",
		"c'est ou", "cest ou", "ou est votre restaurant", "ou sont les restaurants", "ou se trouvent les restaurants",
	)
	// "où se trouve ma commande ?", "vous livrez où ?" are about an order or a delivery.
	notLocationTerms = newTerms("command", "livr", "colis", "paiement")
)

func isLocationQuestion(userText string) bool {
	t := foldAccents(userText)
	return locationTerms.in(t) && !notLocationTerms.in(t)
}

func locationReply(restaurants []domain.RestaurantSummary) string {
	var b strings.Builder
	b.WriteString("Voici nos restaurants :\n")
	for _, r := range restaurants {
		b.WriteString("- " + describeRestaurant(r) + "\n")
	}
	b.WriteString("\nNotre siège social est au 12 rue de la République, 75011 Paris.")
	return b.String()
}

// ── Output filters ──────────────────────────────────────────

// scopeRefusalMessage replaces any answer that contains code: writing code is
// never part of this assistant's job, and it was observed doing it.
const scopeRefusalMessage = "Je ne peux pas t'aider pour ça : je suis l'assistant Good Food, je réponds sur le menu, les commandes et le service. Une envie de manger ?"

func containsCode(content string) bool { return strings.Contains(content, "```") }

// An order recap is built by the server (formatProposal) from real prices. One
// written by the model has no confirm button and unverified arithmetic.
var recapLine = regexp.MustCompile(`(?m)^\s*[-•*]?\s*\d+\s*[×x]\s+\S.*[—–-]\s*\d+[.,]\d{2}\s*€`)

func looksLikeTextRecap(content string) bool {
	l := strings.ToLower(content)
	if recapLine.MatchString(content) {
		return strings.Contains(l, "livraison à") || strings.Contains(l, "je confirme") || strings.Contains(l, "total :")
	}
	// Prose forms: "Voici le récapitulatif… Le montant total est de 38.97€".
	if !strings.Contains(l, "€") {
		return false
	}
	return strings.Contains(l, "récapitulatif") || strings.Contains(l, "montant total") || strings.Contains(l, "le total est")
}

const (
	noTextRecapReminder = `

Rappel : ne rédige jamais de récapitulatif de commande toi-même (ni liste de plats avec prix, ni total). Si le client veut commander et que tu as le restaurant, le ou les plats et l'adresse, appelle l'outil propose_order maintenant. Sinon, pose UNE seule question courte.`

	textRecapFailedMessage = "Je n'ai pas réussi à préparer le récapitulatif de ta commande. Peux-tu me redire le plat, la quantité et l'adresse ?"
)

// ── "oui" typed under a recap ───────────────────────────────

const proposalHeader = "Voici ce que je te propose :"

const (
	confirmByButtonMessage = "Pour confirmer, clique sur le bouton « Confirmer la commande » juste au-dessus : seul ce bouton passe la commande."
	cancelByButtonMessage  = "Pas de souci : clique sur « Annuler » sous le récapitulatif, ou dis-moi ce que tu veux changer."
)

var (
	confirmWords = unionWords(yesWords, "confirme", "confirmer", "je", "commande", "le", "la", "valide", "valider", "envoie", "lance", "allez", "c", "est", "parti")
	cancelWords  = wordSet("non", "nan", "nope", "annule", "annuler", "stop", "merci", "laisse", "tomber", "finalement", "pas", "cest", "bon", "plus", "je", "veux", "ne")
	cancelStarts = wordSet("non", "nan", "nope", "annule", "annuler", "stop")
)

func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func unionWords(base map[string]bool, extra ...string) map[string]bool {
	m := wordSet(extra...)
	for w := range base {
		m[w] = true
	}
	return m
}

func wordsOf(text string) []string {
	return strings.FieldsFunc(foldAccents(strings.ReplaceAll(text, "'", "")), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
}

// typedAnswerToRecap answers, without the model, a bare "oui" / "non" typed
// right under an order recap: only the buttons place or cancel an order, and
// sending the words to the model produced a pile of identical recaps. Anything
// that says more than yes/no ("oui mais 2 burgers") still goes to the model.
func typedAnswerToRecap(history []domain.Message) (string, bool) {
	n := len(history)
	if n < 2 || history[n-1].Role != domain.RoleUser || history[n-2].Role != domain.RoleAssistant ||
		!strings.HasPrefix(strings.TrimSpace(history[n-2].Content), proposalHeader) {
		return "", false
	}
	words := wordsOf(history[n-1].Content)
	if len(words) == 0 || len(words) > 5 {
		return "", false
	}
	if allIn(words, confirmWords) {
		return confirmByButtonMessage, true
	}
	if cancelStarts[words[0]] && allIn(words, cancelWords) {
		return cancelByButtonMessage, true
	}
	return "", false
}

func allIn(words []string, set map[string]bool) bool {
	for _, w := range words {
		if !set[w] {
			return false
		}
	}
	return true
}

// ── The delivery address is never retyped by the model ──────

// snapAddress returns the customer's saved address when the one the model
// wrote is that address with a typo (a model re-typing "CAUDEBEC" as
// "CAUDEBECA" would send the courier to a wrong place). A genuinely different
// address is left untouched.
func snapAddress(given, saved string) string {
	if strings.TrimSpace(saved) == "" {
		return given
	}
	a, b := addressKey(given), addressKey(saved)
	if a == b {
		return saved
	}
	if abs(len(a)-len(b)) <= 4 && levenshtein(a, b) <= 3 {
		return saved
	}
	return given
}

func addressKey(s string) string {
	var b strings.Builder
	for _, r := range foldAccents(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// ── Which restaurant did the customer already name? ─────────

// customerNamedOneRestaurant reports whether the customer's own words already
// pick exactly one restaurant, in which case offering buttons again is noise.
func customerNamedOneRestaurant(list []domain.RestaurantSummary, customerText string) bool {
	named := 0
	for i := range list {
		words := distinguishingWords(list, i)
		if len(words) == 0 {
			continue
		}
		for _, w := range words {
			if strings.Contains(customerText, w) {
				named++
				break
			}
		}
	}
	return named == 1
}
