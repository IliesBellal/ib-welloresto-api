# Architecture API — WelloResto
### Document de référence pour les sessions IA

Généré le : 2026-06-18

**Objectif** : référence pour l'implémentation du module Kiosk (`/kiosk/*`). Ce document décrit les conventions réelles du projet, observées dans le code — il ne propose rien (voir `KIOSK_DECISIONS.md` pour les propositions).

---

## 1. Structure du projet

### 1.1 Arborescence (extrait pertinent pour le Kiosk)

```
cmd/api/
  main.go              Point d'entrée : charge la config, MySQL, Zap, appelle SetupRoutes()
  routes.go            Construction de TOUTES les dépendances (constructeurs) + déclaration des routes chi
  tasks.go              Cron / tâches de fond (actuellement désactivées via un `return` précoce)

internal/
  ai/                  Couche d'abstraction LLM (Registry, providers Anthropic/OpenAI, cache Redis)
  config/              Chargement de la config depuis les variables d'environnement (un fichier par domaine, ex. ai.go)
  database/            Connexion MySQL (pool limité à 1 conn — contrainte hébergement Hostinger)
  helpers/             Fonctions utilitaires transverses (génération d'ID, tokens, OTP, normalisation téléphone...)
  infrastructure/      Clients vers services externes : stripe, redis, r2 (R2/S3), websocket, brevo (mail/sms), sms
  logger/              Wrapper Zap + extraction du logger depuis le context
  middleware/          CORS, auth, permissions (RBAC), logging de requêtes, recovery, sanitize
  models/              DTOs partagés, enveloppe de réponse JSON (SendJSON/SendErrorJSON), erreurs sentinelles
  modules/             ~30 modules métier (voir 1.3)
  tasks/                Implémentations des tâches de fond (TasksManager)
  utils/               security (hash HMAC, PIN), dbutils (transactions), receipt
  webhook/             Handlers entrants pour Stripe / Uber Eats / Deliveroo (hors internal/modules)

migrations/            Fichiers SQL bruts, numérotés séquentiellement (NNN_description.up.sql / .down.sql)
docs/                  Documentation approfondie par sujet (ex. DELIVERY_DESIGN.md)
```

### 1.2 Conventions de nommage

| Élément | Convention | Exemple |
|---|---|---|
| Package Go | snake_case, nom du module | `scannorder`, `order_life_cycle`, `delivery_sessions` |
| Fichiers | toujours les mêmes 4 noms par module | `handler.go`, `service.go`, `repository.go`, `models.go` |
| Types exportés | PascalCase, suffixé par son rôle | `Handler`, `Service`, `Repository` (ou `AuthHandler`, `AuthService` si ambiguïté avec un autre module) |
| Constructeurs | `NewXxx(...)` retourne un pointeur (ou une valeur, voir auth) | `NewHandler(s *Service) *Handler` |
| Champs JSON | **snake_case** systématique | `merchant_id`, `order_type`, `is_open` |
| Colonnes SQL | snake_case | `merchant_id`, `created_at`, `is_available_on_sno` |
| Variables / fonctions internes | camelCase | `merchantID`, `computeGetMerchant` |
| Erreurs sentinelles | `ErrXxx`, déclarées dans `internal/models/responses_models.go` | `models.ErrInvalidToken`, `models.ErrTooLateToDeleteOrder` |
| IDs métier générés en Go | `PREFIX-<uuid>` via `helpers.GeneratePrefixedID(prefix)` | `"PAY-3fa85f64-..."` |
| Migrations | `NNN_description.up.sql` / `.down.sql`, séquence globale (pas par module) | `036_merchant_google_maps_monthly.up.sql` |

### 1.3 Organisation des modules

**Par feature, pas par couche technique.** Chaque module sous `internal/modules/<nom>/` est autonome et regroupe ses 4 fichiers (`handler.go`, `service.go`, `repository.go`, `models.go`). Il n'y a pas de dossier `handlers/`, `services/`, `repositories/` séparés au niveau global — chaque module porte sa propre verticale complète (Handler → Service → Repository).

Les modules peuvent dépendre d'autres modules (ex. `scannorder` dépend de `menu`, `orders`, `order_life_cycle`, `delivery_sessions`) — l'injection se fait par pointeur de service/structure concrète passé au constructeur, **pas par interface globale** (sauf exception, voir `auth`).

### 1.4 Comment un nouveau module est branché

Tout se passe dans `cmd/api/routes.go`, fonction `SetupRoutes(log *zap.Logger, mysqlDB *sql.DB, cfg *config.AppConfig) *chi.Mux` :

1. **Import** du package avec un alias si besoin (`scannorder "welloresto-api/internal/modules/scannorder"`).
2. **Construction manuelle de la chaîne de dépendances**, dans l'ordre : `Repository` → `Service` (qui reçoit le repo + les services d'autres modules dont il a besoin) → `Handler` (qui reçoit le service).
   ```go
   scannRepo := scannorder.NewRepository(mysqlDB)
   scannService := scannorder.NewService(cfg.ScanNOrder, scannRepo, menuService, ordersService, stripeManager, redisClient, ordersLifeCycleService)
   scannHandler := scannorder.NewHandler(scannService)
   ```
3. **Déclaration des routes** chi, regroupées par préfixe avec `r.Route("/prefix", func(r chi.Router) {...})`. Le middleware d'auth (`authMiddleware`) et de permissions (`middleware.RequirePermission(...)`) sont ajoutés via `r.Use(...)` à l'intérieur du groupe **si la route est protégée**. Les routes publiques (ScanNOrder, webhooks) n'ont **aucun** `r.Use(authMiddleware)`.
4. Pas de réflexion, pas de registry dynamique de modules : tout est explicite et statique dans `routes.go`. C'est volontairement verbeux mais lisible — un nouveau module Kiosk suivra exactement ce schéma.

Il n'existe **pas** de configuration déclarative séparée (pas de fichier de routing JSON/YAML) : `routes.go` est la seule source de vérité du routing.

---

## 2. Pattern handler / service / repository

Référence : module **`scannorder`** (`internal/modules/scannorder/`), copié intégralement en 2.4.

### 2.1 Handler

- Type : `struct` avec un seul champ, le service (concret, pas une interface) : `type Handler struct { service *Service }`
- Constructeur : `NewHandler(s *Service) *Handler`
- Chaque méthode a la signature standard `http.HandlerFunc` : `func (h *Handler) Xxx(w http.ResponseWriter, r *http.Request)`
- Séquence systématique dans chaque méthode :
  1. `ctx := r.Context()` (+ `log := logger.FromContext(ctx)` si logging nécessaire)
  2. Extraction des paramètres : `chi.URLParam(r, "xxx")` pour le path, `r.URL.Query().Get("xxx")` pour la query, `json.NewDecoder(r.Body).Decode(&req)` pour le body
  3. Si erreur de parsing body → `models.SendJSON(w, http.StatusBadRequest, "<module>", "<fn>", map[string]string{"error": "invalid_body", "message": err.Error()})` puis `return`
  4. Appel du service : `resp, err := h.service.Xxx(ctx, ...)`
  5. Si erreur → soit `models.SendErrorJSON(w, "<module>", "<fn>", err)` (mapping erreurs sentinelles, **pattern recommandé/le plus récent**), soit l'ancien pattern manuel `models.SendJSON(w, http.StatusInternalServerError, ..., map[string]string{"error": err.Error()})` (legacy, encore présent dans le code mais à éviter pour du nouveau code)
  6. Succès → `models.SendJSON(w, http.StatusOK, "<module>", "<fn>", resp)`
- Le handler ne contient **aucune logique métier** ni accès SQL — uniquement parsing HTTP + délégation.

### 2.2 Service

- Type : `struct` exportée, champs = dépendances (repo du module + services d'autres modules + clients externes + config)
- Constructeur : `NewService(...)` reçoit tout en paramètres explicites (pas de struct d'options, pas de wire/DI framework)
- Pas d'interface `Service` définie pour scannorder (le handler référence directement `*Service`) — **exception** : le module `auth` définit `AuthService` comme interface (`service_interface.go`) car le middleware d'auth doit pouvoir l'injecter de façon découplée (testabilité). Suivre ce pattern uniquement si un autre composant transverse (middleware) doit consommer le service.
- La logique métier vit ici : validations, orchestration multi-repository/multi-module, calculs (ex. `CustomerInDeliveryZone`, `validateAndCleanPricingPayload`), application du cache Redis (pattern cache-aside, voir 2.5).
- Convention de retour : `(*XxxResponse, error)` où `XxxResponse` porte presque toujours un champ `Status string` interne (`"success"`, `"qr_code_expired"`, etc.) **en plus** de l'erreur Go — un statut métier "doux" n'est pas forcément une erreur Go (ex. QR code expiré → `Status: "qr_code_expired"`, `error == nil`).

### 2.3 Repository

- Type : `struct` avec un seul champ `database *sql.DB`
- Constructeur : `NewRepository(db *sql.DB) *Repository`
- Une méthode par requête, jamais de query builder ni d'ORM : SQL brut avec `?` comme placeholders (driver MySQL), exécuté via `db.QueryRowContext`, `db.QueryContext`, `db.ExecContext`
- Toujours passer par `dbutils.GetDB(ctx, r.database)` plutôt que `r.database` directement — cette fonction retourne soit la connexion globale, soit la transaction (`*sql.Tx`) en cours si une transaction est ouverte dans le contexte (voir 6.1 / `dbutils.RunInTx`)
- Gestion des erreurs SQL :
  - `sql.ErrNoRows` est systématiquement traité explicitement et transformé en `nil, nil` (ressource absente, pas une erreur) **ou** en erreur sentinelle métier (`models.ErrUserNotFound`) selon le contexte attendu par l'appelant
  - Toute autre erreur SQL est simplement propagée (`return nil, err`), sans wrapping particulier dans la plupart des cas legacy ; le nouveau code (sécurité prix, voir `GetProductPricesForSNO`) wrap avec `fmt.Errorf("...: %w", err)` pour garder le contexte
  - `defer rows.Close()` systématique après `QueryContext`
  - `rows.Err()` vérifié après la boucle `for rows.Next()` dans le code récent (pas systématique dans le code plus ancien)

### 2.4 Fichiers de référence — `scannorder` (templates à dupliquer)

#### handler.go

```go
package scannorder

import (
	"encoding/json"
	"net/http"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

func (h *Handler) GetMerchant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	qr := chi.URLParam(r, "merchant_slug")

	merchantData, err := h.service.GetMerchant(ctx, qr)
	if err != nil {
		log.Error("service error" + err.Error())
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_merchant", map[string]string{"error": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_merchant", merchantData)
}

func (h *Handler) GetPricingSNO(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "get_pricing_sno", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	qr := chi.URLParam(r, "merchant_slug")
	req.QRCode = qr
	resp, err := h.service.GetPricingSNO(ctx, &req)
	if err != nil {
		log.Error("SNO pricing failed", zap.Error(err))
		models.SendJSON(w, http.StatusInternalServerError, "scannorder", "get_pricing_sno", map[string]string{"error": err.Error(), "message": err.Error()})
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "get_pricing_sno", resp)
}

func (h *Handler) CreateOrderSNO(w http.ResponseWriter, r *http.Request) {
	var req models.PricingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "scannorder", "create_order_sno", map[string]string{"error": "invalid_body", "message": err.Error()})
		return
	}

	qr := chi.URLParam(r, "merchant_slug")
	req.QRCode = qr

	create_order, err := h.service.CreateOrderSNO(r.Context(), &req)
	if err != nil {
		models.SendErrorJSON(w, "scannorder", "create_order_sno", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "scannorder", "create_order_sno", create_order)
}

func (h *Handler) CheckDeliveryZone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	var req DeliveryCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error("Invalid request body", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "check_delivery_zone", err)
		return
	}

	qrCode := chi.URLParam(r, "merchant_slug")
	log.Info("CheckDeliveryZone", zap.String("merchant_slug", qrCode), zap.Float64("lat", req.Lat), zap.Float64("lng", req.Lng))

	resp, err := h.service.CheckDeliveryZone(ctx, qrCode, &req)
	if err != nil {
		log.Error("CheckDeliveryZone service error", zap.Error(err))
		models.SendErrorJSON(w, "scannorder", "check_delivery_zone", err)
		return
	}

	statusCode := http.StatusOK
	if resp.Status == "out_of_delivery_zone" {
		statusCode = http.StatusUnprocessableEntity // 422
	}

	models.SendJSON(w, statusCode, "scannorder", "check_delivery_zone", resp)
}

// ... (GetMenu, GetOrderSNO, CancelOrderSNO, GetBrand, GetLoyaltyPrograms, GetSlots,
//      GetDiscounts, GetUpsell, GetProduct suivent rigoureusement le même schéma :
//      ctx → parse params/body → appel service → SendJSON/SendErrorJSON)
```

> Note : le fichier réel contient 13 handlers suivant ce schéma exact. Voir `internal/modules/scannorder/handler.go` (276 lignes) pour la version complète. On observe deux générations de gestion d'erreur côté handler : `models.SendJSON(..., map[string]string{"error": err.Error()})` (ancien) et `models.SendErrorJSON(w, module, fn, err)` (nouveau, à utiliser pour le Kiosk).

#### service.go (extrait représentatif — constructeur, cache-aside, validation anti-fraude, switch métier)

```go
package scannorder

type Service struct {
	repo                   *Repository
	menu                   *menu.MenuService
	orderingService        *orders.OrdersService
	orderLifeCycleSvc      *order_life_cycle.OrdersLifeCycleService
	deliverySessionService delivery_sessions.DeliverySessionsService
	StripeManager          *stripeclient.StripeManager
	cfg                    config.ScanNOrderConfig
	redis                  *redis.Client
}

func NewService(config config.ScanNOrderConfig, r *Repository, m *menu.MenuService, o *orders.OrdersService, manager *stripeclient.StripeManager, redis *redis.Client, orderLifeCycleSvc *order_life_cycle.OrdersLifeCycleService) *Service {
	return &Service{cfg: config, repo: r, menu: m, orderingService: o, StripeManager: manager, redis: redis, orderLifeCycleSvc: orderLifeCycleSvc}
}

// Pattern cache-aside Redis (lecture) : utilisé pour toutes les données peu volatiles
func (s *Service) GetMerchant(ctx context.Context, qr string) (*MerchantResponse, error) {
	if s.redis == nil {
		return s.computeGetMerchant(ctx, qr)
	}
	cacheKey := models.ScannorderMerchant + qr
	cached, found := s.redis.Get(ctx, cacheKey)
	if found {
		var merchant MerchantResponse
		if err := json.Unmarshal([]byte(cached), &merchant); err == nil {
			return &merchant, nil
		}
	}
	merchant, err := s.computeGetMerchant(ctx, qr)
	if err != nil {
		return nil, err
	}
	if merchant == nil {
		return nil, nil
	}
	if serialized, err := json.Marshal(merchant); err == nil {
		s.redis.Set(ctx, cacheKey, string(serialized), models.ScannorderMerchantTTL)
	}
	return merchant, nil
}

// 🔒 Sécurité critique : les prix envoyés par le client ne sont JAMAIS utilisés tels quels.
// Tous les prix sont récupérés depuis la base avant tout calcul.
func (s *Service) validateAndCleanPricingPayload(ctx context.Context, req *models.PricingRequest, merchant *models.MerchantRow) error {
	// 1. Collecte des product_id / option_id envoyés par le client
	// 2. Récupération des prix officiels en base (GetProductPricesForSNO, GetConfigurationOptionPricesForSNO)
	// 3. Si un ID envoyé par le client n'existe pas en base -> erreur (tentative de fraude probable, loggée en zap.Warn)
	// 4. Écrasement des prix du payload avec les valeurs officielles avant tout calcul de pricing
	...
}

func (s *Service) CreateOrderSNO(ctx context.Context, req *models.PricingRequest) (models.CreateOrderResult, error) {
	merchant, err := s.repo.GetMerchantByQR(ctx, req.QRCode)
	// ... vérif merchant trouvé, vérif POS ouvert (sauf type "IN")
	switch order.OrderType {
	case "IN":
		// client depuis QR, booking éventuel, approbation automatique
	case "DELIVERY":
		// vérif zone de livraison, fallthrough vers TAKE_AWAY
	case "TAKE_AWAY":
		// résolution client par téléphone, paiement en ligne, approbation PENDING
	}
	// Pricing (re-vérifié et sécurisé) -> création commande via orderLifeCycleSvc.CreateOrder
	// -> si paiement requis : création de la session Stripe Checkout
}
```

> Fichier réel : 1222 lignes, 20+ méthodes publiques. Voir `internal/modules/scannorder/service.go`.

#### repository.go (extrait représentatif)

```go
package scannorder

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

func (r *Repository) GetMerchantByQR(ctx context.Context, qr string) (*models.MerchantRow, error) {
	db := dbutils.GetDB(ctx, r.database)

	query := `
	SELECT m.id, m.fullName, m.address, m.lat, m.lng, m.timezone, m.merchantTel, ...
	FROM   qrcodes qr
	      INNER JOIN merchant m on m.id = qr.merchant_id
	      LEFT JOIN stripe_accounts sa on sa.merchant_id = m.id
	      INNER JOIN scannorder_settings snos on snos.merchant_id = m.id
	      INNER JOIN merchant_parameters mp on mp.merchant_id = m.id
	      ...
	WHERE qr.code = ?`

	row := models.MerchantRow{}
	err := db.QueryRowContext(ctx, query, qr).Scan(&row.MerchantID, &row.FullName, ...)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// "not found" -> nil, nil (pas une erreur)
func (r *Repository) GetDeliverySessionByOrderID(ctx context.Context, orderID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)
	query := `SELECT dso.order_id, dso.delivery_session_id FROM delivery_session ds
	          INNER JOIN delivery_session_order dso ON ds.id = dso.delivery_session_id
	          INNER JOIN orders o ON o.order_id = dso.order_id WHERE o.order_id = ?`
	row := db.QueryRowContext(ctx, query, orderID)
	var dsID string
	var oID string
	err := row.Scan(&oID, &dsID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &dsID, nil
}

// Pattern "IN clause" dynamique avec placeholders construits manuellement
func (r *Repository) GetProductPricesForSNO(ctx context.Context, merchantID string, productIDs []string) (map[string]map[string]int64, error) {
	if len(productIDs) == 0 {
		return make(map[string]map[string]int64), nil
	}
	db := dbutils.GetDB(ctx, r.database)
	placeholders := ""
	args := []interface{}{merchantID}
	for i, id := range productIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(`SELECT p.product_id, p.price, COALESCE(p.price_delivery, p.price), COALESCE(p.price_take_away, p.price)
	                       FROM products p WHERE p.merchant_id = ? AND p.product_id IN (%s)`, placeholders)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query product prices: %w", err)
	}
	defer rows.Close()
	result := make(map[string]map[string]int64)
	for rows.Next() {
		var productID string
		var price, priceDelivery, priceTakeaway int64
		if err := rows.Scan(&productID, &price, &priceDelivery, &priceTakeaway); err != nil {
			return nil, fmt.Errorf("failed to scan product price: %w", err)
		}
		result[productID] = map[string]int64{"price": price, "price_delivery": priceDelivery, "price_take_away": priceTakeaway}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error during product price fetch: %w", err)
	}
	return result, nil
}
```

> Fichier réel : 985 lignes, ~20 méthodes. Voir `internal/modules/scannorder/repository.go`.

### 2.5 Injection de dépendances

Pas de framework (pas de wire, pas de fx). Tout est construit à la main dans `routes.go`, dans l'ordre topologique des dépendances. Les services reçoivent des **pointeurs de struct concrets** d'autres modules (pas d'interfaces), sauf le cas `AuthService` (interface, car consommé par le middleware transverse `middleware.Auth(service AuthService)`).

---

## 3. Format des réponses JSON

### 3.1 Enveloppe de succès — `models.SendJSON`

```go
type HandlerDefaultResponse struct {
	ID   string      `json:"id"`
	Data interface{} `json:"data"`
}

func SendJSON(w http.ResponseWriter, statusCode int, module string, fnName string, data interface{}) {
	result := HandlerDefaultResponse{
		ID:   module + "." + fnName,
		Data: data,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(result)
}
```

Toute réponse (succès **et** erreur "métier") passe par cette enveloppe : `{"id": "<module>.<fonction>", "data": <payload>}`. Le `id` n'est pas un identifiant de ressource — c'est un identifiant de l'**endpoint appelé**, utile pour le debug/les logs côté client.

### 3.2 Enveloppe d'erreur — `models.SendErrorJSON`

```go
func SendErrorJSON(w http.ResponseWriter, module string, fnName string, err error) {
	status := http.StatusInternalServerError
	errorStatus := "internal_server_error"
	errorMsg := "internal_server_error"

	switch {
	case errors.Is(err, ErrInvalidRequestBody):
		status = http.StatusBadRequest
		errorStatus = "invalid_request_body"
		errorMsg = "The request body is invalid or malformed."
	case errors.Is(err, ErrMissingResourceID):
		status = http.StatusBadRequest
		errorStatus = "missing_resource_id"
		...
	// ... une branche par erreur sentinelle déclarée dans ce fichier
	}

	SendJSON(w, status, module, fnName, map[string]string{"error": errorStatus, "message": errorMsg})
}
```

`SendErrorJSON` mappe une **erreur sentinelle** Go (`errors.Is`) vers un code HTTP + un `error` slug stable + un message lisible, puis délègue à `SendJSON`. C'est le pattern à privilégier pour tout nouveau code (Kiosk inclus) — il évite de dupliquer le mapping erreur→HTTP dans chaque handler.

### 3.3 Distinguer erreur métier vs technique

- **Erreur métier** = sentinelle déclarée dans `internal/models/responses_models.go` (`var ErrXxx = errors.New("xxx")`), mappée explicitement dans `SendErrorJSON` vers un code 4xx précis.
- **Erreur technique** (SQL non prévu, panique réseau, etc.) = ne matche aucun `errors.Is`, tombe dans le `default` → 500 `internal_server_error`. Le détail réel de l'erreur n'est **jamais** renvoyé au client dans ce cas (seulement loggé côté serveur via `zap`).
- Dans le code legacy (ancien pattern, encore présent), certains handlers renvoient `err.Error()` brut au client (`map[string]string{"error": err.Error()}`) — **à éviter** pour le Kiosk : ça fuite des détails internes.

### 3.4 Convention de nommage des champs

snake_case partout côté JSON exposé (`merchant_id`, `order_type`, `is_open`, `delivery_fees_limit`), même quand les champs Go sont en PascalCase avec tag `json:"snake_case"` explicite.

### 3.5 Exemples réels

**Succès simple** (`GET /scannorder/{merchant_slug}` → `scannorder.GetMerchant`) :
```json
{
  "id": "scannorder.get_merchant",
  "data": {
    "status": "success",
    "merchant": {
      "merchant_id": "abc123",
      "business_name": "Le Bistrot",
      "phone": "+33612345678",
      "currency": "EUR",
      "status": { "is_open": true, "open_hours": [...] },
      "address": { "address": "12 rue de Paris", "lat": 48.85, "lng": 2.35 },
      "order_types": { "takeaway_enabled": true, "delivery_enabled": false, "in_enabled": true },
      "payment_types": { "online": true, "cash": false }
    }
  }
}
```

**Liste** (`GET /scannorder/{merchant_slug}/discounts` → `scannorder.GetDiscounts`) :
```json
{
  "id": "scannorder.get_discounts",
  "data": {
    "discounts": [
      { "discount_id": "d1", "discount_name": "Happy Hour", "discount_value": 10, "discount_unit": "PERCENT", "available": true }
    ]
  }
}
```

**Erreur** (body invalide sur `POST /scannorder/{merchant_slug}/orders`) :
```json
{
  "id": "scannorder.create_order_sno",
  "data": { "error": "invalid_body", "message": "EOF" }
}
```
(statut HTTP 400 — ancien pattern manuel observé dans `scannorder.CreateOrderSNO` pour ce cas précis ; le pattern recommandé pour une nouvelle erreur métier est `SendErrorJSON` avec une sentinelle dédiée.)

---

## 4. Middleware et authentification

### 4.1 Principe général

**Il n'y a pas de JWT dans ce projet.** L'authentification repose sur un **token opaque permanent**, stocké en base (`users_rights.token`), généré une fois et réutilisé indéfiniment (pas d'expiration, pas de refresh). Le token est mis en cache Redis (clé `models.UserCachePrefix + token`, TTL `models.UserCacheTTL`) pour éviter une requête SQL à chaque appel — avec une protection anti "cache stampede" via `singleflight.Group`.

### 4.2 `middleware.Auth` (extraction + validation + injection dans le contexte)

```go
type contextKey string

const userContextKey contextKey = "authenticatedUser"

type AuthService interface {
	GetUserByToken(ctx context.Context, token string) (*auth.UserLoginRow, error)
	UpdateMFAStatus(ctx context.Context, userID string, status string) error
	IsMFAVerificationRequired(ctx context.Context, user *auth.UserLoginRow) bool
}

func Auth(service AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token manquant"}`, http.StatusUnauthorized)
				return
			}

			token := authHeader
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}
			token = strings.TrimSpace(token)
			if token == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"format token invalide"}`, http.StatusUnauthorized)
				return
			}

			user, err := service.GetUserByToken(r.Context(), token)
			if err != nil || user == nil {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token invalide ou expiré"}`, http.StatusUnauthorized)
				return
			}

			isBackoffice := r.Header.Get("X-App-Source") == "backoffice"
			if isBackoffice && service.IsMFAVerificationRequired(r.Context(), user) {
				if r.URL.Path != "/auth/verify" {
					service.UpdateMFAStatus(r.Context(), user.UserID, models.MFAStatusPending)
					SetCORSHeaders(w, r)
					models.SendErrorJSON(w, "auth", "login", models.ErrMFARequired)
					return
				}
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(r *http.Request) *auth.UserLoginRow {
	user, _ := r.Context().Value(userContextKey).(*auth.UserLoginRow)
	return user
}

func UserFromContext(ctx context.Context) (*auth.UserLoginRow, error) {
	user, ok := ctx.Value(userContextKey).(*auth.UserLoginRow)
	if !ok || user == nil {
		return nil, ErrUnunauthenticated
	}
	return user, nil
}
```

Points clés pour le Kiosk :
- Le user authentifié est récupéré via `middleware.GetUser(r)` (handler) ou `middleware.UserFromContext(ctx)` (service, retourne une erreur si absent).
- `merchant_id` n'est **jamais** un paramètre de requête sur les routes protégées : il vient toujours de `user.MerchantID` (le user authentifié), garantissant le scoping multi-tenant. **Le Kiosk devra reproduire ce principe avec son propre "user" (le device/kiosk authentifié), pas avec un `merchant_id` passé en clair par le client.**
- Le header `X-App-Source: backoffice` déclenche la vérification MFA — mécanisme spécifique au back-office web, non pertinent pour Kiosk a priori.

### 4.3 `middleware.RequirePermission` / `PermissionFunc`

```go
type PermissionFunc func(user *auth.UserLoginRow) bool

func RequirePermission(permissions ...PermissionFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				SetCORSHeaders(w, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			for _, hasPermission := range permissions {
				if !hasPermission(user) {
					if !IsEmailVerified(user) {
						renderError(w, r, "EMAIL_VERIFICATION_REQUIRED", http.StatusForbidden)
						return
					}
					if user.Rights.Admin && !IsTelVerified(user) {
						renderError(w, r, "TEL_VERIFICATION_REQUIRED", http.StatusForbidden)
						return
					}
					renderError(w, r, "access_denied", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

`PermissionFunc` est une simple fonction `func(*auth.UserLoginRow) bool`. Les permissions concrètes sont définies dans `internal/middleware/permissions.go`, ex. :

```go
func HasMenuAccess(user *auth.UserLoginRow) bool {
	return user.HasMenuAccess() || user.IsAdmin()
}
```

Combinateurs disponibles : `AnyOf(perms...)` (OR), `AllOf(perms...)` (AND — équivalent au comportement par défaut de `RequirePermission` qui prend déjà plusieurs `PermissionFunc` en AND).

Usage typique dans `routes.go` :
```go
r.Route("/menu", func(r chi.Router) {
	r.Use(authMiddleware)
	r.Use(middleware.RequirePermission(middleware.HasMenuAccess))
	r.Get("/", menuH.GetMenu)
})
```

### 4.4 Extraction du merchant

Deux mécanismes coexistent selon le type de route :
1. **Routes protégées par token utilisateur** (back-office, apps mobiles staff) : `merchant_id` vient de `user.MerchantID` injecté par `middleware.Auth`.
2. **Routes publiques ScanNOrder** (`/scannorder/{merchant_slug}/...`) : pas d'utilisateur authentifié — le `merchant_slug` est un **QR code** (`chi.URLParam(r, "merchant_slug")`) résolu en `merchant_id` via `repository.GetMerchantByQR` / `GetMerchantIDAndTZFromQR`. Le QR code a une durée de validité de 2h (`creationTime` vérifié dans `computeGetMerchant`).

### 4.5 Organisation public vs protégé dans `SetupRoutes`

- Routes protégées : groupées avec `r.Use(authMiddleware)` (et éventuellement `r.Use(middleware.RequirePermission(...))`) en tête du `r.Route(...)`.
- Routes publiques : **aucun** `r.Use(authMiddleware)` dans le groupe. C'est le cas de `/scannorder` (commande client final via QR code) et des `/webhooks/*` (signature vérifiée différemment, par provider).
- Il n'y a pas de liste blanche/noire centralisée — la décision est prise route par route, visuellement, dans `routes.go`.

---

## 5. Gestion des erreurs

### 5.1 Erreurs sentinelles

Toutes déclarées comme variables `var ErrXxx = errors.New("slug_snake_case")` dans `internal/models/responses_models.go` (fichier central, ~80 lignes de déclarations). Pas de hiérarchie de types d'erreurs custom (`type X struct{}` implémentant `error`) sauf cas spécifiques nécessitant des données structurées, ex. `auth.PINLockoutError` :

```go
type PINLockoutError struct {
	DelaySeconds int
}
func (e *PINLockoutError) Error() string {
	return fmt.Sprintf("pin_locked: retry in %ds", e.DelaySeconds)
}
```
détecté côté handler via `errors.As(err, &lockoutErr)`.

### 5.2 Pattern de comparaison

- `errors.Is(err, models.ErrXxx)` pour les sentinelles simples — utilisé massivement dans `SendErrorJSON`.
- `errors.As(err, &typedErr)` pour les erreurs porteuses de données (`PINLockoutError`).
- Pas de `panic`/`recover` métier — un middleware `recovery.go` existe en filet de sécurité au niveau HTTP global, pas comme pattern de contrôle de flux applicatif.

### 5.3 SQL "not found" → 404

Le repository transforme `sql.ErrNoRows` en `nil, nil` (ressource absente, comportement attendu) **ou** en erreur sentinelle explicite selon le besoin de l'appelant :
```go
err := db.QueryRowContext(ctx, query, qr).Scan(&merchantID)
if err == sql.ErrNoRows {
	return nil, nil // pas trouvé, pas une erreur technique
}
```
Le service décide alors quoi faire de ce `nil` (souvent un statut métier `"qr_code_expired"` plutôt qu'un 404 HTTP strict, car ScanNOrder n'est pas du REST orienté ressource classique). Pour des endpoints plus "ressource" (ex. `GetUserByToken`), un `nil` se traduit en `models.ErrUserNotFound` / `models.ErrInvalidToken`, mappé en 401 par `SendErrorJSON`.

### 5.4 Logging

`go.uber.org/zap`, récupéré depuis le contexte via `logger.FromContext(ctx)` (injecté par le middleware de logging au niveau global). Niveaux utilisés : `log.Info(...)`, `log.Warn(...)`, `log.Error(...)`, `log.Debug(...)`. Convention zap structurée : `zap.String("key", val)`, `zap.Error(err)`. Le code plus ancien mélange parfois la concaténation de strings (`log.Error("xxx: " + err.Error())`) — préférer la forme structurée `zap.Error(err)` pour le nouveau code Kiosk.

---

## 6. Base de données

### 6.1 Driver et accès

`database/sql` natif, driver MySQL (`github.com/go-sql-driver/mysql` implicite). **Aucun ORM**, aucun query builder — SQL brut écrit à la main partout. Pool de connexions volontairement restreint à **1 connexion ouverte + 1 idle**, durée de vie 3 minutes (`internal/database/mysql.go`, contrainte d'hébergement Hostinger) — implication directe : **toute requête longue ou bloquante dégrade immédiatement tout le service**, donc prudence avec des requêtes Kiosk lourdes ou des transactions longues.

Les transactions passent par `dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {...})` — le repository utilise ensuite `dbutils.GetDB(ctx, r.database)` qui retourne automatiquement le `*sql.Tx` si le contexte en contient un, sinon le `*sql.DB` global. Ce pattern permet à un repository d'être appelé indifféremment en transaction ou hors transaction sans changer son code.

### 6.2 Convention de tables/colonnes

- Tables : snake_case, souvent au singulier pour les entités centrales historiques (`merchant`, `customer`) mais au pluriel pour les modules plus récents (`orders`, `discounts`, `delivery_sessions`, `kiosk...` à créer suivrait le pluriel)
- Colonnes : snake_case (`merchant_id`, `created_at`, `is_available_on_sno`)
- Clé primaire : **mixte selon l'ancienneté du module**
  - Tables historiques : `id` ou `<entité>_id`, souvent `VARCHAR` (UUID applicatif généré côté Go, ex. `merchant.id`, `products.product_id`)
  - Tables plus récentes orientées séquentielles (`orders.order_id`) : `INT UNSIGNED AUTO_INCREMENT`, avec ajout récent d'un `public_id VARCHAR(45)` opaque (`RANDOM_BYTES`/`UUID()`) pour ne pas exposer l'ID séquentiel aux clients externes (migration 033)
  - Nouvelles tables internes pures (logs, positions) : `BIGINT UNSIGNED AUTO_INCREMENT`
- Pas de FK systématique : le projet **n'ajoute pas de contrainte FK** vers les tables historiques (commentaire explicite dans la migration 032 : *"no FK to historical tables anywhere in this codebase"*). Les FK sont utilisées localement entre tables d'un même module récent si jugé sûr, mais ce n'est pas une règle stricte.
- Timestamps : `DATETIME`, souvent `DEFAULT CURRENT_TIMESTAMP` à la création ; pas de convention uniforme `created_at`/`updated_at` sur toutes les tables (certaines tables n'en ont pas du tout).
- Soft delete : convention **`enabled` (BOOLEAN/TINYINT, 1 = actif)**, pas `deleted_at`. Vu sur `customer.enabled`, `availabilities.enabled`, `qrcodes.deleted` (exception nommée différemment), `discounts.enabled`. Pour une nouvelle table Kiosk, utiliser `enabled BOOLEAN NOT NULL DEFAULT TRUE` est la convention dominante.

### 6.3 Migrations

- Dossier `migrations/`, fichiers `NNN_description.up.sql` + `NNN_description.down.sql`, numérotation séquentielle **globale** (pas par module).
- Pas d'outil de migration automatisé identifié dans le repo (pas de `golang-migrate` binaire visible dans les dépendances utilisées) — exécution manuelle supposée.
- Le fichier `.up.sql` contient souvent un **bloc de commentaires de contexte** en tête (pourquoi cette migration, quelles incertitudes `[A VERIFIER]`, liens vers `docs/*.md`) — convention à reprendre pour les migrations Kiosk.

Exemple complet (migration 032, `CREATE TABLE` + `ALTER TABLE` mixés) :
```sql
-- Delivery module (driver app): per-stop FSM, current stop pointer, position
-- history, delivery instructions.
--
-- [A VERIFIER EN BASE] run `SHOW CREATE TABLE orders` ... and adjust this type
-- if order_id is not an INT UNSIGNED. No FK constraint added, consistent with
-- the existing delivery_session_order.order_id column (no FK to historical
-- tables anywhere in this codebase).

ALTER TABLE delivery_session_order ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'pending';
ALTER TABLE delivery_session_order ADD COLUMN arrived_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN delivered_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN failed_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN canceled_at DATETIME NULL DEFAULT NULL;
ALTER TABLE delivery_session_order ADD COLUMN fail_reason VARCHAR(255) NULL DEFAULT NULL;

ALTER TABLE delivery_session ADD COLUMN current_order_id INT UNSIGNED NULL DEFAULT NULL;
CREATE INDEX idx_delivery_session_current_order ON delivery_session (current_order_id);

ALTER TABLE users ADD COLUMN last_position_at DATETIME NULL DEFAULT NULL;

CREATE TABLE delivery_position (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id VARCHAR(64) NOT NULL,
    delivery_session_id INT UNSIGNED NOT NULL,
    lat DECIMAL(10,7) NOT NULL,
    lng DECIMAL(10,7) NOT NULL,
    heading FLOAT NULL DEFAULT NULL,
    accuracy FLOAT NULL DEFAULT NULL,
    speed FLOAT NULL DEFAULT NULL,
    recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_delivery_position_session (delivery_session_id, recorded_at),
    KEY idx_delivery_position_user (user_id, recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

Exemple de migration minimaliste, ajout d'opaque ID (033) :
```sql
ALTER TABLE orders
    ADD COLUMN public_id VARCHAR(45) NULL,
    ADD UNIQUE INDEX idx_orders_public_id (public_id);
```

**Style retenu pour les futures tables Kiosk** : `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, clé primaire `BIGINT UNSIGNED AUTO_INCREMENT` pour les nouvelles tables sans besoin d'ID applicatif partagé avec un autre système, `VARCHAR` pour les IDs applicatifs déjà utilisés ailleurs (ex. `merchant_id`), pas de FK vers les tables historiques mais FK possible entre tables Kiosk elles-mêmes si cela apporte une garantie d'intégrité utile.

---

## 7. Module `scannorder` — référence Kiosk

### 7.1 Routes (toutes publiques, aucun `authMiddleware`)

```go
r.Route("/scannorder", func(r chi.Router) {
	r.Get("/brands/{brand_slug}", scannHandler.GetBrand)
	r.Get("/{merchant_slug}", scannHandler.GetMerchant)
	r.Get("/{merchant_slug}/slots", scannHandler.GetSlots)
	r.Get("/{merchant_slug}/menu", scannHandler.GetMenu)
	r.Get("/{merchant_slug}/loyalty_programs", scannHandler.GetLoyaltyPrograms)
	r.Get("/{merchant_slug}/discounts", scannHandler.GetDiscounts)
	r.Get("/{merchant_slug}/upsell", scannHandler.GetUpsell)
	r.Post("/{merchant_slug}/pricing", scannHandler.GetPricingSNO)
	r.Post("/{merchant_slug}/delivery/check", scannHandler.CheckDeliveryZone)
	r.Post("/{merchant_slug}/orders", scannHandler.CreateOrderSNO)
	r.Post("/{merchant_slug}/create", scannHandler.CreateOrderSNO) // TO BE DELETED (legacy)
	r.Get("/{merchant_slug}/orders/{order_id}", scannHandler.GetOrderSNO)
	r.Get("/{merchant_slug}/products/{product_id}", scannHandler.GetProduct)
	r.Delete("/{merchant_slug}/orders/{order_id}", scannHandler.CancelOrderSNO)
})
```
`merchant_slug` est en réalité le **code QR**, pas un slug lisible — résolu en `merchant_id` à chaque appel (avec cache Redis sur le merchant complet).

### 7.2 `CreateOrderSNO` — flux détaillé

1. Parse `models.PricingRequest` depuis le body JSON, injecte `req.QRCode` depuis le path.
2. Résout le `merchant` via le QR code ; si absent → `Status: "qr_code_expired"` (pas une erreur HTTP, un statut métier 200 avec `status` différent de `"success"`).
3. Si `order_type != "IN"` et pas d'heure programmée (`EstimatedReady`) : vérifie que le restaurant est ouvert (`GetMerchantStatus`) → sinon `Status: "pos_closed"`.
4. **Switch sur `order.OrderType`** ("IN" / "DELIVERY" / "TAKE_AWAY") : résolution client, vérification zone de livraison, statut d'approbation (`MerchantApproval`) différent selon le type.
5. Appelle `GetPricingSNO` (qui ré-applique la validation anti-fraude des prix, voir 7.5) pour obtenir le payload de commande final sécurisé.
6. Si pricing pas `"success"` → renvoie ce statut tel quel.
7. Marque la commande : `CreatedBy = "SCANNORDER"`, `IsSNO = true`, `CashRegisterId = "SCANNORDER"`.
8. Délègue la création réelle à `s.orderLifeCycleSvc.CreateOrder(ctx, &models.RequestObject{...})` (module central partagé avec le reste du système — **c'est le point d'entrée que le Kiosk devra réutiliser**, voir section 9).
9. Si la commande nécessite un paiement en ligne (`newOrder.Action == "payment"`) : crée une session Stripe Checkout et l'attache à la réponse (`CheckoutSession`).

### 7.3 Source/canal de la commande aujourd'hui

Il n'existe **pas** de colonne générique `source`/`channel` sur `orders`. Le marquage actuel se fait via plusieurs signaux distincts et non centralisés :
- `order.IsSNO bool` — flag dédié ScanNOrder
- `order.CreatedBy *string` — vaut le literal `"SCANNORDER"` pour les commandes SNO (sinon un `user_id` réel pour les commandes staff)
- `order.CashRegisterId *string` — vaut aussi `"SCANNORDER"` pour ces commandes
- `orderMeta.Brand` (`models.BrandUberEats`, `models.BrandDeliveroo`, ou interne) — distingue les commandes des marketplaces externes
- Pas de notion de canal "Kiosk" évidemment — **à ajouter explicitement, voir KIOSK_DECISIONS.md section D.**

### 7.4 Upsell

`GetUpsell(ctx, qrCode)` → résout le merchant depuis le QR, puis `repo.GetUpsellProducts(ctx, merchantID)` qui sélectionne les produits avec `is_popular = 1 AND status IN ('available','1')`. Pas de moteur de recommandation : c'est une liste manuelle de produits "mis en avant" par le restaurateur (flag `is_popular` sur `products`). Le tracking d'acceptation de l'upsell (à la création de commande) passe par un module séparé `internal/modules/upsell` (`Tracker.TrackAsync`), appelé depuis `order_life_cycle.CreateOrder` si `req.UpsellSuggestionID` est renseigné — **mécanisme générique, réutilisable tel quel par le Kiosk**.

### 7.5 Pricing serveur — sécurité anti-fraude

`GetPricingSNO` ne fait **jamais confiance** aux prix envoyés par le client. `validateAndCleanPricingPayload` :
1. Collecte tous les `product_id` et `option_id` du payload.
2. Récupère les prix officiels en base (`GetProductPricesForSNO`, `GetConfigurationOptionPricesForSNO`).
3. Si un ID envoyé n'existe pas en base → erreur explicite, loggée en `zap.Warn` avec le tag "SECURITY" (tentative de fraude potentielle).
4. **Écrase** les prix du payload client avec les valeurs officielles avant tout calcul.

**Ce mécanisme est non négociable pour le Kiosk** : un kiosque physique est un client tout aussi non fiable qu'un navigateur — même validation à reprendre intégralement.

### 7.6 Disponibilité produit par canal

Le champ pivot est `models.ProductEntry.IsAvailableOnSNO *bool` (colonne `products.is_available_on_sno` côté SQL). `ComputeGetMenu` filtre le menu complet (`menu.MenuService.GetMenuFromMerchantIdWithMarketing`) en ne gardant que les produits avec `is_available_on_sno = true`, et en remontant les sous-produits d'un groupe non disponible. C'est **une colonne booléenne dédiée par canal sur `products`**, pas une table de disponibilité générique — convention à suivre ou à faire évoluer consciemment pour Kiosk (voir KIOSK_DECISIONS.md section B).

À distinguer de la table `availabilities` (+ `availabilities_products`, `availabilities_schedules`) qui gère les **plages horaires** de disponibilité d'un produit (ex. "menu midi only"), orthogonale à la disponibilité par canal.

---

## 8. Module `auth`

### 8.1 Flux de login

`AuthHandler.Login` → `AuthService.Login(ctx, payload, token, isBackoffice)` :
1. `AuthRepository.Login(ctx, username, password, token)` — requête SQL unique avec un énorme `SELECT` joignant `users`, `users_rights`, `merchant`, `merchant_parameters`, `subscriptions`, `packages`, `scannorder_settings`, intégrations (Uber Eats/Direct, Deliveroo). Authentifie soit par `username`/`email` + mot de passe (bcrypt, `helpers.PasswordMatches`), soit directement par token déjà valide (`loggedByToken`).
2. Si compte désactivé → statut `"account_disabled"`.
3. Si MFA requis **et** `isBackoffice` (header `X-App-Source: backoffice`) → statut `"MFA_REQUIRED"` (HTTP 202), envoi OTP par email/SMS.
4. Sinon → marque MFA vérifié + `MarkLastLoginAt`, construit la réponse complète (`buildLoginResponse`) avec capacités/permissions imbriquées + champs `Legacy` (rétrocompatibilité, à ne pas réutiliser pour du nouveau code).

### 8.2 Pas de JWT — token opaque permanent

**Aucun JWT dans tout le projet.** Le "token" est une chaîne stockée dans `users_rights.token`, générée une fois (mécanisme de génération non visible dans les fichiers audités — probablement `helpers.GenerateToken` à la création du compte), sans expiration ni rotation. La validation = lookup SQL/Redis direct sur cette valeur. Pas de claims, pas de signature, pas de durée de vie — l'invalidation se fait en supprimant/changeant la valeur en base + en vidant le cache Redis associé (`AuthService.InvalidateUserCache`).

### 8.3 "Refresh token" — n'existe pas au sens classique

Il n'y a pas de paire access/refresh token. Le token unique sert indéfiniment. Le seul mécanisme proche d'un refresh est le **PIN employé** (`AuthenticatePIN`) : un device déjà connecté avec un token "ancre" (`anchorToken`, n'importe quel utilisateur du même merchant) permet à un employé de s'authentifier rapidement par PIN 4 chiffres, ce qui retourne le **token permanent** de cet employé (`s.Login(ctx, ..., employee.Token, false)`) — donc toujours le même schéma "token opaque permanent", pas un token de session différent.

Mécanisme de verrouillage anti brute-force sur le PIN : compteur + lockout exponentiel stocké en Redis (`lockoutState{Count, LockedUntil}`, clé `models.PINLockoutPrefix+anchorToken`), pas en base.

### 8.4 `/device/token` (`SaveDeviceToken`) — notion de device existante

```go
func (h *AuthHandler) SaveDeviceToken(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)
	...
	var req SaveDeviceTokenRequest // { device_token, device_id, app }
	...
	resp, err := h.svc.SaveDeviceToken(ctx, token, req.DeviceToken, req.DeviceID, req.App)
	...
}
```
```go
func (s *AuthService) SaveDeviceToken(ctx context.Context, token, deviceToken, deviceID, app string) (map[string]string, error) {
	user, err := s.repo.GetUserByToken(ctx, token)
	...
	err = s.repo.SaveDevice(ctx, user.UserID, user.MerchantID, app, deviceID, deviceToken)
	...
}
```
```sql
INSERT INTO users_devices (user_id, merchant_id, app, device_id, fcm_token, last_used)
VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP())
ON DUPLICATE KEY UPDATE fcm_token = VALUES(fcm_token), last_used = UTC_TIMESTAMP(), ...
```

**Ce mécanisme est orienté notifications push (FCM)**, pas authentification : `device_token` = token FCM, pas un token de session. La table `users_devices` lie un `device_id` à un **`user_id` existant déjà authentifié par ailleurs**. Ce n'est **pas** un device "anonyme" capable de s'authentifier seul — il n'y a donc **aucune collision de concept ni de table** à craindre avec une future authentification Kiosk basée sur un device autonome (sans utilisateur humain derrière), mais il faut bien choisir un nom de table distinct (`kiosk_device_tokens`, pas `users_devices`) pour ne pas mélanger les deux sémantiques.

### 8.5 Notion de "device"/"terminal" existante

- `users_devices` (FCM token push, lié à un user humain) — voir 8.4.
- `cash_registers` (module `internal/modules/cash_registers`) — notion de **caisse physique**, proche conceptuellement d'un "device" Kiosk mais dédiée à l'encaissement staff (`device_id` apparaît dans `order_life_cycle` comme paramètre pour résoudre le registre actif : `GetActiveCashRegisterID(ctx, req.DeviceID)`). À étudier si le Kiosk doit s'enregistrer comme une forme de point de vente pour la comptabilité — voir KIOSK_DECISIONS.md.
- Aucune table `devices`/`terminals` générique et autonome (sans rattachement à un user humain) n'existe à ce jour — **le Kiosk introduira ce concept pour la première fois dans le projet.**

---

## 9. `OrdersLifeCycleService`

Point central de **toutes** les mutations de commande, quel que soit le canal d'origine (staff, ScanNOrder, webhooks Uber Eats/Deliveroo). Le Kiosk devra passer par ce service exactement comme `scannorder` le fait, pas le contourner.

### 9.1 Méthodes exposées (principales)

| Méthode | Rôle |
|---|---|
| `CreateOrder(ctx, *models.RequestObject)` | Création d'une commande — utilisée par ScanNOrder (et donc future base pour Kiosk) |
| `AcceptOrder` / `SetOrderAccepted` | Commande acceptée (OPEN, PENDING→ACCEPTED), déclenche intégrations externes async (Uber Eats/Deliveroo) si pertinent |
| `DenyOrder` / `SetOrderDenied` | Refus de commande |
| `SetReadyForDistribution` | Marque la commande prête (déclenche KDS/notif) |
| `SetDistributedProducts` / `BackToProduction` | Suivi production cuisine (KDS) |
| `SetDelivered` / `DeliverOrder` / `SetDeliveredExternal` | Clôture commande : génère le reçu fiscal, déduit les stocks, notifie |
| `AddPayment` / `CreatePayment` / `DisablePayment` / `ProcessRefund` | Gestion des paiements liés à une commande |
| `DeleteOrder` / `SetOrderDeleted` | Annulation/suppression |
| `UpdateOrder` / `PrepareUpdateOrder` | Mise à jour |
| `ExecuteOrderMutation` | **Wrapper générique transactionnel** : snapshot avant/après + exécution + log d'audit, tout dans une transaction `dbutils.RunInTx`, puis notification post-commit |

### 9.2 Cycle "created" → "confirmed" après paiement

1. `scannorder.CreateOrderSNO` appelle `orderLifeCycleSvc.CreateOrder` → la commande est insérée avec un statut interne (`newOrder.Status`, ex. `"1"`/`"success"`) et une `Action` (`"payment"` si un paiement en ligne est requis).
2. Si `Action == "payment"` : une session Stripe Checkout est créée côté `scannorder.Service`, et son URL renvoyée au client.
3. Le **webhook Stripe** (`internal/webhook/stripe/`) reçoit la confirmation de paiement asynchrone, et c'est lui qui déclenche la suite (acceptation/passage à l'état confirmé) — pas un appel direct synchrone du flux de création.
4. `SetOrderAccepted` (déclenché ensuite, manuellement par le staff ou automatiquement selon le flux) met la commande à `OPEN`/`PENDING`/`ACCEPTED` et déclenche les intégrations externes (Uber Eats/Deliveroo) en async (`go func(){...}`) si la commande est liée à une marketplace.

### 9.3 KDS et impression

- KDS (Kitchen Display System) : piloté via `SetDistributedProducts` / `BackToProduction` / `UpdateProductionStatus`, qui mettent à jour le statut de production des produits et **invalident systématiquement le cache Redis de la commande** (`helpers.GetRedisOrderKey(merchantID, orderID)`) avant de notifier via WebSocket.
- Impression : module séparé `internal/modules/printers` et `internal/modules/receipt` — `HandlerFiscalReceiptGeneration` (appelé depuis `DeliverOrder`) génère le reçu fiscal via `receiptService.GenerateFiscalReceipt`. Le détail de l'impression physique n'a pas été audité dans cette session — voir module `printers` si le Kiosk doit imprimer un ticket.

### 9.4 Statuts de commande

Pas d'`enum` Go typé strict observé — les statuts circulent en `string` (`order.State`, comparés littéralement : `"OPEN"`, `"CLOSED"`, `"DONE"` vus dans `scannorder.CancelOrderSNO`). Les actions d'audit sont en revanche typées via des constantes (`models.ActionOrderClose`, `models.ActionOrderUpdate`, `models.ActionOrderRefund`, `models.ActionOrderReopen`, `models.ActionPaymentAdded`, `models.ActionOrderDelete`) déclarées dans `internal/models`.

### 9.5 Webhook Stripe

Handler dédié dans `internal/webhook/stripe/` (non lu en détail dans cette session — structure handler/service/repository identique au reste du projet, hors `internal/modules/`). Le webhook reçoit l'événement Stripe signé, le valide, puis appelle vraisemblablement `OrdersLifeCycleService` pour faire progresser la commande (paiement confirmé → acceptation) et/ou `AddPayment`/`CreatePayment` pour enregistrer le paiement. **À auditer spécifiquement avant d'implémenter le paiement carte Kiosk** (voir KIOSK_DECISIONS.md, point ouvert).

---

## 10. WebSocket Hub

### 10.1 Structure

```go
type Client struct {
	conn       *websocket.Conn
	merchantID string
	connID     string
	send       chan []byte
	startedAt  time.Time
	log        *zap.Logger
}

type Hub struct {
	clients map[string]map[string]*Client // merchantID -> connID -> *Client
	mu      sync.RWMutex
}
```

Chaque client WebSocket est rattaché à un **merchant** (pas de granularité plus fine — pas de canal par device/Kiosk à ce jour). `Register`/`Unregister` gèrent l'ajout/retrait thread-safe.

### 10.2 Broadcast

```go
func (h *Hub) BroadcastToMerchant(merchantID string, message []byte) bool {
	// copie la liste des clients du merchant sous RLock
	// envoi non-bloquant sur chaque client.send (select/default)
	// désinscription silencieuse des clients dont le channel est plein/cassé
}
```
Tous les clients connectés pour un `merchantID` donné reçoivent le même message — pas de filtrage par type de client. Si le Kiosk doit écouter sélectivement certains événements (ex. juste les mises à jour de sa propre commande), ce filtrage devra être fait **côté client** (le Kiosk reçoit tout le flux merchant et ignore ce qui ne le concerne pas), à moins d'étendre le Hub avec une notion de canal/abonnement — non implémenté aujourd'hui.

### 10.3 Types d'événements

Non audités en détail dans cette session (fichier `handler.go` du module websocket non lu) — mais l'appelant le plus visible est `notification.NotificationTypeOrderUpdate`, déclenché systématiquement après toute mutation de commande dans `OrdersLifeCycleService` via `notificationsService.SendNotificationAsync(merchantID, orderID, notification.NotificationTypeOrderUpdate)`. Le Kiosk pourra s'abonner aux mêmes événements `OrderUpdate` pour rafraîchir son état de commande en temps réel (ex. attente écran de paiement → confirmation).

### 10.4 Ajouter un nouveau type d'événement

Pattern déduit (à vérifier dans `internal/modules/notification/`) : ajouter une nouvelle constante `notification.NotificationTypeXxx`, et appeler `notificationsService.SendNotificationAsync(merchantID, resourceID, notification.NotificationTypeXxx)` au bon endroit du service métier concerné — exactement comme c'est fait pour les commandes. Pas de modification du Hub WebSocket lui-même nécessaire (il reste agnostique du contenu, il transporte juste le message déjà sérialisé).

---

## 11. Conventions à respecter pour le module Kiosk

### 11.1 Règles concrètes

1. **Structure de fichiers** : `internal/modules/kiosk/{handler,service,repository,models}.go`, strictement dans ce découpage.
2. **JSON snake_case** partout, enveloppe `{"id": "kiosk.<fn>", "data": ...}` via `models.SendJSON`/`models.SendErrorJSON` — ne pas inventer un format de réponse différent.
3. **Pas d'ORM, SQL brut**, `?` placeholders, `dbutils.GetDB(ctx, db)` dans chaque méthode repository, `defer rows.Close()`, gestion explicite de `sql.ErrNoRows`.
4. **Erreurs sentinelles** déclarées dans `internal/models/responses_models.go` (ou un fichier dédié du module si très spécifique, comme `auth` le fait pour `ErrPINInvalidLength`), mappées dans `SendErrorJSON`.
5. **Aucun prix/donnée sensible ne doit être recalculé côté client** — reprendre le pattern `validateAndCleanPricingPayload` de ScanNOrder pour toute commande Kiosk.
6. **Toute création/mutation de commande passe par `OrdersLifeCycleService`** — ne jamais réimplémenter la logique de cycle de vie dans le module Kiosk.
7. **Le merchant scoping** ne doit jamais venir d'un paramètre client en clair sur les routes "device authentifié" — il doit venir d'une identité de device validée côté serveur (token Kiosk), exactement comme `user.MerchantID` pour les routes staff.
8. **Logging structuré zap**, pas de concaténation de strings dans les nouveaux fichiers.
9. **Migrations** : un nouveau fichier numéroté séquentiellement après le dernier existant, bloc de commentaire de contexte en tête, `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`, soft delete via colonne `enabled` si pertinent (pas `deleted_at`).
10. **Branchement dans `routes.go`** : import, construction manuelle `NewRepository → NewService → NewHandler`, `r.Route("/kiosk", func(r chi.Router) {...})` avec le bon middleware (probablement un nouveau middleware d'auth device, pas `authMiddleware` existant qui attend un `*auth.UserLoginRow`).

### 11.2 Anti-patterns à éviter

- ❌ Renvoyer `err.Error()` brut au client (legacy, fuite d'info) — toujours passer par `SendErrorJSON` avec une sentinelle.
- ❌ Faire confiance à un prix, un `merchant_id`, ou un statut envoyé par le Kiosk dans le body — toujours revalider serveur.
- ❌ Créer une nouvelle table `devices` générique qui collisionnerait sémantiquement avec `users_devices` (FCM/push, liée à un user humain) — nommer explicitement les tables Kiosk (`kiosk_*`).
- ❌ Implémenter un système de JWT pour le Kiosk alors que tout le reste du projet utilise un token opaque — sauf décision explicite et documentée (voir KIOSK_DECISIONS.md, le sujet est ouvert et structurellement différent du reste : un device n'est pas un humain, un JWT pourrait être justifié ici, **mais cela introduirait une incohérence avec le reste du projet à valider consciemment**).
- ❌ Contourner `OrdersLifeCycleService` pour gagner en rapidité — c'est le point d'intégration KDS/notifications/audit/stock, le contourner casse ces fonctionnalités silencieusement.
- ❌ Ajouter une contrainte FK vers des tables historiques (`merchant`, `products`, `orders`) si ce n'est pas déjà la pratique du module — rester cohérent avec le reste (pas de FK vers l'historique, FK possible entre tables Kiosk).

### 11.3 Checklist avant implémentation

- [ ] Décisions de `KIOSK_DECISIONS.md` validées par Ilies (schéma DB, auth device, disponibilité par canal, paramétrage back-office)
- [ ] Migration(s) Kiosk écrites et numérotées après la dernière existante
- [ ] Module `internal/modules/kiosk/` créé avec les 4 fichiers standards
- [ ] Middleware d'authentification device créé (distinct de `middleware.Auth`, qui suppose un `*auth.UserLoginRow`)
- [ ] Routes branchées dans `routes.go`, groupées sous `/kiosk`, avec le bon niveau de protection
- [ ] Réutilisation effective de `OrdersLifeCycleService.CreateOrder` (pas de duplication de logique commande)
- [ ] Validation serveur des prix reprise du pattern ScanNOrder
- [ ] Test manuel : enrôlement d'une borne, commande complète, paiement, réception KDS, notification temps réel
