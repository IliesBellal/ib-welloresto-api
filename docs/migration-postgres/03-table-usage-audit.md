# Audit d'usage des tables — 180 tables prod vs code — 03

> Fait suite à [01-audit.md](01-audit.md) (audit général MySQL→Postgres) et [02-security-fix-orders-builder.md](02-security-fix-orders-builder.md). Objectif : confronter la liste des 180 tables de production à un grep exhaustif de `FROM` / `JOIN` / `INSERT INTO` / `UPDATE` / `DELETE FROM` sur tout `internal/`, `cmd/` et `migrations/*.sql`, pour identifier les tables mortes avant la migration Postgres (inutile de porter ce qu'on ne va pas migrer). Aucun fichier source modifié — audit en lecture seule.

**Méthode** : grep insensible à la casse sur les motifs `(FROM|JOIN|INTO|UPDATE)\s+\`?<table>\`?` dans tout `.go`/`.sql` sous `internal/`, `cmd/`, `migrations/`, avec vérification manuelle des cas ambigus (mots génériques, tables voisines par le nom) et vérification complémentaire des `CREATE TABLE` dans `migrations/` pour les tables non trouvées via les verbes DML.

**Résultat global : 143 / 180 tables référencées dans le code ou les migrations, 37 candidates orphelines.**

---

## 1. Tables référencées dans le code (143/180)

Regroupées par domaine fonctionnel. "Fichiers" liste jusqu'à 4 emplacements représentatifs (voir le repo pour la liste complète si une table est utilisée dans plus de fichiers).

### Auth / merchant / abonnements

| Table | Fichiers |
|---|---|
| `app_version` | `internal/modules/auth/repository.go` |
| `app_version_merchant` | `internal/modules/auth/repository.go` |
| `merchant` | `internal/modules/auth/repository.go`, `internal/modules/bookings/*.go` (24 fichiers) |
| `merchant_parameters` | `internal/modules/auth/repository.go`, `internal/modules/cash_registers/repository.go`, `internal/modules/integrations/repository.go`, `internal/modules/menu/repository.go` (17 fichiers) |
| `merchant_marketing_settings` | `internal/modules/messaggio/marketing_repository.go`, `internal/modules/pos/create_repository.go`, `internal/modules/pos/repository.go` |
| `merchant_sms_monthly` | `internal/modules/messaggio/marketing_repository.go` |
| `merchant_google_maps_monthly` | `internal/modules/googlemaps/repository.go` |
| `merchant_translation_languages` | `internal/modules/translation/repository.go` |
| `available_languages` | `internal/modules/translation/repository.go` |
| `users` | `internal/modules/auth/*.go`, `internal/modules/bookings/*.go` (17 fichiers) |
| `users_rights` | `internal/modules/auth/repository.go`, `internal/modules/cash_registers/repository.go`, `internal/modules/delivery_sessions/repository.go` (13 fichiers) |
| `users_devices` | `internal/modules/auth/repository.go`, `internal/modules/notification/notification_repository.go` |
| `packages` | `internal/modules/auth/repository.go`, `internal/modules/users/repository.go`, `internal/tasks/orders.go`, `migrations/done/019_add_subscription_feature_flags.sql` |
| `subscriptions` | `internal/modules/auth/repository.go`, `internal/modules/kiosk/repository.go`, `internal/modules/pos/create_repository.go`, `internal/modules/users/repository.go` |
| `subscription_invoices` | `internal/webhook/stripe/repository.go` |
| `audit_logs` | `internal/modules/audit/repository.go` |
| `api_request_logs` | `internal/middleware/request_logger/logger.go` |
| `firebase_fcm_access_token` | `internal/modules/notification/notification_repository.go` |
| `external_tokens` | `internal/modules/ubereats/repository.go` |
| `device_link` | `internal/modules/cash_registers/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/user_services/repository.go` |
| `welloresto_stripe_customers` | `internal/webhook/stripe/repository.go` |
| `stripe_accounts` | `internal/infrastructure/stripe/terminal.go`, `internal/modules/integrations/repository.go`, `internal/modules/kiosk/repository.go`, `internal/modules/order_life_cycle/repository.go` |
| `stripe_payments` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/scannorder/repository.go`, `internal/tasks/payments.go`, `internal/webhook/stripe/repository.go` |

### Menu / produits / stock

| Table | Fichiers |
|---|---|
| `products` | `internal/modules/customers/repository.go`, `internal/modules/discounts/repository.go`, `internal/modules/kiosk/repository.go`, `internal/modules/menu/repository.go` (16 fichiers) |
| `productcateg` | `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/stocks/repository.go` |
| `product_allergens` | `internal/modules/menu/repository.go` |
| `product_configurable_attribute` | `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/webhook/deliveroo_orders/repository.go` |
| `product_marketing_categories` | `internal/modules/menu/repository.go` |
| `product_tags` | `internal/modules/menu/repository.go`, `internal/modules/tags/repository.go` |
| `marketing_categories` | `internal/modules/menu/repository.go` |
| `tags` | `internal/modules/menu/repository.go`, `internal/modules/tags/repository.go` |
| `allergens` | `internal/modules/allergens/repository.go`, `internal/modules/menu/repository.go` |
| `configurable_attributes` | `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/webhook/deliveroo_orders/repository.go`, `internal/webhook/ubereats/repository/attribute_mapping_repo.go` |
| `configurable_attribute_options` | `internal/modules/kiosk/repository.go`, `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `components` | `internal/modules/haccp/repository.go`, `internal/modules/menu/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` |
| `component_category` | `internal/modules/haccp/repository.go`, `internal/modules/menu/repository.go`, `internal/modules/stocks/repository.go` |
| `recipes` | `internal/modules/menu/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `requires` | `internal/modules/menu/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `unit_of_measure` | `internal/modules/menu/repository.go`, `internal/modules/stocks/repository.go` |
| `unit_of_measure_convert` | `internal/modules/menu/repository.go`, `internal/modules/stocks/repository.go` |
| `unit_of_measure_desc` | `internal/modules/haccp/repository.go`, `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/stocks/repository.go` |
| `barcodes` | `internal/modules/stocks/repository.go` |
| `purchased_components` | `internal/modules/stocks/repository.go` |
| `expiration_dates` | `internal/modules/stocks/repository.go` |
| `stock_movements` | `internal/modules/stocks/repository.go`, `migrations/done/004_migrate_stock_movements_text_values.sql` |
| `tva_categories` | `internal/modules/menu/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go`, `internal/modules/pos/accounting/repository.go` |
| `discounts` | `internal/modules/discounts/repository.go`, `internal/modules/kiosk/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `discounts_products` | `internal/modules/discounts/repository.go`, `internal/modules/orders/repository.go` |
| `discounts_products_options` | `internal/modules/orders/repository.go` |
| `discounts_schedules` | `internal/modules/discounts/repository.go`, `internal/modules/kiosk/repository.go`, `internal/modules/orders/repository.go`, `internal/modules/scannorder/repository.go` |

### Commandes / paiements / caisse

| Table | Fichiers |
|---|---|
| `orders` | `internal/modules/cash_registers/repository.go`, `internal/modules/customers/repository.go`, `internal/modules/deliveroo/repository.go`, `internal/modules/delivery_sessions/repository.go` (25 fichiers) |
| `orderitems` | `internal/modules/customers/repository.go`, `internal/modules/deliveroo/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` (15 fichiers) |
| `order_comments` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` |
| `order_item_configuration` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` |
| `order_location` | `internal/modules/locations/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/scannorder/repository.go` |
| `extra` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/pos/accounting/repository.go`, `internal/modules/pos/reports/repository.go` |
| `without` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/stocks/repository.go` |
| `session_orderitem` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` |
| `payments` | `internal/modules/cash_registers/repository.go`, `internal/modules/delivery_sessions/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/order_life_cycle/service.go` (13 fichiers) |
| `receipts` | `internal/modules/receipt/repository.go` |
| `restaurant_ticket` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/pos/repository.go` |
| `cash_registers` | `internal/modules/cash_registers/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/user_services/repository.go` |
| `cash_registers_items` | `internal/modules/cash_registers/repository.go` |
| `cash_registers_custom_items` | `internal/modules/cash_registers/repository.go` |
| `cash_desks` | `internal/modules/cash_registers/repository.go`, `internal/modules/pos/create_repository.go`, `internal/modules/user_services/repository.go` |
| `sub_cash_registers` | `internal/modules/user_services/repository.go` |
| `services_performed` | `internal/modules/user_services/repository.go` |
| `qrcodes` | `internal/modules/kiosk/repository.go`, `internal/modules/messaggio/marketing_repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/pos/create_repository.go` |
| `scannorder_session` | `internal/modules/orders/orders_fetcher_builder.go` |
| `scannorder_settings` | `internal/modules/auth/repository.go`, `internal/modules/integrations/repository.go`, `internal/modules/pos/create_repository.go`, `internal/modules/pos/repository.go` |
| `delivery_session` | `internal/modules/delivery_sessions/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `delivery_session_order` | `internal/modules/delivery_sessions/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/orders_fetcher_builder.go`, `internal/modules/orders/repository.go` |
| `delivery_position` | `internal/modules/users/repository.go` |
| `deletion_reasons` | `internal/modules/bookings/repository.go`, `internal/modules/pos/repository.go` |
| `delays` | `internal/modules/menu/repository.go`, `internal/modules/order_life_cycle/repository.go` |
| `printers` | `internal/modules/printers/repository.go` |
| `upsell_suggestions` | `internal/modules/upsell/repository.go` |
| `average_distribution_time` | `internal/tasks/distribution.go` |
| `labels` | `internal/modules/cash_registers/repository.go`, `internal/modules/pos/accounting/repository.go`, `internal/modules/pos/reports/repository.go`, `internal/modules/pos/repository.go` |

### Clients / fidélité

| Table | Fichiers |
|---|---|
| `customer` | `internal/modules/bookings/*.go`, `internal/modules/customers/repository.go` (14 fichiers) |
| `customer_loyalty_programs` | `internal/modules/customers/repository.go`, `internal/modules/orders/repository.go`, `internal/modules/scannorder/repository.go` |
| `customer_loyalty_program_reward_products` | `internal/modules/customers/repository.go`, `internal/modules/orders/repository.go` |
| `customer_loyalty_program_target_products` | `internal/modules/customers/repository.go` |
| `customer_loyalty_progress` | `internal/modules/customers/repository.go` |
| `customer_loyalty_progress_order` | `internal/modules/customers/repository.go` |
| `customer_rewards` | `internal/modules/customers/repository.go`, `internal/modules/order_life_cycle/repository.go`, `internal/modules/orders/repository.go`, `internal/modules/reservation/repository.go` |

### Intégrations (Uber Eats / Deliveroo)

| Table | Fichiers |
|---|---|
| `integration_uber_eats` | `internal/modules/auth/repository.go`, `internal/modules/integrations/repository.go`, `internal/modules/pos/repository.go`, `internal/modules/ubereats/repository.go` |
| `integration_uber_eats_attributes_mapping` | `internal/webhook/ubereats/repository/attribute_mapping_repo.go` |
| `integration_uber_eats_options_mapping` | `internal/webhook/ubereats/repository/attribute_mapping_repo.go` |
| `integration_uber_eats_products_mapping` | `internal/modules/integrations/repository.go`, `internal/webhook/ubereats/repository/product_mapping_repo.go` |
| `integration_uber_direct` | `internal/modules/auth/repository.go`, `internal/modules/users/repository.go` |
| `integration_deliveroo` | `internal/modules/auth/repository.go`, `internal/modules/deliveroo/repository.go`, `internal/modules/integrations/repository.go`, `internal/modules/users/repository.go` |
| `integration_deliveroo_options_mapping` | `internal/webhook/deliveroo_orders/repository.go` |
| `integration_deliveroo_products_mapping` | `internal/modules/integrations/repository.go`, `internal/webhook/deliveroo_orders/repository.go` |

⚠️ Voir §2bis : `integration_deliveroo_attributes_mapping`, `integration_deliveroo_components_mapping` et `integration_uber_eats_components_mapping` sont les tables jumelles de celles ci-dessus, mais n'apparaissent nulle part dans le code — le mapping au niveau attribut/composant n'est pas implémenté côté Deliveroo/Uber Eats, seuls option et produit le sont.

### Kiosk / floorplan / locations

| Table | Fichiers |
|---|---|
| `locations` | `internal/modules/bookings/bookings_fetcher.go`, `internal/modules/bookings/repository.go`, `internal/modules/locations/repository.go`, `internal/modules/orders/orders_fetcher_builder.go` |
| `floors` | `internal/modules/locations/repository.go`, `migrations/062_location_varchar_ids.sql` |
| `floor_areas` | `internal/modules/locations/repository.go` |
| `floor_obstacles` | `internal/modules/locations/repository.go` |
| `kiosks` | `internal/modules/kiosk/repository.go`, `migrations/done/040_kiosk_simplify_ids.{up,down}.sql` |
| `kiosk_device_tokens` | `internal/modules/kiosk/repository.go`, `migrations/done/040_kiosk_simplify_ids.{up,down}.sql` |
| `kiosk_enrollment_codes` | `internal/modules/kiosk/repository.go`, `migrations/done/040_kiosk_simplify_ids.{up,down}.sql` |
| `kiosk_settings` | `internal/modules/kiosk/repository.go` |

### Réservations (bookings)

| Table | Fichiers |
|---|---|
| `bookings` | `internal/modules/bookingcore/create.go`, `internal/modules/bookings/*.go` (14 fichiers) |
| `bookings_settings` | `internal/modules/bookings/bookings_fetcher.go`, `internal/modules/bookings/repository.go`, `internal/modules/pos/create_repository.go`, `internal/modules/reservation/repository.go` |
| `booked_location` | `internal/modules/bookings/*.go`, `internal/modules/locations/repository.go` (7 fichiers) |
| `booking_duration_rules` | `internal/modules/bookings/repository.go`, `internal/modules/reservation/repository.go` |
| `booking_events` | `internal/modules/bookingevents/events.go` |
| `booking_waitlist` | `internal/modules/bookings/waitlist_repository.go` |
| `hours_of_operation` | `internal/modules/bookings/repository.go`, `internal/modules/pos/repository.go`, `internal/modules/reservation/repository.go`, `internal/modules/scannorder/repository.go` |

### HACCP

| Table | Fichiers |
|---|---|
| `cleaning_zones`, `cleaning_surfaces`, `cleaning_sessions`, `cleaning_executions` | `internal/modules/haccp/repository.go` |
| `temperature_zones`, `temperature_readings`, `temperature_sessions`, `temperature_reading_corrective_actions` | `internal/modules/haccp/repository.go` |
| `haccp_settings` | `internal/modules/haccp/repository.go`, `internal/modules/pos/create_repository.go` |
| `haccp_corrective_actions` | `internal/modules/haccp/repository.go`, `migrations/done/020_haccp_temperature_corrective_actions.sql`, `migrations/done/021_haccp_corrective_actions_french_catalog.sql` |
| `goods_receipts` | `internal/modules/haccp/repository.go` |

### Planning

| Table | Fichiers |
|---|---|
| `employees` | `internal/modules/planning/employees/*.go` (13 fichiers) |
| `employee_documents` | `internal/modules/planning/documents/repository.go` |
| `planning_positions` | `internal/modules/planning/employees/*.go`, `internal/modules/planning/schedule/repository.go` (10 fichiers) |
| `planning_settings` | `internal/modules/planning/settings/repository.go`, `migrations/done/023_planning_shift_swap_approval_mode.sql` |
| `planning_shifts` | `internal/modules/planning/leave/repository.go`, `internal/modules/planning/performance/repository.go` (11 fichiers) |
| `planning_shift_templates` | `internal/modules/planning/shifttemplates/repository.go` |
| `planning_shift_swap_requests` | `internal/modules/planning/swaps/repository.go` |
| `planning_time_entries` | `internal/modules/planning/timeentries/repository.go`, `internal/modules/planning/performance/repository.go` |
| `planning_leave_requests` | `internal/modules/planning/leave/repository.go` |
| `planning_weeks` | `internal/modules/planning/schedule/repository.go`, `migrations/done/024_add_planning_weeks_published_at.sql` |
| `planning_week_templates` | `internal/modules/planning/weektemplates/repository.go` |
| `planning_week_template_shifts` | `internal/modules/planning/weektemplates/repository.go` |
| `planning_revenue_forecasts` | `internal/modules/planning/performance/repository.go`, `internal/modules/planning/revenueforecast/repository.go` |
| `planning_holiday_overrides` | `internal/modules/planning/settings/holidays_repository.go` |
| `holiday_calendar` | `internal/modules/planning/settings/holidays_repository.go` |
| `labor_rules` | `internal/modules/planning/settings/repository.go`, `migrations/done/014_planning_socle.sql` |
| `sys_attendance_sources` | `internal/modules/planning/refs/repository.go`, `migrations/done/014_planning_socle.sql` |
| `sys_contract_types` | `migrations/done/014_planning_socle.sql` (créée, pas de lecture directe repérée dans une requête `SELECT`/`JOIN` du code Go — probablement consultée uniquement via l'ORM implicite des jointures `employees`) |
| `sys_planning_event_types` | `migrations/done/014_planning_socle.sql` (idem) |

---

## 2. Tables candidates "legacy / orphelines" (37/180)

Aucune occurrence de `FROM`/`JOIN`/`INSERT INTO`/`UPDATE`/`DELETE FROM` (ni de `CREATE TABLE`, sauf mention contraire) sur ces noms, dans tout `internal/`, `cmd/` et `migrations/*.sql` :

```
api_calls
average_distribution_time_by_category
average_distribution_time_history
broadcast_list
calendar
cash_funds
cash_reports
category_discount
checkout_orderitems
consumables
customer_advertisement_emails
employment_agreement
employment_contract
integration_deliveroo_attributes_mapping
integration_deliveroo_components_mapping
integration_uber_eats_components_mapping
integration_uber_eats_reports
invoices
merchant_code
migration_users
notifications
order_changes_log
order_ratings
pictures
planned_shifts
planning_roles
product_ratings
shift_templates
shift_templates_items
stock_evolution_records
stock_movements_desc
stock_movements_source
timezone_info
users_nfc_tags
user_vacations
z_platform_daily_activity_recording
```

**Cas particulier — `hours_amendments`** : à part, car cette table **est** créée dans `migrations/done/014_planning_socle.sql:157` (`CREATE TABLE IF NOT EXISTS hours_amendments`), donc *pas* une orpheline au sens strict "absente des migrations". Mais elle n'est référencée par **aucune** requête Go (`FROM`/`JOIN`/`INSERT`/`UPDATE`/`DELETE` : zéro résultat). C'est une table créée pour un besoin du module planning (probablement pour tracer des corrections manuelles d'heures) mais jamais consommée par le code actuel — candidate à la suppression ou à l'implémentation manquante, à trancher avec l'équipe produit avant la migration.

### Pourquoi ces 37 tables sont plausiblement orphelines

- **Analytics / logs plateforme, hors périmètre de cette API** : `api_calls`, `z_platform_daily_activity_recording`, `order_changes_log` — noms qui évoquent des tables alimentées par un autre composant (ancien back PHP, outil interne, ou trigger MySQL) plutôt que par cette API Go.
- **Fonctionnalités jamais branchées côté intégrations livraison** : `integration_deliveroo_attributes_mapping`, `integration_deliveroo_components_mapping`, `integration_uber_eats_components_mapping`, `integration_uber_eats_reports` — leurs tables jumelles "options"/"products"/"attributes" sont actives (§1), seul le niveau composant/reporting ne l'est pas.
- **Reliquats de fonctionnalités non poursuivies** : `order_ratings`, `product_ratings`, `broadcast_list`, `pictures`, `notifications`, `checkout_orderitems`, `category_discount`, `consumables`, `cash_funds`, `cash_reports`, `merchant_code`, `timezone_info`, `users_nfc_tags`, `user_vacations`, `customer_advertisement_emails`, `employment_agreement`, `employment_contract`, `calendar`.
- **Migration de données ponctuelle déjà terminée** : `migration_users` (nom explicite d'une table de transit pour une migration passée, pas un table métier récurrente).
- **Confirmé obsolète par une migration existante** : `stock_movements_desc`, `stock_movements_source` — voir doute documenté ci-dessous, ce n'est ici pas un doute mais une quasi-certitude.
- **Historique statistique remplacé** : `average_distribution_time_by_category`, `average_distribution_time_history` — la table courante `average_distribution_time` est bien utilisée (`internal/tasks/distribution.go`), mais ses variantes "par catégorie"/"historique" ne sont lues/écrites par aucun code Go actuel. Noter que `OrdersRepository.GetEstimatedDistributionTime` (`internal/modules/orders/repository.go:857`) appelle `CALL GET_AVERAGE_DISTRIBUTION_TIME(?, ?)` — une **procédure stockée MySQL** dont le corps n'est pas dans ce repo. Il n'est pas exclu que cette procédure lise en interne `average_distribution_time_by_category` et/ou `_history` ; ce grep ne peut pas le voir. **À vérifier via `SHOW CREATE PROCEDURE GET_AVERAGE_DISTRIBUTION_TIME` côté prod avant de les déclarer mortes.**
- **`planned_shifts`, `planning_roles`, `shift_templates`, `shift_templates_items`** : voir §2bis, doutes de renommage vers le module `planning` actuel.

---

## 2bis. Doutes de renommage (à vérifier avec l'équipe, pas des certitudes)

Pour certaines tables orphelines, le nom suggère un lien avec un module existant — probablement un ancêtre remplacé lors d'une refonte, mais **ceci reste une hypothèse basée sur la ressemblance de nom**, pas une preuve :

| Table orpheline | Successeur probable (actif, cf. §1) | Doute |
|---|---|---|
| `planned_shifts` | `planning_shifts` (créée par `migrations/done/016_planning_shifts_core.sql`) | Nom quasi identique ; `planned_shifts` est probablement l'ancienne table "planning" pré-refonte (module `internal/modules/planning/` visiblement reconstruit de zéro avec préfixe `planning_*` depuis la migration 014). À confirmer : mêmes colonnes ? |
| `planning_roles` | `planning_positions` (créée par `migrations/done/022_planning_positions.sql`) | Les deux décrivent un référentiel de rôles/postes ; `planning_positions` a un nom plus récent et actif. Doute : `planning_roles` pourrait être le prédécesseur direct. |
| `shift_templates`, `shift_templates_items` | `planning_shift_templates` (créée par `migrations/done/028_planning_shift_templates.up.sql`) | Nom quasi identique sans le préfixe `planning_`. Le module `internal/modules/planning/shifttemplates/` n'utilise que `planning_shift_templates` — la variante sans préfixe (avec sa table de détail `_items`) est probablement l'ancienne implémentation. |
| `user_vacations` | `planning_leave_requests` (créée par `migrations/done/018_planning_leave_and_swaps.sql`) | Les deux gèrent des congés/absences employé. Doute : `user_vacations` pourrait être un prédécesseur plus simple (pas de workflow d'approbation) remplacé par le système `planning_leave_requests` (statuts pending/approved/rejected). |
| `employment_agreement`, `employment_contract` | `sys_contract_types` (référentiel créé par `migrations/done/014_planning_socle.sql`) + `employee_documents` | Le module planning gère des types de contrat (`sys_contract_types`) et des documents employé (`employee_documents`), mais aucune table "contrat" à proprement parler n'existe dans le schéma actif. Doute : ces deux tables historiques stockaient peut-être les contrats eux-mêmes, remplacées par une combinaison référentiel + documents génériques. |
| `category_discount` | `discounts` / `discounts_products` (actives, module `internal/modules/discounts/`) | Le système de remise actuel est produit-par-produit (`discounts_products`) plutôt que par catégorie. Doute : `category_discount` pourrait être un ancien mécanisme de remise par catégorie de produit, abandonné au profit du ciblage produit. |
| `checkout_orderitems` | `orderitems` (table active massivement utilisée) | Nom évoquant une étape "panier avant validation" distincte de la table `orderitems` finale. Doute : ancienne table de panier temporaire, probablement remplacée par la gestion en mémoire/session côté scannorder ou par `orderitems` directement avec un statut. |
| `customer_advertisement_emails` | colonne `advertising_consent` sur `customer` (lue dans `internal/modules/customers/repository.go:50,134,1031,1212` et `internal/modules/orders/orders_fetcher_builder.go:606`) | Doute : cette table listait peut-être les emails opt-in marketing séparément, remplacée par un simple booléen `advertising_consent` embarqué dans `customer`. |
| `cash_funds` | colonne `cash_fund` sur `cash_registers` (lue dans `internal/modules/cash_registers/repository.go:330`) | Doute : `cash_funds` était peut-être une table de fonds de caisse séparée, désormais une colonne unique sur `cash_registers`. |
| `merchant_code` | aucun successeur identifié dans `merchant`/`merchant_parameters` (aucune colonne `code` repérée dans les requêtes lisant ces tables) | Doute faible : pourrait être un ancien système de code d'accès/parrainage marchand, sans équivalent visible dans le schéma actif. |
| `users_nfc_tags` | champ `NFC` présent dans `LoginRequestPayload` (`internal/modules/auth/models.go:64`) mais **jamais lu ni écrit dans aucune requête SQL du repo** | Doute : le champ de login existe côté API mais son backend (lookup dans `users_nfc_tags` ou ailleurs) semble non implémenté ou implémenté hors de ce repo. À clarifier : fonctionnalité en cours, abandonnée, ou gérée différemment (ex. table `users` elle-même) ? |
| `notifications` | module `internal/modules/notification/` actif, mais qui ne lit/écrit que `users_devices` et `firebase_fcm_access_token` | Doute : la table `notifications` (historique des notifications envoyées ?) n'est pas consommée par le module notification actuel, qui semble se limiter à la gestion des tokens FCM sans persister un historique. |

**Tables sans lien de renommage plausible identifié** (orphelines "sèches", sans successeur visible dans le schéma actif) : `api_calls`, `average_distribution_time_by_category`, `average_distribution_time_history`, `broadcast_list`, `calendar`, `cash_reports`, `consumables`, `integration_deliveroo_attributes_mapping`, `integration_deliveroo_components_mapping`, `integration_uber_eats_components_mapping`, `integration_uber_eats_reports`, `invoices`, `migration_users`, `order_changes_log`, `order_ratings`, `pictures`, `product_ratings`, `stock_evolution_records`, `stock_movements_desc`, `stock_movements_source`, `timezone_info`, `z_platform_daily_activity_recording`.

---

## Recommandations avant la migration Postgres

1. **Ne pas porter automatiquement les 37 tables orphelines** (38 avec `hours_amendments`) sans confirmation métier — chaque table exclue à tort est un risque de perte de données, chaque table portée à tort est du travail de migration gaspillé sur du mort. Croiser au minimum avec :
   - `information_schema.tables` (dernière date d'écriture si disponible, ou nombre de lignes — déjà connu ici pour la plupart, cf. liste fournie par l'utilisateur) ;
   - les logs d'accès MySQL (`general_log` / `performance_schema.events_statements_summary_by_digest`) sur une fenêtre de plusieurs semaines si possible, pour détecter un accès par un composant hors de ce repo (ancien back PHP, cron externe, BI) ;
   - le corps de `GET_AVERAGE_DISTRIBUTION_TIME` (procédure stockée, cf. §2) qui peut cacher un usage invisible au grep statique.
2. **Statuer sur `hours_amendments`** : schéma présent, jamais utilisé — supprimer la table ou terminer l'implémentation manquante avant de décider de la migrer.
3. Les doutes de renommage (§2bis) valent la peine d'une vérification rapide de structure (`SHOW CREATE TABLE`) pour confirmer ou infirmer le lien de succession avant de les classer définitivement "à ne pas migrer".
