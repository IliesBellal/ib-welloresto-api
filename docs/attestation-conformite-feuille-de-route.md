# Conformité caisse et attestation : feuille de route

**Mise à jour :** 2026-10-08
**Point de départ :** [audit](attestation-conformite-00-audit.md) (constats C1 à C11) et [lot A](attestation-conformite-01-lot-A-brief.md) (C3, C5, C10), commité sur `staging` le 2026-10-07 (`c31fdfd` → `9c7225d`), **pas encore en production**. [Lot B](attestation-conformite-02-lot-B-brief.md) commité sur `staging` le 2026-10-07 (`4231e4a` → `053dcbf`, puis sa documentation), **pas encore en production**. [Lot C](attestation-conformite-03-lot-C-brief.md) terminé le 2026-10-08 (API et caisse Flutter), **non commité**.

## Décisions du 2026-10-07 qui réorganisent la suite

| # | Décision |
|---|---|
| S1 | **Commandes scellées par la clôture journalière** : une ligne scellée par établissement et par jour, qui contient l'empreinte de chaque commande de la journée. La chaîne par commande du lot A est retirée (les lignes déjà écrites restent intactes). Les tickets, avoirs, paiements et registres restent scellés au moment où ils sont écrits. |
| S2 | **Annulations de paiement inscrites au journal d'audit** (chaîné et signé depuis le lot A), par une fonction unique. Pas de nouvelle table. |
| S3 | **Clôtures mensuelles et annuelles calculées à partir des journalières** ; grand total de la période et total perpétuel tenus en cumul. |
| S4 | **Archive fiscale mensuelle** produite par le même passage de nuit. |
| S5 | **Jour calendaire de l'établissement** ; passage horaire qui clôt la veille dès 3 h, heure locale (aucun client ne sert après 23 h). |
| S6 | **Réouverture refusée** si la commande est déjà scellée **ou** si l'un de ses paiements est dans un registre fermé. Une commande rouverte au passage de nuit est scellée la nuit suivant sa reclôture. |
| S7 | **Rattachement des paiements borne et plateformes au premier registre fermé (C11) : inchangé** pour l'instant. |

## Lots restants

| Lot | Contenu | Constats | Dépend de | Taille |
|---|---|---|---|---|
| **B** — [brief](attestation-conformite-02-lot-B-brief.md) | Clôtures journalières, mensuelles et annuelles scellées ; scellement des commandes par la clôture journalière ; retrait de la chaîne par commande ; TVA ventilée sur les tickets et les avoirs | C6, C9, S1, S3, S5 | A | Grande |
| **C** — [brief](attestation-conformite-03-lot-C-brief.md) | Réouverture encadrée et reclôture (avoir et nouveau ticket si la vente change) ; annulations de paiement tracées ; webhooks limités aux commandes ouvertes ; messages de la caisse Flutter | C1, C2, S2, S6 | B (contrôle « commande scellée ») | Moyenne à grande |
| **D** | Archive fiscale mensuelle : CSV ouvert, notice en français, empreinte et signature, stockage privé, journal de génération, téléchargement depuis le back-office | C7, S4 | B | Moyenne |
| **E** | Commande de vérification (toutes les chaînes, les clôtures, et le contrôle croisé commande / ticket) ; numéro fiscal exposé par l'API et imprimé sur le ticket de la caisse | C4, C8 | B, C, D (C8 peut avancer en parallèle dès maintenant) | Moyenne |
| **F** | Nom et numéro de version du logiciel, règle version majeure / mineure ; génération autonome de l'attestation (modèle BOI-LETTRE-000242) depuis la caisse et le back-office | — | E | Moyenne |

**Ordre :** B → C → D → E → F. C8 (dans E) ne dépend de rien et peut être avancé si un développeur Flutter est disponible.

## Mise en production

**Décision d'Ilies (2026-10-07) :**
- **Une seule mise en production**, après le lot F, de toutes les modifications en attente, dans tous les dépôts.
- **Les versions actuelles des applications de caisse doivent continuer de fonctionner** : chaque changement d'API reste rétrocompatible. Un champ peut être ajouté, mais pas retiré ni renommé ; un refus nouveau doit arriver dans une forme d'erreur que les anciennes versions savent afficher.

Le tableau ci-dessous décrit l'ordre logique ; il est remplacé par cette mise en production unique.

| Après | Ce qui part en production | Prérequis |
|---|---|---|
| **B** | Lots A et B ensemble, avec les migrations 168 à 171 | Requête de contrôle des doublons de tickets (en tête de la 169). Ordre : 168, 169, 170, puis `cmd/backfill_fiscal_closures --from=… --apply` (clé de production), puis le code, puis 171. |
| C | Lot C | Migration 172 (index des tickets par commande, `CONCURRENTLY`) **avant** le code : sans elle, chaque clôture parcourt toute la table des tickets sous le verrou de la chaîne. Ordre global de la mise en production unique : 168, 169, 170, 172, rattrapage des clôtures, code, 171. |
| D, E | Chaque lot à sa fin | Migrations du lot, s'il y en a |
| E | — | **Première attestation possible** (signature manuelle sur le modèle officiel) : tous les constats bloquants C1 à C8 sont traités |
| F | Génération autonome des attestations | Version du logiciel fixée |

**Vérifications avant la mise en production** (demande d'Ilies, 2026-10-07), une fois tous les lots terminés :
- **nombre de transactions ouvertes par chaque endpoint** : une seule transaction par requête là où c'est possible, aucune transaction imbriquée ou inutile ;
- **temps d'exécution des endpoints POS** : mesure de chaque endpoint de la caisse avant / après l'ensemble des lots, et optimisation de ceux qui ont ralenti.

Déployer le lot A seul en production n'aurait rien de faux, mais introduirait la chaîne par commande que le lot B retire. C'est pourquoi le lot A attend le lot B.

## Travaux pour plus tard (hors conformité)

- **Mouvements de caisse et page « Trésorerie » du back-office**, comme Zelty (demande d'Ilies, 2026-10-07 ; pas tout de suite) :
  - côté caisse, sur un registre ouvert : apport, retrait, dépense payée en espèces ou remise en banque, avec un montant et un motif ;
  - au Z, le théorique espèces devient : fond + ventes en espèces + entrées − sorties ;
  - côté back-office : historique par période et par registre, remises en banque, écarts, export pour le comptable ;
  - ce n'est pas une obligation BOI (pas des règlements de clients), mais c'est utile au restaurateur et au comptable. Les mouvements sont tracés au journal d'audit, et un registre fermé n'est jamais modifié.

## Fonctionnement commun à tous les lots

- **Phase 0 :** recensement rendu avant tout code ; **une phase, un point d'arrêt.**
- **Migrations :** écrites, appliquées par Ilies.
- **Performance :** mesure avant / après (`TestFiscalChainPerf_Postgres`), aucune régression sur les écritures.
- **Données existantes :** jamais modifiées.
- **Commits :** atomiques, hunks du lot uniquement, sur accord d'Ilies.
- **Documentation :** journal dans le brief, entrée en tête de `decisions.md`, tableau de suivi dans l'audit.
