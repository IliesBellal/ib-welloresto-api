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

## Journal

### Phase 0 — Recensement (2026-10-07)

Aucun code écrit. Chiffres staging en lecture seule, à titre indicatif (données peu représentatives).

#### 1. Réouverture d'une commande close

- **Chemin explicite unique :** `PATCH /orders/{order_id}/reopen` (permission `pos.ticket.reopen`) → `OrdersLifeCycleService.ReopenClosedOrder` → `ExecuteOrderMutation` (audit `ORDER_REOPEN`, chaîné) → `repository.ReopenClosedOrder` : `UPDATE orders SET state = 'OPEN'` sans aucun contrôle (`// FUTURE VALIDATIONS HERE`), puis retrait des statistiques client. `OrdersService.ReopenClosedOrder` est commenté (mort).
- **Appelants :** caisse Flutter seulement, depuis l'historique des registres (`reopen_order_button.dart`, `order_history_details_dialog.dart`). Ni le back-office, ni la borne, ni ScanNOrder.
- **Réouvertures implicites trouvées (hors brief) :**
  - `ubereats.SyncOrderState`, branche « état ouvert » (Uber `ACCEPTED`) : `UPDATE ... state = ?` sans condition sur l'état. C'est R1 bis ;
  - **`SetOrderAcceptedLocal`** écrit `state = 'OPEN'` sans condition. Il est appelé par l'acceptation caisse, la borne, le webhook Stripe (après paiement), le webhook Uber (`SCHEDULED` de livraison) et le webhook Deliveroo (`accepted`, `confirmed`, et auto-acceptation). Un événement tardif ou rejoué sur une commande close la rouvre sans trace.
- **Staging :** 62 réouvertures (57 commandes, 4 établissements, mars → août 2026). Une seule après la fermeture d'un registre portant un paiement de la commande, 2 après la clôture journalière théorique ; délai médian depuis la clôture : 2,5 minutes. **Le refus R1 ne gênera presque jamais l'usage actuel.**
- **Reclôture :** chaque clôture (`DeliverOrder` → `HandlerFiscalReceiptGeneration`) émet un nouveau ticket de vente, sans avoir. 39 commandes ont plusieurs tickets de vente (jusqu'à 9) : la vente y est comptée plusieurs fois, y compris dans les clôtures journalières du lot B. R2 corrige cela.

#### 2. Annulations de paiement

**Chemins qui passent `payments.enabled` à false :**

| Chemin | Contrôle actuel | Trace |
|---|---|---|
| `DELETE /orders/{id}/payments/{payment_id}` → `OrdersLifeCycleService.DisablePayment` | commande ouverte (sinon `nil` silencieux, sans erreur) ; refus Uber Eats / Deliveroo ; remboursement Stripe asynchrone | aucune ; ni le marchand ni le lien paiement → commande ne sont vérifiés dans l'`UPDATE` |
| `DeleteOrder` → `DisablePayments` (tous les paiements) | commande ouverte, vérifiée hors transaction | audit de la commande seulement |
| `SetOrderDenied` → `DisablePayments` | **aucun** (seule la route caisse `DenyOrder` vérifie). Appelé aussi par la tâche `DenyOrders` et par Stripe `checkout.session.expired`, qui attend un `ErrOrderClosed` jamais renvoyé | audit de la commande seulement |
| Stripe `charge.refunded` → `repo.DisablePayment(pi)` | **aucun** : agit aussi sur une commande close | aucune |
| Uber `CancelOrder` (webhook) | **aucun** pour les paiements : désactive ceux de toutes les commandes du `brand_order_id`, closes comprises | aucune |
| Deliveroo `DisablePayments`, `UpdateOrderRejected` | **aucun appelant** (code mort) | — |

Les autres `UPDATE payments` ne touchent pas `enabled` : requalification du registre à la fermeture (S7, inchangée), réattribution au livreur, frais Stripe.

**Lecteurs de `enabled` :**
- Z et registres (`cash_registers` : rapport par moyen, TVA du registre, contrôle des registres) ;
- export comptable (`pos/accounting`), rapports TVA et paiements (`pos/reports`) ;
- analytics, fiche client, « entièrement payée » (`fiscal.OrderFullyPaid`, `isPaid`) ;
- clôtures journalières (paiements désactivés comptés à part).

Tous lisent `p.enabled = TRUE`. La fonction unique garde `enabled = false` comme effet : **aucun lecteur ne change, aucun total ne bouge** pour une annulation autorisée.

**Staging :**
- 283 ventes closes portent un paiement désactivé. 254 restent entièrement payées par leurs autres paiements (correction pendant l'encaissement, le cas R4). **29 sont sous-payées** (554,73 €) : désactivation après coup probable (CB 10, Deliveroo 10, espèces 6, Uber 2, Stripe 1). Aucune n'a d'avoir ;
- 4 commandes annulées ou refusées avaient un paiement dans un registre **déjà fermé** au moment de l'annulation (6 paiements, 110,20 €) : le Z fermé a été modifié après coup ;
- 40 commandes ont des paiements sur plusieurs registres ;
- aucun paiement actif d'une commande encore ouverte dans un registre fermé aujourd'hui.

#### 3. Code mort Deliveroo

`deliveroo_orders.Repository.UpdateOrderRejected` et `DisablePayments` n'ont aucun appelant ; le refus Deliveroo passe par `DeleteOrder`. **Proposition : les supprimer** (deux écritures hors fonction unique en moins).

#### 4. Caisse Flutter

- **Bouton de réouverture :** affiché si le registre de la commande (`order.cashRegister`, un seul) est ouvert. Il ignore la clôture journalière et les paiements sur un autre registre.
- **Annulation de paiement :** calculatrice (`_deletePayment`), avec une confirmation pour Stripe (« entraîne le remboursement »).
- **Erreurs :** `runApiAction` cherche un dialogue dédié par `id|status`. À défaut, il affiche `message` **au premier niveau** du JSON. Or `SendErrorJSON` le place dans `data`. **Une version actuelle affichera donc « Une erreur est survenue côté serveur. »** pour tout refus nouveau. Pas de déconnexion sur un 401 (`order_closed` répond 401).

#### 5. Autres constats

- **Multi-établissement :** `DisablePayment`, `OrderStillOpen` et `GetPayment` ne filtrent pas le marchand. Un utilisateur authentifié pourrait désactiver le paiement d'un autre établissement en connaissant les identifiants (entiers séquentiels). La fonction unique filtrera le marchand.
- **`ProcessRefund`** prend le dernier ticket, avoir compris (`GetReceiptByOrderID`). Un second remboursement partiel échoue donc (`refund_must_be_lower`). À corriger en phase 2 (`GetSaleReceiptByOrderID`).
- **`charge.refunded`** est aussi envoyé par un remboursement partiel ; il désactive aujourd'hui tout le paiement.

### Décisions d'Ilies après la phase 0 (2026-10-07)

1. **Acceptation sur commande close :** ne rouvre jamais ; statut mis à jour seulement sur une commande ouverte, sinon ignoré et journalisé (phase 2).
2. **Refus sur commande close :** contrôlé dans la transaction, `models.ErrOrderClosed` (phase 1).
3. **Annulation d'une commande ouverte dont un paiement est dans un registre fermé :** refusée, avec un message explicite sur la caisse ; la caisse clôt puis rembourse (phase 1).
4. **Code mort Deliveroo :** supprimé (phase 3).
5. **Remboursement Stripe partiel :** nos interfaces ne remboursent qu'en totalité (`RefundOrCancelAsync` ne transmet aucun montant) ; un remboursement partiel ne peut venir que du tableau de bord Stripe. Règle : total → annulation, partiel → journalisé seulement (phase 3).
6. **Compatibilité caisse :** message en français au premier niveau du JSON pour les nouveaux refus, **et** gestion des nouveaux codes dans la caisse Flutter (phase 4).
7. **`can_reopen`** dans les commandes de l'historique (phase 2).

### Phase 1 — Annulations de paiement (2026-10-07)

**Fonction unique** ([payment_cancel.go](../internal/fiscal/payment_cancel.go)), en deux entrées : `fiscal.CancelPayment` (un paiement : caisse, webhooks) et `fiscal.CancelOrderPayments` (tous les paiements d'une commande qu'on annule ou refuse). Dans une transaction (la sienne ou celle de l'appelant) :
1. verrouille la commande (`FOR UPDATE`), vérifie qu'elle appartient à l'établissement et qu'elle est ouverte ;
2. en une requête :
   - verrouille en partage (`FOR SHARE`) les registres des paiements visés et refuse si l'un est fermé. Les marqueurs (`SCANNORDER`, `UBER_EATS`, `KIOSK`) et l'absence de registre comptent comme « pas encore rattaché », donc annulable ;
   - sinon, `UPDATE payments SET enabled = FALSE ... RETURNING` sur le paiement visé, ou sur tous les paiements actifs de la commande. Rien d'autre n'est modifié (montant, moyen, date, registre). Un paiement déjà annulé est un no-op (webhook rejoué, double appui) ;
3. écrit une entrée `PAYMENT_CANCELLED` par paiement au journal d'audit chaîné et signé : `old_values` = état d'origine du paiement, `new_values` = `{enabled: false, source, reason}`, `user_id` = auteur. Plusieurs entrées : un verrou, une lecture du dernier maillon et une insertion (`fiscal.AppendAuditLogs`), datées à une microseconde d'écart pour garder l'ordre de la chaîne.

**Ordre des verrous** (optimisation demandée par Ilies, voir les mesures) :
- **`CancelPayment` :** le verrou de la chaîne d'audit est pris dans la requête qui verrouille la commande, juste après la ligne (projection au-dessus de `LockRows`, vérifié par `EXPLAIN`). Le dernier maillon est lu dans la requête d'annulation, et la commande repasse non payée dans cette même requête. Total : 4 allers-retours. Sans risque d'interblocage : après ce verrou, seuls sont verrouillés les registres (leur fermeture n'écrit pas au journal) et les paiements de cette commande, déjà verrouillée.
- **`CancelOrderPayments` :** la clôture de la commande (`closeOrder`, passée en paramètre) s'exécute entre l'annulation et le journal. Le verrou d'audit est pris en dernier, comme partout ailleurs, parce que la clôture verrouille des lignes (récompenses, fiche client, réservations) que d'autres transactions tiennent avant d'écrire leur propre entrée d'audit. La première version prenait ce verrou avant la clôture ; c'était un interblocage possible, corrigé.
- **Ce qui n'est pas regroupé :** verrouiller la commande et lire ses paiements dans une même requête. Après une attente sur la commande, la requête travaillerait sur un instantané pris avant l'attente, et manquerait un paiement ajouté entre-temps (la commande serait alors close avec un paiement actif).

**Refus :**

| Cas | Erreur | HTTP |
|---|---|---|
| commande ou paiement inconnu pour l'établissement | `ErrNotFound` | 404 |
| paiement d'une commande close | `payment_order_closed` | 409 |
| paiement dans un registre fermé | `payment_register_closed` | 409 |
| annulation ou refus d'une commande close | `order_closed` (existant) | 401 (inchangé) |
| annulation ou refus d'une commande dont un paiement est dans un registre fermé | `order_payment_register_closed` | 409 |

Les trois nouveaux codes portent un message en français au premier niveau de la réponse (`message`), en plus de `data` : les versions actuelles de la caisse l'affichent (décision 6). Les autres réponses d'erreur sont inchangées.

**Branchements :**
- `DELETE /orders/{id}/payments/{payment_id}` (`DisablePayment`) : `repository.CancelPayment` → `fiscal.CancelPayment` (commande repassée non payée, comme avant). Le remboursement Stripe part **après** le commit, et seulement si le paiement vient d'être annulé (avant : avant l'écriture, même en cas d'échec). Une commande close renvoie désormais `payment_order_closed` au lieu d'un succès silencieux ;
- `DeleteOrder` et `SetOrderDenied` : `repository.CancelOrderPayments` → `fiscal.CancelOrderPayments`, qui encadre la clôture de la commande dans la même transaction. Elle vérifie d'abord, sous verrou, que la commande est ouverte (décision 2 : la tâche de refus et Stripe `checkout.session.expired` ne peuvent plus refuser une commande close ; Stripe ignorait déjà `ErrOrderClosed`), annule les paiements, clôt la commande, puis écrit la trace de chaque paiement (décision 3) ;
- source déduite de l'identifiant d'acteur (`paymentCancelSource`) : `STAFF`, `SYSTEM`, `STRIPE`, `UBER_EATS`, `DELIVEROO`, `CUSTOMER` (borne, ScanNOrder) ;
- `OrdersLifeCycleRepository.DisablePayment` et `DisablePayments` sont supprimés ;
- **fermeture de registre :** la lecture « déjà fermé ? » en tête de fermeture prend `FOR UPDATE` sur le registre. Une annulation concurrente attend la fin de la fermeture puis est refusée, ou la fermeture attend l'annulation et calcule son Z sans le paiement annulé ;
- **journal d'audit :** l'écriture chaînée passe de `audit.insertLogWithChain` à `fiscal.AppendAuditLog` (même code, déplacé : `internal/fiscal` ne peut pas importer le module audit). Le module audit l'appelle.

Webhooks Stripe `charge.refunded` et Uber `CancelOrder` : phase 3.

**Totaux :** tous les lecteurs filtrent `p.enabled = TRUE` et la fonction garde cet effet. Aucun lecteur n'est modifié ; une annulation autorisée produit le même état qu'avant. Seuls changent les cas désormais refusés.

**Tests (staging) :**
- `fiscal.TestCancelPayments_Postgres` (dont la chaîne d'audit complète de l'établissement de test : parents et re-scellement de chaque entrée, entrées seules et par lot) :
  - annulation acceptée, paiement d'origine identique hormis `enabled`, entrée d'audit re-scellée à l'identique depuis la base, contenu de `old_values` / `new_values` ;
  - rejeu sans nouvelle entrée ;
  - paiements sans registre ou avec marqueur ;
  - refus : commande close, registre fermé (y compris avec un autre registre encore ouvert), autre établissement, paiement d'une autre commande ;
  - fermeture de registre concurrente (l'annulation attend puis est refusée) ;
  - annulation de toute la commande.
- `order_life_cycle.TestPaymentCancellation_Service_Postgres` :
  - caisse : acceptée et tracée, refus explicites ;
  - annulation de commande refusée (registre fermé) sans rien changer, ou acceptée avec la trace de chaque paiement ;
  - refus Stripe sur commande close refusé, sur commande ouverte accepté (source `STRIPE`).
- `models.TestSendErrorJSON_PaymentCancelRefusals` : code dans `data.status`, message au premier niveau, autres erreurs inchangées.
- Non-régression : suites d'intégration `fiscal`, `order_life_cycle`, `audit`, `cash_registers`, `pos/accounting`, `webhook/stripe`, `receipt`, `tasks`, `webhook/deliveroo_orders`, `webhook/ubereats/repository` vertes. Échecs connus sans lien : `kiosk` (migration 167 non appliquée sur staging), `scannorder` (schéma staging), `ubereats` ligne 226, tests unitaires `planning` et tests `sqlmock` lancés avec `DB_DIALECT=postgres`.

**Mesures** (`TestFiscalChainPerf_Postgres`, depuis le poste, aller-retour réseau 20 ms) :

| Opération | Avant | Après |
|---|---|---|
| encaissement séquentiel, p50 | 145 ms | 145 ms |
| clôture de commande séquentielle, p50 | 308 ms | 304 ms |
| annulation de paiement séquentielle, p50 | 102 ms | 184 ms (1re version) → **126 à 132 ms** |
| annulation de paiement, 10 simultanées (durée totale) | 2,13 s | 3,25 s (1re version) → **2,16 à 2,26 s** |
| annulation de commande payée séquentielle, p50 | 105 ms | 183 ms (1re version) → 197 à 203 ms (bruit réseau : p95 de l'aller-retour à 31-40 ms pendant ces mesures) |
| fourches (toutes chaînes), tickets en double | 0 | 0 |

Encaissement et clôture sont inchangés.
- **Annulation d'un paiement :** un aller-retour de plus qu'avant le lot C, au lieu de quatre dans la première version.
- **Annulation ou refus d'une commande :** quatre de plus (verrou de la commande, annulation, verrou d'audit, lecture du dernier maillon ; l'insertion de la trace s'ajoute à l'ancien `UPDATE`). Le prix de l'ordre des verrous ci-dessus.

Entre l'API et la base hébergées ensemble (aller-retour inférieur à la milliseconde), cela représente au plus quelques millisecondes. Les annulations simultanées d'un même établissement se sérialisent sur la chaîne d'audit, comme toute écriture journalisée.

### Phase 2 — Réouverture et reclôture (2026-10-08)

**Réouverture (R1, S6) : `fiscal.ReopenOrder`** ([reopen.go](../internal/fiscal/reopen.go)), appelée par `repository.ReopenClosedOrder`, toujours dans `ExecuteOrderMutation` (entrée `ORDER_REOPEN` au journal, inchangée). Deux requêtes :
1. verrouille la commande, puis la chaîne des clôtures fiscales de l'établissement (`fiscal_closures`). Une clôture journalière en cours d'écriture se termine avant le contrôle ; une clôture qui démarre attend la réouverture et ne scelle plus la commande ;
2. en une requête :
   - verrouille en partage les registres des paiements **actifs** de la commande ;
   - vérifie l'état et la clôture journalière du jour de `delivered_on` (fuseau de l'établissement ; sans fuseau, pas de clôture, donc jamais scellée) ;
   - rouvre si tout va bien.

**Refus (HTTP 409, message en français au premier niveau) :**
- `reopen_order_sealed` : la journée est clôturée ;
- `reopen_payment_register_closed` : un paiement actif est dans un registre fermé, même si un autre registre de la commande est encore ouvert.

Un paiement annulé ne compte pas. Une commande déjà ouverte reste un no-op. Les statistiques client ne sont retirées que si la commande a vraiment été rouverte.

**`can_reopen`** dans `POST /orders/history` (seul historique où la caisse propose la réouverture). Ajouté aux commandes closes de la page : `fiscal.ReopenableOrders`, mêmes règles sans verrou, une requête pour la page. Champ absent pour une commande ouverte. Ajout seul : les caisses actuelles l'ignorent (la caisse s'en servira en phase 4).

**Plus aucune réouverture implicite :**
- `SetOrderAcceptedLocal` ne touche plus une commande close (`state NOT IN ('CLOSED', 'DONE')`). `SetOrderAccepted` ignore alors l'acceptation, la journalise (`Warn`) et n'appelle pas les plateformes (décision 1). Cela couvre l'acceptation caisse, la borne, Stripe, Uber `SCHEDULED` et Deliveroo `accepted` / `confirmed` ;
- `ubereats.SyncOrderState` vers un état ouvert garde l'état d'une commande close : statut seulement (R1 bis).

**Reclôture (R2, R3) : dans `receipt.GenerateFiscalReceipt`**, donc pour toute clôture : caisse, tâche planifiée, plateformes.
- La tête de chaîne est lue avec la vente en vigueur de la commande, en une requête (`GetReceiptChainHead`, sous le verrou de la chaîne des tickets) : dernier ticket de vente, et ce qu'il en reste après les avoirs émis depuis.
- Le contenu de vente est comparé au dernier ticket de vente, sous forme canonique : totaux TTC et HT, TVA ventilée, lignes (nom, quantité, prix, taux, TVA) sans tenir compte de leur ordre. Les paiements n'en font pas partie (R3).
- **Identique :** aucun ticket.
- **Différent :** avoir du dernier ticket pour ce qu'il en reste (TVA au prorata, lot B), puis nouveau ticket.
- **Vente déjà entièrement annulée par avoirs :** nouveau ticket.
- Un ticket antérieur au lot B (TVA non ventilée) diffère toujours : avoir puis nouveau ticket.

**Annulation ou refus d'une commande rouverte :** la requête qui verrouille la commande (`fiscal.CancelOrderPayments`) indique si elle a un ticket de vente. Si oui, `receipt.CancelSaleReceipt` émet l'avoir de la vente en vigueur, dans la transaction de l'annulation.

**Avoir de correction :** sans paiement dans son détail (`payments_snapshot = []`), puisqu'aucun argent ne bouge. Le remboursement (`ProcessRefund`) garde son paiement négatif.

**`ProcessRefund`** part du dernier **ticket de vente** (`GetSaleReceiptByOrderID`) et non plus du dernier ticket, avoir compris : un second remboursement partiel échouait.

**Migration 172** ([172_receipts_order_index.up.sql](../migrations/todo/172_receipts_order_index.up.sql)) : index `receipts (order_id, created_at DESC)`, en `CONCURRENTLY`, **à appliquer avant le code**. Aucun index ne couvrait `receipts.order_id`. Sur staging, sans elle, la lecture parcourt toute la table : 11 ms pour 5 870 tickets, sous le verrou de la chaîne des tickets. En production, la table est plus grosse.

**Tests (staging, sans la migration 172) :**
- `fiscal.TestReopenOrder_Postgres` :
  - refus : journée scellée (clôture réelle via `CloseDueDays`), paiement dans un registre fermé avec un autre registre ouvert, autre établissement ;
  - un paiement annulé dans un registre fermé ne bloque pas ;
  - commande ouverte : no-op ;
  - `ReopenableOrders` donne les mêmes réponses.
- `receipt.TestReceiptReclose_Postgres` :
  - reclôture identique (lignes permutées, paiement changé) : aucun ticket ;
  - ligne modifiée : avoir ventilé sans paiement, puis nouveau ticket ;
  - remboursement partiel puis modification : avoir du reste seulement ;
  - annulation : un seul avoir, même appelée deux fois ;
  - reclôture d'une vente entièrement annulée : nouveau ticket ;
  - chaîne des tickets : numéros consécutifs, chaque ticket re-scellé à l'identique.
- `order_life_cycle.TestReopenAndAccept_Service_Postgres` :
  - acceptation sur commande close ignorée ;
  - réouverture tracée puis annulation : avoir, somme des tickets nulle, paiement annulé ;
  - refus sans entrée au journal.
- `ubereats.TestUberReconciliation_FiscalChain_Postgres` : `ACCEPTED` sur commande close, statut seulement.
- Non-régression : `fiscal`, `order_life_cycle`, `receipt`, `audit`, `cash_registers`, `pos/accounting`, `webhook/stripe`, `tasks`, `webhook/deliveroo_orders`, `webhook/ubereats/repository`, `customers` verts. Échecs déjà présents sur HEAD (vérifié sur un export propre de HEAD) : `orders` (`TestDiscountTimeRestriction`, `TestOrdersRepository` : schéma staging), `ubereats` ligne 226, tests unitaires `planning` et `ubereats` BYOC.

**Mesures** (depuis le poste, aller-retour 20 ms ; `TestReopenPerf_Postgres`, nouveau, lancé sur HEAD et sur le lot C) :

| Opération | Avant (HEAD) | Lot C |
|---|---|---|
| clôture de commande, p50 | 248 ms | 246 ms |
| réouverture, p50 | 82 ms | 102 ms |
| reclôture à l'identique, p50 | 247 ms | **227 ms** (aucun ticket écrit) |
| tickets écrits pour 20 commandes fermées, rouvertes et refermées | 40 | **20** |

Sur `TestFiscalChainPerf_Postgres`, clôtures simultanées : 4,02 à 4,05 s, comme avant. La clôture séquentielle prend environ 15 ms de plus sur staging : c'est la lecture sans la migration 172, qui disparaît avec elle.

- **Réouverture :** un aller-retour de plus, le verrou des clôtures fiscales. Sans lui, une clôture journalière concurrente pourrait sceller une commande en train d'être rouverte.
- **Ticket de la commande :** il n'est lu que pour l'annulation de toute la commande, jamais sous le verrou d'audit de l'annulation d'un paiement. Le lire aussi dans ce cas faisait passer 10 annulations simultanées de 2,2 à 2,8 s ; corrigé, elles reviennent à 2,3 à 2,6 s.

### Phase 3 — Webhooks (2026-10-08)

**Migration 172** appliquée sur staging par Ilies avant cette phase.

**Stripe `charge.refunded`** (`HandleRefund` → `cancelRefundedPayments`) :
- **Commande ouverte et remboursement total** : le paiement actif du PaymentIntent passe par la fonction unique (`fiscal.CancelPayment`, source `STRIPE`, motif « Remboursement Stripe <charge> »). La commande repasse non payée, et la caisse est notifiée (cache Redis vidé, notification de mise à jour).
- **Remboursement partiel** : rien ne change, c'est journalisé (`Warn`). Nos interfaces ne remboursent qu'en totalité (`RefundOrCancelAsync` ne transmet aucun montant), donc un partiel vient du tableau de bord Stripe (décision 5).
- **Commande close ou registre fermé** : rien ne change, c'est journalisé. La vente reste au ticket, au Z et aux clôtures ; l'avoir se fait depuis la caisse. Avant, le paiement était désactivé dans tous les cas, ce qui retirait deux fois une vente déjà remboursée par avoir.
- **Paiement déjà annulé** (annulation depuis la caisse, qui déclenche le remboursement Stripe, puis ce webhook ; ou refus d'une commande) : il n'est plus actif, donc rien à faire.
- Le mail de remboursement au client part comme avant.
- `Repository.DisablePayment` est remplacé par `GetActivePaymentsByIntent` et `CancelRefundedPayment`.

**Uber Eats, annulation par webhook** (`OrdersRepository.CancelOrder`) : seules les commandes encore ouvertes du `brand_order_id` sont touchées. Pour chacune, `fiscal.CancelOrderPayments` (source `UBER_EATS`) annule ses paiements avec leur trace et la clôt en annulation dans la même transaction. Une commande rouverte après une vente reçoit l'avoir de cette vente. Une commande déjà close ne change plus : avant, ses paiements étaient désactivés. C'est journalisé.

**Code mort Deliveroo supprimé** (décision 4) : `Repository.UpdateOrderRejected` et `Repository.DisablePayments`, sans appelant, ainsi que leurs tests. Le refus et l'annulation Deliveroo passent par `order_life_cycle.DeleteOrder`.

**Plus aucune écriture de `payments.enabled = false` hors de la fonction unique** (vérifié par recherche dans `internal/`).

**Tests (staging) :**
- `webhook/stripe.TestStripeRepository_Postgres` : `GetActivePaymentsByIntent`, puis `CancelRefundedPayment` qui annule sur commande ouverte et refuse sans rien changer sur commande close ;
- `webhook/ubereats/repository.TestOrdersRepository_Postgres` : annulation d'une commande ouverte (paiement annulé, entrée `PAYMENT_CANCELLED` de source `UBER_EATS`) ; commande déjà close : ni statut, ni date, ni paiement modifiés ;
- non-régression : suites d'intégration `fiscal`, `order_life_cycle`, `receipt`, `audit`, `cash_registers`, `pos/accounting`, tous les `webhook/...`, `tasks`, `customers` vertes. Échecs connus sans lien : `kiosk` (migration 167 non appliquée sur staging), `scannorder` (schéma staging), `ubereats` ligne 226, tests unitaires `planning` et `ubereats` BYOC.

La décision « total ou partiel » de `charge.refunded` n'a pas de test unitaire : le service Stripe n'a pas de faux dépôt, et il faudrait en écrire un pour toute l'interface. La règle tient en une ligne (`Refunded` ou montant remboursé ≥ montant).

**Mesures avec la migration 172** (depuis le poste, aller-retour 20 ms, deux passes) :

| Opération | Avant le lot C | Lot C, avec la 172 |
|---|---|---|
| encaissement séquentiel, p50 | 145 ms | 144 à 154 ms |
| clôture séquentielle, p50 | 304 à 318 ms | 304 à 311 ms |
| 10 clôtures simultanées (durée totale) | 4,07 à 4,22 s | 3,69 à 4,14 s |
| annulation de paiement séquentielle, p50 | 102 ms | 123 à 130 ms |
| annulation de commande payée séquentielle, p50 | 105 ms | 188 à 195 ms |
| clôture (`TestReopenPerf`), p50 | 248 ms | 248 à 249 ms |
| réouverture, p50 | 82 ms | 101 à 103 ms |
| reclôture à l'identique, p50 | 247 ms | 222 à 224 ms |

Avec l'index, le surcoût de lecture des clôtures disparaît.

### Phase 4 — Caisse Flutter (2026-10-08)

Dépôt `wello_resto_flutter`, qui porte d'autres modifications en cours non commitées. Les changements du lot sont ciblés et documentés dans son `docs/decisions.md` (entrée du 2026-10-08).

- **Messages clairs sur un refus** : les cinq codes (`payment_order_closed`, `payment_register_closed`, `order_payment_register_closed`, `reopen_order_sealed`, `reopen_payment_register_closed`, lus dans `data.status`) ont un dialogue d'avertissement dédié dans `lib/helpers/api_action_handler.dart`, quel que soit l'appel (suppression d'un paiement, annulation de commande, réouverture). Chaque dialogue dit quoi faire : clôturer, puis rembourser depuis l'historique. Une version antérieure de la caisse affiche le `message` français que l'API pose au premier niveau de la réponse.
- **Bouton de réouverture masqué quand l'API refuserait** : `OrderResponse` / `OrderDto` lisent `can_reopen`, et `OrderDto.showsReopen` pilote la cellule et le détail de l'historique. Sans ce champ (API antérieure), l'ancienne règle s'applique : registre de la commande ouvert.
- **Tests** : `test/helpers/cash_compliance_test.dart` (un dialogue par code, sans le texte serveur ; `can_reopen` fait foi, repli sans le champ). `flutter analyze` sans remarque sur les fichiers touchés ; suite complète : 39 tests verts.

### Bilan du lot C

| Règle | Où | État |
|---|---|---|
| R1 réouverture refusée (scellée, registre fermé) | `fiscal.ReopenOrder` | fait |
| R1 bis plus de réouverture par une plateforme | `SyncOrderState`, `SetOrderAcceptedLocal` | fait (étendu aux acceptations, décision 1) |
| R2 reclôture : rien ou avoir puis nouveau ticket ; avoir à l'annulation | `receipt.GenerateFiscalReceipt`, `CancelSaleReceipt` | fait |
| R3 corrections de paiement sans ticket | comparaison sans les paiements | fait |
| R4 annulation sur commande et registre ouverts | `fiscal.CancelPayment` / `CancelOrderPayments` | fait |
| R5 fonction unique et trace d'audit | idem, `PAYMENT_CANCELLED` | fait |
| R6 après clôture, remboursement seulement | refus explicites ; `ProcessRefund` sur le dernier ticket de vente | fait |
| R7 webhooks limités aux commandes ouvertes | Stripe `charge.refunded`, Uber `CancelOrder` | fait |

**Migration à appliquer en production :** 172, avant le code (ajoutée à l'ordre de la feuille de route).
