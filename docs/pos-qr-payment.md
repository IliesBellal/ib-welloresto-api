# Paiement par QR code depuis la caisse

Le bouton « QR code » de la calculatrice (tablette et téléphone) ouvre une session
Stripe Checkout pour une part de la commande et affiche son adresse en QR code
plein écran. Le client règle sur son téléphone ; le paiement n'est créé qu'à la
confirmation du webhook.

## Flux

1. `POST /orders/{order_id}/payments/qr` `{ amount, device_id }` (centimes).
   - refuse : commande close (`order_closed`), montant < 0,50 € ou > reste à payer
     (`qr_payment_invalid_amount`), pas de compte Stripe (`qr_payment_unavailable`),
     pas de caisse ouverte sur l'appareil (`no cash register`, comme un paiement).
   - crée la session sur le compte Connect de l'établissement, capture automatique,
     une seule ligne « Commande n°X » au montant demandé, validité 40 min.
   - frais applicatifs = ceux de ScanNOrder (`scannorder_settings`), par paiement.
   - renvoie `{ session_id, url, amount, expires_at }`.
2. Le client paie. `checkout.session.completed` (metadata `channel=pos_qr`) :
   `internal/webhook/stripe/pos_qr.go`.
   - enregistre un paiement `STRIPE` rattaché à la caisse relevée à la création de la
     session et à l'utilisateur qui a affiché le QR (`CreatePayment` : audit, cache,
     notification de commande → la caisse ferme son QR).
   - relit les frais réels du paiement (`fee`, `net_amount`) : une capture automatique
     n'émet pas `charge.captured`. Au mieux : un échec n'annule pas le paiement.
   - commande close/annulée entre-temps, ou déjà soldée par une autre part
     (`OrderNotFullyPaidError`) : le client est remboursé automatiquement.
3. `checkout.session.expired` d'une session `pos_qr` est ignoré : le flux ScanNOrder
   refuserait la commande.

Fermer le QR côté caisse n'annule pas la session : le client peut payer jusqu'à
l'expiration, et le paiement apparaît alors tout seul.

## Disponibilité

`capabilities.integrations.stripe` du login = compte Stripe Connect actif
(`stripe_accounts.account_id`). Lecture au mieux (`attachStripeAccount`) : un échec
masque seulement le bouton. Les sessions déjà en cache (Redis) ne l'ont qu'après
une reconnexion.

## Limites connues

- Pas de lien avec l'écran client (CDS), volontairement.
- Aucun polling de secours : si la notification temps réel est perdue, la page reste
  ouverte jusqu'à fermeture manuelle (le paiement apparaîtra à la prochaine mise à
  jour de la commande).
- Un trop-perçu remboursé automatiquement n'est que journalisé (`[stripe webhook]
  pos_qr payment … refunding`), le caissier n'est pas prévenu.
