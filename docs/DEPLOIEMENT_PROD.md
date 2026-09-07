# Déploiement en production — 087 → 123

Runbook unifié. Remplace `docs/RBAC_DEPLOIEMENT_PROD.md` (conservé comme
pointeur historique, voir en tête de ce fichier) : ce document couvre la
totalité de `migrations/todo/` de 087 à 123, plus les trois scripts qui font
la vraie bascule (`cmd/seed_system_roles`, `cmd/assign_admin_role`,
`cmd/backfill_customer_stats`). Un seul document, pas deux qui divergeront
dans trois semaines.

**Principe directeur, non négociable : expansion, déploiement, contraction.**
Les migrations additives passent d'abord, compatibles avec le code
actuellement déployé (`main`, dernier commit `cd388ed`, 2026-08-26). Le
nouveau code ensuite. Les suppressions — de colonnes ou de comportement — en
dernier, seulement quand plus aucun code déployé ne les lit. Une migration
destructive et un déploiement de code ne partent jamais dans le même geste.

Sources : `docs/migration-postgres/67-migration-status-audit.md` (état
staging au 2026-09-03, revérifié par cette session le 2026-09-07),
`docs/decisions.md`, `docs/RBAC_BASCULE.md`, `docs/RBAC_REPLI_HISTORIQUE.md`.

---

## 0. Étape bloquante — diagnostic production

**Rien ne commence avant cette étape.** Aucune migration, aucun déploiement,
aucune commande d'écriture tant que son résultat n'a pas été lu et compris.

### 0.1 Le script existant couvre 087 et 094-117

```
POSTGRES_URL="postgres://...production..." go run ./cmd/diagnose_migrations
```

Lecture seule garantie (une transaction `BEGIN ... READ ONLY`, toujours
annulée). Ne modifie rien, ne fatal jamais sur ce qu'il trouve.

**Sur le résultat de l'audit du 2026-09-03/2026-09-07 (`67-migration-status-audit.md`
§1), ce script trouve 14 migrations "INDÉTERMINABLE" en production — pas
treize.** Correction volontaire d'un chiffre cité par un brief antérieur :
l'audit lui-même liste `087, 101, 102, 103b, 104, 105, 106, 107, 108, 109,
111, 114, 115, 116` (14 entrées), et 115 (`reports.staff_performance.read`)
a été ajoutée au catalogue le jour même où le premier chiffre a été avancé
(2026-09-03) — c'est la ligne la plus probable pour expliquer l'écart de un.
Ne pas répéter "treize" sans revérifier : relancer le script est ce qui
tranche, pas ce document.

### 0.2 Ce que le script ne couvre PAS : 118 à 123

`cmd/diagnose_migrations` a été écrit pour le périmètre "087, 094-117" et
n'a jamais été étendu. Postérieur à sa dernière mise à jour : 118 (clé
entière des remises), 119 (reprise historique), 120 (préparée, non jouée),
121/122 (compteurs client), 123 (`schema_migrations`, ce lot). Vérifier ces
six-là à la main avant de continuer — chacune a un seul signal net,
sans ambiguïté :

```sql
-- 118 : discounts.discount_id_new existe ?
SELECT column_name FROM information_schema.columns WHERE table_name='discounts' AND column_name='discount_id_new';
-- 119 : des lignes reconstruites existent-elles déjà ?
SELECT count(*) FROM discount_redemptions WHERE is_reconstructed = TRUE;
-- 120 (attendu : toujours présentes — préparée, jamais jouée par choix)
SELECT column_name FROM information_schema.columns WHERE table_name='orders' AND column_name IN ('cart_discount_id','cart_discount_code');
-- 121 : orders.customer_stats_counted_at existe ?
SELECT column_name FROM information_schema.columns WHERE table_name='orders' AND column_name='customer_stats_counted_at';
-- 122 : table de traçabilité de la réconciliation existe ?
SELECT to_regclass('public.customer_stats_reconciliation_runs');
-- 123 : schema_migrations existe déjà (déploiement précédent partiel) ?
SELECT to_regclass('public.schema_migrations');
```

### 0.3 Les deux cas, pour chaque migration

Pour chacune des ~36 migrations du périmètre (087 à 123), deux issues
possibles à ce diagnostic, et une seule conduite à tenir pour chacune :

- **Déjà appliquée** (schéma présent, valeurs cohérentes) : ne pas la
  rejouer. L'enregistrer directement dans `schema_migrations` une fois la
  migration 123 posée (§2), avec une note `"déjà appliquée avant ce
  runbook, constatée par cmd/diagnose_migrations le <date>"`. Continuer au
  numéro suivant.
- **Non appliquée** : suivre l'entrée correspondante dans les tableaux de
  vagues ci-dessous (§3 à §7) — commande exacte, durée attendue,
  vérification, retour arrière.

Ne jamais supposer un troisième cas ("probablement appliquée") : si le
signal est ambigu (table absente mais impossible de savoir si c'est parce
que la migration n'est jamais partie ou parce qu'elle a été interrompue en
plein milieu), traiter comme "non appliquée" et rejouer — toutes les
migrations de ce périmètre sont idempotentes (`IF NOT EXISTS`, `ON CONFLICT
DO NOTHING`) précisément pour rendre cette règle sûre.

---

## 1. Vue d'ensemble des vagues

| Vague | Contenu | Peut partir |
|---|---|---|
| **A — expansion** | 087 ; RBAC 094-100, 103a, 115 ; 101, 102, 103b (réécrites) ; 107, 108, 109 (réécrite) ; 114, 116 ; 118, 119, 121, 122 ; 123 | N'importe quand avant B, sans créneau spécial (schéma pur, invisible du code actuellement déployé) — **sauf 111, voir §3.9, à part** |
| **A' — 111, à part** | 111 (réécrite) | Immédiatement avant le déploiement de code de la vague B, **pas avant** — voir §3.9, découverte de cette session |
| **B — déploiement** | Nouveau code (`main` ← `staging`) ; `cmd/seed_system_roles` ; requête nominative (§4.4) ; `cmd/assign_admin_role` | Créneau 3h-5h |
| **C — rattrapage** | `cmd/backfill_customer_stats` (dry-run puis apply) ; 106 si le volume le permet | Après vérification que B tient, même créneau |
| **D — contraction** | 104, 110, 120 | Quand plus aucun code déployé ne lit ce qui disparaît — jours/semaines après B, pas le jour J |
| **Hors séquence** | 112 (extension, geste tableau de bord Render, hors SQL) ; 113 (attend le retrait du repli historique, voir `docs/RBAC_REPLI_HISTORIQUE.md`) ; 117 (décision produit différée, indépendante) | Non planifiées ici |

Déviation assumée par rapport à une proposition antérieure : **111 sort de
la vague A** pour devenir son propre mini-lot, collé à la vague B — la
raison exacte est au §3.9, ce n'est pas une prudence gratuite, c'est une
incompatibilité de code réellement constatée.

---

## 2. `schema_migrations` — à poser en tout premier dans la vague A

```
-- migrations/todo/123_schema_migrations.up.sql
```

Additive, table neuve, aucun risque. Une fois posée, enregistrer
immédiatement le résultat de l'étape 0 (toutes les migrations "déjà
appliquées" constatées) avant de jouer quoi que ce soit d'autre — c'est ce
qui rend le reste de cette procédure vérifiable après coup.

**Ne pas copier le fichier `migrations/staging_only_schema_migrations_backfill.sql`
vers production** — il documente l'état de staging au 2026-09-07, qui n'a
aucune raison de correspondre à celui de production. Écrire les lignes de
production depuis le résultat de l'étape 0.

**Durée** : instantané. **Vérification** : `SELECT count(*) FROM
schema_migrations;` renvoie un nombre cohérent avec ce que l'étape 0 a
trouvé "déjà appliqué". **Retour arrière** : `123_schema_migrations.down.sql`
(`DROP TABLE`), sans risque tant qu'aucun code ne la lit encore (le contrôle
au démarrage, `internal/database/schemamigrations.go`, ne fait qu'avertir,
jamais bloquer).

---

## 3. Vague A — détail migration par migration

Instruction par instruction, jamais de transaction englobante autour d'un
fichier entier contenant du `CONCURRENTLY` (087, 109, 111, et la section 3
de 118 — voir l'avertissement en tête de chacun de ces fichiers). Le reste
peut être joué fichier par fichier normalement.

| # | Fichier | Durée attendue | Vérification | Retour arrière |
|---|---|---|---|---|
| 1 | `087_analytics_indexes.up.sql` | Secondes sur staging (33k/77k lignes) ; **mesurer `pg_stat_user_tables` sur `orders`/`orderitems`/`payments` en production avant de jouer** — l'ordre de grandeur réel n'est pas connu d'ici. `CONCURRENTLY` = pas de blocage des écritures pendant la construction, mais peut prendre plusieurs minutes sur un volume 10-50× staging. | `SELECT indexname, indisvalid FROM pg_indexes JOIN pg_index ON ... ` — 4 index présents, tous `indisvalid = true` (requête complète dans le fichier) | `087_analytics_indexes.down.sql`, un `DROP INDEX CONCURRENTLY` à la fois |
| 2 | `094_roles_schema.up.sql` | < 1 min (DDL pur) | `SELECT to_regclass('public.roles');` non NULL | `.down.sql` |
| 3 | `095_roles_permissions_catalog.up.sql` | < 1 min | `SELECT count(*) FROM permissions;` = 14 à ce stade | `.down.sql` |
| 4 | `096_seed_system_roles.up.sql` | Instantané (no-op SQL, réservation de numéro) | — | `.down.sql` |
| 5 | `097_permission_pos_status_manage.up.sql` | < 1 min | count(*) = 15 | `.down.sql` |
| 6 | `098_access_observation.up.sql` | < 1 min | `SELECT to_regclass('public.access_observation');` | `.down.sql` |
| 7 | `099_merchant_default_role_admin.up.sql` | < 1 min (no-op tant que `roles` est vide) | — | `.down.sql` |
| 8 | `100_deprecate_pos_access_and_discount_apply.up.sql` | < 1 min | count(*) = 13 | `.down.sql` |
| 9 | `103_permission_catalog_lot10.up.sql` ("103a") | < 1 min | count(*) = 18, clés listées en §4.1 de `RBAC_DEPLOIEMENT_PROD.md` (archivé) | `.down.sql` |
| 10 | `115_permission_reports_staff_performance_read.up.sql` | < 1 min | count(*) = **19** — ajustement par rapport au chiffre "18" de l'ancien runbook RBAC, qui datait d'avant cette migration | `.down.sql` |
| 11 | `101_production_profiles.up.sql` (réécrite Phase 1) | < 1 min (2 tables neuves) | `production_profiles` et `product_production_profiles` existent, types Postgres | `.down.sql` |
| 12 | `102_delivery_travel_seconds.up.sql` (réécrite Phase 1) | < 1 min | `orders.delivery_travel_seconds` + `average_delivery_time` existent | `.down.sql` |
| 13 | `103_production_ready_delivery_arrival.up.sql` ("103b", réécrite Phase 1) | < 1 min (ADD COLUMN nullable = métadonnées pures) | `orders.production_ready_at`/`delivery_arrival_at` présentes, `datecall` absente | `.down.sql` (réintroduit `datecall` avec une valeur par défaut, pas les données d'origine — sans importance, cf. le fichier lui-même : colonne confirmée sans usage propre) |
| 14 | `107_import_component_mappings.up.sql` | < 1 min | 2 tables présentes | `.down.sql` |
| 15 | `108_api_request_logs_response_payload.up.sql` | < 1 min | Colonne présente | `.down.sql` |
| 16 | `109_api_request_logs_created_at_index.up.sql` (réécrite Phase 1, `CONCURRENTLY`) | **Mesurer la taille réelle d'`api_request_logs` en production avant de jouer** — 5 313 lignes sur staging au 2026-09-07 (la purge mensuelle a tourné depuis les 207k citées par le fichier), aucune garantie sur le volume réel de production, qui n'a jamais eu cet index pour purger efficacement. | Index présent, `indisvalid = true` | `.down.sql` (`DROP INDEX CONCURRENTLY`) |
| 17 | `114_write_path_instrumentation.up.sql` | Dépend du volume réel d'`orders` — instantané sur les 34k lignes de staging, à mesurer en production (plusieurs `UPDATE ... WHERE ... IS NULL` qui scannent `orders`) | Colonnes présentes ; `SELECT count(*) FILTER (WHERE brand_status <> upper(brand_status)) FROM orders;` = 0 | `.down.sql` |
| 18 | `116_write_path_instrumentation_lot2.up.sql` | < 1 min (pas de rétro-remplissage de coût, décision explicite du fichier) | Colonnes présentes ; largeur `deletion_reason_id` = 32 | `.down.sql` |
| 19 | `118_discounts_integer_key_expansion.up.sql` (réécrite Phase 1 pour l'index `orderitems`) | Rapide pour les tables `discounts*` (19 à 545 lignes sur staging) ; **la section `CREATE UNIQUE INDEX CONCURRENTLY ... ON orderitems` dépend du volume réel d'`orderitems` en production** (77k sur staging) — jouer cette instruction et la suivante (`ADD CONSTRAINT ... USING INDEX`) hors transaction, avant le reste du fichier. **Pré-requis à vérifier avant de jouer** : `SELECT count(*) FROM orderitems oi WHERE oi.discount_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM discounts d WHERE d.discount_id::text = oi.discount_id::text);` — un nombre inattendu de lignes orphelines au-delà des 17 déjà connues (documentées dans le fichier) mérite d'être compris avant de poursuivre. | `discounts.discount_id_new` NOT NULL ; `uq_orderitems_order_item_id` présent et `indisvalid = true` ; `discount_redemptions.scope` NOT NULL | `.down.sql` (échoue volontairement, via un bloc `DO $$ RAISE EXCEPTION`, si des lignes `PRODUCT_LINE` réelles ont été écrites depuis — lire le fichier avant de l'utiliser) |
| 20 | `119_discount_redemptions_historical_backfill.up.sql` | Dépend du volume d'`orderitems` avec remise (545 lignes reconstruites sur staging, instantané) | `SELECT count(*) FROM discount_redemptions WHERE is_reconstructed = TRUE;` cohérent avec le volume mesuré | Pas de `.down.sql` dédié — ces lignes sont retirées par le `.down.sql` de 118 (`DROP COLUMN scope` etc. les invalide) |
| 21 | `121_customer_stats_counted_marker.up.sql` | < 1 min | Colonne présente | `.down.sql` |
| 22 | `122_customer_stats_reconciliation_runs.up.sql` | < 1 min | Table présente | `.down.sql` |

**Vérification globale de fin de vague A** (hors 111) : santé de l'API
inchangée (`GET /health` = `200`), aucune erreur nouvelle dans les logs
applicatifs — cette vague ne doit produire aucun symptôme utilisateur,
c'est la propriété qui la définit.

---

## 3.9 — 111, pourquoi elle sort de la vague A

**Trouvé en vérifiant, pas en supposant** (exactement l'exigence de ce
runbook) : le code actuellement déployé sur `main`
(`internal/modules/ubereats/repository.go`, fonction d'upsert des
identifiants OAuth) écrit aujourd'hui :

```sql
INSERT INTO integration_uber_eats (...) VALUES (...)
ON CONFLICT (merchant_id) DO UPDATE SET ...
```

Le code déjà réécrit pour cibler la nouvelle clé composite (commit
`145bbbb0`, `ON CONFLICT (merchant_id, store_id) DO UPDATE SET ...`, avec un
commentaire explicite "la PK composite posée par la migration 111") existe
sur `staging` mais **n'est pas encore sur `main`** (vérifié :
`git merge-base --is-ancestor 145bbbb0 main` répond non).

Conséquence directe : si 111 est jouée en production **avant** le
déploiement du nouveau code, la clause `ON CONFLICT (merchant_id)` du code
actuellement en ligne se met à échouer immédiatement sur tout nouvel
enregistrement/renouvellement de connexion Uber Eats (Postgres refuse un
`ON CONFLICT` qui ne correspond à aucune contrainte unique restante) — une
vraie régression, pas une hypothèse. C'est exactement la propriété que la
vague A est censée garantir ("inoffensive pour le code actuellement en
ligne") et que 111, seule dans tout le périmètre, ne respecte pas.

**Traitement retenu** : jouer `111_multi_account_uber_deliveroo.up.sql`
(réécrite Phase 1, `CONCURRENTLY`) **dans la même fenêtre que le
déploiement de code, immédiatement avant lui** — jamais des heures ou des
jours en avance comme le reste de la vague A. Elle reste additive
(l'esprit "expansion" est respecté, elle précède toujours le code), mais son
créneau se resserre sur celui de la vague B.

**Pré-requis bloquant** (voir aussi l'en-tête du fichier réécrit) :

```sql
SELECT merchant_id, count(*) FROM integration_uber_eats GROUP BY merchant_id HAVING count(*) > 1;
SELECT merchant_id, count(*) FROM integration_deliveroo GROUP BY merchant_id HAVING count(*) > 1;
```

Doit renvoyer 0 ligne pour les deux — confirmé sans doublon sur staging
(6 et 3 lignes respectivement au 2026-09-07 ; **ces tables n'étaient plus
vides à cette date**, contrairement à ce qu'un audit antérieur, du
2026-09-03, avait constaté — l'état bouge, revérifier en production plutôt
que de supposer qu'elles le sont encore).

**Durée** : PK swap instantané (petites tables) ; les `CREATE INDEX
CONCURRENTLY` sur les tables de mapping sont rapides ; celui sur `orders`
(`idx_orders_brand_store_id`) dépend du volume réel de production — même
remarque que 087. **Vérification** : PK composite sur les deux tables
d'intégration ; colonnes `store_id`/`location_id` présentes sur les
mappings ; `orders.brand_store_id` présent. **Retour arrière** :
`111_multi_account_uber_deliveroo.down.sql` — échoue volontairement si un
marchand a déjà créé un second compte au moment du rollback (violation
d'unicité au retour à la PK simple) : n'utiliser qu'en rollback immédiat.

---

## 4. Vague B — déploiement

### 4.1 Ordre exact

1. **Déployer `main` ← `staging`**, au commit qui inclut la migration 111
   déjà jouée dans le même geste (§3.9), et tout le code RBAC (lots 1-11,
   `docs/decisions.md`). Confirmer `GET /health` = `200` avant de continuer.

   Pourquoi sûr même si la vague A entière (RBAC comprise) vient de
   passer : `role_id` reste NULL pour 100% des comptes tant que
   `assign_admin_role --apply` n'a pas tourné — `Has()` retombe
   systématiquement sur les colonnes booléennes historiques, comportement
   identique à avant ce déploiement.

2. **`cmd/seed_system_roles`** :
   ```
   DB_DIALECT=postgres POSTGRES_URL=<url production> \
     GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=x PIN_PEPPER=x \
     go run ./cmd/seed_system_roles
   ```
   Idempotent, transaction par établissement. Durée : quelques secondes par
   établissement. **Vérification** : une ligne de log par établissement
   actif, aucune erreur.

3. **Invariant du catalogue admin**, indépendant de la sortie de l'étape 2 :
   ```sql
   SELECT r.id, r.merchant_id, COUNT(p.key) AS missing
   FROM roles r CROSS JOIN permissions p
   WHERE r.system_key = 'admin' AND p.deprecated_at IS NULL
     AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_key = p.key)
   GROUP BY r.id, r.merchant_id;
   ```
   Doit renvoyer **0 ligne**. Sinon : **s'arrêter**, ne pas passer à l'étape 4.

### 4.2 La requête nominative — avant la bascule, pas après

**Nouveau dans ce runbook** (réclamé depuis le chantier RBAC, jamais
produit avant) : `docs/migration-postgres/69-rbac-nominative-access-loss-query.sql`.
Lecture seule. À lancer maintenant, après l'étape 3, avant l'étape 5
(`assign_admin_role --apply`) :

- **0 ligne, ou uniquement des comptes de test connus** → bascule sereine,
  continuer à l'étape 5.
- **Une ligne sur un compte réel** (gérant, salarié identifiable) →
  **ne pas lancer `--apply`**. Le cas le plus probable : `seed_system_roles`
  n'a pas encore couvert ce `merchant_id`, ou son rôle admin est incomplet
  (revérifier avec la requête de l'étape 3 ci-dessus, filtrée sur ce
  `merchant_id`).

Contexte chiffré (vérifié contre le catalogue réel, pas recopié d'un
chiffre antérieur) : 9 clés du catalogue à 19 n'ont aujourd'hui **aucun**
repli historique — `bookings.manage`, `inventory.manage`, `kiosk.manage`,
`platforms.manage`, `pos.analytics`, `pos.refund`, `pos.ticket.reopen`,
`reports.staff_performance.read`, `seating_plan.manage`. Elles ne sont
accordées aujourd'hui qu'aux liens `users_rights.admin = TRUE` (le
court-circuit de `Has()`) — un compte admin qui, pour une raison quelconque,
n'obtient pas le rôle admin complet à la bascule perd ces 9 clés sans
qu'aucun test actuel ne l'exerce. C'est exactement ce que la requête rend
visible avant, pas après, que ça arrive.

### 4.3 `cmd/assign_admin_role`

```
DB_DIALECT=postgres POSTGRES_URL=<url production> GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=x PIN_PEPPER=x \
  go run ./cmd/assign_admin_role --dry-run
```
Lire la sortie : le total doit correspondre au nombre de comptes connus
(actifs + inactifs) ; "Merchants with NO admin role" doit être **vide**. Si
non vide, s'arrêter.

```
DB_DIALECT=postgres POSTGRES_URL=<url production> GOOGLE_API_KEY=x R2_PRIVATE_BUCKET=x PIN_PEPPER=x \
  go run ./cmd/assign_admin_role --apply
```
**C'est ici la vraie bascule** — pas le déploiement de code. Le total doit
être identique au dry-run ; toute divergence signale une écriture
concurrente sur `users_rights`, à investiguer avant de continuer.

**Vérifications d'intégrité, dans l'ordre** :
```sql
SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL; -- doit être 0
SELECT COUNT(*) FROM users_rights ur JOIN roles r ON r.id = ur.role_id WHERE r.merchant_id <> ur.merchant_id; -- doit être 0, sinon fuite inter-établissements, urgence
```

**Retour arrière** : `UPDATE users_rights SET role_id = NULL;` — réversible
en une instruction, restaure le comportement historique exact quel que soit
le nombre de lignes déjà traitées.

Purge Redis optionnelle (`redis-cli --scan --pattern 'user:token:v2:*' |
xargs redis-cli DEL` — jamais `FLUSHALL`) : sans conséquence utilisateur si
sautée, juste deux régimes qui cohabitent jusqu'à expiration du TTL
(60 min).

---

## 5. Vague C — rattrapage

### 5.1 `cmd/backfill_customer_stats`

**Toujours dry-run d'abord** :
```
POSTGRES_URL=<url production> go run ./cmd/backfill_customer_stats
```
Puis, si le rapport est cohérent (comparer le % de comptes "changed" à
l'ordre de grandeur staging) :
```
POSTGRES_URL=<url production> go run ./cmd/backfill_customer_stats --apply
```

**Durée : au moins 12 minutes** à volumétrie égale à staging — plus si la
base de clients de production est plus grande (26 224 clients sur staging).
Traite un client à la fois, une petite transaction chacun, jamais un verrou
long sur `customer` (lue en direct par les tablettes).

**Effet attendu, pas une régression** : ce backfill élargit le périmètre
compté aux marketplaces (Uber Eats, Deliveroo — pas seulement Wello Resto
comme le calcul historique) et change `last_order_date` pour devenir la
date de **création** de la commande (pas une autre date déjà en usage). Une
fiche client affichera d'autres nombres après ce passage. **À communiquer
dans le canal support comme un effet attendu de cette migration, pas comme
un bug à traiter en urgence si un restaurateur le signale.**

**Créneau : 3h-5h.** Reprise documentée : `--start-after=<customer_id>` en
cas d'interruption, reprend exactement où l'exécution précédente s'est
arrêtée sans retraiter ce qui l'a déjà été.

**Vérification** : rapport final "changed=0" en relançant le dry-run juste
après l'apply (idempotence).

**Point de vigilance — collision avec le cron de réconciliation** : la
tâche `ReconcileCustomerStats` (`cmd/api/tasks.go`, `30 3 * * *`,
`internal/tasks/customer_stats.go`) échantillonne 500 clients en lecture
seule chaque nuit à 3h30 et journalise une alerte si l'écart dépasse 1%.
Lancer le backfill **après 3h35** (marge de 5 minutes après la fin de ce
cron, qui est rapide — un échantillon, pas un scan complet) évite qu'elle
lise un état transitoire pendant l'`--apply` et journalise une fausse
alerte la nuit même du déploiement. `RecomputeUpsellPatterns` tourne à 3h00
(`internal/tasks/upsell.go`) — sans conflit de verrou avec ce backfill
(tables différentes), mais à garder en tête si le créneau glisse.

### 5.2 Migration 106 — conditionnelle au volume

`106_backfill_shift_status_to_published.up.sql` dépend de `105`
(`ALTER TYPE ... ADD VALUE`, doit être seule dans sa transaction — Postgres
interdit d'utiliser une valeur d'enum dans la transaction qui l'ajoute).

**Mesurer d'abord** :
```sql
SELECT count(*) FROM planning_shifts;
```
55 lignes sur staging. Si la production est du même ordre de grandeur (few
milliers), jouer en un seul `UPDATE` comme le fichier le fait. **Au-delà de
quelques milliers de lignes, procéder par lots** (même logique que la purge
`api_request_logs`, migration 088) plutôt qu'un `UPDATE` unique tenant un
verrou long sur une table potentiellement lue en direct par l'écran
planning.

**Vérification** : `SELECT status, count(*) FROM planning_shifts GROUP BY
status;` — 0 ligne dans `('planned','confirmed','done','cancelled')`, tout
dans `published` ou les états ultérieurs légitimes.

**Pré-requis produit** : confirmer que le code qui lit
`published`/`IsValidPlanningShiftStatus` est bien celui déployé (vague B) —
sinon la vue planning perd des lignes sans que rien ne les réaffiche encore.

---

## 6. Vague D — contraction

**Ne pas jouer le jour J.** Chaque entrée attend sa propre confirmation que
plus aucun code déployé ne lit ce qu'elle supprime — jours ou semaines après
la vague B, pas dans le même créneau.

| Migration | Condition avant de jouer | Vérification |
|---|---|---|
| `104_drop_role_job_title_shift_title_location.up.sql` | Revérifier l'audit "colonnes mortes" (3 dépôts frontend) contre le commit **réellement déployé** en production, pas seulement staging/main | 7 colonnes + `employees_role_enum` absents |
| `110_drop_dead_legacy_rights_columns.up.sql` | Le nouveau code (vague B, ne lit plus ces 5 champs depuis le 2026-08-27) confirmé stable, sans rollback en cours | 5 colonnes absentes de `users_rights` |
| `120_drop_cart_discount_legacy_columns.up.sql` | Revérifier qu'aucun code (Go ou frontend) ne lit/écrit `orders.cart_discount_id`/`cart_discount_code` — vrai au 2026-09-05 (Phase 1 du chantier remises), à reconfirmer au moment de jouer | 2 colonnes absentes |

Chacune est un point de non-retour réel : le `.down.sql` recrée la colonne
avec une valeur par défaut, jamais les données d'origine.

---

## 7. Hors séquence

- **112 (`pg_stat_statements`)** : nécessite `shared_preload_libraries` +
  redémarrage — un geste tableau de bord Render, pas une migration SQL
  classique. Non planifié ici.
- **113 (`drop_users_rights_admin_column`)** : attend que
  `SELECT COUNT(*) FROM users_rights WHERE enabled = TRUE AND role_id IS NULL`
  renvoie 0 **en production** et y reste durablement, **et** un déploiement
  de code qui ne lit plus jamais `Rights.Admin` (`docs/RBAC_REPLI_HISTORIQUE.md`
  pour le recensement complet). Nécessite son propre passage en revue,
  postérieur à ce runbook.
- **117 (`cleanup_deletion_reason_id_quotes`)** : décision produit
  documentée dans le fichier lui-même, indépendante de l'environnement —
  212 lignes concernées sur staging au 2026-09-07. Différée par choix, pas
  par prudence technique.

---

## 8. Checklist finale, réponse oui/non

- [ ] §0 : `cmd/diagnose_migrations` lancé contre production, résultat lu et
      compris (14 migrations indéterminables attendues, pas 13) ?
- [ ] §0.2 : les 6 vérifications manuelles (118-123) faites ?
- [ ] §2 : `schema_migrations` posée, remplie depuis le résultat de l'étape 0 ?
- [ ] Vague A jouée (hors 111) : aucune erreur, `GET /health` toujours `200` ?
- [ ] §3.9 : pré-requis doublon `merchant_id` vérifié à 0 pour les deux
      tables d'intégration, juste avant de jouer 111 ?
- [ ] §3.9 : 111 jouée, PUIS immédiatement le déploiement de code (jamais
      l'inverse, jamais avec un délai) ?
- [ ] §4.1 : `seed_system_roles` exécuté, invariant catalogue = 0 ligne
      manquante ?
- [ ] §4.2 : requête nominative lancée, résultat = 0 ligne ou comptes de
      test uniquement ?
- [ ] §4.3 : `assign_admin_role --dry-run` puis `--apply`, totaux
      identiques, `role_id IS NULL AND enabled` = 0, requête cross-tenant = 0 ?
- [ ] §5.1 : `backfill_customer_stats` dry-run lu, `--apply` lancé après
      3h35, rapport final "changed=0" au second dry-run ?
- [ ] §5.2 : volume `planning_shifts` mesuré, 106 jouée en conséquence
      (lot unique ou par lots) ?

Une seule réponse "non" ou inattendue → **s'arrêter**, comprendre avant de
continuer.

---

## Créneau et durée totale estimée

**3h-5h**, creux d'activité mesuré (hérité de `RBAC_DEPLOIEMENT_PROD.md`
§7, toujours valable). La vague A (hors 111) n'a en réalité besoin d'aucun
créneau spécial et peut partir en journée, dans les jours qui précèdent —
seules 111 + la vague B + la vague C doivent tenir dans la fenêtre de nuit.

Estimation, dominée par deux inconnues (taille réelle d'`orders`/
`orderitems`/`api_request_logs` en production pour les `CONCURRENTLY`, et
le nombre réel de clients pour le backfill) :

- Vague A (hors 111), si jouée à part en journée : 10-15 min de travail
  humain, temps de construction des index en tâche de fond.
- 111 + déploiement de code + vérification santé : 15-30 min.
- `seed_system_roles` + invariant + requête nominative + `assign_admin_role` : 10-20 min.
- `backfill_customer_stats` : **12 min minimum**, plus selon le volume réel.
- 106 (si jouée dans la même fenêtre) : quelques minutes si en lot unique.

**Total fenêtre de nuit réaliste : 45 minutes à 1h30**, hors marge
d'observation post-bascule recommandée avant le retour de l'activité.

## Cinq premières commandes exactes, jour J

```bash
# 1. Diagnostic bloquant — rien d'autre avant d'avoir lu ce résultat.
POSTGRES_URL="postgres://...production..." go run ./cmd/diagnose_migrations

# 2. Les six vérifications manuelles hors périmètre de l'outil (118-123) — §0.2.
#    (à lancer via psql ou un script Go jetable, comme le §0.2 ci-dessus)

# 3. Poser la table de suivi.
psql "$PROD_URL" -f migrations/todo/123_schema_migrations.up.sql

# 4. Enregistrer l'état constaté par 1-2 dans schema_migrations
#    (INSERT écrit à la main depuis le résultat du diagnostic, PAS depuis
#    migrations/staging_only_schema_migrations_backfill.sql).

# 5. Doublons merchant_id — pré-requis bloquant avant TOUTE la suite (§3.9).
psql "$PROD_URL" -c "SELECT merchant_id, count(*) FROM integration_uber_eats GROUP BY merchant_id HAVING count(*) > 1;"
psql "$PROD_URL" -c "SELECT merchant_id, count(*) FROM integration_deliveroo GROUP BY merchant_id HAVING count(*) > 1;"
```

---

*Ce document remplace `docs/RBAC_DEPLOIEMENT_PROD.md` comme référence
opérationnelle. Ce dernier reste dans le dépôt, marqué comme archivé, pour
l'historique du chantier RBAC lots 1-11 — ne plus le mettre à jour.*
