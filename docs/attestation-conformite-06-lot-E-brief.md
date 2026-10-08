# PROMPT — Conformité caisse, lot E : contrôle d'intégrité et numéro fiscal sur le ticket

**Date :** 2026-10-08
**Constats :** C4 (aucun outil ne vérifie les chaînes : « détecter et démontrer », BOI §100) et C8 (le numéro fiscal du ticket n'apparaît pas sur le ticket remis au client) de l'[audit](attestation-conformite-00-audit.md).
**Plan :** [attestation-conformite-04-plan-D-E-F.md](attestation-conformite-04-plan-D-E-F.md), section « Lot E ».
**Dépend de :** lots A à D (chaînes v2, clôtures, réouverture encadrée, archives).

## Règle d'or

Comme pour les lots précédents :
- aucune donnée existante n'est modifiée ; le contrôle est en lecture seule ;
- les caisses actuelles continuent de fonctionner : seules des routes et des champs sont ajoutés ;
- performances mesurées à chaque phase.

## Décisions retenues

Ilies a donné son go pour enchaîner D puis E sans arrêt. Les questions E1, E3 et E4 du plan n'ont pas eu de réponse explicite : la recommandation du plan est retenue.

| # | Question | Retenu |
|---|---|---|
| E1 | Exposition du ticket : champ dans la commande, ou route dédiée | **Route dédiée** `GET /orders/{id}/receipt` : les listes de commandes, chargées souvent par la caisse, ne s'alourdissent pas. |
| E2 | Ticket de la borne | Décision d'Ilies : **ticket simple** (lignes, TVA, date de commande), **sans numéro fiscal**. Le ticket fiscal reste celui de la caisse. |
| E3 | Lien vers le ticket fiscal dans le mail ScanNOrder | **Reporté** (hors caisse physique). |
| E4 | Contrôle d'intégrité au back-office dès E | **Oui**, s'il tient dans le lot. |

## Phases

| Phase | Contenu |
|---|---|
| 0 | Recensement (ci-dessous) |
| 1 | Contrôle d'intégrité : paquet `internal/fiscalverify` et commande `cmd/verify_fiscal` |
| 2 | Ticket fiscal exposé par l'API (`GET /orders/{id}/receipt`) |
| 3 | Caisse Flutter : impression du ticket figé (numéro, TVA ventilée) pour une commande close, « note » pour une commande ouverte |
| 4 | Contrôle d'intégrité au back-office (E4) |
| 5 | Mesures, documentation |

## Journal

### Phase 0 — Recensement (2026-10-08, depuis le code)

**Chaînes et reconstruction des charges scellées.** Chaque chaîne v2 se recalcule depuis la base avec les fonctions qui l'ont écrite :

| Chaîne | Écrite par | Charge reconstruite par | Ordre des maillons |
|---|---|---|---|
| `payments` | `addPaymentAndReturnID` | `fiscal.NewPaymentPayload` (colonnes du paiement) | `payment_date`, `payment_id` |
| `receipts` | `receipt.sealReceipt` | `fiscal.NewReceiptPayload` (en-tête, TVA, détail, paiements) | `created_at`, `receipt_number` |
| `cash_registers` | `CloseCashRegister` | `fiscal.LoadCashRegisterClosure` (registre et lignes du Z) | `end_date` |
| `audit_logs` | `fiscal.AppendAuditLogs` | `fiscal.NewAuditLogPayload` | `created_at`, `id` |
| `fiscal_closures` | `fiscal.insertClosure` | `fiscal.LoadClosure` (prévu pour cela au lot B) | `closed_at`, `id` |
| `fiscal_archives` | `fiscalarchive.Generate` | `fiscalarchive.PayloadOf` | `generated_at`, `id` |
| `orders` | lot A, retirée au lot B | — (chaînage seul) | date de clôture |

- **Signature** : HMAC de l'empreinte par `FISCAL_SIGNING_KEY` (`security.SignHash`), vérifiable pour toute ligne v2.
- **Lignes v1** (antérieures au lot A) : leur formule ne couvrait pas tout le §50 et n'avait pas de signature à clé. Elles sont comptées et leur chaînage est contrôlé, en avertissements. Aucune n'est recalculée.

**Clôtures.**
- `fiscal.ComputeDayClosure` a été écrit au lot B pour servir aussi à la vérification : une clôture journalière se recalcule depuis les tickets, les paiements et les commandes du jour.
- Deux fonctions internes sont exposées sans changer leur comportement : `fiscal.ComputeOpening` (valeur d'ouverture) et `fiscal.ComputeAggregateClosure` (mois et année, extraite de `closeAggregate`, qui l'appelle).
- **Écart légitime** : un paiement d'une commande encore ouverte à minuit peut être annulé le lendemain (lot C : commande ouverte, registre ouvert). Les paiements du jour recalculés diffèrent alors de la clôture par le seul état actif / annulé. C'est un avertissement, pas une erreur ; l'annulation est tracée au journal.

**Ticket et commande.**
- Le TTC du ticket est le prix de la commande (`order.TTC`).
- Une vente close (`brand_status = 'CLOSED'` en caisse, statut de la plateforme sinon) a son ticket, émis dans la transaction de clôture (lot A).
- Une commande annulée après sa vente a l'avoir de cette vente (lot C).
- Recoupement possible : dernier ticket de vente égal au prix, somme des tickets entre 0 et le prix, somme nulle pour une commande annulée.

**Début de la version attestée.** Premier ticket à empreinte v2 de l'établissement. En production, lots A à F partent ensemble : c'est la date de la mise en production. Les anomalies d'une commande close avant cette date sont des avertissements.

**Ticket côté caisse (C8).** La caisse imprime à partir de la commande, sur l'appareil. Aucune route n'expose le ticket fiscal : `GetReceiptByOrderID` / `GetSaleReceiptByOrderID` existent dans le service `receipt`, sans route. Détail en phase 2.

### Phase 1 — Contrôle d'intégrité (2026-10-08)

**Paquet `internal/fiscalverify`** ([report.go](../internal/fiscalverify/report.go), [verify.go](../internal/fiscalverify/verify.go), [chains.go](../internal/fiscalverify/chains.go), [closures.go](../internal/fiscalverify/closures.go), [crosscheck.go](../internal/fiscalverify/crosscheck.go)) : `fiscalverify.Run(ctx, db, Options{MerchantID, From, To, Archives})`. Toutes les lectures se font dans une transaction en lecture seule, `REPEATABLE READ`, donc sur un seul instantané.

| Contrôle | Ce qui est vérifié | Gravité |
|---|---|---|
| `paiements`, `tickets`, `registres`, `journal`, `clotures_chaine`, `archives` | Pour chaque maillon v2 de la période : empreinte recalculée à l'identique, signature valide. Pour tous les maillons : empreinte unique, pas de fourche, parent présent dans la table (même avant la période), pas de redémarrage (parent `GENESIS` ailleurs qu'au premier maillon). | Erreur (v2), avertissement (v1) |
| `archives` (fichiers) | Avec un accès au stockage : empreinte et taille du fichier égales à la ligne scellée, puis contrôle croisé `fiscalarchive.Verify` (lot D). | Erreur |
| `commandes_chaine` | Ancienne chaîne des commandes : chaînage seul. | Avertissement |
| `tickets_lignes` | TTC du ticket v2 égal à sa TVA ventilée. Un écart vient du prix calculé par la caisse ; il est déjà journalisé à l'émission (lot D). | Avertissement |
| `numerotation` | Chaque année touchée par la période : numéros `F-AAAA-NNNNNN` à partir de 000001, sans trou ni doublon, bien formés. | Erreur (v2), avertissement (v1) |
| `clotures_jour` | Voir ci-dessous. | Erreur, sauf paiement annulé plus tard (avertissement) |
| `clotures_mois_annee` | Chaque clôture mensuelle et annuelle de la période égale la somme de ses jours (recalculée). Un mois ou une année dont le dernier jour est clos a sa clôture. | Erreur |
| `commandes_tickets` | Chaque commande close de la période. Vente : un ticket au moins, le dernier égal au prix, somme des tickets entre 0 et le prix. Commande annulée ou refusée : somme nulle. | Erreur si close depuis le début de la version attestée, avertissement avant |

`clotures_jour` vérifie, pour chaque clôture journalière de la période :
- aucun jour ne manque, entre deux clôtures ni jusqu'à l'avant-veille (la veille se clôt après 3 h) ;
- la clôture se recalcule depuis les données :
  - tickets du jour : ventes, avoirs, TVA, canaux, nombre, premier et dernier numéro ;
  - empreinte de chaque commande close du jour ;
  - paiements du jour ;
- les cumuls (total perpétuel, grand total de l'année) suivent la clôture précédente, ou la valeur d'ouverture recalculée pour la première.

**Rapport** : en clair (français) ou en JSON.
- Il contient : un tableau par contrôle (éléments contrôlés, erreurs, avertissements), le verdict, et les 20 premières anomalies de chaque contrôle (toutes sont comptées).
- **Conforme = aucune erreur.** Les avertissements portent sur l'historique, ou sur des écarts connus et tracés.

**Commande `cmd/verify_fiscal`** ([main.go](../cmd/verify_fiscal/main.go)) :
- `--merchant=<id>` (rapport complet) ou `--all` (une ligne par établissement actif) ;
- `--from` / `--to` : jours locaux inclus. Par défaut, depuis le début jusqu'à aujourd'hui ;
- `--json=<fichier>` ;
- relit les fichiers d'archive si les variables `R2_*` du bucket privé sont présentes (`--skip-archive-files` pour s'en passer) ;
- exige `POSTGRES_URL` et `FISCAL_SIGNING_KEY` ;
- code de sortie : 0 sans erreur, 1 avec erreur, 2 en cas d'échec.

**Tests** (verts sur staging) :
- `TestVerify_Postgres`. L'établissement synthétique est scellé par les fonctions réelles : 3 commandes dont une annulée après sa vente, 4 tickets dont un avoir, 2 paiements, un registre fermé, une entrée de journal, 3 jours clos, une archive.
  1. **Données intactes** : ni erreur ni avertissement, sur la période de septembre comme sur le jour courant (journal, chaîne des clôtures, archive). Nombres d'éléments contrôlés vérifiés ; verdict « CONFORME ».
  2. **Paiement annulé après la clôture de son jour** : avertissement seul.
  3. **Montant d'un paiement modifié** : empreinte recalculée différente, paiements du jour différents de la clôture.
  4. **Ligne d'une commande scellée modifiée** : commande citée comme modifiée après scellement.
  5. **Fichier d'archive altéré dans le stockage** : écart avec l'empreinte scellée.
  6. **Ticket supprimé** : parent introuvable dans la chaîne, numéro manquant, tickets du jour différents de la clôture, vente sans ticket. Verdict non conforme.
- `TestVerifyPerf_Postgres` (`FISCAL_PERF=1`) : voir les mesures.
- Suites `fiscal` et `fiscalarchive` vertes après l'exposition de `ComputeOpening` et `ComputeAggregateClosure`.

**Mesures** (depuis le poste, base de staging distante ; jeu généré, un mois très chargé) :

| Période | Contenu | Durée |
|---|---|---|
| Septembre | 3 000 tickets, 3 000 paiements, 3 000 commandes, 29 clôtures recalculées, numérotation | **5,7 s** |
| Jour courant | 12 000 entrées de journal de ~7 Ko (84 Mo à transférer), 29 maillons de clôture | **13,9 s**, dominé par le transfert |

- La mémoire reste bornée : le journal est lu en flux, seuls les maillons (empreintes) sont gardés.
- Sur Render, au plus près de la base, ces durées baissent fortement. Une vérification annuelle complète reste une opération à la demande, de l'ordre de la minute pour un établissement très actif.

**Préalable à la première attestation** (plan, rappel) : faire passer le contrôle sans erreur sur les données réelles, donc **en production**, juste après la mise en production unique.
- Les données antérieures produiront des avertissements (lignes v1, commandes historiques). Ils sont attendus.
- Les erreurs, elles, sont à analyser avant toute attestation.

### Phase 2 — Ticket fiscal exposé par l'API (2026-10-08)

**Route `GET /orders/{order_id}/receipt`** ([internal/modules/receipt/printable.go](../internal/modules/receipt/printable.go), handler `GetReceipt` d'`order_life_cycle`) : les tickets figés de la commande, tels que scellés.
- Elle est placée dans le groupe `/orders` à côté de `GET /orders/{order_id}/payments`, avec la même garde (authentification seule). Une commande d'un autre établissement donne une 404.
- Une requête : la commande et ses tickets, par la clé primaire et l'index des tickets par commande (migration 172).

**Réponse :**

| Champ | Contenu |
|---|---|
| `closed` | Commande close. Ouverte, elle n'a pas de ticket : la caisse imprime une note. |
| `current_receipt_number` | Ticket de vente **en vigueur** : le dernier, sauf si ses avoirs l'annulent en totalité (commande annulée). Un remboursement partiel le laisse en vigueur. Nul sans vente en vigueur. |
| `software` | « WelloResto 2.0.0 » (`internal/version`), à imprimer en pied de ticket. |
| `receipts[]` | Tickets et avoirs, du plus ancien au plus récent. Pour chacun : numéro ; type (`SALE` / `REFUND`) ; date ; TTC, HT, TVA ; remise de caisse ; format complet ou non ; lignes (nature, libellé, quantité, prix unitaire, taux, total, rang de l'article parent) ; TVA par taux ; paiements figés ; empreinte. |

- **Ticket antérieur au lot D** (format historique) : libellé, quantité et prix unitaire seulement. Son taux est ambigu dans les données les plus anciennes (montant de TVA stocké dans le taux), il n'est donc pas exposé.
- **Compatibilité** : route nouvelle. Les caisses actuelles ne l'appellent pas.

**Tests** :
- `TestBuildOrderReceipts` (unitaire) : vente en vigueur ; vente annulée par son avoir ; avoir puis nouvelle vente ; remboursement partiel ; ticket antérieur ; commande ouverte.
- `TestGetOrderReceipts_Postgres` : requête réelle, avec vente, avoir et revente ; commande ouverte ; autre établissement (404).
- Les suites `receipt` et `order_life_cycle` restent vertes.

### Phase 3 — Caisse Flutter : ticket fiscal imprimé (2026-10-08)

Dépôt `wello_resto_flutter`, branche `conformite-caisse-lot-c` (celle du lot C). Détail : `docs/decisions.md` du dépôt, entrée du 2026-10-08 « ticket fiscal imprimé ».

- **Impression manuelle d'une commande close** (`PrinterController.printInvoice`, dialogue d'impression) :
  - la caisse appelle `GET /orders/{id}/receipt` puis imprime le ticket de vente en vigueur ;
  - contenu : titre `TICKET N°F-AAAA-NNNNNN` ; lignes figées, avec les options et suppléments sous leur article ; TVA par taux et HT ; paiements figés ; numéro de commande, logiciel et version, début de l'empreinte.
- **Tout autre document** porte « Note - ne vaut pas ticket de caisse » à la place de l'ancien « Document provisoire », et aucun numéro fiscal. Cela vaut pour :
  - une commande ouverte ;
  - l'impression automatique à la réception ou en livraison ;
  - un montant saisi à la main ;
  - une API injoignable : en cas d'échec de l'appel, la caisse imprime la note, jamais un faux ticket.
- `ReceiptBytesBuilder.buildOrderInvoice` reçoit deux paramètres facultatifs (`fiscalReceipt`, `software`). Les autres appelants sont inchangés.
- **Borne** : inchangée. Elle garde son ticket simple sans numéro fiscal (décision d'Ilies, E2).
- **Tests** :
  - `test/data/services/printer/fiscal_receipt_print_test.dart` : lecture de la réponse de l'API ; ticket fiscal imprimé (numéro, option, TVA 10 %, logiciel, empreinte, commande) ; note sans ticket ;
  - suite complète verte (42 tests) ;
  - `flutter analyze` : aucune alerte nouvelle.
- **Non fait, à reprendre** : l'impression d'un **avoir** depuis la caisse. L'API l'expose (`receipts[]`, type `REFUND`), mais la caisse n'imprime que le ticket de vente en vigueur. À ajouter à l'écran de remboursement si Ilies le souhaite.
- **Coût côté caisse** : un appel de plus, seulement à l'impression manuelle d'une commande close.

### Phase 4 — Contrôle d'intégrité au back-office (2026-10-08)

**Route `POST /accounting/fiscal-integrity`** ([internal/modules/pos/accounting/fiscal_integrity.go](../internal/modules/pos/accounting/fiscal_integrity.go)), dans le groupe `/accounting` sous `reports.financial.read`, comme les archives.
- Même contrôle que `cmd/verify_fiscal`, sur l'établissement du jeton.
- Les fichiers d'archive sont relus si le client R2 privé est disponible.
- **31 jours au plus** : le contrôle s'exécute dans la requête. Les contrôles plus longs passent par la commande.
- **Un contrôle à la fois par établissement** (`fiscal.TryLock`), sinon refus `fiscal_integrity_busy` (409, message en français).
- Réponse : le rapport structuré et sa version texte.

**Back-office** (`wello-back-office`) : carte « Contrôle d'intégrité » sur la page Archives fiscales ([src/components/fiscal/FiscalIntegrityCard.tsx](../../wello-back-office/src/components/fiscal/FiscalIntegrityCard.tsx)). Elle affiche :
- une période (31 jours au plus) et le bouton « Lancer le contrôle » ;
- le verdict (Conforme / Non conforme) ;
- le tableau des contrôles, les anomalies et l'explication des avertissements ;
- le bouton « Télécharger le rapport » (texte).

Type-check, lint et build Vite propres.

**Tests** : `TestFiscalArchivesRoutes_Postgres` complété avec un contrôle sans erreur (et son texte « CONFORME »), une période de 32 jours refusée et un contrôle déjà en cours refusé.

**Documentation des droits** : [RBAC_ROUTES.md](RBAC_ROUTES.md) (`/accounting/fiscal-integrity`, `/orders/{order_id}/receipt`).

### Phase 5 — Mesures (2026-10-08)

| Mesure | Résultat |
|---|---|
| Contrôle d'un mois très chargé (3 000 tickets, paiements, commandes ; 29 clôtures recalculées) | **5,7 s** depuis le poste |
| Contrôle de 12 000 entrées de journal de ~7 Ko | **13,9 s** depuis le poste, dominé par le transfert de 84 Mo |
| `GET /orders/{id}/receipt` | une requête, deux parcours d'index, ~3 ms côté base (plan sur staging, structure seulement) |
| Caisse | encaissement, clôture et réouverture **inchangés** ; un appel de plus à l'impression manuelle d'une commande close |

Aucun endpoint existant de la caisse n'est modifié par le lot E.

## Bilan du lot E (2026-10-08)

**Constat C4 traité** : contrôle d'intégrité complet, lancé par l'éditeur (`cmd/verify_fiscal`) ou par le restaurateur (back-office). Il porte sur :
- les chaînes : empreinte, signature, chaînage ;
- la numérotation des tickets ;
- les clôtures, recalculées depuis les données, avec leurs cumuls ;
- le recoupement des commandes avec leurs tickets ;
- les archives, fichiers compris.

**Constat C8 traité** :
- le ticket fiscal figé est exposé par l'API et imprimé par la caisse avec son numéro, sa TVA ventilée, le logiciel et son empreinte ;
- tout autre document porte « ne vaut pas ticket de caisse » ;
- la borne garde un ticket simple sans numéro fiscal.

**Aucune migration.**

**Première attestation possible** (feuille de route) : tous les constats bloquants C1 à C8 sont traités. Préalable : passer le contrôle sans erreur sur les données de production, juste après la mise en production unique.

**Reste à faire, hors lot :**
- impression d'un avoir depuis la caisse ;
- lien vers le ticket fiscal dans le mail ScanNOrder (E3, reporté) ;
- les points déjà notés aux lots C et D.
