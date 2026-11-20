# Compte client : avance et ardoise

Chantier du 2026-11-17 (lot 2 du chantier « paiements » ; le lot 1 est le
paiement attendu, voir `docs/order-lifecycle.md`). Migration
`migrations/todo/220_customer_accounts`, à appliquer **avant** le code.

## Le besoin

Un client peut verser une somme d'avance (« 1 000 € pour l'année ») puis la
consommer, ou consommer sans payer et tout régler plus tard (ardoise).

## Décisions (Ilies, 2026-11-17)

| Sujet | Décision |
|---|---|
| Solde | Un seul par client et par établissement, signé : > 0 avance, < 0 ardoise. |
| Ardoise | Pour tout client identifié, jusqu'au plafond de l'établissement (`merchant_parameters.customer_account_credit_limit`, 0 = désactivée, défaut). |
| Remboursement du solde | Manuel, tracé, motif obligatoire, droit `pos.refund`. Pas d'expiration du solde. |
| Abandon de créance | Possible (dette seulement), motif obligatoire, droit `pos.refund`. |
| Facture récapitulative | Non en v1 : chaque commande a sa facture (moyen « Compte client »). |
| Paiement mixte | Oui : une commande se règle en partie sur le compte, le reste autrement. |
| Remboursement d'une commande réglée sur le compte | Recrédite le compte (paiement `ACCOUNT` négatif), sans argent. |
| Périmètre | Compte propre à chaque établissement ; caisse uniquement (ni borne ni ScanNOrder). |

## Fiscalité (sources officielles)

- **Versement = bon à usages multiples.** CGI art. 256 ter : un bon dont le
  taux de TVA n'est pas connu à l'émission (crédit utilisable sur des
  produits à 10 % et à 20 %) est un BUM ; son versement est hors champ, la TVA
  est due à la remise des biens ou à la prestation. BOFiP
  BOI-TVA-CHAMP-10-10-40-50 §50 et §80 ; §90 : un BUM non utilisé n'est pas
  taxé. D'où : un versement n'a ni ticket, ni TVA, ni chiffre d'affaires ; un
  remboursement de solde n'a aucune TVA à régulariser.
- **Risque.** Un versement lié à une opération déterminée serait un acompte,
  taxable à l'encaissement (CGI art. 269, 2-a, acomptes sur livraisons de biens
  depuis le 1er janvier 2023 ; 2-c pour les services). Le compte client est
  volontairement un crédit libre, sans engagement d'achat. À faire confirmer
  par l'expert-comptable de chaque établissement.
- **Ardoise.** Vente à paiement différé : la commande est une vente normale, sa
  TVA est comptée à la commande (lecture prudente : la vente à emporter et la
  livraison sont exigibles à la livraison, CGI art. 269, 2-a ; la vente sur
  place l'est en principe à l'encaissement, 2-c, sauf option pour les débits —
  la compter plus tôt ne fait qu'avancer la TVA). Une dette jamais payée se
  régularise selon CGI art. 272 (créance définitivement irrécouvrable,
  facture rectifiée) : travail du comptable, l'abandon de créance de la caisse
  en garde la trace.
- **Espèces.** Un versement en espèces est limité à 1 000 € (code monétaire et
  financier, art. L112-6 et D112-3 ; service-public.gouv.fr, fiche F10999).
- **Caisse sécurisée.** Le BOI-TVA-DECLA-30-10-30 impose d'enregistrer et
  d'inaltérer toutes les données de règlement : les mouvements de compte sont
  scellés et chaînés (chaîne `customer_account_operations`), archivés et
  vérifiés comme les paiements.
- Les articles 269, 272 et 289 du CGI sont repris dans le code des impositions
  sur les biens et services au 1er janvier 2027 (ordonnance n° 2025-1247) :
  à revoir alors, sans changement de fond attendu.

## Modèle

Le solde n'est stocké nulle part :

```
solde = Σ customer_account_operations.amount − Σ payments.amount (mop = 'ACCOUNT', actifs)
```

- `customer_account_operations` : `DEPOSIT` (> 0), `REFUND` (< 0), `WRITE_OFF`
  (> 0, sans moyen). Scellée et chaînée par établissement, jamais modifiée :
  une erreur se corrige par une opération inverse.
- `payments.customer_id` : client débité par un paiement `ACCOUNT`, scellé avec
  le paiement (champ `customer_id` de la charge, absent pour les autres
  paiements : leur empreinte ne change pas).
- Verrou consultatif par client (`internal/customeraccount.Lock`) : deux
  paiements simultanés ne dépassent pas à eux deux le solde disponible
  (avance + plafond).

## Effets

| Où | Versement / remboursement | Commande réglée sur le compte |
|---|---|---|
| Ticket, CA, TVA | rien | vente normale, moyen « Compte client » |
| Z du registre | ligne `ACCOUNT_IN_<moyen>` / `ACCOUNT_OUT_<moyen>`, dans le théorique ; espèces dans le fond de caisse final | ligne `ACCOUNT`, hors théorique (`account_payments` du résumé, comme les remises) |
| Clôture fiscale journalière | `account_operations` (absent sans mouvement) | `payments_by_mop`, moyen `ACCOUNT` |
| Rapport comptable (PDF) | bloc « Comptes clients » : versements, remboursements, abandons, variation des soldes | dans la TVA ; ligne du bloc |
| Écriture comptable (Pennylane) | débit moyen / crédit `payment_accounts.ACCOUNT` | débit `payment_accounts.ACCOUNT` |
| Archive fiscale | `comptes_clients.csv`, colonne `comptes_clients_json` des clôtures | colonne `client_compte` de `paiements.csv` |

Sans compte `ACCOUNT` (ni `WRITE_OFF`) dans la correspondance comptable, le
compte d'écart les absorbe : jamais le compte par défaut des encaissements.

**Écart TVA / encaissements.** Il existe et il est justifié : ventes −
argent encaissé = − (variation des soldes clients) + abandons. Le bloc du
rapport comptable le montre.

## API

| Route | Droit | Rôle |
|---|---|---|
| `GET /customer-accounts` | — | comptes non soldés (dettes d'abord) |
| `GET /customer-accounts/{customer_id}` | — | solde, plafond, disponible, historique (mouvements et commandes, solde après chaque ligne) |
| `POST /customer-accounts/{customer_id}/deposit` | — | `{device_id, amount, mop, comment}` ; registre ouvert requis |
| `POST /customer-accounts/{customer_id}/refund` | `pos.refund` | idem, motif obligatoire, ≤ solde créditeur |
| `POST /customer-accounts/{customer_id}/write-off` | `pos.refund` | `{amount, comment}`, ≤ dette |
| `GET` / `PUT /customer-accounts/settings` | `PUT` : `settings.manage` | `{credit_limit}` (centimes) |
| `POST /orders/{id}/payments/create` | — | `mop: "ACCOUNT"` : commande avec client identifié, montant ≤ disponible. `customer_id` (facultatif) : client choisi à la caisse pour une commande **sans** client, rattaché à la commande dans la même transaction (`customeraccount.AttachOrderCustomer`) ; refus `account_order_has_other_customer` si elle en a déjà un autre, `account_customer_required` si le client n'est pas un client actif de l'établissement ou si la commande vient d'une plateforme |

Moyens d'un versement ou d'un remboursement : `ES`, `CB`, `CHEQUE`,
`TRANSFER`. Erreurs (`message` en français pour la caisse) :
`account_customer_required`, `account_insufficient_balance`,
`account_refund_exceeds_balance`, `account_write_off_exceeds_debt`,
`account_invalid_mop`, `account_invalid_amount`, `account_cash_limit`,
`account_reason_required`, `no_cash_register_opened`.

## Lot 2 (2026-11-18, migration 221)

Décisions d'Ilies : réglage des espèces par établissement, plafond par
client, badge de solde, balance âgée, relevé, « Sur compte » annoncé,
tableau mensuel, alerte de plafond, relances (email, SMS en option) et lien de
paiement en ligne (établissements avec Stripe). Écartés :
`docs/customer-accounts-travaux-futurs.md`.

| Sujet | Mise en œuvre |
|---|---|
| Espèces au-delà de 1 000 € | `merchant_parameters.customer_account_cash_mode` : `BLOCK` (défaut, refus) ou `WARN` (accepté jusqu'à 15 000 € si la caisse envoie `non_resident_confirmed` ; mention ajoutée au motif scellé). CMF D112-3. |
| Plafond par client | `customer_account_settings` : plafond propre (NULL = celui de l'établissement), ardoise interdite (avance utilisable) et `reminders_disabled` (aucune relance automatique, la relance manuelle reste possible). `CheckDebit` prend le plafond effectif ; refus `account_credit_blocked`. |
| Badge / filtre | `account_balance` dans `/customers/search`, `/frequent`, `/list` ; `/customers/list?account=debt|credit`. |
| Balance âgée | `GET /customer-accounts/aging` (`reports.financial.read`) : chaque crédit solde d'abord les dettes les plus anciennes (`customeraccount.AgeDebts`). |
| Totaux mensuels | `GET /customer-accounts/monthly?year=` (`reports.financial.read`) : versements, consommé, remboursé, abandonné, variation, avances (4191) et ardoises (411) de fin de mois. |
| Relevé | `GET /customer-accounts/{id}/statement?from=&to=` (PDF) et `POST …/statement/send` (email avec PDF ; défaut : 3 derniers mois). |
| « Sur compte » annoncé | `expected_payment.mop = ACCOUNT` accepté ; « Terminer » une tournée règle la commande sur le compte. |
| Alerte de plafond | notification `CUSTOMER_ACCOUNT_LIMIT` de `GET /users/notifications` (≥ 80 % du plafond ou au-delà), calculée à la volée. |
| Relances | tâche `5 * * * *`, 10 h heure locale : première relance quand la plus ancienne dette a N jours, puis tous les N jours, au plus M fois pour cette dette ; email (`customer_account_reminder.html`), SMS si activé (`merchantsms.SendTransactional`, SMS activés et crédits de l'établissement). `POST /customer-accounts/{id}/remind` (`customers.manage`). Journal : `customer_account_reminders`. |
| Lien de paiement | `POST /customer-accounts/{id}/payment-link` ; page publique `GET /pay/account/{token}` : relit la dette, crée une session Stripe Checkout sur le compte Connect (frais ScanNOrder de l'établissement s'il en a), redirige. Webhook `checkout.session.completed` avec `metadata.channel = customer_account` : versement `STRIPE`, sans registre, une seule fois par session (`customer_account_online_payments`). Les autres événements de ces paiements sont ignorés. |

Configuration : `PUBLIC_API_BASE_URL` (adresse publique de l'API, base des
liens de paiement ; défaut staging, signalé au démarrage).

Remboursement d'un paiement en ligne : décidé dans le tableau de bord du
prestataire de paiement, il est répercuté automatiquement. Le webhook
`charge.refunded` d'un paiement `channel = customer_account` écrit un
`REFUND` négatif (moyen `STRIPE`, « Remboursement en ligne », sans registre),
pour la part du cumul remboursé pas encore répercutée
(`customer_account_online_payments.refunded_amount`) : rejouable, partiel
possible, jamais plus que le montant payé. L'argent étant déjà rendu, aucun
plafond ne s'applique à ce mouvement : si le client avait déjà consommé son
avance (ou si une ardoise réglée en ligne est remboursée), le solde passe
sous le plafond, même à 0 ou « ardoise interdite ». Les nouvelles commandes
sur compte sont alors refusées et l'alerte de plafond (cloche du BO) le
signale. Un événement reçu avant le versement correspondant renvoie une
erreur (le prestataire le rejoue).

Limites connues : les versements en carte saisis en caisse ne sont pas liés
à un paiement en ligne (remboursement : « Rembourser le solde ») ; un
remboursement qui échoue plus tard côté banque n'est pas annulé sur le
compte ; la page de paiement est servie par l'API (HTML minimal).

## Non fait

- Remboursement carte automatique (l'opérateur rembourse sur le TPE ; la
  caisse enregistre).
- Droit dédié à l'ardoise (le plafond d'établissement en tient lieu).
- Tests d'intégration Postgres (verrou, chaîne, Z, relances, webhook) : à
  écrire et lancer sur la base de test.
