# Blocage de clients (commande en ligne ScanNOrder)

Un restaurateur peut bloquer un client de son établissement : ses commandes en
ligne ScanNOrder (à emporter et livraison) sont refusées, sans rien révéler au
client. Implémenté le 2026-10-14. Migration `183_customer_blocking` (à jouer à
la main, **avant** le déploiement du code).

## Décisions

| Sujet | Décision |
|---|---|
| Périmètre | Un seul établissement : `customer.merchant_id`. Bloquer chez A n'a aucun effet chez B. |
| Canaux | ScanNOrder uniquement. Borne (kiosk), réservations et caisse ne sont pas concernées. |
| Modes de commande | `TAKE_AWAY` et `DELIVERY`. Les commandes `IN` (QR en salle) ne sont pas contrôlées : elles viennent d'une table, pas d'un client saisi. |
| Message au client | Neutre : statut `order_unavailable`, aucun message, aucun motif. Le front affiche un refus générique. |
| Client inconnu | Bloquer un numéro qui n'a pas de fiche **crée la fiche** (puis la bloque). |
| Client supprimé | Un blocage survit à la suppression (soft delete) de la fiche : le contrôle ignore `enabled`, et la fiche bloquée reste listée pour pouvoir être débloquée. |
| Doublons | Un blocage couvre toutes les fiches de l'établissement qui partagent le téléphone **ou** l'email (insensible à la casse) de la fiche visée. `FindCustomerByPhone` n'en retient qu'une, le contrôle de commande les parcourt toutes. |
| Motifs | Table `deletion_reasons` (object `customer`), libellés FR dans `labels` (`label_type = 'deletion_reason'`), comme les motifs d'annulation de commande. |

## Motifs de blocage

Seedés par la migration (français uniquement, comme les autres motifs).
`GET /pos/deletion_reasons/customer` les sert sans code supplémentaire.

1. Commande non retirée
2. Fausse commande / canular
3. Comportement abusif envers le personnel
4. Impayé / litige de paiement
5. Fraude suspectée
6. Autre — `requires_comment = true`

Les IDs sont `GENERATED ALWAYS` : ne jamais les coder en dur, le front les lit
via l'endpoint ci-dessus. Pour ajouter un motif : `INSERT` dans
`deletion_reasons` (object `customer`) puis dans `labels` avec
`label_value = deletion_reason_id` (cf. migration 183).

## Modèle de données

Colonnes ajoutées à `customer` :

| Colonne | Rôle |
|---|---|
| `blocked_at` | `NULL` = non bloqué. Seule colonne lue par le contrôle de commande. |
| `blocked_by` | `user_id` (texte) de celui qui a bloqué. |
| `blocked_reason_id` | `deletion_reasons.deletion_reason_id`, en texte. Pas de FK (convention du repo). |
| `blocked_comment` | Précision libre (255 car.), obligatoire si le motif l'exige. |

Index partiel `idx_customer_blocked_tel (merchant_id, customer_tel) WHERE blocked_at IS NOT NULL`.
Pas d'index sur l'email : la commande ScanNOrder ne transporte pas d'email
(voir Limites).

## API

Les deux routes exigent `customers.manage` et écrivent dans le journal d'audit
(`customer_blocked`, `customer_unblocked`).

### `POST /customers/block`

```json
{
  "customer_id": "123",          // soit une fiche existante...
  "phone": "06 12 34 56 78",     // ...soit un téléphone et/ou un email
  "email": "x@example.fr",
  "first_name": "", "last_name": "",   // utilisés seulement pour une création
  "reason_id": "42",             // obligatoire, motif 'customer' actif
  "comment": ""                  // obligatoire si le motif l'exige
}
```

Réponse `200` (`201` si une fiche a été créée) :
`{ "status": "success", "customer_id": "...", "created": false, "blocked_count": 2 }`.

Erreurs `400` : `identity_required`, `invalid_phone`, `invalid_email`,
`invalid_reason`, `comment_required`. `404` : `customer_not_found` (ID inconnu
ou d'un autre établissement). Rien n'est créé en cas de refus.

Re-bloquer une identité déjà bloquée est idempotent : pas de nouvelle fiche, le
dernier motif saisi remplace le précédent.

### `PATCH /customers/{customer_id}/unblock`

Lève le blocage de la fiche **et** de toutes celles qui partagent son
téléphone ou son email (lever une seule fiche en doublon laisserait le client
bloqué). `404 customer_not_found` si la fiche n'existe pas dans
l'établissement.

### Lecture

`GET /customers/list` et `/customers/search` exposent `blocked` (bool),
`blocked_at`, `blocked_reason_id`, `blocked_comment` sur chaque client. Le
libellé du motif est à résoudre côté front avec `/pos/deletion_reasons/customer`.
Ces deux listes incluent désormais les fiches supprimées **mais bloquées**.

## Contrôle à la commande

`scannorder.Service.GetPricingSNO`, juste après la vérification du mode de
commande : si le mode est `TAKE_AWAY`/`DELIVERY` et qu'un client bloqué de
l'établissement porte le téléphone de la commande, la réponse est
`{"status": "order_unavailable"}`. `CreateOrderSNO` appelle toujours
`GetPricingSNO` avant de créer quoi que ce soit, le contrôle couvre donc les deux
routes (`/pricing` et `/orders`), **avant tout paiement Stripe**, et ne laisse
aucune commande ni aucun client créé.

- Le téléphone est normalisé comme `FindCustomerByPhone` (indicatif FR par défaut) : `06 12 34 56 78`, `0612345678` et `+33612345678` sont le même numéro.
- Un lookup en échec renvoie une erreur (jamais « non bloqué »), comme le lookup client existant.
- Le refus est loggé côté serveur (`SNO order refused: customer blocked by merchant`), jamais détaillé dans la réponse.
- Une commande sans téléphone n'est jamais bloquée.

## Limites connues

- **Contournement.** L'identité d'un client ScanNOrder est le téléphone qu'il saisit : un autre numéro contourne le blocage. La commande ne transporte ni email ni compte (`models.CustomerRequest` n'a pas d'email), donc bloquer par email n'a d'effet à la commande que via les fiches liées au même client (doublons). Aller plus loin (empreinte d'appareil, IP) est un autre chantier, avec un enjeu vie privée.
- Les commandes déjà en cours au moment du blocage ne sont pas annulées.
- Le blocage n'est pas visible pour le client, et ne prévient pas l'établissement qu'une commande a été refusée (uniquement le log serveur).

## Fronts (autres dépôts, intégrés le 2026-10-14)

- **wello-resto-scannorder** — `src/components/cart/CheckoutFlow.tsx` : le statut `order_unavailable` renvoyé par `POST /scannorder/{slug}/orders` affiche un toast neutre (« Impossible de passer commande » / « Votre commande n'a pas pu être enregistrée »), sans cause ni démarche suggérée, et le panier est conservé. Seule la création de commande est concernée : le pricing en direct du panier envoie un téléphone vide (`useCartPricing`) et ne déclenche donc jamais le refus.
- **wello-back-office** (droit `customers.manage` requis pour toutes les actions) :
  - `components/customers/CustomerBlockDialog.tsx` : boîte « Bloquer » (motif via `GET /pos/deletion_reasons/customer`, précision obligatoire si le motif l'exige). Sans fiche, elle sert à **bloquer un numéro** (téléphone requis, nom/email facultatifs) : l'API crée la fiche au besoin.
  - Liste des clients : entrée « Bloquer un numéro » dans le menu du bouton « Créer un client », badge rouge « Bloqué » dans le tableau.
  - Fiche client : badge « Bloqué », encart (motif, précision, date) avec bouton « Débloquer », bouton « Bloquer ce client » sinon. Avertissement si le client n'a pas de téléphone (le blocage ne pourrait rien refuser).
  - `services/customersService.ts` (`blockCustomer`, `unblockCustomer`, `getCustomerBlockReasons`, `describeBlockCustomerError`) et `hooks/useCustomerBlockReasons.ts` (cache 10 min, partagé entre dialogue, liste et fiche).

## Déploiement

1. Jouer `migrations/todo/183_customer_blocking.up.sql` (idempotent, rejouable).
2. Déployer l'API. Sans la migration, chaque commande ScanNOrder `TAKE_AWAY`/`DELIVERY` échoue (colonne `blocked_at` absente), ainsi que `/customers/list` et `/customers/search`.
3. Retour arrière : `183_customer_blocking.down.sql` (supprime colonnes, index, motifs et libellés).

## Tests

- Unitaires : `internal/modules/customers/blocking_test.go`, `internal/modules/scannorder/customer_block_guard_test.go`.
- Intégration Postgres (`-tags postgres_integration`, migration 183 requise) : `internal/modules/customers/blocking_postgres_integration_test.go` — motifs et libellés, numéro inconnu, formats de téléphone, doublons par téléphone et par email, fiche supprimée, isolation entre établissements, déblocage.

```
go test ./internal/modules/customers/... ./internal/modules/scannorder/...
go test -tags postgres_integration ./internal/modules/customers/ -run TestCustomerBlocking_Postgres
```
