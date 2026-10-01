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
- Pas testé contre Stripe (ni staging ni prod).
