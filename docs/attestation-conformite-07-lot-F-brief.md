# PROMPT — Conformité caisse, lot F : version du logiciel et génération autonome de l'attestation

**Date :** 2026-10-08
**Objet :** le restaurateur obtient lui-même son attestation individuelle de l'éditeur, à son nom, depuis le back-office ou la caisse (BOI-TVA-DECLA-30-10-30 §270 à §375, modèle BOI-LETTRE-000242). Il en a besoin, par exemple, quand son expert-comptable la demande.
**Plan :** [attestation-conformite-04-plan-D-E-F.md](attestation-conformite-04-plan-D-E-F.md), section « Lot F ».
**Dépend de :** lots A à E.

## Règle d'or

- L'attestation **engage pénalement** le représentant légal de l'éditeur (C. pén. art. 441-1). Elle reste **fermée** (`ATTESTATION_ENABLED` absent ou faux) jusqu'à deux conditions :
  - la mise en production des lots A à F ;
  - un `cmd/verify_fiscal --all` **sans erreur** sur les données réelles.
- Aucune donnée existante n'est modifiée. Les caisses actuelles continuent de fonctionner : seules des routes et des écrans sont ajoutés.

## Décisions

| # | Question | Retenu |
|---|---|---|
| F1 | Signature du volet 2 | **Signature électronique simple** (décision d'Ilies, confirmée le 2026-10-08 : une case cochée suffit). Le signataire saisit son nom (le modèle commence par « Je soussigné, NOM Prénom ») et coche la certification. L'horodatage, le compte connecté et l'entrée au journal d'audit chaîné sont enregistrés ; la mention est imprimée dans le volet 2. |
| F2 | Identité de l'éditeur et signature du volet 1 | Décision d'Ilies (2026-10-08) : **BINYA**, représentée par **BELLAL Ilies**, « Fait à **Metz** » ; version 2.0.0 mise sur le marché le **15/07/2026**. Valeurs par défaut du code, chacune remplaçable par sa variable d'environnement. Signature pré-apposée (§370) : image générée à la demande d'Ilies (« I. Bellal », écriture manuscrite), intégrée au binaire et remplaçable par une image du bucket privé. |
| F3 | Nom et version | « WelloResto », **2.0.0**, racine majeure **2**, versions mineures **2.x.y** (décision d'Ilies). |
| F4 | Fonctionnalités couvertes / non couvertes | Décision d'Ilies : **le strict nécessaire fiscal**. Le modèle permet d'attester « les fonctionnalités de caisse de ce logiciel/système », et non tout le logiciel : c'est la variante retenue. Couvert : les fonctionnalités de caisse sur tous les canaux qui enregistrent un règlement client. Non couvert : tout le reste. |
| F5 | Une attestation par établissement ou par entité | **Par établissement** (SIRET), nominative. |
| F6 | Numéro de licence | `WR-<identifiant de l'établissement>` (validé par Ilies). |
| F7 | Qui peut générer | Consultation, téléchargement et envoi : `reports.financial.read`. Génération : en plus `settings.manage` (validé par Ilies). |

## Phases

| Phase | Contenu | État |
|---|---|---|
| 1 | Version : `internal/version` 2.0.0, `GET /version`, [versions-logiciel.md](versions-logiciel.md) | **Faite**, commitée (`d0d4438`) |
| 2 | Attestation côté API : migration 176, PDF fidèle au modèle, garde-fous, stockage, journal d'audit, routes | Faite et commitée ; migration 176 appliquée sur staging, test d'intégration vert |
| 3 | Back-office : page « Attestation de conformité » | Faite (type-check, lint) |
| 4 | Caisse Flutter : « Attestation de conformité » dans les réglages, à la place de l'ancien « Document NF525 » | Faite (analyse, tests) |
| 5 | Documentation, mesures, commits | Faite |

## Journal

### Phase 0 — Recensement (2026-10-08)

- **Modèle officiel** BOI-LETTRE-000242 (version du 25/03/2026), relevé sur bofip.impots.gouv.fr. Il comporte :
  - un préambule ;
  - un **volet 1**, rempli par l'éditeur : déclaration ; mention facultative (3) sur la racine majeure et les subdivisions ; périmètre couvert et non couvert ; « Fait à, le » ; signature ;
  - un **volet 2**, rempli par l'entreprise utilisatrice : date et distributeur de l'acquisition ; date de début d'utilisation ; « Fait à, le » ; signature ;
  - le rappel de l'article 441-1 du code pénal, sur chaque volet.
- **PDF** : `gofpdf` (déjà utilisé par les factures), polices de base en cp1252 (accents, €, §, °).
- **Établissement** : la table `merchant` n'a pas de raison sociale distincte du nom commercial (`fullname`). La raison sociale est donc **proposée** et modifiable par le signataire. Le SIRET est obligatoire et non modifiable.
- **Dates du volet 2** :
  - acquisition : date de création du compte, proposée ;
  - début d'utilisation : premier ticket, proposé ;
  - toutes deux modifiables par le signataire, qui les certifie.
- **E-mail au comptable** : `SendAsyncWithAttachment` (Brevo), avec un gabarit dédié.
- **Constat sur la caisse Flutter** : les réglages proposaient un « Document NF525 ». Ce document se présentait comme une « Certification NF525 » et une « auto-certification », avec une version d'application figée (« 109 (Bêta 1.5) ») et des mécanismes inexacts (« archivage chiffré »). Le logiciel n'est pas certifié NF525 : ce document était **trompeur**. Il est remplacé par l'attestation (phase 4) et ses fichiers sont supprimés.

### Phase 1 — Version (2026-10-08)

- `internal/version` : `Version = "2.0.0"`, injectable au build ; `MajorRoot()` vaut « 2 » et `MinorPattern()` « 2.x.y ».
- `GET /version`, public : nom, version, racine, subdivisions.
- [docs/versions-logiciel.md](versions-logiciel.md) :
  - ce qui est attesté (le système, version de l'API) ;
  - règle majeure / mineure ;
  - garde-fou de revue : toute modification de `internal/fiscal*`, des migrations des tables scellées ou de l'émission des tickets oblige à décider « majeure ou non » ;
  - historique.
- La version figure déjà dans chaque archive, chaque rapport de contrôle et chaque ticket imprimé (lots D et E). Elle n'est pas ajoutée aux clôtures : cela changerait leur charge scellée, et la date de clôture rattache chaque clôture à une version de l'historique.

### Phase 2 — Attestation côté API (2026-10-08)

**Migration 176** ([176_attestations.up.sql](../migrations/todo/176_attestations.up.sql)) : table `attestations`, une ligne par document, jamais modifiée. Elle porte :
- la version et la racine majeure ;
- `content`, toutes les valeurs reportées dans le document ;
- le signataire, le compte et l'horodatage ;
- le fichier dans le bucket privé et son SHA-256.

**Configuration** ([internal/config/attestation.go](../internal/config/attestation.go)) : valeurs par défaut du code (décision d'Ilies), chacune remplaçable par sa variable d'environnement.

| Variable | Défaut | Contenu |
|---|---|---|
| `ATTESTATION_ENABLED` | fermée | `true` pour ouvrir la génération |
| `ATTESTATION_EDITOR_REPRESENTATIVE` | `BELLAL Ilies` | NOM Prénom du représentant légal de l'éditeur |
| `ATTESTATION_EDITOR_COMPANY` | `BINYA` | Raison sociale de l'éditeur |
| `ATTESTATION_EDITOR_CITY` | `Metz` | Ville du « Fait à » du volet 1 |
| `ATTESTATION_EDITOR_SIGNATURE_KEY` | vide | Image PNG du bucket privé qui remplace la signature intégrée (`internal/modules/attestations/signature_editeur.png`) |
| `ATTESTATION_RELEASE_DATE` | `2026-07-15` | Date de mise sur le marché de la version attestée |

**Document** ([document.go](../internal/modules/attestations/document.go), [pdf.go](../internal/modules/attestations/pdf.go)) :
- Le texte du modèle est repris **mot pour mot**. Seules les zones à compléter sont remplies, et les choix « à adapter selon le cas » tranchés au plus juste (F4) : « les fonctionnalités de caisse de ce logiciel/système », « satisfont ».
- La mention facultative (3) est retenue (racine 2, subdivisions 2.x.y, engagement de l'éditeur) : elle ouvre la tolérance du § 380.
- Volet 1 en page 1, avec la signature de l'éditeur. Volet 2 en page 2, avec le bloc de signature électronique : « Signé électroniquement par …, le … à …, depuis le compte … », et le résultat du contrôle d'intégrité préalable.
- Chaque page porte en pied la référence `ATT-<établissement>-<horodatage>`, le logiciel et la version.

**Périmètre (F4, décision d'Ilies : le strict nécessaire fiscal) :**
- couvert : « Les fonctionnalités de caisse : enregistrement des règlements des clients et émission des tickets et avoirs (application de caisse, borne de commande, commande en ligne ScanNOrder, commandes des plateformes de livraison intégrées), avec leurs clôtures, leur journal et leur archivage. » Tous les canaux qui enregistrent un règlement client doivent y figurer, sinon ils ne seraient pas couverts ;
- non couvert : « Toutes les autres fonctionnalités du logiciel, qui n'enregistrent pas de règlement client. »

**Génération** (`POST /accounting/attestations`, [service.go](../internal/modules/attestations/service.go)). Les garde-fous s'appliquent dans cet ordre, chacun avec un refus explicite et un message en français :
1. génération ouverte (`attestation_disabled`) ;
2. identité de l'éditeur complète et stockage disponible (`attestation_not_configured`) ;
3. SIRET de l'établissement renseigné (`attestation_siret_missing`) ;
4. volet 2 complet et cohérent (`attestation_invalid`) : signataire, raison sociale, ville, dates valides, aucune date future, début d'utilisation après l'acquisition, case certifiée ;
5. une génération à la fois par établissement (`attestation_busy`) ;
6. **contrôle d'intégrité des 31 derniers jours sans erreur** (`attestation_integrity_errors`).

Ensuite, dans l'ordre :
- PDF ;
- dépôt dans le bucket privé, sous une clé jamais écrasée ;
- ligne en base et entrée `ATTESTATION_GENERATED` au journal d'audit chaîné, dans une même transaction ;
- lien signé d'une heure.

**Autres routes**, sous `reports.financial.read` :
- `GET /accounting/attestations` : disponibilité (avec la raison, sinon), valeurs proposées pour le volet 2, attestations. Une attestation dont la racine diffère de la version en service est marquée **périmée**.
- `GET /accounting/attestations/{id}/download` : entrée `ATTESTATION_DOWNLOAD` au journal, puis lien signé d'une heure.
- `POST /accounting/attestations/{id}/email` : envoi en pièce jointe, après vérification de l'empreinte du fichier stocké ; entrée `ATTESTATION_SENT` au journal.

**Réémission à la version majeure suivante** : elle ne peut pas être automatique, car le volet 2 doit être signé par le restaurateur. L'attestation périmée est signalée au back-office et à la caisse ; il en génère une nouvelle.

**Tests** :
- `TestDocumentTextFollowsOfficialModel` : phrases du modèle, zones remplies.
- `TestRenderPDF` : PDF avec et sans image de signature ; image illisible refusée. Rendu relu à l'œil sur un exemple.
- `TestAttestations_Postgres` (**à lancer après la migration 176**) couvre :
  - les six garde-fous, avec sept cas de volet 2 invalide ;
  - la génération : PDF stocké et empreinte, contenu en base, entrée au journal ;
  - la liste, le lien et l'envoi par e-mail, chacun avec son entrée au journal ;
  - le cloisonnement par établissement ;
  - un ticket v2 altéré, qui bloque la génération.

### Phase 3 — Back-office (2026-10-08)

Page **Comptabilité → Attestation de conformité** (`wello-back-office`, `src/pages/Attestation.tsx`, `src/services/attestationsService.ts`) :
- explication ;
- raison d'indisponibilité, le cas échéant ;
- formulaire du volet 2 : raison sociale, SIRET (non modifiable), dates, représentant légal, ville, case de certification rappelant l'article 441-1 ;
- génération (ouvre le PDF) ;
- liste des attestations (périmées signalées), téléchargement, envoi par e-mail.

Type-check et lint propres.

### Phase 4 — Caisse Flutter (2026-10-08)

Réglages → Autres → **Attestation de conformité** (`lib/ui/widgets/dialogs/settings/other/attestation/attestation_dialog.dart`, `lib/data/api/attestation_api.dart`). Même parcours que le back-office :
- le PDF s'ouvre dans le navigateur ;
- les refus de l'API s'affichent par `runApiAction` ;
- le délai d'attente de la génération est de 90 s (contrôle d'intégrité préalable).

L'ancien « Document NF525 » est retiré (constat de la phase 0). `flutter analyze` ne relève rien ; `test/data/api/responses/attestation_response_test.dart` est vert.

## Reste à faire pour clore le lot

1. **Point d'attention, date de mise sur le marché.** Le volet 1 dit « mis sur le marché à compter du 15/07/2026, dans sa version n° 2.0.0 ». Or la 2.0.0 (lots A à F) n'est pas encore en production. Si un contrôleur rapproche cette date de celle du déploiement, l'écart se voit. Deux options :
   - retenir comme date celle de la mise en production de la 2.0.0, par `ATTESTATION_RELEASE_DATE`, sans changer le code ;
   - garder le 15/07/2026, si cette date est celle de la mise sur le marché du logiciel dans sa version courante.
2. Le conseil d'Ilies peut confirmer la signature électronique simple (F1). Ce n'est pas bloquant.
3. **Ouvrir la génération** (`ATTESTATION_ENABLED=true`) après la mise en production et un `cmd/verify_fiscal --all` sans erreur.

### Phase 5 — Tests et mesures (2026-10-08, migrations 175 et 176 appliquées sur staging par Ilies)

- `TestAttestations_Postgres` : **vert**, du premier coup. Il couvre :
  - les six garde-fous ;
  - les sept cas de volet 2 invalide ;
  - la génération, la liste, le lien et l'envoi, chacun avec son entrée au journal ;
  - le cloisonnement par établissement ;
  - un ticket altéré, qui bloque la génération.
- **Durée d'une génération** : **930 ms** depuis le poste. Elle comprend le contrôle d'intégrité des 31 derniers jours, le PDF, le dépôt, l'écriture en base et au journal. Elle reste très en deçà du délai d'attente de 90 s de la caisse.
- **Migration 175** : les deux index sont utilisés (plans `EXPLAIN` sur staging, structure seulement). Les paiements d'un jour et les commandes closes d'un jour se lisent par parcours d'index, au lieu d'un parcours complet de la table.
- Suites d'intégration relancées, toutes migrations en place, toutes vertes : `fiscal`, `fiscalarchive`, `fiscalverify`, `accounting`, `receipt`, `attestations`, `tasks`, `cash_registers`, `order_life_cycle`.
