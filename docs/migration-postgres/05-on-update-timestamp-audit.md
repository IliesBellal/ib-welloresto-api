# Audit `ON UPDATE current_timestamp()` — dépendance du code Go

Contexte : en MySQL, les 41 colonnes listées dans `docs/migration-postgres/04-schema-postgres-target.sql`
ont un `ON UPDATE current_timestamp()` qui rafraîchit automatiquement la colonne à **chaque** UPDATE
touchant la ligne, que la colonne soit ou non dans la clause `SET`. PostgreSQL n'a pas d'équivalent
déclaratif : sans trigger, seule une colonne explicitement mise à jour par le code Go continuera de
se rafraîchir après la bascule.

Méthode : pour chaque table, tous les `UPDATE ...` (et les upserts `INSERT ... ON DUPLICATE KEY UPDATE`,
qui sont un chemin d'UPDATE à part entière) ont été localisés dans `internal/modules/**/repository.go`
et `internal/webhook/**/repository.go`, `internal/tasks/**`. Pour chaque requête, on vérifie si la
colonne apparaît explicitement dans la clause `SET`.

Légende :
- **CONFIRMÉ** : toutes les requêtes UPDATE/upsert pertinentes sur la table mettent à jour explicitement la colonne (les updates ciblés sur un champ indépendant et sans rapport, ex. simple toggle `enabled`, ne sont pas comptés comme fautifs).
- **PARTIEL** : au moins un UPDATE la met à jour, au moins un autre UPDATE généraliste ne la met pas à jour.
- **ABSENT** : il existe au moins un UPDATE/upsert sur la table, mais aucun ne touche la colonne.
- **AUCUN UPDATE** : aucune requête UPDATE ni upsert n'existe pour cette table dans le code Go (uniquement des INSERT/SELECT/DELETE, ou la table n'est même jamais référencée).

## Tableau récapitulatif

| # | Colonne | Classification |
|---|---|---|
| 1 | bookings.creation_date | ABSENT |
| 2 | broadcast_list.create_date | AUCUN UPDATE (table jamais référencée en Go) |
| 3 | calendar.date | AUCUN UPDATE (table jamais référencée en Go) |
| 4 | discounts.valid_from | CONFIRMÉ |
| 5 | employees.updated_at | PARTIEL |
| 6 | employee_documents.updated_at | CONFIRMÉ |
| 7 | floors.creation_date | ABSENT |
| 8 | goods_receipts.updated_at | AUCUN UPDATE (INSERT seul) |
| 9 | haccp_corrective_actions.updated_at | AUCUN UPDATE (table en lecture seule) |
| 10 | haccp_settings.updated_at | CONFIRMÉ |
| 11 | holiday_calendar.updated_at | AUCUN UPDATE (table en lecture seule) |
| 12 | hours_amendments.updated_at | AUCUN UPDATE (table jamais référencée en Go) |
| 13 | hours_of_operation.valid_from | CONFIRMÉ |
| 14 | kiosks.updated_at | ABSENT |
| 15 | kiosk_settings.updated_at | ABSENT |
| 16 | labor_rules.updated_at | AUCUN UPDATE (table en lecture seule) |
| 17 | marketing_categories.updated_at | ABSENT |
| 18 | migration_users.updatedAt | AUCUN UPDATE (table jamais référencée en Go) |
| 19 | order_comments.creation_date | CONFIRMÉ |
| 20 | payments.payment_date | ABSENT |
| 21 | planning_day_comments.updated_at | CONFIRMÉ |
| 22 | planning_holiday_overrides.updated_at | CONFIRMÉ |
| 23 | planning_leave_requests.updated_at | CONFIRMÉ |
| 24 | planning_positions.updated_at | CONFIRMÉ |
| 25 | planning_revenue_forecasts.updated_at | ABSENT |
| 26 | planning_settings.updated_at | CONFIRMÉ |
| 27 | planning_shifts.updated_at | CONFIRMÉ |
| 28 | planning_shift_swap_requests.updated_at | CONFIRMÉ |
| 29 | planning_shift_templates.updated_at | CONFIRMÉ |
| 30 | planning_time_entries.updated_at | CONFIRMÉ |
| 31 | planning_weeks.updated_at | CONFIRMÉ |
| 32 | planning_week_templates.updated_at | CONFIRMÉ |
| 33 | planning_week_template_shifts.updated_at | AUCUN UPDATE (delete+reinsert, pas d'UPDATE) |
| 34 | printers.updated_at | ABSENT |
| 35 | product_marketing_categories.updated_at | CONFIRMÉ |
| 36 | purchased_components.registration_date | AUCUN UPDATE (INSERT seul) |
| 37 | subscription_invoices.invoice_date | ABSENT |
| 38 | temperature_readings.updated_at | AUCUN UPDATE (INSERT seul) |
| 39 | temperature_reading_corrective_actions.updated_at | AUCUN UPDATE (INSERT seul) |
| 40 | temperature_sessions.updated_at | AUCUN UPDATE (INSERT seul) |
| 41 | temperature_zones.updated_at | CONFIRMÉ |

Décompte : **CONFIRMÉ = 17**, **PARTIEL = 1**, **ABSENT = 8**, **AUCUN UPDATE = 15**.

---

## Détail des cas PARTIEL

### 5. employees.updated_at — PARTIEL

Toutes les UPDATE générales mettent à jour `updated_at` (`internal/modules/planning/employees/repository.go:271` `UpdateEmployee`, `:294` `UpdateEmployeeUserLink`, `:311` `SoftDeleteEmployee`), **sauf** :

- `internal/modules/users/admin_repository.go:480-486` — `ClearMerchantEmployeeLinks` :
  ```go
  UPDATE employees
  SET user_id = NULL
  WHERE merchant_id = ? AND user_id = ? AND enabled = 1
  ```
  Ne touche pas `updated_at` alors qu'elle modifie réellement l'état de la ligne (détache le lien utilisateur). Contrairement à un simple toggle `enabled` indépendant, cette colonne (`user_id`) fait partie des champs métier normalement suivis par `updated_at` — c'est un vrai oubli, pas un cas à exclure.

**Action recommandée avant bascule Postgres** : ajouter `updated_at = UTC_TIMESTAMP()` (ou `NOW()` en Postgres) à cette requête, ou créer un trigger dédié si on préfère ne pas toucher au code.

---

## Détail des cas ABSENT

### 1. bookings.creation_date — ABSENT

Aucune des ~9 requêtes UPDATE trouvées sur `bookings` (`internal/modules/bookings/repository.go:859,893,914,954,969,1242,1313`; `internal/modules/reservation/repository.go:387,400,457`; `internal/modules/order_life_cycle/repository.go:954` (commentée, code mort); `internal/webhook/brevo_sms_reply/repository.go:56,65`) ne touche `creation_date`. Sémantiquement attendu (date de création immuable) — le retrait de `ON UPDATE` en Postgres ne change donc rien d'observable, mais à documenter car c'est un changement de comportement implicite MySQL → Postgres.

### 7. floors.creation_date — ABSENT

- `internal/modules/locations/repository.go:530-536` (`UpdateFloor`, `SET name = ?`)
- `internal/modules/locations/repository.go:558-576` (`DeleteFloor`, `SET enabled = FALSE`)

Aucune ne touche `creation_date` (idem bookings — colonne de création, comportement correct par construction).

### 14. kiosks.updated_at — ABSENT

Six UPDATE trouvées dans `internal/modules/kiosk/repository.go`, **aucune** ne set `updated_at` :
- `:151` `UpdateKioskHeartbeat` — `SET last_heartbeat_at = UTC_TIMESTAMP(), app_version = ?, last_ip = ?`
- `:162` `UpdateKioskLastError` — `SET last_error = ?, last_error_at = UTC_TIMESTAMP()`
- `:171` `UpdateKioskStatus` — `SET status = ?`
- `:268` `UpdateKioskName` — `SET name = ?`
- `:279` `UpdateKioskAdminPinEncrypted` — `SET admin_pin_encrypted = ?`
- `:287` `SetKioskStatusEnabled` — `SET status = ?, enabled = ?`

Toutes ces mutations changent des champs métier réels (nom, statut, PIN) — `updated_at` ne sera donc plus jamais rafraîchi après migration sans intervention.

**Action recommandée** : soit ajouter `updated_at = NOW()` à ces 6 requêtes, soit poser un trigger Postgres `BEFORE UPDATE` sur `kiosks`.

### 15. kiosk_settings.updated_at — ABSENT

Un seul point de mutation, un upsert :
- `internal/modules/kiosk/repository.go:347-369` `UpsertSettings` :
  ```sql
  INSERT INTO kiosk_settings (...) VALUES (...)
  ON DUPLICATE KEY UPDATE
      fulfillment_dine_in = VALUES(fulfillment_dine_in), ... , primary_color = VALUES(primary_color)
  ```
  Ne mentionne pas `updated_at` dans la clause `ON DUPLICATE KEY UPDATE`.

**Action recommandée** : ajouter `updated_at = NOW()` à la clause `ON CONFLICT ... DO UPDATE` équivalente en Postgres.

### 17. marketing_categories.updated_at — ABSENT

Trois UPDATE dans `internal/modules/menu/repository.go`, aucune ne set `updated_at` :
- `:3631-3657` `UpdateMarketingCategory` (construction dynamique de `SET`, champs `name`/`available` uniquement)
- `:3668-3676` `DeleteMarketingCategory` — `SET enabled = 0`
- `:3683-3697` `UpdateMarketingCategoriesDisplayOrder` — `SET display_order = ?`

**Action recommandée** : ajouter `updated_at = NOW()` dans les trois requêtes (au minimum dans `UpdateMarketingCategory`, qui est la mutation "normale" côté produit).

### 20. payments.payment_date — ABSENT

De nombreuses UPDATE touchent `payments` mais aucune ne set `payment_date` :
- `internal/modules/cash_registers/repository.go:292,306` — `SET p.cash_register_id = ?`
- `internal/modules/delivery_sessions/repository.go:451,932` — `SET p.user_id = ds.user_id`
- `internal/modules/order_life_cycle/repository.go:321,927` — `SET enabled = 0`
- `internal/webhook/deliveroo_orders/repository.go:230-234` — `SET p.enabled = FALSE`
- `internal/webhook/stripe/repository.go:284` — `SET p.fee = ?, p.net_amount = ...`
- `internal/webhook/stripe/repository.go:298-301` (`DisablePayment`) — `SET p.enabled = '0'`
- `internal/webhook/ubereats/repository/orders_repo.go:53-58` — `SET p.enabled = FALSE`

`payment_date` semble être la date d'encaissement initiale (posée à l'INSERT) — comme pour `creation_date`, il n'y a a priori pas d'intention de la faire changer après coup, mais c'est un changement de comportement MySQL → Postgres à documenter (elle ne sera plus jamais "rafraîchie silencieusement" par un UPDATE annexe).

### 25. planning_revenue_forecasts.updated_at — ABSENT

Un seul point de mutation, un upsert :
- `internal/modules/planning/revenueforecast/repository.go:21-27` `Upsert` :
  ```sql
  INSERT INTO planning_revenue_forecasts (id, merchant_id, forecast_date, amount_ht_cents)
  VALUES (?, ?, ?, ?)
  ON DUPLICATE KEY UPDATE amount_ht_cents = VALUES(amount_ht_cents)
  ```
  Ne set pas `updated_at` dans la clause de conflit, alors que c'est le seul chemin de mise à jour de cette table (le montant prévisionnel peut être recalculé plusieurs fois pour la même date).

**Action recommandée** : ajouter `updated_at = NOW()` à la clause `ON CONFLICT ... DO UPDATE`.

### 34. printers.updated_at — ABSENT

Deux UPDATE dans `internal/modules/printers/repository.go`, aucune ne set `updated_at` :
- `:159-208` (`UpdatePrinter`, construction dynamique de `SET`, champs `name`/`connection_type`/`ip_address`/`port`/`bluetooth_address`/`role`/`language`/`production_product_ids`/`paper_width_mm`)
- `:214-238` (`DeletePrinter`) — `SET enabled = 0`

**Action recommandée** : ajouter `updated_at = NOW()` dans `UpdatePrinter` au minimum.

### 37. subscription_invoices.invoice_date — ABSENT

Un seul UPDATE :
- `internal/webhook/stripe/repository.go:318-323` `PayInvoice` :
  ```sql
  UPDATE subscription_invoices SET status = '1', payment_date = FROM_UNIXTIME(?) WHERE invoice_id = ?
  ```
  Met à jour `status` et un autre champ `payment_date` (distinct de `invoice_date`, colonne de création de la facture posée à l'INSERT `CreateInvoice`, ligne 307-316). `invoice_date` n'est donc jamais retouchée, ce qui est cohérent (date d'émission immuable) mais correspond à un changement de comportement MySQL → Postgres à documenter.

---

## Cas particuliers — AUCUN UPDATE (table jamais mise à jour ou jamais référencée)

Ces colonnes n'ont aucune requête UPDATE ni upsert dans le code Go pour leur table. Probablement inoffensif (tables de référence, historiques en écriture seule, ou tables non gérées par cette API), mais à vérifier côté métier avant de décider de ne pas poser de trigger.

- **goods_receipts.updated_at** — table alimentée uniquement par `INSERT` (`internal/modules/haccp/repository.go:1537`), aucun UPDATE.
- **haccp_corrective_actions.updated_at** — table de référence, lue uniquement (`internal/modules/haccp/repository.go:203,249,1251`), aucun INSERT/UPDATE trouvé dans ce repo (seedée par migration).
- **holiday_calendar.updated_at** — table de référence, lue uniquement en `JOIN`/`FROM` (`internal/modules/planning/settings/holidays_repository.go:43,50,86`), aucun INSERT/UPDATE trouvé (seedée par migration/job externe).
- **labor_rules.updated_at** — table de référence, lue uniquement (`internal/modules/planning/settings/repository.go:163`), aucun INSERT/UPDATE trouvé (seedée par migration).
- **purchased_components.registration_date** — alimentée uniquement par `INSERT` (`internal/modules/stocks/repository.go:171`), aucun UPDATE.
- **planning_week_template_shifts.updated_at** — géré en pattern « delete + reinsert » (`internal/modules/planning/weektemplates/repository.go:153` `DELETE`, `:203` `INSERT`), jamais d'`UPDATE` réel.
- **temperature_readings.updated_at**, **temperature_reading_corrective_actions.updated_at**, **temperature_sessions.updated_at** — toutes trois alimentées uniquement par `INSERT` dans `internal/modules/haccp/repository.go` (lignes 308, 352, 285 respectivement), aucun UPDATE.
- **broadcast_list.create_date**, **calendar.date**, **hours_amendments.updated_at**, **migration_users.updatedAt** — ces 4 tables ne sont référencées **nulle part** dans le code Go de cette API (aucun SELECT/INSERT/UPDATE), y compris en cherchant des variantes de nom. Ce sont probablement soit des tables legacy non reprises côté API (ex. `migration_users` évoque un outil de migration one-off), soit gérées par un autre service. À confirmer avant la bascule — si elles sont réellement mortes, aucun trigger n'est nécessaire ; si un autre composant (script SQL, autre repo) les alimente encore, il faudra l'auditer séparément.

---

## Synthèse des actions correctives avant bascule Postgres

Colonnes où le code Go doit être corrigé (ajouter la colonne à la clause `SET`/`ON CONFLICT DO UPDATE`) pour préserver le comportement actuel sans trigger :

1. `employees.updated_at` — `internal/modules/users/admin_repository.go:483` (`ClearMerchantEmployeeLinks`)
2. `kiosks.updated_at` — 6 requêtes dans `internal/modules/kiosk/repository.go` (lignes 151, 162, 171, 268, 279, 287)
3. `kiosk_settings.updated_at` — `internal/modules/kiosk/repository.go:347-369` (`UpsertSettings`)
4. `marketing_categories.updated_at` — `internal/modules/menu/repository.go` (lignes 3631-3697, 3 requêtes)
5. `planning_revenue_forecasts.updated_at` — `internal/modules/planning/revenueforecast/repository.go:21-27` (`Upsert`)
6. `printers.updated_at` — `internal/modules/printers/repository.go` (lignes 159-238, 2 requêtes)

Pour les colonnes ABSENT restantes (`bookings.creation_date`, `floors.creation_date`, `payments.payment_date`,
`subscription_invoices.invoice_date`) : aucune correction de code n'est nécessaire car aucun UPDATE ne
devrait légitimement toucher ces colonnes (dates de création/paiement immuables) — le comportement
Postgres sans trigger sera en fait plus correct que le comportement MySQL actuel. Pas de trigger à créer.

Pour les 15 colonnes en catégorie **AUCUN UPDATE**, aucun trigger n'est nécessaire tant que ces tables restent en écriture-seule (INSERT) ou non gérées par l'API — à confirmer côté métier pour les 4 tables jamais référencées.
