# 27 — Journal de conversion Tier 3 (dbx + vérification réelle Postgres)

Conversion des 9 modules Tier 3 de [07-module-inventory.md](07-module-inventory.md) vers
l'infrastructure `internal/database/dbx` ([08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)),
même méthodologie que les rapports [14 (Tier 1)](14-tier1-conversion-log.md) et
[25 (Tier 2)](25-tier2-conversion-log.md) : chaque module vérifié par un test d'intégration réel
(tag de build `postgres_integration`) contre le Postgres Docker de dev (`localhost:5433`, base
`welloresto_dev`), avec données de test insérées puis nettoyées par le test.

**Ordre traité** (simple → complexe) : users, cash_registers, orders, haccp, delivery_sessions,
ubereats, scannorder, kiosk, customers.

**Commande de vérification** (par module) :

```bash
POSTGRES_URL='postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev' \
  go test -tags postgres_integration ./internal/modules/<module>/...
```

**Résultat global : 9/9 modules convertis et verts.** `go build ./...` OK ; suite unitaire
complète : liste d'échecs strictement identique à la baseline préexistante **moins un**
(`bookingcomm`, `planning/{employees,leave,swaps}`, `pos/accounting` build failed — tous hors
Tier 3, préexistants, non touchés ; `ubereats` est **sorti** de la liste d'échecs, voir §
« correctif vet »). Suite `postgres_integration` complète (Tiers 1+2+3) : verte après réparation
d'une fixture Tier 1 obsolète (voir § upsell).

## Sondage préalable — règles de liaison pgx (validées contre le Postgres de dev)

Avant le premier module, un programme jetable a établi empiriquement les règles de liaison du
driver pgx (stdlib, `sql.Open("pgx", ...)`, mode d'exécution par défaut) qui conditionnent tout
le tier :

| Liaison Go → colonne PG | Résultat | Conséquence |
|---|---|---|
| `string` numérique → `integer` | ✅ (texte parsé par le serveur) | pas besoin de `strconv.Atoi` quand la valeur est toujours numérique (merchantID, orderID) |
| `string` non numérique → `integer` | erreur SQL dure 22P02 | là où MySQL coerçait en 0 — confirme l'écart transverse n°6 du rapport 25 |
| `bool` → `integer` | ❌ encode impossible | convertir en 0/1 côté Go (ex. `users.enabled`, resté integer en cible) |
| `int`/`float64` → `text`/`varchar` | ❌ encode impossible | formater côté Go (`strconv`) |

Règle inverse (Scan) : `boolean` PG ne se scanne pas dans un `int` Go → `CASE WHEN x THEN 1 ELSE 0 END`
en SQL (valide aussi en MySQL) partout où le code scanne un booléen dans un int.

## Tableau de synthèse

| # | Module | Statut | Fichiers modifiés | Test réel Postgres | Écarts non prévus par l'audit |
|---|---|---|---|---|---|
| 1 | `users` | ✅ | `repository.go`, `admin_repository.go`, `create_repository.go`, `admin_service_test.go` (fixtures) (+ test) | CRUD profil/droits, flux livreur (session, positions, stops), liste admin paginée, upsert de droits | `users.lat/lng` sont des colonnes **text** et `heading` un **integer NOT NULL** alimentés par des float64/NULL → formatage/arrondi Go (`SetUserLocation`, `UpdateUserProfile`). `users.enabled` est resté **integer** (pas boolean) → filtre `Active` converti en 0/1 Go. `users.token` est `varchar(30)` mais `UpdatePassword` génère 128 hex chars — MySQL non-strict tronquait, PG rejette → troncature Go identique. Jointure héritée `ur.id = usv.user_id` (PK integer vs varchar) dans `GetUserLocation` → cast dialecte, sémantique « ne matche que les user_id numériques » préservée à l'identique. 2 `LastInsertId` → `InsertReturningID`. Jointures merchant.id castées (pattern auth) dans `GetUserByToken` |
| 2 | `cash_registers` | ✅ | `repository.go` (+ test cycle de vie ajouté au test du rapport 22) | Ouverture (InsertReturningID), clôture complète avec requalification des paiements STRIPE/UBER_EATS/DELIVEROO/**CB Kiosk (3bis, rapport 20)** vérifiée sur 5 paiements, hash fiscal, résumé, items personnalisés, en-clôture, historique, device_link | Les 3 `UPDATE ... INNER JOIN` (dont le 3bis) → `UPDATE ... FROM` via helper `paymentsRequalifySQL` branché par dialecte. `OpenCashRegister` omettait `closure_comment` **NOT NULL sans défaut** (MySQL non-strict insérait `''`) → `''` explicite. `cash_fund` integer alimenté par un float64 JSON → arrondi Go. `SET cr.enclosed` alias-qualifié (interdit PG) → déqualifié. Jointure `pstats.cash_register_id` (payments varchar) vs PK integer dans l'historique → cast dialecte. Scan `p.enabled` boolean → int Go via `CASE 1/0`. Upsert `device_link` → branche `ON CONFLICT (device_id)`. `GetCashRegisterReport`/MOP (rapport 22) inchangés — tout le module passe désormais par `dbx.GetDB` |
| 3 | `orders` | ✅ | `repository.go`, `orders_fetcher_builder.go` (+ test) | **Chaque variante IN (...) exécutée réellement** : GetOrdersByIDs (2 ids), GetHistory (3 IN simultanés channel/order_type/status + recherche), GetRewards (2 IN), ValidateProducts, GetProductsForPricing, GetUnavailableProducts, GetConfigurationOptionPrices ; assemblage complet FetchAndBuildOrders (7 sous-requêtes, extras/withouts/config/comments/payments/locations/customer/cash register/delivery session) | `QueryFilter` (rapport 02) branché tel quel sur le rebind `dbx` — aucun placeholder réécrit à la main. 5 jointures cross-type non repérées à l'audit : `pca.product_id` (varchar) vs `oi.product_id` (×2), `d.discount_id` (varchar) vs `oi.discount_id`, `o.responsible` (integer) vs `u.user_id`, `cr.cash_register_id` (PK integer) vs `o.cash_register_id`, `mp.merchant_id` vs `m.id` (GetMerchantPricingInfo), `dpo.product_id` (varchar 20) vs `dp.product_id` (integer) → helper `castChar` par dialecte. Booléens scannés en NullInt64 (`oi.isPaid/isDistributed`, `o.isDelivery`, `use_customer_temporary_address`) → `CASE 1/0`. `COALESCE(o.order_id, '')`/`c.customer_id` integer dans la recherche → cast. `HAVING` sans GROUP BY (GetUnavailableProducts) → sous-requête. `TIME(UTC_TIMESTAMP())`+`DAYOFWEEK` (GetDiscounts) → branche PG `CAST(now() AT TIME ZONE 'UTC' AS time)` + `EXTRACT(ISODOW)`. Comparaisons time vs timestamp (GetDiscountProductOptions) → fenêtre d'heure du jour côté PG, fragment MySQL conservé tel quel. `ValidateProducts` liait un int64 sur merchant_Id varchar → FormatInt ; `c.status = 0` numérique sur varchar → énumération des statuts bloquants (déviation documentée pour un statut non numérique inconnu). **Bug préexistant identique aux deux dialectes** : le Scan de `GetDiscountProductOptions` est décalé par rapport au SELECT (map indexée par option_id, OptionID = discount_id) — documenté, non corrigé, comportement figé par le test |
| 4 | `haccp` | ✅ | `repository.go` (+ test) | Settings (insert UTCNow + replace), zones/relevés température avec actions correctives (batchs multi-VALUES), zones/surfaces/sessions de nettoyage, activités (UNION ALL + filtre + pagination), réception marchandise (jsonb), composants HACCP | Aucun écart structurel : IDs client varchar (pas d'identity), pas d'ON DUPLICATE. Conversion mécanique : booléens `enabled/active = 1/0` → TRUE/FALSE (y compris les littéraux `1` des batchs d'INSERT), 3 UTC_TIMESTAMP → `dbx.UTCNow()`. Vert au premier passage |
| 5 | `delivery_sessions` | ✅ | `repository.go` (+ test) | Cycle complet d'une tournée : démarrage (InsertReturningID + brand_status), FSM des stops (select/arrive/deliver/fail avec avance du stop courant), réaffectation des paiements au livreur, clôtures livreur/manager/annulation, assemblage sessions (delivery notes) | 5 `UPDATE ... INNER JOIN` (orders ×3, payments ×2) → `UPDATE ... FROM` par dialecte. `distance/duration` (integer) alimentés par des strings client → `strconv.Atoi` avec repli 0 (= coercition MySQL non-strict). `SET heading = NULL` sur NOT NULL (CloseMyDeliverySession) → `heading = 0` (= repli MySQL non-strict). Même jointure héritée `ur.id = usv.user_id` que users.GetUserLocation dans l'assemblage du livreur → cast, sémantique préservée (le test crée le user avec un user_id égal à l'id de sa ligne users_rights, seule configuration qui matche — dans les deux dialectes) |
| 6 | `ubereats` | ✅ | `repository.go`, `service.go` (correctif vet) (+ test) | Lookups store (jointures castées), tokens externes (upsert + intervalle paramétré), acceptation de commande (estimated_ready = now + ? minutes vérifié), statuts denied/canceled/ready, SyncOrderState CLOSED, busy mode, prep time manuel/auto, fermeture temporaire | Aucun résidu de `CALL GET_AVERAGE_DISTRIBUTION_TIME` (rapport 23 confirmé — passe par `distributiontime.EstimatedSeconds`). `DATE_ADD(x, INTERVAL ? SECOND/MINUTE)` **paramétré** → branche PG `now() + (? * interval '1 second')`. UPDATE multi-table MySQL dont le `state = 'CLOSED'` non qualifié modifiait **orders** (orderitems n'a pas de colonne state) → 2 requêtes côté PG, même effet. `estimated_preparation_time` varchar alimenté par un int → `strconv.Itoa` (même correctif que integrations, Tier 2). **Bug préexistant identique aux deux dialectes** : `EnableIntegration`/`DisableIntegration` référencent `access_token`/`is_active`/`updated_at`, colonnes absentes du DDL MySQL source ET de la cible — le chemin OAuth Uber échoue à l'exécution dans les deux dialectes ; converti mécaniquement (branche ON CONFLICT), erreur attendue vérifiée par le test, non corrigé |
| 7 | `scannorder` | ✅ | `repository.go` (+ test ×2) | GetMerchantByQR (5 jointures castées), **GetAvailableSlots (CTE récursif MySQL MAKETIME/ADDTIME/DATE_FORMAT/TIME_FORMAT/DAYOFWEEK entièrement traduit, créneaux jour même + lendemain vérifiés à la main)**, discounts, horaires d'ouverture, produits indisponibles, session de livraison par commande, paiement Stripe, client depuis QR de table (réservation en cours), marques multi-restaurants (Haversine avec et sans filtre 50 km), upsell, prix produits/options | Aucun résidu de `CALL GET_POS_STATUS` (rapport 24 confirmé — openinghours). Branche PG complète pour le CTE récursif (time/interval natifs, `EXTRACT(ISODOW)` sans le CASE MySQL, `CROSS JOIN` explicite, comparaison heure+délai en interval — ne boucle pas à minuit, comme ADDTIME) avec sa propre liste d'arguments. `HAVING distance_km < 50` sans GROUP BY → sous-requête PG. Heures `time` scannées en string → `CAST(... AS CHAR(8))` (pattern rapport 24). Placeholders `$1` **codés en dur** dans 2 requêtes (GetCustomerFromQR, rewards) — cassées sous MySQL depuis leur écriture → `?` (portable, corrige MySQL au passage). **Anomalie préexistante** : `GetMerchantByQR` scanne `qr.creation_date`/`last_waiter_call` (timestamps) dans des `*int64` epoch — échoue dans les deux dialectes dès que la colonne est non-NULL (requête issue d'un commit WIP) ; documenté, le test couvre la seule configuration fonctionnelle (colonnes NULL). **Dépendance transitive** : `GetMerchantStatus` → `planning/settings.ResolvePlanningHoliday` (Tier 4 non converti, `?` non rebindés) — non testable sous PG jusqu'à la conversion planning, même précédent que reservation→customers au Tier 2 |
| 8 | `kiosk` | ✅ | `repository.go` (+ test) | Enrôlement complet (codes, bornes, tokens avec rotation/révocation), heartbeat/erreurs, listing avec fenêtre 24 h des bornes révoquées, quotas, settings (upsert ON CONFLICT insert+update), fees, Terminal location, slug, disponibilité produits/options, kiosk_id sur commande, bascule carte→caisse (idempotence vérifiée), discounts | `DATE_SUB(x, INTERVAL 24 HOUR)` → `x - INTERVAL '24' HOUR` (forme SQL standard, valide dans les deux dialectes). Upsert `kiosk_settings` → branche `ON CONFLICT (merchant_id)`. GetDiscounts : même traduction que scannorder (fenêtre d'heure + CASE 1/0). Cohérence rapport 20 : le correctif `mop='CB'` vit dans `webhook/stripe` (Tier 2, déjà converti) et la requalification 3bis est vérifiée par le test cash_registers ; `GetActiveCashRegisterID` est dans **order_life_cycle** (Tier 4) — hors périmètre, rien à faire côté kiosk. Vert au premier passage |
| 9 | `customers` | ✅ | `repository.go` (+ test) | Upsert client (INSERT dynamique InsertReturningID + UPDATE dynamique), lookups (id/email insensible casse/téléphone normalisé), CRUD programmes de fidélité avec produits cibles/récompenses, progression manuelle avec palier, **UpdateLoyaltyFromOrder complet** (stats client via UPDATE...FROM, progression, idempotence par commande, création de récompense), usage/réactivation des récompenses, recherche multi-critères (nom + téléphone nettoyé), listing trié | Jointures cross-type non repérées : `clp.customer_id`/`cr.customer_id` (varchar 30) vs PK integer `customer.customer_id`, `tp/rp.product_id` (varchar 50) vs `products.product_id`/`oi.product_id` (integer) → `custCastChar` (6 sites). `COALESCE(available, 0)` sur boolean → `FALSE`. `UPDATE customer INNER JOIN orders` → `UPDATE ... FROM`. Troncatures varchar MySQL non-strict répliquées côté Go : `customer_loyalty_progress.id` (50) et `progress_order.progress_id` (30) face à des IDs préfixés de 49-53 chars. `customer_brand` NOT NULL reçoit un NULL explicite si non fourni — erreur identique en MySQL (1048), les appelants la fournissent toujours ; non modifié. **Voir § bug de production critique** (colonne `id` fantôme de customer_rewards) |

## Écarts transverses nouveaux (à retenir pour le Tier 4)

1. **Liaison pgx** (sondage en tête de rapport) : string numérique → integer passe, bool → integer
   et int/float → varchar échouent. Les 8 écarts du rapport 25 restent valables tels quels.
2. **`UPDATE ... INNER JOIN` MySQL → `UPDATE ... FROM` PG** : 9 sites convertis sur ce tier
   (cash_registers ×3, delivery_sessions ×5, customers ×1) — l'alias de la table cible est permis
   en PG, mais les cibles du SET doivent être non qualifiées (écart n°5 du rapport 25). Un UPDATE
   multi-table MySQL qui modifiait **deux** tables (colonne non qualifiée résolue sur l'autre
   table, ubereats.SyncOrderState) n'a pas d'équivalent PG → deux requêtes.
3. **Booléen PG scanné dans un int Go** : impossible via database/sql → `CASE WHEN x THEN 1 ELSE 0 END`
   dans le SELECT (valide en MySQL, le tinyint s'évalue comme booléen). Sites : cash_registers,
   orders (fetcher), scannorder/kiosk (GetDiscounts).
4. **`DATE_ADD/SUB(x, INTERVAL ? unité)` avec quantité paramétrée** : pas de forme commune →
   branche PG `x + (? * interval '1 unité')` (pattern Tier 1 upsell). Quand la quantité est un
   littéral, la forme standard `INTERVAL '24' HOUR` passe telle quelle dans les deux dialectes.
5. **`TIME()/DAYOFWEEK()` et comparaisons time-vs-timestamp** : traduits en
   `CAST(now() AT TIME ZONE 'UTC' AS time)` + `EXTRACT(ISODOW ...)` côté PG (ISODOW est déjà en
   convention 1=lundi, le CASE MySQL disparaît). Le fragment MySQL d'origine est conservé sous
   `DB_DIALECT=mysql` — comportement prod inchangé même là où la sémantique MySQL de la
   comparaison time-vs-datetime est douteuse (orders.GetDiscountProductOptions).
6. **`HAVING` sur alias sans GROUP BY** (tolérance MySQL) : rejeté par PG → envelopper en
   sous-requête filtrée en WHERE (orders.GetUnavailableProducts, scannorder Haversine).
7. **Troncature varchar MySQL non-strict** : troisième famille de coercition silencieuse après les
   identity et les NOT NULL omis — répliquée côté Go (`users.token` 30, ids fidélité 50/30) pour
   une valeur stockée identique. À greper au Tier 4 : longueur des IDs préfixés vs largeur de
   colonne.
8. **Jointures héritées PK-integer = varchar sans lien métier** (`ur.id = usv.user_id` dans users
   et delivery_sessions) : cast texte par dialecte, en préservant la sémantique « ne matche que
   les valeurs numériques » — pas de correction fonctionnelle.

## Bug de production critique signalé en cours de chantier

**La création automatique de récompenses de fidélité est cassée en production, dans les deux
dialectes** : les deux INSERT de `customer_rewards` (UpdateLoyaltyProgress et
UpdateLoyaltyFromOrder) listaient une colonne `id` qui n'existe **ni** dans le DDL MySQL source
**ni** dans la cible (PK = `reward_id` auto-généré) — erreur « Unknown column » à chaque palier
de fidélité atteint. Corrigé dans le cadre de la conversion (colonne fantôme retirée,
l'auto-increment/identity prend le relais), même précédent que `GetUserByPIN` au rapport 25 —
**à vérifier/déployer séparément de la migration Postgres**, le bug existe identiquement en MySQL.

Anomalies préexistantes de même famille, **documentées sans correction** (comportement d'erreur
identique dans les deux dialectes) :
- `ubereats.EnableIntegration/DisableIntegration` : colonnes `access_token`/`is_active`/`updated_at`
  inexistantes partout — chemin OAuth Uber jamais fonctionnel (le test vérifie l'erreur) ;
- `scannorder.GetMerchantByQR` : scan de `qr.creation_date`/`last_waiter_call` (timestamps) dans
  des `*int64` epoch — échoue dès que la valeur est non-NULL (commit WIP récent) ;
- `orders.GetDiscountProductOptions` : ordre du Scan décalé par rapport au SELECT (résultat
  mal indexé) — figé par le test.

Deux requêtes de `scannorder` étaient par ailleurs écrites avec des placeholders `$1` en dur
(donc **cassées sous MySQL** depuis leur écriture) : remises en `?`, ce qui les répare sous MySQL
et les rend portables — seule « correction » MySQL de ce chantier avec le vet ci-dessous.

## Correctifs annexes (hors conversion stricte)

- `internal/modules/ubereats/service.go` : `fmt.Errorf("No store " + storeID)` →
  `fmt.Errorf("No store %s", storeID)`. Cette erreur `go vet` préexistante (baseline des rapports
  14/25) bloquait désormais **l'exécution de tout test du paquet** (`go test` lance vet) — la
  corriger était la condition pour tester le module ; elle sort le paquet ubereats de la liste
  d'échecs de la baseline.
- `internal/modules/upsell/postgres_integration_test.go` (Tier 1) : fixture `OrderID`
  "itest-order-1" devenue invalide depuis l'unification order_id → integer (rapports 17/18,
  postérieurs à la vérification Tier 1) → id numérique. Seule retouche hors Tier 3, purement test.

## Dépendances transitives constatées (pour le Tier 4)

- `scannorder.GetMerchantStatus` → `planning/settings.ResolvePlanningHoliday` (non converti) :
  à re-tester quand `planning` sera converti.
- `orders`/`ubereats` → `distributiontime.EstimatedSeconds` : déjà portable (rapport 23), vérifié
  au passage par les deux tests (cas « aucune donnée → 0 »).
- Le correctif Kiosk du rapport 20 est réparti entre `webhook/stripe` (Tier 2, converti) et
  `cash_registers` (converti ici, étape 3bis testée sur paiements `'KIOSK'` et NULL) ;
  `GetActiveCashRegisterID` reste dans `order_life_cycle` (Tier 4).
- `customers` : plus aucun module Tier 4 n'en dépend de façon *bloquante* pour sa propre
  conversion — `bookingcore`, `bookings`, `order_life_cycle`, `reservation`, `scannorder`
  l'importent, et tous ses points d'entrée utilisés (`UpdateOrCreateCustomer`,
  `FindCustomerByPhone`, `UpdateLoyaltyFromOrder`, `ReactivateRewards`) sont désormais portables.

## Découpage en commits atomiques (préparé, non exécuté)

Un commit par module, dans l'ordre de conversion. `DB_DIALECT=mysql` reste le défaut en prod ;
aucun commit n'a été exécuté — à faire par l'utilisateur.

| Commit suggéré | Fichiers |
|---|---|
| `postgres: convert users module to dbx` | `internal/modules/users/{repository,admin_repository,create_repository}.go`, `internal/modules/users/admin_service_test.go`, `internal/modules/users/postgres_integration_test.go` |
| `postgres: convert cash_registers module to dbx (UPDATE...FROM requalification)` | `internal/modules/cash_registers/repository.go`, `internal/modules/cash_registers/postgres_integration_test.go` |
| `postgres: convert orders module to dbx (QueryFilter sur rebind dbx)` | `internal/modules/orders/{repository,orders_fetcher_builder}.go`, `internal/modules/orders/postgres_integration_test.go` |
| `postgres: convert haccp module to dbx` | `internal/modules/haccp/repository.go`, `internal/modules/haccp/postgres_integration_test.go` |
| `postgres: convert delivery_sessions module to dbx` | `internal/modules/delivery_sessions/repository.go`, `internal/modules/delivery_sessions/postgres_integration_test.go` |
| `postgres: convert ubereats module to dbx (fix vet format string)` | `internal/modules/ubereats/{repository,service}.go`, `internal/modules/ubereats/postgres_integration_test.go` |
| `postgres: convert scannorder module to dbx (fix hardcoded $1 placeholders)` | `internal/modules/scannorder/repository.go`, `internal/modules/scannorder/postgres_integration_test.go` |
| `postgres: convert kiosk module to dbx` | `internal/modules/kiosk/repository.go`, `internal/modules/kiosk/postgres_integration_test.go` |
| `postgres: convert customers module to dbx (fix phantom customer_rewards.id)` | `internal/modules/customers/repository.go`, `internal/modules/customers/postgres_integration_test.go` |
| `postgres: fix stale upsell integration fixture after order_id unification` | `internal/modules/upsell/postgres_integration_test.go` |
| `docs: add Tier 3 postgres conversion log` | `docs/migration-postgres/27-tier3-conversion-log.md` |

> Rappel : l'arbre de travail contient toujours le chantier Tier 2 non commité (rapport 25) et la
> fonctionnalité daycomments (rapport 26) — leurs découpages respectifs restent ceux de leurs
> rapports, indépendants des commits ci-dessus.
