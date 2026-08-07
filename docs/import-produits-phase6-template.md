# Phase 6 — Endpoint template `GET /menu/import/template`

Phase 6 de la feature « Importer des produits ». Elle livre le **modèle vierge** que télécharge le
restaurateur qui n'a pas d'export à fournir : il le remplit, le renvoie, et le provider `wello-generic`
(phase 3) le relit.

Aucune lecture ni écriture en base : le modèle ne dépend pas du marchand, seulement du format attendu par le
parser.

Amont : [phase 3](import-produits-phase3-modele-canonique.md) (`WelloGenericProvider`) ·
[phase 4](import-produits-phase4-preview.md) · [phase 5](import-produits-phase5-commit.md).

## 1. Livrables

```
internal/modules/menu/importer/
├── template.go        TemplateColumn · TemplateProvider · implémentation wello-generic
└── template_test.go   round-trip · résolution des en-têtes · disposition des feuilles

internal/modules/menu/
├── import_service.go       + ImportTemplate(slug) (données, nom de fichier, erreur)
├── import_handler.go       + DownloadImportTemplate
└── import_handler_test.go  + 200 relisible · 4 cas de 400 · RBAC

cmd/api/routes.go   GET /menu/import/template
```

## 2. Le piège des unités, et pourquoi il compte

Le brief initial proposait une ligne d'exemple à `950 / 950 / 1050`. Ces valeurs sont des **centimes** — mais
`WelloGenericProvider` passe chaque cellule de prix dans `parsePriceCents`, qui convertit **euros →
centimes**. Un modèle rempli sur cet exemple aurait produit des prix **cent fois trop élevés**.

Les deux unités sont voulues, et diffèrent par porte :

| Porte | Unité | Qui remplit |
|---|---|---|
| `.xlsx` (ce modèle) | **euros**, virgule FR (`9,50`) | un restaurateur, dans un tableur |
| JSON de saisie de masse (phase 4) | **centimes** | le back-office, qui convertit déjà |

L'exemple est donc en euros, et `TestWelloGenericTemplateRoundTrip` asserte `PriceIn == 950` centimes à
partir de `9,50`. C'est précisément le genre de divergence que ce test existe pour attraper.

## 3. Le provider est la source de vérité

Le modèle est décrit **dans le provider**, pas dans le handler, via une interface facultative :

```go
type TemplateProvider interface {
    ImportProvider
    TemplateColumns() []TemplateColumn
    TemplateFilename() string
    BuildTemplate(w io.Writer) error
}
```

Zelty ne l'implémente pas — c'est un export produit par un logiciel tiers, il n'y a pas de modèle Wello à
proposer. **L'absence d'implémentation suffit à l'exclure** : aucune liste de providers à tenir à jour
quelque part, et le jour où un second provider aura un modèle, il suffira qu'il implémente l'interface.

Le service fait `registry.Get(slug)` puis une assertion de type ; provider inconnu et provider sans modèle
donnent deux 400 distincts.

### En-têtes : deux jeux de libellés, une seule vérité

`welloGenericLabels` (phase 3) est en ASCII sans accents — il sert aux **messages d'erreur**.
`welloGenericTemplate` porte les en-têtes **accentués**, parce qu'un restaurateur les lit. Les deux se
rejoignent par `foldHeader`, qui replie les accents avant de consulter la table d'alias.

**Deux filets anti-divergence, indépendants :**

1. `TestWelloGenericTemplateHeadersResolve` — chaque en-tête écrit dans le modèle doit être reconnu par
   `welloGenericAliases` **et pointer sur le champ attendu** ; aucune colonne dupliquée ; toutes les colonnes
   obligatoires du parser présentes et annoncées obligatoires ; les 10 champs couverts. Si ça casse, le test
   dit *quelle* colonne.
2. `TestWelloGenericTemplateRoundTrip` — le modèle est généré, puis relu par `WelloGenericProvider.Parse`, et
   la ligne d'exemple doit produire un `IntermediateImport` correct de bout en bout. Si ça casse, on sait que
   le contrat est rompu même si les en-têtes ont l'air justes.

## 4. Contenu du classeur

**Feuille 1 « Produits »** — position **contractuelle** : `readSheetRows` lit `GetSheetList()[0]`, et
`TestWelloGenericTemplateSheetLayout` verrouille l'ordre. Ligne 1 : en-têtes en gras sur fond léger, **volet
figé** (vérifié dans le XML produit, excelize n'ayant pas de getter). Ligne 2 : l'exemple. Largeurs par
colonne.

**Feuille 2 « Mode d'emploi »** — une ligne par colonne (obligatoire ? exemple ? explication), puis les
règles générales : ne pas renommer les colonnes, prix en euros, tous les prix à 0 = produit retiré de la
carte, rien n'est enregistré avant validation de l'aperçu.

**Pas d'astérisque ni d'unité dans les en-têtes.** « Nom * » ou « Prix sur place (€) » ne seraient plus
reconnus — la correspondance se fait sur le libellé exact replié. L'information va donc dans la feuille
d'aide.

Toutes les valeurs sont posées en chaîne (`SetCellStr`) pour que `9,50` reste `9,50` à l'écran. Un
restaurateur qui retape la ligne produira une cellule numérique, que le parser accepte aussi — `parsePriceCents`
tolère les deux séparateurs décimaux.

## 5. Endpoint

```
GET /menu/import/template?provider=wello-generic
```

Gardé par `RequirePermission(HasMenuAccess)`, comme les deux autres routes d'import.

| Cas | Réponse |
|---|---|
| succès | `200` · `Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` · `Content-Disposition: attachment; filename="wello-modele-import-produits.xlsx"` · `Content-Length` |
| `provider` absent ou vide | `400 missing_provider` |
| `provider` inconnu | `400 unknown_provider` |
| `provider=zelty` | `400 template_unavailable` |

**`attachment`, pas `inline`** : c'est un fichier à remplir, comme l'export comptable
([pos/accounting/handler.go:87](../internal/modules/pos/accounting/handler.go#L87)), pas un document à
consulter comme l'affiche allergènes ([menu/handler.go:224](../internal/modules/menu/handler.go#L224)).

**Aucune valeur par défaut sur `provider`** : un défaut implicite masquerait un appel fautif du back-office
le jour où un second provider proposera un modèle. Le front passe toujours le paramètre.

## 6. Tests

`go build`, `go vet` (avec et sans le tag `postgres_integration`) verts. Le paquet `importer` passe de 71 à
**77** tests, le paquet `menu` de 19 à **22**.

- **Round-trip** : modèle → `Parse` → 1 produit, `9,50 €` → **950 centimes**, `10,50 €` → 1050, TVA 10 sur les
  trois canaux, catégorie `NOS PIZZAS` explicite et rattachée, 2 tags découpés sur la virgule.
- **Résolution des en-têtes** : les 10 en-têtes accentués résolvent vers leur propre champ, sans doublon, avec
  les 3 obligatoires bien marquées.
- **Disposition** : 2 feuilles, « Produits » en première position, 2 lignes, en-têtes exacts, `D2 == "9,50"`.
- **Volet figé** : `ySplit="1"` et `state="frozen"` présents dans `xl/worksheets/sheet1.xml`.
- **`TemplateColumns()`** décrit exactement le fichier généré, et chaque colonne porte une explication.
- **Disponibilité** : `wello-generic` expose un modèle, `zelty` non.
- **Endpoint** : en-têtes de téléchargement corrects, corps **relu par le parser** (c'est bien ce fichier-là
  qui sort), 4 cas de 400, RBAC composée comme dans `routes.go`. Aucune attente SQL déclarée : la moindre
  requête ferait tomber le test.
