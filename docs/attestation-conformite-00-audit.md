# Attestation de conformité caisse — Phase 0 : audit des quatre conditions

**Date :** 2026-10-06
**Référentiel :** [BOI-TVA-DECLA-30-10-30-20260325](https://bofip.impots.gouv.fr/bofip/10691-PGP.html/identifiant=BOI-TVA-DECLA-30-10-30-20260325), §50 à §260 (inaltérabilité, sécurisation, conservation, archivage).
**Contexte :** l'attestation individuelle de l'éditeur est de nouveau admise depuis le 21/02/2026 (loi n° 2026-103, art. 125), selon le modèle BOI-LETTRE-000242. Elle engage la responsabilité pénale du signataire (C. pén., art. 441-1) : on ne l'émet que si le logiciel respecte réellement les quatre conditions.
**Périmètre audité :** API (`ib-welloresto-api`) et caisse Flutter (`wello_resto_flutter`, impression du ticket). Audit limité aux exigences du BOI, sans amélioration hors conformité.
**Méthode :** lecture du code. Les données de staging ne sont pas utilisées (non représentatives).

## Verdict

**Non conforme en l'état. Aucune attestation ne doit être émise avant les corrections C1 à C8.**

Le socle existe : chaînage avec signature à clé sur quatre tables, avoirs en opérations négatives, données conservées ligne par ligne. Mais des fonctions de la caisse modifient directement des données enregistrées. Les empreintes ne couvrent pas le détail des tickets. Rien ne permet de vérifier les chaînes. Enfin, les clôtures mensuelles et annuelles, les totaux cumulés et l'archivage n'existent pas.

## Points à corriger

### C1 — Réouverture d'une commande clôturée · Bloquant
**Exigences :** §90 (corrections par opérations de « plus » et de « moins »), §100 (aucune fonction ne doit permettre de modifier les données).

**Constat :**
- `PATCH /orders/{order_id}/reopen` ([routes.go:1617](../cmd/api/routes.go#L1617)) appelle `ReopenClosedOrder` ([order_life_cycle/repository.go:60](../internal/modules/order_life_cycle/repository.go#L60)), qui fait `UPDATE orders SET state = 'OPEN'` sur une commande déjà encaissée et munie de son ticket fiscal.
- La commande redevient alors modifiable :
  - `UpdateOrder` réécrit le prix et les lignes, et supprime des lignes avec `DELETE FROM orderitems` ([repository.go:2075](../internal/modules/order_life_cycle/repository.go#L2075)) ;
  - les paiements peuvent être désactivés (voir C2).
- La reclôture écrase `previous_hash`, `hash` et `signature` de la commande ([repository.go:992-1005](../internal/modules/order_life_cycle/repository.go#L992-L1005)). Elle émet un **second ticket de vente**, sans avoir qui annule le premier.

**Correction :** interdire la réouverture d'une commande clôturée. Le besoin métier (corriger une vente) passe par un avoir total chaîné sur le ticket d'origine, puis une nouvelle commande liée à l'ancienne.

### C2 — Paiements annulés par mise à jour directe · Bloquant
**Exigence :** §90 (une correction est une nouvelle opération, jamais une modification de la donnée d'origine).

**Constat :** les paiements enregistrés sont « annulés » par `UPDATE payments SET enabled = FALSE`. La colonne `enabled` n'entre pas dans l'empreinte, donc ces annulations sont invisibles pour la chaîne. Chemins concernés :
- `DisablePayment` ([repository.go:393](../internal/modules/order_life_cycle/repository.go#L393)), via la route `/{order_id}/payments`. Ce chemin ne passe pas par `ExecuteOrderMutation` : **aucune trace dans le journal d'audit** ([service.go:511-565](../internal/modules/order_life_cycle/service.go#L511-L565)).
- `DisablePayments` ([repository.go:1117](../internal/modules/order_life_cycle/repository.go#L1117)), appelé par l'annulation de commande ([service.go:186](../internal/modules/order_life_cycle/service.go#L186)) et par le refus ([service.go:837](../internal/modules/order_life_cycle/service.go#L837)).
- Remboursement Stripe reçu par webhook ([webhook/stripe/repository.go:500-509](../internal/webhook/stripe/repository.go#L500-L509)), y compris sur une commande déjà clôturée.
- Webhooks Uber Eats ([webhook/ubereats/repository/orders_repo.go:64-78](../internal/webhook/ubereats/repository/orders_repo.go#L64-L78)) et Deliveroo ([webhook/deliveroo_orders/repository.go:273-293](../internal/webhook/deliveroo_orders/repository.go#L273-L293)).

**Correction :** une annulation de paiement devient une nouvelle ligne de paiement négative, chaînée et signée, avec un `operation_type` dédié et une référence au paiement d'origine. On ne modifie plus jamais `enabled` sur un paiement enregistré.

### C3 — Empreintes qui ne couvrent pas les données du §50 · Bloquant
**Exigences :** §100 (preuve, à bas niveau, que la donnée n'a pas été modifiée), §130 et §140 (sécuriser les données de détail, les données de modification et les données qui servent à produire les justificatifs).

**Constat :**

| Table | Ce que l'empreinte couvre | Ce qu'elle ne couvre pas |
|---|---|---|
| `receipts` ([receipt/service.go:51](../internal/modules/receipt/service.go#L51), [:127](../internal/modules/receipt/service.go#L127)) | empreinte précédente, numéro, TTC, date | détail des articles (`items_snapshot`), paiements (`payments_snapshot`), `total_ht`, `tax_details`, `order_id` |
| `orders` ([order_life_cycle/repository.go:987](../internal/modules/order_life_cycle/repository.go#L987), [:868](../internal/modules/order_life_cycle/repository.go#L868), [:734](../internal/modules/order_life_cycle/repository.go#L734)) | empreinte précédente, date, prix, id | aucune ligne `orderitems`, HT/TVA, type de commande |
| `payments` ([order_life_cycle/repository.go:189](../internal/modules/order_life_cycle/repository.go#L189)) | empreinte précédente, date, montant, moyen de paiement, commande | `enabled`, `operation_type`, `cash_register_id`, utilisateur |
| `cash_registers` ([cash_registers/repository.go:466](../internal/modules/cash_registers/repository.go#L466)) | id, établissement, fond de caisse final, empreinte précédente | totaux du Z (TTC, HT, TVA, moyens de paiement) |
| `audit_logs` ([audit/repository.go:82](../internal/modules/audit/repository.go#L82)) | empreinte précédente, date, action, ressource, nouvelles valeurs | anciennes valeurs, utilisateur ; **aucune signature à clé** : quiconque a accès à la base peut recalculer toute la chaîne |

**Correction :** pour chaque enregistrement fiscal, calculer l'empreinte sur une sérialisation canonique de **toutes** les données du §50, puis la signer avec la clé. Ajouter la signature à clé sur `audit_logs`. Prévoir une colonne de version du schéma d'empreinte, pour que les lignes déjà écrites restent vérifiables avec l'ancienne formule.

### C4 — Aucun outil de vérification de l'intégrité · Bloquant
**Exigences :** §100 (« détecter et démontrer »), §120 (« fournir un système de preuve »).

**Constat :** aucune fonction ne rejoue les chaînes ni ne contrôle les signatures. Les chaînes existent, mais rien ne permet d'en tirer une preuve.

**Correction :** une fonction de contrôle d'intégrité par établissement et par période. Elle rejoue chaque chaîne (tickets, commandes, paiements, clôtures, journal d'audit), contrôle les signatures et la continuité de la numérotation, et produit un rapport. Elle doit pouvoir être lancée par le commerçant, ou par l'éditeur à la demande de l'administration.

### C5 — Chaînes et numérotation non protégées contre les accès concurrents · Bloquant
**Exigences :** §100 et §140 (intégrité de la chaîne), §50 (numéro du justificatif).

**Constat :**
- Le dernier maillon est lu par `ORDER BY … LIMIT 1 FOR UPDATE` ([receipt/repository.go:35](../internal/modules/receipt/repository.go#L35), [order_life_cycle/repository.go:178](../internal/modules/order_life_cycle/repository.go#L178), [:718](../internal/modules/order_life_cycle/repository.go#L718), [:857](../internal/modules/order_life_cycle/repository.go#L857), [:976](../internal/modules/order_life_cycle/repository.go#L976), [audit/repository.go:64](../internal/modules/audit/repository.go#L64)). En mode `READ COMMITTED` de Postgres, une seconde transaction concurrente attend le verrou, puis relit **la même ligne** : deux enregistrements chaînent sur le même parent, et deux tickets peuvent recevoir le **même numéro**.
- La chaîne `cash_registers` lit son maillon précédent sans aucun verrou ([cash_registers/repository.go:450](../internal/modules/cash_registers/repository.go#L450)).
- `receipts` n'a pas de contrainte d'unicité sur `(merchant_id, receipt_number)` (seule la clé primaire `receipt_id` existe).

**Correction :** sérialiser chaque chaîne par établissement, par exemple avec une ligne de séquence par établissement et par chaîne verrouillée en `FOR UPDATE`, ou un `pg_advisory_xact_lock`. Ajouter une contrainte d'unicité sur le numéro de ticket.

### C6 — Clôtures journalière, mensuelle et annuelle, grand total et total perpétuel · Bloquant
**Exigences :** §170 (trois clôtures « cumulatives et impératives » avec grand total de la période et total perpétuel), §180 et §260 (ces totaux sont conservés et jamais purgés).

**Constat :**
- Seul existe le Z d'un registre de caisse, ouvert et fermé à la main. Rien n'impose une clôture **journalière** : aucune tâche planifiée, et un registre peut rester ouvert plusieurs jours.
- Le registre est facultatif (`cash_register_required_for_ordering`, [order_life_cycle/repository.go:2368](../internal/modules/order_life_cycle/repository.go#L2368)). Les ventes faites sans registre ouvert (borne, Scan'n'Order, plateformes) n'entrent dans un Z que si un registre se ferme plus tard ([cash_registers/repository.go:380-407](../internal/modules/cash_registers/repository.go#L380-L407)).
- **Pas de clôture mensuelle ni annuelle.**
- **Pas de grand total de période ni de total perpétuel.**
- Les totaux du Z ne sont pas scellés (voir C3).

**Correction :** clôtures journalière, mensuelle et annuelle (ou par exercice) par établissement, sur son calendrier local :
- automatiques (tâche planifiée) et indépendantes des registres de caisse ;
- chaînées et signées ;
- chacune avec ses totaux TTC, HT et TVA par taux, ventes et avoirs, moyens de paiement, le grand total de la période et le total perpétuel. Le total perpétuel n'est jamais remis à zéro, y compris lors d'un changement de version.

### C7 — Archivage fiscal · Bloquant
**Exigences :** §220 (archivage au plus annuel, qui fige les données et leur donne date certaine), §230 (format ouvert et notice en français), §240 (traçabilité de la génération), §250 (l'éditeur doit fournir une fonction de génération d'archives).

**Constat :** aucune fonction d'archivage fiscal. Les exports comptables existants ([pos/accounting/exports_archive.go](../internal/modules/pos/accounting/exports_archive.go)) sont des rapports PDF agrégés : pas de données ligne par ligne, pas de format ouvert.

**Correction :** génération d'archives, au plus tard à chaque clôture annuelle (mensuelle conseillée) :
- contenu : données du §50 ligne par ligne, avoirs, traces de corrections, clôtures et totaux ;
- format ouvert (CSV ou JSON) avec une notice en français ;
- empreinte et signature de l'archive ;
- journal de génération conservé ;
- téléchargement par le commerçant, conservation 6 ans.

### C8 — Le ticket remis au client ne porte pas le numéro fiscal · Bloquant
**Exigences :** §50 (numéro du justificatif), §130 (données qui servent à produire les justificatifs émis).

**Constat :**
- La caisse Flutter construit le ticket sur l'appareil, à partir de la commande ([receipt_bytes_builder.dart:678](../../wello_resto_flutter/lib/data/services/printer/receipt_bytes_builder.dart#L678) : la référence imprimée est le libellé du type de commande).
- Le numéro fiscal `F-YYYY-NNNNNN` n'apparaît nulle part dans la caisse.
- Aucune route n'expose le reçu fiscal (`receipts`) à la caisse.

**Correction :**
- exposer le reçu fiscal, avec son numéro, à la caisse et au back-office ;
- imprimer le ticket client à partir de ce reçu figé, avec son numéro.

### C9 — Avoir sans ventilation de TVA · À corriger
**Exigence :** §50 (taux de TVA associé à chaque ligne).

**Constat :** `GenerateRefundReceipt` écrit une seule ligne avec `TaxRate = 0` et `TotalHT = TotalTTC` ([receipt/service.go:108-139](../internal/modules/receipt/service.go#L108-L139)).

**Correction :** ventiler l'avoir par taux, au prorata du ticket d'origine ou par lignes remboursées.

### C10 — Commandes Uber Eats et Deliveroo clôturées hors chaîne · À corriger
**Exigences :** §80 et §130.

**Constat :**
- Plusieurs chemins passent la commande en `state = 'CLOSED'` par un `UPDATE` direct, sans empreinte ni ticket :
  - [ubereats/repository.go:464-491](../internal/modules/ubereats/repository.go#L464-L491) et [:504-511](../internal/modules/ubereats/repository.go#L504-L511) ;
  - [webhook/ubereats/repository/orders_repo.go:49-56](../internal/webhook/ubereats/repository/orders_repo.go#L49-L56) et [:142-147](../internal/webhook/ubereats/repository/orders_repo.go#L142-L147) ;
  - [webhook/deliveroo_orders/repository.go:263-268](../internal/webhook/deliveroo_orders/repository.go#L263-L268).
- Pourtant, leurs paiements sont rattachés au Z ([cash_registers/repository.go:389](../internal/modules/cash_registers/repository.go#L389)).

**Correction :** faire passer toutes les clôtures par la clôture commune (empreinte et ticket ; annulation = opérations négatives). L'alternative, à décider, serait d'exclure explicitement les plateformes du périmètre attesté (champ « fonctionnalités exclues » de l'attestation).

### C11 — Numéro de caisse modifié après coup · À corriger
**Exigences :** §50 (numéro de la caisse), §90.

**Constat :** à la fermeture d'un registre, `UPDATE payments SET cash_register_id = …` réaffecte les paiements STRIPE, KIOSK, UBER_EATS et DELIVEROO déjà enregistrés ([cash_registers/repository.go:380-407](../internal/modules/cash_registers/repository.go#L380-L407)), sans trace.

**Correction :** fixer le numéro de caisse au moment de l'encaissement (une caisse virtuelle par canal : borne, web, plateforme). Le rattachement au Z se calcule, il ne s'écrit pas. Le point disparaît en grande partie avec C6.

## Ce qui est conforme

- **§50 conservé ligne par ligne :** les commandes, lignes, paiements et tickets restent en base. Aucune tâche ne purge les tables fiscales : seules `api_request_logs`, `password_resets` et `signup_sessions` sont purgées.
- **§90, remboursements :** paiement négatif et avoir chaîné rattaché au ticket d'origine (`ProcessRefund`), hors ventilation de TVA (C9).
- **§150 :** il n'existe aucun mode école ou test, l'exigence est donc sans objet.
- **§190 :** la caisse fonctionne entièrement en ligne, sans stockage local des ventes ; les données sont centralisées sur le serveur.
- **Commandes ouvertes :** leurs modifications sont tracées dans `audit_logs` (état avant et après, journal chaîné), sous réserve de la signature à clé (C3).
- **Commandes refusées :** elles sont désormais chaînées (test `deny_order_fiscal_chain`).
- **Clé de signature :** `FISCAL_SIGNING_KEY` est obligatoire au démarrage ([config.go:109](../internal/config/config.go#L109)).

## Prérequis de l'attestation (hors conformité technique)

- **Nom commercial et numéro de version** du logiciel attesté, avec une règle version majeure / mineure (BOI §330 et §315). L'API n'a aujourd'hui aucun numéro de version. Les corrections C1 à C8 touchent la sécurisation : elles constituent la version majeure à attester.
- **Périmètre :** caisse, borne, Scan'n'Order, plateformes (voir C10).

## Arbitrages (Ilies, 2026-10-06)

| # | Position d'Ilies | Retenu |
|---|---|---|
| C1 | Réouverture normale tant que le registre n'est pas clôturé | **Nécessaire, en version réduite.** On garde la réouverture, mais (a) le code l'interdit une fois le registre clôturé : aujourd'hui rien ne le vérifie, voir `// FUTURE VALIDATIONS HERE` dans `ReopenClosedOrder` ; (b) la réouverture émet automatiquement un avoir chaîné qui annule le ticket de vente. La reclôture émet ensuite un nouveau ticket. |
| C2 | L'annulation d'un paiement aide à l'encaissement (carte refusée) et n'est pas un remboursement ; registre clôturé = remboursement | **Nécessaire, en version réduite.** L'annulation reste possible tant que la commande et le registre sont ouverts. Elle est enregistrée dans un journal chaîné et signé des annulations, sans toucher au montant, au moyen de paiement ni à la date d'origine ; `enabled` reste un état dérivé. Après clôture, seul le remboursement est possible, y compris pour les webhooks Stripe, Uber Eats et Deliveroo. |
| C3 | D'accord | Nécessaire. Le `cash_register_id` reste hors de l'empreinte (voir C11). |
| C4 | Plus tard, à plus grande échelle | **Version minimale nécessaire avant la première attestation :** une commande interne (`cmd/`) qui rejoue les chaînes d'un établissement. L'interface viendra plus tard. |
| C5 | singleflight ? | **Non** : singleflight fusionne les appels simultanés (même numéro et même empreinte rendus à deux ventes) et ne vaut que dans un seul processus. Retenu : `pg_advisory_xact_lock` par établissement et par chaîne, plus une contrainte d'unicité sur le numéro de ticket. |
| C6 | Les rapports d'analyse montrent déjà les mois et les années ; le restaurateur est responsable de ses clôtures | **Nécessaire** : le §170 impose au logiciel des clôtures « cumulatives et impératives », intègres et inaltérables, avec grand total et total perpétuel. Un rapport recalculé ne le permet pas. Retenu : une tâche planifiée qui scelle automatiquement les clôtures journalière, mensuelle et annuelle, sans action du restaurateur. Le Z de registre reste tel quel. |
| C7 | Chantier en cours, prêt sur staging ? | Le chantier en cours est l'archivage des **exports comptables PDF** (EXPORT_COMPTABLE_MODES_CLOTURE.md, phase 3) : la table `accounting_exports` existe sur staging, aucun export n'y est encore généré (vérifié le 2026-10-06). Ce n'est pas l'archive fiscale du §230 : données agrégées, format non ouvert. **Nécessaire** : ajouter une archive ligne par ligne en CSV ou JSON avec une notice, en réutilisant la même infrastructure (R2, table, sha256, lien signé). |
| C8 | D'accord, numéro présent seulement une fois la commande clôturée | Conforme : le justificatif est le ticket final. |
| C9 | Plus tard | Reporté : c'est une erreur de contenu (TVA de l'avoir), pas une faille d'inaltérabilité. |
| C10 | À intégrer dans la chaîne fiscale | Nécessaire. |
| C11 | Normal, pour ne rien oublier : borne, Scan'n'Order, Uber Eats et Deliveroo hors registre | **Acceptable** : on n'affecte qu'une valeur provisoire, et la clôture journalière (C6) couvre tous les paiements quel que soit le registre. Condition : `cash_register_id` hors de l'empreinte (C3) ; le canal d'encaissement fait office de numéro de caisse. |

### Second tour (Ilies, 2026-10-06)

| # | Position d'Ilies | Retenu |
|---|---|---|
| C1 | Blocage si le registre est clôturé : oui. Pas d'avoir (les paiements ne sont pas touchés), pas de nouveau ticket à la reclôture | Blocage retenu. À la reclôture, on compare le **contenu de la vente** (lignes, quantités, prix, taux de TVA, TTC/HT) au dernier ticket : **identique → ni avoir ni nouveau ticket** ; **différent → avoir du ticket d'origine puis nouveau ticket** (sinon le ticket remis au client est faux). Les corrections de paiement ne déclenchent rien : elles sont tracées par la chaîne `payments` et le journal des annulations (C2). L'empreinte de la commande n'est plus réécrite à la reclôture. NB : aujourd'hui, chaque reclôture émet déjà un second ticket sans avoir (`DeliverOrder` → `HandlerFiscalReceiptGeneration`). |
| C2 | Les webhooks ne bougent pas : un remboursement Stripe vient de l'établissement, qui aura déjà fait l'avoir ; une annulation de plateforme est une annulation de paiement | Comportement des webhooks inchangé, avec deux garde-fous : (1) ils n'agissent que si la commande est **ouverte**. Aujourd'hui `charge.refunded` désactive le paiement d'origine même sur une commande clôturée, ce qui, après un avoir de l'établissement, retire la vente deux fois ; (2) quand ils annulent, ils passent par la même fonction tracée que C2. |
| C4 | Commande minimale | Retenu. |
| C5 | Au plus simple | `pg_advisory_xact_lock` par établissement et par chaîne (une fonction utilitaire) + contrainte d'unicité sur `(merchant_id, receipt_number)`. Vérifier l'absence de doublons existants avant d'appliquer la migration. |
| C6 | Seulement si rien ne change dans le fonctionnement actuel | Tâche planifiée qui lit les données et écrit dans de nouvelles tables : registres, Z, exports et rapports inchangés. |
| C7 | Génération automatique ? Ne pas confondre avec l'export comptable ; mêmes données ? Commande à 00:15 le 1er ? | Archive générée automatiquement à chaque clôture mensuelle, objet distinct de l'export comptable (seule l'infrastructure est partagée). Mêmes données que les clôtures fiscales par construction, **pas** que l'export comptable : périmètre (l'export exclut Uber Eats, Deliveroo et Scan'n'Order), date de référence (date de création de la commande pour l'export, date du ticket pour la clôture), mode manuel (réel du registre). Rattachement : jour calendaire de l'établissement de la **date du ticket** (voir réponse du 2026-10-06). |

## Suite

Une fois C1 à C8 corrigés (C9 à C11 recommandés), et le contrôle d'intégrité (C4) passé sans erreur sur des données réelles, on pourra rédiger le plan d'implémentation de la génération autonome des attestations (caisse et back-office).

## Journal

| Date | Lot | Constats | État |
|---|---|---|---|
| 2026-10-07 | A ([brief et journal](attestation-conformite-01-lot-A-brief.md), [decisions.md](decisions.md)) | C3, C5, C10 | **Traités** sur staging (migrations 168 et 169 appliquées). Production : migrations avant le déploiement du code, après la requête de contrôle des doublons de tickets. |
| 2026-10-07 | — | — | **Suite réorganisée** (décisions S1 à S7) : voir la [feuille de route](attestation-conformite-feuille-de-route.md). |
| 2026-10-07 | B ([brief et journal](attestation-conformite-02-lot-B-brief.md)) | C6, C9 | **Traités** : clôtures journalières, mensuelles et annuelles scellées (tâche horaire et rattrapage) ; commandes scellées par leur clôture journalière ; TVA ventilée sur les tickets et les avoirs. Migrations 170 et 171 appliquées sur staging. |
| 2026-10-08 | C ([brief et journal](attestation-conformite-03-lot-C-brief.md)) | C1, C2 | **Traités** : réouverture refusée sur commande scellée ou paiement dans un registre fermé, plus aucune réouverture implicite (acceptations, synchronisation Uber) ; reclôture sans ticket en double (avoir puis nouveau ticket si la vente change, avoir à l'annulation d'une commande rouverte) ; annulations de paiement par une fonction unique, tracées au journal d'audit, refusées hors commande et registre ouverts ; webhooks limités aux commandes ouvertes ; caisse Flutter (dialogues, `can_reopen`). Migration 172. Les 23 commandes annulées de staging qui portent un ticket sans avoir restent en l'état (données existantes jamais modifiées). |
| 2026-10-08 | D ([brief et journal](attestation-conformite-05-lot-D-brief.md)) | C7 | **Traité** : archive fiscale mensuelle automatique (rattrapage compris) et à la demande ; CSV ligne par ligne, notice, manifeste ; chaîne signée `fiscal_archives` ; R2 privé ; back-office (liste, empreinte, téléchargement tracé au journal d'audit) ; contrôle croisé tickets / clôtures à chaque génération. Ticket figé complet (options, suppléments, frais, remises), surcoût des options figé à la commande. Migrations 173 et 174 appliquées sur staging, 175 (index) à appliquer. |
| — | E | C4, C8 | À faire. La commande de vérification réutilisera les lectures de `internal/fiscal`. |
| — | F | — | Version du logiciel et génération autonome de l'attestation. |
