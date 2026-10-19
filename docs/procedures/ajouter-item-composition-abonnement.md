# Ajouter un item à la composition de l'abonnement (avant le premier RIB SEPA)

Cas typique : poser à l'avance ce que l'établissement doit payer (plan, module) avant qu'il ait
renseigné son RIB pour le mandat SEPA. Sans risque de prélèvement immédiat : la ligne posée ici est
purement déclarative — `subscriptions.current_period_end` (début réel de la facturation) reste
`NULL` jusqu'au mandat, aucun cycle de facturation ne démarre tant qu'il n'existe pas.

## Codes disponibles

Depuis 2026-10-18, plus de liste fermée en dur côté Go : tout code présent dans `pricing_catalog`
sous `kind='plan'` ou `kind='module'` est automatiquement un code `subscription_items` facturable —
y compris une variante négociée que vous venez d'ajouter en grille (ex. `essentiel-d01`), sans
toucher au code (`resolveUnitPriceCents`/`ResolveStripeLineItems`/`itemKindForCode`,
[internal/modules/subscriptions/amount.go](internal/modules/subscriptions/amount.go) résolvent
désormais génériquement via `catalog.Plans[code]`/`catalog.Modules[code]`).

| `code` | `kind` | Remarque |
|---|---|---|
| N'importe quel `code` de `pricing_catalog` avec `kind='plan'` | `plan` | Un seul plan actif à la fois (pas de contrainte en base, à respecter à la main) |
| N'importe quel `code` de `pricing_catalog` avec `kind='module'` | `module` | `reservation`, `haccp`, `planning`, `marketplaces`, `delivery` aujourd'hui, plus toute variante ajoutée |
| `planning_employee`, `extra_pos` | `metered` | Seuls codes encore en dur — ils ne se résolvent pas sous leur propre nom en grille (facturés via le module `planning`/l'addon `extra_seat`). Quantité toujours recalculée à l'appel, la quantité posée ici est indifférente |
| `kiosk`, `sms` | — | Codes reconnus mais **sans prix exploitable** (ni `pricing_catalog`, ni variante) — voir l'étape 0 |

## Étape 0 — vérifier que le code a un prix exploitable (obligatoire, API comme SQL)

`GET /v1/subscriptions/current` (et `/preview`) recalculent le montant en relisant `pricing_catalog`
à chaque appel — jamais depuis une valeur figée. Si un item **déjà actif** porte un code sans prix
exploitable, ces deux routes échouent en boucle avec :
```
409 {"status":"pricing_unavailable_for_code","error":"This subscription item code has no usable price in the pricing catalog yet."}
```
**Ce n'est pas un item Stripe manquant** — `stripe_price_id` n'intervient pas dans ce calcul
(`ComputeSubscriptionAmount` ne fait aucun appel Stripe). C'est `pricing_catalog` qui n'a pas de prix
en centimes exploitable pour ce code précis. Les deux problèmes sont indépendants : un code peut
avoir l'un sans l'autre (voir `models.ErrStripeCatalogPriceMissing`, distinct de l'erreur ci-dessus,
pour le cas où c'est vraiment `stripe_price_id` qui manque — ça ne se déclenche qu'au moment de la
facturation réelle, pas ici — voir la section « Tarif négocié » plus bas).

Vérifier **avant** d'ajouter, quel que soit le chemin choisi (API ou SQL) :
```sql
-- module
SELECT monthly_price_cents FROM pricing_catalog WHERE kind = 'module' AND code = '<code>';
-- plan
SELECT monthly_price_cents FROM pricing_catalog WHERE kind = 'plan' AND code = '<code>';
-- planning_employee (prix au salarié porté par le module 'planning')
SELECT per_unit_price_cents FROM pricing_catalog WHERE kind = 'module' AND code = 'planning';
-- extra_pos (nom de code différent côté grille : 'extra_seat')
SELECT monthly_price_cents FROM pricing_catalog WHERE kind = 'addon' AND code = 'extra_seat';
```
Une ligne vide ou `NULL` sur la colonne attendue = code à ne **pas** ajouter en l'état. Aujourd'hui,
c'est le cas de `kiosk` (la grille ne porte que 3 lignes `addon` tarifées par palier —
`kiosk_first_tier1`/`kiosk_additional_tier1`/`kiosk_tier2` — jamais mappées vers un code
`subscription_items.kiosk` unique) et de `sms` (aucune ligne du tout). Rien en base n'empêche
d'insérer quand même la ligne `subscription_items` par SQL direct — l'insert réussit, et c'est
seulement l'appel suivant à `/current` ou `/preview` qui casse. L'API (`POST /items`,
`AddItem`/`ApplyItemChanges`) fait cette même vérification avant d'écrire et refuse ces deux codes
directement.

## Procédure — via l'API (recommandé)

Même route que celle utilisée par le client lui-même depuis son réglage d'abonnement — rien de
spécifique à l'avant-RIB, un item posé avant ou après le mandat suit exactement le même chemin.

```
POST /v1/subscriptions/items
Authorization: <token d'un utilisateur de l'établissement, permission settings.manage>
```
```json
{ "add": ["haccp"] }
```
Renvoie le nouveau montant total recalculé (`Amount`). Pour voir l'effet sans l'appliquer :
`GET /v1/subscriptions/preview?add=haccp`.

> Si le compte client n'a pas encore d'utilisateur/session exploitable à ce stade de l'onboarding,
> passer par le SQL direct ci-dessous.

## Procédure — en SQL direct

1. **Vérifier qu'aucune ligne active n'existe déjà pour ce code** (un item déjà actif pour ce
   `(merchant_id, code)` ferait doublon — bloqué par un index unique partiel) :
   ```sql
   SELECT id FROM subscription_items WHERE merchant_id = '<merchant_id>' AND code = 'haccp' AND removed_at IS NULL;
   ```

2. **Lire le tarif courant en grille** (jamais une valeur inventée à la main) — requête de l'étape 0
   pour le `code` concerné. C'est aussi cette lecture qui sert de garde-fou : si elle ne renvoie
   rien, on s'arrête là, on n'insère pas.
   ```sql
   SELECT monthly_price_cents FROM pricing_catalog WHERE kind = 'module' AND code = 'haccp';
   ```

3. **Insérer la ligne** :
   ```sql
   INSERT INTO subscription_items (id, merchant_id, code, kind, quantity, unit_price_cents)
   VALUES ('sbit-' || gen_random_uuid(), '<merchant_id>', 'haccp', 'module', 1, <monthly_price_cents lu à l'étape 2>);
   ```

4. **Vérifier** :
   ```sql
   SELECT code, kind, quantity, unit_price_cents, created_at
   FROM subscription_items WHERE merchant_id = '<merchant_id>' AND removed_at IS NULL;
   ```
   Ou via l'API, une fois le compte client exploitable : `GET /v1/subscriptions/current`.

## Retirer un item posé par erreur

Jamais de suppression physique — toujours un retrait logique :
```sql
UPDATE subscription_items SET removed_at = now(), updated_at = now()
WHERE merchant_id = '<merchant_id>' AND code = 'haccp' AND removed_at IS NULL;
```

## Tarif négocié (un vrai prix Stripe différent, pas juste un affichage)

Cas : « c'est l'offre Essentiel, mais avec une réduction négociée » — donc un vrai prix Stripe à
part, pas juste un montant affiché différent.

**La dérogation commerciale (`subscription_overrides`, kind='price', voir
[accorder-periode-essai.md](accorder-periode-essai.md)) ne convient pas ici** : elle ne change que
ce qu'affichent `/current` et `/preview` (`subscriptions.override_price_cents`). Elle n'a **aucun
effet sur le prélèvement SEPA réel** — `billing.Service.CreateOrUpdateStripeSubscription`
construit l'abonnement Stripe uniquement depuis `ResolveStripeLineItems`, qui ne lit jamais
`override_price_cents`. Un override sur un marchand avec `code='essentiel'` standard continuerait
à le facturer au tarif plein via Stripe, quoi que dise l'override.

La bonne façon, maintenant que les codes sont pilotés par la grille :

1. **Ajouter la variante en grille** avec son propre prix (fait pour `essentiel-d01`, 5000 cts/mois) :
   ```sql
   INSERT INTO pricing_catalog (id, kind, code, package_name, label, monthly_price_cents, annual_price_cents, included_modules_count)
   VALUES ('price-plan-<slug>', 'plan', '<code-variante>', 'Essentiel', '<libellé>', <cents>, <cents annuel ou NULL>, 0);
   ```
2. **Poser l'item** sur le marchand avec ce code (procédure ci-dessus, étape 0 incluse) — devient
   immédiatement facturable, aucune modification de code nécessaire.
3. **Provisionner le vrai Price Stripe** pour cette ligne — `cmd/ensure_stripe_prices` parcourt déjà
   `pricing_catalog` génériquement (pas de liste figée côté Go non plus) et crée un Price manquant
   pour toute ligne `plan`/`module` sans `stripe_price_id` :
   ```powershell
   DB_DIALECT=postgres POSTGRES_URL=... STRIPE_API_KEY=sk_... GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=x PIN_PEPPER=x `
     go run ./cmd/ensure_stripe_prices
   ```
   Idempotent — ne retouche pas les lignes qui ont déjà un `stripe_price_id`.
4. **Vérifier** :
   ```sql
   SELECT code, monthly_price_cents, stripe_price_id FROM pricing_catalog WHERE code = '<code-variante>';
   ```
   `stripe_price_id` doit être rempli. À partir de là, le jour où ce marchand pose un mandat SEPA,
   `CreateOrUpdateStripeSubscription` facturera réellement ce Price négocié — plus seulement un
   affichage.

## Limites à connaître

- `unit_price_cents` est figé au moment de l'ajout (traçabilité/audit). Le montant réellement
  affiché/facturé, lui, relit toujours la grille tarifaire en direct (`ComputeSubscriptionAmount`) —
  un écart entre la valeur stockée ici et un changement de grille ultérieur n'est pas un bug, c'est
  voulu.
- Rien en base n'empêche de poser deux plans actifs en même temps (`essentiel` + `pro`, par
  exemple) — c'est à l'API/au back-office de l'empêcher côté règles métier ; en SQL direct, à
  vérifier soi-même avant d'insérer.
- `kiosk` et `sms` n'ont encore aucun prix dans `pricing_catalog` : ne pas les ajouter manuellement
  tant que ce n'est pas réglé (l'API les refuse déjà explicitement).
