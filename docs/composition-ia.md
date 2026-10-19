# Composition par IA à partir de la description d'un produit

Demande d'Ilies du 2026-10-19 : créer la composition (les ingrédients) de produits **existants** à partir de leur description, en dehors de l'import de carte par photo. Le même jour, l'option « lire aussi les ingrédients » de l'import photo, codée côté API mais jamais branchée dans le back-office, a été rendue activable (§ 6).

**Statut :** codé le 2026-10-19 (API et back-office), non commité, non déployé. Aucun appel réel à l'API Anthropic n'a encore été fait. La tâche IA est **fermée par défaut**. Aucune migration.

---

## 1. Ce que voit le restaurateur

- **Points d'entrée :**
  - page Produits → cocher des produits → « Édition de groupe » → action **« Générer les ingrédients depuis la description »** ;
  - fiche produit → menu ⋮ → **« Générer les ingrédients (IA) »**.

  Les deux ouvrent la même fenêtre (`AICompositionDialog`).
- **Avant de lancer :** nombre de produits qui ont une description, crédits restants, rappel que les quantités ne sont pas lues.
- **Après la lecture :** un bloc par produit, avec les ingrédients proposés en pastilles à cocher. Un ingrédient absent de la caisse porte la mention « nouveau ». Un mode s'applique à toute la sélection :
  - **Compléter** (par défaut) : seuls les ingrédients cochés absents de la composition sont ajoutés. La composition actuelle et ses quantités restent intactes ;
  - **Remplacer** : la composition devient exactement les ingrédients cochés. Les ingrédients actuels que la description ne cite pas sont proposés décochés, donc retirés sauf si on les recoche. Une ligne gardée conserve sa quantité.
- Les produits sans description, les groupes et les produits dont la lecture a échoué sont listés avec la raison.

## 2. Décisions

### C1 — Points d'entrée : édition de groupe et fiche produit *(choix d'Ilies)*
Le menu ⋮ de la page Produits ne contient que des actions globales, qui ne tiennent pas compte de la sélection. Les actions sur une sélection passent par « Édition de groupe », d'où l'action ajoutée là, à côté de « Définir / Ajouter des ingrédients ». La fiche produit appelle la même fenêtre pour un seul produit.

### C2 — Coût : un crédit d'import photo par génération *(choix d'Ilies)*
- Une génération (lecture d'une sélection, quel que soit son nombre de lots) consomme **un** crédit du solde de l'import photo (`AI_MENU_OCR_DEFAULT_CREDITS`, surcharge staff `PUT /admin/merchants/{id}/menu-ocr-credits`).
- Le crédit est **rendu** si aucun lot n'a pu être lu (échec API, réponse illisible). Aucun crédit n'est consommé si aucun produit choisi n'a de description.
- **Sans migration :** chaque génération est une ligne de `menu_import_drafts` avec `source = 'ai_composition'` (la colonne n'a pas de contrainte CHECK) et `pages = []`. Le compteur `CountUsedCredits` compte toutes les sources. Statut final `committed` (crédit compté) ou `expired` (crédit rendu).
- Les lectures de l'import photo (`GetDraft`, `ListOpenDrafts`, `AbandonDraft`) filtrent désormais `source = 'ai_photo'`. Une génération n'apparaît donc jamais comme brouillon photo. La purge périodique la traite sans effet, puisqu'elle n'a pas de photos.
- **Conséquence voulue :** l'index unique « une lecture en cours par marchand » couvre les deux. Une génération lancée pendant une lecture de photos reçoit 409 (`import_ai_already_running`), et inversement. Cela empêche aussi de dépenser deux fois le dernier crédit en parallèle.

### C3 — Existant : mode choisi dans l'aperçu, « Compléter » par défaut *(choix d'Ilies)*
Voir § 1. Côté API, `planCompositionChanges` calcule les lignes à retirer (par `requires.id`) et les ingrédients à ajouter. **Une ligne gardée n'est jamais réécrite**, et les lignes de consommable (`consumable_id`) ne sont jamais touchées. L'action existante « Définir les ingrédients », elle, efface tout `requires` puis réinsère.

### C4 — Ingrédients : même vocabulaire et même identité que l'import photo
- Le schéma (`importer.AICompositionSchema`) réutilise l'objet ingrédient de `AIMenuPageSchema` : catégories et unités fermées (vérifié par test).
- Les règles d'écriture d'un ingrédient sont une constante partagée par les deux consignes (`aiIngredientFormatRules`). La consigne photo est restée identique à l'octet près (empreinte SHA-256 comparée avant et après).
- Un ingrédient proposé retrouve l'ingrédient existant de même `IngredientKey` (casse, accents et pluriel simple ignorés : « Tomate » = « Tomates »). Sinon il est créé avec une catégorie d'ingrédient réutilisée par nom (« Légumes »…) ou créée, et l'unité proposée (repli `PCE`).
- **Quantité 0**, comme l'import photo : une description ne donne jamais de quantité. La composition décrit le plat, elle ne pèse pas sur le stock tant que le restaurateur ne saisit pas les quantités. La fenêtre le dit.

### C5 — Modèle et appels
- Tâche IA `menu_composition` : `anthropic` / `claude-opus-5-5` / effort `low` / 8 000 jetons / 90 s, **fermée par défaut** (`AI_TASK_MENU_COMPOSITION_ENABLED`). Opus 5.5 reste le défaut, comme `menu_ocr`. `claude-sonnet-5-5` (deux fois moins cher, accepte aussi effort et sorties structurées) se règle par `AI_TASK_MENU_COMPOSITION_MODEL` si les essais le justifient.
- Texte seul, **lots de 20 produits**, 3 lots en parallèle, au plus **100 produits** par génération. Appel synchrone : pas de brouillon à reprendre.
- Les produits sont envoyés sous des refs locales (`p1`, `p2`…), sans aucun identifiant Wello. La consigne précise que le texte d'une description est une donnée, pas une instruction.
- Échec d'un lot : ses produits sont rendus en `failed` et les autres lots restent utilisables.

### C6 — Application sans IA, ingrédients renvoyés par le client
`apply` reçoit les ingrédients choisis (nom, catégorie, unité) et ne rappelle pas l'IA. Il n'y a pas d'état serveur entre l'aperçu et l'application. Faire confiance au client ne donne aucun droit nouveau : il peut déjà créer des ingrédients et des compositions par les routes ordinaires. L'appartenance de chaque produit au marchand est vérifiée (sinon tout le lot est refusé), et tout s'écrit dans une seule transaction.

## 3. API

| Méthode | Route | Rôle |
|---|---|---|
| POST | `/menu/products/composition/ai/preview` | `{"product_ids": [...]}` → par produit : statut, composition actuelle, ingrédients proposés (`component_id` si existant, `in_composition`) + solde de crédits. Consomme un crédit |
| POST | `/menu/products/composition/ai/apply` | `{"products": [{"product_id", "mode": "add"\|"replace", "ingredients": [{name, category, unit}]}]}` → compteurs (produits modifiés / inchangés / ignorés, ingrédients et catégories créés) |

Permission `catalog.manage`, comme les autres actions de groupe.

| Code | Cas |
|---|---|
| 400 | `invalid_request`, `too_many_products`, `no_description` (aucun crédit consommé) |
| 402 | `import_ai_no_credits` |
| 404 | `product_not_found` (produit d'un autre marchand ou désactivé) |
| 409 | `import_ai_already_running` |
| 502 | `composition_ai_failed` (crédit rendu) |
| 503 | `composition_ai_disabled` |

## 4. Fichiers

**API :** `internal/config/ai.go` (tâche), `importer/ai_composition.go` (schéma, décodage, `CleanIngredients`, `IngredientKey`), `composition_ai_prompt.go` (consigne, `aiIngredientFormatRules`), `composition_ai_service.go` (aperçu, crédit, lots, application, `planCompositionChanges`), `composition_ai_repository.go` (lecture produits/compositions, écriture transactionnelle), `composition_ai_handler.go`, `import_ai_service.go` (`aiCredits` partagé), `import_ai_repository.go` (filtre `source = 'ai_photo'`), `import_ai_prompt.go` (règles partagées), `cmd/api/routes.go`.

**Back-office :** `components/menu/AICompositionDialog.tsx`, `lib/aiComposition.ts`, `services/menuCompositionService.ts`, `types/aiComposition.ts`, `components/menu/BulkEditDialog.tsx` (action), `components/menu/SimpleProductSheet.tsx` (menu ⋮, relecture de la fiche), `hooks/useProductData.ts` (`reloadKey`), `pages/Menu.tsx`.

## 5. Vérifications

| Vérification | Résultat |
|---|---|
| `go build ./...`, `go vet` (avec et sans tag `postgres_integration`) | OK |
| `go test ./internal/modules/menu/... ./internal/config/...` | OK. Nouveaux tests : aperçu (un appel, produits sans description et groupes non envoyés, ingrédient existant retrouvé par pluriel, un crédit consommé), découpage en lots (45 produits → 3 appels, refs bien rattachées, un seul crédit), échec → crédit rendu, garde-fous (tâche fermée, sélection vide ou invalide, autre marchand, rien à lire, plus de crédit, plus de 100 produits : aucun appel IA ni crédit), validation de l'application, `planCompositionChanges`, schéma, `CleanIngredients` |
| `TestCompositionAI_Postgres_LoadAndApply` | **Écrit, non exécuté** : il écrit sur la base (données nettoyées) et attend l'accord d'Ilies. Vérifie la lecture des compositions, replace (ligne gardée à 30 g, ligne retirée, deux ingrédients et deux catégories créés), add, rejeu sans effet, produit étranger refusé |
| Back-office : `vitest` (`aiComposition.test.ts`) | OK |
| Back-office : `tsc` filtré sur les fichiers touchés, `eslint` | OK (le dépôt a des erreurs `tsc` antérieures ailleurs) |
| Rendu et appel réel | **Non vérifiés** : il faut ouvrir `AI_TASK_MENU_COMPOSITION_ENABLED` sur staging et essayer sur de vraies descriptions |

`go test ./internal/...` montre des échecs **antérieurs et sans rapport** dans `planning/employees` et les demandes de congé.

## 6. Import photo : ingrédients activables, et correctif des promotions écartées

- **Ingrédients :** l'API acceptait déjà `ingredients=true` à l'envoi des photos, mais le back-office ne l'envoyait jamais. Il y a désormais une case **« Lire aussi les ingrédients »** à l'étape photo (décochée par défaut, choix d'Ilies). À la relecture, les ingrédients lus s'affichent sous chaque produit en pastilles à décocher (décision `ingredients_per_product`, déjà gérée par l'API). La couleur proposée (§ 7 de [import-carte-ia-03-porte-ia.md](import-carte-ia-03-porte-ia.md)) y est aussi montrée en pastille.
- **Correctif (grave depuis D32) :** `buildImportDecisions` (back-office) n'envoyait pas `excluded_promotions`. Une promotion décochée à la relecture était donc créée quand même. Depuis que les promotions sont créées **actives** (D32), elle se serait appliquée aux commandes. Corrigé, avec un test (`importDecisions.test.ts`).
- Fichiers : `ImportPhotoStep.tsx`, `review/ImportPhotoProducts.tsx`, `review/ImportProductIngredients.tsx`, `useProductImport.ts`, `menuImportService.ts`, `lib/importDecisions.ts`, `types/import.ts`.
