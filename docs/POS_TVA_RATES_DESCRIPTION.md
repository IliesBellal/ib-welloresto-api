# POS — `description` sur GET /pos/tva_rates

Distribution de `tva_categories.tva_desc` dans chaque `rate.description` de
`GET /pos/tva_rates`.

## 1. Contrat d'API

```jsonc
// GET /pos/tva_rates  ->  200
[
  {
    "id": "59",
    "name": "Sur place",
    "delivery_type": "0",
    "rates": [
      {
        "id": "12",
        "value": 10,
        "label": "TVA 10%",            // tva_categories.tva_title (inchangé)
        "description": "Restauration sur place"  // tva_categories.tva_desc (nouveau)
      }
    ]
  }
]
```

- **Type** : `string`, jamais `null` — `tva_categories.tva_desc` est
  `varchar(150) NOT NULL` (cf. `docs/migration-postgres/04-schema-postgres-target.sql:3722`).
- Champ **additif** : aucun champ existant renommé ni retiré, les clients
  actuels ne cassent pas.

## 2. Implémentation

| Couche | Fichier | Changement |
|---|---|---|
| Modèle | `internal/modules/pos/models.go` | `Description string \`json:"description"\`` sur `Rate` |
| Repo | `internal/modules/pos/repository.go` | `t.tva_desc` ajouté au `SELECT` + au `Scan` de `GetTVARates` |

Ni le service ni le handler ne changent : ils passent la struct telle quelle.

## 3. Décisions

1. **`string` et non `*string`.** La colonne est `NOT NULL` dans le schéma
   cible comme dans le DDL MySQL d'origine : un pointeur n'aurait ajouté qu'un
   `null` impossible à produire côté client.
2. **Nom de clé `description`** (et pas `desc`) — demandé tel quel, et
   cohérent avec le reste des payloads du repo.
3. **Pas de colonne ajoutée au `GROUP BY` / à l'agrégation.** `tva_desc` est
   porté par la ligne `tva_categories`, donc par le `Rate`, pas par le
   `ConsumptionType` qui regroupe : le champ est posé au bon niveau, aucun
   risque de valeur arbitraire choisie parmi N lignes.
4. **`Rate` n'a qu'un seul producteur** (`GetTVARates`, vérifié par recherche
   sur `Rate{`) : pas d'autre endroit à mettre à jour, donc pas de risque de
   `"description": ""` renvoyé par une autre route.

## 4. Exploitation

Aucune migration, aucune variable d'environnement. Une `description` vide en
réponse signifie une ligne `tva_categories` dont `tva_desc` est une chaîne
vide — donnée de référentiel à corriger, pas un bug de l'API :

```sql
SELECT tva_id, delivery_type, tva_title, tva_desc FROM tva_categories WHERE enabled = true;
```

## 5. Tests — statut d'exécution honnête

| Test | Portée | Statut |
|---|---|---|
| `TestPOSRepository_Postgres` (étendu, `postgres_integration_test.go`) | seed `tva_desc = 'itest-pos-tva-desc'` puis assert `rate.description` **et** `rate.label` (pour prouver que les deux colonnes ne sont pas confondues) | ⚠️ **non exécuté** — `POSTGRES_URL` non défini sur le poste ; compile (`go vet -tags postgres_integration` OK) mais *skippé* |
| `go build ./...`, `go vet` | tout le repo | ✅ OK |
| `go test ./internal/modules/pos/...` | package `pos` sans tag : **aucun fichier de test** ; `pos/accounting` PASS | ✅ OK (ne couvre pas ce changement) |

Le paquet `pos` n'a pas de test unitaire hors tag `postgres_integration` : la
vérification réelle de ce champ reste à faire tourner avec `POSTGRES_URL`.
