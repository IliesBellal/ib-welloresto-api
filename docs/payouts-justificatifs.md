# Justificatifs de versement

Chaque payout Stripe versé à un restaurateur est accompagné de deux documents,
envoyés dans un seul mail :

- le **relevé de versement** : ce que contient le virement (encaissements par
  canal, remboursements, commission, frais Stripe, net versé) ;
- la **facture de la commission** Wello Resto prélevée sur ces encaissements.

Les payouts des restaurateurs sont mensuels (le 7, avec 7 jours de décalage) :
un payout = un relevé + une facture par restaurateur et par mois. Les documents
sont rattachés au **payout**, pas au mois calendaire : le payout du 7 contient
les soldes disponibles à cette date, la période affichée est celle des ventes
qu'il contient.

## Fonctionnement

```
Stripe ── payout.paid ──► webhook ──► payout_documents (pending)
                                            │
              cron @hourly RunPayoutDocuments (une instance à la fois)
                                            ▼
        lignes du payout (API Stripe) ─► agrégation ─► relevé PDF
                                            │            + facture PDF
                                            ▼
                    archive R2 privé ─► mail ─► payout_documents (done)
```

1. Le webhook `payout.paid` ([internal/webhook/stripe/service.go](../internal/webhook/stripe/service.go))
   insère une ligne `pending` (clé unique sur `payout_id` : un événement rejoué
   ne crée pas de doublon). L'ancien mail « Virement en cours » est conservé.
2. La tâche horaire `RunPayoutDocuments` ([internal/tasks/payout_documents.go](../internal/tasks/payout_documents.go))
   traite les payouts `pending` :
   - lignes du payout : `GET /v1/balance_transactions?payout=po_…&expand[]=data.source`
     sur le compte connecté ([internal/infrastructure/stripe/payouts.go](../internal/infrastructure/stripe/payouts.go)) ;
   - rattachement des paiements aux commandes par `stripe_payments.payment_intent_id`,
     canal tiré de `orders.order_source` (`SCANNORDER`, `KIOSK`, le reste = « autres ») ;
   - **contrôle** : la somme des lignes doit égaler le montant du payout. Sinon
     Stripe n'a peut-être pas fini de rapprocher : le payout est retenté 3 fois
     (une par heure) puis **envoyé quand même**, avec une ligne « Écart non
     rapproché par Stripe » et un avertissement sur le relevé, et un log
     d'erreur ;
   - relevé et facture en PDF, archivés dans le bucket R2 privé
     (`payouts/<marchand>/<payout>/…`), envoyés par Brevo.
3. Un payout dont le mail échoue (ou dont Stripe est injoignable) reste `pending`
   et est retenté (72 tentatives, soit 3 jours, puis `failed` + log d'erreur) ;
   la facture, elle, n'est jamais émise deux fois (une par `payout_id`).
4. **Rien d'indéfini ne bloque l'envoi** : une mention légale inconnue est omise
   du PDF, un compte Stripe sans restaurateur est identifié par son numéro de
   compte, un restaurateur sans e-mail est remplacé par `testRecipient`.

Code : [internal/modules/payouts/](../internal/modules/payouts/).

## Ce que Stripe renvoie

Sur un compte connecté, un paiement est une ligne `charge` dont `fee_details`
contient deux entrées : `application_fee` (la commission Wello Resto, TTC) et
`stripe_fee` (frais de traitement). Il n'y a pas de ligne `application_fee`
séparée côté restaurateur (elle existe côté plateforme). Vérifié sur un
paiement borne en mode test. Les remboursements (`refund`) portent le retour
proportionnel de la commission. Tout ce qui n'est ni vente ni remboursement
(litiges, ajustements, types inconnus) est rangé dans « Litiges et ajustements »
plutôt que de faire échouer le relevé.

## Décisions

| Sujet | Décision |
|---|---|
| Détail des commandes | Absent pour l'instant (relevé de totaux). La ventilation par canal est conservée. |
| Commission | TTC, TVA 20 % : HT = TTC / 1,2, arrondi au centime ; `HT + TVA = TTC` toujours. |
| Commission négative | `log.Error`, aucune facture, le relevé part seul. Commission nulle : pas de facture, sans bruit. |
| Numérotation | `<série>-<année>-<n°6 chiffres>`, compteur `invoice_counters` incrémenté dans la transaction de l'insertion de la facture : jamais de trou. |
| Mentions légales, destinataire, série | En dur dans [legal.go](../internal/modules/payouts/legal.go), sans variable d'environnement. |
| Émetteur | Wello Resto, exploité par BINYA SAS (capital 1 000 €, SIREN 103020558, 13 rue Principale, 57450 Theding). RCS et n° de TVA inconnus : lignes omises de la facture. |
| Mail | Un seul mail, deux pièces jointes (une seule si pas de facture). |

## Back-office

Page **Comptabilité → Versements** (`/accounting/payouts` dans wello-back-office,
droit `reports.financial.read`, comme les archives fiscales) : historique des
versements dont les justificatifs ont été envoyés, avec boutons « Relevé » et
« Facture ». API, dans le groupe `/accounting` :

| Route | Rôle |
|---|---|
| `GET /accounting/payouts` | Versements de l'établissement de l'utilisateur, du plus récent au plus ancien (60 au plus). |
| `GET /accounting/payouts/{payout_id}/{kind}/download` | `kind` = `statement` ou `invoice`. Lien signé R2 d'une heure ; 404 si le payout est inconnu, d'un autre établissement, sans facture ou non archivé. |

Les liens passent par l'archive R2 privé : un PDF dont l'archivage a échoué
(log d'erreur `archivage R2 impossible`) est envoyé par mail mais n'est pas
téléchargeable. Un payout dont le restaurateur est inconnu n'apparaît dans
aucun back-office.

## Phase de test (état actuel)

`documentsTestMode = true` dans `legal.go` :

- **tous** les justificatifs partent à `iliesbellal@gmail.com` (objet préfixé
  `[TEST] <restaurateur>`, bandeau « phase de test » dans le corps) ;
- les factures sont numérotées `TEST-2026-000001…`, bandeau « SPÉCIMEN » sur le
  PDF, compteur séparé : la série légale `WR-` n'est pas consommée ;
- le mail « Virement en cours » existant continue de partir aux restaurateurs.

## Passage en production

1. Appliquer la migration `migrations/todo/178_payout_documents.up.sql` (avant le
   déploiement : le webhook écrit dans `payout_documents`).
2. Compléter l'émetteur dans `legal.go` (`issuer.RCS` et `issuer.VATNumber`, vides
   aujourd'hui : une facture avec ligne de TVA doit porter le n° de TVA de
   l'émetteur). Rien ne bloque s'ils restent vides, mais les factures réelles
   seraient alors incomplètes.
3. Passer `documentsTestMode` à `false` : destinataire = `merchant.email`, série `WR-`.
4. Décider du sort de l'ancien mail « Virement en cours » (`SendPayoutPaidNotification`) :
   il fait doublon avec le nouveau mail ; à retirer de `HandlePayoutPaid`.
5. Régénérer les documents des payouts passés avec la commande de rattrapage
   (les lignes de test `done` doivent d'abord être supprimées de `payout_documents`
   et de `commission_invoices` pour être refaites en série `WR-`).

## Rattrapage et premiers essais

```bash
# Liste seulement (rien n'est écrit ni envoyé) :
DB_DIALECT=postgres POSTGRES_URL=... STRIPE_API_KEY=... BREVO_API_KEY=... \
  GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=... PIN_PEPPER=x \
  go run ./cmd/backfill_payout_documents --payout=po_xxx

# Génère et envoie :
... go run ./cmd/backfill_payout_documents --payout=po_xxx --apply
... go run ./cmd/backfill_payout_documents --since-days=60 --apply
```

La clé Stripe et la base doivent être celles de l'environnement visé : un payout
live n'existe pas avec une clé `sk_test_`.

## À vérifier / reporté

- **Premier payout réel** : confirmer sur un export du dashboard que la somme des
  lignes retombe sur le montant et que la ventilation canal est juste (testé en
  mode test et sur des lignes simulées, pas sur un payout live).
- **Mentions légales** de la facture à faire valider par le comptable : RCS et
  n° de TVA de l'émetteur, régime de TVA sur encaissements/débits, mention
  d'acquittement.
- **Détail des commandes** (liste + CSV) : reporté.
- **Facture avec commission négative** (avoir) : non gérée, seulement loguée.
- **Pied du mail « Virement en cours »** (Wello Resto SAS, Woippy) : adresse
  différente de l'identité de la facture (BINYA SAS, Theding) ; à harmoniser quand
  ce mail sera retiré.
- **Renvoi d'un justificatif** par mail depuis le back-office : non fait.
