# RBAC lot 4 — bascule des comptes vers le rôle Administrateur (production)

Date : 2026-08-27 · Branche : `staging`

Ce document est destiné à être suivi en production par quelqu'un qui n'a pas
suivi les échanges qui ont mené à cette décision. Il liste, dans l'ordre
exact, ce qu'il faut exécuter, ce qu'on doit voir à chaque étape, et comment
revenir en arrière si quelque chose ne va pas.

## Contexte et décision produit

Décision actée : **tous les comptes passent au rôle Administrateur**, en
recette comme en production. Pas d'inventaire préalable des droits actuels,
pas de rôle intermédiaire — le parc de comptes est connu et les droits par
rôle ne sont pas encore exploités depuis une interface. Les quelques profils
non-admin repérés en recette sont assumés : la recette n'est utilisée que par
les développeurs.

**Effet immédiat et concret** : dès qu'un compte se voit assigner un
`role_id`, `UserLoginRow.Has()` ([internal/modules/auth/permissions.go](../internal/modules/auth/permissions.go))
bascule ce compte des colonnes booléennes historiques (`admin`,
`access_wrreception`, `manage_menu`, ...) vers les droits du rôle. Le rôle
"admin" porte `permission.All` (tous les droits du catalogue) et court-circuite
toute vérification (`RoleSystemKey == SystemKeyAdmin` renvoie toujours `true`).
Autrement dit : à la seconde où `cmd/assign_admin_role --apply` (étape 3
ci-dessous) écrit une ligne, ce compte devient administrateur complet sur son
établissement, pour toute nouvelle requête. Les sessions déjà en cache Redis
continuent sur les booléens jusqu'à expiration du TTL (60 minutes) ou jusqu'à
la purge optionnelle de l'étape 6.

## Prérequis avant de commencer

- Vérifier qu'aucune autre session/déploiement n'écrit sur la même base au
  moment de l'exécution.
- Confirmer que la variable `POSTGRES_URL` du service pointe bien vers la
  base de production cible, et que `DB_DIALECT=postgres` est bien la valeur
  active sur le service déployé (pas seulement dans un `.env` local).
- Avoir un accès direct à la base (psql ou équivalent) pour les vérifications
  du §5.

---

## 1. Migrations 094 à 099

**Important : instruction par instruction, pas de transaction englobante.**
Chaque fichier `.up.sql` doit être exécuté séparément (`psql -f`, un fichier à
la fois, ou l'équivalent de l'outil de migration utilisé). N'enveloppez pas
l'ensemble dans un `BEGIN ... COMMIT` unique : si une instruction plus loin
dans la série échoue, on veut savoir exactement laquelle, pas rembobiner
silencieusement tout ce qui a déjà réussi.

Dans l'ordre :

| # | Fichier | Contenu |
|---|---|---|
| 094 | [migrations/done/094_roles_schema.up.sql](../migrations/done/094_roles_schema.up.sql) | Schéma RBAC lot 1 : `permissions`, `roles`, `role_permissions`, `users_rights.role_id`, `merchant.default_role_id`, élargissement de deux colonnes `audit_logs` |
| 095 | [migrations/done/095_roles_permissions_catalog.up.sql](../migrations/done/095_roles_permissions_catalog.up.sql) | Seed des 14 permissions du catalogue |
| 096 | [migrations/done/096_seed_system_roles.up.sql](../migrations/done/096_seed_system_roles.up.sql) | No-op SQL (`SELECT 1`) — réservation de numéro, la vraie population est `cmd/seed_system_roles` (étape 2) |
| 097 | [migrations/done/097_permission_pos_status_manage.up.sql](../migrations/done/097_permission_pos_status_manage.up.sql) | Ajoute la 15e permission (`pos.status.manage`) |
| 098 | [migrations/done/098_access_observation.up.sql](../migrations/done/098_access_observation.up.sql) | Table `access_observation` (observation RBAC lot 2) |
| 099 | [migrations/done/099_merchant_default_role_admin.up.sql](../migrations/done/099_merchant_default_role_admin.up.sql) | Repointe `merchant.default_role_id` sur le rôle "admin" de chaque établissement |

Ces six migrations sont déjà **appliquées en recette** (094-098 sous leurs
anciens numéros 089-093 — voir la note en tête de
[094_roles_schema.up.sql](../migrations/done/094_roles_schema.up.sql) pour le
détail de la collision de numérotation qui a motivé le renommage ; 099 a été
exécutée directement sous ce numéro le 2026-08-27, 29 établissements
repointés de "staff" vers "admin", vérifié). En production, aucune des six
n'a jamais été jouée : elles partent de zéro sous ces numéros.

`099` est écrite en syntaxe Postgres (`UPDATE ... FROM`) — cohérent avec le
reste de la série 094-098, qui est déjà écrite pour `DB_DIALECT=postgres`
exclusivement.

---

## 2. `cmd/seed_system_roles`

Crée les rôles système ("admin", "staff") pour chaque établissement existant
et pointe `merchant.default_role_id` sur "admin" (là où il est encore NULL —
ne touche jamais un établissement déjà repointé manuellement).

```
DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/seed_system_roles
```

Attendu : une ligne de log par établissement (« merchant X: system roles
ensured, default_role_id -> admin (role-...) ») et un total en fin d'exécution.
Idempotent — sûr à relancer si besoin.

Sur un environnement neuf (production), c'est **cette étape**, pas la
migration 099, qui pointe effectivement `default_role_id` sur "admin" pour la
première fois : au moment où 099 s'exécute (étape 1), la table `roles` est
encore vide, donc 099 n'a rien à faire — c'est attendu. 099 sert de correctif
pour un environnement où des établissements avaient déjà été seedés *avant*
cette décision (avec l'ancien défaut "staff") — c'est le cas de la recette.

---

## 3. `cmd/assign_admin_role --dry-run`

```
DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/assign_admin_role --dry-run
```

N'écrit rien. Affiche, par établissement, le nombre de lignes `users_rights`
qui seraient assignées au rôle admin, un total, et — si applicable — la liste
des établissements sans rôle admin (ce qui indiquerait que l'étape 2 n'a pas
tourné pour eux, ou qu'un `merchant_id` orphelin existe dans `users_rights`
sans ligne `merchant` correspondante).

**Ce qu'on attend de voir** : un total cohérent avec le nombre de comptes
actifs et inactifs connus de l'établissement (l'outil traite `enabled = TRUE`
et `enabled = FALSE` sans distinction). Si la liste "sans rôle admin" n'est
pas vide, **ne pas passer à `--apply`** avant d'avoir compris pourquoi (very
probablement : l'étape 2 a échoué ou n'a pas couvert cet établissement).

Exemple observé en recette (2026-08-27, avant application) :

```
DRY-RUN — nothing written
  merchant 1: 1 row(s)
  ...
TOTAL: 55 row(s) across 28 merchant(s)
Merchants with NO admin role — left untouched, run cmd/seed_system_roles first:
  - 99999230
```

(`99999230` en recette est un `merchant_id` orphelin dans `users_rights` sans
ligne `merchant` correspondante — donnée de test préexistante, pas un effet de
cette bascule. Voir §5, vérification 1, pour l'impact exact sur le résultat
attendu.)

---

## 4. `cmd/assign_admin_role --apply`

```
DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/assign_admin_role --apply
```

Exécute la même logique que le dry-run, **en transaction** (tout ou rien),
et affiche le même décompte, cette fois réellement écrit. Le total et la
répartition par établissement doivent être identiques à ceux du dry-run de
l'étape précédente — toute divergence signale qu'un autre processus a écrit
sur `users_rights` entre les deux commandes.

Idempotent : si la commande est relancée après un premier `--apply` réussi,
elle ne trouve plus aucune ligne `role_id IS NULL` à traiter pour les
établissements déjà couverts (total = 0 pour eux).

---

## 5. Vérifications d'intégrité

Exécuter les deux requêtes suivantes après l'`--apply` :

```sql
-- doit renvoyer 0 : aucun compte actif sans rôle
SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL;
```

```sql
-- doit renvoyer 0 : un rôle d'un AUTRE établissement serait une faille
SELECT COUNT(*)
FROM users_rights ur JOIN roles r ON r.id = ur.role_id
WHERE r.merchant_id <> ur.merchant_id;
```

La seconde est la plus importante : une valeur non nulle signifierait qu'un
compte a reçu les droits d'un établissement qui n'est pas le sien — à traiter
en urgence, ne pas continuer le déploiement. Elle est aussi couverte en
permanence par
[`TestNoCrossTenantRoleAssignment`](../internal/modules/roles/postgres_integration_test.go)
(`go test -tags postgres_integration ./internal/modules/roles/...`).

**Résultat attendu et résultat réellement observé en recette** (2026-08-27,
après `--apply`) :

| Vérification | Attendu | Observé en recette | Explication si différent |
|---|---|---|---|
| Comptes actifs sans rôle | 0 | **1** | L'unique ligne restante est celle du `merchant_id` orphelin `99999230` vu au §3 (`enabled = TRUE`, `role_id` toujours NULL) : ce `merchant_id` n'existe dans aucune ligne `merchant`, donc aucun rôle admin ne peut légitimement lui être assigné — l'outil l'a correctement laissée de côté plutôt que de créer un rôle pour un établissement fantôme. C'est une anomalie de données préexistante à cette bascule, pas une régression qu'elle introduit. À trancher séparément (désactiver la ligne ? corriger le `merchant_id` ? l'ignorer ?) — hors périmètre de ce runbook. |
| Rôle d'un autre établissement | 0 | 0 | Conforme. |

Si en production la première vérification renvoie autre chose que 0 pour une
raison différente (pas un `merchant_id` orphelin connu), **ne pas continuer**
avant d'avoir identifié la ou les lignes concernées :

```sql
SELECT id, user_id, merchant_id, enabled, admin, role_id
FROM users_rights
WHERE enabled = TRUE AND role_id IS NULL;
```

---

## 6. Purge Redis (optionnelle, coupure nette)

Sans cette étape, les sessions déjà en cache continuent sur les colonnes
booléennes jusqu'à expiration du TTL (`models.UserCacheTTL`, 60 minutes au
plus) — résultat identique côté utilisateur puisque tout le monde est admin
des deux côtés (booléens ou rôle), mais deux régimes cohabitent en vol
pendant cette fenêtre. Pour une coupure nette immédiate :

```
redis-cli --scan --pattern 'user:token:v2:*' | xargs redis-cli DEL
```

(Adapter selon l'outil d'accès Redis disponible sur l'environnement de
production — l'important est de cibler exactement le préfixe
`user:token:v2:*`, pas `FLUSHALL`.)

---

## 7. Procédure de retour arrière

**Si quelque chose ne va pas après l'application**, la commande suivante
restaure intégralement le comportement précédent :

```sql
UPDATE users_rights SET role_id = NULL;
```

Pourquoi ça suffit : `Has()` retombe sur les colonnes booléennes historiques
dès que `role_id` est NULL (voir le §"Effet immédiat" en tête de ce document).
Ces colonnes booléennes n'ont **jamais été modifiées** par cette bascule —
seule `role_id` a été écrite. Cette seule instruction annule donc l'intégralité
de l'effet de `cmd/assign_admin_role`, quel que soit le nombre de lignes qu'il
a traitées, sans avoir besoin de connaître leur état d'avant.

Ce que cette commande **ne défait pas** (et n'a pas besoin de défaire pour que
l'annulation soit complète) :
- les migrations 094-099 (schéma additif, aucune colonne existante modifiée
  ni supprimée) ;
- les rôles créés par `cmd/seed_system_roles` (`roles`, `role_permissions`) ;
- `merchant.default_role_id` (n'est lu qu'à la création d'un nouvel
  utilisateur — voir `internal/modules/pos/create_service.go`,
  `internal/modules/users/create_repository.go`,
  `internal/modules/users/admin_repository.go` — jamais par
  `RequirePermission`/`Has()`).

Si une régression plus profonde est constatée et qu'un retour arrière complet
du schéma est nécessaire, les fichiers `.down.sql` de chaque migration
(094 à 099) existent et se rejouent dans l'ordre inverse (099 → 094),
toujours instruction par instruction.
