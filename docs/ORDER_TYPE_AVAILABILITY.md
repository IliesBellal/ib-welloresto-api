# Disponibilité par mode de commande (Scan'N'Order & Kiosk)

Date : 2026-09-28 — branche `staging`.

## Constat de départ

Les colonnes `products.available_in`, `available_take_away` et
`available_delivery` (toggles "sur place / à emporter / livraison" du
back-office) n'étaient lues que par les mappers Uber Eats / Deliveroo.

- **Scan'N'Order** : `GET /scannorder/{slug}/menu?order_type=` ne filtrait que
  sur `is_available_on_sno` + disponibilités horaires. `order_type` ne servait
  qu'au choix du prix et au filtrage fidélité/remises. Les trois champs étaient
  ensuite mis à `nil` par `cleanProductForSNO` : le front
  (`wello-resto-scannorder/src/lib/api/normalize.ts`) les force à `true` en
  supposant que le backend a déjà filtré — ce qui n'était pas le cas.
- **Kiosk** : même situation (`is_available_on_kiosk` + horaires uniquement).
- **Upsell** (SNO et Kiosk) : aucun filtre par mode.
- **Pricing / création de commande** : aucun contrôle — un produit non livrable
  pouvait être commandé en livraison.

## Règle implémentée

Un produit n'est proposé / accepté que si le flag correspondant au mode demandé
l'autorise :

| Mode (`order_type`) | Colonne vérifiée        |
|---------------------|-------------------------|
| `IN`                | `available_in`          |
| `TAKE_AWAY`         | `available_take_away`   |
| `DELIVERY`          | `available_delivery`    |

Ce contrôle s'ajoute (ET logique) aux filtres existants : flag canal
(`is_available_on_sno` / `is_available_on_kiosk`) et disponibilités horaires.

Helpers partagés (`internal/models/order_type_availability.go`) :
`models.DefaultChannelOrderType`, `models.NormalizeOrderType`,
`models.OrderTypeAvailabilityColumn`, `(*models.ProductEntry).IsAvailableForOrderType`.

## Surfaces couvertes

### Scan'N'Order (`internal/modules/scannorder`)

| Endpoint | Comportement |
|---|---|
| `GET /menu?order_type=` | produit (et sous-produit) retiré s'il n'est pas disponible pour le mode |
| `GET /products/{id}?order_type=` | `product_not_available_on_sno` (même erreur que canal/horaire) |
| `GET /upsell?order_type=` (déprécié) | nouveau paramètre optionnel, suggestions filtrées |
| `POST /upsell` (`order.order_type`) | suggestions non disponibles pour le mode retirées |
| `POST /pricing` + création | produit listé dans `unavailable_products` avec `status = "not_available_for_order_type"` → la création renvoie `unavailable_products` (même mécanique que `out_of_schedule`) |

### Kiosk (`internal/modules/kiosk`)

| Endpoint | Comportement |
|---|---|
| `GET /kiosk/menu?order_type=` | produit (et sous-produit) retiré s'il n'est pas disponible pour le mode |
| `GET /kiosk/products/{id}?order_type=` | `ErrKioskProductUnavailable` |
| `POST /kiosk/upsell` | nouveau champ optionnel `order_type` dans le body ; suggestions filtrées |
| pricing + création | `ErrKioskProductUnavailable` (via `validateKioskProductAvailability`) |

## Décisions

1. **Type par défaut = `TAKE_AWAY`** (choix produit du 2026-09-28), sur les deux
   canaux. Une valeur absente ou inconnue est ramenée à `TAKE_AWAY` (après
   `TrimSpace` + `ToUpper`). Objectif : les anciennes versions d'app qui
   n'envoient pas le mode (notamment l'upsell Kiosk, qui ne le transmettait
   pas du tout) continuent de fonctionner sans erreur. La normalisation est
   faite dans le service, **avant** la construction de la clé Redis et de
   l'ETag : `""`, `"take_away"` et `"TAKE_AWAY"` partagent la même entrée de
   cache.
   - *Proposition initiale écartée* : `IN` par défaut (repli historique du
     prix de base). Remplacée par `TAKE_AWAY` à la demande du produit.
   - *Conséquence* : un client qui ne passe pas `order_type` au menu/produit
     reçoit désormais le **prix à emporter** (et non plus le prix de base).
     Le front SNO envoie toujours le type ; le Kiosk l'envoie dès que le mode
     est choisi (le premier chargement sur l'écran d'accueil, sans mode, est
     rechargé ensuite avec le mode choisi).
2. **Kiosk : pas de `DELIVERY`**. Le Kiosk n'accepte que `IN` / `TAKE_AWAY`
   (cf. `kioskFulfillmentToOrderType`) ; `DINE_IN` (vocabulaire pricing Kiosk)
   est accepté comme alias de `IN` ; tout le reste, `DELIVERY` compris,
   retombe sur `TAKE_AWAY`.
3. **`NULL` = disponible**. Les colonnes sont scannées en `sql.NullBool` et un
   `NULL` laisse le pointeur à `nil`. On ne masque un produit que sur un `false`
   explicite (même convention que `Available` dans
   `mapProductEntryToKioskProduct`, et `COALESCE(col, TRUE)` côté SQL), pour ne
   pas faire disparaître des produits sur des lignes non renseignées.
   *Vérifié sur staging (2026-09-28)* : les trois colonnes sont
   `boolean NOT NULL DEFAULT true` — le cas `NULL` est donc purement défensif.
4. **Produits "groupes" / sous-produits** : le flag de mode est combiné au flag
   canal (même traitement que `is_available_on_sno` / `is_available_on_kiosk`) :
   un parent indisponible pour le mode n'est pas affiché, mais ses
   sous-produits sont évalués individuellement. Différent des disponibilités
   horaires, où un groupe hors créneau emporte tous ses sous-produits.
5. **Pricing SNO : nouveau statut `not_available_for_order_type`** plutôt
   qu'une erreur dure : même forme de réponse que `out_of_schedule`
   (`status: "success"` + `unavailable_products`, sans prix). Les anciens
   fronts traitent `status` comme un texte libre et bloquent déjà le CTA dès que
   la liste est non vide (libellé générique "indisponible"). Un produit à la fois
   hors créneau et non disponible pour le mode n'est signalé qu'une fois
   (`out_of_schedule` prioritaire).
6. **Prix du pricing SNO inchangé** : `validateAndCleanPricingPayload` garde son
   repli historique (`default: // "IN"` → `price`) pour un `order_type` vide ;
   seul le contrôle de disponibilité utilise le type normalisé. Le type stocké
   sur la commande n'est pas réécrit. Le front SNO envoie toujours
   `order_type` au pricing, le cas vide n'est pas un flux réel.
7. **`ordersService.ComputePricing` non modifié** : partagé avec le POS, qui ne
   doit pas hériter de cette règle canal. Le contrôle est fait dans
   `scannorder.GetPricingSNO` et `kiosk.validateKioskProductAvailability`.
8. **Cache** : `order_type` suffixait déjà les clés `ScannorderMerchantMenu` /
   `KioskMerchantMenu` ; le filtrage se fait avant la mise en cache, et toute
   modification des toggles passe par une mutation menu qui invalide ces clés
   (`InvalidateMerchantMenuCaches`).
   - *Upsell GET SNO* : première idée — filtrer après le cache comme le filtre
     horaire — **écartée** : la réponse en cache est déjà passée par
     `cleanProductForSNO`, qui efface les flags `available_*`, et son prix
     dépend du mode. La clé devient `ScannorderMerchantUpsell + merchantID +
     ":" + mode`, le filtre est appliqué avant nettoyage et mise en cache, et
     `InvalidateMerchantMenuCaches` purge en plus le motif
     `ScannorderMerchantUpsell + merchantID + ":*"` (l'ancienne clé sans mode
     reste purgée pour son TTL résiduel).
9. **`cleanProductPricesForSNO` ne déréférence plus un prix de mode `nil`** :
   le prix de base est conservé (même repli que `COALESCE(price_x, price)` du
   pricing). Avec `TAKE_AWAY` par défaut, l'ancien upsell GET (qui passait `""`
   et ne touchait jamais `PriceTakeAway`) serait passé par ce déréférencement.
   Sur staging, `price_take_away` / `price_delivery` sont `NOT NULL DEFAULT 0` :
   garde défensive uniquement.
10. **Kiosk upsell** : `KioskUpsellRequest` gagne un champ optionnel
    `order_type` (body JSON). La dette « fulfillmentType non transmis »
    (`docs/KIOSK_DECISIONS.md`) est levée côté API ; la borne l'envoie à
    partir de la version adaptée (repo `wello-kiosk`).

## Point d'attention remonté (non traité)

Sur staging, **498 produits actifs ont `price_take_away = 0` alors que
`price > 0`** (499 pour `price_delivery`) — conséquence du `DEFAULT 0` des
colonnes. Préexistant pour tout menu/pricing `TAKE_AWAY` (le pricing SNO fait
`COALESCE(price_take_away, price)`, qui ne rattrape pas un `0`), mais le
passage de `TAKE_AWAY` en mode par défaut l'expose aussi aux clients qui
n'envoient pas le mode (chargement initial Kiosk, anciennes bornes pour
l'upsell). Staging n'étant pas représentatif, le volume en production est à
mesurer avant d'arbitrer (requête agrégée, lecture seule) :

```sql
SELECT count(*) FILTER (WHERE price_take_away = 0 AND price > 0) AS take_away_zero,
       count(*) FILTER (WHERE price_delivery  = 0 AND price > 0) AS delivery_zero
FROM products
WHERE enabled = true AND status NOT IN ('removed_from_menu');
```

## Adaptations des clients

- **wello-kiosk** : `ApiService.getUpsell` envoie `order_type` (mode courant du
  panier, `'IN'`/`'TAKE_AWAY'`) dans le body de `POST /kiosk/upsell`.
- **wello-resto-scannorder** : le POST upsell envoyait déjà `order.order_type`.
  Ajout du libellé du statut `not_available_for_order_type` (bannière panier) ;
  `getUpsell` (déprécié) accepte le mode.

## Fichiers modifiés

- `internal/models/order_type_availability.go` (+ test) — helpers.
- `internal/models/request_objects.go` — `UnavailableStatusNotAvailableForOrderType`.
- `internal/modules/scannorder/{service,handler,repository}.go` (+ `order_type_availability_test.go`).
- `internal/modules/kiosk/{service,handler,repository,models}.go`
  (+ `order_type_availability_test.go`, `schedule_availability_test.go`,
  `postgres_integration_test.go`).
- `internal/infrastructure/redis/client.go` — motif d'invalidation upsell.

## Exécution (2026-09-28, poste local)

- `go build ./...` : OK.
- `go vet -tags postgres_integration` sur `kiosk`, `scannorder`, `models` : OK
  (le test d'intégration Kiosk compile ; **non exécuté** — il écrit en base,
  pas lancé contre staging sans accord).
- `go test ./internal/models/ ./internal/modules/kiosk/... ./internal/modules/scannorder/...` : OK.
- `go test ./internal/...` : tout vert sauf `planning/employees`,
  `planning/leave`, `planning/swaps` — **échecs préexistants**, reproduits à
  l'identique sur un worktree propre au `HEAD` (`fd61231`), sans rapport.
- Lecture seule sur staging (`RENDER_STAGING_DATABASE_URL`) : schéma des
  colonnes (`NOT NULL DEFAULT true` / prix `NOT NULL DEFAULT 0`), 2602 produits
  actifs dont 4 `available_in = false`, 101 `available_take_away = false`,
  100 `available_delivery = false` ; comptage des prix de mode à 0 (voir
  point d'attention).
- Pas d'appel HTTP de bout en bout (serveur non lancé).
- Fronts : `wello-kiosk` — `flutter analyze` OK (2 infos préexistantes hors
  périmètre), `flutter test` 107/107 ; `wello-resto-scannorder` — eslint OK
  sur les fichiers modifiés, `vitest run` 51/51 (`tsc` : 2 erreurs `google`
  préexistantes dans `TrackingMap.tsx` / `useMapBounds.ts`).
- Aucun commit.
