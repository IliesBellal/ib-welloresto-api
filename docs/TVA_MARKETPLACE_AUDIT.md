# Audit TVA marketplace (B1) — PROMPT 07 lot 1

> Périmètre : audit seul, aucun correctif. Répond à la question posée par le
> lot 1 de `ROADMAP-analytics.md` : Uber Eats et Deliveroo transmettent-ils
> une ventilation HT/TVA exploitable ?

## Constat

**Non.** Ni le payload webhook Uber Eats ni celui de Deliveroo ne portent le
moindre champ TVA/taxe. `orders.ht` et `orders.tva` sont écrits à `0` pour
100 % des commandes des deux plateformes, à l'écriture — pas par accident,
pas par un bug de calcul, simplement parce qu'aucune donnée fiscale
n'arrive jamais côté webhook pour être écrite ailleurs.

## 1. Ce que les payloads contiennent réellement

### Uber Eats (`internal/webhook/ubereats/models/order.go`)

`UberOrder` / `UberCart` / `UberCartItem` / `UberPayment` — grep exhaustif
(insensible à la casse) de `tax|vat|tva|charge|fee` sur tout le paquet :
**zéro résultat pertinent**. Le seul champ monétaire annexe est
`UberPayment.Charges.SubTotalPromoApplied.Amount` — un montant de sous-total
*après promo*, pas une taxe, utilisé uniquement pour corriger le TTC
(`order_mapper.go`). Le prix d'un article (`Price.UnitPrice.Amount`) est un
entier brut en centimes, sans composante taxe ni taux associé.

### Deliveroo (`internal/webhook/deliveroo_orders/deliveroo_models.go`)

`DeliverooOrder` / `DeliverooItem` / `DeliverooItemFee` / `DeliverooFeeBreakdown`
— même grep, même résultat : **zéro champ tax/vat/tva**. Les seuls champs
« fee »/« charge » présents (`BagFee`, `Surcharge`, `FeeBreakdown`,
`ItemFees`) sont des frais opérationnels de la marketplace (emballage,
majoration, frais par article dont le `Type` est une chaîne libre définie
par Deliveroo) — structurellement sans rapport avec la TVA due par le
restaurant, et le code ne les interprète jamais comme tels.

Aucun des deux payloads ne donne de taux (%), de montant « dont TVA », ni de
distinction par ligne — il n'y a tout simplement rien à en extraire, à
aucune granularité.

## 2. Comment `orders.ht`/`orders.tva` sont écrits aujourd'hui

**Deliveroo** — codé en dur à `0`, avec un commentaire qui l'assume :
```go
// internal/webhook/deliveroo_orders/service.go (buildOrderRequestObject)
HT:  0, // Calculé par le service généralement
TVA: 0, // Calculé par le service généralement
```
**Uber Eats** — jamais renseigné du tout (`order_mapper.go`), reste donc à
la valeur zéro de `int` en Go :
```go
// internal/webhook/ubereats/service/order_mapper.go (MapUberOrderToRequest)
// OrderRequest.HT / .TVA ne sont jamais assignés
```
Les deux `RequestObject` finissent dans le même
`insertOrderBase` (`internal/modules/order_life_cycle/repository.go`),
qui insère `req.Order.TVA`/`req.Order.HT` tels quels — aucun recalcul
n'intervient entre le webhook et l'écriture en base.

Il existe bien un moteur de recalcul HT/TVA ligne par ligne
(`internal/modules/orders/service.go`, `computeTotals`/`ComputePricing`,
qui divise par `1 + taux/100` produit par produit), mais il n'est câblé que
sur les commandes **kiosk** et **ScanNOrder** — jamais appelé depuis
`internal/webhook/ubereats` ni `internal/webhook/deliveroo_orders`. Ce
n'est donc pas ce qui « fait déjà le travail » pour les deux marketplaces.

## 3. Ce qui recalcule réellement HT/TVA pour ces deux marques

Deux endroits recomputent HT/TVA depuis `orderitems × products × tva_categories`
plutôt que depuis `orders.ht`/`orders.tva` — mais avec un périmètre différent :

- **`internal/modules/pos/reports`** (rapport TVA historique du POS) :
  filtre explicitement `AND o.brand = 'WELLO_RESTO'` — **exclut** Uber Eats
  et Deliveroo. Si c'est ce rapport qui sert à la déclaration TVA du
  marchand, les commandes marketplace n'y apparaissent tout simplement pas.
- **`internal/modules/analytics`** (`GetRevenueTotalsHT`/`GetVATTotals`/
  `GetVATByRate`/`GetVATByChannel`) : même formule de recalcul, **sans** le
  filtre `brand`, avec un commentaire qui documente explicitement le
  problème :
  ```go
  // GetRevenueTotalsHT recomputes HT line-by-line from orders×orderitems×
  // products×tva_categories. It CANNOT come from orders.ht/orders.tva: 100%
  // of Uber Eats and Deliveroo orders have ht=0 there.
  ```
  C'est cette voie-là, et uniquement celle-là, qui couvre aujourd'hui les
  deux marketplaces — mais côté analytique, hors périmètre de ce lot.

## 4. Réserve sur la fiabilité de ce recalcul pour ces deux marques

Le recalcul analytique dépend d'une jointure `INNER JOIN tva_categories ON
tva.tva_id = p.tva_in_id/tva_delivery_id/tva_take_away_id` — donc de la
catégorie TVA affectée au `products` interne créé lors du premier mapping
d'un article marketplace inconnu. Les deux intégrations ne se comportent
**pas pareil** à la création automatique d'un produit :

- **Uber Eats** (`internal/modules/menu/repository.go`,
  `CreateExternalProductTx`) affecte en dur `tva_in_id=5, tva_delivery_id=9,
  tva_take_away_id=3` à tout produit auto-créé.
- **Deliveroo** (`internal/webhook/deliveroo_orders/repository.go`,
  `SyncProduct`) n'affecte **aucune** catégorie TVA à l'auto-création — les
  trois colonnes tombent au défaut de la table, `0`.

Si aucune ligne `tva_categories` avec `tva_id = 0` n'existe, l'`INNER JOIN`
exclut silencieusement ces lignes des totaux HT/TVA recalculés (perte
silencieuse, pas une erreur visible) plutôt que de les compter à un taux
faux — sous-estimation potentielle du CA/TVA marketplace côté Deliveroo,
non quantifiée ici (nécessiterait une vérification directe en base de la
proportion de produits Deliveroo auto-créés avec `tva_*_id = 0`, hors
périmètre de cet audit).

## 5. Ce que ça exclut (déjà établi ci-dessus, redit pour mémoire)

- Pas de ventilation HT/TVA par ligne dans les payloads.
- Pas de taux transmis.
- Pas de distinction entre TVA du restaurant et frais propres à la
  marketplace (livraison, emballage, commission) — même si un jour un champ
  taxe apparaissait dans l'un des deux payloads, il faudrait encore établir
  s'il couvre uniquement les produits ou aussi ces frais, avant de l'écrire
  tel quel.

## Verdict

**Aucune donnée à ventiler HT/TVA n'est reçue des deux marketplaces.** Le
recalcul depuis les lignes existe déjà et couvre déjà Uber Eats/Deliveroo,
mais uniquement côté `internal/modules/analytics` (pas `pos/reports`), et sa
fiabilité pour ces deux marques précises dépend d'un mapping de catégorie
TVA qui diffère entre les deux intégrations (Uber Eats en affecte une par
défaut, Deliveroo non). Conformément à la règle d'or de ce lot, **rien n'est
écrit** dans `orders.ht`/`orders.tva` pour ces commandes : une TVA
approximative dans une colonne que d'autres modules pourraient lire
directement serait pire que de la laisser à 0/vide. Le correctif éventuel
(a minima : aligner l'auto-création Deliveroo sur Uber Eats pour au moins
garantir que le recalcul analytique n'échappe pas silencieusement des
lignes) est un chantier séparé, non traité ici.
