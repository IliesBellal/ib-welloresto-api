# 20 — Correctif clôture de caisse : paiements carte Kiosk

Date : 2026-07-18
Portée : **comportement applicatif MySQL uniquement** — aucun fichier SQL de
migration ni schéma modifié. À committer séparément du reste de la migration
Postgres.

## Contexte

Les encaissements carte sur borne Kiosk (Stripe Terminal, `card_present`) sont
insérés dans `payments` par le webhook Stripe (`recordTerminalPayment`,
[internal/webhook/stripe/service.go](../../internal/webhook/stripe/service.go))
avec `cash_register_id = NULL` (une borne n'a pas de caisse). Ils n'étaient
rattachés à **aucune** clôture de caisse : trou dans le rapport Z.

Objectif fonctionnel confirmé : peu importe le canal (POS différé, ScanNOrder,
Uber Eats, Deliveroo, Kiosk), un paiement sans registre de caisse associé doit
être rattaché au premier registre de caisse qui se ferme ensuite pour ce
merchant.

## 1. Vérification du MOP Kiosk

**Valeur trouvée avant correction : `mop = 'STRIPE'`** (`models.StripeMOP`),
et non `'CB'`. Incohérent avec les autres paiements carte du système (le POS
enregistre la carte en `'CB'`).

### Correction

- Nouvelle constante `CardMOP = "CB"` dans
  [internal/models/users_models.go](../../internal/models/users_models.go)
  (aux côtés de `StripeMOP`, `TicketRestoMOP`).
- `recordTerminalPayment` insère désormais `MOP: models.CardMOP` (`'CB'`).

### Effet de bord traité — ligne `stripe_payments`

`AddPaymentAndReturnID`
([internal/modules/order_life_cycle/repository.go](../../internal/modules/order_life_cycle/repository.go))
n'insérait la ligne `stripe_payments` (porteuse du `payment_intent_id`) **que
si `MOP == 'STRIPE'`**. Cette ligne est indispensable au webhook
`charge.captured` (`UpdateFees` : écriture de `fee` / `net_amount`) et au
refund (désactivation du paiement par `payment_intent_id`).

La condition a donc été élargie :

```go
} else if payment.MOP == models.StripeMOP || (payment.PaymentIntentID != nil && *payment.PaymentIntentID != "") {
```

Les paiements `'CB'` POS classiques ne fournissent jamais de
`PaymentIntentID` : leur comportement est inchangé.

## 2. Rattrapage à la clôture de caisse

Ajout d'un 3ème UPDATE de requalification dans `CloseCashRegister`
([internal/modules/cash_registers/repository.go](../../internal/modules/cash_registers/repository.go)),
étape « 3bis », placé après l'étape 3 (UBER_EATS/DELIVEROO) et **avant**
l'appel à `GetCashRegisterReport` (étape 4), pour que les paiements rattrapés
soient couverts par le rapport Z de cette clôture :

```sql
UPDATE payments p
INNER JOIN orders o ON o.order_id = p.order_id
SET p.cash_register_id = ?
WHERE o.state = 'CLOSED'
  AND p.mop = 'CB'
  AND (p.cash_register_id IS NULL OR p.cash_register_id = 'KIOSK')
  AND p.merchant_id = ?
```

Même modèle que les étapes 2 (`STRIPE`/`SCANNORDER`) et 3
(`UBER_EATS`/`DELIVEROO`) déjà en place. La clause `= 'KIOSK'` est défensive
(les insertions actuelles laissent `NULL`).

## 3. Nettoyage du commentaire obsolète

Le commentaire `// Remplacer 'CASH' par l'ID exact que tu utilises pour
l'espèce (ex: 'ESPECES')` avant `if mopLine.MOP == "ES"` **n'existe plus dans
le code** (déjà supprimé sur cette branche) — rien à faire, vérifié par grep
sur tout le module `cash_registers`.

## 4. Tests

- `go build ./...` : OK, aucune erreur.
- `internal/modules/cash_registers` : aucun fichier de test existant.
- `go test -count=1 ./internal/modules/order_life_cycle/...` : **ok** (0.143s).
- `internal/webhook/stripe` : aucun fichier de test existant.

## Note migration Postgres

Le nouvel UPDATE utilise la syntaxe MySQL `UPDATE ... INNER JOIN`. Lors de la
conversion Postgres de `cash_registers/repository.go`, il devra être converti
en `UPDATE ... FROM` comme les étapes 2 et 3 (voir
[08-conversion-pattern-reference.md](08-conversion-pattern-reference.md)).
