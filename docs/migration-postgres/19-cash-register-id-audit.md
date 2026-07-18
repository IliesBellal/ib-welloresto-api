# 19 — Audit exhaustif de `payments.cash_register_id` et `orders.cash_register_id`

Objectif : cartographier tous les sites Go (lecture/écriture) touchant ces deux colonnes, confirmer
la liste des sentinelles, documenter précisément le mécanisme de requalification à la clôture de
caisse ([cash_registers/repository.go:290-316](../../internal/modules/cash_registers/repository.go#L290)),
et vérifier si `GET_CASH_REGISTER_REPORT`/`GET_CASH_REGISTER_REPORT_MOP` ont un angle mort sur les
lignes encore « parkées ». Complète le rapport [16 §6](16-order-id-format-check.md#6-à-part-paymentscash_register_id--sentinelles-confirmées-modèle--parking-puis-rattachement).
Analyse en **lecture seule** — aucun fichier modifié, aucune donnée de prod interrogée (voir §5).

## Résumé exécutif

> Le modèle « parking puis rattachement » décrit dans le rapport 16 est confirmé, mais **deux
> corrections factuelles importantes** par rapport à ce rapport :
>
> 1. **`'UBER_EATS'` et `'DELIVEROO'` ne sont pas que des reliquats PHP** : le Go actuel les
>    **écrit activement** dans `orders.cash_register_id` à la création de commande, pour tous les
>    canaux Uber Eats et Deliveroo ([deliveroo_orders/service.go:343](../../internal/webhook/deliveroo_orders/service.go#L343),
>    [ubereats/service/order_mapper.go:41](../../internal/webhook/ubereats/service/order_mapper.go#L41)).
>    Le rapport 16 (§6, tableau) affirmait l'inverse (« aucun site Go n'écrit UBER_EATS/DELIVEROO »)
>    — c'est **inexact**, corrigé ici.
> 2. **La requalification (phase 2) ne s'applique qu'à `payments.cash_register_id`, jamais à
>    `orders.cash_register_id`.** Une commande ScanNOrder/Kiosk/UberEats/Deliveroo garde sa
>    sentinelle dans `orders.cash_register_id` **pour toujours** — ce n'est pas un état
>    transitoire pour cette colonne, contrairement à ce que le modèle « parking puis
>    rattachement » suggère pour l'ensemble. Seule `payments.cash_register_id` est requalifiée.
>
> Par ailleurs, deux cas identifiés où **`payments.cash_register_id` peut rester indéfiniment non
> numérique** (voir §4.3) : les paiements Kiosk Stripe Terminal (`NULL` permanent, jamais couvert
> par les deux `UPDATE` de clôture) et, plus largement, tout paiement dont le canal/MOP ne
> correspond à aucune des deux branches de l'`UPDATE` de clôture.
>
> Les deux procédures stockées `GET_CASH_REGISTER_REPORT`/`GET_CASH_REGISTER_REPORT_MOP` ne sont
> **définies nulle part dans ce repo** (corps SQL introuvable — confirmé par [01-audit.md:82](01-audit.md#L82) :
> `grep CREATE PROCEDURE` = 0 occurrence, exactement comme `user_status_view`). L'analyse du §6 se
> fonde donc sur les sites d'appel et sur une requête Go équivalente (`GetCashRegisterSummary`,
> mêmes filtres), pas sur le corps des procédures — à confirmer si vous pouvez coller leur SQL.

---

## 1. Inventaire complet des sites Go — `orders.cash_register_id`

### 1.1 Écriture

| # | Site | Valeur posée | Contexte métier |
|---|---|---|---|
| 1 | [order_life_cycle/repository.go:1060-1071](../../internal/modules/order_life_cycle/repository.go#L1060) (dans `CreateOrder`) | Si `req.DeviceID` fourni et non vide : id numérique stringifié de la caisse active du device (`GetActiveCashRegisterID`). Si `req.DeviceID` nil **et** `req.Order.CashRegisterId` déjà renseigné : **valeur laissée telle quelle** (aucune caisse active requise). Sinon : erreur `ErrDeviceIDMissing`. | POS (device_id fourni → id numérique) vs canaux sans device physique (ScanNOrder/Kiosk/UberEats/Deliveroo, qui pré-remplissent `CashRegisterId` en amont — voir lignes 2-5) |
| 2 | [order_life_cycle/repository.go:1653,1657](../../internal/modules/order_life_cycle/repository.go#L1653) (`insertOrderBase`, `INSERT INTO orders(...cash_register_id...)`) | Recopie de `req.Order.CashRegisterId` (déjà résolu par le site 1) | Point d'écriture SQL unique pour `orders.cash_register_id` — aucun `UPDATE orders SET cash_register_id` ailleurs dans le repo (confirmé par grep exhaustif, voir §1.2) |
| 3 | [scannorder/service.go:917-921](../../internal/modules/scannorder/service.go#L917) | `'SCANNORDER'` (littéral local `ScannorderOwner`) | Commande ScanNOrder (QR code table) — `DeviceID` n'est jamais renseigné sur le `RequestObject` envoyé à `CreateOrder` ⇒ passe par la branche « valeur déjà posée » du site 1 |
| 4 | [kiosk/service.go:1552-1553](../../internal/modules/kiosk/service.go#L1552) (const `kioskCashRegister = "KIOSK"`, [:78-80](../../internal/modules/kiosk/service.go#L78)) | `'KIOSK'` | Commande borne kiosque — même schéma (pas de `DeviceID` sur le `RequestObject`) |
| 5a | [webhook/deliveroo_orders/service.go:343,366](../../internal/webhook/deliveroo_orders/service.go#L343) | `models.BrandDeliveroo` = `'DELIVEROO'` ([request_objects.go:860](../../internal/models/request_objects.go#L860)) | Commande créée par le webhook Deliveroo |
| 5b | [webhook/ubereats/service/order_mapper.go:41,54](../../internal/webhook/ubereats/service/order_mapper.go#L41) | `models.BrandUberEats` = `'UBER_EATS'` ([request_objects.go:859](../../internal/models/request_objects.go#L859)) | Commande créée par le webhook Uber Eats |

**Aucun site ne pose jamais NULL explicitement sur `orders.cash_register_id`** — la colonne est
`DEFAULT NULL` en DDL ([wello-resto-mysql-ddl.md:2027](wello-resto-mysql-ddl.md#L2027) `varchar(11) DEFAULT NULL`),
donc NULL ne peut survenir que si aucun des 5 sites n'alimente `CashRegisterId` avant l'insert — ce
qui est bloqué par le `else return ErrDeviceIDMissing` du site 1 (ligne 1069-1071) sauf si
`DeviceID == nil` **et** `CashRegisterId` non nil (branche empruntée par 3/4/5a/5b). En clair : à ce
jour, un ordre ne peut être inséré qu'avec soit un id numérique (POS), soit une des 3 sentinelles
`'SCANNORDER'`/`'KIOSK'`/`'UBER_EATS'`/`'DELIVEROO'` (4 sentinelles en tout) — jamais NULL en écriture
vivante.

### 1.2 Lecture (comparaisons/jointures)

| Site | Requête | Remarque |
|---|---|---|
| [cash_registers/repository.go:277](../../internal/modules/cash_registers/repository.go#L277) (`CloseCashRegister`, étape 1) | `WHERE o.cash_register_id = ?` avec `?` = id numérique de la caisse qu'on ferme | Ne peut matcher que des orders déjà posés avec l'id numérique (POS) — les orders ScanNOrder/Kiosk/UberEats/Deliveroo, dont `cash_register_id` est une sentinelle, ne sont **jamais** retournés par cette requête, quel que soit leur état |
| [orders/orders_fetcher_builder.go:611,616](../../internal/modules/orders/orders_fetcher_builder.go#L611) | `LEFT JOIN cash_registers cr on cr.cash_register_id = o.cash_register_id` puis `case when cr.end_date is null AND cr.cash_register_id is not null then false else true end as closed` | Pour tout order posé sur une sentinelle, la jointure ne matche jamais (`cash_registers.cash_register_id` est `int(11)` PK) ⇒ `cr.*` est NULL ⇒ `ord.CashRegister` reste `nil` côté Go ([:746-751](../../internal/modules/orders/orders_fetcher_builder.go#L746)). **Effet permanent**, pas transitoire, puisque `orders.cash_register_id` n'est jamais requalifié (§3) |
| [order_life_cycle/repository.go:2132](../../internal/modules/order_life_cycle/repository.go#L2132) (`insertPayments`) | `CashRegisterID: *req.Order.CashRegisterId` | Recopie la valeur de l'order (numérique ou sentinelle) vers le paiement embarqué — voir §2.1 ligne 2 |

---

## 2. Inventaire complet des sites Go — `payments.cash_register_id`

### 2.1 Écriture

| # | Site | Valeur posée | Contexte métier |
|---|---|---|---|
| 1 | [order_life_cycle/service.go:397-406](../../internal/modules/order_life_cycle/service.go#L397) (`AddPayment`, paiement manuel POS) | `GetActiveCashRegisterID(merchantID, req.DeviceID)` → id numérique de la caisse ouverte du device, ou `""` si aucune (convertie en NULL, voir site 4) | Encaissement comptoir/POS (tous MOP) — nécessite un `DeviceID` dans la requête HTTP |
| 2 | [order_life_cycle/service.go:918-946](../../internal/modules/order_life_cycle/service.go#L918) (remboursement) | Idem — `activeRegister` du device qui effectue le remboursement (peut différer de la caisse d'origine du paiement) | Remboursement POS — commentaire ligne 946 : « On l'attache au registre d'AUJOURD'HUI » |
| 3 | [order_life_cycle/repository.go:2128-2136](../../internal/modules/order_life_cycle/repository.go#L2128) (`insertPayments`, paiements embarqués à la création de commande) | `*req.Order.CashRegisterId` — recopie **telle quelle** la valeur déjà posée sur l'order (numérique **ou** sentinelle) | N'importe quel canal, si `req.Order.Payments` est non vide à la création (POS avec paiement immédiat typiquement) |
| 4 | [order_life_cycle/repository.go:176-191](../../internal/modules/order_life_cycle/repository.go#L176) (`AddPaymentAndReturnID`, point d'insertion SQL unique) | Convertit `payment.CashRegisterID == ""` → `NULL` (`sql.NullString`) ; sinon passe la valeur telle quelle | Fonction bas niveau utilisée par tous les sites ci-dessus **et** par le webhook Stripe (site 6) — c'est le site où NULL apparaît réellement en pratique |
| 5 | [webhook/stripe/service.go:130](../../internal/webhook/stripe/service.go#L130) (Checkout carte en ligne ScanNOrder) | `models.ScanNOrderCashRegisterID` = `'SCANNORDER'` ([users_models.go:12](../../internal/models/users_models.go#L12)) | Paiement Stripe Checkout d'une commande ScanNOrder, MOP = `STRIPE` |
| 6 | [webhook/stripe/service.go:431-444](../../internal/webhook/stripe/service.go#L431) (`recordTerminalPayment`, Stripe Terminal Kiosk) | `CashRegisterID` **non renseigné** dans le struct `models.Payment{}` → `""` Go zero-value → **NULL** via le site 4 | Paiement carte via Terminal physique sur borne Kiosk — commentaire explicite ligne 426 : « une borne n'a pas de caisse » |
| 7 | [webhook/stripe/repository.go:97](../../internal/webhook/stripe/repository.go#L97) (`InsertPayment`, marqué `// Decom`) | NULL (colonne absente de la liste de colonnes de l'`INSERT`) | Chemin marqué décommissionné — a confirmer qu'il n'est plus appelé en production, hors périmètre de cet audit |
| — | *(aucun site Go)* | `'UBER_EATS'`/`'DELIVEROO'` directement sur `payments` | Ces sentinelles arrivent sur `payments.cash_register_id` **uniquement par recopie** depuis `orders.cash_register_id` via le site 3 (si un paiement est embarqué à la création d'une commande UberEats/Deliveroo) — en pratique, les paiements UberEats/Deliveroo transitent plutôt par des flux d'intégration dédiés hors du scope Go direct de cet audit (webhooks de paiement plateforme, non examinés ici) |

### 2.2 Lecture (comparaisons/jointures)

| Site | Requête | Remarque |
|---|---|---|
| [cash_registers/repository.go:597](../../internal/modules/cash_registers/repository.go#L597) (`GetCashRegisterSummary`) | `WHERE p.cash_register_id = ?` (`?` = id numérique) | Ne retourne que les paiements déjà requalifiés — si un paiement Kiosk Terminal (NULL, jamais requalifié, §4.3) devait un jour être attendu ici, il resterait invisible en permanence |
| [cash_registers/repository.go:862-868](../../internal/modules/cash_registers/repository.go#L862) (`GetCashRegisterHistory`, sous-requête `pstats`) | `pstats.cash_register_id = cr.cash_register_id` (jointure hybride `varchar(20)` ↔ `int(11)`) | Casse en Postgres typé strict (déjà documenté rapport 15) |

---

## 3. Sentinelles confirmées — liste exhaustive

Recherche de tous les littéraux comparés/assignés à ces deux colonnes dans tout le repo Go :

| Sentinelle | Écrite par le Go actuel ? | Site(s) d'écriture | Lue/comparée où |
|---|---|---|---|
| `'SCANNORDER'` | ✅ Oui | `orders.cash_register_id` (scannorder/service.go:921) ; `payments.cash_register_id` (webhook/stripe/service.go:130, et recopie depuis orders) | `cash_registers/repository.go:297` (`WHERE p.cash_register_id = 'SCANNORDER'`) |
| `'KIOSK'` | ✅ Oui | `orders.cash_register_id` uniquement (kiosk/service.go:1553) — **jamais** posée sur `payments` par un site direct (le paiement Kiosk Terminal est NULL, pas `'KIOSK'` — voir §2.1 site 6) | Aucune comparaison `payments.cash_register_id = 'KIOSK'` trouvée nulle part — confirmé par grep exhaustif du littéral `KIOSK` dans le repo |
| `'UBER_EATS'` | ✅ Oui (correction vs rapport 16) | `orders.cash_register_id` (ubereats/service/order_mapper.go:41,54) | `cash_registers/repository.go:310-311` (`p.mop IN ('UBER_EATS','DELIVEROO') AND (... OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))`) |
| `'DELIVEROO'` | ✅ Oui (correction vs rapport 16) | `orders.cash_register_id` (deliveroo_orders/service.go:343,366) | Idem ligne ci-dessus |
| `NULL` | ✅ Oui, sur `payments` seulement | Site 4/6 §2.1 (Kiosk Terminal, paiement sans device) | `cash_registers/repository.go:311` (branche `IS NULL`) — **uniquement** dans la branche `mop IN ('UBER_EATS','DELIVEROO')`, pas dans la branche `STRIPE`/`SCANNORDER` (voir §4.3) |

**Aucune autre sentinelle textuelle** n'a été trouvée en grepant tous les littéraux quotés comparés
ou assignés aux deux colonnes dans `internal/`. Le rapport 16 mentionnait ces 4 valeurs +
NULL — confirmé complet, à la correction near du statut « héritées PHP » de `UBER_EATS`/`DELIVEROO`
(voir résumé exécutif).

**Sur `orders.cash_register_id` spécifiquement**, la sentinelle `'KIOSK'` est une **5ᵉ valeur non
numérique possible**, en plus des 4 listées au rapport 16 (`SCANNORDER`, id numérique,
`UBER_EATS`/`DELIVEROO` hérités) — le rapport 16 la mentionne bien en tableau (§6) mais ne
l'intègre pas dans son décompte initial des sentinelles « confirmées, il y en a une troisième » (il
en identifie 3 : SCANNORDER, KIOSK, et implicitement UBER_EATS/DELIVEROO comme un groupe). Ici on
compte séparément **4 sentinelles textuelles** (`SCANNORDER`, `KIOSK`, `UBER_EATS`, `DELIVEROO`) +
NULL + id numérique = 6 états possibles au total sur `orders.cash_register_id`, et les mêmes 6 sur
`payments.cash_register_id` (mais `'KIOSK'` littéral n'y apparaît jamais en pratique, remplacé par
NULL pour ce canal).

---

## 4. Mécanisme de requalification à la clôture de caisse

### 4.1 Ce qui est requalifié

Dans `CloseCashRegister` ([cash_registers/repository.go:260-400](../../internal/modules/cash_registers/repository.go#L260)),
**seule `payments.cash_register_id` est réécrite** — deux `UPDATE` distincts, dans cet ordre, après
la vérification qu'aucun order du merchant n'est encore ouvert (étape 1, §4.2) :

```sql
-- Étape 2 : paiements Stripe ScanNOrder
UPDATE payments p
INNER JOIN orders o ON o.order_id = p.order_id
SET p.cash_register_id = ?                    -- id numérique de la caisse qui clôture
WHERE o.state = 'CLOSED'
  AND p.mop = 'STRIPE'
  AND p.cash_register_id = 'SCANNORDER'
  AND p.merchant_id = ?

-- Étape 3 : paiements plateformes de livraison
UPDATE payments p
INNER JOIN orders o ON o.order_id = p.order_id
SET p.cash_register_id = ?
WHERE o.state = 'CLOSED'
  AND p.mop IN ('UBER_EATS','DELIVEROO')
  AND (p.cash_register_id IS NULL OR p.cash_register_id IN ('UBER_EATS','DELIVEROO'))
  AND p.merchant_id = ?
```

**`orders.cash_register_id` n'est jamais mis à jour, ni ici ni ailleurs dans le repo** (grep exhaustif
de `UPDATE orders` avec `cash_register_id` dans la liste `SET` : aucune occurrence). Une commande
ScanNOrder/Kiosk/UberEats/Deliveroo garde donc sa sentinelle dans `orders.cash_register_id` pour
toute sa durée de vie — ce n'est **pas** un état transitoire pour cette colonne, seulement pour
`payments.cash_register_id`.

Portée : le filtre est `p.merchant_id = ?`, **pas** un lien vers la caisse physique qui ferme. Donc
n'importe quelle caisse d'un merchant, en se fermant, rattache à elle-même **tous** les paiements
en attente (`'SCANNORDER'`/NULL/`'UBER_EATS'`/`'DELIVEROO'`) dont l'order associé est `state='CLOSED'`
— pas seulement ceux passés sur le device qui ferme. Le choix de la caisse qui « récupère » ces
paiements en ligne est donc arbitraire (le premier device à fermer après le passage en `CLOSED` de
l'order), pas fonctionnel.

### 4.2 Le garde-fou de l'étape 1 et une incohérence d'états relevée

```sql
SELECT o.order_id FROM orders o
WHERE o.cash_register_id = ?                  -- id numérique de la caisse à fermer
AND o.state NOT IN ('CLOSED','DONE')
AND (o.scheduled = false OR (o.scheduled = true AND UTC_TIMESTAMP > o.estimated_ready))
LIMIT 1
```

Ce garde tolère `state IN ('CLOSED','DONE')` comme états « terminaux » avant de permettre la
fermeture — mais l'`UPDATE` de requalification (étape 2/§4.1) ne filtre que sur `state = 'CLOSED'`,
**pas** `'DONE'`. Recherche exhaustive : **aucun site Go n'assigne jamais `orders.state = 'DONE'`**
(le littéral `'DONE'` n'apparaît que sur la colonne `brand_status`, par exemple
[order_life_cycle/repository.go:398](../../internal/modules/order_life_cycle/repository.go#L398) ;
la constante `state` pour Uber Eats est `StateOpen`/`StateClosed` uniquement, [ubereats/models.go:91-92](../../internal/modules/ubereats/models.go#L91)).
`'DONE'` comme valeur de `orders.state` n'est donc atteignable aujourd'hui que par des **lignes
héritées d'avant la migration PHP→Go** (si `state` y portait cette valeur) — pas par un flux Go
vivant. **Ce n'est donc pas un angle mort actif**, mais un résidu à vérifier sur les données
historiques (§5) : si des orders legacy ont `state='DONE'` avec un paiement encore en sentinelle,
ce paiement ne serait jamais requalifié par ce garde ni par l'`UPDATE`.

Cette étape 1 ne bloque que sur `o.cash_register_id = ?` (l'id numérique de la caisse à fermer) —
elle ne regarde donc que les orders POS rattachés à *cette* caisse, jamais les orders
ScanNOrder/Kiosk/UberEats/Deliveroo (sentinelle), qui ne peuvent de toute façon jamais matcher cette
condition (§1.2).

### 4.3 Est-ce que toutes les commandes finissent par être requalifiées ?

**Non — réponse factuelle en trois parties :**

1. **`orders.cash_register_id` : jamais requalifié, par construction (§4.1).** Toute commande
   créée avec une sentinelle la garde pour toujours dans cette colonne.
2. **`payments.cash_register_id` : requalifié seulement si le paiement matche une des deux
   branches `UPDATE`.** Concrètement, avec les sites d'écriture recensés en §2.1 :
   - Paiement ScanNOrder Stripe (`mop='STRIPE'`, `cash_register_id='SCANNORDER'`) → **couvert**
     par l'étape 2, dès que l'order passe `state='CLOSED'` et qu'une caisse quelconque du merchant
     ferme.
   - Paiement UberEats/Deliveroo recopié depuis l'order (`mop` variable, `cash_register_id` =
     `'UBER_EATS'`/`'DELIVEROO'`) → **couvert** par l'étape 3, mais **seulement si `p.mop` vaut
     exactement `'UBER_EATS'` ou `'DELIVEROO'`** — si le paiement recopié porte un autre MOP
     (ex. `STRIPE` pour un paiement carte en ligne UberEats/Deliveroo), il ne matche **ni** l'étape
     2 (qui exige `cash_register_id='SCANNORDER'`) **ni** l'étape 3 (qui exige `mop IN
     ('UBER_EATS','DELIVEROO')`) → **reste bloqué sur la sentinelle indéfiniment**. À vérifier avec
     les données réelles (§5) quel MOP est effectivement utilisé pour ces paiements recopiés.
   - **Paiement Kiosk Stripe Terminal (`cash_register_id=NULL`, `mop='STRIPE'`)** → **jamais
     couvert par aucune des deux branches** : l'étape 2 exige `cash_register_id='SCANNORDER'`
     (pas NULL), l'étape 3 exige `mop IN ('UBER_EATS','DELIVEROO')` (pas STRIPE). **Ce paiement
     reste `NULL` en permanence**, quel que soit le nombre de fermetures de caisse ultérieures.
     C'est le cas le plus net d'une commande qui ne sera **jamais** rattachée à une caisse
     numérique.
3. Un paiement `mop='STRIPE'` avec `cash_register_id` NULL provenant d'un autre flux que Kiosk
   (ex. site 7 §2.1, `webhook/stripe/repository.go:97`, marqué `// Decom`) aurait le même sort s'il
   est encore actif — hors périmètre de vérification ici (marqué décommissionné dans le code).

**Conclusion : oui, des paiements peuvent rester indéfiniment non numériques** — pas par accident
temporaire, mais par construction du filtre des deux `UPDATE` de clôture, qui ne couvrent que 2
combinaisons `(mop, valeur sentinelle)` sur l'ensemble des combinaisons réellement produites en
écriture (§2.1).

---

## 5. Vérification des données réelles — non réalisable dans cette session

Aucun accès à une base MySQL de production/staging n'est configuré dans cet environnement (pas de
`.env` ni de chaîne de connexion MySQL trouvée à la racine du repo ; seul un
`docker-compose.postgres.yml` local vide, cible de la migration, est présent — non représentatif
des données de production). Je n'ai donc **pas pu exécuter** de requête de vérification.

Requêtes à faire tourner par quelqu'un ayant un accès lecture à la base de production/staging pour
clore ce point :

```sql
-- Valeurs distinctes non numériques sur orders
SELECT cash_register_id, COUNT(*) FROM orders
WHERE cash_register_id IS NOT NULL AND cash_register_id NOT REGEXP '^[0-9]+$'
GROUP BY cash_register_id;

-- Idem sur payments (inclut NULL)
SELECT cash_register_id, COUNT(*) FROM payments
WHERE cash_register_id IS NULL OR cash_register_id NOT REGEXP '^[0-9]+$'
GROUP BY cash_register_id;

-- Paiements sentinelle jamais requalifiés malgré un order CLOSED (candidats bloqués, §4.3)
SELECT p.mop, p.cash_register_id, COUNT(*)
FROM payments p JOIN orders o ON o.order_id = p.order_id
WHERE o.state = 'CLOSED'
  AND (p.cash_register_id IS NULL OR p.cash_register_id NOT REGEXP '^[0-9]+$')
GROUP BY p.mop, p.cash_register_id;

-- Résidus legacy state='DONE' (§4.2)
SELECT COUNT(*) FROM orders WHERE state = 'DONE';
```

---

## 6. `GET_CASH_REGISTER_REPORT` / `GET_CASH_REGISTER_REPORT_MOP` — angle mort ou hypothèse implicite ?

**Corps des procédures introuvable dans ce repo.** Confirmé par [01-audit.md §1.6](01-audit.md#L82) :
un grep `CREATE TRIGGER|CREATE PROCEDURE|CREATE VIEW|CREATE FUNCTION` sur tout le repo (`.sql` et
`.go`) retourne **0 occurrence** — ces objets sont créés manuellement en base (comme
`user_status_view`, signalé dans le même rapport), jamais versionnés. Je ne peux donc pas confirmer
la logique interne exacte des deux procédures ; l'analyse ci-dessous s'appuie sur (a) leurs deux
seuls sites d'appel Go et (b) une requête Go équivalente et non ambiguë sur le même besoin.

**Ce qui est certain côté appelant :**

- Les deux procédures sont appelées avec un seul paramètre positionnel, `cashRegisterID`
  ([cash_registers/repository.go:113,164,1040,1072](../../internal/modules/cash_registers/repository.go#L113)),
  qui provient systématiquement soit du path param `cash_register_id` de la route HTTP (validé en
  amont par `isCashRegisterClosed(Formerchant)` qui fait `SELECT ... FROM cash_registers WHERE
  cash_register_id = ?` — donc une ligne `cash_registers` doit exister, id **toujours numérique**
  par construction du PK `int(11) AUTO_INCREMENT`), soit du retour de `OpenCashRegister`/
  `CloseCashRegister` (idem, id numérique). **Le paramètre lui-même n'est donc jamais une
  sentinelle.**
- La requête Go équivalente et non ambiguë sur le même besoin, `GetCashRegisterSummary`
  ([:591-599](../../internal/modules/cash_registers/repository.go#L591)), filtre explicitement
  `payments p WHERE p.cash_register_id = ?` (comparaison directe à l'id numérique). Si les
  procédures stockées suivent la même logique interne (probable, vu qu'elles répondent au même
  besoin de rapport de caisse — ventilation TVA et MOP des encaissements de *cette* caisse), elles
  filtrent très probablement aussi `payments`/`orders` sur `cash_register_id = <id numérique>`.

**Conséquence si cette hypothèse est correcte :** oui, c'est un **angle mort actuel**, pas
seulement théorique — pour deux raisons distinctes :

1. **Angle mort structurel permanent** : ces procédures ne peuvent, par construction, jamais
   inclure un paiement encore sur une sentinelle ou NULL (elles comparent à un id numérique) —
   donc tout paiement du §4.3 qui ne sera *jamais* requalifié (Kiosk Terminal NULL, paiement
   UberEats/Deliveroo à MOP non couvert) est **invisible en permanence** dans le rapport de caisse
   Z (`GetCashRegisterReport`) et dans la ventilation par MOP, quelle que soit la caisse ou la date
   d'exécution. Le montant du paiement Kiosk Terminal, par exemple, n'apparaîtra dans **aucun**
   rapport de clôture de caisse.
2. **Angle mort temporel, uniquement pour `GetCashRegisterReport`/`CloseCashRegister`** : dans
   `CloseCashRegister`, la requalification (étapes 2/3, §4.1) est exécutée **avant** l'appel à
   `GetCashRegisterReport` (étape 4, [:319](../../internal/modules/cash_registers/repository.go#L319))
   — donc au moment précis de la clôture, les paiements *qui seront requalifiés avec succès* le
   sont déjà quand le rapport Z est généré. Pas d'angle mort temporel sur ce chemin précis, sous
   réserve que la portée large de l'`UPDATE` (tout le merchant, §4.1) fasse déjà son travail avant.
   En revanche, `GetCashRegisterTVADetails`/`GetCashRegisterSummary` (consultation a posteriori
   d'une caisse déjà fermée) n'exécutent **aucune** requalification avant de lire — s'ils sont
   appelés bien après la clôture, ils dépendent entièrement du fait que la requalification ait
   déjà eu lieu à un moment donné (via une clôture de caisse quelconque du merchant), sans quoi le
   paiement reste invisible pour toujours (cas 1 ci-dessus).

**Recommandation de vérification** (pas une décision) : coller ici le corps SQL réel des deux
procédures dès qu'il est disponible, pour confirmer ou infirmer l'hypothèse « elles filtrent sur
`cash_register_id = <numérique>` » plutôt que, par exemple, sur `orders.cash_register_id` en plus
de `payments` (ce qui élargirait encore l'angle mort, vu que `orders.cash_register_id` n'est
jamais requalifié du tout, §4.1).

---

## 7. Options de modélisation Postgres (factuel, non tranché)

Trois pistes, sans arbitrage — à discuter ensemble avec les résultats du §5 :

**Option A — conserver un varchar avec les sentinelles telles quelles**
Migration quasi gratuite (pas de transformation de données), mais perpétue la colonne hybride en
Postgres : les 3 jointures/égalités hybrides déjà identifiées (rapport 15 : lignes 277, 597, 868)
doivent être traitées par cast explicite (`CAST(cash_register_id AS INTEGER)` protégé par un
`WHERE cash_register_id ~ '^[0-9]+$'`, ou une colonne générée). N'élimine aucun des angles morts
du §4.3/§6 — les documente juste tels qu'ils sont aujourd'hui. Le plus proche du comportement
actuel, donc le risque de régression le plus faible à court terme.

**Option B — séparer en deux colonnes : id numérique nullable + enum/texte de canal**
Ex. `cash_register_id integer NULL REFERENCES cash_registers` + `origin_channel` (`POS`,
`SCANNORDER`, `KIOSK`, `UBER_EATS`, `DELIVEROO`) NOT NULL avec défaut `POS`. Rendrait explicite ce
qui est aujourd'hui déduit implicitement (une commande a soit une caisse, soit un canal, jamais
les deux en même temps dans le modèle actuel) et permettrait un vrai FK sur `cash_register_id`.
Coût : réécrire les 2 `UPDATE` de clôture (deviennent des `UPDATE ... SET cash_register_id = ?`
sans changer `origin_channel`, qui resterait informatif même après rattachement) et tous les sites
de lecture du §1.2/§2.2. Résout le problème de fond du §4.1 (`orders.cash_register_id` jamais
requalifiable) en le rendant simplement non pertinent — l'id de caisse et le canal ne sont plus la
même colonne.

**Option C — garder le varchar mais généraliser la requalification**
Reprendre l'option A pour le typage, mais élargir les 2 `UPDATE` de clôture (§4.1) pour couvrir
toutes les combinaisons `(mop, sentinelle)` réellement produites en écriture (§2.1), et ajouter une
requalification explicite pour `orders.cash_register_id` (aujourd'hui inexistante). Ne change pas
le schéma cible mais corrige le comportement applicatif documenté aux §4.3/§6 avant ou pendant la
migration — à faire de toute façon si l'option A ou B est retenue, indépendamment du choix de
typage.

Aucune des trois options n'a été écartée ni retenue ici — la décision dépend notamment du volume
réel de lignes en sentinelle/NULL non requalifiées trouvé au §5.
