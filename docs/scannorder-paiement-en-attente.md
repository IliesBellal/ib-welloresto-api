# ScanNOrder — retour de Stripe sans paiement

Décidé et codé le 2026-10-22. Front : `wello-resto-scannorder` (branche `staging`).

## Problème

La commande est créée **avant** le paiement (`ONLINE_PAYMENT_PENDING`). Le client qui
quitte Stripe sans payer (flèche retour de Stripe) arrivait sur la page de suivi sans
aucune action possible, et la session n'expire que 30 min plus tard
(`checkout.session.expired` → `HandleCheckoutSessionCanceled` → commande `DENIED`,
motif 43).

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| D1 | Le bouton « Payer » rouvre la **même** session Stripe (URL gardée dans le `localStorage`, `sno_active_order`), pas une nouvelle | Une 2ᵉ session ouverte sur la même commande ouvre un risque de double paiement ; l'URL reste valable jusqu'à l'expiration (30 min) |
| D2 | Pas d'annulation à la volée au retour | `CancelOrderSNO` est limité à 60 s et n'expire pas la session ; la commande `ONLINE_PAYMENT_PENDING` est exclue des listes caisse (`orders/repository.go`), donc invisible du restaurant pendant l'attente |
| D3 | `success_url` = `…/order/{id}?payment=success`, `cancel_url` = `…?payment=canceled` (`infrastructure/stripe/checkout.go`) | Le webhook peut arriver après le retour du client : sans ce paramètre, un client qui vient de payer verrait un bouton « Payer » |
| D4 | À l'expiration (30 min) on garde l'auto-refus existant ; la page affiche « Paiement non abouti » (motif 43, lu dans `cancellation.reason_id`) | Pas d'endpoint d'expiration à écrire |
| D5 | Écran « Redirection vers le paiement… » ramené de 1,5 s à 0,5 s, et `pageshow` (bfcache) renvoie sur le suivi | Le retour arrière du navigateur restaurait la page figée sur cet écran |

## Comportement

- **Suivi** (`OrderTrackingPage`) : commande en attente de paiement + lien mémorisé +
  pas de `?payment=success` → bouton « Payer {montant} » ; avec `?payment=success` →
  « Paiement reçu, confirmation en cours… ».
- **Bandeau du menu** (`ActiveOrderBanner`) : « Paiement en attente · Payer » (ouvre
  Stripe) tant que la commande attend le paiement ; « Commande en cours · Suivre »
  sinon ; disparaît quand la commande est terminale (il interroge la commande, comme le
  suivi).
- Autre appareil / `localStorage` vidé / session expirée : pas de bouton, comportement
  d'avant (statut « En attente de paiement »).

## Limites connues

- Le client qui abandonne ne peut pas annuler lui-même : la commande se refuse seule
  à l'expiration.
- Le front ne reconnaît pas encore `?payment=canceled` (aucun traitement particulier :
  le bouton est affiché comme pour tout retour non payé).
- À tester sur mobile réel : retour arrière iOS Safari / Android Chrome (bfcache).
