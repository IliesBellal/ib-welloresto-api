# 29 — Journal de conversion Tier 4 (dbx + vérification réelle Postgres)

Conversion des 5 modules Tier 4 de [07-module-inventory.md](07-module-inventory.md) — le cœur
métier, dernier lot — vers `internal/database/dbx`
([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)), même méthodologie que
les rapports [14 (Tier 1)](14-tier1-conversion-log.md), [25 (Tier 2)](25-tier2-conversion-log.md)
et [27 (Tier 3)](27-tier3-conversion-log.md) : chaque module/sous-package vérifié par un test
d'intégration réel (tag `postgres_integration`) contre le Postgres Docker de dev
(`localhost:5433`, base `welloresto_dev`), données insérées puis nettoyées par le test.

**Ordre traité** : pos (+accounting, reports), menu, bookings, order_life_cycle, puis planning
découpé en sous-packages : refs → settings → employees → documents → shifttemplates/weektemplates
→ schedule → timeentries → leave/swaps → performance/revenueforecast → daycomments.

**Commande de vérification** (par module) :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  DB_DIALECT=postgres go test -tags postgres_integration ./internal/modules/<module>/... -run '_Postgres'
```

**Résultat global : 5/5 modules (dont 13 sous-packages planning) convertis et verts.**
`go build ./...` OK. Suite unitaire complète : liste d'échecs strictement identique à la baseline
préexistante **moins un** (`bookingcomm` 1, `planning/employees` 2, `planning/leave` 7,
`planning/swaps` 3 — tous préexistants et intacts ; `pos/accounting [build failed]` est **sorti**
de la liste, voir § correctif vet). Suite `postgres_integration` complète (Tiers 1+2+3+4) :
**60 paquets verts, 0 échec**, après synchronisation de la base de dev avec le schéma cible du
rapport 28 (élargissements varchar, voir § base de dev). Les deux réserves transitives du Tier 3
sont levées : `pos.GetPOSStatus` et `scannorder.GetMerchantStatus` sont désormais testées sous PG
(planning/settings converti).

## Tableau de synthèse — modules

| # | Module | Statut | Test réel Postgres | Écarts non prévus par l'audit |
|---|---|---|---|---|
| 1 | `pos` (+accounting, reports) | ✅ | Création merchant complète (satellites, droits, InsertReturningID ×3), UPDATE...FROM (is_open via users/users_rights), GetPOSStatus (openinghours rapport 24 confirmé branché + férié planning forcé fermé), TVA/labels castés, toggles avec coercition Go, horaires CRUD + upsert ON CONFLICT, SET dynamiques (jsonb customer_form_requirements incl.), agrégats accounting/reports (DATE_FORMAT→to_char, IFNULL→COALESCE, ROUND→numeric, GROUP_CONCAT absent ici) | Paramètre `?` nu en colonne de SELECT (statut) intypable en PG → affecté côté Go après Scan. 3 jointures cross-type non repérées (`mp/iue.merchant_id` vs `m.id`, `l.label_value` vs `dr.deletion_reason_id`, `ur.merchant_id` vs `m.id` dans GetDeliveryMen) → `posCastChar`. `CAST(x AS CHAR)` sans longueur = char(1) en PG → cast texte par dialecte. Colonnes NOT NULL sans défaut sur le chemin vivant de création de merchant (MySQL non-strict insérait '' / zéro-date) → valeurs explicites : `subscriptions.stripe_subscription_id` '', `bookings_settings.code` '', `scannorder_settings.seo_*` '', `merchant_parameters.last_menu_update` = UTCNow (le zéro-date MySQL est de toute façon inscannable côté Go). Toggles : chaîne client sur colonne boolean → `posStatusFlag` (coercition MySQL numérique/0 reproduite). Divergence RowsAffected changed/matched rows sur toggles idempotents : purement informative (valeur echo JSON), documentée |
| 2 | `menu` | ✅ | **Chaque variante dynamique exécutée réellement** : BulkAssignProductsToCategory (IN ×2 racines+sous-produits), BulkAssignTag/ProductsToTag (IN + multi-VALUES), BulkAssignAllergen (IN + INSERT IGNORE→ON CONFLICT DO NOTHING, idempotence vérifiée), BulkAssignProductsToMarketingCategory (multi-VALUES + ON CONFLICT), SyncProductTags/Allergens/Attributes/Components, SET dynamiques (UpdateComponent, UpdateMarketingCategory, BulkUpdateProductPrices, UpdateProduct COALESCE), upsert product_configurable_attribute, GetMenu/GetAllProducts/GetAllComponents complets (foodcost, conversions d'unités), GetMarketingCategories (GROUP_CONCAT→string_agg), GetMenuWithMarketingCategories | `"removed_from_menu"` entre guillemets doubles (identifiant en PG) → simples. `by_product_of = ''` sur colonne integer → `= 0` (portable, = coercition MySQL). `configurable_attribute_options.id` : identity alimentée par un ID préfixé client (MySQL coerçait à 0 → auto-increment prenait le relais) → colonne retirée de l'INSERT, même effet net ; `enabled` de cette table est resté **integer** (les autres sont boolean). `configurable_attributes.product_id` NOT NULL hérité jamais renseigné (précédent Tier 2 webhook/ubereats) → 0 explicite, chemin vivant back-office. `productcateg/component_category.merchant_categ_id` NOT NULL omis avant l'UPDATE de rattrapage → '' explicite. Prix float64 JSON sur colonnes integer → arrondi Go (précédent cash_fund Tier 3). IDs client sur colonnes integer (tva, unités, options) → garde `menuNumericID` (coercition MySQL 0 reproduite). `recipes.merchant_id` NOT NULL jamais renseigné → merchantID réel écrit (disponible dans le scope). 5 jointures cross-type castées (pca.product_id, product_allergens/product_tags.product_id, unit_of_measure_desc.id vs purchase_unit_id varchar). **Voir § bugs de production** (CreateMarketingCategory, ListTags) |
| 3 | `bookings` | ✅ | Le module le plus dense en dates du repo : settings (défauts COALESCE + upsert), règles de durée CRUD, horaires (upsert transactionnel ON CONFLICT, formats time), création staff via bookingcore + tables, fetcher (jointures castées, unix), back-office (IN + LIKE + string_agg + pagination), conflits de tables (FOR UPDATE, fenêtres, exclusion), **GetBookingAvailability avec occupation réelle vérifiée (10-4=6)**, CheckCapacityForWindow, reschedule, rappels (fenêtre paramétrée), auto-seat (±30 min autour de maintenant), expiration des pending (paramétrée par settings), waitlist complète (enum, intervalles paramétrés) | 27 fonctions de date traduites via helpers par dialecte (`bkgPlusMinutes/Hours`, `bkgEndOfBooking`, `bkgAbsSecondsFromNow`, `bkgTimeFmt/DateTimeFmt`). `? IS NULL` intypable en PG (fetcher) → `CAST(? AS CHAR/TEXT) IS NULL`. `DELETE bl FROM ... JOIN` → `DELETE ... USING`. `UPDATE ... LEFT JOIN` (ExpirePendingBookings) sans équivalent UPDATE...FROM → sous-requête corrélée côté PG. TIMESTAMPDIFF (durée reschedule) → calcul Go (même troncature). **Scan des dates d'occupation** : pgx renvoie le timestamptz avec offset numérique, que le parse RFC3339-«Z» de bookingcore rejetait silencieusement → scan en NullTime + formatage Go identique au comportement MySQL/parseTime (fixtures sqlmock adaptées). `deletion_reason_id` : littéral texte codé en dur coercé à 0 par MySQL → 0 explicite ; garde numérique Go sur l'ID client (IsValidDeletionReason). excludeBookingID '' sur booking_id integer → "0" lié côté Go. Cohérence avec reservation (Tier 2) : mêmes casts `bs.merchant_id` vs `m.id`. **Voir § bugs de production** (UpsertBookingSettings) |
| 4 | `order_life_cycle` | ✅ | Cycle complet : caisse active (direct + device_link + erreurs typées), CreateOrder complet (validation IN, upsert client, items/extras/withouts/configs/locations/commentaires, paiement partiel), **AddPaymentAndReturnID (rapport 20 confirmé : TR + stripe_payments + refresh isPaid via UPDATE...FROM, chaînage fiscal)**, sur-paiement refusé, désactivation de paiement, UpdateOrder (upsert item existant + nouveau + retrait implicite), distribution partielle/retour production/statuts production, transitions (accept, delivery started, deny, delete), **SetDeliveredLocal (clôture + hash fiscal + qrcodes USING + delivery_session + items via délais)**, réouverture ; ex-procédure GET_AVERAGE_DISTRIBUTION_TIME (rapport 23) confirmée via distributiontime (cas « aucune donnée → estimated_ready vide ») | Hack `formatQuery/isPostgres` mort retiré (le rebind dbx le remplace). `orderitems` : PK composite (order_item_id, order_id, product_id) identity → chemins séparés côté PG : INSERT...RETURNING pour les nouveaux items, `OVERRIDING SYSTEM VALUE + ON CONFLICT` pour l'upsert d'items existants. `order_comments` : ON DUPLICATE mort (PK auto-inc seule, les appelants suppriment avant réinsertion) → INSERT simple. `orders.responsible` integer alimenté par user_id texte → `olcResponsible` (coercition MySQL 0). **`stripe_payments.success_key` NOT NULL sans défaut sur le chemin vivant CB Kiosk** : MySQL insérait '', PG rejetait — et l'erreur est écrasée par le code (l'insert échouait silencieusement, cassant le webhook charge.captured) → '' explicite. `UPDATE orders o JOIN` ×2 + `UPDATE delivery_session ds JOIN` + `UPDATE orderitems LEFT JOIN delays` → UPDATE...FROM / sous-requête corrélée. Anomalie préexistante identique aux deux dialectes : Scan de `brand_order_id` (NULL pour WELLO_RESTO) dans un string non nullable (MarkOrderAsDeliveryStarted) — documentée, le test pose une valeur |
| 5 | `planning` (13 sous-packages) | ✅ | Un test d'intégration par sous-package, dans l'ordre prescrit : refs (référentiels sys_*), settings (labor rules, fériés légaux + overrides, **ResolvePlanningHoliday**), employees (+positions, lien user), documents, shifttemplates/weektemplates (to_char HH24:MI, agrégats), schedule (semaines draft/publish/unpublish, shifts, vue équipe), timeentries (pointage ouvert/clos, fenêtres), leave (conflits congés/shifts), swaps (**approbation transactionnelle : échange effectif des employés vérifié**), performance (planifié date+time avec nuit à cheval sur minuit 660 min, pointé via AT TIME ZONE ?::interval 25200 s, prévisionnel), revenueforecast (upsert), daycomments (upsert, auteur d'origine préservé) | Conversion majoritairement mécanique (tables récentes, dates côté Go, colonnes boolean) : `enabled/active = 1/0` → TRUE/FALSE partout + littéraux d'INSERT. `CASE ... THEN 1 ELSE 0` mélangé à `o.count_as_holiday` boolean → CASE homogène TRUE/FALSE (settings). `(SELECT ? AS holiday_date)` intypable → `CAST(? AS DATE)` portable. Comparaison de label sensible à la casse en PG (positions) → `LOWER() = LOWER()` (= collation MySQL). `weektemplates` appelait `r.db` directement (sans GetDB) → routé via dbx. Transaction brute de `swaps.Approve` → `dbx.Wrap(tx)` (le rebind s'applique aux tx aussi). `users.enabled` resté **integer** (Tier 3) ≠ `users_rights.enabled` boolean — corrigé après un passage trop large. `planning_shifts.title` NOT NULL absent de l'INSERT (MySQL insérait '') → colonne ajoutée avec la valeur du modèle. CONVERT_TZ (performance) → AT TIME ZONE ?::interval (écart n°1 rapport 25), TIMESTAMPDIFF/TIMESTAMP(d,t)/DATE_ADD → arithmétique date+time native PG. ON DUPLICATE ×2 (daycomments, revenueforecast) → ON CONFLICT sur les clés uniques (merchant_id, date). Fixtures sqlmock des 22 fichiers de tests planning alignées ; échecs préexistants employees/leave/swaps **inchangés à l'identique** |

## Bugs de production signalés en cours de chantier (identiques aux deux dialectes)

1. **`bookings.UpsertBookingSettings` : sauvegarde des réglages de réservation cassée.**
   `bookings_settings` n'a **aucune contrainte unique sur merchant_id** (PK = id auto-incrémenté) :
   le `ON DUPLICATE KEY UPDATE` ne se déclenchait jamais — chaque sauvegarde insérait une ligne
   dupliquée, et les lecteurs (`LIMIT 1` sans ORDER BY → la plus ancienne) ne voyaient jamais les
   modifications. Corrigé par un upsert réel (UPDATE de la ligne la plus ancienne — celle que
   lisent les SELECT — sinon INSERT), vérifié par le test (1 ligne, valeur à jour). Candidat à une
   contrainte UNIQUE(merchant_id) + déduplication des lignes existantes en prod — **à déployer et
   vérifier séparément de la migration**, même précédent que GetUserByPIN (25) et
   customer_rewards (27).
2. **`menu.CreateMarketingCategory` retournait toujours "0"** : `LastInsertId()` sur une table à
   PK varchar générée côté client (pas d'auto-increment) → 0 en MySQL, erreur dure en pgx.
   Corrigé : l'ID `mark-categ-...` réellement inséré est retourné au client.
3. **`order_life_cycle`/stripe_payments (chemin CB Kiosk, rapport 20)** : `success_key` NOT NULL
   sans défaut, jamais renseignée, **et l'erreur d'insertion est écrasée** par le code — sous PG
   la ligne stripe_payments n'était silencieusement jamais créée (webhook charge.captured cassé).
   '' explicite ajouté (= valeur MySQL non-strict). La non-propagation de l'erreur reste un point
   de fragilité documenté, non corrigé (hors périmètre dialecte).

Anomalies préexistantes **documentées sans correction** (comportement identique aux deux dialectes) :
- `menu.ListTags` : `SELECT id ...` sur une table sans colonne `id` (tag_id) — endpoint cassé
  depuis son écriture, l'erreur est vérifiée par le test ;
- `order_life_cycle.MarkOrderAsDeliveryStarted` : Scan de `brand_order_id` NULL dans un string
  non nullable — échoue pour toute commande WELLO_RESTO ;
- `bookings.FindConflictingBookings` ne matche que les statuts legacy
  (PENDING_APPROVAL/ACCEPTED/ORDER_OPEN), par conception (T-08) — le test bascule le statut ;
- `session_orderitem` (bulkInsertWithSuffix) : suffixe ON DUPLICATE MySQL-only sur un chemin mort
  (customersArgs toujours vide) — non traduit, à traiter si la fonctionnalité est réactivée.

## Écarts transverses nouveaux (au-delà des rapports 25/27, tous appliqués d'emblée par ailleurs)

1. **Paramètre `?` nu intypable en PG** : en colonne de SELECT (pos), en `? IS NULL` (bookings
   fetcher), en table dérivée `(SELECT ? AS x)` (planning/settings) → affecter côté Go, ou
   `CAST(? AS CHAR/TEXT/DATE)` (portable, MySQL accepte).
2. **`CAST(x AS CHAR)` sans longueur = char(1) en Postgres** (troncature silencieuse à 1 caractère)
   → cast texte par dialecte (`*CastChar` locaux, pattern orders.castChar).
3. **PK identity composite + upsert** (orderitems) : `OVERRIDING SYSTEM VALUE` requis pour insérer
   un id explicite, ON CONFLICT sur la clé composite complète ; quand l'id est absent, chemin
   INSERT...RETURNING séparé.
4. **`ON DUPLICATE KEY UPDATE` sans contrainte unique correspondante = upsert mort** en MySQL
   (bookings_settings, order_comments) : à détecter avant de traduire en ON CONFLICT — PG
   refuserait la clause, et la traduction naïve aurait figé un bug.
5. **Transactions brutes (`db.BeginTx` + `tx.ExecContext`)** : hors du chemin dbx.GetDB → les
   envelopper avec `dbx.Wrap(tx)` (planning/swaps) ; grep `BeginTx` en fin de conversion.
6. **Scan de timestamptz en string** : pgx formate avec offset numérique (+00:00), MySQL/parseTime
   avec «Z» — tout parse Go en aval du format «Z» casse silencieusement (occupation bookings) →
   scanner en time.Time et formater côté Go.
7. **Collation** : comparaisons d'unicité sur libellés (`label = ?`) insensibles à la casse en
   MySQL, sensibles en PG → `LOWER() = LOWER()` explicite (planning/positions) ; l'insensibilité
   aux accents reste divergente (documentée au 04).
8. **`UPDATE/DELETE multi-table avec LEFT JOIN`** : pas d'équivalent direct en UPDATE...FROM
   (sémantique INNER) → sous-requête corrélée (ExpirePendingBookings, orderitems/delays).

## Correctifs annexes (hors conversion stricte)

- `internal/modules/pos/accounting/service.go` : `%s` sur `*string` (VATNumber) — l'erreur vet
  préexistante qui bloquait **le build des tests du paquet** (baseline `pos/accounting [build
  failed]`) ; corrigée (déréférencement sûr), le paquet sort de la liste d'échecs. Même précédent
  que le vet ubereats au Tier 3. Au passage, le PDF affichait l'adresse du pointeur au lieu du
  numéro de TVA.
- Fixtures sqlmock adaptées aux requêtes converties (bookings ×2, planning ×10 fichiers) — aucune
  assertion fonctionnelle modifiée, uniquement le texte SQL/le nombre d'arguments attendus.

## Base de dev synchronisée (hors code)

- `planning_day_comments` créée (le schéma cible du rapport 26 n'avait pas été appliqué au
  Docker de dev) — DDL + index unique (merchant_id, comment_date) + index de plage.
- Élargissements varchar du rapport 28 appliqués (`users.token`, `customer_loyalty_progress.id`,
  `customer_loyalty_progress_order.progress_id` → varchar(64)) — les tests Tier 3 échouaient en
  22001 depuis le retrait des troncatures Go.

## Réserves des tiers précédents levées

- `pos.GetPOSStatus` (dépendance planning/settings.ResolvePlanningHoliday) : testé sous PG,
  y compris jour férié forcé fermé (TestPOSStatus_Postgres).
- `scannorder.GetMerchantStatus` (même dépendance, rapport 27) : testé sous PG
  (TestScannorderMerchantStatus_Postgres, ajouté au fichier de test scannorder).

## Découpage en commits atomiques (préparé, non exécuté)

Un commit par module, un par sous-package planning. `DB_DIALECT=mysql` reste le défaut en prod ;
aucun commit exécuté — à faire par l'utilisateur.

| Commit suggéré | Fichiers |
|---|---|
| `postgres: convert pos module to dbx (fix accounting vet, NOT NULL parity on merchant creation)` | `internal/modules/pos/{repository,create_repository}.go`, `internal/modules/pos/accounting/{repository,service}.go`, `internal/modules/pos/reports/repository.go`, `internal/modules/pos/postgres_integration_test.go`, `internal/modules/pos/accounting/postgres_integration_test.go` |
| `postgres: convert menu module to dbx (fix CreateMarketingCategory id, option identity)` | `internal/modules/menu/repository.go`, `internal/modules/menu/postgres_integration_test.go` |
| `postgres: convert bookings module to dbx (fix bookings_settings dead upsert)` | `internal/modules/bookings/{repository,waitlist_repository,bookings_fetcher}.go`, `internal/modules/bookings/{auto_hooks_test,reschedule_test}.go`, `internal/modules/bookings/postgres_integration_test.go` |
| `postgres: convert order_life_cycle module to dbx (fix stripe_payments success_key)` | `internal/modules/order_life_cycle/repository.go`, `internal/modules/order_life_cycle/postgres_integration_test.go` |
| `postgres: convert planning/refs to dbx` | `internal/modules/planning/refs/*` |
| `postgres: convert planning/settings to dbx (+ deferred pos/scannorder status tests)` | `internal/modules/planning/settings/*`, `internal/modules/pos/postgres_integration_test.go` (TestPOSStatus), `internal/modules/scannorder/postgres_integration_test.go` (TestScannorderMerchantStatus) |
| `postgres: convert planning/employees to dbx (LOWER label match)` | `internal/modules/planning/employees/*` |
| `postgres: convert planning/documents to dbx` | `internal/modules/planning/documents/*` |
| `postgres: convert planning/shifttemplates+weektemplates to dbx` | `internal/modules/planning/shifttemplates/*`, `internal/modules/planning/weektemplates/*` |
| `postgres: convert planning/schedule to dbx (fix missing NOT NULL title)` | `internal/modules/planning/schedule/*` |
| `postgres: convert planning/timeentries to dbx` | `internal/modules/planning/timeentries/*` |
| `postgres: convert planning/leave+swaps to dbx (wrap approve tx)` | `internal/modules/planning/leave/*`, `internal/modules/planning/swaps/*` |
| `postgres: convert planning/performance+revenueforecast to dbx` | `internal/modules/planning/performance/*`, `internal/modules/planning/revenueforecast/*` |
| `postgres: convert planning/daycomments to dbx` | `internal/modules/planning/daycomments/*` |
| `docs: add Tier 4 postgres conversion log` | `docs/migration-postgres/29-tier4-conversion-log.md` |

> Rappel : l'arbre de travail contient toujours les chantiers non commités des rapports 25/26/27/28
> (leurs découpages restent ceux de leurs rapports respectifs). Le commit planning/settings inclut
> les ajouts de tests différés pos/scannorder, même précédent croisé que la fixture upsell au
> Tier 3.
