# Audit de migration MySQL → PostgreSQL — 01 : État des lieux

> Date : 2026-07-13 — généré par analyse statique du repo `ib-welloresto-api` (aucun accès DB).
> Périmètre : code Go (`cmd/`, `internal/`), migrations SQL (`migrations/`), configuration.

---

## 1. Schéma actuel

### 1.1 Limite importante : le schéma complet n'est PAS dans le repo

Les migrations commencent à `001` et ne couvrent que les évolutions récentes. **Toutes les tables "historiques" (cœur métier) n'ont aucun `CREATE TABLE` dans le repo** — constat déjà documenté dans [docs/DELIVERY_API_AUDIT.md](../DELIVERY_API_AUDIT.md). **Prérequis bloquant pour la migration : extraire le DDL de production via `mysqldump --no-data` (ou `SHOW CREATE TABLE`) avant toute conversion.**

### 1.2 Tables créées par les migrations du repo (~60 tables, DDL disponible)

Moteur/charset systématiques quand déclarés : `ENGINE=InnoDB` (52×), `CHARSET=utf8mb4` (52×), `COLLATE=utf8mb4_unicode_ci` (39×).

| Domaine | Tables | Migration source |
|---|---|---|
| Availabilities | `availabilities`, `availabilities_products`, `availabilities_schedules` | 003 |
| Marketing | `marketing_categories`, `product_marketing_categories` | 007 |
| HACCP température | `temperature_zones`, `temperature_readings`, `temperature_sessions`, `haccp_settings`, `haccp_corrective_actions`, `temperature_reading_corrective_actions` | 010, 020 |
| HACCP nettoyage/réception | `cleaning_zones`, `cleaning_surfaces`, `cleaning_tasks`, `cleaning_sessions`, `cleaning_executions`, `goods_receipts` | 011, 012 |
| Planning | `planning_settings`, `planning_positions`, `employees`, `employee_documents`, `hours_amendments`, `labor_rules`, `holiday_calendar`, `planning_holiday_overrides`, `planning_weeks`, `planning_shifts`, `planning_time_entries`, `planning_leave_requests`, `planning_shift_swap_requests`, `planning_shift_templates`, `planning_week_templates`, `planning_week_template_shifts`, `planning_revenue_forecasts`, `sys_attendance_sources`, `sys_contract_types`, `sys_planning_event_types` | 014–030 |
| Delivery | `delivery_position`, `delivery_session_order` (recréation) | 032 |
| Google Maps | `merchant_google_maps_monthly` | 036 |
| Kiosk | `kiosks`, `kiosk_enrollment_codes`, `kiosk_device_tokens`, `kiosk_settings` | 037–042 |
| Discounts | `discount_redemptions` | 041 |
| Floorplan | `floors`, `floor_areas`, `locations` (baseline documentaire), `floor_obstacles` | 050, 063 |
| Bookings | `bookings`, `booked_location`, `booked_location_dedup`, `order_location`, `hours_of_operation` (baseline), `bookings_settings`, `booking_duration_rules`, `booking_events`, `booking_waitlist` | 051–059 |

### 1.3 Tables historiques référencées uniquement dans le code Go (DDL absent du repo)

Extraites par grep des clauses `FROM/JOIN/INSERT INTO/UPDATE` (~1 050 requêtes) :

`orders`, `orderitems`, `order_comments`, `order_item_configuration`, `session_orderitem`, `customer`, `customer_loyalty_programs`, `customer_loyalty_progress`, `customer_loyalty_progress_order`, `customer_loyalty_rewards` (`customer_rewards`), `customer_loyalty_program_target_products`, `customer_loyalty_program_reward_products`, `merchant`, `merchant_parameters`, `merchant_translation_languages`, `merchant_marketing_settings`, `users`, `users_rights`, `users_devices`, `payments`, `receipts`, `restaurant_ticket`, `stripe_payments`, `stripe_accounts`, `subscriptions`, `subscription_invoices`, `packages`, `products`, `productcateg`, `product_tags`, `product_allergens`, `product_configurable_attribute`, `configurable_attributes`, `configurable_attribute_options`, `tags`, `allergens`, `tva_categories`, `recipes`, `components`, `component_category`, `unit_of_measure`, `unit_of_measure_desc`, `unit_of_measure_convert`, `stock_movements`, `barcodes`, `brands`, `labels`, `delays`, `extra`, `discounts`, `discounts_schedules`, `discounts_products`, `qrcodes`, `scannorder_settings`, `time_slots`, `delivery_session`, `printers`, `integration_uber_eats`, `integration_uber_direct`, `integration_deliveroo`, `integration_uber_eats_products_mapping`, `integration_uber_eats_options_mapping`, `integration_uber_eats_attributes_mapping`, `integration_deliveroo_products_mapping`, `integration_deliveroo_options_mapping`, `cash_registers`, `cash_registers_items`, `cash_registers_custom_items`, `cash_desks`, `audit_logs`, `external_tokens`, `firebase_fcm_access_token`, `app_version_merchant`, `available_languages`, `deletion_reasons`, `upsell_suggestions`, `deliveroo_*`, **`user_status_view`** (nom suggérant une **VUE** côté DB — à confirmer en prod, aucun `CREATE VIEW` dans le repo).

### 1.4 Inventaire des types MySQL-spécifiques (dans les migrations disponibles)

| Type | Occurrences | Équivalent PostgreSQL |
|---|---|---|
| `TINYINT(1)` (booléens) | **99** | `BOOLEAN` (ou `SMALLINT` si valeurs >1) |
| `BIGINT UNSIGNED` | 17 | `BIGINT` (perte du contrôle unsigned) |
| `INT UNSIGNED` | 6 | `INTEGER` ou `BIGINT` |
| `ENUM(...)` | **16 colonnes** (détail ci-dessous) | `CREATE TYPE ... AS ENUM` ou `VARCHAR + CHECK` |
| `JSON` | 6 colonnes | `JSONB` |
| `MEDIUMTEXT` | 1 | `TEXT` |
| `DECIMAL(p,s)` | ~22 | `NUMERIC(p,s)` (compatible) |
| `AUTO_INCREMENT` | 22 déclarations | `GENERATED ALWAYS AS IDENTITY` / `BIGSERIAL` |
| `ZEROFILL` | 0 | — |
| `SET(...)` | 0 | — |
| `ON UPDATE CURRENT_TIMESTAMP` | **33** | ⚠️ N'existe pas en PG → trigger `BEFORE UPDATE` requis |

Colonnes `ENUM` (fichier:ligne) :

- [migrations/063_floor_obstacles.up.sql:17](../../migrations/063_floor_obstacles.up.sql#L17) — `type ENUM('wall','bar','stairs','door')`
- [014_planning_socle.sql:125](../../migrations/done/014_planning_socle.sql#L125) — `role ENUM('employee','manager','admin')`
- [014_planning_socle.sql:161](../../migrations/done/014_planning_socle.sql#L161) — `type ENUM('permanent','temporary')`
- [010_haccp_temperature_tranche1.sql:144](../../migrations/done/010_haccp_temperature_tranche1.sql#L144) — `status ENUM('ok','alert','critical')`
- [011_haccp_cleaning_and_reception.sql:9,27](../../migrations/done/011_haccp_cleaning_and_reception.sql#L9) — `frequency_unit ENUM('day','week','month')`, `status ENUM('done')`
- [012_haccp_cleaning_redesign.sql:24](../../migrations/done/012_haccp_cleaning_redesign.sql#L24) — `frequency_unit ENUM('day','week','month')`
- [016_planning_shifts_core.sql:7,32](../../migrations/done/016_planning_shifts_core.sql#L7) — `status ENUM('draft','published','locked')`, `status ENUM('planned','confirmed','done','cancelled')`
- [018_planning_leave_and_swaps.sql:5,8,33](../../migrations/done/018_planning_leave_and_swaps.sql#L5) — `leave_type ENUM('paid','unpaid','sick','other')`, 2× `status ENUM('pending','approved','rejected','cancelled')`
- [037_kiosk_module.up.sql:20](../../migrations/done/037_kiosk_module.up.sql#L20) — `status ENUM('pending','active','inactive','revoked')`
- [041_cart_discounts.up.sql:7](../../migrations/done/041_cart_discounts.up.sql#L7) — `discount_scope ENUM('PRODUCT','ORDER_TOTAL')` (sur `discounts`)
- [049_upsell_suggestions_channel.up.sql:6](../../migrations/done/049_upsell_suggestions_channel.up.sql#L6) — `channel ENUM('POS','SNO','KIOSK')` (sur `upsell_suggestions`)
- [059_bookings_waitlist_sms.up.sql:29](../../migrations/done/059_bookings_waitlist_sms.up.sql#L29) — `status ENUM('waiting','notified','seated','expired','cancelled')`

⚠️ Les tables historiques (hors repo) peuvent contenir d'autres ENUM/UNSIGNED — à inventorier depuis le dump prod.

Colonnes `JSON` : `locations.attributes` (064), `goods_receipts.non_conformities` (011), `printers.production_product_ids` (043), `merchant_parameters.customer_form_requirements` (047), `floor_areas.points` (050), `booking_events.metadata` (059).

### 1.5 Contraintes et index (migrations disponibles)

- **FOREIGN KEY : 27** déclarations (planning, HACCP, kiosk, bookings principalement). Les FK des tables historiques sont inconnues côté repo.
- **UNIQUE KEY/INDEX : 21** (+ contraintes UNIQUE inline).
- **Index secondaires (`KEY`/`INDEX` inline) : ~103**, + 4 `CREATE INDEX`.
- **FULLTEXT : 0. Index spatiaux : 0.**

### 1.6 Triggers, procédures stockées, vues

Grep `CREATE TRIGGER|CREATE PROCEDURE|CREATE VIEW|CREATE FUNCTION` sur tous les `.sql` et `.go` : **0 occurrence dans le repo**. Point d'attention unique : `user_status_view` interrogée par le code (probable vue créée manuellement en prod) — vérifier avec `SHOW FULL TABLES WHERE Table_type = 'VIEW'`.

---

## 2. Requêtes SQL dans le code Go

### 2.1 Volumétrie

Grep sur `.Query( / .QueryRow( / .Exec( / *Context(` (db et tx confondus, via le helper `dbutils.GetDB`) : **~1 050 sites d'appel dans ~100 fichiers** (léger surcomptage : quelques `.Exec` non-SQL et fichiers de test inclus).

Fichiers les plus denses :

| Fichier | Appels |
|---|---|
| `internal/modules/menu/repository.go` | 107 |
| `internal/modules/order_life_cycle/repository.go` | 68 |
| `internal/modules/delivery_sessions/repository.go` | 42 |
| `internal/modules/customers/repository.go` | 41 |
| `internal/modules/haccp/repository.go` | 40 |
| `internal/modules/kiosk/repository.go` | 37 |
| `internal/modules/bookings/repository.go` | 36 |
| `internal/modules/cash_registers/repository.go` | 32 |
| `internal/modules/pos/repository.go`, `internal/modules/stocks/repository.go` | 28 chacun |
| `internal/modules/scannorder/repository.go` | 25 |
| `internal/webhook/stripe/repository.go` | 24 |
| `internal/modules/locations/repository.go` | 22 |
| `internal/modules/ubereats/repository.go`, `internal/webhook/deliveroo_orders/repository.go` | 21 chacun |

### 2.2 Placeholders

Tout le code utilise le placeholder MySQL **`?`**. Aucune occurrence de `$1`. La conversion `?` → `$n` concerne donc la quasi-totalité des ~1 050 requêtes (automatisable pour les chaînes statiques ; voir 2.3 pour les cas risqués).

### 2.3 Patterns de construction

**a) Chaîne statique (le cas majoritaire, ~90 %)** — conversion mécanique :

```go
// internal/modules/googlemaps/repository.go:37
`INSERT INTO merchant_google_maps_monthly (...)
 VALUES(?, DATE_FORMAT(UTC_TIMESTAMP(), '%Y-%m-01'), ?)
 ON DUPLICATE KEY UPDATE ...`
```

**b) `fmt.Sprintf` + placeholders dynamiques (`IN (?,?,...)`)** — **128 occurrences du pattern `strings.Repeat("?,"...)` / `placeholders` dans 18 fichiers** ([menu/repository.go](../../internal/modules/menu/repository.go) 22×, [orders/repository.go](../../internal/modules/orders/repository.go) 23×, [order_life_cycle/repository.go](../../internal/modules/order_life_cycle/repository.go) 12×, bookings 9×, etc.). Exemple :

```go
// internal/modules/bookings/repository.go:763-785
placeholders := strings.TrimSuffix(strings.Repeat("?,", len(locationIDs)), ",")
query := fmt.Sprintf(`SELECT ... WHERE bl.location_id IN (%s) AND b.merchant_id = ? ... FOR UPDATE`, placeholders)
```

⚠️ **Cas les plus risqués pour la conversion `$1..$n`** : le nombre de `?` varie à l'exécution. Il faudra soit générer `$1..$n` dynamiquement, soit passer par `pgx` + `ANY($1)` avec un slice, soit un helper de réécriture.

**c) SET dynamique (colonnes conditionnelles)** :

```go
// internal/modules/menu/repository.go:3657
query := fmt.Sprintf(`UPDATE marketing_categories SET %s WHERE id = ? AND merchant_id = ?...`, strings.Join(fields, ", "))
```

**d) Bulk insert construit** ([menu/repository.go:3443-3456](../../internal/modules/menu/repository.go#L3443)) : `INSERT IGNORE INTO product_tags ... VALUES (?,?),(?,?),...` — doublement MySQL-spécifique (`INSERT IGNORE` + placeholders variables).

**e) ⚠️ Query builder maison avec valeurs interpolées SANS placeholder** — `internal/modules/orders/orders_fetcher_builder.go` : `FetchAndBuildOrders(ctx, merchantID, whereFilters, orderByFilter, limitsFilters string)` concatène des fragments SQL bruts dans **10 requêtes**. Les appelants ([orders/repository.go:80-129](../../internal/modules/orders/repository.go#L80)) construisent les filtres par interpolation directe de valeurs :

```go
filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)          // ligne 96
filterOptimized := fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr) // lignes 75-84, IDs quotés à la main
```

C'est à la fois le point le plus risqué de la migration (fragments à auditer un par un) **et une surface d'injection SQL existante** à corriger à cette occasion.

---

## 3. Fonctions et syntaxe MySQL-spécifiques

### 3.1 `ON DUPLICATE KEY UPDATE` — ~25 occurrences Go dans 15 fichiers (+5 dans les migrations)

À convertir en `INSERT ... ON CONFLICT (...) DO UPDATE SET col = EXCLUDED.col` (⚠️ nécessite de connaître la contrainte unique cible, et `VALUES(col)` → `EXCLUDED.col`) :

| Fichier:ligne | Extrait |
|---|---|
| [auth/repository.go:633](../../internal/modules/auth/repository.go#L633) | upsert users_devices (fcm_token) |
| [bookings/repository.go:219, 497](../../internal/modules/bookings/repository.go#L219) | upsert bookings/booked_location |
| [cash_registers/repository.go:1164](../../internal/modules/cash_registers/repository.go#L1164) | upsert items |
| [googlemaps/repository.go:38](../../internal/modules/googlemaps/repository.go#L38) | compteur mensuel |
| [menu/repository.go:3732, 3809, 4098](../../internal/modules/menu/repository.go#L3732) | `... UPDATE marketing_category_id = VALUES(marketing_category_id), updated_at = CURRENT_TIMESTAMP` |
| [pos/repository.go:632](../../internal/modules/pos/repository.go#L632) | upsert |
| [messaggio/marketing_repository.go:73](../../internal/modules/messaggio/marketing_repository.go#L73) | compteur SMS mensuel |
| [ubereats/repository.go:95, 108](../../internal/modules/ubereats/repository.go#L95) | `UPDATE expires_at = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND), access_token = ?` |
| [kiosk/repository.go:356](../../internal/modules/kiosk/repository.go#L356) | upsert kiosk_settings |
| [translation/repository.go:153, 204](../../internal/modules/translation/repository.go#L153) | `UPDATE enabled = VALUES(enabled)` |
| [tasks/distribution.go:88](../../internal/tasks/distribution.go#L88) | `UPDATE distribution_time = ?` |
| [order_life_cycle/repository.go:1312, 1457, 1853, 1870](../../internal/modules/order_life_cycle/repository.go#L1312) | `quantity=VALUES(quantity)` ; `content = ?, creation_date = UTC_TIMESTAMP()` |
| [planning/revenueforecast/repository.go:26](../../internal/modules/planning/revenueforecast/repository.go#L26) | `amount_ht_cents = VALUES(amount_ht_cents)` |
| Migrations : 014 (×4), 021 | seeds idempotents |

### 3.2 `GROUP_CONCAT` — 2 occurrences

→ `STRING_AGG(DISTINCT col, sep ORDER BY col)` :

- [bookings/repository.go:615](../../internal/modules/bookings/repository.go#L615) : `GROUP_CONCAT(DISTINCT l.location_name ORDER BY l.location_name SEPARATOR '||')`
- [menu/repository.go:3553](../../internal/modules/menu/repository.go#L3553) : `COALESCE(GROUP_CONCAT(DISTINCT pmc.product_id ... SEPARATOR ','), '')`

### 3.3 `IFNULL` — 9 occurrences (→ `COALESCE`, trivial)

[webhook/stripe/repository.go:115](../../internal/webhook/stripe/repository.go#L115), [order_life_cycle/repository.go:907](../../internal/modules/order_life_cycle/repository.go#L907), [pos/accounting/repository.go:140, 374-376](../../internal/modules/pos/accounting/repository.go#L140), [pos/reports/repository.go:32](../../internal/modules/pos/reports/repository.go#L32), [stats/repository.go:333-334](../../internal/modules/stats/repository.go#L333). Exemple : `((oi.price + IFNULL(e.extra_price, 0)) * oi.quantity)`. Aucun `IF(...)` SQL détecté (le code utilise `CASE WHEN`). `COALESCE`/`NULLIF` déjà massivement utilisés (portables).

### 3.4 Fonctions de date — **~213 occurrences dans 44 fichiers** (le plus gros chantier de réécriture)

| Fonction | Exemples | Conversion PG |
|---|---|---|
| `UTC_TIMESTAMP()` (et `UTC_TIMESTAMP` sans parenthèses, ex. [ubereats/repository.go:94](../../internal/modules/ubereats/repository.go#L94)) | partout (defaults applicatifs, comparaisons) | `(NOW() AT TIME ZONE 'utc')` / `CURRENT_TIMESTAMP` si colonnes timestamptz |
| `DATE_SUB/DATE_ADD(x, INTERVAL ? UNIT)` | [tasks/upsell.go:78](../../internal/tasks/upsell.go#L78) `DATE_SUB(NOW(), INTERVAL ? DAY)` ; [notification_repository.go:116](../../internal/modules/notification/notification_repository.go#L116) `DATE_ADD(UTC_TIMESTAMP(), INTERVAL 50 MINUTE)` ; [scannorder/repository.go:83-101](../../internal/modules/scannorder/repository.go#L83) (8×) | `x - ($1 * INTERVAL '1 day')` — ⚠️ `INTERVAL ? DAY` paramétré n'a pas d'équivalent direct |
| `DATE_FORMAT(col, '%Y-%m-%d %H:%i:%s')` | bookings/repository.go:363-364, 611, 1204, 1276 ; waitlist_repository.go:51-54 ; pos/repository.go:489-490, 855-856 ; pos/reports (bornes de journée `'%Y-%m-%d 00:00:00'`) ; pos/accounting:365-419 ; stocks:715 ; googlemaps:37 (`'%Y-%m-01'`) ; stats:125 | `TO_CHAR(col, 'YYYY-MM-DD HH24:MI:SS')` / `date_trunc` |
| `TIMESTAMPDIFF(unit, a, b)` | tasks/orders.go:35-94, tasks/payments.go:24-41, bookings:835, 973, planning/performance:44-103, distribution.go | `EXTRACT(EPOCH FROM (b - a)) / n` |
| `TIMESTAMPADD` | [order_life_cycle/repository.go:907](../../internal/modules/order_life_cycle/repository.go#L907) | `a + n * INTERVAL '1 second'` |
| `CONVERT_TZ(col, '+00:00', ?)` | [stats/repository.go:125](../../internal/modules/stats/repository.go#L125), [planning/performance/repository.go:101](../../internal/modules/planning/performance/repository.go#L101) | `col AT TIME ZONE 'UTC' AT TIME ZONE $1` |
| `DAYOFWEEK` | [scannorder/repository.go:92](../../internal/modules/scannorder/repository.go#L92) (avec remapping dimanche=1 → 7) | `EXTRACT(ISODOW FROM ...)` (simplifie même la logique) |
| `UNIX_TIMESTAMP` | [tasks/distribution.go:23, 116](../../internal/tasks/distribution.go#L23) | `EXTRACT(EPOCH FROM ...)` |
| `STR_TO_DATE` | 0 occurrence | — |

### 3.5 `LAST_INSERT_ID` / `res.LastInsertId()` — **33 sites d'appel**

Aucun `LAST_INSERT_ID()` en SQL, mais 33 usages Go de `res.LastInsertId()` (webhook/deliveroo_orders, webhook/stripe, webhook/ubereats, bookingcore, cash_registers, delivery_sessions, customers, stocks, menu ×7, pos/create_repository, reservation, users, order_life_cycle ×7...). ⚠️ **Non supporté par les drivers PostgreSQL** → chaque `INSERT` concerné doit devenir `INSERT ... RETURNING id` + `QueryRow().Scan(&id)`. C'est un changement de code (pas juste de SQL) sur ~33 points.

### 3.6 `LIMIT x, y` (syntaxe virgule) — **0 occurrence** ✅ (uniquement `LIMIT n` / `LIMIT ? OFFSET ?`)

### 3.7 Opérateurs/fonctions JSON MySQL (`JSON_EXTRACT`, `->>`, `JSON_ARRAYAGG`...) — **0 occurrence** ✅

Les 6 colonnes JSON sont lues/écrites comme chaînes et (dé)sérialisées en Go — portable vers `JSONB` sans réécriture de requête.

### 3.8 `MATCH ... AGAINST` / FULLTEXT — **0 occurrence** ✅ · `LOCK IN SHARE MODE` — **0** ✅

En revanche **`SELECT ... FOR UPDATE` : ~10 requêtes** (compatible PG, mais la sémantique des verrous diffère — voir §4.3) : [webhook/ubereats/orders_repo.go:79](../../internal/webhook/ubereats/repository/orders_repo.go#L79), [audit/repository.go:68](../../internal/modules/audit/repository.go#L68) (chaîne de hash d'audit), [bookings/repository.go:785](../../internal/modules/bookings/repository.go#L785) (anti-double-booking, s'appuie sur les **gap locks InnoDB**, cf. commentaire `conflict_test.go:431` — ⚠️ PG n'a pas de gap locks : la protection contre les insertions concurrentes devra être repensée, la contrainte UNIQUE de 052 aide), [order_life_cycle/repository.go:165, 746, 785, 828](../../internal/modules/order_life_cycle/repository.go#L165), [receipt/repository.go:40](../../internal/modules/receipt/repository.go#L40) (numérotation séquentielle des reçus → un `SEQUENCE` PG serait plus simple).

### 3.9 ENUM en SQL applicatif

Les colonnes ENUM sont lues/écrites comme chaînes (`'POS'`, `'pending'`...) ; aucune fonction ENUM MySQL. Conversion transparente si on retient `VARCHAR + CHECK` côté PG.

### 3.10 Backticks — quasi absents ✅

Aucun backtick dans le SQL Go (les requêtes sont dans des raw strings Go délimitées par des backticks, ce qui les exclut de fait). Présents dans **7 fichiers de migration** (ex. 010, 037, 050-051) → à remplacer par des guillemets doubles ou rien.

### 3.11 `CONCAT()` — 15 occurrences (compatible PG, vigilance NULL)

`CONCAT` existe en PG. ⚠️ Différence : MySQL `CONCAT` retourne `NULL` si un argument est `NULL`, PG `CONCAT` ignore les `NULL` — les usages type [auth/repository.go:71](../../internal/modules/auth/repository.go#L71) (`CONCAT(m.street_number,' ',m.street,...)` pour l'adresse) changeront de comportement sur données NULL. Autres : users/admin_repository.go:19, 148 ; stats:403 ; haccp:1388, 1414 ; planning/swaps:85-87 ; planning/schedule:248 (déjà défensifs avec `COALESCE`). Aucun opérateur `||` MySQL (mode `PIPES_AS_CONCAT`) utilisé.

### 3.12 Divers

- `INSERT IGNORE` : 1 occurrence ([menu/repository.go:3994](../../internal/modules/menu/repository.go#L3994)) → `ON CONFLICT DO NOTHING`.
- `REPLACE INTO`, `STRAIGHT_JOIN`, `GET_LOCK`, `LOCK TABLES`, `SQL_CALC_FOUND_ROWS` : 0 ✅.
- `RAND()` : 0 dans le SQL.
- Comparaisons de chaînes : MySQL `utf8mb4_unicode_ci` est **insensible à la casse** ; PG est sensible par défaut → tout `WHERE email = ?`, recherche par nom, etc. peut changer de comportement (utiliser `CITEXT`, `LOWER()`, ou collation ICU).

---

## 4. Driver et connexion

### 4.1 Driver

`github.com/go-sql-driver/mysql v1.9.3` (go.mod), importé en side-effect uniquement dans [internal/database/mysql.go:8](../../internal/database/mysql.go#L8). Tout le reste du code ne dépend que de `database/sql` → le remplacement par `pgx/stdlib` ou `lib/pq` est localisé à ce fichier, **hors** les points listés en §3 (LastInsertId, placeholders).

### 4.2 Initialisation

[internal/database/mysql.go](../../internal/database/mysql.go) : `sql.Open("mysql", dsn.MySQLURL)` — le DSN vient **entièrement de l'env `MYSQL_URL`** ([internal/config/database.go](../../internal/config/database.go)) ; les options (`parseTime`, `charset`, `loc`, `multiStatements`) ne sont donc **pas visibles dans le repo**. Le code scanne des `time.Time` (31 occurrences dans les repositories) → `parseTime=true` est nécessairement présent dans le DSN de prod. Pool volontairement bridé (contrainte Hostinger) :

```go
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
db.SetConnMaxLifetime(3 * time.Minute)
db.SetConnMaxIdleTime(30 * time.Second) // < wait_timeout ~60s Hostinger
```

→ Opportunité : un PG managé permettrait de lever cette limite de 1 connexion (goulet d'étranglement actuel de toute l'API), mais attention, du code peut implicitement dépendre de la sérialisation totale des requêtes qu'elle impose.

### 4.3 Transactions

- Helper central [internal/utils/dbutils/run_in_tx.go](../../internal/utils/dbutils/run_in_tx.go) : `RunInTx` injecte le `*sql.Tx` dans le contexte ; les repositories récupèrent DB ou Tx via `dbutils.GetDB(ctx)`. **L'imbrication est gérée en réutilisant la tx existante (pas de SAVEPOINT)** — pattern portable tel quel.
- `BeginTx(ctx, nil)` partout : **aucun niveau d'isolation explicite, aucun savepoint**. MySQL InnoDB = REPEATABLE READ par défaut, PostgreSQL = READ COMMITTED → revalider les flux sensibles (bookings `FOR UPDATE` + gap locks §3.8, chaîne d'audit, numérotation des reçus).
- Tests d'intégration bookings ([conflict_test.go:304](../../internal/modules/bookings/conflict_test.go#L304)) : DSN MySQL en dur `root@tcp(127.0.0.1:33077)/bookings_test?multiStatements=true` — à porter vers PG (et `multiStatements` n'existe pas tel quel).

---

## 5. Migrations de schéma existantes

- **Aucun outil de migration** (pas de golang-migrate, goose, etc. dans go.mod). **Aucune table de tracking** (`schema_migrations` introuvable dans le code et les SQL). Le suivi est **par convention de fichiers** : les migrations appliquées sont déplacées dans `migrations/done/` (100 fichiers), les en attente restent à la racine de `migrations/` (3 fichiers au moment de l'audit : `062_location_varchar_ids.sql`, `063_floor_obstacles.up.sql`, `064_locations_attributes.up.sql`).
- Nommage : `NNN_description.sql` (001–025), puis paires `NNN_description.up.sql` / `.down.sql` à partir de 026 (37 fichiers `.down.sql`). ⚠️ Numéros dupliqués : deux `024_*` et deux `033_*`.
- Exécution manuelle (cf. CLAUDE.md). → La migration PG est l'occasion d'introduire un vrai outil (golang-migrate/goose) avec table de tracking.
- Note : les IDs métier récents sont des **VARCHAR générés côté application** (`prefix-uuid`, cf. [internal/helpers/ids.go](../../internal/helpers/ids.go)) — ces tables ne dépendent pas d'AUTO_INCREMENT ; les 22 `AUTO_INCREMENT` restants + les tables historiques (orders, payments, bookings...) si.

---

## 6. Volumétrie

**Non mesurable depuis le repo** : aucun accès à une base dev/staging depuis cet environnement (pas de `.env`, `MYSQL_URL` non défini localement), et aucun dump/fixture volumétrique versionné. À collecter en prod avant migration :

```sql
SELECT table_name, table_rows, ROUND(data_length/1024/1024) AS data_mb, engine, table_collation
FROM information_schema.tables
WHERE table_schema = DATABASE()
ORDER BY table_rows DESC;
```

Tables attendues comme les plus volumineuses (d'après le rôle métier) : `orderitems`, `orders`, `payments`, `order_item_configuration`, `audit_logs`, `stock_movements`, `temperature_readings`, `planning_time_entries`.

---

## Synthèse des risques (par ordre décroissant)

1. **DDL des tables historiques absent du repo** — bloquant, dump prod requis (types réels, FK, index, la vue `user_status_view`).
2. **Conversion `?` → `$n` sur ~1 050 requêtes**, dont 128 constructions dynamiques `IN (%s)` et le builder `orders_fetcher_builder.go` qui interpole des valeurs sans placeholder (injection SQL à corriger au passage).
3. **~213 usages de fonctions de date MySQL** (DATE_FORMAT/DATE_ADD/TIMESTAMPDIFF/UTC_TIMESTAMP/CONVERT_TZ) + 33 `ON UPDATE CURRENT_TIMESTAMP` à remplacer par des triggers.
4. **33 `res.LastInsertId()`** → réécriture en `INSERT ... RETURNING`.
5. **~25 `ON DUPLICATE KEY UPDATE`** → `ON CONFLICT`, nécessite l'inventaire des contraintes uniques (dump prod).
6. **Verrouillage** : gap locks InnoDB (anti-double-booking) et isolation REPEATABLE READ implicite sans équivalent direct en PG.
7. **Sémantique** : collation insensible à la casse, `CONCAT` vs NULL, `TINYINT(1)` vs `BOOLEAN`, 16 colonnes ENUM.
8. Points faciles : pas de FULLTEXT, pas de JSON path MySQL, pas de `LIMIT x,y`, pas de procédures/triggers/vues dans le repo, driver isolé dans un seul fichier, helper de transaction central portable.
