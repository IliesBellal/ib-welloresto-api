# Description des produits dans la session Stripe Checkout (Scan&Order)

Date : 2026-10-01 — fichier : `internal/infrastructure/stripe/checkout.go` (`CreateCheckoutSession`).

## Contexte

Après une commande Scan&Order à payer en ligne, `scannorder.Service` appelle
`StripeManager.CreateCheckoutSession`, qui crée une ligne Stripe par produit. La
description de chaque ligne (affichée sous le nom du produit sur la page de paiement
Stripe) était construite par concaténation manuelle et donnait par exemple :

```
Burger MaxiFrites, Coca(+1.50 EUR), 
```

Défauts : nom du produit répété (il est déjà dans `Name`), collé à la première option
sans séparateur, virgule finale, prix au format anglais, pas d'espace avant la parenthèse.

## Implémentation

La boucle a été découpée en trois fonctions pures :

- `selectedOptions(config)` : options cochées (`Selected`) de tous les attributs ;
- `optionsDescription(options)` : libellés joints par `", "`, suffixés de
  `(+x,xx €)` quand `ExtraPrice > 0` ; renvoie `nil` s'il n'y a rien à afficher ;
- `formatEuros(cents)` : `150` → `"1,50 €"`.

Résultat : `Frites, Coca (+1,50 €)`.

Le montant (`UnitAmount = prix unitaire + somme des suppléments > 0 des options cochées`)
est **inchangé**.

## Décisions

1. **Nom du produit retiré de la description** : Stripe l'affiche déjà en titre de ligne.
2. **`nil` plutôt que chaîne vide** quand aucune option n'a de libellé : Stripe rejette les
   paramètres chaîne vide (`product_data.description`). Avant, la description contenait
   toujours au moins le nom du produit, donc le cas ne se posait pas.
3. **Option sans libellé ignorée** dans la description (avant : seul son supplément
   apparaissait, sans nom). Son supplément reste compté dans le montant.
4. **Libellés `TrimSpace`-és** pour éviter les doubles espaces.
5. **Quantité d'option non affichée** : le montant ne la prend pas en compte (voir
   ci-dessous) ; afficher « 2× Sauce » en ne facturant qu'une sauce aurait rendu l'écart
   visible sans le corriger. À revoir avec le point suivant.
6. `CreateCheckoutSessionOld` (non appelée) n'a pas été touchée.

## Correctif 2026-10-02 : libellés des options jamais renseignés

Constat en test (commande « Tacos 3 viandes ») : la ligne Stripe n'affichait que le nom du
produit, aucune option. Cause : le front Scan&Order n'envoie pour chaque option que
`id` / `quantity` / `selected` ; le backend réécrit le prix des options depuis la base
(`validateAndCleanPricingPayload`) mais ne renseignait jamais `Label`. Le nom du produit,
lui, vient de la base (`orders.buildSelectedProducts`). Le défaut existait avant le
nettoyage : l'ancienne description valait `Tacos 3 viandes 🍖🍖🍖, , , , , `.

Correctif (`internal/modules/scannorder`) :

- `GetConfigurationOptionPricesForSNO` → `GetConfigurationOptionsForSNO`, qui lit aussi
  `COALESCE(title, '')` et renvoie `map[id]SNOConfigurationOption{ExtraPrice, Title}` ;
- `validateAndCleanPricingPayload` renseigne `Label` depuis `title` (si non vide) au même
  endroit que le prix. La configuration est partagée par pointeur jusqu'à
  `CreateCheckoutSession`, donc le libellé y arrive sans autre modification.

Décisions :

7. **Libellé pris en base, pas dans le payload** : cohérent avec le prix (le client n'est
   pas source de vérité) et ne nécessite aucun changement du front.
8. **Correctif limité à Scan&Order** plutôt qu'au pricing commun
   (`orders.applyConfigurationOptionPrices`) : seul ce flux crée une session Checkout, et
   on évite de changer le pricing des autres canaux.
9. **Pas d'effet sur le regroupement des produits** (`generateProductKey` sérialise la
   configuration) : une même option a toujours le même libellé, la clé reste discriminante
   de la même façon. `Label` n'est lu nulle part ailleurs dans `orders`/`order_life_cycle`.
10. Titre de base, non traduit (pas de locale côté session Stripe aujourd'hui).

Résultat attendu pour le tacos : `Viande hachée, Kebab, Nuggets, Algérienne, Mayonnaise`
(ordre des attributs du payload).

## Point ouvert (non traité)

Le montant Stripe ignore `option.Quantity` et `product.Extra`, alors que le TTC de la
commande (`orders.Service`, calcul `opt.ExtraPrice * optionQty * qty` + extras) les
inclut. Si Scan&Order envoie une option en quantité > 1 ou des extras, le montant
autorisé par Stripe est inférieur au TTC de la commande. Non corrigé ici (changement
de montant facturé, hors périmètre d'un nettoyage de libellé).

## Vérification

- `go build ./...` OK.
- `go test ./internal/infrastructure/stripe/ -run 'TestOptionsDescription|TestFormatEuros'`
  OK (`checkout_test.go`).
- 2026-10-02 : `go build ./...`, `go vet` (y compris `-tags postgres_integration`) et
  `go test ./internal/modules/scannorder/ ./internal/modules/orders/ ./internal/infrastructure/stripe/` OK.
- Test d'intégration Postgres mis à jour (vérifie aussi `Title == "Mayo"`) mais **non
  exécuté** : Postgres Docker local éteint, et le test insère des données (pas lancé sur staging).
- Requête vérifiée en lecture seule sur la base staging avec les options du tacos
  (1856, 1859, 1860, 1785, 1774) → titres corrects, suppléments à 0.
- Pas testé contre Stripe (ni staging ni prod).
