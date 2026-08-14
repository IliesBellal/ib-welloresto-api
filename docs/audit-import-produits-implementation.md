# Audit — Feature « Importer des produits », état de l'implémentation réelle

Date : 2026-08-14 · Branche : `staging` · Périmètre : `ib-welloresto-api` + `wello-back-office`

Ce document décrit **l'implémentation telle qu'elle existe aujourd'hui dans le code**, pas l'état
« à construire » de [`docs/audit-import-produits.md`](audit-import-produits.md) (audit préalable, phase 0).
Sources complémentaires lues pour contexte de décision : les cinq docs de phase
([phase 3](import-produits-phase3-modele-canonique.md), [phase 4](import-produits-phase4-preview.md),
[phase 5](import-produits-phase5-commit.md), [phase 6](import-produits-phase6-template.md)) et
[docs/migration-postgres/080-081-import-provider-mappings.md](migration-postgres/080-081-import-provider-mappings.md).

Objectif de ce rapport : servir de référence de conception pour construire une fonctionnalité équivalente
**d'import de clients**. Il documente donc autant « comment c'est fait » que « ce qui est réutilisable tel
quel » (§14).

Convention : chemins relatifs à la racine du dépôt cité. `ib-welloresto-api` sauf mention contraire ;
`wello-back-office` explicité par un préfixe `[BO]`.

---

## 0. Flux de bout en bout, tel qu'il existe dans le code

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ÉTAPE 1 — Point d'entrée (3 portes, wello-back-office)                 │
│                                                                           │
│  [BO] pages/Menu.tsx (dropdown « Nouveau Produit »)                     │
│    ├─ "Importer des produits"     → ProductImportDialog (step: choose)  │
│    └─ "Créer plusieurs produits"  → ProductImportDialog (step: manual)  │
│                                                                           │
│  ImportDoorPicker propose 3 chemins :                                   │
│    A. Envoyer un fichier (Zelty export, ou modèle Wello rempli)         │
│    B. Télécharger le modèle Wello vierge (.xlsx) → le remplir → chemin A│
│    C. Saisir les produits à la main dans une grille                     │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
        ┌───────────────────────────┴──────────────────────────┐
        │ A/B : fichier .xlsx                    │ C : grille manuelle │
        ▼                                          ▼
┌─────────────────────────────┐        ┌──────────────────────────────┐
│ POST /menu/import/preview    │        │ POST /menu/import/preview     │
│ multipart: provider + file   │        │ application/json:             │
│                               │        │  {"products":[...]}           │
└─────────────────────────────┘        └──────────────────────────────┘
        │                                          │
        ▼                                          ▼
  ImportProvider.Parse(io.Reader)          BuildManualImport(products)
  (ZeltyProvider | WelloGenericProvider)   (importer/manual.go)
        │                                          │
        └──────────────────┬───────────────────────┘
                            ▼
              *IntermediateImport (modèle canonique)
              (Categories, Tags, Products, Attributes —
               le parser ne décide rien : ni TVA, ni catégorie,
               ni dédup, ni collision)
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  ÉTAPE 2 — PREVIEW (dry-run, aucune écriture)                           │
│                                                                           │
│  ImportService.buildAndStore (import_service.go)                        │
│   1. LoadImportPreviewLookups (9 SELECT séquentiels, lecture seule) :   │
│      tva_categories, productcateg, tags, products, configurable_        │
│      attributes, + 4 tables import_*_mapping (idempotence)             │
│   2. importer.BuildPreview(imp, lookups)  — fonction pure :             │
│      résout la TVA, classe les libellés catégorie/tag, détecte les      │
│      doublons de nom, détecte les entités déjà importées, calcule les   │
│      backfills (canal fermé, prix, TVA)                                 │
│   3. Dépose un PreviewSnapshot (canonique + décisions proposées) dans   │
│      Redis, clé menu:import:preview:{merchantID}:{token}, TTL 30 min    │
│   4. Renvoie PreviewResult (résumé + décisions proposées + warnings)    │
└─────────────────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  ÉTAPE 3 — WIZARD DE VÉRIFICATION [BO] (ImportReviewStep.tsx)           │
│                                                                           │
│  5 sections, dans l'ordre où elles se conditionnent :                   │
│   1. Catégories et tags  → classification par libellé                  │
│   2. TVA                 → résolution (taux, canal) → tva_id            │
│   3. Produits sans catégorie → affectation manuelle / en masse          │
│   4. Noms déjà utilisés  → skip / import_anyway par produit             │
│   5. Déjà importés       → skip / recreate par produit (ou en masse)    │
│  + panneau de warnings non bloquants                                    │
│  Le bouton « Importer N produit(s) » n'est actif que si canCommit       │
│  (précalculé côté front, miroir des blocages du backend — §7)           │
└─────────────────────────────────────────────────────────────────────────┘
                            │
                            ▼
              POST /menu/import/commit  { token, decisions }
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  ÉTAPE 4 — COMMIT (seule étape qui écrit)                               │
│                                                                           │
│  ImportService.CommitImport (import_commit_service.go)                  │
│   1. Relit le snapshot Redis, vérifie MerchantID                        │
│   2. Recharge les lookups (base à jour, pas ceux de la preview)         │
│   3. importer.BuildCommitPlan(imp, decisions, lookups) — fonction pure, │
│      revérifie TOUT ce que le client renvoie (rien n'est cru sur        │
│      parole) → CommitPlan ou []CommitBlocker                            │
│   4. Si blocages → 422, AUCUNE écriture, token conservé (l'utilisateur  │
│      corrige et rejoue)                                                 │
│   5. Sinon → MaterializeImportTx : 1 seul dbutils.RunInTx, ordre         │
│      Catégories → Tags → Attributs → Produits, écrit aussi les 5 tables │
│      import_*_mapping. Tout ou rien.                                    │
│   6. Hors transaction, une fois pour tout le lot : setMenuUpdated +     │
│      invalidation cache Redis + suppression du token (consommé)        │
└─────────────────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  ÉTAPE 5 — RÉSULTAT [BO] (ImportDoneStep.tsx)                           │
│  Tableau créé / réutilisé / ignoré par type d'entité + options créées   │
│  onImported() → la page Menu recharge sa liste (pas de cache react-query│
│  sur le menu, rechargement manuel)                                      │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 1. Points d'entrée

### 1.1 Routes API

| Méthode + URL | Handler | RBAC |
|---|---|---|
| `POST /menu/import/preview` | `ImportHandler.PreviewImport` [import_handler.go:38](internal/modules/menu/import_handler.go#L38) | `RequirePermission(HasMenuAccess)` |
| `POST /menu/import/commit` | `ImportHandler.CommitImport` [import_handler.go:162](internal/modules/menu/import_handler.go#L162) | `RequirePermission(HasMenuAccess)` |
| `GET /menu/import/template?provider=<slug>` | `ImportHandler.DownloadImportTemplate` [import_handler.go:228](internal/modules/menu/import_handler.go#L228) | `RequirePermission(HasMenuAccess)` |

Enregistrement : [cmd/api/routes.go:712-717](cmd/api/routes.go#L712-L717). Commentaire explicite dans le code :
« Seule route du bloc `/menu` à porter un contrôle RBAC » (le reste du bloc `/menu` n'a que
`authMiddleware`, écart hérité de l'audit préalable §1.7, non corrigé ici).

Câblage DI : [cmd/api/routes.go:449-453](cmd/api/routes.go#L449-L453) — `ImportService` reçoit
`menuRepoLegacy` deux fois (comme `reader` et `writer`), `importerModule.DefaultRegistry()`, `redisClient`
et `tagsRepo`. Dépendances **volontairement disjointes** de `MenuService` (pas de Deliveroo, pas d'Uber
Eats, pas de synchronisation de statuts).

### 1.2 Pages / composants UI

| Composant | Chemin (wello-back-office) | Rôle |
|---|---|---|
| Point d'entrée | `src/pages/Menu.tsx` (dropdown « Nouveau Produit », lignes ~301-318) | Ouvre `ProductImportDialog`, avec `initialDoor: 'manual'` pour « Créer plusieurs produits » ou `undefined` pour « Importer des produits » |
| Orchestrateur du wizard | `src/components/menu/import/ProductImportDialog.tsx` | Modale (Dialog), plein écran sur mobile ; calibre par étape (`STEP_DIALOG_CLASS`) |
| Étape 0 | `src/components/menu/import/ImportDoorPicker.tsx` | Les 3 portes |
| Étape fichier | `src/components/menu/import/ImportProviderStep.tsx` | Choix provider + upload/drag-drop |
| Étape saisie manuelle | `src/components/menu/import/ImportManualStep.tsx` + `manual/ImportManualRow.tsx` | Grille éditable |
| Étape vérification | `src/components/menu/import/ImportReviewStep.tsx` + 6 sous-composants `review/*` | Wizard de décisions |
| Étape résultat | `src/components/menu/import/ImportDoneStep.tsx` | Résumé du commit |

Aucun job/CLI : le pipeline est entièrement synchrone, déclenché par requête HTTP.

---

## 2. Formats de fichiers supportés

**Un seul format binaire : `.xlsx`** (OpenXML). Aucun CSV. Aucun `.xls` legacy.

**Lib de parsing** : `github.com/xuri/excelize/v2 v2.11.0` — première (et seule) bibliothèque Excel du
dépôt, ajoutée pour cette feature ([go.mod:23](go.mod#L23)). Utilisée en lecture (`readSheetRows`,
[xlsx.go:31](internal/modules/menu/importer/xlsx.go#L31)) comme en écriture (génération du template,
[template.go](internal/modules/menu/importer/template.go)).

**Deux providers fichier**, résolus par slug via `Registry` (interface `ImportProvider{Slug(), Parse}`,
[provider.go:24](internal/modules/menu/importer/provider.go#L24)) :

| Slug | Provider | Format |
|---|---|---|
| `zelty` | `ZeltyProvider` [zelty.go](internal/modules/menu/importer/zelty.go) | Export tiers, une seule feuille « longue » (12 colonnes), sections Tag/Produit/Option/Option Value entrelacées, routées sur la colonne `Type` |
| `wello-generic` | `WelloGenericProvider` [wello_generic.go](internal/modules/menu/importer/wello_generic.go) | Template Wello : 1 ligne d'en-tête + 1 ligne par produit |

**Troisième porte, sans fichier** : `ManualSlug = "manual"`, saisie JSON traduite en canonique par
`BuildManualImport` [manual.go:45](internal/modules/menu/importer/manual.go#L45) — pas de parsing, les
valeurs arrivent déjà typées.

**Template téléchargeable** : `GET /menu/import/template?provider=wello-generic` (seul `wello-generic`
l'implémente, via l'interface optionnelle `TemplateProvider`
[template.go:33](internal/modules/menu/importer/template.go#L33) ; `zelty` ne l'implémente pas, ce qui
suffit à l'exclure — pas de liste à maintenir). Le classeur généré a 2 feuilles : « Produits » (position
contractuelle, en-tête figé) et « Mode d'emploi » (colonne par colonne + règles générales). Nom de fichier :
`wello-modele-import-produits.xlsx`.

**Encodage / en-têtes** :
- Excelize lit directement l'OpenXML, pas de souci d'encodage de type CSV.
- `readSheetRows` lit **toujours la première feuille** (`GetSheetList()[0]`), jamais un nom en dur.
- Les en-têtes sont reconnus via `foldHeader` (accents repliés, casse et espaces normalisés) — un
  restaurateur qui retape « Categorie » sans accent est reconnu.
- `excelize` tronque les cellules vides de fin de ligne : toute lecture de colonne passe par `cellAt`, qui
  tolère les lignes courtes ([xlsx.go:53](internal/modules/menu/importer/xlsx.go#L53)).
- `ErrInvalidWorkbook` distingue un flux qui n'est pas un `.xlsx` valide (ex. CSV renommé) → 400, pas 500.

**Taille de fichier** : `maxImportFileSize = 5 << 20` (5 Mo), constante définie dans le module `menu`
([import_models.go:8](internal/modules/menu/import_models.go#L8)), alignée sur la limite existante des
uploads d'images. Les exports réels observés pèsent ~15 Ko pour 141 produits.

---

## 3. Mapping des colonnes

**Aucun mapping interactif** (pas d'écran « associez votre colonne X à notre champ Y »). Deux stratégies
fixes, par provider :

### 3.1 Zelty — colonnes fixes par position

Positions figées ([zelty.go:15-24](internal/modules/menu/importer/zelty.go#L15-L24)) : `ID` (0), `Type` (1),
`Nom` (2), `Prix` (3), `TVA` (4), `TVA emporte` (5), `TVA livraison` (6), `Tags` (7). Le routage se fait sur
la colonne `Type` (`Tag` / `Produit` / `Option` / `Option Value`), pas sur la position de la ligne dans le
fichier — les exports réels comportent des sections d'en-tête sans aucune ligne de données.

### 3.2 `wello-generic` — alias d'en-tête, table de correspondance

Table `welloGenericAliases` ([wello_generic.go:69-90](internal/modules/menu/importer/wello_generic.go#L69-L90))
associe des libellés d'en-tête (repliés par `foldHeader`) à un champ interne. Exemple : `"prix sur place"`,
`"prix"` → `wgFieldPriceIn`. Colonne dupliquée → la première occurrence gagne.

**Obligatoires** (`welloGenericRequired`, [wello_generic.go:60-64](internal/modules/menu/importer/wello_generic.go#L60-L64)) :
`Nom`, `Catégorie`, `Prix sur place`. Absente → `ErrMissingColumn`, 400.

**Facultatives** : `Description`, `Prix emporté`, `Prix livraison`, `TVA sur place`, `TVA emporté`,
`TVA livraison`, `Tags`. Une colonne absente laisse le champ à sa valeur neutre (`0` pour un prix, `nil`
pour un taux) — c'est la preview qui réclame la complétion, pas le parser qui rejette.

### 3.3 Saisie manuelle — mapping = structure du formulaire

`ImportPreviewJSONProduct` ([import_models.go:31-45](internal/modules/menu/import_models.go#L31-L45)) : champs
typés, pas de colonnes à faire correspondre. `category` est une **chaîne (nom)**, pas un identifiant.

**Aucune persistance de mapping appris** entre deux imports : chaque fichier est reparsé de zéro sur ses
règles fixes.

---

## 4. Validation des données

### 4.1 Au niveau ligne (fichier)

Faite dans le **parser**, jamais dans la preview :
- `parsePriceCents` ([values.go:59](internal/modules/menu/importer/values.go#L59)) — conversion **sur la
  chaîne**, pas via `float64` (évite l'imprécision binaire de `9,9 * 100`). Tolère virgule et point,
  séparateurs de milliers (espace normal/insécable/insécable étroit/fine), symbole `€`. Cellule vide → `0`,
  pas une erreur (cas légitime des lignes de frais). Montant illisible → erreur dure avec ligne + colonne.
- `parseTvaRate` ([values.go:116](internal/modules/menu/importer/values.go#L116)) — `nil` si cellule vide
  (colonne absente/non renseignée), `*float64` sinon. `nil` ≠ `0` : `0` signifie taux explicitement nul
  (désactivation de canal), distinction conservée jusqu'à la preview. Taux négatif → erreur.
- Champs obligatoires par type de ligne : `Nom`/`ID` pour un produit, tag, groupe d'options, valeur
  d'option Zelty ; `Nom` pour une ligne `wello-generic`.
- Erreurs typées `RowError{Line, Column, Reason}` ([provider.go:79](internal/modules/menu/importer/provider.go#L79)),
  exportées pour que le handler les distingue (400 avec ligne/colonne actionnable) d'une panne serveur (500).

### 4.2 Doublons intra-fichier

- **Nom de produit dupliqué** (`wello-generic`, `manual`) → erreur dure citant la ligne précédente
  ([wello_generic.go:135](internal/modules/menu/importer/wello_generic.go#L135)) : deux lignes homonymes se
  réduiraient au même `external_id` généré, cassant l'idempotence. **Zelty n'a pas cette contrainte** — les
  ID viennent de la source, pas générés.
- **Tag cité deux fois sur une même ligne produit** → dédupliqué silencieusement (`product_tags` a pour PK
  `(product_id, tag_id)`).
- **Libellé Zelty référencé par un produit mais jamais déclaré en section Tags** → tag synthétique généré
  (préfixe `zt-syn-`), marqué `Synthetic: true`, signalé en warning (`tag_synthesized`) plutôt que rejeté ou
  ignoré silencieusement.

### 4.3 Au niveau fichier entier

- `ErrNoProducts` si zéro produit après parsing — garde contre le mauvais fichier/mauvais provider choisi.
- `ErrEmptyWorkbook` si le classeur n'a aucune feuille.
- `ErrMissingColumn` si une colonne obligatoire manque à l'en-tête (`wello-generic`).

### 4.4 Validation métier (preview et commit, pas le parser)

Le parser « ne décide rien » (règle explicite du paquet, [models.go:9-13](internal/modules/menu/importer/models.go#L9-L13)) :
la résolution TVA, la classification catégorie/tag, la détection de doublon contre l'existant et les
collisions de nom sont **entièrement déléguées à la preview et au commit** (voir §5 et §7).

`assignChannels` dans `commit_plan.go` est explicitement documenté comme un **miroir en mémoire** de
`validateProductForCreate` (chemin unitaire de création produit) — même 4 validations (TVA obligatoire,
`tva_id` numérique, catégorie existante et active, 3 taux existants et actifs), mais faites une fois sur
des lookups en mémoire plutôt qu'en 4 requêtes SQL par produit (contrainte du pool 1 connexion). Le
commentaire du code demande explicitement de garder les deux synchronisés en cas d'évolution.

---

## 5. Déduplication / upsert

### 5.1 Deux mécanismes orthogonaux

**a) Idempotence via tables de mapping** (`import_*_mapping`, migration 080) : clé unique
`(merchant_id, provider, external_id)`. Un identifiant externe déjà mappé pour ce provider et ce marchand
→ l'entité est `already_imported`, **ignorée par défaut** au commit (sauf demande explicite de recréation
pour les produits, `ReimportRecreate`). C'est le mécanisme qui rend un ré-import du même fichier sans effet.

Un mapping **survit à la suppression de l'entité qu'il désigne** (rien ne le désactive) : la preview et le
plan de commit détectent ce cas (`mapping_stale` / `RemapExisting`) et **recréent + réaffectent** le
mapping plutôt que de laisser l'import définitivement sans effet.

**b) Déduplication par nom, entièrement applicative** (catégories et tags) : aucune contrainte d'unicité
SQL n'existe sur `productcateg.categ_name` ni `tags.name`. `indexCategoriesByName` /
`indexTagsByName` ([preview.go:869-891](internal/modules/menu/importer/preview.go#L869-L891)) comparent en
`normalizeLabel` (casse + espaces neutralisés, **accents conservés** — `VÉGÉ` ≠ `VEGE`). Match trouvé →
`reuse_existing`, sinon `create`.

### 5.2 Collision de nom produit

Un produit dont le nom (comparé en minuscule, sur les `enabled = TRUE`) existe déjà chez le marchand →
`NameCollision` avec résolution par défaut `skip` ([preview.go:652-662](internal/modules/menu/importer/preview.go#L652-L662)).
L'utilisateur peut choisir `import_anyway` (doublon assumé) dans le wizard.

**C'est ce qui remplace le mécanisme de confirmation Redis à double appel du chemin unitaire de création
produit** (`ErrProductNameAlreadyExistsWithRetry`, décrit dans l'audit préalable §3.6) : celui-ci exige un
second appel identique pour passer outre, inutilisable en lot. L'import détecte et **laisse décider dans le
wizard** au lieu de forcer un rejeu automatique.

Les produits issus d'un import précédent **du même provider** sont exclus de la détection de collision
(`indexProductsByName` retire les `ProductID` déjà présents dans `imported`,
[preview.go:896-913](internal/modules/menu/importer/preview.go#L896-L913)) : c'est le mapping qui fait
autorité, pas le nom.

### 5.3 Le commit ne fait jamais confiance à la preview

`BuildCommitPlan` recharge les lookups **au moment du commit** (pas ceux de la preview, qui peut dater de
30 minutes) et revérifie chaque décision renvoyée par le wizard contre l'état courant : un `tva_id` doit
exister, être actif, et porter le bon `delivery_type` ; une catégorie forcée doit être classée `category` ;
une collision doit avoir été réellement détectée. Sinon → blocage 422, aucune écriture.

---

## 6. Modèle de traitement

**Entièrement synchrone**, aucune file de tâches, aucun job en arrière-plan. Trois appels HTTP successifs
(`preview`, puis N ajustements côté client uniquement, puis `commit`), chacun traité dans le cycle de vie
de sa propre requête.

**Preview** : 9 SELECT séquentiels ([import_repository.go:21](internal/modules/menu/import_repository.go#L21))
— explicitement documenté comme séquentiel à cause du pool MySQL plafonné à **1 connexion ouverte + 1
idle** (contrainte Hostinger, `internal/database/mysql.go`). Aucune parallélisation possible.

**Commit** : **une seule transaction** (`dbutils.RunInTx`, réentrante) englobant catégories → tags →
attributs → produits → 5 tables de mapping. Contraste avec le chemin unitaire de création produit, qui ouvre
une transaction par produit.

**Suivi de progression** : **aucun**. Pas de barre de progression, pas de statut intermédiaire — le fichier
est parsé et prévisualisé en un seul aller-retour HTTP. Les exports réels (141 produits, ~15 Ko) sont traités
en dessous du seuil où ce serait un problème perceptible ; rien dans le code ne gère un fichier plus gros que
ce que 5 Mo / la mémoire du processus peuvent absorber en une requête.

**Limites de taille** : 5 Mo par fichier (§2). Pas de limite explicite sur le nombre de lignes/produits —
seule la mémoire et le temps de requête HTTP la bornent implicitement.

**Snapshot intermédiaire** : Redis, clé `menu:import:preview:{merchantID}:{token}`
([redis_helpers.go], `GetMenuImportPreviewKey`), TTL **30 minutes**
(`models.MenuImportPreviewTTL`). Le token est **consommé** (supprimé) après un commit réussi — un double
envoi ne peut pas rejouer le lot (bien que l'idempotence par mapping le rendrait de toute façon inoffensif).

---

## 7. Gestion des erreurs et reporting

### 7.1 Erreurs de parsing (avant preview)

Toutes remontées en **400**, avec distinction fine par le handler
([import_handler.go:115-146](internal/modules/menu/import_handler.go#L115-L146)) :

| Cas | Code renvoyé |
|---|---|
| Provider absent/vide | `missing_provider` |
| Provider inconnu | `unknown_provider` |
| Fichier absent | `missing_file` |
| Fichier trop lourd / illisible en multipart | `file_too_large_or_invalid` |
| Zéro produit trouvé | `no_products` |
| Colonne obligatoire manquante / classeur vide / pas un `.xlsx` | `invalid_file` (message du parser inclus) |
| Ligne mal remplie (`RowError`, avec ligne + colonne) | `invalid_file_content` |

`isImportRowError` reconnaît spécifiquement les erreurs situant une ligne/colonne — « la seule catégorie
d'erreur que le restaurateur peut corriger lui-même » (commentaire du code).

### 7.2 Warnings de la preview (non bloquants)

7 codes stables (`preview.go:42-49`), utilisés par le back-office pour router vers l'écran de correction
adéquat : `tva_rate_unresolved`, `tva_rate_missing`, `product_needs_category`, `product_name_collision`,
`product_removed_from_menu`, `label_dropped`, `tag_synthesized`. Affichés dans `ImportWarningsPanel`
« pour information — rien de bloquant » (section dédiée du wizard).

### 7.3 Blocages du commit (bloquants, 422)

5 codes stables (`commit_plan.go:11-17`) : `product_needs_category`, `tva_rate_unresolved`,
`product_name_collision_unresolved`, `invalid_tva_mapping`, `invalid_category_decision`. Chaque blocage
porte `{code, ref, message}` — `ref` est l'identifiant externe de l'entité fautive (ou `"<taux>:<canal>"`
pour la TVA), ce qui permet au front de rattacher le message au bon champ (`indexBlockersByRef`
[BO] [lib/importDecisions.ts:251](../../wello-back-office/src/lib/importDecisions.ts#L251)).

### 7.4 Échec partiel / transaction / rollback

**Tout ou rien à l'échelle du commit.** `MaterializeImportTx` exécute tout dans **un seul** `RunInTx` : la
moindre erreur SQL annule l'ensemble, y compris les correspondances `import_*_mapping` déjà écrites dans la
même transaction. Testé explicitement par `RollsBackEntireBatchOnError` (test d'intégration Postgres,
phase 5) : une erreur sur le second produit après écriture de catégories/tags/attributs/premier produit →
**0 ligne** dans les 10 tables concernées.

**Avant le commit**, un lot avec blocages est refusé **entièrement** (422) sans qu'une seule ligne parte en
base — le token n'est **pas consommé**, l'utilisateur corrige les décisions et rejoue le même commit.

**Pas de reprise partielle** : soit tout le lot est écrit, soit rien ne l'est. Un import de 141 produits
dont un seul échoue en cours d'écriture ne laisse pas les 140 autres.

### 7.5 Codes HTTP du commit

| Statut | Cas |
|---|---|
| 200 | Résumé `{created, reused, skipped}` par entité |
| 400 | Corps illisible, token absent |
| 410 | Token inconnu, expiré, déjà consommé, ou d'un autre marchand — **indistingués volontairement** (« les trois cas sont indistinguables — la clé Redis a simplement disparu — et les distinguer renseignerait un appelant sur l'existence d'un token qui n'est pas le sien ») |
| 422 | `{"error":"import_not_committable","blockers":[...]}`, aucune écriture |

---

## 8. Modèle de données

### 8.1 Tables de mapping (staging / historique des correspondances)

Migration [`080_import_provider_mappings.up.sql`](migrations/080_import_provider_mappings.up.sql), 5 tables
identiques en structure, une par type d'entité cible :

| Table | Cible | `wello_id` |
|---|---|---|
| `import_products_mapping` | `products.product_id` | `integer` |
| `import_categories_mapping` | `productcateg.categ_id` (⚠️ **pas** `merchant_categ_id`, résolution applicative) | `integer` |
| `import_tags_mapping` | `tags.tag_id` | `varchar(42)` |
| `import_attributes_mapping` | `configurable_attributes.id` | `varchar(64)` |
| `import_attribute_options_mapping` | `configurable_attribute_options.id` | `integer` |

Colonnes communes : `id` (PK identity), `merchant_id varchar(64)`, `provider varchar(32)`,
`external_id varchar(64)`, `wello_id` (typé selon la cible), `creation_date timestamptz default now()`,
`deletion_date timestamptz null`, `enabled boolean default true`.

**Index UNIQUE `(merchant_id, provider, external_id)`** = clé d'idempotence, **volontairement sans filtre
sur `enabled`** (un mapping désactivé continue de bloquer un ré-import du même `external_id`). **Index
simple `(merchant_id, wello_id)`** = lookup inverse (« d'où vient cette entité ? »), destiné à un futur
rollback par import. **Aucune FK** (convention du dépôt), listées en commentaire dans le `.up.sql`.

Pas de table « staging » intermédiaire au sens classique (pas de zone de dépôt en base avant validation) —
c'est **Redis** qui joue ce rôle via le snapshot (§6), pas SQL.

### 8.2 Pas de colonnes d'import sur les tables cibles

`products`, `productcateg`, `tags`, `configurable_attributes`, `configurable_attribute_options` ne portent
aucune colonne `provider`/`external_id`/`sku` — choix explicite (tables dédiées plutôt qu'élargir des
tables « chaudes » du menu).

### 8.3 Migration annexe

[`081_widen_configurable_attribute_options_title.up.sql`](migrations/081_widen_configurable_attribute_options_title.up.sql) :
`configurable_attribute_options.title` élargi de `varchar(25)` à `varchar(80)` — nécessaire pour que des
libellés d'option réels (ex. « Jambon de Parme 24 mois AOP », 27 caractères) ne fassent pas échouer
l'import (Postgres lève une erreur dure sur dépassement, contrairement à MySQL qui tronquait
silencieusement).

### 8.4 Pas de métadonnées d'import au niveau « lot »

**Aucune table `import_batches` / `import_runs`** : rien ne trace « cet import du 2026-08-14 a créé ces 141
produits comme un ensemble ». Chaque ligne des tables de mapping est indépendante ; le regroupement par
lot n'existe pas. Explicitement noté comme hors V1 dans la phase 5 (« Suivi par lot et annuler l'import
n°X : hors V1 »).

---

## 9. Permissions et sécurité

- **RBAC** : les 3 routes d'import sont les **seules** du bloc `/menu` à porter
  `middleware.RequirePermission(middleware.HasMenuAccess)` — écart volontairement corrigé pour ce chemin
  seul (« un endpoint qui réécrit le catalogue entier d'un coup ne pouvait pas hériter de » l'absence de
  garde du reste du bloc `/menu`, qui reste ouvert par ailleurs).
- **Scoping merchant** : `middleware.UserFromContext(ctx).MerchantID` injecté à chaque étape ; le snapshot
  Redis porte le marchand à la fois dans la clé et dans la valeur (défense en profondeur — le commit
  vérifie les deux, refuse silencieusement en 410 sur discordance, avec un log `WARN` serveur).
- **Anti-abus** :
  - Taille de fichier plafonnée à 5 Mo.
  - TTL de 30 min sur le snapshot (fenêtre d'exploitation d'un token limitée).
  - Token à usage unique (consommé après commit réussi).
  - Aucun rate-limiting spécifique à l'import identifié (pas de throttle par merchant/IP sur ces 3 routes
    au-delà de ce qui s'applique globalement — **[À CLARIFIER]**, non vérifié en dehors du module).
- **Pas d'audit log dédié** : ni `audit_logs` ni `api_request_logs` ne sont câblés spécifiquement pour
  l'import (writes en base, oui, mais pas de trace applicative « qui a importé quoi, quand » au-delà des
  `creation_date` des lignes de mapping et des logs `zap` d'erreur).

---

## 10. UX / parcours utilisateur

### 10.1 Étapes du wizard (état machine côté front)

`ImportStep = 'choose' | 'provider' | 'preview' | 'manual' | 'done'`, géré par le hook
`useProductImport` ([hooks/useProductImport.ts](../../wello-back-office/src/hooks/useProductImport.ts)).
`door: 'provider' | 'manual' | null` retient la porte d'origine pour que « Retour » depuis la vérification
ramène au bon écran plutôt que systématiquement au choix initial.

1. **`choose`** — `ImportDoorPicker` : 3 cartes (envoyer un fichier / télécharger le modèle / saisir à la
   main). Modale `max-w-4xl`.
2. **`provider`** — `ImportProviderStep` : sélection du provider (`Select`), zone de dépôt de fichier avec
   **drag & drop maison** (pas de composant dropzone générique réutilisable — implémentation ad hoc avec
   compteur `dragDepth` pour gérer les événements enfants). Validation côté client de l'extension `.xlsx`
   avant envoi. Bouton « Analyser le fichier » désactivé tant qu'aucun fichier n'est choisi.
3. **`manual`** — `ImportManualStep` + `ImportManualRow` : grille éditable, une ligne = un produit sur deux
   niveaux visuels (nom/description, puis prix/TVA par canal). TVA choisie dans une liste des taux
   réellement configurés chez le marchand (`useQuery` sur `GET /pos/tva_rates`, **seule vraie requête
   cachée par react-query du parcours** — le reste passe par `useMutation` sans cache). Navigation clavier
   (Entrée sur la dernière ligne → ajoute une ligne). Dupliquer une ligne conserve catégorie/prix/TVA, vide
   nom/description (geste répété pour saisir 12 pizzas d'affilée).
4. **`preview`** — `ImportReviewStep` : 4 compteurs en tête (produits à créer, catégories, tags, groupes
   d'options), puis 5 sections séquentielles correspondant aux 4 `ImportDecisions` + panneau warnings.
   Barre d'action collante en bas (`sticky bottom-0`) avec bouton retour et bouton de validation, ce dernier
   affichant le compte restant à trancher (`X sans catégorie · Y TVA à choisir · Z doublon(s)`).
5. **`done`** — `ImportDoneStep` : tableau créé/réutilisé/ignoré par entité, note explicative sur les
   groupes d'options non rattachés (à faire depuis « Options & Suppléments »), boutons « Importer un autre
   fichier » / « Voir mes produits ».

### 10.2 Notifications

- Toasts `sonner` pour le téléchargement du template (succès/échec).
- Pas de notification push/email/websocket liée à l'import — tout est synchrone et visible dans le wizard
  lui-même.
- `onImported()` callback : la page `Menu.tsx` recharge sa liste de produits (`loadData`) après un commit
  réussi — pas d'invalidation react-query car le menu n'est pas sur ce système de cache
  ([BO] confirmé dans `menuImportService.ts`, commentaire « Le menu n'est pas sur react-query »).

### 10.3 Aides à la décision en masse

- « Affecter à tous » pour la catégorie manquante (`assignCategoryToAll`) — explicitement motivé par le cas
  réel des lignes « Frais de livraison » sans catégorie qu'on ne veut pas trancher une par une.
- « Appliquer à tous » pour la résolution des produits déjà importés (`setAllReimportResolutions`) — motivé
  par le cas d'un menu entier supprimé puis réimporté.

---

## 11. Historique et audit

**Consultation** : aucune UI ni endpoint listant les imports passés. Les seules traces sont les lignes des
5 tables `import_*_mapping` elles-mêmes (consultables uniquement en SQL direct, pas via l'API).

**Rollback** : **aucun mécanisme**. Pas de bouton « annuler cet import », pas d'endpoint de suppression en
lot. Explicitement noté hors V1 dans la phase 5 : « Suivi par lot (`import_batches`) et « annuler l'import
n°X » : hors V1 (décision 17). Les correspondances posées ici sont ce qui le rendra possible » — c'est-à-
dire que l'infrastructure (index inverse `(merchant_id, wello_id)`) est posée en prévision, mais rien
n'exploite encore ce lookup pour un rollback réel.

**Upsert / mise à jour** : également hors V1. « Une entité mappée est sautée, pas mise à jour » — réimporter
un fichier modifié ne met pas à jour les produits existants, il les ignore (sauf demande explicite de
recréation pour les produits, ce qui recrée un doublon plutôt que de mettre à jour l'original).

**Logging technique** : `zap` (`logger.FromContext(ctx)`), niveau `Error` sur les pannes serveur du handler
et `Warn` sur les cas suspects (snapshot d'un autre marchand, échec de `setMenuUpdated` post-commit). Pas de
log structuré dédié « import terminé : N produits, M erreurs » au-delà de la réponse HTTP elle-même.

---

## 12. Tests

### 12.1 Backend (Go)

| Fichier | Nb tests (`func Test`) | Portée |
|---|---|---|
| [internal/modules/menu/importer/commit_plan_test.go](internal/modules/menu/importer/commit_plan_test.go) | 22 | `BuildCommitPlan` — rejets, décisions incohérentes, cas nominaux, miroir de `validateProductForCreate` |
| [internal/modules/menu/importer/preview_test.go](internal/modules/menu/importer/preview_test.go) | 17 | `BuildPreview` — résolution TVA, backfill, classification, collisions, idempotence |
| [internal/modules/menu/importer/zelty_test.go](internal/modules/menu/importer/zelty_test.go) | 12 | Parser Zelty, sur les 2 fixtures réelles (`testdata/*.xlsx`) + cas limites synthétiques (`buildXLSX`) |
| [internal/modules/menu/importer/wello_generic_test.go](internal/modules/menu/importer/wello_generic_test.go) | 7 | Parser template |
| [internal/modules/menu/importer/template_test.go](internal/modules/menu/importer/template_test.go) | 6 | Round-trip génération → parse, résolution des en-têtes, disposition des feuilles |
| [internal/modules/menu/importer/values_test.go](internal/modules/menu/importer/values_test.go) | 5 | `parsePriceCents`, `parseTvaRate`, helpers purs |
| [internal/modules/menu/importer/snapshot_test.go](internal/modules/menu/importer/snapshot_test.go) | 5 | (dé)sérialisation du `PreviewSnapshot` |
| [internal/modules/menu/importer/provider_test.go](internal/modules/menu/importer/provider_test.go) | 4 | `Registry` |
| [internal/modules/menu/importer/xlsx_test.go](internal/modules/menu/importer/xlsx_test.go) | 3 | Lecture de feuille, numérotation de ligne |
| [internal/modules/menu/importer/manual_test.go](internal/modules/menu/importer/manual_test.go) | 3 | `BuildManualImport` |
| [internal/modules/menu/import_handler_test.go](internal/modules/menu/import_handler_test.go) | 14 | HTTP : multipart/JSON, 400/401/410/422, garde RBAC composée, `sqlmock` (`ExpectQuery` uniquement sur les chemins de refus — toute écriture non attendue fait échouer le test) |
| [internal/modules/menu/import_commit_postgres_integration_test.go](internal/modules/menu/import_commit_postgres_integration_test.go) | 3 | `//go:build postgres_integration` — end-to-end + idempotence, statut `removed_from_menu`, rollback total sur erreur, contre un vrai Postgres de dev |

**Total** : ~101 tests unitaires + 3 tests d'intégration Postgres (déclenchés séparément, tag de build
dédié, nécessitent `DB_DIALECT=postgres POSTGRES_URL=...`).

Fixtures réelles committées : `internal/modules/menu/importer/testdata/*.xlsx` (2 exports Zelty réels,
107 et 141 produits).

### 12.2 Frontend (TypeScript/React)

**Aucun test trouvé.** Recherche explicite dans `wello-back-office/src` : aucun fichier
`*.test.*`/`__tests__` sous `components/menu/import/`, `hooks/useProductImport*`, ou `lib/manualImport*` /
`lib/importDecisions*`. Le miroir des règles métier backend (`importDecisions.ts` reproduit les conditions
de rejet de `commit_plan.go`) n'est donc **vérifié par aucun test automatisé côté client** — seul le
pré-contrôle visuel dans le wizard en dépend.

---

## 13. Dépendances techniques

### 13.1 Backend (`go.mod`, go 1.25.0)

| Dépendance | Version | Rôle dans l'import |
|---|---|---|
| `github.com/xuri/excelize/v2` | `v2.11.0` | Lecture/écriture `.xlsx` — **seule lib Excel du dépôt**, ajoutée pour cette feature |
| `github.com/xuri/efp` | `v0.0.1` (indirect) | Dépendance transitive d'excelize |
| `github.com/xuri/nfp` | `v0.0.2-...` (indirect) | Dépendance transitive d'excelize |
| `github.com/google/uuid` | `v1.6.0` | Génération du token de preview |
| `go.uber.org/zap` | `v1.27.0` | Logging |
| `github.com/DATA-DOG/go-sqlmock` | `v1.5.2` | Tests handler (mock SQL strict) |

L'ajout d'excelize a fait remonter en transitif `golang.org/x/crypto v0.37.0 → v0.53.0` (résolution MVS
standard), documenté comme sans effet observable sur le reste du dépôt (phase 3).

Pas de nouvelle dépendance pour Redis, R2, ou HTTP — réutilisation intégrale de l'infrastructure existante.

### 13.2 Frontend (`package.json`)

| Dépendance | Version | Rôle |
|---|---|---|
| `react` | `^18.3.1` | — |
| `@tanstack/react-query` | `^5.83.0` | `useMutation` pour preview/commit/template ; **une seule vraie lecture cachée** (`GET /pos/tva_rates`) dans tout le parcours |
| `react-hook-form` / `zod` | `^7.61.1` / `^3.25.76` | **Non utilisés dans le wizard d'import** (état géré à la main via `useState` dans `useProductImport`) — contraste avec `ProductCreateSheet` qui les utilise |
| `sonner` | `^1.7.4` | Toasts (téléchargement du template) |

**Aucune bibliothèque xlsx côté client** : le fichier est envoyé brut en `multipart/form-data`, tout le
parsing est serveur. Pas de lib de parsing CSV/Excel dans `package.json`.

**Aucun composant dropzone générique** : le drag & drop de `ImportProviderStep.tsx` est une implémentation
ad hoc (gestion manuelle de `dragenter`/`dragover`/`dragleave`/`drop`, compteur de profondeur), pas un
composant réutilisable posé dans `components/ui/`.

---

## 14. Composants réutilisables vs spécifiques au domaine « produit »

### 14.a Générique, réutilisable tel quel pour un import clients

| Composant | Chemin | Pourquoi il est générique |
|---|---|---|
| **Moteur de parsing xlsx** | [xlsx.go](internal/modules/menu/importer/xlsx.go) (`readSheetRows`, `cellAt`, `rowIsEmpty`) | Ne connaît rien du domaine produit : lit une feuille en `[][]string`, gère les lignes courtes. Directement applicable à n'importe quel classeur. |
| **Helpers de parsing valeur** | [values.go](internal/modules/menu/importer/values.go) (`parsePriceCents`, `slugify`, `foldHeader`, `normalizeLabel`, `generatedExternalID`, `splitLabels`) | `parsePriceCents`/`parseTvaRate` sont spécifiques prix/TVA (pas clients), mais `foldHeader`, `normalizeLabel`, `slugify`, `generatedExternalID` sont purement textuels et réutilisables tels quels pour générer des ID déterministes clients (ex. `cl-p-<slug>-<hash>`). |
| **Interface `ImportProvider` + `Registry`** | [provider.go](internal/modules/menu/importer/provider.go) | Le patron `Slug() + Parse(io.Reader)` et le registre injectable ne présument d'aucun domaine — il suffirait d'implémenter un `ImportProvider` clients. |
| **`RowError` et son typage** | [provider.go:79](internal/modules/menu/importer/provider.go#L79) | Mécanique ligne/colonne/raison, générique. |
| **Mécanique preview/snapshot/commit (le squelette)** | [preview.go](internal/modules/menu/importer/preview.go), [snapshot.go](internal/modules/menu/importer/snapshot.go), [commit_plan.go](internal/modules/menu/importer/commit_plan.go) | Le **motif** — parser pur → dry-run pur avec lookups injectés → snapshot Redis à token/TTL → plan de commit pur revalidé contre l'état frais → transaction unique — est indépendant du produit. Le code lui-même (structs `PreviewResult`, `CommitPlan`...) est en revanche typé produit (§14.b) : c'est l'architecture qui est réutilisable, pas les structs telles quelles. |
| **Tables `import_*_mapping` (le patron DDL)** | [migrations/080_import_provider_mappings.up.sql](migrations/080_import_provider_mappings.up.sql) | Le schéma `(merchant_id, provider, external_id) → wello_id` avec UNIQUE + index inverse est un patron directement copiable pour `import_customers_mapping`. |
| **Handler HTTP (le squelette 3-routes)** | [import_handler.go](internal/modules/menu/import_handler.go) | Le motif preview (multipart|JSON) / commit / template, la distinction d'erreurs, la gestion 400/410/422, est un gabarit copiable — le contenu métier des messages est spécifique. |
| **Wizard UI (l'orchestrateur, le state machine)** | [ProductImportDialog.tsx](../../wello-back-office/src/components/menu/import/ProductImportDialog.tsx), [useProductImport.ts](../../wello-back-office/src/hooks/useProductImport.ts) (structure) | Le motif `choose → provider|manual → preview → done`, la gestion de `door`, le `back()` contextuel, le pattern `useMutation` sans cache, sont réutilisables comme squelette. Le contenu de l'état (`ImportDecisions`, `ManualRow`) est spécifique produit. |
| **`ImportDoorPicker`, `ImportProviderStep` (structure), `ImportDoneStep` (structure)** | `components/menu/import/*.tsx` | Le drag & drop, le choix de provider, le tableau créé/réutilisé/ignoré sont visuellement et structurellement neutres — seuls les libellés et les types de données sont produit. |
| **Génération de template xlsx (le mécanisme)** | [template.go](internal/modules/menu/importer/template.go) (`TemplateProvider` interface, écriture excelize, feuille d'aide) | Le mécanisme (colonnes décrites déclarativement → génération classeur avec style d'en-tête, volet figé, feuille d'aide) est générique ; le contenu des colonnes (`welloGenericTemplate`) est spécifique. |

### 14.b Spécifique au domaine « produit », à réécrire pour les clients

| Élément | Pourquoi il ne se réutilise pas tel quel |
|---|---|
| **`IntermediateImport` / `CanonicalProduct` / `CanonicalCategory` / `CanonicalTag` / `CanonicalAttribute`** | Structs typées produit (prix par canal, taux TVA par canal, groupes d'options). Un client n'a ni prix, ni TVA, ni groupes d'options — le modèle canonique clients aura ses propres champs (email, téléphone, adresse, préférences RGPD, etc.) |
| **`ZeltyProvider` / `WelloGenericProvider` (le contenu)** | Colonnes et règles de mapping propres au menu (Prix, TVA, Tags-comme-catégorie). Un import clients viendra probablement d'un autre outil source (CRM caisse, export fidélité) avec un format différent — nouveaux parsers à écrire, même s'ils suivent le patron `ImportProvider`. |
| **Règles de validation métier produit** | TVA multi-canal (résolution taux→tva_id, backfill sur canal fermé), catégorie obligatoire, statut `removed_from_menu` sur prix nul — aucun équivalent direct côté client. Les règles clients seront probablement : email/téléphone valide, dédup par email ou téléphone, consentement marketing, etc. |
| **Dédup produit : par nom (`normalizeLabel`)** | Les clients ne se dédupliquent pas par nom — collision probable par email, téléphone, ou une combinaison nom+téléphone. Le module `customers` existant (`internal/modules/customers/`) a probablement déjà ses propres règles d'unicité à respecter — **[À CLARIFIER]** vérifier son modèle avant de concevoir la dédup import clients. |
| **`materializeCategoriesTx` / `materializeTagsTx` / `materializeAttributesTx` / `materializeProductsTx`** | Écrivent dans `productcateg`, `tags`, `configurable_attributes`, `products` — tables cibles du menu. Le commit clients écrira dans les tables du module `customers`, avec ses propres contraintes (à auditer séparément). |
| **`insertProductTx` / `CreateProductCategory` / `tags.Repository.CreateTag` réutilisés** | Fonctions cœur du module `menu`, injectées dans le commit d'import produit. L'équivalent clients réutiliserait les fonctions cœur du module `customers` (si elles existent en forme transaction-agnostique comparable — **[À CLARIFIER]**). |
| **`ImportDecisions` (le contenu : `TagClassification`, `CategoryPerProduct`, `TvaMapping`, `NameCollisions`)** | 4 décisions précises au domaine produit. Les décisions clients seront différentes (ex. fusion de doublons, choix du champ d'unicité, validation RGPD) — la **structure** de « décisions proposées puis amendées » est réutilisable, le contenu non. |
| **Sections du wizard `review/*`** (`ImportTvaResolution`, `ImportTagClassification`, `ImportMissingCategories`) | Écrans dédiés à la TVA et aux catégories produit — sans équivalent client direct. |
| **`ManualRow` / `ImportManualRow` (contenu des colonnes)** | Grille de saisie à colonnes produit (prix × 3 canaux, TVA × 3 canaux). Une grille clients aurait ses propres colonnes (nom, téléphone, email...). |
| **Migration 081 (élargissement `configurable_attribute_options.title`)** | Correctif ponctuel lié à un champ produit, sans rapport avec les clients. |

### 14.c Recommandation

**Ne pas extraire un `ImportEngine` générique maintenant ; dupliquer/adapter le pattern actuel pour les
clients.**

Justification, fondée sur ce qui a été observé dans le code :

1. **Le couplage entre le canonique et le domaine est profond et volontaire.** `IntermediateImport` porte
   directement des champs métier produit (`PriceIn`, `TvaRateIn`, `AllPricesZero`...) — ce ne sont pas des
   métadonnées génériques enveloppant un payload typé, mais des champs de premier niveau. Généricifier
   `IntermediateImport` demanderait soit des génériques Go (`IntermediateImport[T]`), soit un retour à des
   `map[string]any`, ce qui casserait exactement l'invariant que la phase 3 a posé en dur : « le parser ne
   décide rien » repose sur des types stricts qui rendent les décisions explicites et testables par le
   compilateur.

2. **La preview et le commit sont truffés de règles métier produit non extractibles sans réécriture
   complète.** Le backfill TVA sur canal fermé, la résolution `(taux, canal) → tva_id`, le statut
   `removed_from_menu` sur prix nul, la catégorie-comme-premier-libellé — aucune de ces règles n'a
   d'équivalent générique à deviner pour un domaine client. Un `ImportEngine` générique n'abstrairait que le
   **squelette** (parse → preview pure avec lookups injectés → snapshot Redis → commit pur revalidé → 1
   transaction), qui représente une fraction du code (`preview.go` fait ~920 lignes, dont l'essentiel est
   spécifique TVA/catégorie/produit).

3. **Le motif est déjà documenté et reproductible sans abstraction.** Les 5 docs de phase de cette feature
   constituent de facto un guide de conception explicite (modèle canonique pur → parsers purs → preview pure
   avec lookups → snapshot Redis token/TTL → commit pur revalidé → transaction unique → tables de mapping
   `(merchant_id, provider, external_id)`). Ce guide se réapplique au domaine clients en écrivant un second
   paquet `internal/modules/customers/importer/` qui suit la même succession de fichiers
   (`models.go`, `provider.go`, un ou deux parsers, `preview.go`, `snapshot.go`, `commit_plan.go`), sans
   dépendre du paquet `menu/importer`.

4. **Le risque d'une extraction prématurée dépasse son bénéfice mesurable.** Il n'existe aujourd'hui qu'un
   seul domaine importé (produits). Extraire un moteur générique sur la base d'un unique cas d'usage revient
   à deviner la forme de l'abstraction avant de connaître les contraintes réelles du second domaine (clients)
   — notamment sa propre logique de dédup (par email/téléphone plutôt que par nom), ses propres tables
   cibles, et des règles RGPD potentielles sans équivalent côté produit. Écrire l'import clients en dupliquant
   le pattern révélera quelles parties sont *réellement* communes (probablement : le moteur xlsx, les
   helpers texte, la mécanique snapshot/token/TTL, le squelette HTTP/wizard) — à ce moment-là, une
   factorisation ciblée sur ces parties précises sera un refactoring à faible risque plutôt qu'une
   abstraction spéculative.

Concrètement, ce qui **peut être extrait dès maintenant sans risque** (parce qu'il est déjà sans dépendance
métier) : `xlsx.go` en entier, les helpers texte de `values.go` (`foldHeader`, `normalizeLabel`, `slugify`,
`generatedExternalID`, `splitLabels`) vers un paquet partagé (`internal/importutil/` par exemple), et
côté front le squelette de `ProductImportDialog`/`useProductImport` comme référence de copie (pas comme
composant paramétrable — les types qu'il manipule sont trop spécifiques pour être généricisés proprement en
TypeScript sans complexifier inutilement les deux usages).

---

## Points restant à clarifier

| # | Point |
|---|---|
| 1 | Anti-abus / rate-limiting spécifique aux 3 routes d'import : non vérifié au-delà du RBAC et du TTL de token. |
| 2 | Modèle d'unicité existant du module `internal/modules/customers/` (email ? téléphone ? les deux ?) — préalable indispensable à la conception de la dédup import clients, non audité ici (hors périmètre de cette lecture, centrée sur l'import produit). |
| 3 | Existence ou non, côté module `customers`, de fonctions cœur transaction-agnostiques comparables à `insertProductTx` / `CreateProductCategory` / `tags.Repository.CreateTag`, réutilisables telles quelles par un futur commit d'import clients. |
