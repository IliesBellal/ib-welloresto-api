# 47 — API locale branchée sur Postgres dev, prête pour wello-back-office

Objectif : faire tourner l'API en local sur le Postgres Docker de dev (déjà chargé avec les
données de l'établissement de test) et vérifier qu'elle est appelable depuis `wello-back-office`
en local. Aucune donnée réelle n'est citée dans ce rapport.

## 1. Blocage trouvé : `DB_DIALECT=postgres` ne branchait rien dans le vrai serveur

Le câblage `DB_DIALECT` / `POSTGRES_URL` n'existait jusqu'ici que côté tests
([`pgtest.Open`](../../internal/database/dbx/pgtest/pgtest.go), tag `postgres_integration`).
Le point d'entrée réel, [`cmd/api/main.go`](../../cmd/api/main.go), ouvrait toujours MySQL en dur
(`database.NewMySQL(cfg.Database)`), et [`config.validate()`](../../internal/config/config.go)
faisait un `log.Fatal` si `MYSQL_URL` était vide, quel que soit `DB_DIALECT`. Le reste du code
(repositories, `dbx.Rebind`, `InsertReturningID`...) savait déjà gérer les deux dialectes — il
manquait juste la sélection du driver de connexion au démarrage.

**Correctif appliqué** (additif, gardé par `DB_DIALECT`, comportement MySQL/prod inchangé par
défaut) :

- [`cmd/api/main.go`](../../cmd/api/main.go) : sélectionne `database.NewPostgres` ou
  `database.NewMySQL` selon `dbx.ActiveDialect()`.
- [`internal/config/config.go`](../../internal/config/config.go) : `validate()` exige
  `POSTGRES_URL` en mode Postgres, `MYSQL_URL` sinon (au lieu d'exiger `MYSQL_URL` dans tous les
  cas).

Validé par `go build ./cmd/api` puis un run réel (voir §4) : la requête `POST /auth/login` avec
des identifiants bidon renvoie `"status":"user_not_found"` — la réponse vient bien d'une requête
SQL exécutée sur le Postgres dev, pas d'une erreur de connexion.

## 2. Port : conflit avec le dev server de wello-back-office

`wello-back-office` (Vite) sert son dev server sur le port **8080** par défaut
(`server.port: 8080` dans son `vite.config.ts`, pas de override dans `package.json`). C'est
aussi le port par défaut de l'API (`PORT` fallback `"8080"` dans `internal/config/config.go`) —
les deux ne peuvent pas tourner en même temps sur 8080.

La whitelist CORS de l'API contient déjà `http://localhost:8080` **et** `http://localhost:8081`
pour le dev (voir §3) — l'API tourne donc sur **8081** en local, laissant 8080 au back-office.

## 3. État de la config CORS — aucune modification nécessaire

[`internal/middleware/cors.go`](../../internal/middleware/cors.go) whitelist déjà, dans la
section « Dev » de `CORSMiddleware()` (middleware global appliqué dans `routes.go`) :

```go
"http://localhost:8080",
"http://localhost:8081",
```

avec `AllowCredentials: true` et les méthodes/headers usuels (`GET, POST, PUT, PATCH, DELETE,
OPTIONS`, `Authorization`, `Content-Type`, etc.). Vérifié en conditions réelles (preflight CORS
depuis l'origine `http://localhost:8080`, celle du dev server back-office) :

```
OPTIONS /health  Origin: http://localhost:8080
→ HTTP 204
  Access-Control-Allow-Origin: http://localhost:8080
  Access-Control-Allow-Credentials: true
  Access-Control-Allow-Methods: GET
```

**Aucun ajustement CORS n'a été appliqué ni n'est nécessaire** pour ce scénario (back-office sur
son port par défaut 8080 → API sur 8081). Si le back-office tourne un jour sur un autre port
(3000, 5173, etc. — pas le cas actuellement, son `vite.config.ts` est en dur sur 8080), il
faudrait ajouter cette origine à `AllowedOrigins` dans `CORSMiddleware()` (et au miroir
`allowedOrigins` de `SetCORSHeaders`, utilisé pour les réponses d'erreur early) — un vrai
changement de comportement, à valider avant application.

## 4. Comment démarrer l'API en local (commande exacte)

Prérequis : conteneurs Docker `welloresto-postgres-dev` (Postgres, port 5433) et `redis-local`
(Redis, port 6379) démarrés — les deux existaient déjà sur la machine et ont été (re)démarrés
pour ce test :

```bash
docker start welloresto-postgres-dev redis-local   # si pas déjà "Up"
```

Puis, depuis la racine du repo :

```bash
DB_DIALECT=postgres \
POSTGRES_URL="postgres://welloresto:dev_local_only@localhost:5433/welloresto_dev?sslmode=disable" \
REDIS_URL="redis://localhost:6379/0" \
PORT=8081 \
GOOGLE_API_KEY="dev-placeholder" \
R2_PRIVATE_BUCKET="dev-placeholder" \
PIN_PEPPER="dev-placeholder-pepper" \
ENV=local \
go run ./cmd/api
```

- `GOOGLE_API_KEY`, `R2_PRIVATE_BUCKET`, `PIN_PEPPER` : requis par `config.validate()` au
  démarrage mais non exercés par un simple `/health` ou `/auth/login` — des valeurs bidon
  suffisent pour ce test. Pour des flux qui touchent réellement Google Maps, R2 ou le hachage de
  PIN, utiliser les vraies valeurs de dev.
- Pas de `MYSQL_URL` nécessaire en mode `DB_DIALECT=postgres` (cf. correctif §1).
- Laisser tourner le process au premier plan dans un terminal dédié (ou l'arrière-plan de votre
  choix) tant que `wello-back-office` doit l'appeler. `Ctrl+C` pour arrêter.

**Côté `wello-back-office`** : pointer `VITE_API_BASE_URL` vers `http://localhost:8081` (sinon il
retombe sur son défaut, `https://welloresto-api-prod.onrender.com` — voir
[`src/services/apiClient.ts`](../../../wello-back-office/src/services/apiClient.ts)), par exemple
via un `.env.local` :

```
VITE_API_BASE_URL=http://localhost:8081
```

## 5. Vérifications faites

```bash
curl http://localhost:8081/health
# → 200 "OK"

curl -X POST http://localhost:8081/auth/login -H "Content-Type: application/json" \
  -d '{"email":"nope@example.com","password":"wrong"}'
# → 404 {"id":"auth.login","data":{"error":"User not found","status":"user_not_found"}}
# (confirme une requête SQL réelle exécutée côté Postgres, pas une erreur de connexion)

curl -X OPTIONS http://localhost:8081/health \
  -H "Origin: http://localhost:8080" -H "Access-Control-Request-Method: GET"
# → 204, Access-Control-Allow-Origin: http://localhost:8080
```

L'API tourne actuellement sur ce process local (port 8081, `DB_DIALECT=postgres`) pour permettre
un test immédiat depuis `wello-back-office`.
