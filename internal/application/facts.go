package application

// serviceFacts is everything the assistant is allowed to state about the
// service itself, taken from the product (cart, help center, contact page).
// A small model asked about opening hours, delivery fees or payment will
// invent a confident answer if it has none: the facts below replace invention,
// and the "Tu ne sais PAS" list makes ignorance an acceptable answer.
const serviceFacts = `

Ce que tu sais du service Good Food (et rien d'autre) :
- Livraison : les frais et le délai dépendent du restaurant et de la distance ; ils sont vérifiés pour l'adresse du client quand tu prépares la commande, et s'affichent dans le panier avant de payer.
- Paiement : par carte bancaire, débité à la confirmation de la commande.
- Suivi : la page « Mes commandes » (dans le compte) montre le statut en temps réel.
- Annulation : tant que le restaurant n'a pas commencé la préparation, le client annule lui-même depuis la page de la commande (« Mes commandes »), en indiquant un motif ; il est remboursé intégralement. Ensuite, il faut contacter le support.
- Une adresse de livraison ne peut pas être modifiée une fois la commande passée (le support peut voir si c'est encore possible).
- Toi, tu ne peux PAS annuler, modifier ni rembourser une commande, ni changer son adresse : oriente vers la page de la commande ou le support.
- Codes promo : ils se saisissent dans le panier, champ « Code promo ».
- Réservation de table : onglet « Réservations » du site (restaurant, date, couverts). Tu ne peux pas réserver à la place du client.
- Commande programmée : impossible, une commande est préparée dès sa confirmation.
- Personnalisation : tu ne peux transmettre aucune demande particulière (« sans oignons », « bien cuit »). Dis-le, et propose le plat tel qu'il est au menu.
- Siège social : 12 rue de la République, 75011 Paris. Les adresses des restaurants figurent dans la liste des restaurants quand elle est donnée.
- Support : contact@goodfood.fr ou +33 1 23 45 67 89, du lundi au samedi de 9h à 22h (pages « Contact » et « Centre d'aide »).

Tu ne sais PAS : les horaires d'ouverture des restaurants, ni les allergènes, traces ou régimes (halal, vegan, sans gluten…). Sur ces sujets, dis franchement que tu ne sais pas — ne devine jamais, n'invente jamais — et oriente vers le restaurant ou le support.`

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
