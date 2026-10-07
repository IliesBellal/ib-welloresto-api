# PROMPT — Conformité caisse, lot C : réouverture d'une commande close et annulations de paiement

**Date :** 2026-10-07
**Références :**
- [feuille de route](attestation-conformite-feuille-de-route.md), décisions S2, S6 et S7 ;
- [audit](attestation-conformite-00-audit.md) : constats C1 et C2, arbitrages d'Ilies des 2026-10-06 et 2026-10-07 ;
- [lot A](attestation-conformite-01-lot-A-brief.md) : socle `internal/fiscal`, journal d'audit chaîné et signé ;
- [lot B](attestation-conformite-02-lot-B-brief.md) : clôture journalière, qui définit une commande « scellée », et avoirs avec TVA ventilée (C9) ;
- BOI-TVA-DECLA-30-10-30 §90 : une correction se fait par des opérations de « plus » et de « moins », elle-même enregistrée et inaltérable.

**Prérequis :** lot B terminé.

**Objectif :** une commande close et un paiement enregistré ne sont plus jamais modifiés sans trace scellée. La caisse garde ses usages : réouvrir pour corriger, annuler un paiement saisi par erreur.

## Règle d'or

Elle est reprise des lots A et B :
- aucune régression de performance (mesure avant / après) ;
- aucune donnée existante modifiée ;
- aucun changement fonctionnel visible en dehors des règles ci-dessous ;
- migrations écrites, jamais appliquées ;
- une phase, un point d'arrêt ; phase 0 rendue avant tout code ;
- commits atomiques, hunks du lot uniquement, sur accord d'Ilies.

## Règles décidées par Ilies

**C1 — Réouverture d'une commande close**
- **R1. Réouverture refusée (code d'erreur dédié) dans deux cas :**
  - la commande est **scellée**, c'est-à-dire que la clôture journalière de sa date de clôture est passée (lot B) ;
  - **l'un de ses paiements** est rattaché à un registre fermé. Une commande peut être encaissée sur plusieurs registres, par exemple un standardiste et un livreur avec sa banane.
  
  Aujourd'hui, `ReopenClosedOrder` ne vérifie rien (`// FUTURE VALIDATIONS HERE`), et la caisse propose la réouverture depuis l'historique des registres ([reopen_order_button.dart](../../wello_resto_flutter/lib/ui/widgets/cash_register/reopen_order_button.dart)).
- **R1 bis. Plus aucune réouverture par une plateforme.** `ubereats.SyncOrderState` vers un état ouvert ne rouvre plus une commande close : le statut seulement.
- **R2. À la reclôture, on compare le contenu de la vente au dernier ticket de vente** (lignes, quantités, prix, taux de TVA, totaux TTC et HT, sous forme canonique) :
  - **identique :** ni avoir, ni nouveau ticket ;
  - **différent :** avoir du ticket d'origine (TVA ventilée, lot B), puis nouveau ticket ;
  - **commande rouverte puis annulée ou refusée :** avoir du ticket d'origine. Staging compte 23 commandes annulées porteuses d'un ticket sans avoir.
- **R3. Les corrections de paiement ne déclenchent ni avoir ni nouveau ticket.**

**C2 — Annulation d'un paiement**
- **R4. Annuler un paiement est une aide à l'encaissement**, par exemple une carte refusée après saisie, et non un remboursement. C'est possible seulement si la commande **et** le registre du paiement sont ouverts.
- **R5. Chaque annulation passe par une fonction unique, qui écrit dans la même transaction :**
  - une **entrée du journal d'audit** (action dédiée, par exemple `payment.cancelled`) : paiement, commande, source (`STAFF`, `STRIPE`, `UBER_EATS`), utilisateur et motif. Ce journal est chaîné et signé (S2) ;
  - `enabled = false`, qui reste un état dérivé.
  
  Le montant, le moyen et la date d'origine ne sont jamais modifiés. Aujourd'hui, `DELETE /orders/{id}/payments/{payment_id}` ne laisse **aucune** trace.
- **R6. Une fois la commande ou le registre clôturé, seul le remboursement existe** (`ProcessRefund`, déjà en place).
- **R7. Les webhooks n'agissent que sur une commande ouverte.**
  - Commande ouverte : Stripe `charge.refunded` et l'annulation Uber passent par la fonction de R5.
  - Commande close : ils ne font rien, et c'est journalisé.
  - Aujourd'hui, `charge.refunded` désactive le paiement d'origine même sur une commande close, ce qui, après l'avoir, retire la vente deux fois.

## Phase 0 — Recensement (rendu avant tout code)

1. **Réouverture :** chemins qui rouvrent une commande close, et leurs appelants côté caisse, back-office et borne.
2. **Annulations :** chemins qui passent `payments.enabled` à false (`DisablePayment`, `DisablePayments` dans `DeleteOrder` et `SetOrderDenied`, webhooks Stripe et Uber), et tous les lecteurs de `enabled` : rapports, Z, export comptable, `isPaid`, contrôle « entièrement payée ». **Aucun total ne doit changer.**
3. **Code mort Deliveroo** (`UpdateOrderRejected`, `DisablePayments` du webhook) : le supprimer ou le garder ? Proposition à faire.
4. **Données staging, en lecture seule, à titre indicatif :**
   - nombre de réouvertures ;
   - réouvertures après la fermeture d'un registre ou après une clôture journalière ;
   - paiements désactivés sur une commande close ;
   - commandes avec plusieurs tickets de vente.
5. **Caisse Flutter :** traitement des refus de réouverture et d'annulation (codes d'erreur), bouton de réouverture dans l'historique des registres.

**Point d'arrêt.**

## Phase 1 — Annulations de paiement (C2)

- **Fonction unique d'annulation :** elle vérifie R4, écrit l'entrée d'audit et passe `enabled` à false, dans une transaction.
- **Tous les chemins** de la phase 0 l'utilisent.
- **Hors des conditions de R4 :** erreur explicite pour la caisse ; aucune action, journalisée, pour un webhook.
- **`DELETE /payments/{id}`** gagne ainsi sa trace.

## Phase 2 — Réouverture et reclôture (C1)

- **Réouverture :**
  - R1 et R1 bis, avec un code d'erreur dédié ;
  - la réouverture est elle-même inscrite au journal d'audit, comme c'est déjà le cas via `ExecuteOrderMutation`.
- **Reclôture, ou annulation / refus d'une commande rouverte :** comparaison R2, puis avoir et nouveau ticket si nécessaire.

## Phase 3 — Webhooks (R7)

- Stripe `charge.refunded` et Uber `CancelOrder`.
- Code mort Deliveroo, selon la décision de la phase 0.

## Phase 4 — Caisse Flutter (dépôt `wello_resto_flutter`)

- Message clair sur un refus : commande scellée, registre fermé, commande close.
- Bouton de réouverture masqué quand la réouverture serait refusée.

## Tests attendus

- **R1 :**
  - refus sur une commande scellée ;
  - refus si un paiement est dans un registre fermé, y compris quand l'autre registre est encore ouvert ;
  - réouverture acceptée sinon.
- **R2 :**
  - reclôture sans changement → aucun ticket ;
  - ligne modifiée → avoir ventilé et nouveau ticket ;
  - annulation après réouverture → avoir.
- **R3 :** changement de moyen de paiement → aucun ticket.
- **R4 et R5 :**
  - entrée d'audit re-scellée à l'identique depuis la base ;
  - paiement d'origine inchangé ;
  - refus hors conditions.
- **R7 :**
  - `charge.refunded` sur une commande close → rien, totaux inchangés ;
  - sur une commande ouverte → entrée d'audit.
- **Totaux :** Z, export comptable, rapports et clôtures journalières identiques avant / après sur un même jeu de données.
- **Non-régression :** suites d'intégration des paquets touchés, tests des lots A et B.

## Livrables

- Code et tests.
- Mesures avant / après.
- Une entrée dans `decisions.md`.
- Le journal du lot.
- Commits sur accord.
