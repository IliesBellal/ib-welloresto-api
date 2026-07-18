# 16 — Format réel de `order_id` et ampleur d'une unification INTEGER/VARCHAR

Objectif : mesurer, sur le modèle des rapports [10](10-merchant-id-type-scope.md) (ampleur code Go)
et [11](11-merchant-id-format-check.md) (format réel), ce que contiennent les colonnes `order_id`
déjà en `varchar` (signalées par [15-fk-type-mismatch-audit.md](15-fk-type-mismatch-audit.md)) et le
coût d'une unification avec `orders.order_id integer`. Inclut l'audit à part de
`payments.cash_register_id` (§6). Analyse en **lecture seule** — aucun fichier modifié.

## Réponse

> **Même diagnostic que pour `merchant_id`, en encore plus net.** Les colonnes `order_id` en
> `varchar` stockent **l'entier auto-increment stringifié** (`"42"`), sans aucun préfixe. Le format
> naît en un point unique : `insertOrderBase` fait `strconv.FormatInt(res.LastInsertId(), 10)`.
> Côté Go, `order_id` est déjà une **string de bout en bout** : **129 signatures `orderID string`
> contre 1 en `int`** (morte), **48 champs de struct `string` contre 2 en `int`** (1 mort, 1 scanné
> mais jamais lu). Il n'y a **aucun site vivant d'arithmétique ni de conversion `Atoi/ParseInt`**
> sur `order_id`. L'unification de type est un chantier **schéma + données**, quasi gratuit côté Go.

L'ID préfixé existe mais il vit **ailleurs** : `orders.public_id` reçoit `order-<uuid>` via
`helpers.GeneratePrefixedID("order-")` ([order_life_cycle/repository.go:1649](../../internal/modules/order_life_cycle/repository.go#L1649)) —
même distinction PK historique entière / identifiant public préfixé que pour merchant (rapport 11 §2).

## 1. Le point de naissance du format — un site unique

`orders.order_id` est l'auto-increment MySQL ; sa forme string est fabriquée immédiatement après
l'INSERT, dans [order_life_cycle/repository.go:1642-1677](../../internal/modules/order_life_cycle/repository.go#L1642) :

```go
func (r *OrdersLifeCycleRepository) insertOrderBase(ctx …) (orderID string, err error) {
	…
	res, err := db.ExecContext(ctx, `INSERT INTO orders(public_id, …)`, PublicID, …)
	lastID, err := res.LastInsertId()                    // int64, auto-increment
	req.Order.OrderID = helpers.Int64ToStringPtr(lastID) // *string "42"
	…
	return strconv.FormatInt(lastID, 10), nil            // string "42"
}
```

C'est le **seul** endroit du repo où un `order_id` est créé. Toute valeur `order_id` circulant
ensuite dans le code (contexte, payloads, colonnes varchar) est cette représentation décimale.
Conséquence directe pour Postgres : ce site devra passer par le RETURNING de
[dbx](../../internal/database/dbx/db.go) (LastInsertId non supporté par pgx — le helper existe déjà).

**Propagation vérifiée vers les colonnes varchar** — toutes reçoivent ce string tel quel :

- `orderitems.order_id` ← `req.Order.OrderID` dans l'upsert des items
  ([repository.go:1336-1338](../../internal/modules/order_life_cycle/repository.go#L1336)) ;
- `customer_loyalty_progress_order.order_id` ← paramètre `orderID string`
  ([customers/repository.go:1459](../../internal/modules/customers/repository.go#L1459)) ;
- `customer_rewards.used_on_order_id` ← idem ([customers/repository.go:1329](../../internal/modules/customers/repository.go#L1329),
  [order_life_cycle/repository.go:648](../../internal/modules/order_life_cycle/repository.go#L648)) ;
- `upsell_suggestions.order_id` ← `UPDATE … SET order_id = ?` avec `OrderID *string`
  ([upsell/repository.go:110](../../internal/modules/upsell/repository.go#L110), [types.go:32](../../internal/modules/upsell/types.go#L32)).

## 2. Inventaire des 20 colonnes référençant `orders.order_id`

Le schéma cible est **déjà majoritairement integer** — contrairement à `merchant_id` (où le varchar
dominait), ici c'est le **varchar qui est minoritaire** (8 colonnes sur 20) :

| Colonne | Type actuel | Vivante (cf. 03) ? |
|---|---|---|
| **Déjà `integer` — rien à faire (12)** | | |
| `bookings.order_id`, `delivery_session.current_order_id`, `delivery_session_order.order_id`, `extra.order_id`, `order_comments.order_id`, `order_location.order_id`, `payments.order_id`, `receipts.order_id`, `stripe_payments.order_id`, `without.order_id` | `integer` | oui |
| `invoices.order_id` | `integer` | orpheline |
| `checkout_orderitems.order_item_id` *(voisin)* | `integer` | orpheline |
| **En `varchar` — à converger (8)** | | |
| `orderitems.order_id` | `varchar(20)` **dans la PK composite** | **oui — cœur du système** |
| `orders.parent_order_id` (self-ref) | `varchar(50)` | oui |
| `customer_loyalty_progress_order.order_id` | `varchar(30)` | oui |
| `customer_rewards.used_on_order_id` | `varchar(20)` | oui |
| `stock_movements.order_id` | `varchar(50)` | oui |
| `upsell_suggestions.order_id` | `varchar(64)` | oui |
| `order_changes_log.order_id` | `varchar(25)` | orpheline |
| `order_ratings.order_id` | `varchar(255)` | orpheline |

Comme les valeurs varchar sont des entiers stringifiés, la conversion `varchar → integer` des
6 colonnes vivantes est un `ALTER`/`CAST` sans transformation de valeurs — **sous réserve d'un
contrôle de données préalable** (`WHERE order_id !~ '^[0-9]+$'`) pour détecter d'éventuels résidus
PHP non numériques, et d'un traitement des `''` éventuels vers NULL sur les colonnes nullables.

Attention au cas `orderitems` : le `varchar(20)` est dans la **PK composite**
`(order_item_id, order_id, product_id)` — la conversion touche la PK et ses index, et l'upsert
`ON DUPLICATE KEY UPDATE` ([repository.go:1309](../../internal/modules/order_life_cycle/repository.go#L1309))
devra de toute façon être réécrit en `ON CONFLICT` pour PG.

## 3. Typage global Go : string quasi partout

| Mesure | `string` | `int`/`int64` | % string |
|---|---:|---:|---:|
| Signatures de fonction (`orderID …`) | **129** | **1** | 99,2 % |
| Champs de struct (`OrderID …`, dont pointeurs) | **48** (41 + 7 `*string`) | **2** | 96,0 % |

Sur **825 occurrences** de l'identifiant Go `OrderID`/`orderID` (478 lignes contenant `order_id`,
123 sites SQL `order_id = ?`, 29 répertoires), le typage entier se réduit à **3 sites**, tous listés
ci-dessous. Rappel méthodologique du rapport 10 : les sites SQL `order_id = ?` sont insensibles au
type — seul le typage statique Go compte.

## 4. Détail des 3 sites typés `int` : 2 morts, 1 vivant-inutile

| # | Site | Nature | Statut |
|---|---|---|---|
| 1 | [deliveroo/repository.go:138](../../internal/modules/deliveroo/repository.go#L138) — `GetBrandOrderIDAndMerchant(ctx, orderID int)` | Signature + `var merchantID int` en cible de Scan | **MORT** — zéro appelant dans le repo (c'est aussi l'un des 7 sites morts `merchant_id` du rapport 10) |
| 2 | [notification/notification_models.go:11](../../internal/modules/notification/notification_models.go#L11) — `NotificationMessage{ OrderID int }` | Champ de struct | **MORT** — la struct n'est instanciée nulle part ; le handler vivant du module utilise `OrderID string` ([notification_handler.go:23](../../internal/modules/notification/notification_handler.go#L23)) |
| 3 | [ubereats/models.go:38](../../internal/modules/ubereats/models.go#L38) — `UberOrderMetadata{ OrderID int \`db:"order_id"\` }` | Cible de `Scan` ([repository.go:142](../../internal/modules/ubereats/repository.go#L142)) | **VIVANT mais valeur jamais lue** — `GetOrderMetadata` est appelé 5× dans `ubereats/service.go`, mais seuls `meta.BrandOrderID` (et le paramètre `orderID string` d'origine) sont utilisés ensuite. Conversion triviale : passer le champ en `string`, ou retirer `o.order_id` du SELECT |

**Aucune arithmétique, aucun `strconv.Atoi/ParseInt` sur `order_id` dans tout le repo.** Les seules
« conversions » trouvées sont défensives et déjà compatibles string :
`fmt.Sprintf("%v", orderID)` ([ubereats/service.go:255](../../internal/modules/ubereats/service.go#L255) —
le commentaire dit explicitement « pour gérer string ou int ») et `fmt.Sprintf("%s", orderID)`
([:500](../../internal/modules/ubereats/service.go#L500)). Le tri `ORDER BY order_id DESC`
([order_life_cycle/repository.go:1691](../../internal/modules/order_life_cycle/repository.go#L1691))
repose sur l'ordre *numérique* de la colonne integer — il resterait faux sur un varchar, argument
de plus pour converger vers integer plutôt que l'inverse.

### Verdict d'unification

Contrairement à `merchant_id` (unifié vers **string/varchar**, rapports 12-13), pour `order_id` le
sens naturel est **l'inverse : tout vers `integer`**, car (a) la PK est un auto-increment integer
conservé tel quel dans le schéma cible, (b) 12 colonnes référentes sur 20 sont déjà integer,
(c) le code Go, en string, est **insensible au type de colonne** (le driver stringifie en lecture ;
en écriture, un paramètre string numérique vers une colonne integer est à valider avec pgx — à
couvrir par les tests d'intégration dbx). Coût Go total : **1 champ de struct** (site 3), plus
2 suppressions de code mort optionnelles.

## 5. Pas de préfixe `order` sur `order_id` — mais un voisin piège

Le générateur d'IDs préfixés ([helpers/ids.go:63](../../internal/helpers/ids.go#L63)) cite justement
`"order-xxxx-xxxx"` en exemple… mais son unique usage « order » alimente **`orders.public_id`**,
colonne dédiée ([order_life_cycle/repository.go:1649](../../internal/modules/order_life_cycle/repository.go#L1649)),
jamais `order_id`. Les `order_item_id` suivent le même schéma que `order_id` :
auto-increment stringifié via `Int64ToStringPtr` ([repository.go:1356](../../internal/modules/order_life_cycle/repository.go#L1356), [:1997](../../internal/modules/order_life_cycle/repository.go#L1997)).

## 6. À part : `payments.cash_register_id` — sentinelles confirmées, modèle « parking puis rattachement »

Les sentinelles signalées par le rapport 15 sont confirmées, et il y en a une **troisième** :
`'KIOSK'`. Le modèle réel est un cycle en deux temps :

**Phase 1 — au paiement, la colonne « gare » la provenance quand aucune caisse n'est ouverte :**

| Écrivain | Valeur écrite | Site |
|---|---|---|
| POS (paiement/remboursement) | id numérique stringifié de la caisse ouverte (`GetActiveCashRegisterID` scanne `cash_registers.cash_register_id` INTEGER dans un `sql.NullString` — [order_life_cycle/repository.go:82-92](../../internal/modules/order_life_cycle/repository.go#L82)) | [service.go:406](../../internal/modules/order_life_cycle/service.go#L406), [:946](../../internal/modules/order_life_cycle/service.go#L946) |
| Paiements embarqués à la création de commande | recopie de `orders.cash_register_id` — donc id numérique **ou sentinelle** selon le canal | [repository.go:2132](../../internal/modules/order_life_cycle/repository.go#L2132) |
| ScanNOrder (commande) | `'SCANNORDER'` dans `orders.cash_register_id` ([scannorder/service.go:917-921](../../internal/modules/scannorder/service.go#L917)) |
| Kiosk (commande) | `'KIOSK'` dans `orders.cash_register_id` ([kiosk/service.go:80](../../internal/modules/kiosk/service.go#L80), [:1553](../../internal/modules/kiosk/service.go#L1553)) — « même convention que SCANNORDER » dixit le commentaire |
| Webhook Stripe (paiement SNO) | `'SCANNORDER'` (constante `models.ScanNOrderCashRegisterID`, [users_models.go:12](../../internal/models/users_models.go#L12)) | [webhook/stripe/service.go:130](../../internal/webhook/stripe/service.go#L130) |
| Webhook Stripe (`InsertPayment`, marqué `// Decom`) | **NULL** (colonne absente de l'INSERT) | [webhook/stripe/repository.go:97](../../internal/webhook/stripe/repository.go#L97) |
| *(aucun site Go n'écrit `'UBER_EATS'`/`'DELIVEROO'`)* | valeurs héritées (PHP) ou NULL — le code Go les tolère seulement en lecture | — |

**Phase 2 — à la clôture de caisse, requalification vers l'id numérique**
([cash_registers/repository.go:292-315](../../internal/modules/cash_registers/repository.go#L292)) :

```sql
UPDATE payments p INNER JOIN orders o ON o.order_id = p.order_id
SET p.cash_register_id = ?          -- id numérique de la caisse qui clôture
WHERE o.state = 'CLOSED' AND p.mop = 'STRIPE' AND p.cash_register_id = 'SCANNORDER' …
-- puis idem pour p.mop IN ('UBER_EATS','DELIVEROO')
--   AND (p.cash_register_id IS NULL OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))
```

**Lecteurs** (tous comparent à un id numérique stringifié) :
rapport de caisse `WHERE p.cash_register_id = ?` ([:597](../../internal/modules/cash_registers/repository.go#L597)) ;
historique avec **jointure hybride** `pstats.cash_register_id = cr.cash_register_id`
(varchar(20) ↔ INTEGER — [:862-868](../../internal/modules/cash_registers/repository.go#L862)),
qui casse en PG ; côté `orders`, `WHERE o.cash_register_id = ?` ([:277](../../internal/modules/cash_registers/repository.go#L277)).

**Conséquences pour la décision (non prise ici)** :

1. La colonne est **structurellement hybride** : un simple passage en `integer` est impossible sans
   éliminer d'abord l'état transitoire (sentinelles + NULL avant clôture). `orders.cash_register_id
   varchar(11)` porte exactement le même modèle (`'SCANNORDER'`/`'KIOSK'` à la création).
2. Les jointures/égalités hybrides à traiter pour PG sont au nombre de **3** :
   [:277](../../internal/modules/cash_registers/repository.go#L277),
   [:597](../../internal/modules/cash_registers/repository.go#L597),
   [:868](../../internal/modules/cash_registers/repository.go#L868) — plus les deux UPDATE de
   requalification qui écrivent un id numérique dans une colonne comparée à des littéraux texte.
3. Pistes évoquées (rapport 15 §6.2) : colonne `source`/`channel` séparée portant
   SCANNORDER/KIOSK/UBER_EATS/DELIVEROO + `cash_register_id` purement numérique nullable ; ou
   conserver le varchar et caster les 3 sites de jointure. À trancher avec les données réelles
   (part de lignes encore en sentinelle en prod).

## 7. Synthèse

| Question | Réponse |
|---|---|
| Format des `order_id` varchar | Entier auto-increment stringifié (`"42"`), né en un site unique (`insertOrderBase`) |
| Préfixe type `ord-<uuid>` | Non — le préfixé existe mais sur `orders.public_id`, colonne séparée |
| Sites Go typés int | 3 (2 morts, 1 champ scanné jamais lu) — modules `deliveroo`, `notification`, `ubereats` |
| Sens d'unification naturel | `varchar → integer` sur 6 colonnes vivantes (+2 orphelines à ne pas porter), inverse du chantier merchant_id |
| Coût Go | ~1 champ de struct à retyper ; aucun `strconv`, aucune arithmétique |
| `payments.cash_register_id` | Sentinelles `'SCANNORDER'`/`'KIOSK'` écrites par Go, `'UBER_EATS'`/`'DELIVEROO'` héritées tolérées, requalifiées en id numérique à la clôture de caisse — colonne hybride, décision à part |
