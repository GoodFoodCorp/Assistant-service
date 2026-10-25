package application

// serviceFacts is everything the assistant is allowed to state about the
// service itself, taken from the product (cart, help center, contact page).
// A small model asked about opening hours, delivery fees or payment will
// invent a confident answer if it has none: the facts below replace invention,
// and the "Tu ne sais PAS" list makes ignorance an acceptable answer.
const serviceFacts = `

Ce que tu sais du service Good Food (et rien d'autre) :
- Livraison : 3,99 € de frais, offerts dès 30 € de commande ; le montant exact s'affiche dans le panier avant de payer.
- Paiement : par carte bancaire, débité à la confirmation de la commande.
- Suivi : la page « Mes commandes » (dans le compte) montre le statut en temps réel.
- Annulation : il n'y a pas de bouton d'annulation pour le client dans l'application. Une commande ne peut être annulée que tant qu'elle n'est pas en préparation : il faut en faire la demande au support (contact@goodfood.fr ou +33 1 23 45 67 89), le plus vite possible.
- Une adresse de livraison ne peut pas être modifiée une fois la commande passée (le support peut voir si c'est encore possible).
- Tu ne peux PAS annuler, modifier ni rembourser une commande, ni changer son adresse : dis-le simplement et oriente vers le support.
- Réservation de table : onglet « Réservations » du site (restaurant, date, couverts). Tu ne peux pas réserver à la place du client.
- Commande programmée : impossible, une commande est préparée dès sa confirmation.
- Personnalisation : tu ne peux transmettre aucune demande particulière (« sans oignons », « bien cuit »). Dis-le, et propose le plat tel qu'il est au menu.
- Siège social : 12 rue de la République, 75011 Paris. Les adresses des restaurants figurent dans la liste des restaurants quand elle est donnée.
- Support : contact@goodfood.fr ou +33 1 23 45 67 89, du lundi au samedi de 9h à 22h (pages « Contact » et « Centre d'aide »).

Tu ne sais PAS : les horaires d'ouverture des restaurants, le délai de livraison, les zones livrées, ni les allergènes, traces ou régimes (halal, vegan, sans gluten…). Sur ces sujets, dis franchement que tu ne sais pas — ne devine jamais, n'invente jamais — et oriente vers le restaurant ou le support.`

// orderStatusLabels explain the order-service status codes in the customer's
// language. The label for CONFIRMED deliberately avoids the word "confirmée":
// the output guard treats "commande confirmée" as a fabricated confirmation.
var orderStatusLabels = map[string]string{
	"PLACED":           "enregistrée, en attente de paiement",
	"PAYMENT_PENDING":  "paiement en cours",
	"CONFIRMED":        "payée et acceptée, pas encore en préparation",
	"IN_PREPARATION":   "en préparation",
	"READY_FOR_PICKUP": "prête, en attente du livreur",
	"IN_DELIVERY":      "en cours de livraison",
	"DELIVERED":        "livrée",
	"CANCELLED":        "annulée",
}

func orderStatusLabel(code string) string {
	if label, ok := orderStatusLabels[code]; ok {
		return code + " — " + label
	}
	return code
}
