# Conformité caisse : plan anticipé des lots D, E et F

**Date :** 2026-10-08
**Point de départ :** lots A, B et C commités sur `staging` (API) et sur la branche `conformite-caisse-lot-c` (caisse Flutter), rien en production. Voir la [feuille de route](attestation-conformite-feuille-de-route.md).
**Statut :** plan à valider. Chaque lot aura son brief, une phase 0 de recensement et un point d'arrêt par phase, comme A, B et C.

## Décisions d'Ilies (2026-10-08)

- **Signature du volet 2 (F1)** : signature électronique simple dans l'application, comme proposé. Elle reste à faire valider par son conseil avant le lot F.
- **Identité de l'éditeur et signature du volet 1 (F2)** : fournies au moment du lot F.
- **Nom et version (F3)** : « WelloResto », version **2.x.x**. Numérotation retenue : **2.0.0** pour la première version attestée, c'est-à-dire la mise en production unique des lots A à F. Racine majeure `2`. Les subdivisions `2.MINEUR.CORRECTIF` sont réservées aux versions qui ne touchent pas aux quatre conditions. Tout changement de ces conditions donnera `3.0.0`, avec une nouvelle attestation.
- **Ticket de la borne (E2)** : la borne garde un **ticket simple** (lignes, TVA, date de commande), **sans numéro fiscal**. Le ticket fiscal reste celui de la caisse.

## Vue d'ensemble

| Lot | Objet | Constats | Dépôts | Taille | Débloque |
|---|---|---|---|---|---|
| **D** | Archive fiscale mensuelle, téléchargeable | C7 | API, back-office | Moyenne | — |
| **E** | Contrôle d'intégrité ; numéro fiscal sur le ticket client | C4, C8 | API, caisse Flutter (borne à décider) | Moyenne | **Première attestation possible** (signée à la main) |
| **F** | Version du logiciel ; génération autonome de l'attestation | prérequis BOI §270-§375 | API, caisse, back-office | Moyenne | Mise en production unique |

**Ordre conseillé :** D → E → F. Deux pans peuvent avancer en parallèle :
- **C8** (numéro fiscal sur le ticket) ne dépend de rien ;
- la **version** (début de F) aussi.

Ce que le BOI exige, rappelé ici parce que le plan en découle :
- **Archivage (§220-§250)** :
  - périodicité au plus annuelle ;
  - l'archive « fige les données et leur donne date certaine », avec un dispositif d'intégrité dans le temps ;
  - format ouvert et notice en français ;
  - traçabilité de la génération, conservée ;
  - l'éditeur doit fournir la fonction de génération ;
  - l'utilisateur conserve 6 ans.
- **Attestation (§270-§375, modèle BOI-LETTRE-000242)** :
  - nominative ; une mention dans les CGV ne vaut pas attestation ;
  - **volet 1** signé par le **représentant légal de l'éditeur** : logiciel et version, numéro de licence s'il existe, fonctionnalités couvertes et non couvertes, date de mise sur le marché, et, en option, racine de la version majeure et engagement de réserver les subdivisions aux versions mineures ;
  - **volet 2** complété et **signé par le restaurateur** : date d'acquisition, distributeur, date de début d'utilisation. L'attestation n'a de valeur que si le volet 2 est signé ;
  - un document pré-rempli et pré-signé par l'éditeur, complété par l'assujetti, est admis (§370) ;
  - une nouvelle version majeure exige une nouvelle attestation.

---

## Lot D — Archive fiscale mensuelle (C7)

**Objectif :**
- produire automatiquement, pour chaque établissement et chaque mois clos, une archive de ses données fiscales ligne par ligne ;
- la figer, la sceller et la tracer ;
- la rendre téléchargeable depuis le back-office pendant au moins 6 ans.

**Contenu (décidé au lot B, S4 : « mêmes données que les clôtures fiscales par construction ») :** une archive ZIP par établissement et par mois, qui contient :
- des fichiers CSV (UTF-8, séparateur `;`, montants en centimes et en euros, dates ISO 8601 UTC et heure locale) :
  - tickets et avoirs du mois, avec leurs lignes (`items_snapshot` à plat), la TVA par taux (`tax_details`) et leurs empreintes ;
  - paiements du mois, annulés compris, avec leur état et leurs empreintes ;
  - annulations de paiement et réouvertures (entrées du journal d'audit du mois) ;
  - clôtures journalières du mois, la clôture mensuelle, et, en décembre, la clôture annuelle, avec leurs totaux, grand total, total perpétuel et empreintes ;
  - registres de caisse fermés du mois (Z) ;
- `NOTICE.txt` : notice en français (structure de chaque fichier, signification des colonnes, méthode de vérification des empreintes) ;
- `MANIFEST.json` : empreinte SHA-256 de chaque fichier, période, établissement, version du logiciel, date de génération.

**Intégrité et date certaine :**
- l'archive est inscrite dans une nouvelle chaîne fiscale `fiscal_archives`, chaînée et signée comme les autres. L'empreinte couvre le SHA-256 du ZIP et du manifeste ;
- l'archive N+1 porte l'empreinte de l'archive N. C'est ce qui donne la date certaine, par la chaîne et la signature de l'éditeur ;
- **option** : un horodatage qualifié par un tiers (RFC 3161), à décider (voir plus bas).

**Génération :**
- même passage horaire que les clôtures (`RunFiscalClosures`), dès que la clôture mensuelle est écrite ;
- idempotente, une archive par mois ;
- jamais régénérée en écrasant : une nouvelle génération éventuelle s'ajoute dans la chaîne ;
- **rattrapage** des mois passés par la tâche horaire elle-même (prévu d'abord par une commande `cmd/backfill_fiscal_archives`, abandonnée au lot D phase 3).

**Stockage et accès :**
- bucket R2 **privé** (infrastructure des exports comptables, migration 166 : table, SHA-256, lien signé d'une heure) ;
- table `fiscal_archives` (migration 174 ; la 173 fige le surcoût des options, phase 1 du lot D) ;
- **back-office** : liste des archives par mois, téléchargement, empreinte affichée ;
- **journal de génération** : la chaîne elle-même, et une entrée d'audit à chaque téléchargement.

**Phases :**
- **phase 0** : recensement des données §50 réellement présentes, volumes par mois (taille des archives), format des CSV soumis à validation ;
- **phase 1** : ticket figé complet, options payantes comprises (migration 173) — ajoutée après la phase 0 ;
- **phase 2** : génération et scellement (API, migration 174) ;
- **phase 3** : tâche et rattrapage ;
- **phase 4** : routes et back-office ;
- **phase 5** : mesures (durée de génération par mois, effet sur le passage horaire, qui ne doit pas ralentir les clôtures).

Le détail à jour est dans le [brief du lot D](attestation-conformite-05-lot-D-brief.md).

**Décisions à prendre :**

| # | Question | Recommandation |
|---|---|---|
| D1 | Format | **CSV + notice** (lisible par l'administration sans outil) ; JSON écarté. |
| D2 | Horodatage par un tiers (RFC 3161, payant selon le prestataire) | **Non pour l'instant** : la chaîne signée suffit au BOI (« procédé fiable ») ; à reconsidérer si un contrôleur le demande. |
| D3 | Le restaurateur peut-il régénérer à la demande une archive d'une période quelconque ? | **Oui, en plus de l'automatique** : une génération à la demande s'inscrit aussi dans la chaîne. |
| D4 | Second support (le BOI l'évoque, sans l'imposer) | **Envoi par e-mail du lien au gérant chaque mois**, en option ; pas de second stockage technique. |
| D5 | Point de départ du rattrapage | Le premier mois qui a des tickets (**mars 2026**), comme le total perpétuel. |

---

## Lot E — Contrôle d'intégrité (C4) et numéro fiscal sur le ticket (C8)

### E1 — Contrôle d'intégrité (C4)

**Objectif :** « détecter et démontrer » (§100). Un outil rejoue toutes les chaînes d'un établissement sur une période et rend un rapport.

**Contrôles :**
- **chaque chaîne** (`payments`, `receipts`, `cash_registers`, `audit_logs`, `fiscal_closures`, `fiscal_archives`, et `orders` pour ses lignes antérieures au lot B) :
  - empreinte recalculée à l'identique ;
  - signature vérifiée ;
  - parent égal au maillon précédent : ni fourche, ni trou ;
- **numérotation des tickets** : continue, sans doublon ;
- **clôtures** :
  - chaque jour clos une fois ;
  - totaux des clôtures égaux aux tickets du jour ;
  - mois et années égaux à la somme des jours ;
  - total perpétuel cumulatif ;
- **contrôle croisé** :
  - chaque commande close a son empreinte dans la clôture de son jour, et cette empreinte se recalcule ;
  - chaque vente close a un ticket ;
  - la somme des tickets d'une commande vaut sa vente nette ;
- **archives** : le SHA-256 du fichier stocké est égal à celui de la chaîne.

**Livraison en deux temps (arbitrage C4 : « commande minimale » d'abord) :**
1. `cmd/verify_fiscal --merchant=… --from=… --to=…` : rapport texte et JSON, code de sortie non nul en cas d'anomalie. C'est l'outil de l'éditeur, à la demande de l'administration ;
2. le même contrôle exposé en lecture au back-office : « Vérifier l'intégrité », avec un rapport téléchargeable. Le BOI veut qu'il puisse être lancé par le commerçant. À faire dans E si le temps le permet, sinon dans F.

**Lignes antérieures au lot A (`hash_version = 1`) :**
- leur ancienne formule ne couvrait pas tout le §50, et l'audit n'avait pas de signature à clé ;
- le rapport les **compte et vérifie leur chaînage**, mais les signale comme « antérieures à la version attestée » ;
- l'attestation porte sur la version qui écrit le v2.

**Préalable à la première attestation (audit, « Suite ») :** le contrôle passé **sans erreur sur des données réelles**. En production, puisque staging n'est pas représentatif ; c'est donc la première action après la mise en production.

### E2 — Numéro fiscal sur le ticket client (C8)

**Constat :** la caisse imprime le ticket à partir de la commande, sur l'appareil. Le numéro `F-AAAA-NNNNNN` n'apparaît nulle part, et aucune route n'expose le ticket fiscal.

**Arbitrage C8 :** le numéro n'existe qu'une fois la commande close. Le justificatif est le ticket final.

**Proposition :**
- **API** : le ticket fiscal (numéro, date, lignes, totaux, TVA par taux) est exposé pour une commande close. Deux options :
  - dans la commande (`receipt` dans `POST /orders/history` et le détail) ;
  - par une route `GET /orders/{id}/receipt`.
- **Caisse** :
  - commande close → impression **à partir du ticket figé**, avec son numéro et sa TVA ventilée ;
  - commande ouverte → impression d'une **note** marquée « Note — ne vaut pas ticket de caisse », sans numéro ;
  - les avoirs aussi : impression de l'avoir avec son numéro.
- **Compatibilité** : un ajout de champ ou de route, que les caisses actuelles ignorent.

**Décisions à prendre :**

| # | Question | Recommandation |
|---|---|---|
| E1 | Exposition du ticket : champ dans la commande, ou route dédiée | **Route dédiée** `GET /orders/{id}/receipt` (et `/receipts/{number}`) : n'alourdit pas les listes de commandes, que la caisse charge souvent. |
| E2 | La borne (`wello-kiosk`) imprime un ticket au paiement, souvent avant la clôture | **Le ticket de la borne devient un bon de commande**, marqué comme tel ; le ticket fiscal se demande en caisse ou par e-mail. À confirmer selon ton usage réel. |
| E3 | ScanNOrder : le justificatif du client est le mail ou le suivi | **Lien vers le ticket fiscal** dans le mail de fin de commande, une fois la commande close. Peut être reporté (hors caisse physique). |
| E4 | Contrôle d'intégrité au back-office dès E | **Oui si possible**, sinon au début de F. |

---

## Lot F — Version du logiciel et génération autonome de l'attestation

### F1 — Version

**Objectif :** un nom commercial et un numéro de version clairement identifiés, avec la règle majeure / mineure (§340, §375).

**Proposition :**
- **ce qui est attesté** : le **système de caisse WelloResto**. Les fonctions fiscales sont toutes côté serveur (empreintes, clôtures, archives, contrôle) ; la caisse Flutter n'imprime que le ticket figé (E2) ;
- **numéro de version** : celui de l'**API fiscale**, sous la forme `MAJEUR.MINEUR.CORRECTIF` :
  - le majeur ne change que si l'on touche aux conditions d'inaltérabilité, de sécurisation, de conservation ou d'archivage ;
  - l'attestation porte la **racine** `MAJEUR` et s'engage à réserver `MAJEUR.x.y` aux versions mineures ;
- **où vit la version** :
  - un paquet `internal/version` (valeur injectée au build) ;
  - une route publique `GET /version` ;
  - la version est écrite dans chaque clôture, chaque archive et chaque rapport de contrôle ;
- **liste des versions de caisse compatibles**, pour l'impression du ticket figé : la caisse affiche la version du système dans ses réglages ;
- **garde-fou en revue de code** : un changement dans `internal/fiscal` ou les migrations fiscales impose de décider « majeur ou non ». Une note de version courte est consignée dans `docs/`.

### F2 — Génération autonome de l'attestation

**Objectif :** le restaurateur obtient lui-même son attestation, à son nom, depuis la caisse ou le back-office. C'est le besoin de départ : l'expert-comptable d'un client la demande.

**Proposition, dans le cadre du §370 (document pré-rempli et pré-signé par l'éditeur, complété par l'assujetti) :**
- **volet 1, pré-rempli et pré-signé par WelloResto** :
  - éditeur (raison sociale, SIREN, adresse, représentant légal) ;
  - logiciel « WelloResto » et version (racine majeure et engagement sur les mineures) ;
  - numéro de licence = identifiant d'abonnement ;
  - fonctionnalités couvertes : caisse, borne, ScanNOrder, plateformes, clôtures, archives ;
  - fonctionnalités non couvertes (à lister : par exemple les modules sans encaissement) ;
  - date de mise sur le marché de la version ;
  - **signature du représentant légal** : image stockée dans le bucket privé, apposée par l'API ;
- **volet 2, pré-rempli à partir des données de l'établissement** :
  - raison sociale, SIRET, adresse ;
  - date d'acquisition = début de l'abonnement ;
  - distributeur = WelloResto ;
  - date de début d'utilisation = premier ticket ;
  - **il reste à signer par le restaurateur** (voir F1-F2 plus bas) ;
- **production** :
  - PDF généré par l'API, avec la bibliothèque déjà utilisée pour les factures ;
  - conservé (table `attestations`, migration 176, fichier dans le bucket privé, SHA-256) ;
  - inscrit au journal d'audit ;
  - réémis automatiquement quand la version majeure change ;
- **accès** :
  - back-office : générer, télécharger, **envoyer au comptable par e-mail** ;
  - caisse : générer, envoyer par e-mail (pas d'impression nécessaire).

**Garde-fous :**
- génération possible seulement si le **contrôle d'intégrité (E1) de l'établissement passe** ; sinon le restaurateur est orienté vers le support ;
- l'attestation engage pénalement le signataire de l'éditeur (C. pén. art. 441-1) : elle n'est disponible qu'une fois les lots A à E en production **et** le contrôle passé sur les données réelles.

**Décisions à prendre :**

| # | Question | Recommandation |
|---|---|---|
| F1 | Signature du volet 2 par le restaurateur | **Signature électronique simple dans l'application** : nom du signataire, case « je certifie », horodatage, identité du compte connecté, tracée au journal d'audit, puis PDF final. À défaut : PDF à imprimer et signer à la main. **À faire valider par ton conseil**, car le modèle prévoit une signature. |
| F2 | Signature du volet 1 (représentant légal de WelloResto) | **Image de signature pré-apposée** (§370 l'admet pour un document pré-rempli). Qui est le représentant légal, et quelle raison sociale, SIREN et adresse ? |
| F3 | Nom commercial et première racine majeure | Par exemple « WelloResto », **version 2** (la 1 étant l'avant-lot A). |
| F4 | Fonctionnalités non couvertes | À lister ensemble : tout ce qui n'enregistre pas de règlement client (planning, HACCP, menu…) n'a pas à y figurer ; à préciser pour les modules ambigus. |
| F5 | Une attestation par établissement ou par entité juridique (SIRET) | **Par établissement** (SIRET), ce que le BOI demande (« nominative »). |

---

## Avant la mise en production (rappel)

- **Vérifications demandées par Ilies** :
  - nombre de transactions ouvertes par chaque endpoint ;
  - temps d'exécution des endpoints POS, avant / après l'ensemble des lots, avec optimisation de ce qui a ralenti.
- **Sécurité** : vérifier la signature des webhooks Uber Eats (aujourd'hui `return true`, `internal/webhook/ubereats/client/signature.go`).
- **Ordre de déploiement** :
  - migrations 163 à 167 (autres chantiers) ;
  - 168 → 169 → 170 → 172 → 173 (prix des options) → 174 (archives) → 175 (index des lectures par période) → 176 (attestations) ;
  - rattrapage des clôtures ;
  - code (la tâche horaire rattrape ensuite les archives des mois passés) ;
  - migration 171 ;
  - puis contrôle d'intégrité sur les données réelles, avant toute attestation.
- **Caisse Flutter** : fusionner la branche `conformite-caisse-lot-c` (et les suivantes), et publier une version qui imprime le ticket figé (E2) **avant** d'ouvrir la génération d'attestation (F2).

## Charge estimée (indicative)

| Lot | API | Caisse | Back-office | Borne |
|---|---|---|---|---|
| D | grande partie (génération, chaîne, tâche, rattrapage) | — | liste et téléchargement | — |
| E | contrôle + exposition du ticket | impression du ticket figé, note, avoir | contrôle (option) | bon de commande (selon E2) |
| F | version, PDF, table, routes | écran attestation | écran attestation, envoi au comptable | — |
