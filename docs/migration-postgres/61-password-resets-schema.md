# 61 — Schéma `password_resets` (flux « Mot de passe oublié »)

Étape 1/7 de l'implémentation de la fonctionnalité « Mot de passe oublié », absente du produit
jusqu'ici (aucune route publique de récupération côté API, dialogue non branché côté POS Flutter,
aucun lien côté back-office). Ce rapport ne couvre que le **schéma** ; le code Go arrive à l'étape 2.

Aucune donnée réelle n'est citée ci-dessous.

## 1. Livrables

| Fichier | Contenu |
|---|---|
| [`migrations/078_password_resets.up.sql`](../../migrations/078_password_resets.up.sql) | `CREATE TABLE password_resets` + 3 index + commentaires |
| [`migrations/078_password_resets.down.sql`](../../migrations/078_password_resets.down.sql) | `DROP TABLE IF EXISTS password_resets` |
| [`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) | Table ajoutée entre `packages` et `payments` (ordre alphabétique du fichier) |

**Numéro `078`** : vérifié libre dans `migrations/` (max `077`) **et** dans `migrations/done/`
(max `068`) — pas de collision comme celle qu'avait dû corriger le [rapport 56](56-haccp-traceability-integration.md).

**DDL Postgres uniquement.** Hypothèse validée avec le demandeur : la base réelle tourne désormais
sur Postgres, aucun fichier jumeau en DDL MySQL n'est produit. C'est la première migration du dépôt
créant une table **nativement Postgres**, jamais passée par MySQL — il n'y a donc aucune règle de
conversion de type à documenter, contrairement aux tables issues du dump.

## 2. Schéma

```
id            varchar(64)  PK
user_id       varchar(64)  NOT NULL
token_hash    varchar(64)  NOT NULL UNIQUE
expires_at    timestamptz  NOT NULL
used_at       timestamptz  NULL
requested_ip  varchar(45)  NULL
created_at    timestamptz  NOT NULL DEFAULT now()
```

### Choix de conception

**Le token n'est jamais stocké en clair** — seul son `sha256` hex (64 caractères) est persisté. Une
fuite de la table (dump, logs, accès en lecture) ne permet donc pas de forger un lien valide.

**`varchar(64)` et non `char(64)`** pour `token_hash`, bien que la longueur soit fixe : en Postgres
`char(n)` est un `bpchar`, complété par des espaces et à la sémantique de comparaison surprenante sur
les espaces significatifs. `varchar` est aussi ce qu'utilise tout le reste du schéma cible.

**Usage unique garanti par la base.** La consommation du token se fera (étape 2) par un CAS atomique :

```sql
UPDATE password_resets SET used_at = now()
WHERE token_hash = ? AND used_at IS NULL AND expires_at > now()
```

suivi d'un contrôle `RowsAffected == 1`. Deux clics concurrents sur le même lien : un seul gagne.

**Pourquoi pas Redis**, comme le fait l'OTP MFA existant ([`auth/service.go`](../../internal/modules/auth/service.go)) —
c'était la proposition initiale, abandonnée après revue de [`redis/client.go`](../../internal/infrastructure/redis/client.go) :

- `Get` (l. 47-62) journalise les erreurs mais renvoie `("", false)` : une panne Redis rendrait tous
  les liens « invalides ou expirés », sans distinction avec un vrai token périmé ;
- `Delete` (l. 94-100) fait `_ = c.rdb.Del(...).Err()` et **renvoie `true` inconditionnellement** :
  l'usage unique reposant dessus n'était pas garanti, même Redis en bon état — un échec silencieux
  laissait le lien rejouable jusqu'à expiration du TTL.

Redis reste utilisé à l'étape 3, mais uniquement comme **throttle best-effort par IP**, jamais comme
source de vérité. Le rate limit par compte s'appuiera sur un `COUNT(*)` de cette table.

**Aucune FK**, conformément à la convention du chantier (« pas de nouvelles FK »). La candidate
`user_id -> users.user_id` est listée en commentaire au-dessus de la table dans le schéma cible.

**Pas de colonne `enabled`** : la convention de soft delete du dépôt n'a pas de sens pour une table
de jetons éphémères purgés par cron.

### Index

| Index | Sert à |
|---|---|
| `uq_password_resets_token_hash` (UNIQUE) | Lookup de consommation — 1 accès par appel à `/auth/reset-password` |
| `idx_password_resets_user_created` | Rate limit par compte : demandes d'un `user_id` sur la dernière heure |
| `idx_password_resets_created_at` | Purge quotidienne (étape 5) |

Le troisième index peut sembler superflu sur une table maintenue petite par le rate limit. Il est
justifié par le plafond de **1 connexion ouverte** du pool ([`internal/database/postgres.go`](../../internal/database/postgres.go),
options alignées sur la config MySQL le temps de la bascule) : un `DELETE` en seq scan bloquerait
l'unique connexion de l'API pendant toute sa durée.

## 3. Statut d'exécution

| Environnement | Statut |
|---|---|
| Postgres dev (Docker `welloresto-postgres-dev`, `localhost:5433`) | ✅ appliquée et vérifiée le 2026-08-02 |
| Postgres staging (Render, `welloresto_staging`) | ✅ appliquée et vérifiée le 2026-08-02 |
| **Production** | 🔴 **NON appliquée** — aucune URL de production présente dans l'environnement (seul `RENDER_STAGING_DATABASE_URL` est défini) |

### Vérifications exécutées (Postgres 16)

| Test | Résultat |
|---|---|
| Application de l'`up` | ✅ `CREATE TABLE` + 3 `CREATE INDEX` + 4 `COMMENT` |
| Rejeu de l'`up` (idempotence) | ✅ `NOTICE ... already exists, skipping`, aucune erreur |
| `down` puis contrôle | ✅ `DROP TABLE`, `to_regclass` → absente |
| Re-`up` après `down` | ✅ |
| CAS de consommation, 1er passage | ✅ `UPDATE 1` |
| CAS rejoué sur le même token | ✅ `UPDATE 0` — usage unique confirmé |
| CAS sur token expiré | ✅ `UPDATE 0` |
| Doublon de `token_hash` | ✅ rejeté par `uq_password_resets_token_hash` |
| Plan du lookup de consommation | ✅ `Index Only Scan using uq_password_resets_token_hash` |

Le fichier reste dans `migrations/` tant qu'il n'a pas été appliqué en **production**, puis sera
déplacé vers `migrations/done/` par `git mv` (convention du [rapport 60](60-mysql-migrations-status-checklist.md)).

Pour appliquer sur un autre environnement :

```bash
docker exec -i -e PGURL="<url>" welloresto-postgres-dev sh -c 'psql -v ON_ERROR_STOP=1 "$PGURL"' \
  < migrations/078_password_resets.up.sql
```

## 4. Constat annexe — dérive du schéma cible

En cherchant le point d'insertion, on constate que
[`04-schema-postgres-target.sql`](04-schema-postgres-target.sql) n'a pas suivi toutes les migrations
récentes :

| Table | Migration | Présente dans le schéma cible ? |
|---|---|---|
| `planning_day_comments` | 065 | ✅ |
| `haccp_traceability_records` | 067 (done) | ✅ |
| `outbound_messages` | 072 | ❌ **absente** |
| `planning_published_shift_snapshots` | 073 | ❌ **absente** |

Une instance Postgres reconstruite à partir de ce seul fichier n'aurait donc pas ces deux tables.
Hors périmètre de ce ticket, **à traiter séparément** — signalé ici pour ne pas le perdre.

## 5. Suite

Étape 2 : repository + service (`RequestPasswordReset` / `ConfirmPasswordReset`), avec les
requêtes écrites en placeholders `?` et exécutées via `dbx.GetDB` — le rebind `?` → `$N` est
assuré par [`dbx.Rebind`](../../internal/database/dbx/dialect.go) selon `DB_DIALECT`.
