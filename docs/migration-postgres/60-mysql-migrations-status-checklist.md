# 60 — Checklist consolidée des migrations MySQL réelles (chantier migration Postgres)

Consolidation demandée : toutes les migrations MySQL réelles produites pendant le chantier
`docs/migration-postgres/`, avec leur statut d'application en base MySQL réelle (production/staging
Hostinger — **à ne pas confondre** avec l'instance Postgres de staging Render utilisée pour la
répétition de la bascule, voir § Note de méthode). Aucune donnée réelle citée ci-dessous.

## Note de méthode — deux notions de « base de données » à ne pas confondre

Ce chantier manipule deux bases distinctes, et les rapports sources parlent parfois de l'une en
citant l'autre :

1. **MySQL réel (Hostinger, production/staging)** — la base qui alimente aujourd'hui l'API en
   production. C'est elle que ce document qualifie de « MySQL prod » et pour laquelle une migration
   `.up.sql` doit être exécutée manuellement (`phpMyAdmin`, pas d'outil de migration — cf. `CLAUDE.md`).
2. **Postgres de staging Render / Docker de dev** — l'instance cible de la future bascule, utilisée
   pour rejouer `04-schema-postgres-target.sql` et valider les tests d'intégration
   (`postgres_integration_test.go`). Le [rapport 58](58-render-staging-schema-sync.md) confirme par
   exemple que `audit_logs.id` est déjà `varchar(64)` **sur Render Postgres** — ceci ne dit rien sur
   l'état de la colonne équivalente côté MySQL réel, qui reste une base entièrement différente.

**Convention du dépôt** : un fichier de migration MySQL vit dans `migrations/` tant qu'il n'a pas été
exécuté contre la base réelle, puis est déplacé (simple `git mv`, contenu inchangé) vers
`migrations/done/` une fois l'exécution confirmée — convention introduite au commit `9b18801`
(« executed migrations + fix cors ») et reconfirmée au [rapport 56 §1.1](56-haccp-traceability-integration.md#11-recherche-du-prochain-numéro-réellement-libre).
Ce document s'appuie sur cette convention (emplacement du fichier) **et** sur le texte explicite de
chaque rapport pour déterminer le statut de chaque ligne.

## 1. Migrations produites par le chantier (065 → 071)

| # | Fichier | Contenu | Type | Rapport(s) | Confirmation explicite d'application en prod ? | Statut |
|---|---|---|---|---|---|---|
| 065 | [`planning_day_comments.up.sql`](../../migrations/065_planning_day_comments.up.sql) | `CREATE TABLE planning_day_comments` | **Schéma MySQL réel** (CREATE TABLE) | [26](26-planning-day-comments-integration.md) | Non — le rapport 26 ne traite que la traduction Postgres, aucune mention d'exécution MySQL. Fichier toujours dans `migrations/` (pas `migrations/done/`). | 🟡 À VÉRIFIER |
| 066 | [`widen_varchar_columns.up.sql`](../../migrations/066_widen_varchar_columns.up.sql) | `ALTER TABLE users.token`, `customer_loyalty_progress.id`, `customer_loyalty_progress_order.progress_id` → `varchar(64)` | **Schéma MySQL réel** (ALTER TABLE) | [28](28-varchar-widening.md) | **Non** — le rapport 28 indique explicitement : *« Changement de schéma MySQL réel en production, distinct de la bascule Postgres à venue. À appliquer et tester séparément (staging d'abord). **Non exécutée par ce chantier.** »* Fichier toujours dans `migrations/`. | 🟡 À VÉRIFIER |
| 069 | [`widen_audit_logs_id.up.sql`](../../migrations/069_widen_audit_logs_id.up.sql) | `ALTER TABLE audit_logs MODIFY id varchar(64)` | **Schéma MySQL réel** (ALTER TABLE) | [53](53-audit-logs-column-width.md) | **Non** — même formule explicite : *« Non exécutée par ce chantier. »* (Renumérotée de 067→069 par le [rapport 56](56-haccp-traceability-integration.md) suite à une collision avec `migrations/done/067_haccp_traceability` — voir § 3.) Fichier toujours dans `migrations/`. | 🟡 À VÉRIFIER |
| 070 | [`widen_loyalty_program_id_columns.up.sql`](../../migrations/070_widen_loyalty_program_id_columns.up.sql) | `ALTER TABLE customer_loyalty_progress`/`_order`/`customer_rewards` `.loyalty_program_id` → `varchar(64)` | **Schéma MySQL réel** (ALTER TABLE) | [55](55-generated-id-column-widths-full-audit.md) | **Non** — même formule explicite : *« Non exécutée par ce chantier. »* (Renumérotée de 068→070 par le [rapport 56](56-haccp-traceability-integration.md).) Fichier toujours dans `migrations/`. | 🟡 À VÉRIFIER |
| 071 | [`users_enabled_boolean.up.sql`](../../migrations/071_users_enabled_boolean.up.sql) | `ALTER TABLE users MODIFY enabled tinyint(1)` | **Schéma MySQL réel** (ALTER TABLE) | [59](59-users-enabled-boolean-conversion.md) | **Non** — le rapport 59 est explicite : *« Fichiers créés dans le repo, non exécutés (aucune connexion à la base MySQL réelle n'a été faite). »* et liste l'exécution comme *« la seule étape non exécutée de ce ticket »*. Fichier **non commité** (`??` en `git status`). | 🟡 À VÉRIFIER |

**Aucune des 5 migrations 065–071 n'a de confirmation explicite d'application en MySQL réel dans les
rapports** — contrairement à ce que la prémisse de la demande supposait pour 28/071 : en relisant les
deux rapports mot à mot, **ni le rapport 28 (066) ni le rapport 59 (071) ne confirment une exécution
réelle** ; les deux disent l'inverse (« non exécutée », « reste à exécuter »). C'est cohérent avec la
convention `migrations/` vs `migrations/done/` : les 5 fichiers sont tous encore dans `migrations/`.

## 2. Catégories demandées absentes de la liste — pourquoi

| Catégorie demandée | Constat |
|---|---|
| **`order_id`** | Converti uniquement côté **schéma cible Postgres** ([rapport 18](18-order-id-schema-update.md)) : 6 colonnes `varchar → integer` dans `04-schema-postgres-target.sql`. Le rapport le dit explicitement : *« Seul le schéma Postgres cible est modifié. Aucun fichier `.go` touché. »* **Aucune migration MySQL réelle produite** — ces colonnes restent `varchar` côté MySQL source, inchangées. Non applicable à ce document. |
| **`merchant_id`** | Même situation ([rapport 13](13-merchant-id-schema-update.md)) : 72 colonnes alignées en `varchar(64)` **uniquement dans `04-schema-postgres-target.sql`**. Rapport explicite : *« Aucun fichier `.go` touché, aucune migration MySQL modifiée. »* Non applicable. |
| **`haccp_traceability`** | A bien nécessité une vraie migration MySQL — mais **produite avant ce chantier documentaire** et **déjà exécutée** : voir § 3 (`migrations/done/067_haccp_traceability.up.sql`). Pas un livrable 065–071, une découverte a posteriori du [rapport 55](55-generated-id-column-widths-full-audit.md)/[56](56-haccp-traceability-integration.md). |

## 3. Migrations réelles découvertes déjà appliquées (`migrations/done/`)

Deux migrations, antérieures à la plage 065–071, ont été identifiées pendant l'audit comme
**déjà exécutées en MySQL réel** — présentes dans `migrations/done/`, jamais dans `migrations/` :

| # | Fichier | Contenu | Rapport(s) qui la documente | Statut |
|---|---|---|---|---|
| 067 (done) | [`migrations/done/067_haccp_traceability.up.sql`](../../migrations/done/067_haccp_traceability.up.sql) | `CREATE TABLE haccp_traceability_records`, `haccp_traceability_photos` | [55](55-generated-id-column-widths-full-audit.md), [56](56-haccp-traceability-integration.md) — citée à répétition comme *« déjà exécutée »* | 🟢 CONFIRMÉ APPLIQUÉ |
| 041 (done) | [`migrations/done/041_cart_discounts.up.sql`](../../migrations/done/041_cart_discounts.up.sql) | `CREATE TABLE discount_redemptions` + `ALTER TABLE discounts`/`orders` (colonnes `discount_scope`, `max_redemptions*`, `cart_discount_*`) | [56 § Partie 3](56-haccp-traceability-integration.md#partie-3--vérification-élargie--dautres-tables-manquantes-), [57](57-discount-redemptions-schema.md) | 🟢 CONFIRMÉ APPLIQUÉ |

Le fondement de ce « CONFIRMÉ » n'est pas une phrase isolée mais la convention structurelle du dépôt :
un fichier dans `migrations/done/` signifie qu'il a été exécuté contre la base réelle (c'est le sens
même du dossier, introduit pour cette distinction). Les rapports 55–57 s'appuient sur ce fait sans le
remettre en question.

**Nuance importante, sans impact sur le statut d'application** : ces deux migrations sont
« schéma-only » à des degrés différents —
- `haccp_traceability_*` est **vivante** : le code Go (`internal/modules/haccp/repository.go`) lit et
  écrit déjà ces deux tables.
- `discount_redemptions` (+ colonnes `discount_scope`/`max_redemptions*`/`cart_discount_*`) est
  **schéma prêt, non câblé côté Go** : aucun repository ne lit ni n'écrit ces colonnes à ce jour
  (confirmé par grep exhaustif au rapport 57 § 3). La migration MySQL est bien appliquée ; c'est
  seulement la fonctionnalité applicative qui n'existe pas encore.

## 4. Migrations réelles hors périmètre documentaire (062–064)

Trois migrations supplémentaires, réelles et toujours **en attente** dans `migrations/` (jamais
déplacées vers `done/`), n'ont pas de rapport dédié dans `docs/migration-postgres/` — elles relèvent
du chantier « plan de salle » (floor plan), pas de la conversion Postgres :

| # | Fichier | Contenu | Type | Statut |
|---|---|---|---|---|
| 062 | [`location_varchar_ids.sql`](../../migrations/062_location_varchar_ids.sql) | `floors`/`locations`/`floor_areas`/`order_location`/`booked_location`/`qrcodes`/`kiosks` : PK/FK `INT → VARCHAR(64)` | **Schéma MySQL réel** (ALTER TABLE, multi-tables) | 🟡 À VÉRIFIER — aucun rapport `migration-postgres` ne le documente ; fichier en attente dans `migrations/` |
| 063 | [`floor_obstacles.up.sql`](../../migrations/063_floor_obstacles.up.sql) | `CREATE TABLE floor_obstacles` | **Schéma MySQL réel** (CREATE TABLE) | 🟡 À VÉRIFIER — idem |
| 064 | [`locations_attributes.up.sql`](../../migrations/064_locations_attributes.up.sql) | `ALTER TABLE locations ADD COLUMN attributes JSON` | **Schéma MySQL réel** (ALTER TABLE) | 🟡 À VÉRIFIER — idem |

**Collision de numérotation préexistante, non corrigée (documentée au [rapport 56 § 1.3](56-haccp-traceability-integration.md#13-cross-références-corrigées))** :
`migrations/062_location_varchar_ids.sql` (en attente) et `migrations/done/062_kiosks_device_id.up.sql`
(déjà exécutée) partagent le numéro 062. Sans conséquence d'exécution à ce jour (dossiers différents,
jamais en conflit réel), mais à renuméroter avant la nuit de bascule au même titre que l'ex-collision
067 déjà traitée.

## 5. Changements purement applicatifs Go (aucune migration MySQL associée)

Pour mémoire — ne nécessitent **aucune** action `phpMyAdmin`, déjà dans le code (committé ou en
attente de commit selon le rapport) :

| Rapport | Changement Go | Dépend d'une migration MySQL non encore appliquée ? |
|---|---|---|
| [28](28-varchar-widening.md) | Retrait de `truncateVarchar` (`users` , `customers` repositories), réduction de `generateToken()` 64→32 octets | Oui, partiellement — le retrait de la troncature suppose que la colonne MySQL réelle soit déjà élargie (migration 066). Tant que 066 n'est pas appliquée, ce code retiré s'appuyait sur une marge que MySQL non-strict masquait silencieusement (le risque documenté au rapport 28 § 2 redevient actif). |
| [59](59-users-enabled-boolean-conversion.md) | `admin_repository.go` (filtre bool direct), `planning/employees/repository.go` (`= TRUE` au lieu de `= 1`), 3 fixtures de test | D'après le rapport lui-même (§ 5.4) : **non** — MySQL accepte un `bool` Go lié à une colonne `int(11)` ou `tinyint(1)` de façon identique ; ce changement fonctionne indépendamment de l'exécution de la migration 071. |

## 6. Résumé exécutif

| Statut | Migrations |
|---|---|
| 🟢 **CONFIRMÉ APPLIQUÉ** (MySQL réel) | `done/041_cart_discounts` (discount_redemptions), `done/067_haccp_traceability` |
| 🟡 **À VÉRIFIER** (fichier écrit, aucune confirmation d'exécution MySQL réelle trouvée) | 062, 063, 064, 065, 066, 069, 070, 071 |
| ⚪ **NON APPLICABLE** (pur schéma cible Postgres ou pur code Go, aucune migration MySQL requise) | `merchant_id` (rapport 13), `order_id` (rapport 18), retrait `truncateVarchar` (rapport 28, sous réserve de 066), filtre bool `users.enabled` (rapport 59) |

**Action requise avant bascule** : les 8 migrations 🟡 doivent être exécutées manuellement via
`phpMyAdmin` sur la base MySQL réelle (staging d'abord, hors pic de trafic — chaque fichier porte son
propre avertissement en tête), puis déplacées de `migrations/` vers `migrations/done/` pour refléter
leur exécution, dans cet ordre de dépendance : 062 → 063 → 064 → 065 → 066 → 069 → 070 → 071 (ordre
numérique = ordre de production, aucune dépendance croisée identifiée entre elles au-delà de ce tri).
