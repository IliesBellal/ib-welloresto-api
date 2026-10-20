# Scan&Order — minimum de commande en livraison

Correctif du 2026-10-20.

## Incident

Commande 40494 (marchand 226, `tastyfoodyutz`), livraison à 12,00 € alors que
`minimum_cart_for_delivery_order` = 1500 (15 €). Elle a été créée et la session
Stripe ouverte.

## Causes

1. **API : minimum calculé mais jamais appliqué.** `orders.applyDeliveryRules`
   passe `PricingRequest.IsOrderable = false`
   (`NotOrderableReason = "minimum_cart_for_delivery_not_reached"`) tout en
   répondant `Status: "success"`. `CreateOrderSNO` ne testait que `Status` et
   `UnavailableProduct` : il ne lisait jamais ce drapeau.
2. **Front : mauvais nom de champ.** La fiche marchand (`GET /scannorder/{slug}`)
   expose `minimum_order_amount`, mais le front lisait `merchant.min_order_amount`,
   qui n'existe pas. Le minimum valait toujours 0 : ni message au panier, ni bouton
   bloqué, ni affichage « Minimum : » dans la popup d'accueil.
3. **API : mauvais seuil dans `/delivery/check`.** `min_order_amount` renvoyait
   `delivery_fees_limit` (seuil de livraison **offerte**) au lieu de
   `minimum_cart_for_delivery_order`. Aucun client ne le lisait, mais la valeur
   était fausse.

## Règle (inchangée)

Commande Scan&Order `DELIVERY` refusée si le **TTC produits après remises et
récompenses, hors frais de livraison**, est inférieur à
`merchant_parameters.minimum_cart_for_delivery_order`. Le minimum exact est
accepté. La règle ne s'applique ni au POS, ni au kiosk, ni à l'emporter ou au
sur place.

## Correctifs

### API (`ib-welloresto-api`)

- `internal/modules/scannorder/minimum_cart_guard.go` : `minimumCartNotReached`
  lit `OrderRequest.IsOrderable`, pas `PricingResponse.IsOrderable` (ce dernier
  intègre aussi la zone de livraison, déjà contrôlée avant via `address_too_far`).
  Le contrôle est placé **après** celui des produits indisponibles, parce que sur
  ce chemin le pricing s'arrête avant `applyDeliveryRules` et que `IsOrderable`
  y reste à `false`.
- `CreateOrderSNO` renvoie un HTTP 200 avec
  `status: "minimum_cart_for_delivery_not_reached"`, avant toute création en
  base ou session Stripe. Le refus est journalisé en `Warn` (TTC et minimum).
  Le statut est une constante, jamais le `not_orderable_reason` du corps de
  requête, qui est lié au JSON client.
- `CheckDeliveryZone` renvoie `minimum_cart_for_delivery_order`.
- `GetPricingSNO` teste l'erreur et `pricing == nil` avant de lire `pricing`.
  Avant, `ComputePricing` renvoyant `(nil, err)` ou `(nil, nil)` faisait
  paniquer le service.
- Tests : `scannorder/minimum_cart_guard_test.go` et
  `orders/delivery_rules_test.go` (paramètres réels du marchand 226).

### Front (`wello-resto-scannorder`)

- `Merchant.minimum_order_amount` remplace `min_order_amount` (`types.ts`,
  `CartDrawer`, `CartSidePanel`, `WelcomePopup`). Le contrôle du panier
  (message « il manque X € », bouton désactivé) fonctionne de nouveau.
- `CheckoutFlow` :
  - contrôle avant création, sur la base du pricing serveur
    (`ttc - delivery_fees < minimum_cart_for_delivery_order`). Le panier compare
    le prix avant remise ; une remise peut faire repasser la commande sous le
    minimum ;
  - gestion du statut `minimum_cart_for_delivery_not_reached` à la création
    (toast, panier conservé), comme `delivery_closed` et les autres statuts.

## Compatibilité

- Ancien front + nouvelle API : une commande sous le minimum reçoit un statut
  inconnu de l'ancien front. Celui-ci affiche le message générique
  « Erreur API : minimum_cart_for_delivery_not_reached », et rien n'est créé
  ni débité.
- Nouveau front + ancienne API : le panier bloque de nouveau sous le minimum
  avant remise.
- `/delivery/check` : seul `min_order_amount` change de valeur ; le front ne le
  lit pas.
