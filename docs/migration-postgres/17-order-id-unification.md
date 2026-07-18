# 17 — Préparation unification `order_id` vers `INTEGER` côté Go (exécution)

Exécution du chantier scopé par [16-order-id-format-check.md](16-order-id-format-check.md) : élimine
le seul site Go vivant où un `order_id` transitait par un champ de struct `int` inutilisé.
**Aucun schéma MySQL touché**, aucun fichier `.sql` modifié. Périmètre Go uniquement.

## Rappel du contexte : sens inverse du chantier `merchant_id`

Contrairement à [12-merchant-id-unification.md](12-merchant-id-unification.md) (convergence vers
`string`), ici la cible finale est `INTEGER` côté schéma (`orders.order_id` est l'auto-increment,
12 des 20 colonnes référentes le sont déjà). Le Go étant déjà quasi 100 % `string`, ce chantier ne
retype **rien vers `int`** — au contraire, il retire le seul champ `int` restant, devenu inutile.

## État de référence (baseline) — inchangée depuis le rapport 12

**La suite de tests n'était pas verte avant ce chantier**, et ce n'est pas ce chantier qui doit la
rendre verte. Baseline reprise du rapport 12, revérifiée avant toute modification : **7 packages en
échec, 16 tests `--- FAIL`**, aucun dans les modules touchés ici :

| Package en échec (préexistant) | Cause |
|---|---|
| `internal/modules/auth` | 3 tests : mocks `sqlmock` désynchronisés |
| `internal/modules/bookingcomm` | 1 test : URL `ManagementLink` attendue obsolète |
| `internal/modules/planning/employees`, `planning/leave`, `planning/swaps` | mocks désynchronisés (2+7+3 tests) |
| `internal/modules/pos/accounting` | **build failed** (vet : `%s` sur `*string`) |
| `internal/modules/ubereats` | **build failed** (vet : format non constant dans `fmt.Errorf`, [service.go:100](../../internal/modules/ubereats/service.go#L100)) |

Le critère de succès est donc : **zéro nouvelle régression** — diff exact des échecs avant/après.
Note : `ubereats` est déjà en échec de build pour une raison **totalement indépendante** (un `vet`
sur `service.go:100`, hors du fichier touché ici) — ce chantier ne peut ni aggraver ni corriger ce
point, non traité (hors périmètre).

## 1. Vérification du site à traiter : `UberOrderMetadata.OrderID`

Confirmation par lecture complète des appelants de `GetOrderMetadata` (5 sites dans
[ubereats/service.go](../../internal/modules/ubereats/service.go), lignes 208, 308, 362, 397, 421) :
seuls `orderMeta.BrandOrderID`/`meta.BrandOrderID` sont lus après le `Scan`
([service.go:260](../../internal/modules/ubereats/service.go#L260),
[:312](../../internal/modules/ubereats/service.go#L312),
[:367](../../internal/modules/ubereats/service.go#L367),
[:403](../../internal/modules/ubereats/service.go#L403),
[:405](../../internal/modules/ubereats/service.go#L405),
[:424](../../internal/modules/ubereats/service.go#L424)). `meta.OrderID`/`orderMeta.OrderID`
n'apparaît **nulle part** en dehors de sa déclaration et du `Scan` lui-même. Aucun fichier de test
dans le module ne référence `UberOrderMetadata` ou `GetOrderMetadata` (`ls
internal/modules/ubereats/*_test.go` → aucun fichier).

**Décision : suppression du champ**, pas de retype en `int`. Aucune raison de compatibilité de
colonnes ne le justifiait — la valeur n'était utile à aucun appelant, et retenir `o.order_id` dans le
`SELECT` uniquement pour le jeter était un coût sans contrepartie.

### Changement

| Fichier | Avant | Après |
|---|---|---|
| [models.go](../../internal/modules/ubereats/models.go) | `UberOrderMetadata{ OrderID int \`db:"order_id"\`; BrandOrderID string; CreationDate time.Time }` | `UberOrderMetadata{ BrandOrderID string; CreationDate time.Time }` |
| [repository.go:134-142](../../internal/modules/ubereats/repository.go#L134) | `SELECT o.order_id, o.brand_order_id, o.creation_date … .Scan(&meta.OrderID, &meta.BrandOrderID, &meta.CreationDate)` | `SELECT o.brand_order_id, o.creation_date … .Scan(&meta.BrandOrderID, &meta.CreationDate)` |

La clause `WHERE o.order_id = ?` (paramètre lié, pas colonne projetée) est inchangée — elle reste
insensible au type de la colonne, comme documenté au rapport 16 §3.

## 2. Les 2 sites morts : non touchés

Conformément à la consigne (même politique qu'au rapport 12 §« Les 7 sites morts ») :

- [deliveroo/repository.go:138](../../internal/modules/deliveroo/repository.go#L138) —
  `GetBrandOrderIDAndMerchant(ctx, orderID int)` : zéro appelant dans le repo. C'est le même site que
  le rapport 10 §4 avait déjà repéré pour `merchant_id` (la fonction a **deux** paramètres entiers
  morts, `orderID` et `merchantID` — seul le second avait été documenté côté merchant).
- [notification/notification_models.go:11](../../internal/modules/notification/notification_models.go#L11) —
  `NotificationMessage{ OrderID int }` : struct jamais instanciée ; le handler vivant du module
  utilise une struct JSON distincte, déjà en `string`
  ([notification_handler.go:23](../../internal/modules/notification/notification_handler.go#L23)).

Ni l'un ni l'autre n'est lié à l'unification `order_id` de façon à justifier une exception — ce sont
des suppressions sûres mais sans rapport avec ce chantier ; à traiter dans un nettoyage dédié si
souhaité.

## 3. Grep de confirmation : plus aucun site typé int

```
grep -rnE '\b(OrderID)\s+\*?(int64|int)\b' internal cmd --include=*.go
  → internal/modules/notification/notification_models.go:11   (mort, non touché)

grep -rnE '\borderID\s+\*?(int64|int)\b' internal cmd --include=*.go
  → internal/modules/deliveroo/repository.go:138               (mort, non touché)
```

Exactement les 3 sites annoncés par le rapport 16 : les 2 confirmés morts restent, le 3ᵉ (vivant
mais inutile) est éliminé. **Zéro site Go vivant** où `order_id` transite encore par un type entier.

## Récapitulatif des fichiers modifiés (2)

```
internal/modules/ubereats/models.go
internal/modules/ubereats/repository.go
```

Aucune migration SQL créée ni modifiée. Aucun `.sql` touché.

## Vérifications post-chantier

```
go build ./...                     → OK
go vet ./internal/modules/ubereats/... → échec préexistant (service.go:100, hors périmètre, cf. baseline)
go test ./internal/modules/ubereats/... → build failed, identique à la baseline (pas de régression : déjà cassé avant)
go test ./internal/...             → 7 packages / 16 tests en échec, liste strictement identique à la
                                      baseline (vérifié par diff après `git stash`/`git stash pop`)
grep OrderID/orderID typés int     → seulement les 2 sites morts documentés au §2
```

Le repo ne contient plus aucun site Go **vivant** où `order_id` transite par un type entier. Le futur
chantier schéma (rapport 16 §2 : convergence `varchar → integer` des 6 colonnes vivantes
`orderitems.order_id`, `orders.parent_order_id`, `customer_loyalty_progress_order.order_id`,
`customer_rewards.used_on_order_id`, `stock_movements.order_id`, `upsell_suggestions.order_id`) n'a
plus aucun prérequis côté Go.
