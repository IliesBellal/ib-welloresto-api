# Phase 3 — Modèle canonique + parsers d'import

Phase 3 de la feature « Importer des produits ». Elle construit le **pivot du pipeline** : le modèle
canonique, l'abstraction provider, le parser Zelty et le parser du template Wello.

**Périmètre strictement fermé** : aucun endpoint, aucun handler, aucune route, aucune dépendance vers
`repository` ou la base, aucune migration. Un parser est une fonction pure `io.Reader → struct`.

Amont : [`docs/audit-import-produits.md`](audit-import-produits.md) (phase 0), commit `1b0028f` (phase 1,
extraction de `insertProductTx` / `insertAttributeTx`),
[`docs/migration-postgres/080-081-import-provider-mappings.md`](migration-postgres/080-081-import-provider-mappings.md)
(phase 2, tables `import_*_mapping`).

## 1. Livrables

```
internal/modules/menu/importer/
├── models.go              modèle canonique + ImportDecisions
├── provider.go            interface ImportProvider, Registry, rowError
├── values.go              helpers purs (prix→centimes, taux, libellés, identifiants générés)
├── xlsx.go                lecture d'une feuille via excelize
├── zelty.go               ZeltyProvider
├── wello_generic.go       WelloGenericProvider
├── values_test.go
├── provider_test.go
├── xlsx_test.go           + buildXLSX / openFixture, helpers de test
├── zelty_test.go
├── wello_generic_test.go
└── testdata/
    ├── Zelty Menu OK PIZZA DLP - 2025-09-24.xlsx
    └── Zelty Menu OK Pizza - Devant-les-Ponts - 2026-08-04.xlsx
```

**Dépendance ajoutée** : `github.com/xuri/excelize/v2 v2.11.0`. C'est la première bibliothèque Excel du
dépôt (l'audit §1.8 confirmait l'absence). Elle tire en transitif un relèvement de
`golang.org/x/crypto v0.37.0 → v0.53.0`, `x/net`, `x/sys`, `x/sync`, `x/text` (résolution MVS standard).
`go build ./...` et `go test ./internal/...` se comportent à l'identique avant/après — vérifié en
comparant à la baseline, y compris les deux échecs préexistants de `planning/leave` et `planning/swaps`
et les trois signalements `go vet` sur `auth`/`cmd/api`, qui sont là avant ce commit.

## 2. La règle qui gouverne le paquet

> **Le parser ne décide rien.**

Il ne résout pas la TVA, ne classe pas les libellés en catégorie ou en tag, ne déduplique pas contre
l'existant en base, ne tranche pas les collisions de noms. Il produit une représentation neutre du
fichier. Tous les arbitrages sont pris dans la **preview** (phase 4) et redescendent au commit via
`ImportDecisions`.

La conséquence pratique : ce paquet est testable intégralement sans base ni réseau, et un nouveau
provider ne peut pas introduire de règle métier en douce.

## 3. Le modèle canonique

```go
IntermediateImport {
    Provider   string
    Categories []CanonicalCategory   // uniquement si la source les désigne explicitement
    Tags       []CanonicalTag        // TOUS les libellés source
    Products   []CanonicalProduct
    Attributes []CanonicalAttribute  // groupes d'options, non rattachés (V1a)
}
```

### 3.1 Catégorie : explicite ou vide

Le point structurant, **révisé par rapport à la note de passation** :

| Champ | Zelty | Template / formulaire |
|---|---|---|
| `CanonicalProduct.CategoryExternalID` | `""` | l'identifiant tiré de la colonne `Catégorie` |
| `CanonicalProduct.TagExternalIDs` | **tous** ses libellés, ordre du fichier | la colonne `Tags` |
| `IntermediateImport.Categories` | **vide** | les catégories citées, dédoublonnées |

Le parser Zelty ne calcule **pas** « catégorie = 1er tag ». Ce défaut existe bien, mais il est appliqué
en preview, au moment de la classification des libellés. Raison : l'ordre des tags est fiable dans
l'export 2026 (parent en tête) et ne l'est pas dans celui de 2025. Figer la règle au parse la rendrait
fausse la moitié du temps et invisible ; la porter en preview la rend révisable par l'utilisateur, avec
le produit sous les yeux. C'est aussi ce qui permet à la preview de promouvoir **n'importe lequel** des
libellés en catégorie, pas seulement le premier.

`Categories` reste donc vide pour Zelty : la promotion libellé → catégorie caisse est une décision, pas
une lecture.

### 3.2 Prix et TVA

Prix en **centimes entiers**, un champ par canal. Zelty n'expose qu'un prix : il est recopié sur les
trois. `AllPricesZero` marque les lignes de frais (prix 0), destinées au statut `removed_from_menu`.

Les taux de TVA sont des `*float64` en pourcentage, conservés **bruts** :

- `nil` = colonne absente ou cellule vide ;
- `0` = taux explicitement nul, ce qui vaudra désactivation du canal — mais c'est la preview qui le
  traduira en `available_* = false` et choisira le taux de repli.

La distinction `nil` / `0` est donc porteuse de sens et testée comme telle.

### 3.3 Groupes d'options

Aucun export connu ne fournit min/max. `applyDefaults` pose `attribute_type = 'CHECK'`,
`min_options = 0`, `is_required = false`, `max_options = nombre d'options du groupe`. Ce dernier n'est
pas arbitraire : `configurable_attributes.max_options` est `NOT NULL` **sans défaut**, et le laisser à
zéro rendrait le groupe inselectionnable.

## 4. Le format Zelty tel qu'il arrive vraiment

Constats relevés sur les deux exports, au-delà de la spec de départ :

| Constat | Conséquence sur le parser |
|---|---|
| **4** lignes d'en-tête par fichier, pas 3 : une section (r129 / r166) porte un en-tête et **aucune ligne** | Le routage se fait sur la colonne `Type`, pas sur la section courante — un automate indexé sur les en-têtes aurait calé sur la section vide |
| `Option` et `Option Value` sont **entrelacés** ; une valeur ne référence jamais son groupe | Seul état réellement nécessaire : le groupe courant. Une valeur orpheline est une erreur dure |
| Le texte est en cellules `inlineStr`, **les 3 colonnes TVA sont numériques** | `excelize` rend `"5.5"` avec un **point** là où `Prix` arrive en `"9,9"` avec une **virgule** : les deux séparateurs doivent être acceptés |
| Taux observés : `0`, `5.5`, `10`, `20`, jamais de cellule vide | Le `*float64` reste justifié pour le template, pas pour Zelty |
| 100 % des libellés cités par un produit résolvent contre la section `Tags` | Le tag synthétique est un filet, pas un chemin nominal |
| Les seuls produits sans libellé sont les **4 lignes « Frais »** de 2025, toutes à prix 0 | Un produit sans catégorie ne doit pas faire échouer le parse |

## 5. Décisions de parsing

| Décision | Pourquoi |
|---|---|
| **Prix converti sur la chaîne**, pas via `float64` | `9,9 * 100` vaut `989,999…` en binaire ; arrondir ce résultat est une roulette russe pour des montants qui finissent en base. Découpage partie entière / décimales, arrondi au demi supérieur sur la 3ᵉ décimale |
| **Cellule de prix vide = 0**, sans erreur | C'est le cas légitime des lignes de frais et des options sans supplément |
| **Montant illisible = erreur dure**, avec `ligne N, colonne "X"` | Un prix silencieusement mis à 0 partirait en base sans que personne ne le voie. Le numéro est celui du tableur, directement actionnable |
| **Libellé inconnu → tag synthétique** ajouté à `Tags` | Le rejeter perdrait l'information ; l'ignorer ferait disparaître une catégorie potentielle. Préfixe `zt-syn-`, minuscule, sans collision possible avec les `ZT…` réels |
| **Aucun produit → `ErrNoProducts`** | Garde contre le mauvais fichier ou le mauvais provider, sinon l'utilisateur enchaîne sur une preview vide sans comprendre |
| **Première feuille**, pas `"Sheet1"` en dur | Les deux exports connus la nomment ainsi, rien ne le garantit ; aucun format visé n'est multi-feuilles |
| **Accents conservés** dans `normalizeLabel`, **repliés** dans `foldHeader` | `VÉGÉ` et `VEGE` sont deux tags distincts pour le restaurateur ; `Catégorie` et `Categorie` sont le même en-tête |
| Tag cité deux fois sur une ligne → **dédupliqué** | `product_tags` a pour PK `(product_id, tag_id)` |

## 6. Le provider `wello-generic`

Format tabulaire : une ligne d'en-tête, une ligne par produit. Colonnes reconnues par **table d'alias
repliés** (`foldHeader`), donc insensibles à la casse, aux espaces multiples et aux accents.

- **Obligatoires** : `Nom`, `Catégorie`, `Prix sur place`. Absente → `ErrMissingColumn`.
- **Facultatives** : les autres. Une colonne absente laisse le champ à sa valeur neutre (0 pour un prix,
  `nil` pour un taux) — la preview complétera, notamment via le backfill au plus haut prix défini.
- **Cellule `Catégorie` vide** : tolérée, `CategoryExternalID` reste `""`. La catégorie est obligatoire
  à la création, mais c'est la preview qui la réclame.

**`ExternalID` généré** : `wg-p-<slug>-<sha256[:8]>` (et `wg-c-`, `wg-t-`). Le template n'a pas
d'identifiant source — l'identité d'une ligne est son nom. L'identifiant doit donc être **déterministe**,
puisque c'est lui qui porte l'idempotence via `(merchant_id, provider, external_id)` : deux imports
successifs du même libellé produisent la même valeur. Le slug garde l'identifiant lisible en preview,
l'empreinte le désambiguïse, et le tout est borné pour tenir dans `external_id varchar(64)`.

**Nom dupliqué → erreur dure** citant les deux lignes : deux lignes de même nom se réduiraient au même
`external_id` et l'import cesserait d'être rejouable. Le template étant le nôtre, exiger des noms uniques
est légitime.

## 7. Registre

`Registry` est une **valeur injectable**, pas une map globale mutable — aligné sur la DI par constructeur
du dépôt, et substituable en test.

```go
type ImportProvider interface { Slug() string; Parse(io.Reader) (*IntermediateImport, error) }

DefaultRegistry()          // zelty + wello-generic
(*Registry).Get(slug)      // erreur enveloppant ErrUnknownProvider
(*Registry).Slugs()        // trié, pour alimenter la liste de choix du back-office
```

## 8. Tests

31 tests, `go test ./internal/modules/menu/importer/...` vert.

Les **deux exports réels** sont les fixtures de référence. Les cas limites qu'aucun export ne contient
(ligne malformée, colonne manquante, valeur d'option orpheline) passent par `buildXLSX`, qui fabrique un
classeur en mémoire — pas de binaire supplémentaire commité.

Comptes de référence, garde-fou de l'automate à sections :

| Fixture | Tags | Produits | Groupes d'options | Options |
|---|---|---|---|---|
| `… PIZZA DLP - 2025-09-24` | 16 | 107 | 6 | 49 |
| `… Devant-les-Ponts - 2026-08-04` | 19 | 141 | 12 | 78 |

Cas ciblés notables :

- `"9,9"` → `990` sur les trois canaux (`ZD1676511`), et la table de `parsePriceCents` couvre point,
  virgule, séparateurs de milliers (espace, insécable, insécable étroit, fine), symbole euro, arrondi à
  la 3ᵉ décimale, retenue, montant négatif ;
- `ZD1676512` (*4 Fromages*) : `CategoryExternalID` **vide**, `TagExternalIDs` = `[ZT541858, ZT541863,
  ZT541866]` dans l'ordre du fichier ;
- `ZD1676517` (*Carbonara*) : TVA `10 / 0 / 0` conservée brute, les `0` restant des `0` explicites et non
  des `nil` ;
- `ZD1557688` (*Frais de livraison*, 2025) : `AllPricesZero`, sans libellé — et le compte global de
  produits sans prix vaut **4** en 2025, **0** en 2026 ;
- `ZO247656` : `MaxOptions = 6 = len(Options)`, `MinOptions = 0`, `IsRequired = false`, `Type = CHECK` ;
- titres d'option de plus de 25 caractères : exactement **2** en 2026 (`Jambon de Parme 24 mois AOP`,
  `Chocolat noisette gianduja`), **préservés entiers** — c'est ce que la migration `081` a rendu possible,
  et une troncature ici serait invisible et définitive ;
- `TestReadSheetRowsPreservesRowNumbering` verrouille l'hypothèse dont dépendent tous les numéros de
  ligne des messages d'erreur : `excelize` conserve les lignes vides, donc `index + 1` = numéro tableur.

## 9. Ce que la phase 4 doit reprendre

Le canonique est volontairement incomplet : il ne suffit pas à écrire en base. La preview devra fournir,
via `ImportDecisions` :

1. **`TagClassification`** — chaque libellé distinct → catégorie caisse ou tag Wello ;
2. **`CategoryPerProduct`** — la catégorie de chaque produit ; par défaut le **premier** libellé de
   `TagExternalIDs` classé en catégorie, modifiable. Obligatoire : `products.category` est `NOT NULL` et
   validé à la création ;
3. **`TvaMapping`** — `(taux, canal) → tva_categories.tva_id`, `TvaChannel` reprenant les valeurs
   stockées de `delivery_type` (`0` sur place, `1` livraison, `3` emporté) pour être directement
   utilisable en requête. Un taux `0` désactive le canal et déclenche le backfill de la TVA **et** du
   prix au plus haut défini ;
4. **`NameCollisions`** — les homonymes d'un produit existant. Le mécanisme de confirmation Redis du
   chemin unitaire (audit §3.6) ne s'applique pas à l'import : il est interactif et non rejouable.

Restent aussi à la charge de la phase 4 : la déduplication applicative des catégories et tags contre
l'existant, par `(merchant_id, LOWER(name))`, aucune contrainte d'unicité n'existant en base.
