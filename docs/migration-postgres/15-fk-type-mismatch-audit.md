# 15 — Audit des incohérences de type entre colonnes `*_id` et les PK qu'elles référencent

Fait suite à [03-table-usage-audit.md](03-table-usage-audit.md) (tables vivantes/orphelines) et
[12](12-merchant-id-unification.md)/[13-merchant-id-schema-update.md](13-merchant-id-schema-update.md)
(unification `merchant_id`). Objectif : détecter systématiquement, sur les 180 tables de
[04-schema-postgres-target.sql](04-schema-postgres-target.sql), les colonnes `*_id` dont le type ne
correspond pas au type de la PK de la table qu'elles référencent probablement. **Lecture seule** —
aucun fichier source ni schéma modifié.

## Méthode

1. Parsing complet du schéma cible : 180 tables, **360 colonnes `*_id`** (hors colonne PK
   se référençant elle-même).
2. Déduction de la table cible par le nom (`cash_register_id` → `cash_registers`,
   `user_id` → `users.user_id`), complétée manuellement pour les 108 colonnes non résolues
   automatiquement (suffixes `*_by_user_id`, tables planning/HACCP, identifiants externes…).
3. Comparaison du type de la colonne au type de la PK cible.
4. Croisement avec la liste des 37 tables orphelines du rapport 03 + vérification par grep des
   jointures réelles dans `internal/` pour les cas critiques.

**Répartition brute** : 85 concordances exactes, 139 incohérences de famille de type
(dont 109 `merchant_id`, cas déjà tranché — voir §5), 7 écarts de largeur `varchar`,
21 cibles à PK composite, 108 résolues manuellement (dont identifiants externes).

## Pourquoi c'est bloquant pour Postgres (et pas pour MySQL)

MySQL/MariaDB caste implicitement `integer = varchar` dans les jointures et les `WHERE`.
**PostgreSQL refuse cette comparaison** (`operator does not exist: integer = character varying`).
Chaque incohérence ci-dessous qui est traversée par une jointure Go vivante est donc une **erreur
SQL à l'exécution après migration**, pas un simple défaut d'hygiène.

---

## 1. Incohérences VIVANTES critiques (int ↔ varchar), triées par centralité

### 1.1 Cœur commandes — cassent le fetcher de commandes

| Colonne | Type actuel | PK visée | Type PK | Preuve d'usage vivant |
|---|---|---|---|---|
| `orderitems.order_id` | `varchar(20)` | `orders.order_id` | `integer` | `INNER JOIN orderitems oi ON o.order_id = oi.order_id` — [orders_fetcher_builder.go:119](../../internal/modules/orders/orders_fetcher_builder.go#L119), [tasks/distribution.go:119](../../internal/tasks/distribution.go#L119), `order_life_cycle/repository.go` |
| `product_configurable_attribute.product_id` | `varchar(64)` | `products.product_id` | `integer` | `pca.product_id = p.product_id` — [menu/repository.go:896](../../internal/modules/menu/repository.go#L896) ; `pca.product_id = oi.product_id` — [orders_fetcher_builder.go:231](../../internal/modules/orders/orders_fetcher_builder.go#L231) |
| `order_item_configuration.configuration_attribute_id` | `integer` | `configurable_attributes.id` | `varchar(64)` | table vivante (order flows) ; les jointures vivantes sur `ca.id` sont en varchar |
| `orderitems.discount_id` | `integer` | `discounts.discount_id` | `varchar(50)` | orderitems/discounts vivants — sens inverse (int stocké, PK varchar) |
| `orders.parent_order_id` | `varchar(50)` | `orders.order_id` (self) | `integer` | table `orders`, vivante partout |
| `orders.cash_register_id` | `varchar(11)` | `cash_registers.cash_register_id` | `integer` | `WHERE o.cash_register_id = ?` — [cash_registers/repository.go:277](../../internal/modules/cash_registers/repository.go#L277) |
| `payments.cash_register_id` | `varchar(20)` | `cash_registers.cash_register_id` | `integer` | [cash_registers/repository.go:294-311](../../internal/modules/cash_registers/repository.go#L294) — **attention** : contient aussi des sentinelles `'SCANNORDER'`, `'UBER_EATS'`, `'DELIVEROO'` → le varchar est en partie *intentionnel*, la colonne est hybride (id numérique ou code plateforme) |
| `orders.deletion_reason_id` | `varchar(11)` | `deletion_reasons.deletion_reason_id` | `integer` | orders vivant |
| `delivery_session_order.deletion_reason_id` | `varchar(20)` | `deletion_reasons.deletion_reason_id` | `integer` | module `delivery_sessions` vivant |

À noter : `orderitems` a une **PK composite `(order_item_id integer, order_id varchar(20),
product_id integer)`** — le `varchar(20)` est donc ancré dans la PK elle-même.

### 1.2 Stock / produits

| Colonne | Type actuel | PK visée | Type PK | Usage vivant |
|---|---|---|---|---|
| `stock_movements.component_id` | `varchar(50)` | `components.component_id` | `integer` | [stocks/repository.go:706-707](../../internal/modules/stocks/repository.go#L706) — `COALESCE(c.name, sm.component_id)` mélange déjà les types |
| `stock_movements.order_id` | `varchar(50)` | `orders.order_id` | `integer` | stocks vivant |
| `stock_movements.order_item_id` | `varchar(50)` | `orderitems.order_item_id` | `integer` | stocks vivant |
| `stock_movements.product_id` | `varchar(50)` | `products.product_id` | `integer` | stocks vivant |
| `stock_movements.consumable_id` | `varchar(50)` | `consumables.consumable_id` | `integer` | colonne vivante, mais table cible `consumables` **orpheline** (03 §2) |
| `product_allergens.product_id` | `varchar(255)` | `products.product_id` | `integer` | menu vivant |
| `product_tags.product_id` | `varchar(255)` | `products.product_id` | `integer` | menu/tags vivants |
| `product_marketing_categories.product_id` | `varchar(64)` | `products.product_id` | `integer` | menu vivant |
| `discounts_products_options.product_id` | `varchar(20)` | `products.product_id` | `integer` | discounts vivant |
| `discounts_products_options.option_id` | `varchar(20)` | `configurable_attribute_options.id` | `integer` | discounts vivant |
| `components.purchase_unit_id` | `varchar(35)` | `unit_of_measure.id` | `integer` | components vivant — cible à confirmer (peut viser un code d'unité, pas la PK) |

### 1.3 Fidélité / clients

| Colonne | Type actuel | PK visée | Type PK | Usage vivant |
|---|---|---|---|---|
| `customer_loyalty_progress.customer_id` | `varchar(30)` | `customer.customer_id` | `integer` | module customers (fidélité) vivant |
| `customer_loyalty_progress_order.order_id` | `varchar(30)` | `orders.order_id` | `integer` | vivant |
| `customer_rewards.customer_id` | `varchar(30)` | `customer.customer_id` | `integer` | vivant |
| `customer_rewards.used_on_order_id` | `varchar(20)` | `orders.order_id` | `integer` | vivant |
| `customer_loyalty_program_reward_products.product_id` | `varchar(50)` | `products.product_id` | `integer` | vivant |
| `customer_loyalty_program_target_products.product_id` | `varchar(50)` | `products.product_id` | `integer` | vivant — cf. `tp.loyalty_program_id` joint dans [customers/repository.go:1441](../../internal/modules/customers/repository.go#L1441) |
| `booking_waitlist.customer_id` | `varchar(64)` | `customer.customer_id` | `integer` | `*string` en Go ([waitlist_models.go:7](../../internal/modules/bookings/waitlist_models.go#L7)) alors que `bookings.customer_id` est `integer NOT NULL` — la même notion a deux types dans le même domaine |

### 1.4 Autres modules vivants

| Colonne | Type actuel | PK visée | Type PK | Usage vivant |
|---|---|---|---|---|
| `api_request_logs.user_id` | `bigint` | `users.user_id` | `varchar(64)` | middleware [request_logger/logger.go](../../internal/middleware/request_logger/logger.go) — seul `user_id` numérique du schéma |
| `floor_obstacles.floor_id` | `varchar(64)` | `floors.id` | `integer` | [locations/repository.go:279-390](../../internal/modules/locations/repository.go#L279) — incohérent aussi avec sa sœur `floor_areas.floor_id integer` |
| `kiosks.location_id` | `varchar(64)` | `locations.location_id` | `integer` | kiosk vivant, `*string` en Go ([kiosk/models.go:25](../../internal/modules/kiosk/models.go#L25)) ; FK candidate notée dans le schéma lui-même |
| `upsell_suggestions.order_id` | `varchar(64)` | `orders.order_id` | `integer` | module upsell vivant |
| `integration_uber_eats_attributes_mapping.configurable_attribute_id` | `integer` | `configurable_attributes.id` | `varchar(64)` | webhook Uber Eats vivant (`attribute_mapping_repo.go`) |
| `integration_deliveroo_products_mapping.product_id` | `varchar(50)` | `products.product_id` | `integer` | webhook Deliveroo menu vivant |
| `integration_uber_eats_products_mapping.product_id` | `varchar(50)` | `products.product_id` | `integer` | webhook Uber Eats vivant |

---

## 2. Incohérences vivantes mineures (même famille, largeurs différentes)

Sans risque d'erreur SQL en PG (comparaison `varchar`/`varchar` valide), mais risque de troncature
à l'INSERT si la valeur réelle dépasse la largeur cible, et signe d'un référentiel flou :

| Colonne | Type actuel | PK visée | Type PK |
|---|---|---|---|
| `audit_logs.user_id` | `varchar(36)` | `users.user_id` | `varchar(64)` |
| `device_link.user_id` | `varchar(20)` | `users.user_id` | `varchar(64)` |
| `order_comments.user_id` | `varchar(20)` | `users.user_id` | `varchar(64)` |
| `order_changes_log.changed_by_user_id` | `varchar(25)` | `users.user_id` | `varchar(64)` — table orpheline |
| `discounts_products_options.discount_id` | `varchar(20)` | `discounts.discount_id` | `varchar(50)` |
| `product_allergens.allergen_id` | `varchar(255)` | `allergens.allergen_id` | `varchar(35)` |
| `product_tags.tag_id` | `varchar(255)` | `tags.tag_id` | `varchar(42)` |
| `integration_deliveroo.brand_id` | `varchar(150)` | `brands.brand_id` | `varchar(35)` |
| `customer_loyalty_progress.loyalty_program_id` (+3 tables sœurs) | `varchar(30)` | `customer_loyalty_programs.id` | `varchar(50)` |
| `customer_loyalty_progress_order.progress_id` | `varchar(30)` | `customer_loyalty_progress.id` | `varchar(50)` |
| `components.category_id` | `varchar(15)` | `component_category.merchant_categ_id` (clé métier, la PK est `id integer`) | `varchar(11)` — le code vivant ([menu/repository.go:2767](../../internal/modules/menu/repository.go#L2767), haccp) n'utilise que `merchant_categ_id`, jamais `id` |

---

## 3. Incohérences sur tables ORPHELINES (basse priorité — à ne pas porter, cf. 03 §2)

| Colonne | Type actuel | PK visée | Type PK |
|---|---|---|---|
| `checkout_orderitems.order_item_id` | `integer` | `orderitems.order_item_id` | `integer` (ok) — table orpheline de toute façon |
| `category_discount.merchant_discount_id` | `integer` | `discounts.discount_id` | `varchar(50)` |
| `customer_advertisement_emails.customer_id` | `varchar(30)` | `customer.customer_id` | `integer` |
| `order_changes_log.order_id` | `varchar(25)` | `orders.order_id` | `integer` |
| `order_ratings.order_id` | `varchar(255)` | `orders.order_id` | `integer` |
| `product_ratings.product_id` | `varchar(255)` | `products.product_id` | `integer` |
| `users_nfc_tags.tag_id` | `integer` | `tags.tag_id` | `varchar(42)` — probablement pas une FK vers `tags` (tag NFC physique) malgré le commentaire "FK candidate" du schéma |
| `integration_deliveroo_attributes_mapping.configurable_attribute_id` | `integer` | `configurable_attributes.id` | `varchar(64)` |
| `integration_deliveroo_components_mapping.component_id` | `varchar(50)` | `components.component_id` | `integer` |
| `integration_uber_eats_components_mapping.component_id` | `varchar(50)` | `components.component_id` | `integer` |
| `invoices.invoice_id` | `varchar(50)` | (identifiant métier, PK = `id integer`) | — table orpheline |

---

## 4. Colonnes écartées : identifiants externes (pas des FK internes)

Non comptées comme incohérences — elles stockent des identifiants de plateformes tierces :

- **Stripe** : `stripe_accounts.account_id`/`terminal_location_id`, `stripe_payments.payment_intent_id`/`checkout_session_id`, `packages.stripe_price_id`, `subscriptions.stripe_subscription_id`, `welloresto_stripe_customers.stripe_customer_id`, `subscription_invoices.invoice_id` (id Stripe `in_…`, pas la table orpheline `invoices`).
  `stripe_accounts.customer_id varchar(50)` : le commentaire du schéma le donne "FK candidate → customer.customer_id (integer)", mais **aucun code Go ne lit cette colonne** — probablement un id Stripe ou une colonne morte, à trancher avant de créer une FK.
- **Uber / Deliveroo** : `integration_uber_eats.store_id`, `integration_uber_direct.customer_id`/`client_id`/`external_store_id`, `integration_deliveroo.location_id` (site Deliveroo, pas la table `locations`), tous les `item_id`/`modifier_group_id`/`workflow_id` des tables `integration_*_mapping`, `orders.brand_order_id`, `orderitems.brand_order_item_id`.
- **Google** : `customer.customer_google_place_id`.
- **Divers non-FK** : `app_version.app_id` (code applicatif 0/1/2), `audit_logs.resource_id` (polymorphe), `customer.customer_brand_id`, `customer_advertisement_emails.marketing_campaing_id` (sic), `migration_users._id`/`history___id` (héritage Mongo), `orders.public_id`, `qrcodes.QR_id`, `device_id` (`cash_registers`, `sub_cash_registers`, `device_link` — identifiants matériels, largeurs incohérentes 50 vs 255 avec `users_devices.device_id`), `productcateg.merchant_categ_id`/`component_category.merchant_categ_id` (clés métier propres), `employees.member_id` (bigint, origine inconnue), `planned_shifts.department_id` (orphelin, aucune table `departments`), `kiosk_device_tokens.new_id`.

## 5. Cas `merchant_id` — déjà tranché, exclu du décompte

109 des 139 incohérences brutes sont les colonnes `merchant_id varchar(64)` face à
`merchant.id integer`. C'est **volontaire** : les rapports [10](10-merchant-id-type-scope.md) à
[13](13-merchant-id-schema-update.md) ont établi que l'identifiant tenant de référence est le
`varchar(64)` (aligné sur `haccp_settings`/`kiosk_settings`), le Go a été unifié en `string` (12) et
le schéma cible en `varchar(64)` (13). La relation réelle entre ce `merchant_id` applicatif et
`merchant.id` reste un point ouvert de la modélisation, hors périmètre ici.

Cohérences confirmées au passage (aucune action) : `*_by_user_id`/`creator_user_id`/
`notification_user_id` → `users.user_id varchar(64)` ; tout le domaine planning
(`week_id`, `shift_id`, `position_id`, `*_employee_id`, `week_template_id` → PK `varchar(64)`) ;
tout le domaine HACCP (`session_id`, `zone_id`, `surface_id`, `reading_id`, `action_id` →
`varchar(64)`) ; `products.tva_*_id integer` → `tva_categories.tva_id integer` ;
`booking_events.waitlist_id` → `booking_waitlist.id varchar(64)`.

## 6. Recommandations

1. **Bloquant migration** : les jointures du §1.1/1.2 (`orderitems.order_id`,
   `product_configurable_attribute.product_id`, `stock_movements.*`, `cash_register_id`)
   échoueront en PG. Deux options par colonne : aligner le type sur la PK cible dans le schéma
   cible (avec conversion des données à la copie), ou caster explicitement dans les requêtes Go —
   l'alignement de type est préférable (index utilisables, FK possibles à terme).
2. **Cas hybride** `payments.cash_register_id` (et probablement `orders.cash_register_id`) :
   les sentinelles `'SCANNORDER'`/`'UBER_EATS'`/`'DELIVEROO'` interdisent un simple passage en
   `integer`. À modéliser (colonne source séparée, ou id négatifs réservés, ou table
   `cash_registers` virtuelle) avant la migration.
3. **Largeurs varchar** (§2) : unifier vers la largeur de la PK cible dans le schéma cible —
   changement sans risque tant que fait avant la copie des données.
4. Les incohérences du §3 disparaissent d'elles-mêmes si les tables orphelines ne sont pas portées.
5. Trancher `stripe_accounts.customer_id` (morte ? Stripe ?) et `components.purchase_unit_id`
   avant d'écrire les FK Postgres définitives.
