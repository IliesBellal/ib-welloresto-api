# 10 — Ampleur réelle d'un changement `merchant_id` INTEGER → VARCHAR (code Go)

Objectif : mesurer le coût réel, côté code Go uniquement, d'une conversion de `merchant_id`
en `varchar` sur les 56 tables historiques où il est encore `INTEGER`.

Analyse en lecture seule. Aucun fichier source modifié. `go build ./...` passe (état de référence).

## Verdict en une ligne

> **Intuition CONFIRMÉE, et largement.** `merchant_id` est déjà traité comme une **string de bout en
> bout** dans quasiment tout le code : **359 signatures de fonction `merchantID string` contre 3 en
> `int64`**. Il reste **18 sites Go vivants** à toucher, concentrés dans **6 modules**, dont **aucun**
> sur 38 des 40 tables vivantes concernées. Ce n'est pas un chantier massif : c'est une poignée
> d'îlots isolés, déjà identifiés comme de la dette PHP par des commentaires dans le code.

## Périmètre : 40 tables vivantes / 56

Croisement avec [03-table-usage-audit.md](03-table-usage-audit.md) §2 : **16 des 56 tables sont des
orphelines confirmées** (aucune requête Go), donc hors périmètre de tout effort de portage Go :

`average_distribution_time_by_category`, `average_distribution_time_history`, `cash_reports`,
`category_discount`, `consumables`, `employment_agreement`, `employment_contract`,
`integration_deliveroo_components_mapping`, `integration_uber_eats_components_mapping`, `invoices`,
`merchant_code`, `pictures`, `planned_shifts`, `planning_roles`, `shift_templates`, `users_nfc_tags`.

Restent **40 tables vivantes**, totalisant **754 sites SQL** en Go (`FROM`/`JOIN`/`INSERT INTO`/`UPDATE`/`DELETE FROM`).

## Le point méthodologique décisif

**Les sites SQL ne comptent pas comme des sites à toucher.** Un littéral `WHERE merchant_id = ?`
fonctionne à l'identique que la colonne soit `integer` ou `varchar` : c'est le driver qui lie le
paramètre, et il lie ce que Go lui donne. Les **589 lignes `merchant_id = ?`** et les **1268 lignes**
Go contenant `merchant_id` sont donc **insensibles au type**.

Le seul code qui casse est celui où **le type statique Go est un entier** — un `int`/`int64` en
signature, en champ de struct, ou en cible de `Scan()`. C'est ce que compte ce document.

## 1. Typage global : string quasi partout

| Mesure | `string` | `int`/`int64` | % string |
|---|---:|---:|---:|
| Signatures de fonction (`merchantID …`) | **359** | **3** | **99,2 %** |
| Champs de struct (`MerchantID …`) | **111** | **8** | **93,3 %** |

Sur 3440 occurrences de l'identifiant Go `MerchantID`/`merchantID`, le typage entier est résiduel.

Deux commentaires dans le code disent explicitement que l'entier est une survivance PHP et que la
cible est la string — l'intention est déjà écrite noir sur blanc :

- [internal/models/request_objects.go:870](../../internal/models/request_objects.go#L870) —
  `MerchantID string // kept as string to match your desired future ids`
- [internal/modules/order_life_cycle/repository.go:556](../../internal/modules/order_life_cycle/repository.go#L556) —
  `// convert int64 -> string to match your models (in PHP merchant_id was int)`

## 2. Total global : 18 sites vivants à toucher (+ 7 morts)

| Statut | Sites | Signification |
|---|---:|---|
| **Vivants — à convertir** | **18** | Friction réelle |
| Morts — à supprimer ou ignorer | 7 | Code jamais exécuté (voir §4) |
| **Total sites typés `int`** | **25** | |

À comparer aux 754 sites SQL et 1268 lignes `merchant_id` : **le typage entier ne concerne que ~2 %
des points de contact avec `merchant_id`**.

## 3. Modules les plus coûteux (les 6 seuls concernés)

Il n'y a pas 10 modules à lister — il n'y en a que **6** avec de la friction vivante :

| # | Module | Sites | Nature | Table(s) SQL visée(s) |
|---|---|---:|---|---|
| 1 | `messaggio` | **7** | Chaîne `int64` complète (interfaces + impl + struct) | `merchant_sms_monthly`, `merchant_marketing_settings` |
| 2 | `googlemaps` | **4** | Interface + impl `int64` + `ParseInt` | `merchant_google_maps_monthly` |
| 3 | `order_life_cycle` | **3** | `sql.NullInt64` + `Sprintf` + champ struct `int` | `orders` |
| 4 | `delivery_sessions` | **2** | `strconv.ParseInt` puis passage en `int64` | *(pont vers messaggio)* |
| 5 | `deliveroo` | **1** | `var merchantID int` en cible de `Scan` | `orders` |
| 6 | `bookings` | **1** | Champ struct `int` en cible de `Scan` | `merchant` (`m.id`) |

### Détail des 18 sites

**`messaggio` (7)** — le seul vrai îlot `int64` de bout en bout :
[marketing_repository.go:9](../../internal/modules/messaggio/marketing_repository.go#L9),
[:10](../../internal/modules/messaggio/marketing_repository.go#L10),
[:21](../../internal/modules/messaggio/marketing_repository.go#L21),
[:65](../../internal/modules/messaggio/marketing_repository.go#L65),
[models.go:4](../../internal/modules/messaggio/models.go#L4),
[service.go:11](../../internal/modules/messaggio/service.go#L11),
[:48](../../internal/modules/messaggio/service.go#L48).

**`delivery_sessions` (2)** — le pont qui alimente messaggio :
[service.go:55](../../internal/modules/delivery_sessions/service.go#L55) (`strconv.ParseInt(merchantID, 10, 64)`),
[:75](../../internal/modules/delivery_sessions/service.go#L75).

**`googlemaps` (4)** : [repository.go:13](../../internal/modules/googlemaps/repository.go#L13),
[:34](../../internal/modules/googlemaps/repository.go#L34),
[service.go:51](../../internal/modules/googlemaps/service.go#L51) (`ParseInt`),
[:57](../../internal/modules/googlemaps/service.go#L57).

**`order_life_cycle` (3)** : [models.go:6](../../internal/modules/order_life_cycle/models.go#L6)
(`DeliveredOrderMetadata.MerchantID int`, cible des `Scan` en
[:736](../../internal/modules/order_life_cycle/repository.go#L736) et
[:818](../../internal/modules/order_life_cycle/repository.go#L818)),
[repository.go:543](../../internal/modules/order_life_cycle/repository.go#L543) (`sql.NullInt64`),
[:557](../../internal/modules/order_life_cycle/repository.go#L557) (`fmt.Sprintf("%d", …)`).

**`deliveroo` (1)** : [repository.go:72](../../internal/modules/deliveroo/repository.go#L72) —
`var merchantID int` scanné depuis `orders.merchant_id`. Vivant (appelé en
[service.go:66 et :101](../../internal/modules/deliveroo/service.go#L66)).

**`bookings` (1)** : [models.go:225](../../internal/modules/bookings/models.go#L225) —
`MerchantBookingParams.MerchantID int`, cible du `Scan` de `m.id` en
[repository.go:1345](../../internal/modules/bookings/repository.go#L1345). À noter : un **doublon de
cette struct existe déjà en `string`** ([internal/models/bookings_availability_models.go:19](../../internal/models/bookings_availability_models.go#L19),
utilisée par `locations`) — les deux versions cohabitent, l'une `int`, l'autre `string`.

### 4 conversions explicites seulement

Sur tout le repo, il n'existe que **4 conversions explicites** liées à `merchant_id` (les autres hits
`strconv`/`Sprintf` proches du mot « merchant » portent sur des années, montants, codes HTTP ou
compteurs de logs — faux positifs) :

| Site | Conversion | Devient après migration |
|---|---|---|
| [delivery_sessions/service.go:55](../../internal/modules/delivery_sessions/service.go#L55) | `strconv.ParseInt(merchantID, 10, 64)` | **supprimable** |
| [googlemaps/service.go:51](../../internal/modules/googlemaps/service.go#L51) | `strconv.ParseInt(merchantID, 10, 64)` | **supprimable** |
| [order_life_cycle/repository.go:557](../../internal/modules/order_life_cycle/repository.go#L557) | `fmt.Sprintf("%d", merchantID.Int64)` | **supprimable** |
| [order_life_cycle/repository.go:543](../../internal/modules/order_life_cycle/repository.go#L543) | `sql.NullInt64` → `sql.NullString` | 1 ligne |

Point important : ces conversions existent **parce que** la colonne est `integer` alors que le code
veut une string. Passer en `varchar` ne crée pas de travail ici — **il en supprime**. Les deux
`ParseInt` disparaissent purement et simplement, et avec eux deux chemins d'erreur
(`invalid merchant id`) aujourd'hui possibles en production.

## 4. Zéro arithmétique, zéro comparaison numérique

Recherche de `merchant_id` dans une opération arithmétique, un `SUM`/`MAX`/`MIN`/`AVG`, un `BETWEEN`,
une comparaison `<`/`>`, ou un tri sémantique : **un seul hit sur tout le repo**, et il est hors
périmètre —
[bookings/waitlist_repository.go:224](../../internal/modules/bookings/waitlist_repository.go#L224) :
`ORDER BY merchant_id, created_at ASC` sur la table `booking_waitlist` (qui n'est pas dans la liste
des 56). C'est un tri de **regroupement** pour un cron, pas un ordre numérique signifiant : en
`varchar` il trie lexicographiquement au lieu de numériquement, ce qui ne change rien au
comportement (le cron itère sur tout le résultat).

Les hits Go de type `merchantID + …` sont tous de la **concaténation de chaînes** — par exemple
[redis/client.go:115](../../internal/infrastructure/redis/client.go#L115) et
[scannorder/service.go:1169](../../internal/modules/scannorder/service.go#L1169)
(`models.ScannorderMerchantUpsell + merchantID`, construction de clé de cache). C'est une preuve
supplémentaire, et non une friction : **le code fait déjà de la string avec `merchant_id`**.

**Aucun site ne dépend de la nature entière de `merchant_id`.**

## 5. Les 7 sites morts (à ne pas compter dans l'effort)

Vérifiés sans appelant ni écrivain — ils ne s'exécutent jamais :

| Site | Type | Preuve |
|---|---|---|
| [request_logger/middleware.go:43](../../internal/middleware/request_logger/middleware.go#L43), [:44](../../internal/middleware/request_logger/middleware.go#L44), [models.go:5](../../internal/middleware/request_logger/models.go#L5) | `*int64` | `models.ContextMerchantID`/`ContextUserID` ne sont **jamais écrits** (aucun `context.WithValue` correspondant dans le repo) → toujours `nil` → **`api_request_logs.merchant_id` est toujours NULL en prod** |
| [orders/repository.go:364](../../internal/modules/orders/repository.go#L364) | `ValidateProducts(… merchantID int64 …)` | aucun appelant |
| [deliveroo/repository.go:142](../../internal/modules/deliveroo/repository.go#L142) | `GetBrandOrderIDAndMerchant(… ) (string, int, error)` | aucun appelant |
| [models/request_objects.go:904](../../internal/models/request_objects.go#L904) | `DeleteOrderRequest.MerchantID int` | struct jamais instanciée |
| [notification/notification_models.go:10](../../internal/modules/notification/notification_models.go#L10) | `NotificationMessage.MerchantID int` | struct jamais instanciée |

Le cas `request_logger` mérite une remontée à part : ce n'est pas seulement du code mort typé `int`,
c'est un **bug silencieux existant** — la colonne `merchant_id` d'`api_request_logs` n'est jamais
renseignée, indépendamment de toute migration.

## 6. Répartition par table : 38 des 40 sont indolores

En croisant les 18 sites vivants avec les tables qu'ils interrogent réellement :

| Table (dans les 40) | Sites `int` vivants | Détail |
|---|---:|---|
| `orders` | **4** | `deliveroo:72` + `order_life_cycle` (models.go:6 → scans 736/818, repository.go:543/557) |
| `api_request_logs` | 0 *(3 morts)* | `request_logger` — code mort, colonne toujours NULL |
| `qrcodes` | **0** | messaggio ne joint `qrcodes` que par `qr.merchant_id = mms.merchant_id` (colonne à colonne, aucun paramètre Go lié) |
| **Les 37 autres** | **0** | `users`, `users_rights`, `bookings`, `customer`, `components`, `orderitems`, `delivery_session`, `merchant_parameters`, `payments`, `scannorder_settings`, `cash_registers`, `hours_of_operation`, `integration_*`, `stripe_accounts`, `productcateg`, `locations`, `discounts`, `recipes`, `subscriptions`, `floors`, `extra`, `component_category`, `cash_desks`, `without`, `barcodes`, `receipts`, `restaurant_ticket`, `subscription_invoices`, `welloresto_stripe_customers`, `services_performed`, `purchased_components`, `expiration_dates`, `average_distribution_time`… |

**Une seule table sur les 40 — `orders` — porte de la friction `int` réelle**, sur 4 sites dans
2 modules. Les tables les plus massivement requêtées (`users` : 54 sites SQL, `users_rights` : 43,
`bookings` : 43, `customer` : 40, `components` : 40) sont **déjà 100 % string** côté Go.

### Effet de bord à anticiper

Les îlots `int64` les plus gros (`messaggio` 7 sites, `googlemaps` 4) ne portent **pas** sur les
tables de la liste des 56 : ils écrivent dans `merchant_sms_monthly`,
`merchant_marketing_settings` et `merchant_google_maps_monthly` — trois tables satellites qui
stockent elles aussi un `merchant_id`. Elles devront être converties **dans le même lot** sous peine
d'incohérence de type entre `merchant.id` et ces tables. C'est le vrai « angle mort » de la liste
fournie : **11 des 18 sites vivants concernent des tables hors périmètre déclaré**.

## Conclusion

L'inquiétude d'un chantier massif et mêlé n'est **pas confirmée par le code**. Les faits :

- **99,2 %** des signatures passent déjà `merchantID` en `string` ;
- **zéro** arithmétique et **zéro** comparaison numérique sur `merchant_id` ;
- **4** conversions explicites au total, toutes **supprimées** par la migration (pas ajoutées) ;
- **18 sites vivants** dans **6 modules**, dont **une seule table** (`orders`) parmi les 40 ;
- deux commentaires dans le code désignent déjà l'entier comme de la dette PHP à résorber.

Le code Go a manifestement **déjà été écrit en anticipant cette migration**. Le risque résiduel
n'est pas dans le nombre de sites — il est dans les **`Scan()` silencieux** : un
`Scan(&merchantID)` vers un `int` échoue **à l'exécution**, pas à la compilation. Les 4 sites de
`Scan` vers un entier ([deliveroo:72](../../internal/modules/deliveroo/repository.go#L72),
[order_life_cycle:736](../../internal/modules/order_life_cycle/repository.go#L736) et
[:818](../../internal/modules/order_life_cycle/repository.go#L818),
[bookings:1345](../../internal/modules/bookings/repository.go#L1345)) sont donc les points à traiter
en priorité et à couvrir par un test : `go build` ne les attrapera pas.

### Vérifications restantes (hors code Go, non couvertes ici)

Cet audit ne regarde **que** le Go de ce repo. Avant de conclure sur la faisabilité globale :

- la procédure stockée `GET_AVERAGE_DISTRIBUTION_TIME` (appelée en
  [orders/repository.go:857](../../internal/modules/orders/repository.go#L857)) manipule `merchant_id`
  hors du repo — son corps doit être relu ;
- les clients (Flutter, kiosk, scannorder) peuvent sérialiser `merchant_id` en nombre JSON — à
  vérifier côté contrats d'API ;
- tout composant externe écrivant en base (ancien back PHP, BI, cron) est invisible à ce grep.
