# Phase 5 — Endpoint commit `POST /menu/import/commit`

Phase 5 de la feature « Importer des produits ». **La seule phase qui écrit.** Elle consomme le token
déposé par la preview, applique les décisions du wizard, et matérialise le lot dans une transaction unique.

Amont : [phase 3](import-produits-phase3-modele-canonique.md) (canonique + parsers) ·
[phase 4](import-produits-phase4-preview.md) (preview + snapshot) ·
[migrations 080/081](migration-postgres/080-081-import-provider-mappings.md) (tables `import_*_mapping`).

## 1. Livrables

```
internal/modules/menu/importer/
├── commit_plan.go        CommitPlan · Planned{Category,Tag,Attribute,Product} · CommitBlocker · BuildCommitPlan
└── commit_plan_test.go

internal/modules/menu/
├── import_commit_models.go     DTO HTTP (requête, réponse, résumé)
├── import_commit_repository.go MaterializeImportTx + écriture des 5 correspondances
├── import_commit_service.go    ImportService.CommitImport
├── import_handler.go           + CommitImport
├── import_handler_test.go      + 410 / 422 / RBAC / token consommé
└── import_commit_postgres_integration_test.go   //go:build postgres_integration

cmd/api/routes.go   POST /menu/import/commit, RequirePermission(HasMenuAccess)
```

### Modification d'un cœur Phase 1

`insertAttributeOptionsTx` retourne désormais `([]int64, error)` au lieu de `error`, en passant de
`ExecContext` à `dbx.InsertReturningID`. Additif : son unique appelant (`insertAttributeTx`) ignore le
retour, le chemin unitaire est inchangé.

Motif : sans les id d'options, `import_attribute_options_mapping.wello_id` aurait dû être déduit en relisant
la table et en **appariant positionnellement** — une hypothèse fragile sur l'ordre d'identity. Le `RETURNING`
la supprime.

## 2. Contrat HTTP

```jsonc
POST /menu/import/commit
{
  "token": "…",              // celui rendu par la preview
  "decisions": { … }         // le champ `decisions` de la preview, amendé.
                             // Omis ou null → les défauts du snapshot s'appliquent.
}
```

`decisions` se décode directement dans `importer.ImportDecisions` : `TvaRateKey` sait se lire depuis une clé
JSON `"<taux>:<canal>"` (le `TextMarshaler` posé en phase 4). Le wizard renvoie donc littéralement ce qu'il a
reçu.

| Statut | Cas |
|---|---|
| **200** | résumé `{created, reused, skipped}` par entité + la liste `{external_id, wello_id, action}` |
| **400** | corps illisible, token absent |
| **410** | token inconnu, expiré, déjà consommé, ou d'un autre marchand |
| **422** | `{"error":"import_not_committable","blockers":[{code, ref, message}]}` — **aucune écriture** |

**410 plutôt que 404** : expiré, consommé et inexistant sont indistinguables (la clé Redis a simplement
disparu) et les distinguer renseignerait un appelant sur l'existence d'un token qui n'est pas le sien. Dans
les trois cas, l'action est la même — relancer un import.

Codes de blocage : `product_needs_category` · `tva_rate_unresolved` · `product_name_collision_unresolved` ·
`invalid_tva_mapping` · `invalid_category_decision`.

## 3. `BuildCommitPlan` — pur, et méfiant

```go
func BuildCommitPlan(imp *IntermediateImport, decisions ImportDecisions, lk PreviewLookups) (*CommitPlan, []CommitBlocker)
```

Les lookups sont **rechargés au commit**, pas repris de la preview : entre les deux, une catégorie a pu être
créée, un produit renommé, un import concurrent passer. Le plan doit refléter la base au moment où il sera
écrit — c'est aussi ce qui donne l'état d'idempotence à jour.

**Rien de ce que le client renvoie n'est cru sur parole** :

| Décision renvoyée | Revérifiée contre |
|---|---|
| `TvaMapping[(taux, canal)] = id` | le `tva_id` doit exister, être actif, **et porter le `delivery_type` du canal annoncé** |
| `CategoryPerProduct[produit] = libellé` | le libellé doit être classé `category`, ou être une catégorie explicite de la source |
| `NameCollisions[produit]` | la collision doit avoir été réellement détectée |
| `TagClassification` | complétée par le défaut de la preview pour tout libellé non tranché |

Un `CommitPlan` ne contient plus aucune valeur à résoudre : que des `tva_id`, des prix en centimes, un statut
et des identifiants externes. La transaction n'a plus qu'à l'exécuter.

### Validation ensembliste, miroir de `validateProductForCreate`

`validateProductForCreate` émet **quatre requêtes par produit** et revalide 141 fois les mêmes trois `tva_id`
et la même catégorie — alors que le pool est plafonné à **une connexion**. Le plan fait la même chose en
mémoire, à partir de deux lectures.

| Validation unitaire | Équivalent dans le plan |
|---|---|
| 1 · `tva_*_id` obligatoires et numériques | les `tva_id` viennent du résolveur, jamais d'une saisie ; un canal non résolu bloque |
| 2 · catégorie existante **et activée** | `ExistingCategories` n'est chargé qu'avec `enabled = TRUE` ; une catégorie déjà importée mais désactivée depuis est signalée `Usable() == false` et bloque |
| 3 et 4 · les trois taux existent et sont actifs | le résolveur n'est construit qu'à partir des lignes `enabled = TRUE` |

`assignChannels` porte le commentaire « miroir de `validateProductForCreate`, garder synchronisé », et
`TestBuildCommitPlanMirrorsValidateProductForCreate` couvre les quatre cas de rejet, chacun annoté de la
validation unitaire qu'il reproduit.

### Deux cas que la preview ne voyait pas

Une entité **déjà mappée mais disparue depuis** : le mapping empêche de la recréer, mais elle n'est plus
utilisable. Le traitement est proportionné à son caractère obligatoire :

- **catégorie désactivée** → blocage `product_needs_category` (`products.category` est `NOT NULL` et validé) ;
- **tag supprimé** → simplement écarté du produit. Un tag est facultatif ; faire échouer tout un import pour
  lui serait disproportionné, et `SyncProductTags` refuserait le produit entier.

## 4. Transaction

Un seul `dbutils.RunInTx`. Ordre imposé par les dépendances, avec une correspondance `externalID → welloID`
construite au fil de l'eau :

1. **Catégories** — réutiliser (par nom ou par mapping) ou créer via `CreateProductCategory`, puis écrire
   `import_categories_mapping` (`wello_id` = `categ_id`). On mémorise le `merchant_categ_id`, qui est ce que
   `products.category` référence.
2. **Tags** — idem via `tags.Repository.CreateTag`, puis `import_tags_mapping`.
3. **Attributs** — `insertAttributeTx` **sans options**, puis `insertAttributeOptionsTx` séparément pour
   récupérer les id ; `import_attributes_mapping` et `import_attribute_options_mapping`.
4. **Produits** — `CreateProductPayload` complet → `insertProductTx`, puis `import_products_mapping`.

Toute entité déjà mappée est sautée. Tout ou rien : la moindre erreur annule l'ensemble, correspondances
comprises — un lot à moitié écrit laisserait des mappings qui feraient sauter les entités manquantes au
ré-import.

Après la transaction, **une seule fois pour tout le lot** : `setMenuUpdated`, invalidation des caches de
menu, et suppression de la clé Redis du token.

### Décisions de composition

| Point | Choix | Motif |
|---|---|---|
| Création de tag | **`tags.Repository.CreateTag` réutilisé** | Vérifié transaction-agnostique (`dbx.GetDB(ctx)`) : il s'exécute dans notre transaction et respecte le tout-ou-rien. Un seul endroit crée un tag. Injecté par interface, donc substituable |
| `CreateProductCategory` | réutilisé, **puis relecture de contrôle** | Il renseigne `merchant_categ_id` en deux temps et **n'en remonte pas l'erreur du second `UPDATE`**. En unitaire cela passe inaperçu ; en lot, un `merchant_categ_id` resté vide ferait pointer tous les produits de la catégorie sur `''`. La relecture transforme un bug silencieux en rollback. Le chemin unitaire n'est pas touché |
| Attributs | `Configuration` **jamais renseigné** sur les produits | V1a : les groupes d'options sont créés standalone (`product_id = 0`), jamais rattachés. La garde `len(Configuration) > 0` de `insertProductTx` n'est donc jamais franchie, `product_configurable_attribute` reste vide — vérifié en intégration |
| Mapping des entités réutilisées | écrit aussi | Rend le ré-import franchement idempotent (chemin `already_imported`) plutôt que de repasser par la déduplication par nom |

### Inefficience assumée

`insertProductTx` appelle `SyncProductTags` dès que le produit porte des tags, et `SyncProductTags` appelle
`setMenuUpdated` en interne, plus deux `SELECT` de contrôle. Sur 141 produits, cela fait quelques centaines
d'aller-retours superflus dans la transaction. Corriger demanderait de réécrire un cœur de la phase 1 : hors
périmètre, signalé ici. L'`UPDATE` est idempotent, l'effet est un coût, pas une incohérence.

## 5. Tests

`go build`, `go vet` (avec et sans le tag `postgres_integration`) verts. Le paquet `importer` passe de 53 à
**71** tests, le paquet `menu` de 14 à **19**, plus **3 tests d'intégration**.

### Cœur pur — 18 cas

Rejets : produit sans catégorie · taux absent de `tva_categories` · repli d'un canal fermé irrésolvable ·
collision non tranchée · catégorie déjà importée puis désactivée.

Décisions client incohérentes : `tva_id` du canal livraison posé sur le canal sur place → `invalid_tva_mapping` ;
catégorie imposée qui est classée `tag` → `invalid_category_decision`.

Cas nominaux : plan complet sur la fixture 2026 · Carbonara `10/0/0` → canaux fermés avec `tva_id` backfillé ·
reclassement d'un tag en catégorie honoré · collision `skip` vs `import_anyway` · produits déjà importés
exclus des contrôles (103 à créer sur 107) · idempotence totale (rien à recréer) · réutilisation par nom.

### Intégration — exécutée réellement

**Les 3 tests passent** contre le Postgres de dev (`welloresto-postgres-dev`, port 5433). Ils avaient besoin
des migrations **080 et 081**, absentes de ce Postgres : je les y ai appliquées (additives, `IF NOT EXISTS`,
réversibles par les `.down.sql`). Sans **081** en particulier, deux libellés d'option de la fixture 2026
dépassent `varchar(25)` et l'import échoue.

Chaque cas travaille sur **son propre marchand** et nettoie derrière lui ; la base a été vérifiée inchangée
après exécution (2493 produits / 28 marchands avant et après).

| Test | Ce qu'il prouve |
|---|---|
| `EndToEndAndIdempotent` | 141 produits · 12 groupes · 78 options · 19 libellés créés, comptés dans le résumé **et** en base. Les 5 tables de correspondance renseignées. Carbonara : `tva_in_id` = 10 % sur place, `tva_take_away_id` / `tva_delivery_id` = **le taux 10 re-résolu sur leur canal**, `available` = (true, false, false). Catégorie rattachée = `NOS PIZZA`, 2 tags rattachés. `product_configurable_attribute` **vide** et `configurable_attributes.product_id` toujours 0 (V1a). Titre de 27 caractères intact. **Token consommé** : un second commit renvoie 410. **Re-commit après nouvelle preview → 0 création**, 141 correspondances inchangées |
| `ZeroPricedProductsAreRemovedFromMenu` | L'export 2025 sans arbitrage est **refusé** (`*ImportNotCommittableError`) et **0 ligne écrite** ; avec les catégories imposées, 107 produits créés dont **4 en `removed_from_menu`**, aucun valorisé |
| `RollsBackEntireBatchOnError` | Une erreur sur le second produit, après écriture des catégories, tags, attributs et du premier produit → **0 ligne dans les 10 tables**, correspondances comprises |

### Handler — 5 cas

410 sur token inconnu · 410 sur snapshot d'un autre marchand · 422 avec `blockers` structurés **et token non
consommé** (l'utilisateur corrige et rejoue) · 400 sans token · garde RBAC composée comme dans `routes.go`
(`CanManageMenu` et `Admin` passent, sans droit → 403 avant le service).

Comme en phase 4, les tests sqlmock ne déclarent que des `ExpectQuery` : sur les chemins de refus, la moindre
écriture fait tomber le test.

## 6. Reste ouvert

- Suivi par lot (`import_batches`) et « annuler l'import n°X » : hors V1 (décision 17). Les correspondances
  posées ici sont ce qui le rendra possible.
- Upsert (ré-importer pour **mettre à jour**) : V2. Aujourd'hui une entité mappée est sautée, pas mise à jour.
- Le nom d'un groupe d'options est limité à `varchar(50)` par le schéma ; aucun export connu n'approche cette
  limite, et un dépassement ferait échouer proprement la transaction plutôt que de tronquer.
