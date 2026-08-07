# Phase 4 — Endpoint preview `POST /menu/import/preview`

Phase 4 de la feature « Importer des produits ». Elle construit le **dry-run** : parser ou assembler le
canonique, le confronter à l'existant du marchand, proposer toutes les décisions manquantes, et déposer un
snapshot sous un token que le commit consommera.

**Aucune écriture en base.** Uniquement des `SELECT`. Le seul effet de bord est le dépôt du snapshot en
cache, sous TTL.

Amont : [`docs/audit-import-produits.md`](audit-import-produits.md) (phase 0) ·
[`docs/import-produits-phase3-modele-canonique.md`](import-produits-phase3-modele-canonique.md) (phase 3) ·
[`docs/migration-postgres/080-081-import-provider-mappings.md`](migration-postgres/080-081-import-provider-mappings.md)
(phase 2, tables `import_*_mapping`).

## 1. Livrables

```
internal/modules/menu/importer/       (pur — aucun import DB/HTTP)
├── preview.go        PreviewLookups · PreviewResult · BuildPreview
├── manual.go         BuildManualImport — la 3e porte, sans parsing
├── snapshot.go       PreviewSnapshot + (dé)sérialisation
└── {preview,manual,snapshot}_test.go

internal/modules/menu/                (3 couches, structs dédiées)
├── import_models.go       DTO HTTP d'entrée
├── import_repository.go   LoadImportPreviewLookups — 8 SELECT
├── import_service.go      ImportService + interfaces importPreviewStore / importPreviewReader
├── import_handler.go      ImportHandler — multipart | JSON
└── import_handler_test.go

cmd/api/routes.go                    POST /menu/import/preview
internal/models/redis_models.go      MenuImportPreviewPrefix · MenuImportPreviewTTL (30 min)
internal/helpers/redis_helpers.go    GetMenuImportPreviewKey
```

### Retouches additives de la phase 3

Deux, toutes deux nécessaires, aucune ne change le comportement du parse (les 31 tests de la phase 3
restent verts) :

| Retouche | Pourquoi |
|---|---|
| `CanonicalTag.Synthetic bool` | Sans elle, le warning « libellé synthétisé » ne se détecterait qu'en reniflant le préfixe `zt-syn-`. Posée par le seul fallback Zelty |
| `TvaRateKey` implémente `encoding.TextMarshaler` / `TextUnmarshaler` (format `"5.5:1"`) | `encoding/json` **refuse** les maps à clé struct. Sans ça, `ImportDecisions.TvaMapping` ne se sérialise pas, et le commit n'a aucune décision à rejouer |
| `rowError` → `RowError` (exporté) | La couche HTTP doit distinguer une ligne mal remplie (400, avec ligne et colonne) d'une panne serveur (500) |
| `ErrInvalidWorkbook` | Un CSV envoyé à la place d'un `.xlsx` renvoyait 500. Trouvé par le test du handler, corrigé à la source |

## 2. Où vit quoi, et pourquoi

**`BuildPreview` est une fonction pure.** Elle ne lit ni base ni cache : tout l'existant lui arrive par une
struct `PreviewLookups`. C'est ce qui permet de la tester sur les deux exports réels sans infrastructure, et
c'est ce qui garantit que la preview et le commit raisonneront sur les mêmes données.

Elle vit dans le paquet `importer` bien qu'elle manipule des notions Wello (`tva_id`, `merchant_categ_id`) :
ce ne sont que des `int` et des `string` reçus en paramètre. Le paquet n'importe toujours ni `database/sql`
ni `net/http`.

**`ImportService` / `ImportHandler` sont des structs neuves**, pas des méthodes sur `MenuService` /
`MenuHandler`. Deux raisons :

1. Le service déclare ses dépendances en **interfaces** (`importPreviewStore`, `importPreviewReader`), donc
   testables. `*redisclient.Client` est une struct concrète à champ privé : sans interface, aucun fake n'est
   injectable, et « le token est relisible » n'aurait pas pu être asserté.
2. Le chemin unitaire n'est pas touché. Le constructeur de `MenuService` ne bouge pas.

La lecture réutilise `MenuRepository` — il porte déjà la connexion, inutile d'en créer un second.

## 3. Contrat d'entrée

Une route, deux modes distingués par le `Content-Type`.

| Mode | Entrée | Provider |
|---|---|---|
| `multipart/form-data` | champs `provider` + `file` (≤ 5 Mo) | `zelty`, `wello-generic` |
| `application/json` | `{"products":[…]}` | `manual` |

Le mode JSON est la porte du **formulaire de saisie en masse**. Prix en **centimes** (comme
`CreateProductPayload` — le back-office convertit déjà les euros), TVA en **taux %** : c'est la preview qui
résout le `tva_id`, exactement comme pour un fichier. `BuildManualImport` génère des identifiants externes
déterministes (`mn-p-<slug>-<hash8>`), au même schéma que le template, pour que l'idempotence fonctionne
aussi sur cette porte.

**5 Mo** reprend la limite des uploads d'images du module. Les exports réels pèsent une quinzaine de
kilo-octets pour 141 produits : la marge est confortable.

## 4. Ce que la preview calcule

### 4.1 Résolution de TVA

Chaque couple `(taux, canal)` rencontré est résolu contre `tva_categories` (`tva_rate` **et**
`delivery_type` **et** `enabled`). La table est globale, sans `merchant_id` : le couple suffit.

**Les taux sont comparés en centièmes de point entiers, pas en `float64`.** `tva_categories.tva_rate` est un
`real` (float32) et le taux du fichier vient d'un parse texte : un 5,5 des deux côtés doit se rencontrer,
pas se rater sur une différence de représentation.

Un couple sans correspondance sort `resolved: false` + warning `tva_rate_unresolved` — c'est le wizard qui
fera configurer le taux manquant.

### 4.2 Taux 0 : canal désactivé + backfill

`0` est la façon dont Zelty dit « pas vendu sur ce canal ». La preview pose `available: false`. Mais
`tva_*_id` et `price_*` restent `NOT NULL` : les laisser à zéro produirait un produit incohérent le jour où
quelqu'un réactive le canal.

- **TVA** : on prend les taux non nuls du produit, du plus haut au plus bas, et on retient le premier qui se
  **résout sur le canal à remplir** — le `tva_id` doit correspondre au `delivery_type`, on re-résout donc le
  taux sur ce canal-là. Le couple ainsi requis peut ne figurer nulle part dans le fichier : il est ajouté à
  la liste avec `needed_for_backfill: true`.
- **Prix** : le plus haut des trois. No-op sur Zelty, dont le prix unique est recopié sur les trois canaux.

Un taux **absent** (`nil`) n'est pas un taux nul : il ne désactive rien, il réclame une décision
(`tva_rate_missing`).

### 4.3 Classification des libellés

Un libellé est proposé `category` s'il **ouvre la liste d'au moins un produit dépourvu de catégorie
explicite** ; sinon `tag`.

La restriction « dépourvu de catégorie explicite » est ce qui empêche la règle de s'appliquer au template et
à la saisie manuelle, où la catégorie est donnée par une colonne dédiée : leurs libellés restent tous des
tags.

La catégorie d'un produit est alors le **premier de ses libellés classé catégorie**. Aucun → `needs_category`
+ warning : `products.category` est `NOT NULL` et validé à la création.

Un produit portant **deux** libellés classés catégorie n'en garde qu'une ; le second part dans
`dropped_label_external_ids` avec un warning, plutôt que de disparaître en silence.

### 4.4 Déduplication et idempotence

| Cas | Action proposée |
|---|---|
| Libellé classé catégorie, nom déjà pris dans `productcateg` | `reuse_existing` + `existing_category_id` (le `merchant_categ_id`, pas la PK) |
| Libellé classé tag, nom déjà pris dans `tags` | `reuse_existing` + `existing_tag_id` |
| `external_id` déjà dans `import_*_mapping` (marchand + provider) | `already_imported` — ignoré au commit |

La déduplication est **entièrement applicative** : aucune contrainte d'unicité n'existe sur
`productcateg.categ_name` ni sur `tags.name`. Sans elle, un import répété créerait des homonymes en silence.

Les tables de mapping sont lues **sans filtre sur `enabled`**, conformément à la sémantique posée par la
migration 080 : un mapping désactivé continue de signaler que l'entité a déjà été importée, ce qui évite de
ressusciter en doublon ce que le marchand a supprimé côté Wello.

### 4.5 Collisions de nom

Un produit dont le nom (en `LOWER`, sur les `enabled`) existe déjà est listé avec la résolution `skip` par
défaut, modifiable dans le wizard.

**C'est ce qui remplace la danse Redis du chemin unitaire** (audit §3.6) : celle-ci exige un second appel
strictement identique pour passer outre, ce qui est inutilisable en lot. Ici on détecte et on laisse décider.

Les produits Wello issus d'un import précédent **du même provider** sont exclus de la détection : c'est le
mapping qui fait autorité, pas le nom.

### 4.6 Produits déjà importés

Ils sont listés (`already_imported`) mais **pas instruits** : ni `needs_category`, ni collision, ni warning,
ni couple de TVA à résoudre. Ils ne seront pas créés — leur réclamer une décision bloquerait le commit pour
rien.

## 5. Snapshot

Clé `menu:import:preview:{merchantID}:{token}`, TTL **30 minutes**.

Le marchand fait partie de la clé : le commit relira avec le marchand du token d'authentification, donc un
token de preview d'un autre compte ne résout tout simplement pas. `MerchantID` figure **aussi** dans la
valeur, pour que le commit puisse le vérifier sans faire confiance au chemin par lequel le token est arrivé.

Le snapshot porte `IntermediateImport` + `ImportDecisions`, **pas** le `PreviewResult` : celui-ci est
recalculable et le back-office l'a déjà reçu en réponse.

**30 minutes** parce que le wizard fait classer les libellés, mapper les taux et arbitrer les collisions :
c'est un travail de plusieurs minutes sur un menu complet, sans commune mesure avec les 5 minutes d'une
confirmation de doublon.

Si le dépôt échoue, la requête échoue : renvoyer une preview dont le token n'est pas exploitable au commit
serait mentir.

## 6. RBAC

```go
r.With(middleware.RequirePermission(middleware.HasMenuAccess)).
    Post("/import/preview", menuImportH.PreviewImport)
```

C'est la **seule route du bloc `/menu` à porter un contrôle RBAC**. L'écart relevé à l'audit §1.7 — le bloc
n'applique que `authMiddleware` — reste ouvert sur les routes existantes, hors périmètre de cette phase. Mais
un endpoint qui réécrit le catalogue entier d'un coup ne pouvait pas hériter de cette absence.

## 7. Tests

`go test ./internal/modules/menu/...` vert. Le paquet `importer` passe de 31 à **53** tests ; le paquet
`menu` en gagne **6**.

**L'assertion « aucune écriture » est structurelle** : le test du handler ne déclare que des `ExpectQuery`.
sqlmock échoue sur tout appel non attendu, donc le moindre `INSERT`, `UPDATE` ou `BEGIN` fait tomber le test.
Les cas d'entrée invalide ne déclarent **aucune** attente : une requête partie trop tôt les ferait échouer.

Cœur pur, sur les deux fixtures réelles avec lookups simulés :

| Cas | Vérification |
|---|---|
| Résolution 5,5 / 10 / 20 | aucun couple non résolu sur les deux exports, `TvaMapping[10, in] == 2` |
| Taux orphelin | taux 20 retiré des lookups → Monaco non résolu sur place **et** backfill non résolu |
| Carbonara `10/0/0` | emporté + livraison `available:false`, `tva_id` **backfillé** (5 et 8, le taux 10 re-résolu par canal), prix 1390 non backfillé |
| Classification | `NOS PIZZA` → `category`, `BASE TOMATE` → `tag`, catégorie du produit = `NOS PIZZA`, absente de ses tags |
| `needs_category` | 4 en 2025 (les lignes de frais), 0 en 2026 |
| Collision | homonyme détecté en casse différente, résolution `skip`, warning |
| Collision auto-exclue | un produit Wello issu du même import ne déclenche pas de collision |
| Déjà importé | produit / libellé / groupe d'options marqués, compteurs 140 · 11 · 72 |
| Libellé perdu | 2ᵉ libellé catégorie listé et signalé |
| Cohérence du résumé | à créer + déjà importés = nombre de produits ; somme des classifications = nombre de libellés |

Handler : multipart (fixture 2026 réelle, 141 produits, `NOS PIZZA` réutilisée) · JSON (catégorie explicite,
`tva_id` résolu, prix livraison distinct) · token relisible et TTL 30 min via le fake store · 6 entrées
invalides → 400 avec le bon code · 401 sans token · 500 si le snapshot ne se dépose pas · **garde RBAC
composée comme dans `routes.go`** : `CanManageMenu` et `Admin` passent, un utilisateur sans droit est arrêté
en 403 avant d'atteindre le service.

## 8. Ce que la phase 5 devra faire

Le commit rejettera un snapshot dont il subsiste un `needs_category`, un taux non résolu ou une collision non
tranchée — c'est précisément ce que la phase 8 (wizard) rendra facile à lever. Restent à sa charge :

1. relire le snapshot et **vérifier `MerchantID`** avant tout ;
2. appliquer les `ImportDecisions` amendées renvoyées par le wizard, par-dessus le canonique du snapshot ;
3. `ImportProductsTx` en **un seul `RunInTx`**, en réutilisant `insertProductTx` / `insertAttributeTx`
   (phase 1) ;
4. écrire les cinq tables `import_*_mapping` dans la même transaction ;
5. poser le statut (`available`, ou `removed_from_menu` quand `AllPricesZero`) ;
6. `setMenuUpdated` + invalidation du cache **une seule fois en fin de lot**.
