# 28 — Élargissement des colonnes varchar tronquées (Tier 3, écart transverse #7)

Suite à [27-tier3-conversion-log.md](27-tier3-conversion-log.md) (écart transverse #7) : trois
colonnes varchar étaient trop étroites pour les valeurs réellement générées côté Go, ce que
MySQL non-strict tronquait silencieusement et que Postgres rejette (`22001`). Une troncature
Go identique avait été ajoutée pendant le Tier 3 pour préserver le comportement — ce chantier
élargit les colonnes pour que la troncature ne soit plus nécessaire, puis retire ce code.

## 1. Colonnes concernées et longueur réelle générée

| Table.colonne | Largeur actuelle | Générateur Go | Longueur réelle produite |
|---|---|---|---|
| `users.token` | varchar(30) | `generateToken()` (`internal/modules/users/repository.go`), utilisé par `UpdatePassword` pour `newUserToken`/`newMerchantToken` | **128 hex chars** (64 octets aléatoires) — dépasse même la cible varchar(64) |
| `customer_loyalty_progress.id` | varchar(50) (PRIMARY KEY) | `helpers.GeneratePrefixedID("loyalty-progress")` (flux manuel `UpdateLoyaltyProgress`) | 54 chars (`loyalty-progress-` + UUID 36 chars) — dépassait déjà 50 |
| `customer_loyalty_progress.id` (autre flux) | idem | `helpers.GeneratePrefixedID("cus-progress")` (flux commande `UpdateLoyaltyFromOrder`) | 49 chars — tenait déjà dans 50, jamais tronqué |
| `customer_loyalty_progress_order.progress_id` | varchar(30) | copie de `customer_loyalty_progress.id` (l'un ou l'autre flux ci-dessus) | jusqu'à 54 chars — dépassait 30 |

**Écart non prévu par l'audit initial** : `users.token` est alimenté par deux générateurs
différents. `create_service.go` (`CreateUser`) utilise `helpers.GenerateToken(30)`, dont le
commentaire affirmait à tort produire "30 caractères" — en réalité `hex.EncodeToString` double
la longueur en octets, donc **60 caractères** (tenait déjà dans varchar(64) sans modification).
`repository.go` (`UpdatePassword`) utilise en revanche `generateToken()` avec 64 octets
aléatoires → **128 caractères**, qui dépasse encore varchar(64). Élargir à varchar(64) seul
n'aurait donc pas éliminé le besoin de troncature pour ce chemin précis.

**Décision (validée avec l'utilisateur)** : réduire `generateToken()` de 64 à 32 octets
aléatoires → 64 caractères hex exactement, qui remplissent varchar(64) sans troncature. Cela
reste 256 bits d'entropie (largement suffisant pour un token de session), et élimine
complètement le besoin de troncature Go sur `users.token`, conformément à l'objectif du
chantier. Commentaires corrigés dans `create_service.go` (la mention "30-char" était fausse).

## 2. Requête de vérification à faire tourner en prod avant d'appliquer la migration

Aucun accès aux données de production n'était disponible ici. Deux colonnes sont protégées
structurellement : `users.token` n'a **aucune contrainte UNIQUE** (seul un index sur `name`
existe sur `users`), donc deux valeurs tronquées identiques ont pu être stockées sans erreur —
risque réel de collision de token d'authentification (`GetUserByToken` fait `WHERE ur.token = ?
OR u.token = ?`) à vérifier explicitement. `customer_loyalty_progress.id` est en revanche la
**PRIMARY KEY** de sa table : toute collision de troncature aurait déjà fait échouer l'INSERT
en erreur 1062 au moment des faits (pas de corruption silencieuse possible), mais la requête
ci-dessous vaut d'être lancée par prudence. `progress_id` n'est pas contraint UNIQUE.

```sql
-- 1. users.token : recherche de doublons parmi les tokens actuels (risque d'auth croisée)
SELECT token, COUNT(*) AS n
FROM users
WHERE token IS NOT NULL AND token <> ''
GROUP BY token
HAVING n > 1;

-- 2. Distribution des longueurs stockées — une concentration à exactement 30
--    caractères indique des valeurs déjà tronquées (donc potentiellement collisionnées)
SELECT LENGTH(token) AS len, COUNT(*) AS n FROM users GROUP BY len ORDER BY len DESC;

-- 3. customer_loyalty_progress.id : PRIMARY KEY, doublons structurellement impossibles,
--    vérification de principe uniquement
SELECT id, COUNT(*) AS n FROM customer_loyalty_progress GROUP BY id HAVING n > 1;
SELECT LENGTH(id) AS len, COUNT(*) AS n FROM customer_loyalty_progress GROUP BY len ORDER BY len DESC;

-- 4. customer_loyalty_progress_order.progress_id : pas de contrainte UNIQUE, mais une
--    concentration à 30 caractères indiquerait des références ambiguës vers plusieurs
--    progress_id distincts tronqués identiquement
SELECT progress_id, COUNT(*) AS n
FROM customer_loyalty_progress_order
GROUP BY progress_id
HAVING n > 1;
SELECT LENGTH(progress_id) AS len, COUNT(*) AS n FROM customer_loyalty_progress_order GROUP BY len ORDER BY len DESC;
```

Si la requête 1 remonte des lignes, élargir la colonne ne corrige pas rétroactivement
l'ambiguïté déjà en base : les tokens concernés doivent être régénérés (rotation manuelle,
par exemple via un appel `UpdatePassword` ou un script dédié) après la migration.

## 3. Migration MySQL

[`migrations/066_widen_varchar_columns.up.sql`](../../migrations/066_widen_varchar_columns.up.sql) /
[`.down.sql`](../../migrations/066_widen_varchar_columns.down.sql) — prochain numéro libre après
065 (`065_planning_day_comments` déjà pris).

```sql
ALTER TABLE users
  MODIFY token varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL;

ALTER TABLE customer_loyalty_progress
  MODIFY id varchar(64) NOT NULL;

ALTER TABLE customer_loyalty_progress_order
  MODIFY progress_id varchar(64) NOT NULL;
```

**Impact index/clé** : élargir un varchar(50)/(30) vers varchar(64) reste dans la même classe
de stockage (préfixe de longueur sur 1 octet, valable jusqu'à 255) — MySQL exécute ce `MODIFY`
en place, sans reconstruire les index existants, y compris pour `customer_loyalty_progress.id`
qui est la PRIMARY KEY de sa table. Aucun index secondaire n'est affecté.

> ⚠️ **Changement de schéma MySQL réel en production**, distinct de la bascule Postgres à venir.
> À appliquer et tester séparément (staging d'abord), après avoir fait tourner les requêtes de
> vérification ci-dessus. Non exécutée par ce chantier.

## 4. Schéma cible Postgres

[`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) mis à jour en conséquence :
`users.token`, `customer_loyalty_progress.id`, `customer_loyalty_progress_order.progress_id` →
`varchar(64)`. Revalidation `pglast` (même méthode que les rapports
[13](13-merchant-id-schema-update.md)/[18](18-order-id-schema-update.md)/[26](26-planning-day-comments-integration.md)) :

```
PARSE OK - 457 statements
```

Même compte qu'après le rapport 26 (aucune instruction ajoutée/retirée, seules des longueurs de
colonnes modifiées).

## 5. Code Go retiré / modifié

- `internal/modules/users/repository.go` — `UpdatePassword` : retrait de la troncature
  `newUserToken[:30]` (bloc de 7 lignes) devenue inutile. `generateToken()` réduit de 64 à 32
  octets aléatoires (256 bits, 64 caractères hex exacts pour varchar(64)).
- `internal/modules/users/create_service.go` — commentaires corrigés sur `GenerateToken(30)`
  (produisait déjà 60 caractères, pas 30 ; comportement inchangé, tenait déjà dans varchar(64)).
- `internal/modules/customers/repository.go` :
  - retrait de `truncateVarchar(helpers.GeneratePrefixedID("loyalty-progress"), 50)` →
    `helpers.GeneratePrefixedID("loyalty-progress")` (INSERT `customer_loyalty_progress.id`).
  - retrait de `truncateVarchar(progressID, 30)` → `progressID` (INSERT
    `customer_loyalty_progress_order.progress_id`).
  - suppression de la fonction `truncateVarchar` elle-même (plus aucun appelant).

## 6. Vérification

- `go build ./...` : OK.
- `go test ./internal/modules/users/... ./internal/modules/customers/...` : OK.
- Validation `pglast` de `04-schema-postgres-target.sql` : OK, 457 statements, aucune erreur.

Aucun autre module touché.
