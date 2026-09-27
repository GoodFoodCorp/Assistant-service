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

---

## Architecture — Clean / Hexagonale

```
cmd/main.go                # Démarrage, injection des dépendances
internal/
├── domain/                # Message, OrderSummary, MenuItemSummary,
│                          # erreurs typées, ports (LLMProvider, OrdersProvider, MenuProvider)
├── application/           # Cas d'usage unique : SendMessage (construit le
│                          # contexte, appelle le LLM)
├── adapter/
│   ├── http/              # Routeur chi, middleware JWT, DTO
│   ├── llmclient/         # Client HTTP compatible OpenAI + FakeProvider (démo)
│   ├── orderclient/       # Client REST vers order-service (JWT transmis)
│   └── menuclient/        # Client REST vers menu-service (catalogue public)
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
- Historique et longueur de message plafonnés côté serveur (20 messages,
  4000 caractères) pour borner le coût d'un appel.

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
page d'un restaurant, pour que l'assistant connaisse son menu.

---

## Dépendances

> **Légende** — 🔴 indispensable · 🟠 nécessaire à une fonctionnalité (le
> reste continue de marcher) · 🟡 optionnelle (dégradation silencieuse)

| Dépendance | Type | Conséquence si absente |
|---|---|---|
| **Un endpoint IA compatible OpenAI** (local ou cloud) | 🟡 | Sans `AI_BASE_URL`, le `FakeProvider` répond à la place — mode démo, pas d'échec |
| **order-service** | 🟡 | L'assistant répond sans connaître les commandes du client |
| **menu-service** | 🟡 | L'assistant répond sans connaître le menu du restaurant consulté |
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

```bash
brew install ollama       # si pas déjà fait
ollama pull llama3.2      # ~2 Go, ou tout autre modèle de la bibliothèque Ollama
```

Puis dans `.env` :

```
AI_BASE_URL=http://host.docker.internal:11434/v1
AI_API_KEY=
AI_MODEL=llama3.2
```

`host.docker.internal` est ce qui permet au conteneur d'atteindre Ollama qui
tourne sur la machine hôte (pas dans Docker) — `docker-compose.yml` déclare
`extra_hosts` pour que ça marche aussi bien sur Docker Desktop (Mac/Windows,
automatique) que sur Linux (sinon absent par défaut).

```bash
docker compose up -d      # recharge la config, pas besoin de --build
```

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
