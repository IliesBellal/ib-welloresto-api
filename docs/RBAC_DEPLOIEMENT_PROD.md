# RBAC — déploiement en production (lots 1 à 11)

Destiné à être suivi par quelqu'un qui n'a pas suivi les échanges qui ont mené
à cette procédure. La production n'a **jamais reçu** une seule des migrations
RBAC ni la moindre ligne de code de ces lots — ce runbook couvre donc, en une
seule fois, la totalité du travail des lots 1 à 11 (`docs/decisions.md`), pas
seulement la dernière tranche.

**Principe directeur, non négociable : expansion, déploiement, contraction.**
Les migrations additives passent d'abord, restent compatibles avec le code
actuellement déployé. Le nouveau code ensuite. Les suppressions de colonnes
en dernier, seulement quand plus aucun code déployé ne les lit. Une migration
destructive et un déploiement de code ne partent jamais ensemble.

**Ce que ce runbook NE couvre PAS** : `migrations/todo/` contient d'autres
fichiers sans rapport avec RBAC — `087_analytics_indexes` (chantier
analytics), `101_production_profiles`, `102_delivery_travel_seconds`,
`103_production_ready_delivery_arrival` (chantier temps de livraison — noter
la collision de numéro avec `103_permission_catalog_lot10`, ce sont deux
fichiers distincts), `104` à `109` (planning/shifts, logs de requêtes),
`111_multi_account_uber_deliveroo`, `112_pg_stat_statements` (chantier
analytics/multi-compte, explicitement hors périmètre). Ce runbook ne
mentionne et n'ordonne que le sous-ensemble RBAC : **094, 095, 096, 097, 098,
099, 100, 103_permission_catalog_lot10, 110** — et prépare, sans l'exécuter,
**113**.

---

## 1. État de départ à constater

Ne rien supposer — vérifier, avec les requêtes exactes ci-dessous, contre la
base de production et le service déployé.

### 1.1 Schéma — quelles migrations RBAC sont déjà là

Ce dépôt n'a pas de table de suivi de migrations (`schema_migrations`
n'existe pas — confirmé : `SELECT to_regclass('public.schema_migrations')`
renvoie NULL même en staging). L'état s'infère de la présence des objets :

```sql
-- 094 : table roles présente ?
SELECT to_regclass('public.roles');
-- 094 : colonne users_rights.role_id présente ?
SELECT column_name FROM information_schema.columns
WHERE table_name = 'users_rights' AND column_name = 'role_id';
-- 094 : colonne merchant.default_role_id présente ?
SELECT column_name FROM information_schema.columns
WHERE table_name = 'merchant' AND column_name = 'default_role_id';
-- 095/097/100/103 : combien de clés dans le catalogue, et lesquelles
-- (14 après 095 seul, 15 après 097, 13 après 100, 18 après 103 — voir §1.3)
SELECT COUNT(*), array_agg(key ORDER BY key) FROM permissions;
-- 098 : table access_observation présente ?
SELECT to_regclass('public.access_observation');
-- 110 : les 5 colonnes legacy existent-elles encore sur users_rights ?
SELECT column_name FROM information_schema.columns
WHERE table_name = 'users_rights'
  AND column_name IN ('access_wrdelivery', 'access_wrwaiter', 'export_reports', 'export_financials', 'export_customers');
```

Si `roles` n'existe pas (`to_regclass` renvoie NULL), aucune migration RBAC
n'est encore appliquée — c'est l'état attendu au moment de l'écriture de ce
runbook (2026-09-03). Si un résultat contredit cette hypothèse, **arrêter et
comprendre pourquoi** avant de continuer : quelqu'un a peut-être déjà
commencé ce rollout.

### 1.2 Version de l'API actuellement déployée

Il n'existe pas d'endpoint de version dans cette API (`GET /health` renvoie
`"OK"`, rien de plus — vérifié, `cmd/api/routes.go`). Pour savoir quel commit
tourne réellement en production :

1. Consulter l'historique de déploiement du service production sur Render
   (dashboard → service → Events/Deploys) pour trouver le SHA du dernier
   déploiement réussi.
2. `git show <sha>:cmd/api/routes.go | grep -c RequireAdmin` — si la commande
   renvoie `> 0`, le code déployé est antérieur à la phase 4 de ce chantier
   (RequireAdmin existe encore). `git log <sha> -1 --format=%cd` donne la
   date du commit pour se repérer dans `docs/decisions.md`.
3. Confirmer `DB_DIALECT` et `POSTGRES_URL` (pas `MYSQL_URL`) actifs sur le
   service déployé (variables d'environnement Render, pas un `.env` local).

### 1.3 Rappel — trajectoire du nombre de clés du catalogue

Utile pour interpréter le résultat de la requête `COUNT(*) FROM permissions`
en §1.1 et savoir où on en est dans la séquence :

| Après | Nombre de clés | Changement |
|---|---|---|
| 095 | 14 | seed initial |
| 097 | 15 | + `pos.status.manage` |
| 100 | 13 | − `pos.access`, `pos.discount.apply` |
| 103 (lot 10) | 18 | + `pos.analytics`, `bookings.manage`, `platforms.manage`, `kiosk.manage`, `seating_plan.manage` |

18 est le compte final attendu à la fin de la vague A ci-dessous.

---

## 2. Vague A — maintenant, sans risque (additive, sans dépendance au code déployé)

Ces migrations créent des tables/colonnes que **le code actuellement déployé
en production ne lit ni n'écrit** (il ne connaît pas `roles`,
`role_permissions`, `permissions`, `role_id` — ces objets n'existent nulle
part dans son code). Elles peuvent partir avant tout déploiement, à
n'importe quel moment, sans coordination avec un déploiement de code.

**Instruction par instruction, pas de transaction englobante** — chaque
fichier `.up.sql` exécuté séparément (`psql -f`, un fichier à la fois), pour
qu'un échec plus loin dans la série n'efface pas silencieusement ce qui a
déjà réussi.

| # | Fichier | Contenu | Durée attendue |
|---|---|---|---|
| 1 | `094_roles_schema.up.sql` | Tables `permissions`, `roles`, `role_permissions` ; colonnes `users_rights.role_id`, `merchant.default_role_id` ; élargissement de deux colonnes `audit_logs` | < 1 min (DDL pur, tables vides) |
| 2 | `095_roles_permissions_catalog.up.sql` | Seed des 14 permissions initiales | < 1 min |
| 3 | `096_seed_system_roles.up.sql` | No-op SQL (`SELECT 1`) — réservation de numéro, la vraie population est `cmd/seed_system_roles` (vague B) | instantané |
| 4 | `097_permission_pos_status_manage.up.sql` | Ajoute `pos.status.manage` (15e clé) | < 1 min |
| 5 | `098_access_observation.up.sql` | Table `access_observation` | < 1 min |
| 6 | `099_merchant_default_role_admin.up.sql` | `UPDATE merchant SET default_role_id = ... WHERE default_role_id IS NULL` — n'a **rien** à faire tant que `roles` est vide (aucun rôle admin n'existe encore) ; c'est normal, pas un échec | < 1 min |
| 7 | `100_deprecate_pos_access_and_discount_apply.up.sql` | Retire `pos.access`/`pos.discount.apply` du catalogue (13 clés) — `DELETE FROM role_permissions` d'abord (table vide à ce stade, donc no-op), puis `DELETE FROM permissions` | < 1 min |
| 8 | `103_permission_catalog_lot10.up.sql` | Ajoute 5 clés (18 au total) + backfill des descriptions des 13 existantes | < 1 min |

**Vérification après la vague A** (doit renvoyer exactement 18, avec la
liste des clés du tableau §1.3 après 103) :

```sql
SELECT COUNT(*), array_agg(key ORDER BY key) FROM permissions;
```

**Rien d'autre à vérifier humainement à ce stade** — aucun code déployé ne
consulte encore ces tables, donc aucun comportement utilisateur ne peut avoir
changé. Si un restaurateur ou un compte interne signale quoi que ce soit
d'anormal après cette vague, ce n'est pas cette migration — chercher
ailleurs.

---

## 3. Vague B — accompagne le déploiement du nouveau code

C'est ici que se trouve le seul point délicat de séquencement.

### 3.1 Ordre exact

1. **Déployer la nouvelle version de l'API** (ce dépôt, branche `main` au
   commit qui inclut les lots 1 à 11 — en particulier phases 2-4 de
   `docs/decisions.md` : invariant + réconciliation automatique des rôles
   admin, retrait du court-circuit `system_key` dans `Has()`, retrait de
   `RequireAdmin`). Confirmer le déploiement réussi (health check Render vert,
   `GET /health` répond `200 OK`) avant de passer à l'étape suivante.

   **Pourquoi c'est sûr même si rien d'autre n'a encore tourné** : au moment
   de ce déploiement, `users_rights.role_id` est `NULL` pour 100% des comptes
   (vague A n'en a peuplé aucun). `UserLoginRow.Has()` ne bascule en mode
   « rôle » que si `RoleID != nil` — tant que ce n'est vrai pour personne, le
   retrait du court-circuit `system_key` (phase 3) et le retrait de
   `RequireAdmin` (phase 4, les deux routes concernées passent sous
   `permission.StaffManage`, qui retombe sur `Rights.CanManageUsers` en mode
   historique) sont un **no-op comportemental strict** pour tout compte de
   production à cet instant précis. Le déploiement de code, à lui seul, ne
   change l'accès de personne.

2. **`go run ./cmd/seed_system_roles`** contre la base de production.
   Idempotent, transaction par établissement. Crée les rôles "admin"/"staff"
   pour chaque établissement et les peuple intégralement (l'invariant de la
   phase 2 garantit que "admin" reçoit les 18 clés). Pointe
   `merchant.default_role_id` sur "admin" là où NULL.

   ```
   DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/seed_system_roles
   ```

   Durée attendue : quelques secondes par établissement (une transaction
   courte chacune) — pour un parc de l'ordre de grandeur de celui vu en
   staging (30 établissements), quelques secondes au total. Pas besoin
   d'attendre la tâche cron horaire (`ReconcileSystemRolePermissions`,
   `internal/tasks/rbac.go`) qui aurait fini le travail seule sous 1h : lancer
   la commande donne un résultat immédiat et vérifiable, comme fait en
   staging le 2026-09-03 (voir `docs/decisions.md`, phase 2).

   **Vérification** : une ligne de log par établissement
   (« merchant X: system roles ensured, default_role_id -> admin (role-...) »)
   et un total en fin d'exécution correspondant au nombre d'établissements
   actifs connus.

3. **`go run ./cmd/seed_system_roles` — vérification indépendante de
   l'invariant** (ne dépend pas de la sortie de l'étape 2, relit la base) :

   ```sql
   -- doit renvoyer 0 lignes : aucun rôle admin incomplet
   SELECT r.id, r.merchant_id, COUNT(p.key) AS missing
   FROM roles r
   CROSS JOIN permissions p
   WHERE r.system_key = 'admin' AND p.deprecated_at IS NULL
     AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_key = p.key)
   GROUP BY r.id, r.merchant_id;
   ```

   Ou, équivalent, exécuter le test dédié contre production :
   ```
   DB_DIALECT=postgres POSTGRES_URL=<url production> go test -tags postgres_integration ./internal/modules/roles/... -run TestSystemAdminRolesContainFullCatalog_Postgres -v
   ```

4. **`cmd/assign_admin_role --dry-run`, puis `--apply`** (voir §4 ci-dessous
   pour la règle et la justification — c'est la question « chaque lien
   `users_rights` doit-il recevoir un `role_id` ? » du brief).

   ```
   DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/assign_admin_role --dry-run
   ```
   Lire attentivement la sortie : le total doit être cohérent avec le nombre
   de comptes connus (actifs + inactifs, l'outil traite les deux) ; la liste
   « Merchants with NO admin role » doit être **vide**. Si elle ne l'est pas,
   **ne pas passer à `--apply`** avant d'avoir compris pourquoi (l'étape 2
   n'a probablement pas couvert cet établissement).

   ```
   DB_DIALECT=postgres POSTGRES_URL=<url production> go run ./cmd/assign_admin_role --apply
   ```
   Le total et la répartition par établissement doivent être identiques au
   dry-run — toute divergence signale une écriture concurrente sur
   `users_rights` entre les deux commandes (arrêter et investiguer).

5. **Vérifications d'intégrité**, dans cet ordre :

   ```sql
   -- doit renvoyer 0 : aucun compte actif sans rôle
   SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL;
   ```
   ```sql
   -- doit renvoyer 0 : un rôle d'un AUTRE établissement serait une faille de sécurité
   -- (couvert en continu par TestNoCrossTenantRoleAssignment,
   -- internal/modules/roles/postgres_integration_test.go)
   SELECT COUNT(*)
   FROM users_rights ur JOIN roles r ON r.id = ur.role_id
   WHERE r.merchant_id <> ur.merchant_id;
   ```
   Un résultat non nul sur la seconde requête est une fuite de droits
   inter-établissements — **ne pas continuer**, traiter en urgence avant
   toute autre étape (voir `docs/RBAC_BASCULE.md` §5 pour le précédent exact
   de cette vérification, déjà exécutée une fois en staging).

6. **Purge Redis (optionnelle, coupure nette)** : sans elle, les sessions déjà
   en cache continuent sur les colonnes booléennes jusqu'à expiration du TTL
   (`models.UserCacheTTL`, 60 minutes au plus) — sans conséquence utilisateur
   (les deux régimes accordent la même chose au moment de la bascule, voir
   l'étape 1), juste deux mondes qui cohabitent en vol pendant la fenêtre.

   ```
   redis-cli --scan --pattern 'user:token:v2:*' | xargs redis-cli DEL
   ```
   (cibler exactement ce préfixe — jamais `FLUSHALL`).

### 3.2 Migration 110 — quand exactement

`110_drop_dead_legacy_rights_columns.up.sql` (retire `access_wrdelivery`,
`access_wrwaiter`, `export_reports`, `export_financials`,
`export_customers` de `users_rights`) est la seule migration **destructive**
de ce rollout sur une table que le code de production lit/écrit
**aujourd'hui, avant même ce déploiement** (ces 5 colonnes existent depuis
avant ce chantier). Elle ne part **qu'après** l'étape 3.1.1 (le nouveau code,
qui ne lit plus ces 5 champs — voir `docs/decisions.md`, nettoyage du
2026-09-01) confirmée déployée et en bonne santé. Ne jamais la faire partir
avant, ni en même temps que le déploiement de code.

```sql
-- après le déploiement de code de l'étape 3.1.1, avant ou après le reste de la vague B
```
Exécuter `110_drop_dead_legacy_rights_columns.up.sql`, instruction par
instruction comme le reste.

**Vérification** :
```sql
SELECT column_name FROM information_schema.columns
WHERE table_name = 'users_rights'
  AND column_name IN ('access_wrdelivery', 'access_wrwaiter', 'export_reports', 'export_financials', 'export_customers');
-- doit renvoyer 0 ligne
```

---

## 4. Le remplissage des rôles — quelle règle pour les liens existants

**Oui, chaque lien `users_rights` doit recevoir un `role_id`** — c'est
`cmd/assign_admin_role --apply` (étape 3.1.4) qui le fait, et la vérification
de l'étape 3.1.5 (`role_id IS NULL` doit renvoyer 0 pour les comptes actifs)
le confirme.

**La règle exacte, reprise telle quelle de la décision produit actée pour ce
rollout (`docs/RBAC_BASCULE.md`, non révisée par ce chantier)** : chaque
compte existant reçoit le rôle **Administrateur** de son propre
établissement (jamais un autre rôle, jamais l'établissement d'un autre —
c'est ce que vérifie la requête cross-tenant de l'étape 3.1.5). Pas
d'inventaire préalable des droits actuels par compte, pas de rôle
intermédiaire : la justification produit reste celle de lot 4 — les droits
par rôle ne sont pas encore exploités depuis une interface de sélection fine
en production, donc préserver le comportement actuel (tout le monde a accès
à tout, comme avec `Rights.Admin` historique) est le seul choix qui ne
change l'expérience de personne le jour du rollout. Affiner qui a quel rôle
ensuite est un travail produit séparé, postérieur, via l'écran de gestion des
rôles du back-office.

**Un lien resté sans `role_id`** (celui-ci ne devrait pas exister après
l'étape 3.1.4/3.1.5, sauf l'anomalie déjà connue en staging — un
`merchant_id` orphelin sans ligne `merchant` correspondante, voir
`docs/RBAC_BASCULE.md` §5) bascule sur le repli historique de `Has()`
(`docs/RBAC_REPLI_HISTORIQUE.md`) — transitoirement inoffensif (comportement
identique à avant ce rollout pour ce compte précis), mais doit être un choix
explicite et tracé, pas un oubli. La requête qui les compte :

```sql
SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL;
```

---

## 5. Vague C — plus tard, pas dans cette fenêtre

Deux choses restent délibérément hors de ce rollout, chacune bloquée sur une
condition qui ne peut pas être remplie le jour du déploiement lui-même :

1. **Retirer le repli historique** (`legacyPermissionFallback`, la branche
   `RoleID == nil` de `Has()`, `users_rights.admin` cessant d'être lu) —
   condition exacte documentée dans `docs/RBAC_REPLI_HISTORIQUE.md` :
   `SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL`
   doit renvoyer 0 **en production**, pas seulement en staging, et rester à 0
   assez longtemps pour couvrir tout compte réactivé entretemps.
2. **Appliquer `113_drop_users_rights_admin_column.up.sql`** (préparée, non
   appliquée — voir `docs/decisions.md`, phase 4) : seulement après le point
   1 ci-dessus **et** un déploiement de code qui ne lit plus jamais
   `Rights.Admin` nulle part (`HasAdminRole()`, `HasAccessReception()`,
   `CanPrintCashReport()`, `LoginLegacyFields.Admin` — recensement complet
   dans `docs/RBAC_REPLI_HISTORIQUE.md`).

Ces deux étapes ne sont pas planifiées par ce runbook — elles nécessitent
leur propre passage en revue le moment venu, avec leur propre vérification de
l'invariant "0 lien sans role_id" en production.

---

## 6. Points de non-retour et retour arrière

| Étape | Réversible ? | Procédure |
|---|---|---|
| Vague A (094-100, 103) | Oui | `.down.sql` de chaque migration, dans l'ordre inverse (103 → 094), instruction par instruction. Aucune donnée métier existante touchée (tables neuves, ou catalogue pas encore consommé). |
| Déploiement du nouveau code (3.1.1) | Oui | Rollback Render vers le déploiement précédent. Sans effet sur les données (aucune migration destructive n'a encore tourné à ce stade). |
| `cmd/seed_system_roles` (3.1.2) | Oui, mais inutile de le faire | Additif seulement (crée des rôles, ajoute des `role_permissions`). Pour annuler complètement le rollout, voir la ligne `assign_admin_role` ci-dessous — remettre `role_id` à NULL rend les rôles créés ici inertes sans avoir besoin de les supprimer. |
| `cmd/assign_admin_role --apply` (3.1.4) | **Oui — c'est le vrai point de bascule comportementale, mais réversible en une instruction** | `UPDATE users_rights SET role_id = NULL;` — `Has()` retombe intégralement sur les colonnes booléennes historiques, jamais modifiées par ce rollout. Restaure le comportement exact d'avant, quel que soit le nombre de lignes traitées. |
| Purge Redis (3.1.6) | N/A | Pas d'état à restaurer — au pire, des sessions se reconnectent. |
| **`110_drop_dead_legacy_rights_columns` (3.2)** | **Non — point de non-retour réel** | `DROP COLUMN` perd les données de ces 5 colonnes. Le `.down.sql` recrée les colonnes avec leur valeur par défaut, **pas** les données d'origine. Ne pas exécuter avant d'être certain que le nouveau code (déjà confirmé sans lecteur de ces 5 champs — nettoyage du 2026-09-01) est bien celui qui restera déployé. |
| `113_drop_users_rights_admin_column` (vague C, futur) | **Non — même nature que 110** | Même caveat : `.down.sql` restaure la colonne à `false` pour tout le monde, pas les valeurs d'origine. À n'exécuter qu'après la vérification de la vague C ci-dessus — c'est précisément pour ça qu'elle est préparée mais pas jouée par ce runbook. |

---

## 7. Créneau recommandé

**3h–5h**, le creux d'activité mesuré. La vague A (schéma pur, no-op pour le
code actuel) n'a en réalité besoin d'aucun créneau spécial — elle peut partir
en journée sans risque. C'est la vague B (déploiement de code + bascule
`assign_admin_role` + migration 110) qui doit tenir dans ce créneau, pour
avoir de la marge d'observation avant le retour de l'activité si un rollback
s'impose.

---

## 8. Ce qui doit être vérifié avant de passer à l'étape suivante

Check-list à réponse oui/non, dans l'ordre du runbook :

- [ ] §1.1 : `roles` n'existe pas encore (ou : l'état constaté correspond à
      un point précis de ce runbook, identifié avant de continuer) ?
- [ ] Vague A exécutée : `SELECT COUNT(*) FROM permissions` renvoie
      exactement **18** ?
- [ ] 3.1.1 : le déploiement Render est vert et `GET /health` répond `200` ?
- [ ] 3.1.2 : `cmd/seed_system_roles` a affiché une ligne par établissement
      actif connu, sans erreur ?
- [ ] 3.1.3 : la requête (ou le test) d'invariant renvoie **0** rôle admin
      incomplet ?
- [ ] 3.1.4 : le dry-run de `assign_admin_role` liste **0** établissement
      « sans rôle admin » ?
- [ ] 3.1.4 : le total de l'`--apply` correspond exactement à celui du
      dry-run ?
- [ ] 3.1.5 : `role_id IS NULL AND enabled = TRUE` renvoie **0** (ou une
      anomalie déjà identifiée et tracée, jamais un chiffre inattendu) ?
- [ ] 3.1.5 : la requête cross-tenant renvoie **0** ?
- [ ] 3.2 : avant de jouer 110, le nouveau code (3.1.1) est confirmé stable
      depuis un temps d'observation suffisant (pas de rollback en cours) ?
- [ ] 3.2 : après 110, les 5 colonnes ont bien disparu de
      `information_schema.columns` ?

Si une seule ligne répond « non » ou « inattendu », **s'arrêter** et ne pas
passer à l'étape suivante avant d'avoir compris pourquoi — même règle que
`docs/RBAC_BASCULE.md`.
