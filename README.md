# Assistant Service

Microservice **Go** propriétaire du **chat assistant** (bulle de discussion du
site). Il ne stocke rien : chaque requête reçoit l'historique complet depuis le
front, l'enrichit avec le contexte du client (commandes récentes, menu
consulté) et interroge un modèle de langage.

| | |
|---|---|
| **Langage / techno**    | Go 1.26, chi (routeur), zerolog                    |
| **Base de données**     | Aucune — service sans état                         |
| **Port HTTP**           | `8093`                                             |
| **Fournisseur IA**      | N'importe quel endpoint compatible OpenAI (local ou cloud), ou une passerelle factice hors-ligne |
| **Function calling**    | Oui — l'assistant peut préparer une commande (`propose_order`), jamais l'exécuter lui-même |

---

## Architecture — Clean / Hexagonale

```
cmd/main.go                # Démarrage, injection des dépendances
internal/
├── domain/                # Message, OrderSummary, MenuItemSummary,
│                          # OrderProposal, ToolCall, erreurs typées,
│                          # ports (LLMProvider, OrdersProvider, MenuProvider,
│                          # PaymentMethodsProvider)
├── application/           # Cas d'usage unique : SendMessage (construit le
│                          # contexte, appelle le LLM, résout un éventuel
│                          # appel à l'outil propose_order contre le vrai menu)
├── adapter/
│   ├── http/              # Routeur chi, middleware JWT, DTO
│   ├── llmclient/         # Client HTTP compatible OpenAI (+ function calling)
│   │                      # et FakeProvider (démo)
│   ├── orderclient/       # Client REST vers order-service (JWT transmis)
│   ├── menuclient/        # Client REST vers menu-service (catalogue public)
│   ├── paymentclient/     # Client REST vers payment-service (carte par défaut)
│   └── userclient/        # Client REST vers user-service (adresse par défaut)
└── config/                # Configuration typée depuis l'environnement
```

---

## Fonctionnalités

- **Un seul endpoint de chat** : le front envoie tout l'historique de la
  conversation à chaque message (pas de session côté serveur, pas de base de
  données)
- **Contexte automatique** avant chaque appel au modèle :
  - les **commandes récentes** du client (via `order-service`, avec son
    propre JWT — jamais un compte de service, l'assistant ne voit que ce que
    le client pourrait voir lui-même)
  - le **menu du restaurant consulté**, si le front précise `restaurant_id`
    (via `menu-service`, catalogue public)
  - un échec de l'un ou l'autre appel **dégrade** la réponse (contexte en
    moins) mais **ne fait jamais échouer** la conversation
- **Un seul adaptateur pour local ou cloud** : `AI_BASE_URL` pointe vers
  n'importe quel endpoint qui parle le format « chat completions » façon
  OpenAI — un Ollama local, LM Studio, OpenAI, ou tout autre fournisseur
  compatible. Seuls l'URL, la clé et le nom du modèle changent.
- **Mode démo hors-ligne** : `AI_BASE_URL` vide → une passerelle factice
  (`FakeProvider`) répond avec des messages canned mais contextualisés, pour
  que la bulle de chat reste démontrable sans clé ni serveur IA.
- **Commander par le chat** (« je veux un burger, livre-le au 12 rue de
  Paris ») : l'assistant appelle l'outil `propose_order`, dont les arguments
  sont résolus contre le **vrai** menu (jamais un plat ou un prix inventé par
  le modèle) et renvoyés comme un `OrderProposal` structuré — voir « Commander
  par le chat » plus bas pour le détail et les garanties.
- Historique et longueur de message plafonnés côté serveur (20 messages,
  4000 caractères) pour borner le coût d'un appel.

---

## Garde-fous du prompt système (éviter que l'IA dérive)

Tout le comportement de l'assistant tient dans une seule constante,
`baseSystemPrompt` (`internal/application/send_message.go`), envoyée comme
message `system` avant l'historique à **chaque** appel :

```
Tu es l'assistant Good Food, un service de livraison de repas.
Réponds en français, de façon brève, chaleureuse et utile.

Ton périmètre est strictement limité à Good Food : le menu d'un restaurant,
le statut ou l'historique d'une commande, les codes promo, la livraison, ou
le fonctionnement du service.
Pour toute question hors de ce périmètre (recette de cuisine, actualité,
culture générale, aide en programmation, etc.), décline poliment en une
phrase et recentre la conversation sur ce que tu peux faire — ne réponds
jamais à la question hors-sujet elle-même, même partiellement.
Si tu ne sais pas répondre à une question qui relève bien de ton périmètre,
dis-le simplement et propose de contacter le support.
```

Le contexte commandes/menu (voir « Fonctionnalités ») est **ajouté à la
suite** de ce texte avant chaque appel.

**Tester le garde-fou** (avec un jeton valide) :

```bash
# Doit décliner poliment, sans répondre sur le fond
curl -s -X POST http://localhost:8093/api/chat/messages \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Dis moi une recette de cuisine"}]}'

# Doit répondre normalement — sujet dans le périmètre
curl -s -X POST http://localhost:8093/api/chat/messages \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Où en est ma commande ?"}]}'
```

**Le modifier** : éditer `baseSystemPrompt`, puis `go build` et
`docker compose up -d --build` — aucune autre partie du code n'a besoin de
changer.

> ⚠️ **Ce n'est qu'un garde-fou de prompt, pas un filtre garanti.** Un petit
> modèle local (ex. `llama3.2`) suit généralement la consigne, mais peut
> occasionnellement se laisser convaincre par une reformulation insistante
> ou un jailbreak. Pour un vrai verrou (production, ou un modèle moins
> obéissant), il faudrait ajouter un filtre côté code — mots-clés ou
> classification — en plus du prompt. Suffisant pour cette démo.

---

## Commander par le chat

Le client peut dire « je veux un burger deluxe, livre-le au 12 rue de
Paris » et l'assistant prépare la commande — sans jamais la passer lui-même.

**Ce qu'assistant-service fait, et ce qu'il ne fait pas :**

1. Le modèle appelle l'outil `propose_order` avec les plats (noms libres) et
   l'adresse — seulement si le front a précisé `restaurant_id` (l'outil
   n'est même pas proposé au modèle sinon).
2. `resolveProposal` (`internal/application/send_message.go`) résout **chaque
   plat contre le vrai menu** de `menu-service` — nom exact ou approché,
   jamais un plat qui n'existe pas. Le prix et l'identifiant viennent
   **toujours** du menu réel, jamais de ce que le modèle a pu halluciner. Si
   un plat ne correspond à rien, la conversation continue en texte
   (« je ne trouve pas ce plat, peux-tu préciser ? ») — **pas de proposition
   partielle ou approximative**.
3. Le récapitulatif que le client lit (`formatProposal`) est **généré en Go,
   déterministe** — jamais par le modèle — donc jamais de total ou de plat
   halluciné dans ce que le client voit avant de confirmer.
4. La réponse HTTP inclut un `proposal` structuré (voir « Endpoints »)
   **en plus** du texte — **`assistant-service` ne crée, ne paie et ne
   confirme aucune commande.** C'est le front qui, une fois que le client a
   cliqué sur confirmer, appelle `order-service` par son parcours de
   checkout habituel (identique à une commande passée depuis le panier).

**Fiabilité du function calling avec un modèle local.** Un petit modèle
(`llama3.2:3b`) génère souvent un JSON mal formé pour un schéma imbriqué
(tableau d'objets) — vérifié empiriquement, pas juste en théorie. Deux leviers
ont réglé le problème :
- un modèle plus costaud et réputé fiable sur le function calling
  (`qwen2.5:7b`, ~4.7 Go) ;
- une température basse (`0.1`, contre `0.7` en conversation normale)
  spécifiquement quand des outils sont proposés au modèle — voir
  `llmclient.Client.Complete`.

Avec ces deux réglages, `qwen2.5:7b` a été fiable sur des dizaines d'essais
(plat simple, plusieurs plats avec quantités, adresse manquante, plat
inexistant). `llama3.2:3b` reste très bien pour la conversation normale
(sans outils) si la commande par chat ne t'intéresse pas.

**Deux pièges rencontrés en testant en conditions réelles (pas juste en
théorie), et corrigés :**

- **Le modèle inventait un faux appel d'outil en texte** (« Appelons l'outil :
  `propose_order("Salade César", "votre_adresse")` ») quand aucun restaurant
  n'était en contexte. Cause : le prompt mentionnait `propose_order`
  *inconditionnellement*, même quand l'outil n'était pas réellement proposé
  au modèle dans cet appel API. Fix : le prompt ne parle de `propose_order`
  que lorsque l'outil est effectivement offert (`orderingAvailablePrompt`) ;
  sinon une consigne explicite (`orderingUnavailablePrompt`) lui interdit de
  prétendre en appeler un.
- **« livre chez moi » ne résolvait à rien d'exploitable** — le modèle
  passait `"chez moi"` tel quel comme `delivery_address`. Fix : l'adresse par
  défaut du client (`user-service`, best-effort comme le reste du contexte)
  est maintenant injectée dans le prompt quand une commande est possible,
  avec la consigne de l'utiliser silencieusement (jamais annoncée en texte)
  quand le client dit « chez moi » ou équivalent.

---

## Endpoints

| Méthode | Route | Accès |
|---|---|---|
| POST | `/api/chat/messages` | authentifié |
| GET | `/healthz`, `/readyz` | public (sondes) |

### Requête

```json
{
  "messages": [
    { "role": "user", "content": "Où en est ma commande ?" }
  ],
  "restaurant_id": "6380b937-..."
}
```

`restaurant_id` est optionnel — à fournir quand le client discute depuis la
page d'un restaurant, pour que l'assistant connaisse son menu (et puisse
proposer une commande, voir « Commander par le chat »).

### Réponse

```json
{
  "role": "assistant",
  "content": "Voici ce que je te propose :\n- 1× Burger Deluxe — 12.99€\n...\nJe confirme ?",
  "proposal": {
    "restaurant_id": "6380b937-...",
    "items": [
      { "menu_item_id": "c81d4cfd-...", "menu_item_name": "Burger Deluxe", "quantity": 1, "unit_price_cents": 1299 }
    ],
    "delivery_address": "12 rue de Paris",
    "total_amount_cents": 1299,
    "payment_method": "Visa •••• 4242"
  }
}
```

`proposal` n'est présent que lorsque l'assistant a appelé `propose_order` et
que tous les plats ont été résolus contre le vrai menu (voir « Commander par
le chat ») — absent sur une réponse de conversation normale.

---

## Dépendances

> **Légende** — 🔴 indispensable · 🟠 nécessaire à une fonctionnalité (le
> reste continue de marcher) · 🟡 optionnelle (dégradation silencieuse)

| Dépendance | Type | Conséquence si absente |
|---|---|---|
| **Un endpoint IA compatible OpenAI** (local ou cloud) | 🟡 | Sans `AI_BASE_URL`, le `FakeProvider` répond à la place — mode démo, pas d'échec |
| **order-service** | 🟡 | L'assistant répond sans connaître les commandes du client |
| **menu-service** | 🟡 | L'assistant répond sans connaître le menu — et ne peut plus proposer de commande (l'outil n'est offert au modèle que si un menu a pu être chargé) |
| **payment-service** | 🟡 | Une proposition de commande omet juste le moyen de paiement (purement informatif) |
| **user-service** | 🟡 | « Chez moi » ne se résout plus automatiquement — l'assistant redemande l'adresse en clair |
| **auth-service** | 🟠 | Aucun appel réseau, mais la route exige un jeton valide |

**Aucune base de données.**

### Qui dépend de ce service

`web-app` (la bulle de chat) — si `assistant-service` est arrêté, la bulle
affiche une erreur mais rien d'autre n'est affecté.

---

## Lancement

```bash
docker network create microservices-net   # une seule fois, partagé
cp .env.example .env                      # renseigner JWT_SECRET, et AI_* si besoin
docker compose up -d --build
```

⚠️ `JWT_SECRET` doit être **identique** à celui de `auth-service`.

### Brancher un modèle local avec Ollama (macOS)

**1. Installer et démarrer Ollama.**

```bash
brew install ollama        # si pas déjà fait
brew services start ollama # démarre le service en arrière-plan, au démarrage de la session
# ou, pour le lancer une seule fois sans le garder en arrière-plan :
#   ollama serve
```

Ollama n'a **ni fenêtre ni icône** : c'est un simple processus qui écoute en
silence sur `localhost:11434`. Ne pas voir de fenêtre est normal — voir
« Vérifier que ça tourne vraiment » ci-dessous.

**2. Télécharger un modèle.**

```bash
# Conversation seule (pas de commande par chat) : léger et rapide
ollama pull llama3.2       # ~2 Go

# Conversation + commande par chat (function calling fiable) : recommandé
ollama pull qwen2.5:7b     # ~4.7 Go
```

`llama3.2:3b` répond très bien en conversation normale, mais génère souvent
un JSON mal formé dès qu'un outil (function call) lui est proposé — vérifié
empiriquement, voir « Commander par le chat » plus bas. `qwen2.5:7b` est
fiable sur les deux. D'autres modèles marchent aussi — voir
[ollama.com/library](https://ollama.com/library) (chercher ceux avec la
capacité *tools*). Plus le modèle est gros, plus les réponses sont lentes.

**3. Configurer `assistant-service`.** Dans `.env` :

```
AI_BASE_URL=http://host.docker.internal:11434/v1
AI_API_KEY=
AI_MODEL=qwen2.5:7b
```

`host.docker.internal` est ce qui permet au conteneur d'atteindre Ollama qui
tourne sur la machine hôte (pas dans Docker) — `docker-compose.yml` déclare
`extra_hosts` pour que ça marche aussi bien sur Docker Desktop (Mac/Windows,
automatique) que sur Linux (sinon absent par défaut).

```bash
docker compose up -d      # recharge la config, pas besoin de --build
```

**4. Vérifier que ça tourne vraiment.**

```bash
ollama ps                                    # modèle chargé en mémoire (GPU/CPU), ou rien si inactif depuis 5 min
ps aux | grep ollama                         # le(s) processus, en arrière-plan
curl http://localhost:11434/api/tags         # liste des modèles installés
docker logs assistant-service --tail 5       # doit logger "AI provider configured", pas "FakeProvider"
```

Pour se convaincre que c'est un vrai modèle et pas une réponse en dur : poser
**deux fois la même question ouverte** (ex. « recommande-moi un plat au
hasard ») — une vraie IA varie sa réponse à chaque appel, le `FakeProvider`
du mode démo renvoie toujours le même texte.

Ollama décharge le modèle de la mémoire après ~5 min d'inactivité (normal,
`ollama ps` est alors vide) — il se recharge automatiquement (quelques
secondes) dès la question suivante.

### Variables d'environnement

| Variable | Requis | Description |
|---|---|---|
| `PORT` | non (8093) | Port HTTP |
| `JWT_SECRET` | oui | Secret HS256 partagé avec `auth-service` |
| `AI_BASE_URL` | non | Endpoint compatible OpenAI (ex. `http://ollama:11434/v1` en local, `https://api.openai.com/v1` en cloud). Vide → mode démo. |
| `AI_API_KEY` | non | Clé API pour le endpoint ci-dessus (souvent vide en local) |
| `AI_MODEL` | non (`gpt-4o-mini`) | Nom du modèle à demander à l'endpoint |
| `ORDER_SERVICE_URL` | non | Défaut `http://order-service:8082` |
| `MENU_SERVICE_URL` | non | Défaut `http://menu-service:8085` |

---

## Tests

```bash
go test ./internal/... -cover
go vet ./... && gofmt -l .
```

Couvre la validation des messages, le plafonnage de l'historique, l'injection
du contexte commandes/menu, la dégradation gracieuse quand ce contexte est
indisponible, et le mode démo. Aucune base ni appel réseau requis.
