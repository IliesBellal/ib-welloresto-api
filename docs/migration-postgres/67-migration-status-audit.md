# 67 — État réel du schéma : staging et production (PROMPT 13)

Vérification en lecture seule, migration par migration, de ce qui est réellement
appliqué sur staging et de ce qu'on peut savoir sans accès direct sur
production. **Aucune migration n'a été appliquée par cette session.** Portée :
`087_analytics_indexes` et `094` à `117` (`migrations/todo/`) — le seul
périmètre où l'état était en doute (voir §0). `migrations/done/` est traité
séparément, en note méthodologique.

Outil réutilisable produit par cette session : [`cmd/diagnose_migrations`](../../cmd/diagnose_migrations/main.go)
— voir §2.

---

## 0. Méthode

Pour chaque migration de `migrations/todo/`, la vérification teste **l'effet
réel** contre le schéma (colonne, table, index par sa définition dans
`pg_indexes`/`pg_index`, valeur d'enum, ligne de catalogue) — jamais une
inférence depuis le nom du fichier. Quand une requête ne trouvait rien, elle a
d'abord été recroisée avec `pg_indexes`/`information_schema.columns` sans
filtre de nom pour écarter une requête mal ciblée (c'est ce qui a fait échouer
7 vérifications lors de la session précédente) — voir en particulier §1.1 pour
087.

**Trois états, plus un quatrième pour le cas dangereux** : APPLIQUÉE, NON
APPLIQUÉE, INDÉTERMINABLE (aucune trace observable, ou jeu de données trop
petit/vide pour distinguer les deux états — jamais un "probablement"), et
PARTIELLE quand une migration à plusieurs instructions n'a réussi qu'en partie.

**`migrations/done/` n'a pas été revérifié fichier par fichier.** Ce dossier
est la convention du dépôt pour "migration confirmée passée" (CLAUDE.md), et
six sondages répartis sur toute sa plage — `003_create_availabilities_tables`
(le plus ancien), `032_delivery_module`, `079_configurable_attribute_options_ingredient_link`,
`086_merchant_parameters_pos_covers_count_required`, `088_api_request_logs_duration`,
`089_delivery_module_flag` (le plus récent) — confirment tous leur effet sur
staging. Revérifier individuellement les ~179 fichiers restants n'aurait rien
appris de plus que ce que 087 et 094-117 ont déjà révélé : le problème n'est
pas "done ment", c'est que rien n'enregistre le passage de todo/ vers done/.

---

## 1. Tableau définitif

Méthode abrégée dans la colonne "Vérif." : **schéma** = colonne/table/type
présent(e) ou non ; **pg_indexes** = définition d'index recherchée sans
supposer le nom ; **données** = comptage/distribution de valeurs ; **doc** =
affirmation explicite trouvée dans un fichier du dépôt (jamais suffisant seul
sur staging, accepté comme seule source sur production faute d'accès).

| # | Migration | Staging | Prod | Vérif. |
|---|---|---|---|---|
| 087 | analytics_indexes (4 index CONCURRENTLY) | **NON APPLIQUÉE** — 0/4 index présents dans `pg_indexes`, confirmé aussi par un balayage complet de tous les index sur `orders/orderitems/extra/payments` (seuls PK + 3 index préexistants sans rapport) | INDÉTERMINABLE (pas d'accès ; aucune mention de statut prod dans le dépôt) | pg_indexes (nom + balayage large) |
| 094 | roles_schema (RBAC lot 1) | APPLIQUÉE — tables `permissions/roles/role_permissions`, `users_rights.role_id`, `merchant.default_role_id`, `audit_logs.resource_id/user_id` élargis à 64 | NON APPLIQUÉE | schéma ; doc (`docs/RBAC_DEPLOIEMENT_PROD.md` : "la production n'a jamais reçu une seule des migrations RBAC") |
| 095 | roles_permissions_catalog (14 clés) | APPLIQUÉE | NON APPLIQUÉE | données ; doc |
| 096 | seed_system_roles (`cmd/seed_system_roles`, pas du SQL) | APPLIQUÉE — 30 rôles admin / 30 rôles staff pour 30 marchands | NON APPLIQUÉE | données ; doc |
| 097 | permission_pos_status_manage | APPLIQUÉE | NON APPLIQUÉE | données ; doc |
| 098 | access_observation (table) | APPLIQUÉE — table créée, 0 ligne observée à ce jour | NON APPLIQUÉE | schéma ; doc |
| 099 | merchant_default_role_admin | APPLIQUÉE — 30/30 marchands avec rôle admin pointent dessus | NON APPLIQUÉE | données ; doc |
| 100 | deprecate_pos_access_and_discount_apply | APPLIQUÉE — les deux clés absentes | NON APPLIQUÉE | données ; doc |
| 101 | production_profiles | APPLIQUÉE — les deux tables existent, types Postgres corrects | INDÉTERMINABLE | schéma — **voir §1.2, alerte fichier** |
| 102 | delivery_travel_seconds | APPLIQUÉE — colonne + table `average_delivery_time` | INDÉTERMINABLE | schéma — **voir §1.2, alerte fichier** |
| 103a | permission_catalog_lot10 (2ᵉ fichier "103") | APPLIQUÉE — 5/5 clés, 0 description vide | NON APPLIQUÉE | données ; doc |
| 103b | production_ready_delivery_arrival (1ᵉʳ fichier "103") | APPLIQUÉE — colonnes ajoutées, `dateCall` supprimée | INDÉTERMINABLE | schéma |
| 104 | drop_role_job_title_shift_title_location | **NON APPLIQUÉE** — 0/7 colonnes supprimées, type `employees_role_enum` toujours présent | INDÉTERMINABLE | schéma |
| 105 | add_published_shift_status (valeur d'enum) | **NON APPLIQUÉE** — `planning_shifts_status_enum` ne contient que planned/confirmed/done/cancelled/draft | INDÉTERMINABLE | pg_enum |
| 106 | backfill_shift_status_to_published | **NON APPLIQUÉE** — 0 ligne à `published`, 54 encore dans un état pré-migration sur 55 (bloquée par 105 de toute façon : la valeur n'existe pas encore) | INDÉTERMINABLE | données |
| 107 | import_component_mappings | APPLIQUÉE — les deux tables existent | INDÉTERMINABLE | schéma |
| 108 | api_request_logs_response_payload | APPLIQUÉE | INDÉTERMINABLE | schéma |
| 109 | api_request_logs_created_at_index | APPLIQUÉE — index présent et valide | INDÉTERMINABLE | pg_indexes |
| 110 | drop_dead_legacy_rights_columns | APPLIQUÉE — 0/5 colonnes restantes | NON APPLIQUÉE | schéma ; doc (couverte explicitement par `RBAC_DEPLOIEMENT_PROD.md` §3.2) |
| 111 | multi_account_uber_deliveroo | **NON APPLIQUÉE** — PK toujours `(merchant_id)` seul sur les deux tables d'intégration, aucune colonne `store_id/location_id` sur les mappings, `orders.brand_store_id` absent | INDÉTERMINABLE | schéma |
| 112 | pg_stat_statements | *Hors plan (voir consigne)* — extension non installée | *Hors plan* — non installée nulle part à la connaissance du dépôt (nécessite un changement `shared_preload_libraries` + redémarrage, action tableau de bord Render) | pg_extension ; doc |
| 113 | drop_users_rights_admin_column | *Hors plan (voir consigne)* — colonne toujours présente, bloquée par du code en attente de retrait | *Hors plan* — colonne toujours présente | schéma ; doc (fichier lui-même + `RBAC_DEPLOIEMENT_PROD.md` vague C) |
| 114 | write_path_instrumentation lot 1 | APPLIQUÉE — 5/5 colonnes, `brand_status` 100% majuscules, `order_source` rétro-rempli 33859/33862 (99,99%, cohérent avec le commentaire de la migration) | INDÉTERMINABLE | schéma ; données |
| 115 | permission_reports_staff_performance_read | APPLIQUÉE | INDÉTERMINABLE | données |
| 116 | write_path_instrumentation lot 2 | APPLIQUÉE — 4/4 colonnes, `orders.deletion_reason_id` élargie à 32 | INDÉTERMINABLE | schéma |
| 117 | cleanup_deletion_reason_id_quotes | *Hors plan (voir consigne)* — NON APPLIQUÉE, 212 lignes encore entre guillemets | *Hors plan* — décision produit documentée dans le fichier, indépendante de l'environnement ; non vérifiée en direct | données |

### 1.1 Sur 087 spécifiquement

Trois requêtes convergent, aucune ne s'appuie sur le nom supposé de l'index :

1. `pg_indexes` filtré sur les 4 noms de l'énoncé de la migration → 0 ligne.
2. `pg_index`/`pg_class` (même filtre par nom, mais sur le catalogue système,
   qui aurait aussi remonté un index laissé `INVALID` par une création
   `CONCURRENTLY` interrompue) → 0 ligne également : même un index invalide
   n'existe pas.
3. Un balayage de **tous** les index sur `orders`, `orderitems`, `extra`,
   `payments`, sans filtre de nom → seuls les 4 PK plus 3 index préexistants
   sans rapport (`idx_orders_idx_orders_brand_status`,
   `idx_orders_idx_orders_merchant_id`, `idx_orders_idx_orders_state`,
   `idx_orderitems_idx_orderitems_product_id`) apparaissent.

`pg_stat_user_tables.last_analyze` sur les 4 tables est daté du 24/08/2026,
groupé sur ~2 minutes — cohérent avec le protocole de mesure décrit en
en-tête du fichier de migration ("création/suppression alternées dans une
transaction annulée") : les `ANALYZE` de cette étude ont tourné, pas la
migration elle-même. **Conclusion sans ambiguïté : 087 n'est pas appliquée sur
staging.**

### 1.2 Alerte : 101 et 102 (et 103b) ne peuvent pas être rejoués tels quels sur Postgres

`101_production_profiles.up.sql` et `102_delivery_travel_seconds.up.sql`
contiennent de la syntaxe MySQL non convertie (`ENGINE=InnoDB`,
`INT UNSIGNED`, `DATETIME ... ON UPDATE CURRENT_TIMESTAMP`, clause `AFTER`) —
invalide en Postgres, échouerait immédiatement (`103_production_ready_delivery_arrival.up.sql`
utilise aussi `AFTER`, sans autre invalidité). Pourtant les trois migrations
sont bien appliquées sur staging, avec des types Postgres corrects
(`character varying`, `timestamp without time zone`) — l'effet a donc été
obtenu par un autre chemin que ces fichiers (probablement une version adaptée
jouée à la main, jamais reportée sur le fichier source).

**Conséquence pour le plan (§3) : si ces trois migrations sont manquantes en
production, ces trois fichiers doivent être réécrits en syntaxe Postgres
avant exécution — les jouer tels quels échouera.** Ce n'est pas un problème
théorique : c'est exactement le genre d'écart qu'un `schema_migrations`
(§4) aurait signalé plus tôt.

---

## 2. Script de diagnostic autonome

[`cmd/diagnose_migrations`](../../cmd/diagnose_migrations/main.go) — nouveau,
produit par cette session. Reproduit exactement les vérifications du tableau
ci-dessus (mêmes requêtes, même logique de verdict), sous une forme que le
propriétaire produit peut lancer lui-même contre n'importe quelle base :

```bash
POSTGRES_URL="postgres://...production..." go run ./cmd/diagnose_migrations
```

Garanties de sécurité :
- Une seule transaction `BEGIN ... READ ONLY`, systématiquement annulée
  (`ROLLBACK`, jamais `COMMIT`) à la fin — même un bug qui tenterait une
  écriture serait rejeté par Postgres lui-même, pas seulement par la bonne
  conduite du programme.
- Ne dépend pas de `internal/config.Load()` (qui exige `GOOGLE_API_KEY`,
  `R2_PRIVATE_BUCKET`, `PIN_PEPPER` sans rapport avec ce diagnostic) —
  connexion directe via `database/sql` + `pgx`, une seule variable
  d'environnement à fournir.
- Sortie : une ligne par migration, verdict + détail chiffré. 112/113/117
  sont affichés mais marqués explicitement "hors plan" en pied de sortie.

Validé en le faisant tourner contre staging pendant cette session : les 26
verdicts produits correspondent exactement au tableau §1 (voir sortie
capturée dans la session — reproductible en relançant la commande).

---

## 3. Plan d'application ordonné

**Rien n'est exécuté ici.** Exclus explicitement du plan, par consigne : 112
(`pg_stat_statements`), 113 (drop `users_rights.admin`), 117 (nettoyage des
guillemets) — ne pas les ajouter par symétrie avec le reste.

### 3.1 Staging — 5 migrations manquantes

Sur staging, tout le périmètre RBAC (094-100, 103a, 110) est déjà en place ;
seules ces 5 sont manquantes. Indépendantes entre elles à une exception près
(105 → 106) ; ordonnées ici par risque croissant.

| Ordre | Migration | Dépend de | Additive/destructive | Risque | Vérification |
|---|---|---|---|---|---|
| 1 | 087 | — | Additive (CONCURRENTLY, pas de verrou `ACCESS EXCLUSIVE`) | Faible. Volumes staging petits (orders 22 MB/34k lignes, orderitems 11 MB/77k) → construction en quelques secondes. Débloque la campagne de mesure : **exécuter en premier**. Jouer instruction par instruction, hors transaction (le fichier lui-même l'exige — `CONCURRENTLY` refuse un bloc `BEGIN`). | `SELECT indexname FROM pg_indexes WHERE indexname IN ('idx_orders_merchant_creation','idx_orderitems_order_id','idx_extra_order_item_id','idx_payments_order_id');` → 4 lignes, puis `SELECT indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname = ANY(ARRAY[...]);` → tout `true`. Si un index reste `invalid`, `DROP INDEX CONCURRENTLY` (voir `.down.sql`) et rejouer ce seul index — ne jamais laisser un index invalide en place. |
| 2 | 105 | — (mais bloque 106) | Additive (`ALTER TYPE ... ADD VALUE`) | Faible. Doit être seule dans sa transaction/son fichier (déjà le cas) — Postgres interdit d'utiliser la valeur dans la même transaction qui l'ajoute. | `SELECT enumlabel FROM pg_enum e JOIN pg_type t ON t.oid=e.enumtypid WHERE t.typname='planning_shifts_status_enum';` → doit inclure `published`. |
| 3 | 106 | **105** (strictement, transaction séparée) | Destructive au sens données (réécrit `status` de 54 lignes actuellement `planned`) | Faible sur staging (55 lignes au total dans `planning_shifts`). Confirmer que le code qui filtre par statut (`ListPlanningShiftsTeamWeekView`, `IsValidPlanningShiftStatus`) est déjà déployé sur staging avant de jouer — sinon la vue self-service perd des lignes sans que rien ne les réaffiche encore. | `SELECT status, count(*) FROM planning_shifts GROUP BY status;` → 0 ligne dans `('planned','confirmed','done','cancelled')`. |
| 4 | 104 | — | Destructive (7 `DROP COLUMN`, 1 `DROP TYPE`) | Faible sur staging (tables à 1-59 lignes). `DROP COLUMN` est une opération de métadonnées en Postgres (pas de réécriture de table pour un simple retrait), verrou `ACCESS EXCLUSIVE` bref. L'audit du fichier affirme ces 7 colonnes mortes dans les 3 dépôts frontend — revérifier que le commit qui a fait cet audit est bien celui déployé sur staging avant de jouer. | Les 7 requêtes `information_schema.columns` du §1 (voir aussi le fichier `.up.sql`) → 0 ligne chacune ; `SELECT 1 FROM pg_type WHERE typname='employees_role_enum';` → 0 ligne. |
| 5 | 111 | — | Additive (schéma) + destructive au sens contrainte (`DROP CONSTRAINT` PK puis `ADD PRIMARY KEY`) | Faible sur staging : `integration_uber_eats`/`integration_deliveroo` sont **vides** (0 ligne chacune) — le changement de PK est instantané, aucune donnée à recomposer. Les `UPDATE` de rétro-remplissage scannent `orders` (34k lignes) mais ne matchent rien tant que les tables d'intégration restent vides — no-op rapide. | Les requêtes PK + colonnes du §1 ; `SELECT count(*) FROM orders WHERE brand_store_id IS NOT NULL;` (attendu proche de 0 sur staging tant qu'aucun compte multi-Uber/Deliveroo n'existe). |

### 3.2 Production

**Le sous-ensemble RBAC (094, 095, 096, 097, 098, 099, 100, 103a/"lot10",
110, et la préparation de 113) a déjà un runbook détaillé et à jour :
[`docs/RBAC_DEPLOIEMENT_PROD.md`](../RBAC_DEPLOIEMENT_PROD.md).** Ne pas le
redupliquer ici — le suivre tel quel. Deux ajustements à lui apporter avant
usage, découverts par cette session :

- **115** (`reports.staff_performance.read`) n'existe pas encore quand ce
  runbook a été écrit (2026-09-03) — c'est une clé de catalogue additive de
  même forme que 097/103a. À ajouter à sa "vague A" (même risque, même
  méthode), sinon le total attendu après la vague A ("18 clés") sera faux dès
  qu'elle existera aussi.
- Confirmer l'état réel avec `cmd/diagnose_migrations` **avant** de suivre ce
  runbook : son hypothèse de départ ("`roles` n'existe pas") est datée
  2026-09-03 et n'a jamais été reconfirmée depuis.

Le reste — **087, 101, 102, 103b, 104, 105, 106, 107, 108, 109, 111, 114,
116** — n'est couvert par aucun runbook existant et l'état réel en production
est **INDÉTERMINABLE sans accès** (voir §1). Le tableau suivant suppose le pire
cas raisonnable (rien de ce sous-ensemble n'est appliqué, symétrique à ce que
`RBAC_DEPLOIEMENT_PROD.md` affirme déjà pour le sous-ensemble RBAC) — **à
confirmer avec le script du §2 avant toute exécution**, en particulier parce
que 101/102/103b/107/108/109 sont déjà **appliquées sur staging** et rien ne
garantit qu'elles le sont aussi (ou ne le sont pas) en production.

| Ordre | Migration | Dépend de | Additive/destructive | Risque prod (inconnu sur les volumes réels — voir note) | Vérification |
|---|---|---|---|---|---|
| 1 | 108 | — | Additive (`ADD COLUMN jsonb`) | Faible : colonne nullable, pas de réécriture de table sous Postgres 11+. | Colonne présente. |
| 2 | 109 | 108 (logique produit, pas technique) | Additive (index simple, pas `CONCURRENTLY`) | **À vérifier avant de jouer** : `api_request_logs` faisait 207k lignes/51 Mo sur staging au moment de l'écriture de 088 — en production, probablement bien plus. Un `CREATE INDEX` simple (non `CONCURRENTLY`) prend un verrou qui bloque les écritures pendant la construction. **Adapter en `CREATE INDEX CONCURRENTLY` avant de jouer en production** si la table y est significativement plus grosse qu'en staging. | Index présent et `indisvalid = true`. |
| 3 | 107 | — | Additive (2 tables + index) | Faible, tables neuves. | Les 2 tables existent. |
| 4 | 101 | — | Additive (2 tables) | Faible, tables neuves. **Réécrire le fichier en syntaxe Postgres avant de le jouer (§1.2)** — tel quel il échoue. | Les 2 tables existent, types Postgres. |
| 5 | 102 | — | Additive (1 colonne + 1 table) | Faible. **Réécrire en syntaxe Postgres avant de jouer (§1.2).** | Colonne + table présentes. |
| 6 | 103b | 102 (le calcul de `production_ready_at` utilise `delivery_travel_seconds`) | Additive (2 colonnes) + destructive (`DROP COLUMN dateCall`) | Faible si `dateCall` est bien mort en production comme documenté (à revérifier : l'audit cité date de staging). | 2 colonnes présentes, `datecall` absent. |
| 7 | 105 | — | Additive (valeur d'enum) | Faible, mais **vérifier le volume réel de `planning_shifts` en production avant 106** (voir ligne suivante). | Valeur `published` présente dans l'enum. |
| 8 | 106 | **105** | Destructive (données) | **Dépend du volume réel** — staging n'avait que 55 lignes, un volume de production peut être bien supérieur. Si le nombre de lignes concernées est grand, faire un `UPDATE` par lots plutôt qu'en un seul statement (même logique que la purge proposée pour `api_request_logs` en migration 088), pour ne pas tenir un verrou long sur une table potentiellement lue par le planning en direct. | 0 ligne restante dans `('planned','confirmed','done','cancelled')`. |
| 9 | 104 | — | Destructive (7 `DROP COLUMN` + 1 `DROP TYPE`) | Faible techniquement (métadonnées), mais **revérifier l'audit "colonnes mortes" contre le code réellement déployé en production**, pas seulement contre staging/main — un déploiement production en retard pourrait encore lire ces colonnes. | Les 7 colonnes + le type absents. |
| 10 | 111 | — | Additive (colonnes/tables) + contrainte (PK) | **Le point le plus délicat de tout le plan.** Contrairement à staging (tables d'intégration vides), la production a presque certainement des lignes dans `integration_uber_eats`/`integration_deliveroo` — le changement de PK doit rester sans collision (`(merchant_id, store_id)` unique), ce qui est garanti tant qu'un seul compte existe par marchand aujourd'hui (hypothèse du fichier), **à vérifier explicitement** (`SELECT merchant_id, count(*) FROM integration_uber_eats GROUP BY merchant_id HAVING count(*) > 1;` doit renvoyer 0 ligne, sinon la migration échoue sur la contrainte). Les `UPDATE` de rétro-remplissage scannent `orders`, potentiellement une table à plusieurs centaines de milliers de lignes en production (34k sur staging) — mesurer la durée avant de jouer en heure de pointe. **Les `CREATE INDEX` de ce fichier ne sont pas `CONCURRENTLY`** (contrairement à 087, qui l'est précisément parce qu'`orders`/`orderitems`/`payments` sont des tables chaudes) — **réécrire au moins `idx_orders_brand_store_id` en `CREATE INDEX CONCURRENTLY` avant de jouer en production**, sous peine de reproduire exactement le problème de verrouillage que 087 a été écrite pour éviter. | PK composite sur les 2 tables ; colonnes `store_id`/`location_id` sur les mappings ; `orders.brand_store_id` présent. |
| 11 | 087 | — | Additive (`CONCURRENTLY`) | Faible par construction (c'est tout l'objet du fichier), mais l'ordre de grandeur réel d'`orders`/`orderitems`/`payments` en production est inconnu d'ici — prévoir plus de marge que les quelques secondes observées sur staging. | 4 index présents et valides (mêmes requêtes qu'en §3.1). |
| 12 | 114 | — | Additive (colonnes) + rétro-remplissage (`UPDATE ... WHERE order_source IS NULL`, `UPDATE ... WHERE cancelled_by_type IS NULL`, `UPDATE brand_status = upper(...)`) | Le rétro-remplissage scanne toute la table `orders` — mesurer la durée sur le volume réel de production avant de jouer en heure de pointe (34k lignes ⇒ quasi instantané sur staging, pas de garantie sur un volume 10-50x plus grand). | Colonnes présentes ; `SELECT count(*) FILTER (WHERE brand_status <> upper(brand_status)) FROM orders;` → 0. |
| 13 | 116 | 114 (même famille de colonnes coût de revient) | Additive | Faible, colonnes nullables sans rétro-remplissage de coût (décision explicite du fichier). | Colonnes présentes ; largeur de `deletion_reason_id` = 32. |

Note générale sur le risque prod : cette session n'a **aucune visibilité** sur
le volume réel des tables de production (`orders`, `orderitems`,
`integration_uber_eats`, `planning_shifts`...). Toutes les mentions "faible"
ci-dessus supposent un ordre de grandeur proche de staging ; **lancer
`cmd/diagnose_migrations` en production en confirme l'état d'application, pas
la taille des tables — mesurer les tailles séparément
(`pg_stat_user_tables`, déjà interrogé par ce script pour information) avant
toute migration touchant `orders`/`orderitems`/`payments`/`integration_*` en
heure de pointe.**

---

## 4. Empêcher que ça se reproduise — proposition

**Ne pas implémenter dans cette session — ce qui suit est une proposition.**

### 4.1 Table `schema_migrations`

Une table minimale, remplie rétroactivement à partir du diagnostic ci-dessus,
puis à chaque application manuelle future :

```sql
CREATE TABLE schema_migrations (
    version     varchar(20) PRIMARY KEY,  -- "087", "094", "103a", "103b" (les deux fichiers "103" ont besoin d'un identifiant distinct du numéro seul)
    filename    varchar(255) NOT NULL,     -- nom exact du fichier .up.sql, pour retrouver la source même si un numéro est renommé (cf. le renommage 089->094)
    applied_at  timestamptz NOT NULL DEFAULT now(),
    applied_by  varchar(150),              -- qui a joué la migration à la main (nom/email), pas un automatisme
    notes       text                       -- ex. "réécrite en syntaxe Postgres avant exécution, voir 101/102"
);
```

Pas de `down_applied`/rollback tracking : ce projet n'a pas d'outil qui rejoue
les `.down.sql` automatiquement, donc un rollback resterait de toute façon un
geste manuel documenté ailleurs (commit, incident).

**Remplissage rétroactif immédiat** (staging, à partir de ce rapport) :

```sql
INSERT INTO schema_migrations (version, filename, applied_at, notes) VALUES
    ('094', '094_roles_schema.up.sql', now(), 'appliquée sous le numéro 089 avant renumérotation'),
    ('095', '095_roles_permissions_catalog.up.sql', now(), NULL),
    -- ... une ligne par migration APPLIQUÉE du tableau §1, staging uniquement
    ;
```

(Production reçoit son propre remplissage le jour où son propre diagnostic
tourne — ne pas copier celui de staging, les deux bases divergent déjà.)

### 4.2 Contrôle au démarrage de l'API

Dans `cmd/api/main.go`, juste après la connexion à la base et avant
`SetupRoutes()` : lire les fichiers de `migrations/todo/*.up.sql`, comparer
leurs numéros à ceux présents dans `schema_migrations`, et **logger un
avertissement** (pas un `log.Fatal` — ce projet applique ses migrations à la
main, un déploiement ne doit pas se bloquer sur un écart qui peut être
légitime, ex. une migration volontairement préparée-pas-appliquée comme
112/113/117) listant les fichiers présents sans ligne correspondante. Un
avertissement au démarrage, visible dans les logs Render à chaque déploiement,
est la version la plus légère de "quelqu'un le remarque" — cohérent avec le
choix déjà fait par ce projet de ne pas adopter d'outil de migration.

### 4.3 Ce que cette proposition ne fait pas

Ne remplace pas l'application manuelle (choix déjà assumé par le projet), ne
rejoue rien automatiquement, n'ajoute pas de dépendance externe. Le seul
geste nouveau demandé à quiconque applique une migration à la main : une
ligne `INSERT INTO schema_migrations` juste après, avec son nom. C'est la
seule discipline qui aurait évité les deux sessions passées à reconstituer
cet état.

---

## Résumé (15 lignes)

**087 est-elle appliquée sur staging ? Non.** Confirmé par trois requêtes
convergentes contre `pg_indexes`/`pg_index`, dont un balayage complet des
index sur `orders/orderitems/extra/payments` sans supposer de nom : aucun des
4 index n'existe, même à l'état invalide. La campagne de mesure reste
bloquée tant que 087 n'est pas jouée — c'est la première ligne du plan
staging (§3.1), sans dépendance, faible risque.

**5 migrations manquent sur staging** : 087, 104 (7 colonnes mortes à
supprimer), 105 (valeur d'enum `published`), 106 (backfill, dépend de 105),
111 (multi-compte Uber/Deliveroo — PK et colonnes absentes). Tout le reste de
094-117 (17 migrations) est déjà appliqué, y compris 3 fichiers (101, 102,
103b) dont le SQL sur disque est en syntaxe MySQL non convertie et
échouerait s'il était rejoué tel quel — l'effet existe en base par un autre
chemin que le fichier.

**Production** : confirmé sans accès direct, mais avec preuve documentaire
explicite (`docs/RBAC_DEPLOIEMENT_PROD.md`), que 094-100/103a/110 (RBAC)
n'y sont jamais passées — un runbook détaillé existe déjà pour ce
sous-ensemble. Le reste (087, 101, 102, 103b, 104-109, 111, 114, 115, 116)
est INDÉTERMINABLE sans le script `cmd/diagnose_migrations` livré ici, à
lancer avant tout déploiement — ne pas supposer que l'état de staging s'y
reproduit, notamment parce que 111 y demande un `CREATE INDEX CONCURRENTLY`
qu'87 a justement introduit pour éviter de verrouiller `orders`.
