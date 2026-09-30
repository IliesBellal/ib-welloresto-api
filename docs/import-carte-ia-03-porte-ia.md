# Import de carte par photo — Étape 3 : la porte IA côté API

Troisième étape du découpage de [cadrage-import-carte-photo-ia.md](cadrage-import-carte-photo-ia.md) (§ 5 et § 9) : la lecture de photos devient une quatrième porte de l'import produits. Elle s'appuie sur l'étape 2 ([couche IA](import-carte-ia-02-couche-ia.md)).

**Statut :** implémenté et commité le 2026-09-29, non déployé. La migration 161 est appliquée sur staging (pas en production). Aucun appel réel à l'API Anthropic n'a encore été fait (§ 7).

---

## 1. Découpage de l'étape

| Partie | Contenu | État |
|---|---|---|
| 3a | Paquet `importer` (pur) : contrat JSON de l'IA et schéma, fusion des photos (`BuildAIMenuImport`), nature → TVA, preview, plan de commit (nature, groupes, confirmation TVA) | Fait |
| 3b | Écriture des groupes en base (`is_product_group`, `by_product_of`) | Fait |
| 3c | Table des brouillons, crédits (migration 161) | Fait |
| 3d | Service asynchrone (appels IA par photo), endpoints, nettoyage périodique | Fait |

---

## 2. Décisions

### D1 — Identifiants externes par nom, pas par brouillon *(écart au cadrage)*
Le cadrage prévoyait des identifiants synthétiques par brouillon (`ai-<draft>-<ref>`). Ils sont finalement **dérivés du nom**, qualifié par la catégorie (`importutil.GeneratedExternalID`), comme pour le template Excel et la saisie manuelle : `ai-p-<slug>-<empreinte>` pour un produit, `ai-c-` pour une catégorie, `ai-g-` pour un groupe, `ai-o-` / `ai-oo-` pour les groupes d'options et leurs options.

*Pourquoi :* c'est ce qui rend l'import **rejouable**, comme les autres portes. Photographier à nouveau la même carte retrouve les produits déjà importés (« déjà importé, ignoré »), au lieu de créer des doublons. Un identifiant par brouillon aurait tout recréé à chaque essai.

Les identifiants tiennent dans `external_id varchar(64)` : au plus 55 caractères.

### D2 — Doublons entre photos : gardés une fois *(écart au cadrage)*
Même catégorie et même nom sur deux photos (photos qui se chevauchent) : c'est **le même identifiant**, donc un seul produit. On garde la première lecture, et un avertissement `ai_duplicate_merged` donne les deux prix lus.

Le cadrage disait « signalés sans suppression d'office ». Mais deux produits de même identité ne peuvent pas coexister dans un lot : l'avertissement, qui cite les deux prix, remplace la conservation.

### D3 — Groupes : au moins deux déclinaisons, parents en tête
- Un groupe proposé par l'IA n'est créé que s'il garde **au moins deux déclinaisons**. Sinon la déclinaison unique reste à la racine (« s'il n'y a qu'un seul Coca, on le laisse à la racine »), avec un avertissement `ai_group_dissolved`.
- **Au commit, la règle est réappliquée après les décisions de relecture** : exclusions, dégroupages. Un groupe qui tombe à une déclinaison n'est pas créé, et sa déclinaison passe à la racine.
- **Groupe non créé** (exclu, ou homonyme d'un produit existant et ignoré) : ses déclinaisons sont créées à la racine. On ne les rattache pas au produit existant homonyme.
- **Le groupe n'a pas de prix, mais reste `available`** (`AllPricesZero` faux). Le statut `removed_from_menu` masquerait ses enfants partout, `GetMenu` ne rattachant les sous-produits qu'à des parents visibles.
- **TVA du groupe :** la nature de sa première déclinaison, parce que `tva_*_id` est NOT NULL, même pour un produit sans prix.
- **Les groupes précèdent tous les autres produits**, dans le canonique puis dans le plan de commit (`resolveGroups`), pour que chaque parent soit inséré avant ses enfants.
- **Catégorie :** une déclinaison est placée dans la catégorie de son groupe.

### D4 — Nature et TVA
- Chaque produit reçoit une nature (`food`, `hot_drink`, `soft_drink_served`, `soft_drink_sealed`, `packaged_food`, `alcohol`, `other`). Ses trois taux sont tirés de la table validée (`KindTvaRates`, cadrage § 5.7) et posés dans les champs canoniques `TvaRate*`. La suite du pipeline les résout en `tva_id` exactement comme pour Zelty.
- **La nature se change en relecture** : décision `kind_per_product`. Au commit, `applyAIDecisions` recalcule les trois taux sur une copie du canonique ; le snapshot n'est pas modifié. Un nouveau couple taux + canal se résout directement : le plan retombe sur la résolution directe hors `tva_mapping`.
- **`other` n'a aucun taux** : le commit bloque (`tva_rate_unresolved`) tant que la nature n'est pas précisée.
- **Confirmation obligatoire :** sans `tva_confirmed: true` dans les décisions, le commit d'un import `ai_photo` est refusé (`tva_not_confirmed`). La preview ne le propose jamais à vrai.
- **Décisions invalides refusées :** nature inconnue, produit absent du lot, rattachement à autre chose qu'un groupe, groupe rattaché à un groupe (`invalid_kind_decision`, `invalid_group_decision`).

### D5 — Prix
- Un prix unique lu sur la carte est recopié sur les trois canaux, comme pour Zelty.
- **Prix absent, illisible ou supérieur à 10 000 €** : prix 0, donc statut `removed_from_menu` (comportement existant des lignes sans prix), avec le problème « prix absent ou illisible » ajouté à la ligne.

### D6 — Groupes d'options
- **Clé de fusion :** nom + liste des options. Deux « Suppléments » aux options différentes (burgers, pizzas) restent distincts.
- **Options :** dédoublonnées par libellé ; supplément négatif ou aberrant ramené à 0.
- **Bornes proposées ramenées dans l'intervalle valide :** 0 ≤ min ≤ max ≤ nombre d'options ; un max nul ou trop grand ouvre à toutes les options. `is_required` vaut `min > 0`.
- **Rattachement :** les groupes d'options sont rattachés aux produits qui les citent (`AttributeExternalIDs`, déjà géré par le commit) ; une référence inconnue est ignorée.

### D7 — Métadonnées de relecture
Confiance (`high` / `medium` / `low`, toute autre valeur devient `low`), problèmes signalés et numéro de photo sont exposés dans la preview (`confidence`, `issues`, `source_photo`, `kind`, `is_group`, `parent_external_id`), sans aller en base.
- Une ligne à faible confiance, ou qui signale un problème, produit un avertissement `ai_low_confidence`.
- Formules détectées (`ai_formula_not_created`, avec leur prix) et remarques du modèle sur une photo (`ai_photo_warning`) : signalements de la source, repris en fin de liste des avertissements.

### D8 — Schéma JSON envoyé à l'API
`AIMenuPageSchema`, une page par photo.
- Chaque objet a `additionalProperties: false` et déclare toutes ses propriétés comme requises. Un champ facultatif s'exprime par `anyOf` avec `null`.
- L'enum des natures suit `AllProductKinds` (vérifié par test).
- Le schéma est une constante transmise telle quelle : la grammaire compilée reste en cache côté API.

---

## 3. Implémentation (partie 3a)

| Fichier | Contenu |
|---|---|
| [importer/kind.go](../internal/modules/menu/importer/kind.go) (nouveau) | `ProductKind`, `KindTvaRates` |
| [importer/ai_menu.go](../internal/modules/menu/importer/ai_menu.go) (nouveau) | Contrat `AIMenuPage`, `AIMenuPageSchema`, `DecodeAIMenuPage`, `BuildAIMenuImport` |
| [importer/ai_decisions.go](../internal/modules/menu/importer/ai_decisions.go) (nouveau) | `applyAIDecisions`, `resolveGroups`, blocages de la porte IA |
| [importer/models.go](../internal/modules/menu/importer/models.go) | `CanonicalProduct` : `IsGroup`, `ParentExternalID`, `Kind`, `Confidence`, `Issues`, `SourcePhoto` ; `IntermediateImport.SourceWarnings` ; `ImportDecisions` : `kind_per_product`, `group_per_product`, `tva_confirmed` (tous facultatifs, vides pour les autres portes) |
| [importer/preview.go](../internal/modules/menu/importer/preview.go) | Champs de relecture de la porte IA, décisions proposées, avertissements |
| [importer/commit_plan.go](../internal/modules/menu/importer/commit_plan.go) | Application des décisions, `tva_not_confirmed`, `PlannedProduct.IsGroup` / `ParentExternalID` / `EmptyGroup`, `resolveGroups` |

## 4. Vérifications (partie 3a)

| Vérification | Résultat |
|---|---|
| [ai_menu_test.go](../internal/modules/menu/importer/ai_menu_test.go) | OK. Couvre : contraintes du schéma ; fusion des catégories entre photos ; groupes (≥ 2 déclinaisons, en tête, sans prix mais `available`, TVA de la première déclinaison) ; groupe dissous ; doublon entre photos ; prix recopié et TVA par nature ; prix absent ; nature et confiance inconnues ; groupes d'options (fusion, bornes, dédoublonnage) ; formules ; avertissements de photo ; lot vide ; décisions proposées et avertissements de la preview |
| [ai_decisions_test.go](../internal/modules/menu/importer/ai_decisions_test.go) | OK. Couvre : `tva_not_confirmed` ; `other` bloque ; TVA résolue par nature ; changement de nature → TVA recalculée, canonique d'origine intact ; groupe réduit à une déclinaison non créé ; dégroupage ; décisions invalides refusées |
| Tests existants du paquet `importer` et du module `menu` | OK, inchangés : les nouveaux champs sont vides pour les autres portes |

## 5. Partie 3b — écriture des groupes

[import_commit_repository.go](../internal/modules/menu/import_commit_repository.go), `materializeProductsTx` :
- `is_product_group` est posé pour un produit groupe (`CreateProductPayload.IsProductGroup`, déjà géré par `insertProductTx`) ;
- chaque déclinaison est rattachée à son groupe par `UPDATE products SET by_product_of = …` juste après sa création, dans la même transaction. C'est la même requête que le chemin unitaire (`UpdateProduct`) ;
- un groupe absent au moment du rattachement fait échouer tout le lot. Ce cas ne doit pas se produire, le plan plaçant les groupes en tête.

**Test d'intégration** `TestImportCommit_Postgres_AIPhotoGroups`, exécuté **sur staging le 2026-09-29** : OK. Il vérifie :
- le refus sans TVA confirmée, sans rien écrire ;
- 4 produits créés, dont le groupe (`is_product_group`, `available`, prix 0) ;
- 2 déclinaisons rattachées, 1 produit à la racine ;
- le groupe d'options rattaché au bon produit ;
- `GetMenu` rend le groupe avec ses 2 sous-produits.

**Échecs préexistants des autres tests d'intégration d'import** (vérifiés sur le commit de base `f3ea367`, hors chantier, non corrigés) :
- `ZeroPricedProducts…` et `RollsBackEntireBatch…` : l'intitulé de TVA de test (`itest-import-<suffixe>-TAKE_AWAY-5.5`) dépasse `tva_title varchar(30)` sur staging. Mon test utilise un suffixe court (`ai`) pour cette raison ;
- `EndToEndAndIdempotent` : il attend l'identifiant de TVA qu'il vient de créer, mais staging contient déjà de vrais taux au même pourcentage, et le résolveur retient le premier (`tva_in_id = 5`).

---

## 6. Parties 3c et 3d — brouillons, crédits, service, endpoints

### Décisions

**D9 — Le snapshot Redis de la preview est gardé ; le brouillon garde la lecture IA** *(écart au cadrage)*
- Le cadrage prévoyait un jeton de commit pointant vers le brouillon. On garde plutôt le mécanisme existant : le brouillon (`menu_import_drafts.pages`) conserve la sortie IA validée de chaque photo, et **chaque ouverture** (`GET /menu/import/ai/{id}`) refusionne les photos, recalcule la preview et redépose un snapshot Redis de 30 min.
- Le commit reste `POST /menu/import/commit`, inchangé, avec un ajout minimal : `PreviewSnapshot.DraftID`, et le crochet `ImportService.OnDraftCommitted` qui marque le brouillon importé.
- *Pourquoi :* aucune modification du chemin de commit, déjà testé. La preview est aussi toujours confrontée à l'existant **du moment** : un produit créé entre-temps est vu.
- *Limite :* les arbitrages en cours de relecture (natures modifiées, exclusions…) vivent dans le back-office et ne sont pas enregistrés côté API. Un restaurateur qui reprend un brouillon le lendemain repart des propositions. Enregistrer ces décisions dans le brouillon sera possible plus tard, sans migration (colonne jsonb).

**D10 — Crédits**
- Le crédit est **réservé à la création** du brouillon (`consumes_credit = true`), ce qui compte aussi pour une extraction en cours.
- Il est **rendu si aucune photo n'est lue** (échec technique, refus du modèle, lecture interrompue).
- Une **relance ne coûte rien**. Seul cas limite : un brouillon entièrement en échec, donc sans crédit décompté, puis relancé avec succès, consomme alors son crédit, puisque c'est sa première lecture réussie.
- Solde = total (surcharge du staff, sinon `AI_MENU_OCR_DEFAULT_CREDITS`, 10 par défaut) − brouillons décomptés.
- **Une seule extraction en cours par marchand**, garantie en base par un index unique partiel : même deux requêtes simultanées ne passent pas (409).

**D11 — Traitement asynchrone et reprise**
- `POST /menu/import/ai` dépose les photos dans R2, réclame le brouillon (`pending` → `processing`) et répond 202 ; la lecture tourne dans une goroutine (motif existant : goroutine + `recover`).
- **Une requête `menu_ocr` par photo**, 3 photos en parallèle au plus (image, schéma `AIMenuPageSchema`, consigne fixe `menuOCRSystemPrompt`). La progression est enregistrée après chaque photo, et `updated_at` sert de pouls.
- **Fin de lecture :** `ready` dès qu'au moins une photo est lue ; `failed` sinon, avec le crédit rendu.
- **Nettoyage** (`StartMaintenance`, toutes les 5 min et au démarrage) :
  - une lecture sans progrès depuis 10 min (redémarrage du serveur) passe en `failed`, sans crédit ; les photos déjà lues sont conservées ;
  - les brouillons non importés expirent à 30 jours ;
  - les photos R2 sont supprimées 30 jours après import ou expiration (Q8).
- **Pourquoi une boucle dans le service** plutôt qu'une tâche de `cmd/api/tasks.go` : le `TasksManager` n'a pas accès au service d'import, et `internal/tasks` contenait des modifications locales en cours.
- **La boucle est inactive tant que `menu_ocr` est fermée.** Le code peut donc être déployé avant la migration 161.

**D12 — Photos**
- Champ multipart `photos`, répété : 1 à 10 photos, 7 Mo par photo, 20 Mo au total. Le back-office normalise en JPEG de 2576 px au plus.
- **Le type réel est vérifié** (`http.DetectContentType`) : JPEG uniquement. HEIC, PNG et PDF sont refusés (`photo_not_jpeg`).
- **Stockage :** bucket R2 privé, clé `menu-import/<marchand>/<brouillon>/<n>.jpg`. La réponse donne pour chaque photo un lien signé valable une heure, pour l'afficher à côté des lignes en relecture.

**D13 — Messages, erreurs, permissions**
- Les erreurs par photo sont écrites **pour le restaurateur** (« photo trop chargée, lecture incomplète : reprenez cette partie de la carte en deux photos », « cette photo n'a pas pu être analysée »…) ; le détail technique va dans les journaux, avec l'usage (modèle, jetons, latence), enregistré aussi dans le brouillon.
- **Codes HTTP :**

  | Code | Cas |
  |---|---|
  | 202 | lecture lancée ou relancée |
  | 400 | `missing_photos`, `too_many_photos`, `photo_too_large`, `photo_not_jpeg` |
  | 402 | `import_ai_no_credits` |
  | 404 | `import_ai_draft_not_found` |
  | 409 | `import_ai_already_running`, `import_ai_draft_not_retryable` |
  | 503 | `import_ai_disabled` : tâche fermée, ou R2 privé / registre IA indisponibles |

- **Permissions :** les routes marchand sont sous `permission.CatalogManage`, comme les autres routes d'import. Le cadrage disait `HasMenuAccess`, mais ce sont les routes d'import réelles qui font foi. La recharge de crédits est sous `/admin` (`RequirePlatformAdmin`).

### Endpoints

| Méthode | Route | Rôle |
|---|---|---|
| POST | `/menu/import/ai` | Dépôt des photos, lecture lancée (202) |
| GET | `/menu/import/ai/{id}` | État par photo (liens signés) ; preview d'import quand le brouillon est prêt |
| POST | `/menu/import/ai/{id}/retry` | Relance des photos en échec (gratuite) |
| GET | `/menu/import/drafts` | Brouillons ouverts + solde de crédits |
| DELETE | `/menu/import/drafts/{id}` | Abandon |
| POST | `/menu/import/commit` | Existant : validation de la preview (`decisions.tva_confirmed: true` requis) |
| PUT | `/admin/merchants/{id}/menu-ocr-credits` | Staff : `{"credits": n}`, nombre total de crédits du marchand |

### Implémentation

| Fichier | Contenu |
|---|---|
| [161_menu_import_drafts.up.sql](../migrations/todo/161_menu_import_drafts.up.sql) / `.down.sql` | Tables `menu_import_drafts` et `menu_import_ai_credits`, index unique partiel |
| [import_ai_models.go](../internal/modules/menu/import_ai_models.go) | Brouillon, photos, réponses HTTP |
| [import_ai_repository.go](../internal/modules/menu/import_ai_repository.go) | `AIDraftRepository` (Postgres) |
| [import_ai_prompt.go](../internal/modules/menu/import_ai_prompt.go) | Consigne de lecture d'une photo |
| [import_ai_service.go](../internal/modules/menu/import_ai_service.go) | `AIImportService` : extraction, lecture, preview, relance, crédits, nettoyage |
| [import_ai_handler.go](../internal/modules/menu/import_ai_handler.go) | `AIImportHandler` |
| [import_service.go](../internal/modules/menu/import_service.go), [import_commit_service.go](../internal/modules/menu/import_commit_service.go), [importer/snapshot.go](../internal/modules/menu/importer/snapshot.go) | `buildAndStoreForDraft`, `PreviewSnapshot.DraftID`, crochet `OnDraftCommitted` |
| [r2/client.go](../internal/infrastructure/r2/client.go) | `GetFile` (relecture d'une photo pour une relance) |
| [config/import_ai.go](../internal/config/import_ai.go), [config.go](../internal/config/config.go) | `AI_MENU_OCR_DEFAULT_CREDITS` |
| [cmd/api/routes.go](../cmd/api/routes.go) | Branchement (nil explicites si R2 privé ou registre IA absents), routes |

---

## 7. Vérifications et suite

| Vérification | Résultat |
|---|---|
| [import_ai_service_test.go](../internal/modules/menu/import_ai_service_test.go) : 12 tests, faux brouillons / photos / modèle en mémoire | OK. Couvre : extraction puis preview (un appel par photo, image + schéma, sans température, lien signé, snapshot relié au brouillon, crédit décompté) ; crochet de commit ; tâche fermée ou stockage absent → 503 ; crédits épuisés ; rien de lu → crédit rendu, refus noté ; échec partiel (`max_tokens`) puis relance d'une seule photo relue depuis R2, sans nouveau crédit ; relance impossible sans photo en échec ; sortie IA illisible ; une extraction en cours par marchand ; purge des photos ; validation des photos (400) et codes 503 / 402 / 202 du handler |
| Chaque commit de l'étape, isolément (worktree propre) | build + vet + tests `menu`, `importer`, `config`, `migrations` : OK |
| Suite unitaire complète | Mêmes échecs préexistants qu'avant ce chantier (`planning`, `ubereats`), aucun nouveau |
| Détecteur de concurrence (`-race`) | **Non exécuté** : il demande cgo, absent sur ce poste |
| **Migration 161 sur staging** | **Appliquée le 2026-09-29** avec l'accord d'Ilies : 2 tables, 13 colonnes et 3 index pour `menu_import_drafts`. **Non enregistrée dans `schema_migrations`** : cette table (migration 123) n'existe pas sur staging, cas prévu par `WarnUnrecordedMigrations`. En production, l'enregistrer après application si la table y existe |
| [import_ai_postgres_integration_test.go](../internal/modules/menu/import_ai_postgres_integration_test.go), exécuté **sur staging** | OK. `TestAIDraftRepository_Postgres` couvre : création ; cloisonnement par marchand ; index « une extraction en cours » (création et relance) ; réclamation unique ; pages jsonb relues à l'identique ; clôture ; décompte des crédits (un échec ne compte pas) ; surcharge posée puis mise à jour ; brouillons ouverts ; abandon ; import ; purge ; reprise des lectures interrompues ; expiration. `TestAIImport_Postgres_EndToEnd` : lecture de deux photos (faux modèle), preview sur les données réelles, commit, brouillon marqué importé, crédit décompté, déclinaisons rattachées. Aucune ligne de test restante après exécution (vérifié) |
| **Appel réel à l'API Anthropic** | **Non fait** (pas de clé en local) |

**Incident évité pendant le commit :** en ne commitant que mes blocs de `routes.go`, un patch sans contexte a placé la route admin des crédits dans le groupe `/subscriptions`. Je l'ai vu en contrôlant l'index avant de commiter, puis reconstruit l'index à partir du fichier moins les lignes locales en cours d'Ilies. Le commit est correct.

### Pour tester sur staging
1. ~~Appliquer la migration 161 sur staging~~ : fait le 2026-09-29.
2. Sur staging, définir `AI_TASK_MENU_OCR_ENABLED=true` et vérifier `ANTHROPIC_API_KEY` et le bucket R2 privé. Au démarrage, la ligne `tâche IA` des journaux doit montrer `menu_ocr` active sur `claude-opus-5-5`.
3. Sans le back-office (étape 4), on peut tester à la main :
   ```bash
   curl -X POST "$API/menu/import/ai" -H "Authorization: Bearer $TOKEN" \
     -F photos=@carte-1.jpg -F photos=@carte-2.jpg
   curl "$API/menu/import/ai/<id>" -H "Authorization: Bearer $TOKEN"   # jusqu'à status = ready
   # puis POST /menu/import/commit avec le jeton de la preview et ses décisions,
   # en ajoutant "tva_confirmed": true
   ```
   Les photos doivent déjà être des JPEG (un iPhone envoie du HEIC hors navigateur).
4. Ce test donnera les premières mesures réelles : qualité, latence et jetons par photo (dans les journaux et dans `menu_import_drafts.pages`).

### Commits de l'étape 3

| Commit | Contenu |
|---|---|
| `0f375de` | Porte IA dans le paquet `importer` (contrat, fusion, TVA, groupes) + tests |
| `9a2f23a` | Écriture des groupes en base + test d'intégration |
| `eb8a6cb` | `r2.Client.GetFile` |
| `ab8b313` | Migration 161 et repository des brouillons et crédits |
| `ea6cdc0` | Service, consigne, handler, config, routes + tests |
| *(suivant)* | Cette doc, `CLAUDE.md` |

---

## 8. Retours du test de bout en bout (2026-09-30)

Le test sur staging a réussi : 2 photos, 42 produits, 37,7 s et 26,4 s, environ 0,19 $. La relecture d'Ilies a donné quatre changements. La partie back-office est décrite dans `wello-back-office/docs/import-produits-phase10-photo.md`.

### D14 — Formules : deux cas
- **Formule à choix simples** : ses choix ne sont pas vendus seuls sur la carte (« menu enfant : nuggets ou tenders, compote ou jus »). Le modèle la lit comme **un produit configurable** :
  - prix de la formule, nature `food` ;
  - un groupe d'options par étape de choix, min 1 et max 1 ;
  - un problème « formule convertie en produit à choix : vérifier les choix », donc un avertissement `ai_low_confidence` à la relecture.
  
  Rien ne change dans le pipeline : ce sont un produit et des groupes d'options ordinaires (D6).
- **Formule composée de produits de la carte** (entrée + plat + dessert) : toujours non créée. L'avertissement `ai_formula_not_created` invite à la configurer manuellement, en promotion ou en produit « Menu … » à prix fixe. Il est formulé en action à faire, sans dire que l'outil ne sait pas faire (retour d'Ilies). Le vrai développement des formules reste hors de ce chantier.
- En cas de doute, le modèle garde la formule dans `formulas`, ce qui ne crée rien.
- Seule la consigne change (`menuOCRSystemPrompt`). Elle **n'a pas encore été retestée sur de vraies photos** ; c'est à faire au prochain test staging.

### D15 — Prix et TVA modifiables produit par produit
La relecture remplace la colonne « Nature » et le tableau de résolution des taux par un prix et un taux de TVA modifiables pour chaque canal. Il y a deux nouvelles décisions.
- **`price_per_product`** : `{external_id: {in, take_away, delivery}}`, en centimes.
  - Appliquée par `applyAIDecisions` sur la copie du canonique.
  - Chaque prix doit être entre 0 et 10 000 €. Un prix sur un produit groupe ou sur un produit absent est refusé (`invalid_price_decision`).
  - Un prix saisi recalcule `AllPricesZero`, donc le statut disponible ou `removed_from_menu`.
- **`tva_per_product`** : `{external_id: {in?, take_away?, delivery?}}`, des `tva_id`.
  - Appliquée par `assignChannels`. Elle prime sur le taux déduit de la nature.
  - Chaque id est revérifié contre son canal (`describeID`). Un id inconnu, d'un autre canal ou sur un produit absent donne `invalid_tva_mapping`.
  - Un canal non renseigné garde la résolution habituelle.
  - Une TVA choisie débloque un produit `other`, sans qu'il faille changer sa nature.
- **Groupes :**
  - un produit groupe n'est jamais vendu ; il prend la TVA de sa première déclinaison créée (`resolveGroups`) ;
  - une TVA qu'il ne sait pas résoudre ne bloque plus le lot ;
  - un groupe qui n'est pas créé (moins de deux déclinaisons) ne réclame pas de TVA non plus.
- **Ce qui ne change pas :**
  - `kind_per_product` reste accepté ;
  - la confirmation `tva_confirmed` reste obligatoire ; le back-office la place en fin de page.

| Vérification | Résultat |
|---|---|
| `ai_decisions_test.go` : TVA par produit (produit `other` débloqué, groupe aligné sur ses déclinaisons), TVA partielle (un seul canal), prix saisis (canonique d'origine intact), 7 décisions invalides (prix négatif, aberrant, sur un groupe, sur un produit absent ; TVA d'un autre canal, inconnue, sur un produit absent) | OK |
| `go test ./internal/modules/menu/...` | OK |

### Libellés affichés au restaurateur (2026-09-30)
Les messages de la preview et du commit sont affichés tels quels dans le back-office. Ils ne citent donc plus de nom technique :
- statut `removed_from_menu` → « Retiré du menu », le libellé de la page produits ;
- `product_id` → « n° » ;
- formule non créée → « à configurer manuellement… ».

---

## 9. Ingrédients lus dans les descriptions (2026-09-30)

Demande d'Ilies : générer aussi la composition (ingrédients et association produit ↔ ingrédient) d'après la description. Ses choix :
- ingrédients de la description uniquement, sans rien inventer ;
- quantité 0 ;
- unité propre à l'ingrédient ;
- catégories dans une liste fixe ;
- fusion par nom ;
- relecture minimale ;
- option décochée par défaut ;
- allergènes hors périmètre pour l'instant.

### D16 — Lecture à la demande
- **L'option se coche à l'envoi des photos.** Le champ multipart `ingredients=true` de `POST /menu/import/ai` la transmet ; elle est décochée par défaut.
- **Stockage par photo.** Le choix est enregistré dans chaque photo du brouillon (`pages[].ingredients`), pas dans une colonne : une relance relit la photo dans le même mode, et `menu_import_drafts` n'a pas besoin de migration.
- **Le schéma a toujours le champ.** Chaque produit porte `ingredients` (nom, catégorie, unité), que les ingrédients soient demandés ou non. La consigne système est elle aussi unique. Seul le message de chaque photo dit si les ingrédients sont demandés (sinon, liste vide). Schéma et consigne restent donc identiques, et le cache côté API est préservé.
- **Règles de la consigne :**
  - seulement ce qui est écrit dans la description, rien de déduit du nom (« Margherita » sans description = aucun ingrédient) ;
  - au singulier, sans quantité ni préparation ;
  - même écriture d'un produit à l'autre ;
  - sel, poivre, huile et eau ignorés, sauf s'ils distinguent le plat.
- **Coût.** Quelques jetons de sortie par produit quand l'option est décochée (une liste vide), davantage quand elle est cochée. À mesurer au prochain test.

### D17 — Du texte au catalogue
- **Fusion (`aiIngredientKey`).** Même nom → même ingrédient, sans tenir compte de la casse, des accents ni du pluriel simple (s ou x final des mots de plus de 3 lettres). « Tomates » et « tomate » sont un seul ingrédient, « Pommes de terre » et « pomme de terre » aussi. La première apparition fixe le nom, la catégorie et l'unité.
- **Catégories d'ingrédient**, liste fixe (`AIIngredientCategories`) : Viandes, Poissons et fruits de mer, Fromages et crèmerie, Légumes, Fruits, Épicerie, Sauces et condiments, Boulangerie, Autres. Seules les catégories servies sont déclarées. Une catégorie du même nom chez le marchand est réutilisée, par la mécanique existante de la porte « autre établissement ».
- **Unité.** Le modèle choisit parmi PCE, G, KG, L, ML, CL : G pour ce qui se pèse, CL ou ML pour un liquide, PCE pour ce qui se compte. Le code est traduit en `unit_of_measure.id` par une lecture de la table (`UnitIDsByCode`), car les identifiants sont des données, pas des constantes. Un code inconnu retombe sur PCE. Sans PCE, aucun ingrédient n'est repris et l'avertissement `ai_ingredients_skipped` le signale.
- **Composition.**
  - Chaque ligne a une quantité 0, l'unité de l'ingrédient et les trois canaux actifs : elle décrit le produit sans toucher au stock.
  - Rappel : un ingrédient passé en rupture rend indisponibles les produits qui le contiennent (`orders/repository.go`). C'est une raison de plus de n'importer que ce qui est écrit.
- **Réutilisation.** Un ingrédient existant du même nom (`NormalizeLabel` : casse et espaces) est réutilisé au lieu d'être recréé. C'est le comportement existant. Limite : « Tomates » chez le marchand ne rejoint pas « Tomate » lu sur la carte. La consigne demande le singulier pour limiter ce cas.
- **Preview.**
  - `component_external_ids` sur chaque produit photo ;
  - `components` et `component_categories` : liste existante, avec l'action create ou reuse_existing.

### D18 — Relecture et commit
- **Décision `ingredients_per_product`** : `{external_id: [component_external_id…]}`, les ingrédients gardés pour ce produit.
  - Produit absent de la décision : tous ses ingrédients sont gardés.
  - On ne peut que retirer : un ingrédient non lu pour ce produit, ou un produit absent, bloque le commit (`invalid_ingredient_decision`).
- **Ingrédients inutilisés non créés** (`pruneUnusedComponents`, porte IA seulement). Après la résolution des produits, le plan retire les ingrédients qu'aucun produit créé n'utilise plus, puis les catégories devenues vides. Cela couvre les produits exclus, les homonymes ignorés et les ingrédients retirés partout. La porte « autre établissement » garde son comportement : elle reprend tout le catalogue.
- **L'écriture en base est inchangée.** Composants, catégories, `recipes` et `requires` passent par le code de la porte « autre établissement », qui n'est pas propre à une porte.

| Vérification | Résultat |
|---|---|
| `ai_ingredients_test.go` | OK. Couvre : enums du schéma alignés sur les listes Go ; fusion (casse, accents, pluriels) ; unité de la première apparition ; repli PCE ; aucun ingrédient sans unités (avertissement) ; aucun ingrédient sans description lue ; plan avec les ingrédients ; ingrédient retiré partout non créé, catégorie vide non créée ; ingrédients d'un produit exclu non créés ; 3 décisions invalides |
| `import_ai_service_test.go` | OK. Option transmise dans le message de chaque photo, consigne système inchangée, choix gardé par photo, preview avec ingrédients et composition ; option absente = « non demandés » |
| `TestAIImport_Postgres_EndToEndWithIngredients` | **Écrit, pas exécuté** : il écrit sur staging (données de test nettoyées) ; il attend l'accord d'Ilies. Il vérifie : lecture des unités, 3 ingrédients, 3 catégories, 3 lignes de composition à quantité 0, unité proposée conservée |
| Consigne sur de vraies photos | **Non testée** : prochain test staging |
