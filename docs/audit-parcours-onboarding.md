# Audit — Parcours d'inscription, d'abonnement et de conformité (Phase 0)

**Audit en lecture seule.** Aucune modification de code, aucune migration, aucun refactoring, aucune recommandation de solution — uniquement l'état des lieux factuel du code et du schéma tels qu'observés le 2026-08-28, sur le dépôt `ib-welloresto-api` (branche `staging`) et les dépôts satellites de l'écosystème WelloResto (`wello-back-office`, `wello_resto_flutter`, `wello-kiosk`, `wello-resto-scannorder`).

## Note méthodologique préalable

Il n'existe **aucune migration `CREATE TABLE` pour la table marchand** dans `migrations/` (grep exhaustif sur `merchants`/`merchant` dans tout `migrations/done/` : aucun `CREATE TABLE`, seulement des `FOREIGN KEY (merchant_id) REFERENCES ...`, des commentaires, et un seul `ALTER TABLE merchant ADD COLUMN default_role_id ...`). Le schéma de base (table marchand, `users`, `users_rights`, `employees`, etc.) préexiste au dossier `migrations/` et n'est reconstituible depuis aucun fichier de migration du dépôt.

La source la plus fiable trouvée est **`docs/migration-postgres/wello-resto-mysql-ddl.md`** : un export phpMyAdmin réel (`SHOW CREATE TABLE`), en-tête :
```
-- phpMyAdmin SQL Dump
-- version 5.2.2
-- Hôte : 127.0.0.1:3306
-- Généré le : lun. 13 juil. 2026 à 11:05
-- Version du serveur : 11.8.8-MariaDB-log
-- Base de données : `u231520952_welloresto`
```
C'est un dump réel de la base MySQL de production Hostinger, daté du **13 juillet 2026**. Toutes les structures de table citées dans ce document en proviennent, sauf mention contraire explicite (colonnes ajoutées par une migration postérieure à cette date, identifiée séparément).

**Point critique qui conditionne la lecture de tout le document** : le dépôt contient un chantier de bascule MySQL → PostgreSQL en cours (`docs/migration-postgres/`). Le fichier `docs/RBAC_BASCULE.md` indique explicitement, à la date du **2026-08-27** (la veille du jour de cet audit) :

> « Ces six migrations sont déjà **appliquées en recette** (094-098 sous leurs anciens numéros 089-093 [...] ; 099 a été exécutée directement sous ce numéro le 2026-08-27 [...]). **En production, aucune des six n'a jamais été jouée** : elles partent de zéro sous ces numéros. »

et `migrations/done/094_roles_schema.up.sql` (lignes 1-11) confirme :

> « Staging already has this migration's DDL applied (it ran as 089); **production has never received it** and will receive it under this new number, 094. »

Ces migrations 094-099 (schéma RBAC : `permissions`, `roles`, `role_permissions`, `users_rights.role_id`, `merchant.default_role_id`) sont écrites en **syntaxe PostgreSQL pure**, et « recette »/« staging » y désigne l'instance Postgres de bascule (Render), distincte de MySQL Hostinger réel. Par défaut, l'API se connecte en MySQL (`cmd/api/main.go:24` : « DB (MySQL par défaut, Postgres si DB_DIALECT=postgres — migration en cours) »). Ce point conditionne notamment la lecture de la Section 3 (Permissions) et de la Section 8 (création de compte) : le système RBAC (rôles nommés, table `roles`) décrit dans ce document est démontré par du code réel et des migrations réelles, mais — au 2026-08-27, selon la documentation même du dépôt — n'a encore jamais tourné contre la base de production MySQL.

---

## 1. Modèle de données — identité et rattachement

### 1.1. Structure exacte de la table marchand

**Précision terminologique importante** : la table s'appelle **`merchant`, au singulier** — pas `merchants`. Tout le code Go (`internal/modules/pos/create_repository.go`, `internal/modules/auth/repository.go`, `internal/modules/bookings/repository.go`, `internal/modules/kiosk/repository.go`, etc.) écrit et lit `FROM merchant` / `INSERT INTO merchant` / `UPDATE merchant SET`.

Fait notable : `migrations/done/003_create_availabilities_tables.sql:22` contient `FOREIGN KEY (merchant_id) REFERENCES merchants(merchant_id)` — au pluriel, avec une colonne `merchant_id` comme clé, ce qui ne correspond à **aucune** table réelle du dépôt (la vraie table est `merchant`, PK `id`, integer). Cette FK référence une table qui n'existe pas sous ce nom/cette forme dans le schéma réel ; c'est une incohérence brute observée dans le fichier de migration, non résolue plus loin dans le code.

**Structure réelle (dump MySQL du 2026-07-13)**, `docs/migration-postgres/wello-resto-mysql-ddl.md:1782-1806` :

```sql
CREATE TABLE `merchant` (
  `id` int(11) NOT NULL,
  `brand_id` varchar(35) DEFAULT NULL,
  `fullName` varchar(50) NOT NULL,
  `address` text NOT NULL,
  `street_number` varchar(25) NOT NULL,
  `street` varchar(255) NOT NULL,
  `zip_code` varchar(6) NOT NULL,
  `city` varchar(255) NOT NULL,
  `country` varchar(255) NOT NULL DEFAULT 'France',
  `lat` double DEFAULT 0,
  `lng` double DEFAULT 0,
  `timezone` varchar(50) NOT NULL DEFAULT 'Europe/Paris',
  `logo` longtext DEFAULT NULL,
  `logo_url` longtext DEFAULT NULL,
  `handicap_access` tinyint(1) NOT NULL DEFAULT 0,
  `SIRET` varchar(50) NOT NULL,
  `vat_number` varchar(50) DEFAULT NULL,
  `web_site` varchar(100) NOT NULL,
  `email` varchar(100) DEFAULT NULL,
  `merchantTel` varchar(15) NOT NULL,
  `token` varchar(20) NOT NULL,
  `creation_date` datetime NOT NULL DEFAULT current_timestamp(),
  `is_active` tinyint(1) NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;
```

Clé primaire et auto-increment (`docs/migration-postgres/wello-resto-mysql-ddl.md:4084-4085` et `:4910-4911`) :
```sql
ALTER TABLE `merchant`
  ADD PRIMARY KEY (`id`);
...
ALTER TABLE `merchant`
  MODIFY `id` int(11) NOT NULL AUTO_INCREMENT;
```

**ALTER TABLE postérieur** (seul trouvé dans tout `migrations/done/`), `migrations/done/094_roles_schema.up.sql:111` :
```sql
ALTER TABLE merchant ADD COLUMN default_role_id varchar(64) REFERENCES roles(id);
```
Cette colonne n'est **pas** présente dans le dump du 2026-07-13 (normal, migration postérieure). Comme expliqué dans la note méthodologique, cette migration (094) n'a, au 2026-08-27, **jamais été appliquée sur MySQL production** — seulement sur l'instance Postgres de recette.

**Champ « statut »** : oui, `is_active tinyint(1) NOT NULL DEFAULT 1`. C'est le seul champ de type état/statut sur cette table. Utilisé en lecture dans `internal/modules/pos/repository.go:1436` (`GetMerchantSettings`) et exposé en écriture potentielle via `models.MerchantSettings.IsActive *bool` (`internal/models/request_objects.go:659`).

Il n'existe **aucun champ `status`, `onboarding_step` ou équivalent** sur `merchant`. Recherche exhaustive de « onboarding » dans le dépôt : toutes les occurrences renvoient exclusivement à l'onboarding **Stripe Connect** (`internal/infrastructure/stripe/connect.go:45` `CreateOnboardingLink`, `internal/modules/integrations/service.go:361-415`, route `POST /integrations/scannorder/onboarding`) — un mécanisme de paiement/KYC, sans rapport avec un état de progression de création de compte marchand.

### 1.2. Table `establishments`

**N'existe pas.** Recherche `CREATE TABLE` dans `docs/migration-postgres/wello-resto-mysql-ddl.md` (dump réel de production) : aucune table `establishments`. Recherche `grep -in "establishment"` sur tout le dépôt (migrations comprises) : **aucun `CREATE TABLE establishments`**, aucune requête SQL sur une table de ce nom. Le mot « establishment » n'apparaît que comme mot anglais générique dans des commentaires de code et des messages d'erreur, jamais comme nom de table ou d'entité :

- `internal/models/responses_models.go:934,969,974` — messages d'erreur (« This establishment has no default role configured yet. », etc.)
- `internal/modules/roles/service.go:270,316,450,497` — commentaires
- `internal/modules/users/create_repository.go:31,51` — commentaires
- `internal/modules/pos/create_repository.go:164` — commentaire de doc sur `MerchantDefaultRoleID`
- `internal/modules/auth/login_response.go:15` — commentaire
- `cmd/assign_admin_role/main.go:4,16` — commentaires

Aucun module `internal/modules/establishment*` n'existe (liste complète des 45 modules vérifiée). Il s'agit dans tous les cas d'un synonyme informel de « merchant » utilisé dans la prose des commentaires, jamais d'une entité de base de données.

### 1.3. Notion de marque / enseigne / franchise

**Il existe bien une notion réelle de marque au niveau marchand**, distincte du champ homonyme sur `orders`.

**a) `brands` — table réelle, active** (`docs/migration-postgres/wello-resto-mysql-ddl.md:368-376`) :
```sql
CREATE TABLE `brands` (
  `brand_id` varchar(35) NOT NULL,
  `name` varchar(50) NOT NULL,
  `slug` varchar(50) NOT NULL,
  `logo_url` varchar(255) NOT NULL,
  `banner_url` varchar(255) NOT NULL,
  `description` varchar(255) NOT NULL,
  `creation_date` timestamp NOT NULL DEFAULT current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
`merchant.brand_id varchar(35) DEFAULT NULL` est le lien vers cette table — un marchand peut donc appartenir à une « enseigne »/chaîne regroupant plusieurs établissements.

Cette relation est **effectivement exploitée** par un endpoint public :
- Route : `cmd/api/routes.go:715` — `r.Get("/brands/{brand_slug}", scannHandler.GetBrand)` (dans `r.Route("/scannorder", ...)`, sans authentification)
- Handler : `internal/modules/scannorder/handler.go:131-152`
- Service : `internal/modules/scannorder/service.go:386-411` — `GetBrand` appelle `s.repo.GetMerchantsByBrandSlug(ctx, slug, lat, lng)`, qui liste tous les établissements (`merchant`) rattachés à une enseigne, avec filtrage géographique optionnel
- Repository : `internal/modules/scannorder/repository.go:792-919`, extrait :
```go
brandQuery := `
    SELECT brand_id, name, slug, logo_url, banner_url, description
    FROM brands
    WHERE slug = ?
    LIMIT 1`
...
merchantQuery := fmt.Sprintf(`
    SELECT
        m.id,
        m.fullName,
        m.address,
        m.lat,
        m.lng,
        m.timezone,
        m.logo_url,
        ...
    FROM ...
    INNER JOIN merchant m ON m.brand_id = b.brand_id
    ...
    WHERE b.brand_id = ?`, ...)
```

**Cependant, `merchant.brand_id` n'est jamais écrit par le code applicatif.** Recherche exhaustive de `brand_id` hors modules marketplace (Deliveroo/Uber Eats) : les seules occurrences sont en **lecture** (`internal/modules/scannorder/repository.go:797,846,850,881,885,915,919` et `internal/modules/scannorder/models.go:157`). Aucun `INSERT`/`UPDATE` ne touche `merchant.brand_id` ni la table `brands` dans tout `internal/`. Le rattachement d'un marchand à une enseigne est donc une donnée vivante et exploitée en lecture publique, mais alimentée uniquement manuellement en base (aucun endpoint API ne permet de le définir) — même pattern que documenté pour `stripe_accounts.terminal_location_id` (`migrations/done/054_stripe_accounts_terminal_location_id.up.sql:5-7` : « Aucun endpoint admin ne renseigne cette valeur : elle est insérée manuellement en base par le développeur »).

**b) `orders.brand` / `orders.brand_status` — homonyme sans rapport**, propre à la commande, pas au marchand :

`docs/migration-postgres/wello-resto-mysql-ddl.md:2021-2034` :
```sql
CREATE TABLE `orders` (
  `order_id` int(11) NOT NULL,
  ...
  `brand` varchar(20) NOT NULL DEFAULT 'WELLO_RESTO',
  `brand_order_id` varchar(50) DEFAULT NULL,
  `parent_order_id` varchar(50) DEFAULT NULL COMMENT 'Deliveroo : Previous brand_order_id before remake',
  `brand_order_num` varchar(10) DEFAULT NULL,
  `brand_status` varchar(30) NOT NULL,
  ...
```

Les constantes de `brand` sont définies dans `internal/models/request_objects.go:906-908` :
```go
BrandUberEats   = "UBER_EATS"
BrandDeliveroo  = "DELIVEROO"
BrandWelloResto = "WELLO_RESTO"
```

`brand` désigne ici le **canal/plateforme d'origine de la commande** (application native WelloResto vs marketplace Uber Eats vs Deliveroo), et `brand_status` le **statut de la commande dans le vocabulaire de ce canal** — pas un statut de « marque » marchand. Les valeurs observées confirment un statut de cycle de vie de commande, pas de marchand : `PENDING`, `DELIVERING`, `READY_FOR_COLLECTION`, `CANCELED`, `EN_ROUTE_TO_DROPOFF`, `DENIED`, `CLOSED`, `PENDING_CARD_PAYMENT`, `ONLINE_PAYMENT_PENDING`.

**Conclusion factuelle** : les deux notions coexistent sous des noms proches mais ne sont pas liées — `merchant.brand_id`/`brands` = enseigne/chaîne au niveau marchand (réel, lu par un endpoint public, jamais écrit par l'API) ; `orders.brand`/`orders.brand_status` = canal de vente et statut du cycle de vie d'une commande individuelle, y compris pour les commandes natives WelloResto (valeur par défaut `WELLO_RESTO`), sans rapport avec une notion de franchise.
### 1.4. Structure de `users`

`docs/migration-postgres/wello-resto-mysql-ddl.md:3264-3311` :
```sql
CREATE TABLE `users` (
  `user_id` varchar(50) NOT NULL,
  `merchant_id` int(11) DEFAULT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `first_name` varchar(40) NOT NULL COMMENT 'Prénom',
  `last_name` varchar(40) NOT NULL COMMENT 'Nom',
  `password` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `pin_code` varchar(6) DEFAULT NULL,
  `mfa_type` varchar(25) DEFAULT NULL,
  `mfa_status` varchar(25) DEFAULT NULL,
  `mfa_verified_at` timestamp NULL DEFAULT NULL,
  `mfa_otp_sent_at` timestamp NULL DEFAULT NULL,
  `mfa_secret` varchar(50) DEFAULT NULL,
  `userName` varchar(20) DEFAULT NULL,
  `email` varchar(255) NOT NULL,
  `email_verified_at` timestamp NULL DEFAULT NULL,
  `dob` date DEFAULT NULL COMMENT 'date of birth',
  `tel` varchar(20) DEFAULT NULL,
  `tel_verified_at` timestamp NULL DEFAULT NULL,
  `address` varchar(255) DEFAULT NULL,
  `street_number` varchar(20) DEFAULT NULL,
  `street` varchar(255) DEFAULT NULL,
  `city` varchar(255) DEFAULT NULL,
  `country` varchar(255) DEFAULT NULL,
  `zip_code` varchar(9) DEFAULT NULL,
  `lat` text DEFAULT NULL,
  `lng` text DEFAULT NULL,
  `heading` int(11) NOT NULL DEFAULT 0,
  `profile_picture` longtext DEFAULT NULL,
  `planning_color` varchar(11) NOT NULL DEFAULT '#28B2FC',
  `isReception` tinyint(1) NOT NULL DEFAULT 0,
  `isWaiter` tinyint(1) NOT NULL DEFAULT 0,
  `isDelivery` int(1) NOT NULL DEFAULT 0,
  `admin` tinyint(1) NOT NULL DEFAULT 0,
  `access_id` int(11) DEFAULT NULL,
  `waiter_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Waitrer',
  `reception_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Reception',
  `delivery_device_token` varchar(255) DEFAULT NULL COMMENT 'Device token of WR Delivery',
  `token` varchar(30) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `terms_of_use_accepted` tinyint(1) NOT NULL DEFAULT 0,
  `creationDate` datetime NOT NULL DEFAULT current_timestamp(),
  `created_at` timestamp NULL DEFAULT current_timestamp(),
  `lastAccess` datetime DEFAULT NULL COMMENT 'can be deleted (29/05/2026)',
  `last_activity` timestamp NOT NULL DEFAULT current_timestamp(),
  `enabled` int(11) NOT NULL DEFAULT 1,
  `last_login_at` timestamp NULL DEFAULT NULL,
  `last_position_at` datetime DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;
```

Clés (`docs/migration-postgres/wello-resto-mysql-ddl.md:4592-4594`) :
```sql
ALTER TABLE `users`
  ADD PRIMARY KEY (`user_id`),
  ADD UNIQUE KEY `name` (`name`);
```

**Il n'existe pas de contrainte `UNIQUE` sur `email`.** La seule contrainte d'unicité déclarée en base porte sur la colonne `name`. La colonne `email varchar(255) NOT NULL` n'a aucune contrainte d'unicité au niveau SQL.

**Aucun champ d'authentification externe** (`google_id`, `oauth_provider`, `external_id`, ou équivalent) n'existe sur cette table. Les seuls mécanismes d'auth présents sont : mot de passe (`password`), PIN (`pin_code`), et MFA interne (`mfa_type`, `mfa_status`, `mfa_secret`, `mfa_verified_at`, `mfa_otp_sent_at`) — pas de fédération d'identité tierce (pas de Google/Apple/OAuth Sign-In).

### 1.5. Structure de `users_rights`

`docs/migration-postgres/wello-resto-mysql-ddl.md:3349-3394` :
```sql
CREATE TABLE `users_rights` (
  `id` int(11) NOT NULL,
  `user_id` varchar(64) DEFAULT NULL,
  `merchant_id` int(11) NOT NULL,
  `token` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrwaiter` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrreception` tinyint(1) NOT NULL DEFAULT 1,
  `access_wrdelivery` tinyint(1) NOT NULL DEFAULT 1,
  `position_id` varchar(64) DEFAULT NULL,
  `position_note` text DEFAULT NULL,
  `job_title` varchar(150) DEFAULT NULL,
  `role` varchar(32) NOT NULL DEFAULT 'employee',
  `contract_type_code` varchar(32) DEFAULT NULL,
  `contract_start_date` date DEFAULT NULL,
  `contract_end_date` date DEFAULT NULL,
  `probation_end_date` date DEFAULT NULL,
  `last_medical_checkup_date` date DEFAULT NULL,
  `contract_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `max_weekly_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `required_rest_days` int(11) NOT NULL DEFAULT 2,
  `sunday_premium` tinyint(1) NOT NULL DEFAULT 0,
  `night_premium` tinyint(1) NOT NULL DEFAULT 0,
  `hourly_rate` bigint(20) NOT NULL DEFAULT 0,
  `gross_monthly_salary` bigint(20) NOT NULL DEFAULT 0,
  `employer_charges_pct` decimal(5,2) NOT NULL DEFAULT 45.00,
  `transport_cost` bigint(20) NOT NULL DEFAULT 0,
  `hr_comment` text DEFAULT NULL,
  `manage_menu` tinyint(1) NOT NULL DEFAULT 0,
  `manage_plannings` tinyint(1) NOT NULL DEFAULT 0,
  `manage_users` tinyint(1) NOT NULL DEFAULT 0,
  `manage_settings` tinyint(1) NOT NULL DEFAULT 0,
  `manage_haccp` tinyint(1) NOT NULL DEFAULT 0,
  `view_reports` tinyint(1) NOT NULL DEFAULT 0,
  `export_reports` tinyint(1) NOT NULL DEFAULT 0,
  `view_financials` tinyint(1) NOT NULL DEFAULT 0,
  `export_financials` tinyint(1) NOT NULL DEFAULT 0,
  `manage_customers` tinyint(1) NOT NULL DEFAULT 0,
  `export_customers` tinyint(1) NOT NULL DEFAULT 0,
  `admin` tinyint(1) NOT NULL DEFAULT 0,
  `print_merchant_cash_report` tinyint(1) NOT NULL DEFAULT 0,
  `open_cash_drawer` tinyint(1) NOT NULL DEFAULT 0,
  `last_login_at` timestamp NOT NULL DEFAULT current_timestamp(),
  `login_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `pin_hash` varchar(64) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
```

Clé primaire (`docs/migration-postgres/wello-resto-mysql-ddl.md:4612-4613`) :
```sql
ALTER TABLE `users_rights`
  ADD PRIMARY KEY (`id`);
```
**Aucune autre contrainte d'unicité** (ni sur `user_id` seul, ni sur `(user_id, merchant_id)`) n'apparaît dans ce dump.

**Clé de rattachement au marchand** : `merchant_id int(11) NOT NULL` — c'est une table de jointure classique `users_rights(id PK, user_id, merchant_id)`, avec `user_id` et `merchant_id` en colonnes simples (pas de clé composite), permettant plusieurs lignes par `user_id` (voir §1.7).

**Stockage des permissions** : deux mécanismes coexistent, en transition :
- **Colonnes booléennes historiques** (celles réellement lues aujourd'hui pour autoriser une requête, selon le commentaire de `migrations/done/094_roles_schema.up.sql:16-19` : « the users_rights.manage_* / admin boolean columns remain the only thing the API actually reads to authorize a request ») : `access_wrwaiter`, `access_wrreception`, `access_wrdelivery`, `manage_menu`, `manage_plannings`, `manage_users`, `manage_settings`, `manage_haccp`, `view_reports`, `export_reports`, `view_financials`, `export_financials`, `manage_customers`, `export_customers`, `admin`, `print_merchant_cash_report`, `open_cash_drawer`. Il n'y a **ni colonne JSON ni bitmask** — chaque permission est une colonne `tinyint(1)` dédiée.
- **`role_id`**, ajouté par `migrations/done/094_roles_schema.up.sql:103-104` (RBAC lot 1) :
```sql
ALTER TABLE users_rights ADD COLUMN role_id varchar(64) REFERENCES roles(id);
CREATE INDEX idx_users_rights_role_id ON users_rights (role_id);
```
pointant vers une nouvelle table de jointure `role_permissions` (many-to-many rôle↔permission, `migrations/done/094_roles_schema.up.sql:91-95`). Cette bascule n'est, à ce jour, appliquée qu'en recette Postgres (voir Note méthodologique) — **pas** sur MySQL production.

**Gestion du PIN** : deux colonnes différentes, à des niveaux différents, aucune en clair :
- `users.pin_code varchar(6)` — colonne historique sur `users`, non accompagnée de commentaire de hachage dans le dump.
- `users_rights.pin_hash varchar(64) DEFAULT NULL` — colonne dédiée au PIN **hashé**, ajoutée par `migrations/done/031_add_pin_hash_to_users_rights.up.sql` :
```sql
-- PIN authentication: store HMAC-SHA256 hash of the PIN on the user-merchant link.
-- NULL means the link has no PIN set; the unique index allows multiple NULLs (MySQL semantics).
ALTER TABLE users_rights ADD COLUMN pin_hash VARCHAR(64) NULL DEFAULT NULL;

-- Unique per (merchant, pin_hash) so two employees of the same merchant cannot share a PIN.
-- MySQL treats NULL as distinct in unique indexes, so multiple links without a PIN are allowed.
CREATE UNIQUE INDEX idx_users_rights_merchant_pin ON users_rights (merchant_id, pin_hash);
```
Le commentaire précise explicitement le mécanisme de hachage : HMAC-SHA256. Fait à noter : **la colonne `pin_hash` est bien présente dans le dump réel du 2026-07-13, mais l'index unique `idx_users_rights_merchant_pin` décrit dans la même migration n'apparaît nulle part dans la section index de ce même dump** (`docs/migration-postgres/wello-resto-mysql-ddl.md:4610-4613` ne liste que `ADD PRIMARY KEY (id)`). Écriture confirmée en code : `internal/modules/auth/repository.go:771-777` (`SetPINHash`) et lecture de conflit `internal/modules/auth/repository.go:779-789` (`CheckPINConflict`).

### 1.6. Structure de `employees` et son lien vers `users`

`docs/migration-postgres/wello-resto-mysql-ddl.md:1062-1099` :
```sql
CREATE TABLE `employees` (
  `id` varchar(64) NOT NULL,
  `merchant_id` varchar(64) NOT NULL,
  `user_id` varchar(64) DEFAULT NULL,
  `member_id` bigint(20) DEFAULT NULL,
  `first_name` varchar(150) NOT NULL,
  `last_name` varchar(150) NOT NULL,
  `position_id` varchar(64) NOT NULL,
  `position_note` text DEFAULT NULL,
  `job_title` varchar(150) DEFAULT NULL,
  `email` varchar(255) DEFAULT NULL,
  `phone` varchar(64) DEFAULT NULL,
  `role` enum('employee','manager','admin') NOT NULL DEFAULT 'employee',
  `contract_type_code` varchar(32) NOT NULL,
  `contract_start_date` date DEFAULT NULL,
  `contract_end_date` date DEFAULT NULL,
  `probation_end_date` date DEFAULT NULL,
  `last_medical_checkup_date` date DEFAULT NULL,
  `contract_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `max_weekly_hours` decimal(5,2) NOT NULL DEFAULT 35.00,
  `required_rest_days` int(11) NOT NULL DEFAULT 2,
  `sunday_premium` tinyint(1) NOT NULL DEFAULT 0,
  `night_premium` tinyint(1) NOT NULL DEFAULT 0,
  `hourly_rate` bigint(20) NOT NULL DEFAULT 0,
  `gross_monthly_salary` bigint(20) NOT NULL DEFAULT 0,
  `employer_charges_pct` decimal(5,2) NOT NULL DEFAULT 45.00,
  `transport_cost` bigint(20) NOT NULL DEFAULT 0,
  `birth_date` date DEFAULT NULL,
  `gender` varchar(32) DEFAULT NULL,
  `nationality` varchar(80) DEFAULT NULL,
  `address` varchar(255) DEFAULT NULL,
  `hr_comment` text DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT 1,
  `enabled` tinyint(1) NOT NULL DEFAULT 1,
  `deleted_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT current_timestamp(),
  `updated_at` datetime NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp()
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```
Index (`docs/migration-postgres/wello-resto-mysql-ddl.md:3840-3848`) :
```sql
ALTER TABLE `employees`
  ADD PRIMARY KEY (`id`),
  ADD UNIQUE KEY `uq_employees_merchant_user` (`merchant_id`,`user_id`),
  ADD UNIQUE KEY `uq_employees_merchant_member` (`merchant_id`,`member_id`),
  ADD KEY `idx_employees_merchant_active` (`merchant_id`,`active`),
  ADD KEY `idx_employees_merchant` (`merchant_id`),
  ADD KEY `idx_employees_contract_type` (`contract_type_code`),
  ADD KEY `idx_employees_position_id` (`position_id`),
  ADD KEY `idx_employees_member_id` (`member_id`);
```

**État actuel confirmé** : `employees.user_id` est **`VARCHAR(64) NULL` (nullable)**, et **aucune contrainte `FOREIGN KEY` n'existe** sur cette colonne dans le schéma réel — ni dans le dump de production (aucun `CONSTRAINT ... FOREIGN KEY (user_id)` trouvé sur toute la table dans `wello-resto-mysql-ddl.md`), ni dans le fichier de migration actuellement committé `migrations/done/014_planning_socle.sql:115-155` (qui ne contient qu'une `UNIQUE KEY uq_employees_merchant_user (merchant_id, user_id)`, aucun `CONSTRAINT ... FOREIGN KEY`).

**Historique reconstitué via `git log -p` sur ce fichier** :

| Commit | Date | Contenu pertinent |
|---|---|---|
| `9c5c989` « feature: Plannin + fix components edit » | 2026-05-27 | Création initiale de `migrations/014_planning_socle.sql` avec `user_id VARCHAR(64) NULL` **et** `CONSTRAINT fk_employees_user FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE SET NULL` — nullable **et** contrainte FK dès l'origine |
| `5fdfb85` « feature: corrective actions » | 2026-05-28 | Suppression de la contrainte FK, `user_id` reste `NULL` mais n'est plus contraint par une FK |
| `940be00` « feature: delivery sessions » | 2026-06-12 | Déplacement du fichier vers `migrations/done/014_planning_socle.sql` (convention du dépôt marquant « exécuté en MySQL réel ») — déjà sans la FK à ce stade |

Diff exact de la suppression (`git show 5fdfb85 -- migrations/014_planning_socle.sql`) :
```diff
   KEY idx_employees_merchant_active (merchant_id, active),
   KEY idx_employees_merchant (merchant_id),
   KEY idx_employees_contract_type (contract_type_code),
-  KEY idx_employees_time_tracking (time_tracking_mode_code),
-  CONSTRAINT fk_employees_contract_type FOREIGN KEY (contract_type_code)
-    REFERENCES sys_contract_types(code),
-  CONSTRAINT fk_employees_time_tracking FOREIGN KEY (time_tracking_mode_code)
-    REFERENCES sys_time_tracking_modes(code),
-  CONSTRAINT fk_employees_user FOREIGN KEY (user_id)
-    REFERENCES users(user_id) ON DELETE SET NULL
+  KEY idx_employees_time_tracking (time_tracking_mode_code)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Conclusion factuelle** : la colonne `employees.user_id` a été **nullable depuis sa toute première version** (jamais `NOT NULL`). Une contrainte `FOREIGN KEY ... ON DELETE SET NULL` vers `users(user_id)` a existé brièvement dans le fichier de migration entre le 2026-05-27 et le 2026-05-28, avant d'être retirée — et cette suppression a eu lieu **avant** que le fichier ne soit déplacé vers `migrations/done/` (2026-06-12), donc avant toute exécution documentée contre une base réelle. L'état actuel, confirmé à la fois par le fichier de migration committé et par le dump de production, est : colonne nullable, **sans aucune contrainte FK au niveau base de données** (le comportement d'intégrité référentielle, s'il existe, est géré uniquement côté application, pas par le SGBD).

### 1.7. Un utilisateur peut-il être rattaché à plusieurs marchands ?

**Oui, démontré par le schéma et par le code.**

**Schéma** : `users_rights` (voir §1.5) est une table de jointure classique — `id` (PK auto-increment), `user_id`, `merchant_id` — **sans aucune contrainte d'unicité sur `user_id` seul, ni même sur le couple `(user_id, merchant_id)`** (seule `PRIMARY KEY (id)`). Rien n'empêche donc plusieurs lignes `users_rights` portant le même `user_id` avec des `merchant_id` différents — c'est la structure même qui permet le multi-marchand, à l'opposé d'un `merchant_id` unique directement sur `users` (qui existe bien comme colonne `users.merchant_id int(11) DEFAULT NULL`, mais qui n'est visiblement pas ce qui porte le rattachement multiple — c'est `users_rights` qui le fait).

**Code réel** — la requête est explicitement commentée « MULTI-MERCHANT » dans le code de login :

`internal/modules/auth/repository.go:791-823` :
```go
func (r *AuthRepository) GetMerchants(ctx context.Context, userID string) ([]MerchantRow, error) {
	db := dbx.GetDB(ctx, r.database)
	query := fmt.Sprintf(`
SELECT
    m.id,
    m.fullName,
    m.lat,
    m.lng,
    CONCAT(m.street_number,' ',m.street,', ',m.zip_code,' ',m.city,', ',m.country),
    m.city,
    m.country,
    m.zip_code,
	m.logo_url,
    ur.token
FROM merchant m
INNER JOIN users_rights ur ON ur.merchant_id = %s
WHERE ur.user_id IS NOT NULL AND ur.user_id = ?
`, authMerchantJoinCast())
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var list []MerchantRow
	for rows.Next() {
		var m MerchantRow
		rows.Scan(&m.MerchantID, &m.BusinessName, &m.Lat, &m.Lng, &m.Address, &m.City, &m.Country, &m.ZipCode, &m.LogoURL, &m.Token)
		list = append(list, m)
	}
	return list, nil
}
```

Cette requête retourne bien une **liste** (`[]MerchantRow`) de tous les marchands liés à un `userID` donné, et non un marchand unique. Elle est appelée dans le flux de login réel, `internal/modules/auth/service.go:374-377` :
```go
	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	return buildLoginResponse(user, merchants), nil
```
(le second appel identique, `internal/modules/auth/service.go:418`, se trouve dans une fonction `LoginOld` entièrement commentée, donc du code mort — seul l'appel de la ligne 375 est actif.) La réponse de login inclut donc, pour un utilisateur donné, la liste complète des marchands auxquels il est rattaché via `users_rights`.
---

## 2. Authentification

### 2.1. Flux complet de `/auth/login`

**Constat préalable important** : le système **n'utilise pas de JWT**. Le mot « token » désigne un jeton opaque aléatoire (hex, généré par `crypto/rand`), stocké en clair côté serveur dans la colonne `users_rights.token` (VARCHAR(255)) et vérifié par une requête SQL directe (`WHERE ur.token = ?`), avec un cache Redis en lecture. Il n'y a aucune signature cryptographique, donc aucun « secret de signature » au sens JWT n'existe dans le code.

**Route** — `cmd/api/routes.go:575-576` :
```go
r.Get("/login", authH.Login)
r.Post("/login", authH.Login)
```
Wiring du service (`cmd/api/routes.go:197-199`) :
```go
authRepo := authModule.NewAuthRepository(selectedDB)
authService := authModule.NewAuthService(authRepo, redisClient, mailService, smsService, cfg.App.PINPepper, cfg.Auth.PasswordResetBaseURL)
authMiddleware := middleware.Auth(&authService)
```

**Handler** — `internal/modules/auth/handler.go:21-47` :
```go
// Login handler - Can be used with user and pwd, with token in get, or token in authorization
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	token := helpers.ExtractToken(r)

	var req LoginRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && token == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "login", map[string]string{"error": "invalid_request"})
		return
	}

	// Détection du backoffice (ex: via un header envoyé par le front web)
	isBackoffice := r.Header.Get("X-App-Source") == "backoffice"

	// On passe isBackoffice au service
	resp, err := h.svc.Login(r.Context(), req, token, isBackoffice)
	if err != nil {
		models.SendErrorJSON(w, "auth", "login", err)
		return
	}

	// Si le MFA est requis, on renvoie un code 202 Accepted au lieu de 200 OK
	if resp.Status == "MFA_REQUIRED" {
		models.SendJSON(w, http.StatusAccepted, "auth", "login", resp)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "login", resp)
}
```

`helpers.ExtractToken` — `internal/helpers/handler_helpers.go:11-26` :
```go
func ExtractToken(r *http.Request) string {
	// Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// allow "Bearer <token>" or raw token
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			return strings.TrimSpace(auth[7:])
		}
		return strings.TrimSpace(auth)
	}
	// fallback to query param token (legacy)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}
```

**Service `Login()`** — `internal/modules/auth/service.go:308-378` :
```go
func (s *AuthService) Login(ctx context.Context, payload LoginRequestPayload, token string, isBackoffice bool) (*LoginResponse, error) {
	username := payload.Username + payload.Email

	user, err := s.repo.Login(ctx, username, payload.Password, token)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, models.ErrInvalidToken
	}

	if !user.Enabled {
		return newLoginStatusResponse("account_disabled", "false"), nil
	}

	// ==============================================================
	// LOGIQUE MFA (Uniquement si Backoffice ET MFA activé)
	// ==============================================================
	if s.IsMFAVerificationRequired(ctx, user) {

		if isBackoffice {

			// 3. Valider la session en base de données
			err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending)
			if err != nil {
				logger.FromContext(ctx).Error("Erreur lors de la mise à jour du statut MFA: " + err.Error())
				return nil, errors.New("erreur interne lors de la validation")
			}

			if s.canSendMFAOTP(ctx, user) {
				s.SendMFACode(ctx, user, false)
			}

			pendingStatus := models.MFAStatusPending
			user.MFAStatus = &pendingStatus
		}
	} else {
		if err := s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusVerified); err != nil {
			return nil, err
		}
		verifiedStatus := models.MFAStatusVerified
		user.MFAStatus = &verifiedStatus
		if err := s.repo.MarkLastLoginAt(ctx, user.UserID); err != nil {
			return nil, err
		}
	}

	// MULTI-MERCHANT
	merchants, _ := s.repo.GetMerchants(ctx, user.UserID)

	return buildLoginResponse(user, merchants), nil
}
```

**Repository `Login()`** authentifie par username/email/mot de passe OU directement par token existant (`internal/modules/auth/repository.go:341-347`) :
```sql
WHERE
    (
        (UPPER(u.name)=UPPER(?) AND u.name <> '' AND u.name IS NOT NULL)
        OR (UPPER(u.email)=UPPER(?) AND u.email <> '' AND u.email IS NOT NULL)
        OR ur.token = ?
    )
LIMIT 1;
```
Vérification du mot de passe (bcrypt, avec migration automatique de hash legacy) — `internal/modules/auth/repository.go:419-433` :
```go
	loggedByToken := token != "" && token == data.Token
	if !loggedByToken {
		if !helpers.PasswordMatches(plainPwd, data.Password) {
			return nil, models.ErrUserNotFound
		}

		// Migration automatique vers bcrypt pour les mots de passe legacy
		if !strings.HasPrefix(data.Password, "$2") {
			if newHash, err := helpers.HashPassword(plainPwd); err == nil {
				if err := r.UpdatePassword(ctx, data.UserID, newHash); err == nil {
					data.Password = newHash
				}
			}
		}
	}
```

**Aucune génération de token n'a lieu au login** : le token retourné (`data.Token`) est celui déjà stocké en base (`ur.token`), créé une seule fois à la création du compte/du lien merchant. Le login ne fait donc que le lire et le renvoyer au client.

**« Claims » du JWT** : n'existe pas, car il n'y a pas de JWT. L'identité, le rôle et les permissions sont recalculés côté serveur à chaque requête via `GetUserByToken` (jointure SQL `users` + `users_rights` + `roles` + `merchant` + `merchant_parameters` + `subscriptions`/`packages`, `internal/modules/auth/repository.go:42-169`), pas décodés depuis le token.

**Durée de validité du token** : **pas d'expiration** portée par le token lui-même. La table `users_rights` (`token varchar(255) NOT NULL`) ne comporte aucune colonne d'expiration. Le token reste valide indéfiniment jusqu'à rotation explicite (uniquement via `RotateRightsTokensForUser`, appelée uniquement lors d'un reset de mot de passe — voir §2.2). Le cache Redis associé a un TTL de 60 minutes (`internal/models/redis_models.go:10`, `UserCacheTTL = 60 * time.Minute`), mais ce TTL ne concerne que le **cache** : à son expiration, `GetUserByToken` retombe simplement sur la requête SQL et retrouve le même token toujours valide.

**Secret de signature** : n'existe pas. Le token est un identifiant aléatoire cryptographiquement fort :
```go
// internal/helpers/ids.go:75-83
func GenerateToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generateToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}
```
Généré à la création du compte (`internal/modules/users/create_service.go:37`, `helpers.GenerateToken(30)`) ou du rattachement à un établissement (`internal/modules/pos/create_service.go:21,102`). La sécurité repose sur l'entropie du token et sa comparaison exacte en base, pas sur une vérification HMAC/RSA.

Note : seul module utilisant réellement des JWT dans le repo est `internal/modules/notification/token_manager.go` (génération d'un JWT pour s'authentifier auprès de l'API Google FCM via un compte de service), **sans aucun rapport avec l'authentification des utilisateurs de la plateforme**.

### 2.2. Gestion du refresh token

**Il n'existe pas de mécanisme de refresh token pour l'authentification utilisateur** (login classique, PIN). Le token de session (`users_rights.token`) est stocké en base MySQL/Postgres (Redis ne fait que le mettre en cache), et n'a **pas de rotation à chaque usage** — il est réutilisable indéfiniment jusqu'à un événement explicite.

Le seul point de rotation identifié est **la réinitialisation de mot de passe**, qui fait pivoter le token de session pour déconnecter toutes les sessions de l'utilisateur — `internal/modules/auth/repository.go:593-644` :
```go
// RotateRightsTokensForUser issues a fresh session token for every merchant
// link of a user and returns the tokens it replaced.
//
// This is what actually signs the user out everywhere. Deleting the Redis
// entries is not enough: GetUserByToken falls back to `WHERE ur.token = ?` in
// the database, so a cache eviction is silently repaired by the next request.
// Callers should purge the returned tokens from Redis afterwards.
func (r *AuthRepository) RotateRightsTokensForUser(ctx context.Context, userID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `SELECT id, token FROM users_rights WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}

	type rightsRow struct {
		id    string
		token string
	}

	var links []rightsRow
	for rows.Next() {
		var link rightsRow
		if err := rows.Scan(&link.id, &link.token); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	oldTokens := make([]string, 0, len(links))
	for _, link := range links {
		newToken, err := helpers.GenerateToken(32)
		if err != nil {
			return oldTokens, err
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE users_rights SET token = ? WHERE id = ?`, newToken, link.id); err != nil {
			return oldTokens, err
		}
		if strings.TrimSpace(link.token) != "" {
			oldTokens = append(oldTokens, link.token)
		}
	}

	return oldTokens, nil
}
```
Appelée depuis `ConfirmPasswordReset` (`internal/modules/auth/service.go:998`), qui purge ensuite les anciennes entrées du cache Redis.

**Note distincte** : un vrai système de refresh token *avec rotation à chaque usage* existe, mais **uniquement pour les bornes de commande (kiosk)** — module séparé, sans rapport avec l'auth des employés/utilisateurs : `internal/modules/kiosk/service.go` (`RefreshDeviceToken`, ligne 205-256), TTL configurable via `KIOSK_DEVICE_TOKEN_TTL_DAYS` (défaut 30 jours). Ce mécanisme n'est pas branché sur `/auth/login`.

### 2.3. Code intégral de `authMiddleware`

`internal/middleware/auth.go:29-122` :
```go
// Auth est le middleware d'authentification principal
// Il vérifie le token, récupère le user (via Redis), et l'injecte dans le contexte
func Auth(service AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Laisser passer les requêtes OPTIONS (preflight CORS)
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Extraire le header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token manquant"}`, http.StatusUnauthorized)
				return
			}

			// 2. Logique hybride : On nettoie et on extrait
			token := authHeader

			// Si ça commence par "Bearer " (insensible à la casse)
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}

			token = strings.TrimSpace(token)

			// Sécurité : on vérifie que le token n'est pas devenu vide après le nettoyage
			if token == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"format token invalide"}`, http.StatusUnauthorized)
				return
			}

			// 3. Récupérer le user
			user, err := service.GetUserByToken(r.Context(), token)
			if err != nil || user == nil {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token invalide ou expiré"}`, http.StatusUnauthorized)
				return
			}

			// --- LOGIQUE MFA ---
			isBackoffice := r.Header.Get("X-App-Source") == "backoffice"

			if isBackoffice && service.IsMFAVerificationRequired(r.Context(), user) {
				// On laisse passer UNIQUEMENT vers l'endpoint de vérification MFA
				if r.URL.Path != "/auth/verify" {
					service.UpdateMFAStatus(r.Context(), user.UserID, models.MFAStatusPending)
					SetCORSHeaders(w, r)
					var recipient string
					if user.MFAType != nil && *user.MFAType == "email_sms" {
						recipient = helpers.MaskEmail(user.Email)
					}
					models.SendJSON(w, http.StatusUnauthorized, "auth", "login", map[string]string{
						"status":    "mfa_required",
						"message":   "MFA required, please try login",
						"error":     "MFA required, please try login",
						"recipient": recipient,
					})
					return
				}
			}

			// 4. Injecter le user
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

### 2.4. Code intégral de `RequirePermission` (et `AnyOf`/`AllOf`)

`internal/middleware/require_permission.go:70-98` :
```go
func RequirePermission(key permission.Key) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				SetCORSHeaders(w, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			granted := user.Has(key)
			observeDecision(r, user, key, granted)

			if !granted {
				renderError(w, r, "access_denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```
`RequireAdmin` (variante réservée aux admins, distincte de `RequirePermission`) — `internal/middleware/require_permission.go:107-134` :
```go
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user := GetUser(r)
			if user == nil {
				SetCORSHeaders(w, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			granted := IsAdmin(user)
			observeDecision(r, user, adminObservationKey, granted)

			if !granted {
				renderError(w, r, "access_denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

**`AnyOf`/`AllOf` : n'existent pas dans le code actuel.** Ils ont été supprimés (commentaire explicite `internal/middleware/permissions.go:7-18`) :
```go
// ============================================================
// RBAC lot 2 — bascule des prédicats
//
// Toutes les fonctions HasXxx/CanXxx ainsi que les combinateurs AnyOf/AllOf
// ont été retirées : RequirePermission prend désormais directement une
// permission.Key et appelle user.Has(key), qui encapsule la correspondance
// avec les anciennes colonnes booléennes (voir internal/modules/auth/
// permissions.go). Elles ont été supprimées plutôt que dépréciées car aucune
// n'avait plus d'appelant réel dans cmd/api/routes.go au moment de la bascule
// (vérifié : seules HasMenuAccess, HasPlanningAccess, HasUserManagementAccess,
// HasSettingsAccess, HasHACCPAccess, HasCustomerManagementAccess et IsAdmin
// étaient effectivement câblées sur une route).
// ============================================================
```
Une recherche `grep -rn "AnyOf|AllOf" --include="*.go"` sur tout le repo ne remonte que ces deux commentaires historiques — aucun appel réel.

### 2.5. Extraction et propagation du `merchant_id`

Le `merchant_id` n'est **pas** extrait isolément (ni d'un header dédié, ni d'un paramètre d'URL séparé, ni d'un claim JWT). Il est un champ (`MerchantID`) de la structure `*auth.UserLoginRow` récupérée par `GetUserByToken` à partir du token, et c'est **cette structure utilisateur entière** qui est injectée dans le contexte.

**Injection dans le contexte** — `internal/middleware/auth.go:118` :
```go
ctx := context.WithValue(r.Context(), userContextKey, user)
next.ServeHTTP(w, r.WithContext(ctx))
```
avec la clé typée (`internal/middleware/auth.go:15-17`) :
```go
type contextKey string

const userContextKey contextKey = "authenticatedUser"
```

**Relecture depuis le contexte** — `internal/middleware/auth.go:124-156` :
```go
// GetUser récupère le user injecté par le middleware depuis le contexte
func GetUser(r *http.Request) *auth.UserLoginRow {
	user, _ := r.Context().Value(userContextKey).(*auth.UserLoginRow)
	return user
}

// UserFromContext récupère le user du contexte avec gestion d'erreur
func UserFromContext(ctx context.Context) (*auth.UserLoginRow, error) {
	user, ok := ctx.Value(userContextKey).(*auth.UserLoginRow)
	if !ok || user == nil {
		return nil, ErrUnunauthenticated
	}
	return user, nil
}

// WithUser injecte un utilisateur authentifié dans le contexte
func WithUser(ctx context.Context, user *auth.UserLoginRow) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// MustGetUser récupère le user et envoie une erreur HTTP si absent
func MustGetUser(w http.ResponseWriter, r *http.Request) (*auth.UserLoginRow, bool) {
	user := GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"utilisateur non authentifié"}`, http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}
```
Aucun helper `GetMerchantIDFromContext` n'existe. Le pattern systématique observé dans les services est `user, err := middleware.UserFromContext(ctx)` puis `user.MerchantID`, passé ensuite explicitement comme paramètre SQL pour le filtrage multi-tenant.

### 2.6. `/auth/pin`

**Route** — `cmd/api/routes.go:585` :
```go
r.With(authMiddleware).Post("/pin", authH.AuthPIN)
```
Exige donc un token d'ancrage valide (session déjà ouverte, n'importe quel utilisateur du même établissement — typiquement le POS déjà connecté).

**Handler** — `internal/modules/auth/handler.go:181-213` :
```go
// AuthPIN authenticates an employee by PIN.
// Authorization: anchor token (existing session of any user on the same merchant).
// Body: { "pin": "1234" }
// Response: identical to /auth/login, with the permanent token of the matched employee.
func (h *AuthHandler) AuthPIN(w http.ResponseWriter, r *http.Request) {
	anchorToken := helpers.ExtractToken(r)
	if anchorToken == "" {
		models.SendJSON(w, http.StatusUnauthorized, "auth", "pin", map[string]string{"error": "missing_token"})
		return
	}

	var req PINAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.PIN) == "" {
		models.SendJSON(w, http.StatusBadRequest, "auth", "pin", map[string]string{"error": "invalid_request"})
		return
	}

	resp, err := h.svc.AuthenticatePIN(r.Context(), anchorToken, req.PIN)
	if err != nil {
		var lockoutErr *PINLockoutError
		if errors.As(err, &lockoutErr) {
			models.SendJSON(w, http.StatusTooManyRequests, "auth", "pin", map[string]interface{}{
				"error":         "pin_locked",
				"delay_seconds": lockoutErr.DelaySeconds,
			})
			return
		}
		models.SendErrorJSON(w, "auth", "pin", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "pin", resp)
}
```

**Service `AuthenticatePIN`** — `internal/modules/auth/service.go:115-145` — **délègue explicitement à `Login()`** en interne :
```go
// AuthenticatePIN validates a PIN against the merchant of the anchor token,
// then delegates to Login with the employee's permanent token.
// The response is identical to /auth/login by construction.
func (s *AuthService) AuthenticatePIN(ctx context.Context, anchorToken, pin string) (*LoginResponse, error) {
	anchor, err := s.GetUserByToken(ctx, anchorToken)
	if err != nil {
		return nil, err
	}
	if anchor == nil {
		return nil, models.ErrInvalidToken
	}

	if delay := s.checkLockout(ctx, anchorToken); delay > 0 {
		return nil, &PINLockoutError{DelaySeconds: int(delay.Seconds())}
	}

	pinHash := security.HashPIN(pin, s.pepper)
	employee, err := s.repo.GetUserByPIN(ctx, anchor.MerchantID, pinHash)
	if err != nil {
		return nil, err
	}
	if employee == nil {
		s.incrementLockout(ctx, anchorToken)
		return nil, models.ErrUserNotFound
	}

	s.resetLockout(ctx, anchorToken)
	// Login finds the employee by token (loggedByToken path — no password check).
	// isBackoffice=false: MFA trigger skipped; MarkLastLoginAt runs in the non-MFA else branch.
	return s.Login(ctx, LoginRequestPayload{}, employee.Token, false)
}
```
Le PIN est haché avec un « pepper » (variable d'env `PIN_PEPPER`), comparé en base via `GetUserByPIN` scopé au `merchant_id` de l'ancre (jamais inter-tenant). Un lockout exponentiel existe (5 tentatives max, base 30s, doublement jusqu'à 480s).

### 2.7. Flux « mot de passe oublié »

**Ce flux existe.** Deux routes publiques (aucun token requis) — `cmd/api/routes.go:581-583` :
```go
// Public: the caller has lost their password, so no token can be required.
r.Post("/forgot-password", authH.ForgotPassword)
r.Post("/reset-password", authH.ResetPassword)
```

**Étape 1 — Demande de lien**, réponse identique quel que soit le résultat réel pour empêcher l'énumération de comptes :

`internal/modules/auth/handler.go:304-325` :
```go
// ForgotPassword handles POST /auth/forgot-password (public).
//
// Always answers 200 with the same body, whatever happened: unknown account,
// throttled, disabled, or a link actually sent. Any observable difference would
// turn this endpoint into an account-enumeration oracle.
func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "auth", "forgot_password", map[string]string{"error": "invalid_request"})
		return
	}

	if err := h.svc.SendPasswordResetLink(r.Context(), req.Login, helpers.ClientIP(r)); err != nil {
		models.SendErrorJSON(w, "auth", "forgot_password", err)
		return
	}

	models.SendJSON(w, http.StatusOK, "auth", "forgot_password", map[string]string{
		"status":  "success",
		"message": "Si un compte correspond, un email de réinitialisation a été envoyé.",
	})
}
```

Constantes (`internal/modules/auth/models.go:20-29`) : `PasswordResetTTL = 30 * time.Minute`, `PasswordResetTokenBytes = 32` (64 caractères hex), `PasswordResetMaxPerHour = 5` (limite par compte, vérifiée en SQL). Throttle additionnel par IP (`PasswordResetIPThrottleMax = 20`/heure). Seul le hash SHA-256 du token est persisté en base ; le token en clair n'est jamais stocké ni loggé.

**Étape 2 — Réinitialisation** — `internal/modules/auth/service.go:968-1015` : le nouveau mot de passe est validé **avant** de consommer le token (pour ne pas brûler un lien à usage unique en cas de mot de passe rejeté), puis toutes les sessions de l'utilisateur sont invalidées via `RotateRightsTokensForUser` (§2.2). L'URL de base du lien est chargée depuis `PASSWORD_RESET_BASE_URL` ; si non configurée, le token est émis mais aucun email n'est envoyé (log d'erreur uniquement).

### 2.8. Vérification d'adresse e-mail

**Ce flux existe, mais n'est pas automatique à la création du compte.** C'est un endpoint générique et authentifié, distinct du MFA, déclenché manuellement.

Routes — `cmd/api/routes.go:578-579` :
```go
r.Post("/send-verification", authH.SendVerification)
r.Post("/verify", authH.VerifyCode)
```
`SendVerificationCode` (`internal/modules/auth/service.go:838-872`) génère un OTP à 6 chiffres (TTL 5 min via Redis), envoyé par email ou SMS. `MarkAsVerified` (`internal/modules/auth/repository.go:975-1006`) met à jour `users.email_verified_at`/`tel_verified_at`.

**Constat important** : aucun appel à `SendVerificationCode` n'a été trouvé dans les flux de création de compte (`internal/modules/users/create_service.go:15-78`, `internal/modules/pos/create_service.go:13-74`) : la vérification d'email n'est **pas déclenchée automatiquement à l'inscription**, seulement disponible en libre-service une fois authentifié. Par ailleurs, la création d'un utilisateur (`POST /users`) exige déjà elle-même une permission `StaffManage` — ce n'est pas un endpoint de self-registration public.

Une décision de retrait est documentée : la vérification d'email/téléphone comme **condition d'autorisation** (gate RBAC) a été retirée du code (commentaire « RBAC lot 2.5 : IsEmailVerified et IsTelVerified... ont été retirées »), tout en conservant les colonnes et le flux de vérification lui-même.

### 2.9. Authentification par fournisseur externe (Google Sign-In, OAuth, SSO)

Recherche exhaustive menée sur les 4 dépôts (`ib-welloresto-api`, `wello-back-office`, `wello_resto_flutter`, `wello-kiosk`).

**Résultat : aucune fonctionnalité d'authentification par fournisseur externe (Google Sign-In, OAuth « login social », SSO) pour les utilisateurs/employés de la plateforme n'a été trouvée, active ou non, dans aucun des 4 dépôts.**

Occurrences réelles trouvées (toutes sans rapport avec le login utilisateur) :
- **API** — uniquement des flux OAuth **d'intégrations plateforme tierce** : OAuth Uber Eats (`internal/config/ubereats.go:16-17`, `internal/modules/ubereats/client.go:56,90`), OAuth2 Deliveroo (`internal/modules/deliveroo/client.go:29,81-82`), et un JWT + OAuth2 (`https://oauth2.googleapis.com/token`) pour obtenir un access token **Google FCM** via compte de service (`internal/modules/notification/token_manager.go:96-188`) — notifications push, pas login utilisateur. `go.mod` ne déclare que `google/uuid` et `google.golang.org/protobuf`, aucune lib OAuth2/Sign-In.
- **Back-office** — aucune occurrence de `oauth`/`GoogleSignIn`/`sso`/`passport`/`firebase-auth`. Seules mentions Google : `@googlemaps/js-api-loader` (cartes) et une valeur d'énum `"google"` dans `ReservationSource` (source Google Reserve d'une réservation, sans rapport avec l'auth).
- **POS Flutter** — aucune occurrence de `google_sign_in`, `oauth`, `firebase_auth`, `sso`. Dépendances Google : `firebase_core`, `firebase_messaging` (push), `google_fonts`, `google_maps_flutter` — aucune liée à l'authentification.
- **Kiosk** — aucune occurrence. Système d'enrôlement propre par code + refresh token interne (`/auth/enroll`, `/auth/token/refresh`, `/auth/reclaim`), indépendant de tout fournisseur externe.

**Conclusion** : le login (employé/back-office) repose exclusivement sur le couple identifiant + mot de passe, ou sur un PIN pour le POS, avec un token opaque stocké côté serveur. Aucun code, même mort ou expérimental, d'intégration Google Sign-In/OAuth « social login »/SSO pour l'authentification des utilisateurs n'existe dans l'écosystème audité.
---

## 3. Permissions

### 3.1. Liste exhaustive des permissions existantes dans le code

Le système repose sur **deux mondes qui coexistent** (transition RBAC en cours) : un catalogue de permissions nommées (nouveau) et d'anciennes colonnes booléennes (legacy), reliées entre elles par une table de correspondance (« fallback »).

#### 3.1.1. Catalogue RBAC (nouveau) — `internal/permission/keys_gen.go`

Fichier intégral :
```go
// Package permission declares the fixed catalog of RBAC permission keys as
// typed Go constants. The catalog itself lives in the database (table
// `permissions`, seeded by migrations/done/095_roles_permissions_catalog.up.sql,
// extended by migrations/done/097_permission_pos_status_manage.up.sql, and
// reduced by migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql)
// — this file is a typed mirror of every migration's net INSERT/DELETE
// effect, kept honest by keys_gen_test.go, which fails the build the moment
// the two diverge.
//
// Do not add, rename, or remove a key here without making the matching change
// in a migration (a new one for an addition/rename/deprecation; see the
// existing ones for the pattern) in the same change.
package permission

// Key is a permission key from the `permissions` catalog table. Typed rather
// than a bare string so that RequirePermission and UserLoginRow.Has cannot
// accidentally be called with an arbitrary string that was never declared as
// a real permission.
type Key string

const (
	POSStatusManage      Key = "pos.status.manage"
	POSTicketReopen      Key = "pos.ticket.reopen"
	POSRefund            Key = "pos.refund"
	POSCashDrawerOpen    Key = "pos.cash_drawer.open"
	CatalogManage        Key = "catalog.manage"
	InventoryManage      Key = "inventory.manage"
	HACCPManage          Key = "haccp.manage"
	CustomersManage      Key = "customers.manage"
	StaffManage          Key = "staff.manage"
	StaffScheduleManage  Key = "staff.schedule.manage"
	ReportsSalesRead     Key = "reports.sales.read"
	ReportsFinancialRead Key = "reports.financial.read"
	SettingsManage       Key = "settings.manage"
)

// All lists every permission key declared above, in catalog (sort_order) order.
var All = []Key{
	POSStatusManage,
	POSTicketReopen,
	POSRefund,
	POSCashDrawerOpen,
	CatalogManage,
	InventoryManage,
	HACCPManage,
	CustomersManage,
	StaffManage,
	StaffScheduleManage,
	ReportsSalesRead,
	ReportsFinancialRead,
	SettingsManage,
}
```

Soit **13 permissions** dans le catalogue actuellement en vigueur, avec leur libellé exact tel que seedé en base par `migrations/done/095_roles_permissions_catalog.up.sql:17-32` (complété par `migrations/done/097_permission_pos_status_manage.up.sql:14-16`) :

| Clé (`permission.Key`) | Domaine | Libellé (`label`) | `is_sensitive` |
|---|---|---|---|
| `pos.status.manage` | pos | Ouvrir et fermer l'établissement | false |
| `pos.ticket.reopen` | pos | Rouvrir un ticket clôturé | true |
| `pos.refund` | pos | Rembourser une vente | true |
| `pos.cash_drawer.open` | pos | Ouvrir le tiroir-caisse hors encaissement | true |
| `catalog.manage` | catalog | Gérer les produits, les tarifs et les cartes | true |
| `inventory.manage` | inventory | Gérer les stocks et les inventaires | false |
| `haccp.manage` | haccp | Gérer le suivi HACCP | false |
| `customers.manage` | customers | Gérer et exporter les fiches clients | true |
| `staff.manage` | staff | Gérer les employés, les postes, les rôles et les droits | true |
| `staff.schedule.manage` | staff | Gérer le planning et les pointages | false |
| `reports.sales.read` | reports | Consulter et exporter les rapports de vente | false |
| `reports.financial.read` | reports | Consulter et exporter les rapports financiers | true |
| `settings.manage` | settings | Paramétrer l'établissement | true |

**Deux clés ont existé puis ont été supprimées du catalogue** : `pos.access` et `pos.discount.apply`, retirées par `migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql:17-19` (« RBAC lot 8 », 2026-08-27) — le fichier de migration précise qu'aucune des deux n'a jamais gardé de route réelle.

#### 3.1.2. Fonctions `Has*`/`Can*`/`Is*` restantes

`internal/middleware/permissions.go` (fichier intégral) :
```go
package middleware

import (
	"welloresto-api/internal/modules/auth"
)

// ============================================================
// RBAC lot 2 — bascule des prédicats
//
// Toutes les fonctions HasXxx/CanXxx ainsi que les combinateurs AnyOf/AllOf
// ont été retirées : RequirePermission prend désormais directement une
// permission.Key et appelle user.Has(key), qui encapsule la correspondance
// avec les anciennes colonnes booléennes (voir internal/modules/auth/
// permissions.go). Elles ont été supprimées plutôt que dépréciées car aucune
// n'avait plus d'appelant réel dans cmd/api/routes.go au moment de la bascule.
//
// IsAdmin reste à part : il correspond à « détient tous les droits », pas à
// un droit particulier du catalogue — voir middleware.RequireAdmin.
//
// RBAC lot 2.5 : IsEmailVerified et IsTelVerified ont été retirées. Ce
// n'étaient pas des droits RBAC mais un statut de vérification de compte
// détourné en décision d'autorisation — et qui vérifiait de toute façon
// l'utilisateur connecté plutôt que le responsable de l'établissement.
// ============================================================

// IsAdmin vérifie que l'utilisateur est administrateur
func IsAdmin(user *auth.UserLoginRow) bool {
	return user.IsAdmin()
}
```

Donc **une seule fonction prédicat subsiste** dans ce fichier : `IsAdmin`. Toutes les anciennes fonctions `HasMenuAccess`, `HasPlanningAccess`, `HasUserManagementAccess`, `HasSettingsAccess`, `HasHACCPAccess`, `HasCustomerManagementAccess`, `IsEmailVerified`, `IsTelVerified` **n'existent plus** dans le code (supprimées, pas dépréciées).

Et `internal/modules/auth/permissions.go` (fichier intégral) :
```go
package auth

import (
	"welloresto-api/internal/permission"
)

// legacyPermissionFallback maps each catalog permission key to the historical
// boolean field on UserRowRights that used to gate it, consulted only for a
// user with no role yet (RoleID == nil — see Has).
//
// Three catalog keys are deliberately absent: pos.ticket.reopen, pos.refund,
// inventory.manage. No boolean column ever granted these — historically only
// Rights.Admin has them, which Has handles before ever consulting this map.
var legacyPermissionFallback = map[permission.Key]func(UserRowRights) bool{
	permission.POSStatusManage:      func(r UserRowRights) bool { return r.AccessReception },
	permission.POSCashDrawerOpen:    func(r UserRowRights) bool { return r.OpenCashDrawer },
	permission.CatalogManage:        func(r UserRowRights) bool { return r.CanManageMenu },
	permission.HACCPManage:          func(r UserRowRights) bool { return r.CanManageHACCP },
	permission.CustomersManage:      func(r UserRowRights) bool { return r.CanManageCustomers },
	permission.StaffManage:          func(r UserRowRights) bool { return r.CanManageUsers },
	permission.StaffScheduleManage:  func(r UserRowRights) bool { return r.CanManagePlannings },
	permission.ReportsSalesRead:     func(r UserRowRights) bool { return r.CanViewReports },
	permission.ReportsFinancialRead: func(r UserRowRights) bool { return r.CanViewFinancials },
	permission.SettingsManage:       func(r UserRowRights) bool { return r.CanManageSettings },
}

// Has indique si l'utilisateur détient le droit demandé sur son établissement
// courant.
//
// Deux mondes coexistent pendant la transition :
//   - RoleID nil     -> l'utilisateur n'a pas encore de rôle, on retombe sur
//     les colonnes booléennes historiques (comportement identique à
//     aujourd'hui) ;
//   - RoleID non nil -> les droits viennent du rôle, les booléens sont
//     ignorés — même s'ils contredisent le rôle.
func (u *UserLoginRow) Has(key permission.Key) bool {
	if u.RoleID != nil {
		if u.RoleSystemKey != nil && *u.RoleSystemKey == permission.SystemKeyAdmin {
			return true
		}
		for _, granted := range u.Permissions {
			if granted == string(key) {
				return true
			}
		}
		return false
	}

	// Monde historique : admin court-circuite tout, comme aujourd'hui.
	if u.Rights.Admin {
		return true
	}
	if fallback, ok := legacyPermissionFallback[key]; ok {
		return fallback(u.Rights)
	}
	return false
}

// HasAdminRole reports whether the user's RBAC ROLE is the admin role.
//
// Deliberately distinct from IsAdmin() (models.go), which is the legacy
// Rights.Admin column alone. Rights.Admin frequently stays true in
// production regardless of the assigned role (historical seeding), so a
// caller that wants "is this user's *role* admin" must use this method.
func (u *UserLoginRow) HasAdminRole() bool {
	if u.RoleID != nil {
		return u.RoleSystemKey != nil && *u.RoleSystemKey == permission.SystemKeyAdmin
	}
	return u.Rights.Admin
}
```

**Note factuelle importante** : trois clés du catalogue (`pos.ticket.reopen`, `pos.refund`, `inventory.manage`) n'ont **aucune** colonne booléenne héritée correspondante — un utilisateur du « monde historique » (sans `role_id`) ne peut jamais les obtenir sauf via `Rights.Admin`.

#### 3.1.3. Middleware appliquant les permissions

`internal/middleware/require_permission.go` (extraits, cf. §2.4 ci-dessus pour le code intégral de `RequirePermission`/`RequireAdmin`). Deux gardes existent : `middleware.RequirePermission(permission.Key)` et `middleware.RequireAdmin()`. Il n'existe plus de combinateurs `AnyOf`/`AllOf`.

#### 3.1.4. Colonnes booléennes historiques (« monde legacy »)

`internal/modules/auth/models.go:126-154` :
```go
type UserRowRights struct {

	// Accès aux modules
	AccessReception bool
	AccessDelivery  bool
	AccessWaiter    bool

	// Gestion & Rapports
	PrintMerchantCashReport bool
	OpenCashDrawer          bool
	CanManageMenu           bool
	CanManagePlannings      bool
	CanManageUsers          bool
	CanManageSettings       bool
	CanManageHACCP          bool

	// Reports & Financials
	CanViewReports      bool
	CanExportReports    bool
	CanViewFinancials   bool
	CanExportFinancials bool

	// Customers
	CanManageCustomers bool
	CanExportCustomers bool

	// Admin
	Admin bool
}
```
Soit **17 champs booléens** (16 droits + `Admin`), toujours présents en base et lus dans `internal/modules/auth/repository.go:183-188` et `:367-372`. Ce sont ces mêmes 16 clés (hors `admin`) que le back-office manipule sous forme de `MerchantUserPermissions` (voir §3.3).

#### 3.1.5. Routes effectivement câblées sur chaque permission

Extrait de `cmd/api/routes.go`, lignes 587-1486 (occurrences de `RequirePermission`/`RequireAdmin`) :
```
587:  r.With(authMiddleware, middleware.RequirePermission(permission.StaffManage)).Post("/pin/reset", authH.ResetPIN)
599:  r.With(middleware.RequirePermission(permission.StaffManage)).Get("/", usersH.ListMerchantUsers)
600:  r.With(middleware.RequirePermission(permission.StaffManage)).Post("/", usersH.CreateUser)
601:  r.With(middleware.RequirePermission(permission.StaffManage)).Post("/create", usersH.CreateUser)
602:  r.With(middleware.RequirePermission(permission.StaffManage)).Get("/linkable-search", usersH.SearchLinkableUsers)
603:  r.With(middleware.RequirePermission(permission.StaffManage)).Get("/{id}", usersH.GetMerchantUser)
604:  r.With(middleware.RequirePermission(permission.StaffManage)).Post("/{id}/merchant-link", usersH.LinkMerchantUser)
605:  r.With(middleware.RequirePermission(permission.StaffManage)).Get("/{id}/rights", usersH.GetMerchantUserRights)
606:  r.With(middleware.RequirePermission(permission.StaffManage)).Put("/{id}/rights", usersH.UpdateMerchantUserRights)
607:  r.With(middleware.RequirePermission(permission.StaffManage)).Get("/{id}/member", usersH.GetMerchantUserMember)
608:  r.With(middleware.RequirePermission(permission.StaffManage)).Patch("/{id}/member", usersH.PatchMerchantUserMember)
609:  r.With(middleware.RequirePermission(permission.StaffManage)).Put("/{id}/role", rolesH.SetUserRole)
610:  r.With(middleware.RequireAdmin()).Post("/{id}/force-reset-password", usersH.ForceResetPassword)
611:  r.With(middleware.RequireAdmin()).Delete("/{id}/merchant-link", usersH.UnlinkMerchantUser)
631:  r.Use(middleware.RequirePermission(permission.StaffManage))                          // groupe /roles
644:  r.With(middleware.RequirePermission(permission.StaffManage)).Put("/default-role", rolesH.SetMerchantDefaultRole)
655:  r.With(middleware.RequirePermission(permission.ReportsSalesRead))...
667:  r.With(middleware.RequirePermission(permission.StaffManage)).Post("/link-user", posH.LinkUser)
669:  r.With(middleware.RequirePermission(permission.POSStatusManage)).Patch("/status", posH.UpdatePOSStatus)
700:  r.Use(middleware.RequirePermission(permission.ReportsSalesRead))
740:  r.Use(middleware.RequirePermission(permission.ReportsFinancialRead))
766:  r.With(middleware.RequirePermission(permission.InventoryManage))...
806-810: r.With(middleware.RequirePermission(permission.CatalogManage))... (x3)
925:  r.With(middleware.RequirePermission(permission.HACCPManage))...
1005: r.Use(middleware.RequirePermission(permission.StaffScheduleManage))
1088: r.With(middleware.RequirePermission(permission.ReportsFinancialRead))...
1190: r.With(middleware.RequirePermission(permission.POSTicketReopen))...
1192: r.With(middleware.RequirePermission(permission.POSRefund))...
1211: r.With(middleware.RequirePermission(permission.POSRefund))...
1254: r.With(middleware.RequirePermission(permission.POSCashDrawerOpen))...
1290-1294: r.With(middleware.RequirePermission(permission.CustomersManage))... (x3)
1426: r.With(middleware.RequirePermission(permission.ReportsFinancialRead))...
1485-1486: r.With(middleware.RequirePermission(permission.SettingsManage))... (x2)
```
Toutes les 13 clés du catalogue sont utilisées au moins une fois. `RequireAdmin()` n'est câblé que sur deux routes : `POST /users/{id}/force-reset-password` et `DELETE /users/{id}/merchant-link`.

### 3.2. Concept de RÔLE / PROFIL de permissions

**Oui — il existe un système de rôle nommé et réutilisable**, introduit par le commit RBAC en cours de bascule. Ce n'est pas embryonnaire au sens « juste une idée » : le schéma SQL, l'API REST et une bonne partie de la logique métier existent et sont fonctionnels. Il coexiste avec l'ancien modèle « une ligne = 16 droits individuels par utilisateur » qui reste la source de vérité tant qu'aucun rôle n'est assigné.

#### 3.2.1. Schéma SQL — `migrations/done/094_roles_schema.up.sql`

```sql
-- ---------------------------------------------------------------------------
-- 1. permissions — the fixed catalog of grantable actions.
-- ---------------------------------------------------------------------------
CREATE TABLE permissions (
    key           varchar(64) PRIMARY KEY,
    domain        varchar(32) NOT NULL,
    label         varchar(150) NOT NULL,
    description   text NOT NULL DEFAULT '',
    is_sensitive  boolean NOT NULL DEFAULT false,
    sort_order    integer NOT NULL DEFAULT 0,
    deprecated_at timestamptz
);

-- ---------------------------------------------------------------------------
-- 2. roles — per-merchant named bundles of permissions.
-- ---------------------------------------------------------------------------
CREATE TABLE roles (
    id          varchar(64) PRIMARY KEY,          -- role-<uuid>, app-generated
    merchant_id varchar(64) NOT NULL,
    name        varchar(150) NOT NULL,
    description text NOT NULL DEFAULT '',
    system_key  varchar(16),                       -- 'admin' | 'staff' | NULL (custom role)
    version     integer NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT roles_system_key_check CHECK (system_key IS NULL OR system_key IN ('admin', 'staff'))
);
-- ---------------------------------------------------------------------------
-- 3. role_permissions — many-to-many role <-> permission.
-- ---------------------------------------------------------------------------
CREATE TABLE role_permissions (
    role_id        varchar(64) NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_key varchar(64) NOT NULL REFERENCES permissions(key),
    PRIMARY KEY (role_id, permission_key)
);

-- ---------------------------------------------------------------------------
-- 4. users_rights.role_id — nullable pointer, not populated by this lot.
-- ---------------------------------------------------------------------------
ALTER TABLE users_rights ADD COLUMN role_id varchar(64) REFERENCES roles(id);
CREATE INDEX idx_users_rights_role_id ON users_rights (role_id);

-- ---------------------------------------------------------------------------
-- 5. merchant.default_role_id — role assigned to a newly linked user absent
--    any other choice.
-- ---------------------------------------------------------------------------
ALTER TABLE merchant ADD COLUMN default_role_id varchar(64) REFERENCES roles(id);
```
Le rôle est **rattaché au périmètre du merchant** (`roles.merchant_id`), pas global à la plateforme — chaque établissement a ses propres rôles nommés, y compris ses deux rôles « système ».

#### 3.2.2. Rôles système seedés automatiquement

`internal/modules/roles/repository.go:119-181` :
```go
var systemRolePermissions = map[string][]permission.Key{
	SystemKeyAdmin: permission.All,
	SystemKeyStaff: {},
}

var systemRoleNames = map[string]string{
	SystemKeyAdmin: "Administrateur",
	SystemKeyStaff: "Employé polyvalent",
}
```
Deux rôles nommés et réutilisables existent donc par construction pour chaque établissement : **« Administrateur »** (toutes les 13 permissions du catalogue) et **« Employé polyvalent »** (**zéro permission** par défaut). Au-delà, un merchant peut créer des **rôles personnalisés** via `POST /roles`, avec un ensemble de permissions librement défini via `PUT /roles/{id}/permissions`.

#### 3.2.3. Routes REST exposées

`cmd/api/routes.go:618-645` :
```go
// --- PERMISSIONS / ROLES (RBAC lot 6) ---
r.Route("/permissions", func(r chi.Router) {
	r.Use(authMiddleware)
	r.Get("/", rolesH.ListPermissions)
})

r.Route("/me", func(r chi.Router) {
	r.Use(authMiddleware)
	r.Get("/permissions", rolesH.MyPermissions)
})

r.Route("/roles", func(r chi.Router) {
	r.Use(authMiddleware)
	r.Use(middleware.RequirePermission(permission.StaffManage))

	r.Get("/", rolesH.ListRoles)
	r.Post("/", rolesH.CreateRole)
	r.Get("/{id}", rolesH.GetRole)
	r.Patch("/{id}", rolesH.UpdateRole)
	r.Put("/{id}/permissions", rolesH.ReplacePermissions)
	r.Get("/{id}/members", rolesH.ListRoleMembers)
	r.Post("/{id}/archive", rolesH.ArchiveRole)
})

r.Route("/merchant", func(r chi.Router) {
	r.Use(authMiddleware)
	r.With(middleware.RequirePermission(permission.StaffManage)).Put("/default-role", rolesH.SetMerchantDefaultRole)
})
```

#### 3.2.4. Bascule automatique à la création d'un utilisateur

Fait crucial : `internal/modules/users/admin_repository.go:328-337` (`UpsertMerchantUserRights`, branche INSERT) :
```go
// role_id comes from merchant.default_role_id (RBAC lot 4), never
// hardcoded — fails explicitly (models.ErrMerchantDefaultRoleNotSet)
// rather than inserting a new row with no role_id. Only this INSERT
// branch (a brand new link) sets it; the UPDATE branch above re-enables
// an existing link and must never overwrite whatever role_id it already
// carries. See migrations/done/099_merchant_default_role_admin.up.sql.
roleID, err := r.MerchantDefaultRoleID(ctx, merchantID)
if err != nil {
	return 0, err
}

insertID, err := db.InsertReturningID(ctx, `
	INSERT INTO users_rights (
		user_id, merchant_id, token, admin, role_id,
		access_wrreception, access_wrdelivery, access_wrwaiter,
		print_merchant_cash_report, open_cash_drawer, manage_menu,
		manage_plannings, manage_users, manage_settings, manage_haccp,
		view_reports, export_reports, view_financials, export_financials,
		manage_customers, export_customers, enabled
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE)
`, "id", userID, merchantID, token, rights.Admin, roleID, ...)
```

Et `migrations/done/099_merchant_default_role_admin.up.sql:1-28` :
```sql
-- RBAC lot 4: repoints merchant.default_role_id at each establishment's
-- "admin" role. Direct consequence of the product decision behind this lot —
-- every account becomes Administrateur while permissions are not yet
-- exploited from any screen, so a newly linked user must land on the same
-- footing as everyone else.
UPDATE merchant
SET default_role_id = r.id
FROM roles r
WHERE r.merchant_id = CAST(merchant.id AS TEXT)
  AND r.system_key = 'admin';
```

**Conséquence factuelle observée** : tout utilisateur nouvellement créé et lié à un établissement (`POST /users`, `POST /users/create`, ou `POST /users/{id}/merchant-link`) reçoit automatiquement un `role_id` pointant vers le rôle système « Administrateur » de ce merchant (puisque `merchant.default_role_id` pointe systématiquement sur ce rôle, migration 099). Or `Has()` (§3.1.2) court-circuite immédiatement à `true` pour toute clé dès lors que `RoleSystemKey == "admin"`, **avant même de consulter** les droits individuels envoyés dans la requête de création. Le commutateur « Administrateur » du formulaire de création (voir §3.3) — et l'objet `permissions` légataire envoyé par `LinkForm` — n'ont donc, en l'état, aucun effet sur l'autorisation réelle une fois le `role_id` posé : l'utilisateur créé est administrateur RBAC de fait.

#### 3.2.5. Conclusion

Le modèle **n'est pas** « une ligne = un droit individuel par utilisateur » de façon exclusive : c'est un système hybride où un rôle nommé, réutilisable, propre à chaque merchant, coexiste avec l'ancien modèle des 16 colonnes booléennes individuelles sur `users_rights`.

### 3.3. Attribution des droits à la création d'un utilisateur dans le back-office

Répertoire concerné : `wello-back-office/src/pages/equipe`. La page `EquipePage.tsx` délègue la création à un composant dédié, `CreateMemberSheet`, situé dans `src/components/team/CreateMemberSheet.tsx`.

#### 3.3.1. Composant `CreateMemberSheet.tsx` (extrait — logique de soumission du formulaire de création)

```tsx
function CreateForm({ onSuccess }: { onSuccess: () => void }) {
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [email, setEmail] = useState("");
  const [tel, setTel] = useState("");
  const [password, setPassword] = useState("");
  const [admin, setAdmin] = useState(false);
  const [loginEnabled, setLoginEnabled] = useState(true);
  const [positionId, setPositionId] = useState("");
  const [role, setRole] = useState("");
  const [contractTypeCode, setContractTypeCode] = useState("");

  const handleSubmit = () => {
    const payload: CreateUserRequest = {
      first_name: firstName.trim(),
      last_name: lastName.trim(),
      email: email.trim(),
      tel: tel.trim() || undefined,
      password: password ? password : undefined,
      rights: {
        admin,
        login_enabled: loginEnabled,
      },
      planning: {
        ...(positionId ? { position_id: positionId } : {}),
        ...(role ? { role } : {}),
        ...(contractTypeCode ? { contract_type_code: contractTypeCode } : {}),
      },
    };

    mutation.mutate(payload);
  };

  // Bloc "Accès" du formulaire — deux commutateurs seulement :
  //   <Switch id="create-admin" checked={admin} onCheckedChange={setAdmin} />       — Administrateur
  //   <Switch id="create-login" checked={loginEnabled} onCheckedChange={setLoginEnabled} /> — Connexion activée
  //
  // Bloc "Planning (optionnel)" contient un sélecteur "Rôle" avec seulement
  // trois valeurs libres : employee / manager / admin — envoyées dans
  // payload.planning.role. Ce n'est PAS le role_id RBAC (voir 3.2), c'est un
  // champ RH/planning distinct.
}
```

#### 3.3.2. Constats factuels sur l'écran de création

- L'onglet **« Nouveau membre »** (`CreateForm`) ne présente que **deux commutateurs** dans le bloc « Accès » : `Administrateur` (bool `admin`) et `Connexion activée` (bool `login_enabled`). **Aucune case à cocher pour les 16 droits individuels** n'est présente à la création — contrairement à l'onglet « Lier un existant » (`LinkForm`) qui, lui, envoie explicitement les 16 clés à `false` (droits minimaux) lors du rattachement d'un utilisateur déjà existant à l'établissement.
- **Aucun sélecteur de rôle RBAC** (`roles.Role` / `role_id`) n'apparaît dans `CreateForm`. Le champ « Rôle » du bloc « Planning (optionnel) » est un champ RH/planning distinct (`employee`/`manager`/`admin`, envoyé dans `payload.planning.role`), sans rapport avec le `role_id` RBAC.
- Le sélecteur de rôle RBAC existe ailleurs dans le back-office, mais uniquement **après création**, dans l'onglet « Accès » de la fiche d'un membre existant (`AccessTab.tsx`), dont le commentaire d'en-tête précise :
```tsx
/**
 * E3 — replaces the old flat permission-toggle grid (RightsTab) entirely.
 * A single role selector, plus a read-only preview of what that role
 * grants — so an admin sees what they're assigning without opening the
 * roles screen separately.
 */
```
avec le sélecteur appelant `usersApi.updateRole(userId, roleId)` → `PUT /users/{id}/role`.

#### 3.3.3. Format exact du payload envoyé à l'API à la création

`wello-back-office/src/types/adminUsers.ts:136-152` :
```ts
/** Body of `POST /users` / `POST /users/create`. */
export interface CreateUserRequest {
  first_name: string;
  last_name: string;
  username?: string;
  email: string;
  /** May be empty: backend generates a random password when blank. */
  password?: string;
  tel?: string;
  merchant_id?: string | null;
  admin?: boolean;
  rights?: {
    admin?: boolean;
    login_enabled?: boolean;
    permissions?: Partial<MerchantUserPermissions>;
  };
  planning?: Partial<MerchantUserPlanningUpsertRequest>;
}
```

Struct Go réceptrice, `internal/modules/users/create_models.go` (fichier intégral) :
```go
package users

// CreateUserRequest is the JSON payload for POST /users/create.
type CreateUserRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email      string                           `json:"email"`
	Password   string                           `json:"password"`
	Tel        string                           `json:"tel"`
	MerchantID *string                          `json:"merchant_id,omitempty"`
	Admin      bool                             `json:"admin"`
	Rights     *MerchantUserRightsUpsertRequest `json:"rights,omitempty"`
}

// CreateUserResponse is the JSON body returned on success (201).
type CreateUserResponse struct {
	UserID string `json:"user_id"`
}
```

Côté service, `internal/modules/users/create_service.go:51-54` :
```go
rights := defaultMerchantUserRights(req.Admin)
if req.Rights != nil {
	rights = req.Rights.Normalize(defaultMerchantUserRights(req.Admin))
}
```

Comme le payload frontend n'envoie jamais `rights.permissions`, `Normalize` retombe sur les valeurs par défaut de `defaultMerchantUserRights(req.Admin)` pour les 16 droits individuels — seul le commutateur `Administrateur` de l'écran de création a un effet sur ces colonnes historiques. Et comme démontré en §3.2.4, ce même commutateur `admin` n'a de toute façon **aucun effet sur l'autorisation RBAC réelle**, celle-ci étant entièrement déterminée par `role_id` (systématiquement le rôle « Administrateur » du merchant dès la création).
---

## 4. Fiscalité et registre de caisse

Le schéma SQL complet n'existe dans aucun fichier `migrations/` versionné pour les tables fiscales — il n'est visible que dans le snapshot `docs/migration-postgres/04-schema-postgres-target.sql` (dump généré pendant le chantier de migration MySQL→Postgres). Ce point est noté explicitement partout où il s'applique ci-dessous.

### 4.1. Ouverture / clôture d'un registre de caisse

#### 4.1.1. Tables SQL concernées

Six tables portent le registre de caisse. **Aucune n'est créée par un fichier sous `migrations/done/` ou `migrations/todo/`** — leur DDL n'existe que dans le snapshot `docs/migration-postgres/04-schema-postgres-target.sql`, preuve que ces tables préexistent au système de migrations versionnées du dépôt.

**`cash_desks`** (`docs/migration-postgres/04-schema-postgres-target.sql:463-470`) — la caisse physique (le "poste"), pas la session :
```sql
CREATE TABLE cash_desks (
    cash_desk_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    name varchar(50) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    creation_date timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cash_desk_id)
);
```

**`cash_registers`** (lignes 504-523) — la session de caisse (ouverture/clôture), avec les 3 colonnes du chaînage fiscal :
```sql
CREATE TABLE cash_registers (
    cash_register_id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    merchant_id varchar(64) NOT NULL,
    cash_desk_id integer NOT NULL,
    device_id varchar(50) NOT NULL,
    user_id varchar(64) NOT NULL,
    cash_fund integer NOT NULL,
    final_cash_fund integer DEFAULT 0,
    start_date timestamptz NOT NULL,
    end_date timestamptz,
    closed boolean NOT NULL DEFAULT false,
    enclosed boolean NOT NULL DEFAULT false,
    closure_comment varchar(255) NOT NULL,
    closed_by varchar(25),
    hash varchar(64),
    signature text,
    previous_hash varchar(64),
    PRIMARY KEY (cash_register_id)
);
COMMENT ON COLUMN cash_registers.cash_fund IS 'in cents';
```
Deux états successifs et distincts : `closed` (le rapport Z est calculé, mais les `cash_registers_custom_items` restent modifiables) puis `enclosed` (verrouillage définitif).

**`cash_registers_items`** (lignes 548-555) — snapshot automatique des ventes par moyen de paiement, figé à la clôture :
```sql
CREATE TABLE cash_registers_items (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    cash_register_id integer NOT NULL,
    mop varchar(10) NOT NULL,
    amount integer NOT NULL,
    PRIMARY KEY (id)
);
```

**`cash_registers_custom_items`** (lignes 531-540) — ajustements manuels saisis par le restaurateur :
```sql
CREATE TABLE cash_registers_custom_items (
    id integer GENERATED ALWAYS AS IDENTITY NOT NULL,
    label varchar(25) NOT NULL,
    amount integer NOT NULL,
    merchant_id varchar(64),
    created_by varchar(35),
    enabled boolean NOT NULL DEFAULT true,
    cash_register_id integer NOT NULL,
    PRIMARY KEY (id)
);
```

**`device_link`** (lignes 1105-1111) — liaison appareil secondaire → caisse principale.

**Tables présentes dans le schéma mais mortes côté Go** — aucune occurrence d'`INSERT INTO` dans tout le dépôt : `cash_reports` (lignes 564-573), `cash_funds` (lignes 478-494), `sub_cash_registers` (lignes 3747-3755, référencée uniquement en lecture).

#### 4.1.2. Endpoints HTTP

Enregistrés dans `cmd/api/routes.go:1317-1336` et `:1246-1256` :

| Méthode | Route | Handler |
|---|---|---|
| POST | `/cash_register/open` | `cashRegisterH.OpenCashRegister` |
| GET / POST | `/cash_register/history` | `cashRegisterH.GetHistory` |
| POST | `/cash_register/link` | `cashRegisterH.HandleLinkDevice` |
| DELETE | `/cash_register/link` | `cashRegisterH.HandleUnlinkDevice` |
| GET | `/cash_register/{cash_register_id}/` | `cashRegisterH.GetCashRegisterHistoryByID` |
| GET | `/cash_register/{cash_register_id}/summary` | `cashRegisterH.GetCashRegisterSummary` |
| GET | `/cash_register/{cash_register_id}/tva-details` | `cashRegisterH.GetCashRegisterTVADetails` |
| PATCH | `/cash_register/{cash_register_id}/close` | `cashRegisterH.CloseCashRegister` |
| PATCH | `/cash_register/{cash_register_id}/enclose` | `cashRegisterH.EncloseCashRegister` |
| POST | `/cash_register/{cash_register_id}/custom_items` | `cashRegisterH.AddCustomItem` |
| DELETE | `/cash_register/{cash_register_id}/custom_items/{item_id}` | `cashRegisterH.DeleteCustomItem` |
| POST | `/cash_drawer/open` | `cashRegisterH.OpenCashDrawer` (garde RBAC `permission.POSCashDrawerOpen`) |

#### 4.1.3. Séquence de numérotation

Pas une seule séquence, mais **quatre chaînages distincts et indépendants** :

1. **`orders.order_num`** — numéro affiché au client/marchand. Ce n'est **pas** une séquence fiscale continue : elle **se réinitialise à 1 dès que le dernier numéro atteint 99** — `internal/modules/order_life_cycle/repository.go:1825-1857` :
```go
// GetNextOrderNum returns the next order_num following the PHP behaviour:
// - if last order_num is 99 or null -> return 1
// - otherwise last + 1
func (r *OrdersLifeCycleRepository) GetNextOrderNum(ctx context.Context, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	var last sql.NullInt64

	err := db.QueryRowContext(ctx, `
		SELECT order_num
		FROM orders
		WHERE merchant_id = ?
		ORDER BY order_id DESC
		LIMIT 1
		`, merchantID).Scan(&last)

	if err != nil && err != sql.ErrNoRows {
		return "1", err
	}
	if !last.Valid {
		return "1", nil
	}
	if last.Int64 == 99 {
		return "1", nil
	}
	return strconv.FormatInt(last.Int64+1, 10), nil
}
```

2. **`cash_registers.cash_register_id`** — simple PK auto-incrémentée, pas de logique métier de continuité.

3. **`receipts.receipt_number`** — numérotation fiscale séquentielle annuelle au format `F-YYYY-NNNNNN` (voir §4.1.5).

4. **`payments`** — pas de numéro de séquence visible, uniquement chaînage par hash.

#### 4.1.4. Chaînage cryptographique — quatre chaînes de hash indépendantes

Le dépôt implémente un chaînage SHA-256 + signature HMAC sur **quatre tables séparément**, chacune avec sa propre requête "dernier hash" et sa propre formule de payload. La primitive de signature est commune :
```go
// internal/utils/security/hash_signing.go
package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"os"
)

func SignHash(dataHash string) string {
	key := []byte(os.Getenv("FISCAL_SIGNING_KEY"))
	h := hmac.New(sha256.New, key)
	h.Write([]byte(dataHash))
	return fmt.Sprintf("%x", h.Sum(nil))
}
```

**a) Chaînage `cash_registers` (clôture de caisse)** — `internal/modules/cash_registers/repository.go:485-527`, dans `CloseCashRegister` :
```go
	// 6. LOGIQUE FISCALE : Récupération du précédent hash
	var prevHash sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT hash FROM cash_registers 
		WHERE merchant_id = ? AND end_date IS NOT NULL AND cash_register_id != ?
		ORDER BY end_date DESC LIMIT 1
	`, merchantID, cashRegisterID).Scan(&prevHash)

	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("erreur récupération prev_hash: %w", err)
	}

	actualPrevHash := "GENESIS_HASH"
	if prevHash.Valid && prevHash.String != "" {
		actualPrevHash = prevHash.String
	}

	// 7. Calcul du nouveau Hash
	dataToHash := fmt.Sprintf("%s|%s|%.2f|%s", cashRegisterID, merchantID, float64(calculatedFinalCash), actualPrevHash)
	hashBytes := sha256.Sum256([]byte(dataToHash))
	newHash := hex.EncodeToString(hashBytes[:])

	signature := security.SignHash(newHash)

	// 8. Fermer le registre
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE cash_registers
		SET end_date = %s,
			closed = true,
			final_cash_fund = ?,
			previous_hash = ?,
			hash = ?,
			signature = ?
		WHERE cash_register_id = ?
			AND closed = false
	`, dbx.UTCNow()), calculatedFinalCash, actualPrevHash, newHash, signature, cashRegisterID)
```
C'est **la seule des quatre chaînes à poser un marqueur de genèse explicite** (`"GENESIS_HASH"`) quand aucune caisse précédente n'existe.

**b) Chaînage `orders` (clôture de commande / livraison)** — deux points d'écriture identiques : `SetDeliveredLocal` (`internal/modules/order_life_cycle/repository.go:895-926`) et `DeleteOrderLocal` (lignes 784-816) :
```go
	// RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal pour Orders)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
        SELECT hash FROM orders 
        WHERE merchant_id = ? AND state = 'CLOSED' 
        ORDER BY delivered_on DESC, order_id DESC LIMIT 1 
        FOR UPDATE
    `, meta.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	deliveredOn := now.Format(time.RFC3339)

	payload := fmt.Sprintf("%s|%s|%d|%s", prevHash.String, deliveredOn, currentPrice, orderID)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)

	qUpd := `
    UPDATE orders
    SET last_update = ?,
        brand_status = 'CLOSED',
        state = 'CLOSED',
        isPaid = TRUE,
        isDistributed = TRUE,
        delivered_on = ?,
        previous_hash = ?,
        hash = ?,
		signature = ?
    WHERE order_id = ?
    `
	if _, err := db.ExecContext(ctx, qUpd, now, now, prevHash.String, newHash, signature, orderID); err != nil {
```
Aucun marqueur de genèse explicite ici : si aucune commande `CLOSED` précédente n'existe, `prevHash.String` vaut simplement `""`.
Note factuelle : dans `DeleteOrderLocal` (ligne 815), l'argument passé au `previous_hash` de l'`UPDATE` est `prevHash` (le `sql.NullString`) et non `prevHash.String` comme dans `SetDeliveredLocal` — différence de code observée entre les deux points d'écriture de la même chaîne.

**c) Chaînage `payments` (chaque encaissement)** — `internal/modules/order_life_cycle/repository.go:161-193`, dans `AddPaymentAndReturnID` :
```go
	// RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT hash FROM payments 
		WHERE merchant_id = ? 
		ORDER BY payment_date DESC LIMIT 1 
		FOR UPDATE
	`, payment.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	paymentDate := now.Format(time.RFC3339)

	payload := fmt.Sprintf("%s|%s|%d|%s|%s", prevHash.String, paymentDate, payment.Amount, payment.MOP, payment.OrderID)
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)
	...
	paymentID, err := db.InsertReturningID(ctx, `
	INSERT INTO payments
	(merchant_id, cash_register_id, order_id, amount, net_amount, mop, comment, payment_date, user_id, status_check, previous_hash, hash, signature, operation_type)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, "payment_id", payment.MerchantID, cashRegisterID, payment.OrderID, payment.Amount, payment.Amount, payment.MOP, payment.Comment, now, payment.UserID, payment.StatusCheck, prevHash.String, newHash, signature, payment.OperationType)
```
Schéma `payments` : `docs/migration-postgres/04-schema-postgres-target.sql:2696-2718` (colonnes `hash varchar(64)`, `signature text`, `previous_hash varchar(64)`, `operation_type varchar(20) NOT NULL DEFAULT 'SALE'`).

**d) Chaînage `receipts` (reçu fiscal — voir §4.1.5)** — hash + numérotation combinés.

#### 4.1.5. Le "reçu fiscal" (`receipts`) — chaînage + numérotation séquentielle annuelle

Table `receipts` (`docs/migration-postgres/04-schema-postgres-target.sql:3353-3376`) :
```sql
CREATE TABLE receipts (
    receipt_id varchar(50) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    order_id integer NOT NULL,
    receipt_number varchar(50) NOT NULL,
    total_ttc integer NOT NULL,
    total_ht integer NOT NULL,
    tax_details jsonb NOT NULL,
    items_snapshot jsonb NOT NULL,
    payments_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    prev_hash varchar(64),
    hash varchar(64) NOT NULL,
    signature text NOT NULL,
    PRIMARY KEY (receipt_id)
);
COMMENT ON COLUMN receipts.receipt_number IS 'Numéro fiscal séquentiel ex: F-2026-00012';
```

Génération complète — `internal/modules/receipt/service.go` :
```go
func (s *receiptService) GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error {
	lastNumber, lastHash, err := s.repo.GetLastReceiptData(ctx, *order.MerchantID)
	if err != nil {
		return fmt.Errorf("failed to get last receipt data: %w", err)
	}

	newNumber := s.generateNextReceiptNumber(lastNumber)

	itemsJSON, _ := json.Marshal(items)
	paymentsJSON, _ := json.Marshal(payments)
	taxDetailsJSON := []byte("{}")

	now := time.Now().UTC()

	// Formule du chaînage: H_n = SHA256(H_{n-1} | ReceiptNumber | TotalTTC | Date)
	payload := fmt.Sprintf("%s|%s|%d|%s", lastHash, newNumber, order.TTC, now.Format(time.RFC3339))
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)

	receipt := &models.Receipt{
		ReceiptID:        helpers.GeneratePrefixedID(helpers.ReceiptIDPrefix),
		MerchantID:       *order.MerchantID,
		OrderID:          order.OrderID,
		ReceiptNumber:    newNumber,
		TotalTTC:         int(order.TTC),
		TotalHT:          int(*order.HT),
		TaxDetails:       taxDetailsJSON,
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: paymentsJSON,
		CreatedAt:        now,
		PrevHash:         lastHash,
		Hash:             newHash,
		Signature:        signature,
	}

	return s.repo.InsertReceipt(ctx, receipt)
}

// generateNextReceiptNumber transforme "F-2026-000045" en "F-2026-000046"
func (s *receiptService) generateNextReceiptNumber(lastNumber string) string {
	currentYear := time.Now().UTC().Format("2006")
	prefix := "F-" + currentYear + "-"

	if lastNumber == "" || !strings.HasPrefix(lastNumber, prefix) {
		return prefix + "000001"
	}

	parts := strings.Split(lastNumber, "-")
	if len(parts) == 3 {
		seq, err := strconv.Atoi(parts[2])
		if err == nil {
			return fmt.Sprintf("%s%06d", prefix, seq+1)
		}
	}

	return prefix + "ERROR"
}
```

Verrouillage anti-doublon — `internal/modules/receipt/repository.go:25-51` :
```go
// GetLastReceiptData verrouille la lecture pour éviter les doublons de numérotation
func (r *receiptRepository) GetLastReceiptData(ctx context.Context, merchantID string) (string, string, error) {
	db := dbx.GetDB(ctx, r.database)

	var lastNumber sql.NullString
	var lastHash sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT receipt_number, hash 
		FROM receipts 
		WHERE merchant_id = ? 
		ORDER BY created_at DESC, receipt_number DESC 
		LIMIT 1 
		FOR UPDATE
	`, merchantID).Scan(&lastNumber, &lastHash)

	if err == sql.ErrNoRows {
		return "", "", nil // Premier reçu du marchand
	}
	if err != nil {
		return "", "", err
	}

	return lastNumber.String, lastHash.String, nil
}
```

Déclenchement : `HandlerFiscalReceiptGeneration` (`internal/modules/order_life_cycle/service.go:248-268`) est appelé depuis `DeliverOrder` (ligne 279), **immédiatement après** `SetDeliveredLocal` (chaîne (b)). Un reçu d'avoir (`GenerateRefundReceipt`) est généré en cas de remboursement. Le module `receipt` est câblé en production (`cmd/api/routes.go:294-295`), injecté dans `OrdersLifeCycleService`.

### 4.2. Initialisation de la séquence fiscale pour un nouveau marchand

**Réponse : initialisation paresseuse (lazy), pas d'initialisation explicite à la création du marchand.**

La création d'un marchand passe par `CreateMerchant` (`internal/modules/pos/create_service.go:11-74`), transaction complète — voir le détail dans la Section 8. Le contenu exact de `InitMerchantSatellites` (`internal/modules/pos/create_repository.go:57-134`) est la liste complète de tout ce qui est créé pour un nouveau marchand : QR codes, `scannorder_settings`, `merchant_parameters`, `merchant_marketing_settings`, `haccp_settings`, `bookings_settings`, et une **ligne `cash_desks`** (le poste physique nommé « Caisse principale »).

**Constat factuel** : la seule chose créée côté « caisse » à la création du marchand est une ligne `cash_desks`. **Aucune ligne n'est insérée dans `cash_registers`, `orders`, `payments` ou `receipts`.** Il n'existe donc :
- aucun compteur/séquence initialisé explicitement pour `orders.order_num` (premier appel `GetNextOrderNum` → `sql.ErrNoRows` → retourne `"1"`) ;
- aucune première valeur de `cash_registers.hash`/`previous_hash` (le premier `CloseCashRegister` du marchand retombera sur le littéral `"GENESIS_HASH"`) ;
- aucune première valeur de `receipts.receipt_number` (le premier appel à `GetLastReceiptData` retourne `"", ""`, et `generateNextReceiptNumber("")` produit `F-<année>-000001`) ;
- aucune première valeur de `payments.hash` (même mécanisme, `prevHash.String == ""`).

La séquence fiscale est donc **initialisée paresseusement au premier événement réel** (premier ticket / premier encaissement / première clôture de caisse), jamais à la création du marchand.

### 4.3. Notion de "mise en service" / "activation" / "go live"

**N'existe pas.**

Recherche exhaustive (`onboarding`, `go_live`, `go-live`, `activation`, `mise en service`, `activated_at`, `live_at`, `first_ticket`) sur `internal/` : aucune occurrence pertinente hors du module `integrations` (onboarding **Stripe Connect**, KYC du prestataire de paiement, sans rapport avec la conformité fiscale caisse).

Le seul flag qui s'en approche est `merchant.is_active` :
```sql
CREATE TABLE merchant (
    ...
    is_active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (id)
);
```
- Par défaut à `true` **dès la création** (`InsertMerchant` ne renseigne pas explicitement `is_active` — la valeur par défaut SQL s'applique). Un marchand est donc "actif" instantanément, sans étape intermédiaire.
- Modifiable via `UpdateMerchant` (`internal/modules/pos/repository.go:1077-1080`).
- C'est un simple bouton marche/arrêt générique, pas un jalon métier de "début d'exploitation réelle" — rien dans le code ne le relie à la première caisse ouverte, au premier ticket émis, ni à aucune notion fiscale.

Aucune colonne de type `activated_at`, `go_live_at`, `production_since`, aucun statut `"DRAFT"`/`"LIVE"`/`"ONBOARDING"` n'a été trouvé sur `merchant`, `cash_desks`, `cash_registers` ou tables associées.

### 4.4. Mode formation / mode école / mode test

**N'existe pas, sous aucune forme.**

Éléments vérifiés :
- `payments.operation_type` n'a que deux valeurs constantes définies — `internal/models/payment_models.go:4-5` :
```go
OperationTypeSale   = "SALE"
OperationTypeRefund = "REFUND"
```
Pas de `TRAINING`, `TEST`, ou `DEMO`.
- Aucune colonne `is_test`, `is_training`, `training_mode`, `sandbox`, `demo` sur `orders`, `payments`, `cash_registers`, ou `merchant`.
- `orders.brand_status` est une colonne texte libre (`varchar(30)`), sans table d'énumération Go dédiée ; les seules valeurs utilisées dans le code sont `'CLOSED'`, `'CANCELED'`, `'DELETED'`, `'OPEN'` — jamais de valeur liée à un mode formation/test.
- Les seules occurrences de `dry-run`/`dry_run` dans le dépôt concernent l'import de menu et l'import de clients (prévisualisation d'import de fichier), sans rapport avec la caisse ou les tickets de vente.

Conséquence directe : la question "comment ces tickets sont-ils exclus des totaux" est sans objet, puisqu'aucun ticket "de test" n'est identifiable dans le modèle de données actuel.

### 4.5. Attestation de conformité (NF525 ou équivalent)

**N'existe pas.** Aucun endpoint, aucune génération de PDF, aucun texte statique ne produit un document de type "certificat d'inaltérabilité" ou "attestation de conformité logicielle".

Le sigle "NF525" n'apparaît que dans des **commentaires de code Go**, jamais dans une chaîne de caractères produite en sortie :
```
internal/modules/delivery_sessions/service.go:177   — // Conformite NF525 : la fermeture de chaque commande passe par
internal/modules/delivery_sessions/service.go:305   — // (payment check, NF525 hash, state='CLOSED', possible session auto-close to 'done',
internal/modules/delivery_sessions/postgres_integration_test.go:266 — // ouvert via order_life_cycle.SetDelivered — hash NF525, signature, audit —
internal/modules/order_life_cycle/invoice_pdf.go:14  — // buildInvoicePDF génère le PDF de facture à partir du Receipt déjà figé (NF525) — aucun recalcul de montant.
internal/modules/order_life_cycle/service.go:1190    — // SendInvoiceByEmail génère la facture PDF de la commande à partir du Receipt déjà figé (NF525, ...)
```
Ces commentaires désignent le mécanisme de chaînage de hash (§4.1) comme *référence de conformité interne*, pas un document généré et remis au marchand/à l'administration.

Le seul document PDF généré par le système lié à une commande/vente est une **facture client** (`buildInvoicePDF`, envoyée par email via `SendInvoiceByEmail`) et un **PDF de rapport Z de caisse** (`ExportRegisterPDF`, exposé en `POST /accounting/registers/{register_id}/export-pdf`) — un rapport de clôture de caisse, pas une attestation de conformité logicielle.

### 4.6. Exclusion des tickets des exports comptables

Deux modules produisent des exports comptables, avec des filtres `WHERE` quasi identiques et répétés à chaque requête — aucun des deux n'a de filtre "formation/test" (cohérent avec §4.4) ; les exclusions portent uniquement sur l'état métier et le canal de la commande.

**Module `pos/reports`** (`internal/modules/pos/reports/repository.go:45-97`, `GetTVAReportData`) :
```sql
WHERE o.creation_date >= <borne jour début>
  AND o.creation_date <= <borne jour fin>
  AND o.merchant_id = ?
  AND o.state = 'CLOSED'
  AND o.brand = 'WELLO_RESTO'
  AND o.brand_status NOT IN ('DELETED', 'CANCELED')
  AND o.created_by NOT IN ('-1', 'SCANNORDER')
  AND tva.show_in_report
```
Mêmes exclusions pour `GetPaymentsReportData` (lignes 213-235).

**Module `pos/accounting`** (`internal/modules/pos/accounting/repository.go:183-233`, `GetTVAData`) — mêmes cinq exclusions, plus `tva.show_in_report`. Même filtre pour `GetPaymentsData` (payments). `GetVATAggregationRows` (lignes 637-712) reprend le même socle avec en plus un filtre `channels`/`order_types` optionnel qui classe les commandes en `ubereats`/`deliveroo`/`scannorder`/`restaurant`.

**Ce que ces filtres excluent réellement** :
- `o.state <> 'CLOSED'` → toute commande non finalisée exclue.
- `o.brand_status IN ('DELETED', 'CANCELED')` → commandes supprimées ou annulées exclues.
- `o.created_by IN ('-1', 'SCANNORDER')` → commandes créées par le canal ScanNOrder ou par un identifiant système exclues de **ce** rapport (comptabilisées ailleurs).
- `o.brand <> 'WELLO_RESTO'` → commandes Uber Eats / Deliveroo exclues de ce rapport spécifique.
- `tva.show_in_report` → catégories de TVA marquées comme hors reporting exclues.

**Aucun de ces filtres ne porte sur un flag "test" ou "formation"** — parce que cette notion n'existe pas dans le modèle (§4.4).

Un mécanisme distinct, dédié au rapport de "réel" de caisse (`GetTrustedEnclosedRegisterIDs` / `GetRealPaymentsData`, `internal/modules/pos/accounting/repository.go:359-608`), exclut des **registres de caisse entiers** dont l'instantané figé à la clôture diverge d'un recalcul live des paiements, et exclut les codes MOP `STRIPE`/`UBER_EATS`/`DELIVEROO` (canaux hors périmètre du rapport WELLO_RESTO).

### Synthèse des faits marquants — Section 4

1. **Quatre chaînages de hash indépendants** (`cash_registers`, `orders`, `payments`, `receipts`), chacun avec sa propre requête "dernier hash" et sa propre formule de payload — pas un système unifié.
2. **Deux numérotations différentes coexistent** : `orders.order_num` (affichage client, boucle 1→99, **pas continue**) et `receipts.receipt_number` (`F-YYYY-NNNNNN`, séquentielle, remise à `000001` chaque année civile).
3. **Aucune séquence fiscale n'est initialisée à la création du marchand** — tout est amorcé au premier événement réel (lazy init), avec un marqueur de genèse explicite (`"GENESIS_HASH"`) uniquement pour la chaîne `cash_registers`.
4. **Aucun jalon "mise en service" / "go live"** n'existe ; `merchant.is_active` est un simple flag actif/inactif à `true` par défaut dès la création.
5. **Aucun mode formation/test/école** n'existe dans le modèle de données ou le code.
6. **Aucune attestation de conformité NF525** n'est générée par le système ; "NF525" n'apparaît que comme référence en commentaire de code.
7. Les exports comptables excluent uniquement les commandes non `CLOSED`, `CANCELED`/`DELETED`, hors canal `WELLO_RESTO`, ou créées par `SCANNORDER`/`'-1'` — jamais sur la base d'un flag test/formation, qui n'existe pas.
8. Les tables fiscales (`cash_registers`, `payments`, `orders`.hash/signature/previous_hash, `receipts`) n'ont **aucune trace dans `migrations/`** — leur DDL n'est visible que dans le dump `docs/migration-postgres/04-schema-postgres-target.sql`, généré pour le chantier de portage MySQL→Postgres.
---

## 5. Abonnement et facturation

### 5.1. Gestion de l'abonnement du marchand à la plateforme (par opposition aux paiements clients finaux)

**Réponse courte : un embryon de mécanisme existe** (tables `subscriptions` / `packages`, colonnes `stripe_subscription_id` / `stripe_price_id`, webhooks `invoice.created` / `invoice.paid`), **mais dans le code Go actuel il ne sert QUE de mécanisme de feature-flags** (droits d'accès aux modules), et non de facturation réelle. Aucun appel Go vers l'API Stripe Billing/Subscriptions n'a été trouvé. Le reste du Stripe présent dans le code (Checkout, PaymentIntents, Connect, Terminal) sert exclusivement aux paiements des clients finaux du restaurant.

**Tables SQL concernées** — schéma legacy, `data-migration/migration_welloresto_data.sql:614945-614957` (`subscriptions`) :
```sql
CREATE TABLE `subscriptions` (
  `id` int(11) NOT NULL,
  `stripe_subscription_id` varchar(150) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `package_id` int(11) NOT NULL,
  `planning_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `haccp_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `stock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `scannorder_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `bookings_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `kiosks_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `max_kiosks` int(11) NOT NULL DEFAULT 0 COMMENT 'Nombre max de bornes actives (0 = module non inclus)'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
```
et `packages` (`data-migration/migration_welloresto_data.sql:521071-521087`) :
```sql
CREATE TABLE `packages` (
  `id` int(11) NOT NULL,
  `package_name` varchar(50) NOT NULL,
  `stripe_price_id` varchar(200) NOT NULL,
  `trial_period_days` int(11) NOT NULL DEFAULT 0,
  `scannorder_ready` tinyint(1) NOT NULL DEFAULT 1,
  `stock_management` int(11) NOT NULL DEFAULT 0,
  `hr_management` tinyint(1) NOT NULL DEFAULT 0,
  `planning_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `haccp_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `stock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `scannorder_enabled` tinyint(1) NOT NULL DEFAULT 1,
  `bookings_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `kiosks_enabled` tinyint(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
```
Exemple de données historiques — les `stripe_price_id` sont de vrais IDs Stripe Price :
```sql
INSERT INTO `packages` (`id`, `package_name`, `stripe_price_id`, `trial_period_days`, ...) VALUES
(0, 'Developpers', 'price_1NEBOnIpOVvvxHBEfl559NgB', 0, 1, 1, 1, 1, 1, 0, 0, 0, 1, 0, 0),
(1, 'Essentiel', 'price_1NE6nJIpOVvvxHBENVfwdfCD', 30, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0),
(3, 'Standard', 'price_1NE6oTIpOVvvxHBEQpkOTN6t', 30, 1, 1, 1, 0, 1, 0, 0, 0, 1, 0, 0),
(4, 'Premium', 'price_1NE6p7IpOVvvxHBEz27AtHgc', 30, 1, 1, 1, 1, 1, 0, 0, 0, 1, 0, 0),
(5, 'Association', 'price_1OqAszIpOVvvxHBEVLt2jink', 0, 1, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0),
...
```

Certaines lignes de `subscriptions` (legacy) portent bien un vrai `stripe_subscription_id`, preuve qu'un système antérieur (probablement le PHP historique, hors périmètre de ce repo Go) créait réellement des abonnements Stripe :
```sql
(68, 'sub_1NECHpIpOVvvxHBExBjX0eUA', 173, 0, 0, 0, 0, 1, 0, 0, 0),
(80, 'sub_1Oork2IpOVvvxHBElvAQM32S', 196, 5, 0, 0, 0, 1, 0, 0, 0),
(84, 'sub_1OqBP7IpOVvvxHBEAPwjPgR2', 203, 1, 0, 0, 0, 1, 0, 0, 0),
(86, 'sub_1PIEYYIpOVvvxHBEpUS6wuav', 212, 101, 0, 1, 0, 1, 0, 1, 2),
```

Une seconde table, `subscription_invoices`, sert d'historique de facturation d'abonnement :
```sql
CREATE TABLE `subscription_invoices` (
  `id` int(11) NOT NULL,
  `merchant_id` int(11) NOT NULL,
  `invoice_id` varchar(50) NOT NULL,
  `status` int(11) NOT NULL DEFAULT 0 COMMENT '0 => open, 1 => paid, -1 => error',
  `invoice_date` timestamp NOT NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  `amount` int(11) NOT NULL COMMENT 'in cents',
  `payment_date` timestamp NULL DEFAULT NULL,
  `comment` varchar(150) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
```
Et une troisième table, `welloresto_stripe_customers`, fait le lien `merchant_id -> stripe_customer_id`.

Une migration (`migrations/done/019_add_subscription_feature_flags.sql`) confirme explicitement que ces colonnes sont utilisées comme des **droits d'accès aux modules**, et non comme des montants facturés :
```sql
ALTER TABLE packages
    ADD COLUMN planning_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN haccp_enabled TINYINT(1) NOT NULL DEFAULT 1,
    ADD COLUMN stock_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN scannorder_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN bookings_enabled TINYINT(1) NOT NULL DEFAULT 1;

ALTER TABLE subscriptions
    ADD COLUMN planning_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN haccp_enabled TINYINT(1) NOT NULL DEFAULT 1,
    ADD COLUMN stock_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN scannorder_enabled TINYINT(1) NOT NULL DEFAULT 0,
    ADD COLUMN bookings_enabled TINYINT(1) NOT NULL DEFAULT 1;

-- Bootstrap package defaults from the current behavior so the migration does not disable features unexpectedly.
UPDATE packages
SET planning_enabled = hr_management,
    haccp_enabled = TRUE,
    stock_enabled = CASE WHEN stock_management > 0 THEN TRUE ELSE FALSE END,
    scannorder_enabled = scannorder_ready,
    bookings_enabled = TRUE;

-- Copy package defaults to the merchant subscriptions. From this point on, subscription values are the effective rights.
UPDATE subscriptions s
LEFT JOIN packages p ON p.id = s.package_id
SET s.planning_enabled = COALESCE(p.planning_enabled, FALSE),
    s.haccp_enabled = COALESCE(p.haccp_enabled, TRUE),
    s.stock_enabled = COALESCE(p.stock_enabled, FALSE),
    s.scannorder_enabled = COALESCE(p.scannorder_enabled, FALSE),
    s.bookings_enabled = COALESCE(p.bookings_enabled, TRUE);
```

**Ce que fait le code Go actuel à la création d'un marchand** : à la création (`POST /pos/create`), le code insère une ligne `subscriptions` mais **n'appelle jamais l'API Stripe** ; `stripe_subscription_id` est explicitement forcé à une chaîne vide.

`internal/modules/pos/create_repository.go:38-55` (intégral) :
```go
// InsertSubscription creates the effective merchant subscription for the selected package.
func (r *POSRepository) InsertSubscription(ctx context.Context, merchantID, packageID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// stripe_subscription_id est NOT NULL sans défaut : MySQL non-strict
	// insérait '' silencieusement, Postgres rejette — '' explicite pour un
	// résultat identique dans les deux dialectes.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id) VALUES (?, ?, '')`,
		merchantID, packageID,
	); err != nil {
		log.Error("InsertSubscription: failed to insert subscription: " + err.Error())
		return err
	}

	return nil
}
```
Appelé depuis `internal/modules/pos/create_service.go:34-37`. `req.PackageID` provient directement du payload JSON envoyé par le client, **sans validation contre la table `packages`**.

Aucune fonction n'existe pour modifier ensuite le package/l'abonnement d'un marchand existant (aucune occurrence de « UpdateSubscription », « ChangePackage », « UpgradePlan », ni de `UPDATE ... SET package_id`) : le choix du package est figé à la création et n'est jamais recalculé ni facturé par la suite dans le code Go.

**Utilisation actuelle des tables `subscriptions`/`packages`** : lues uniquement pour attacher des **drapeaux de droits d'accès** au retour de login (jointure `LEFT JOIN subscriptions ... LEFT JOIN packages`), jamais pour un calcul de facturation. Répétée à l'identique dans `internal/modules/auth/repository.go:142-158,334,750`, `internal/modules/users/repository.go:249`, et utilisée pour filtrer les marchands actifs dans les tâches cron (`internal/tasks/orders.go:31`, `internal/tasks/products.go:29`, `internal/tasks/upsell.go:40`). Le quota de bornes Kiosk est aussi lu depuis `subscriptions` (`internal/modules/kiosk/repository.go:365-369`, `SELECT max_kiosks FROM subscriptions WHERE merchant_id = ?`).

**Le webhook « invoice.created »/« invoice.paid » : code présent, mais jamais alimenté par le code Go actuel.** Le service webhook Stripe écrit dans `subscription_invoices` (`internal/webhook/stripe/repository.go:350-379`) :
```go
func (r *mysqlRepo) CreateInvoice(cdb context.Context, merchantID, invoiceID string, amount int64, created int64, customerID string) error {
	db := dbx.GetDB(cdb, r.database)

	epochExpr := "FROM_UNIXTIME(?)"
	if dbx.ActiveDialect() == dbx.Postgres {
		epochExpr = "to_timestamp(?)"
	}
	query := fmt.Sprintf(`INSERT INTO subscription_invoices(merchant_id, invoice_id, invoice_date, amount)
			  SELECT ?, ?, %s, ?
			  FROM welloresto_stripe_customers WHERE stripe_customer_id = ?`, epochExpr)
	_, err := db.ExecContext(cdb, query, merchantID, invoiceID, created, amount, customerID)
	return err
}

func (r *mysqlRepo) PayInvoice(cdb context.Context, invoiceID string, paidAt int64) error {
	db := dbx.GetDB(cdb, r.database)

	epochExpr := "FROM_UNIXTIME(?)"
	if dbx.ActiveDialect() == dbx.Postgres {
		epochExpr = "to_timestamp(?)"
	}
	query := fmt.Sprintf(`UPDATE subscription_invoices SET status = '1', payment_date = %s WHERE invoice_id = ?`, epochExpr)
	_, err := db.ExecContext(cdb, query, paidAt, invoiceID)
	return err
}
```
Mais **aucun code Go du repo n'insère de ligne dans `welloresto_stripe_customers`** (seule référence hors test est le `SELECT` ci-dessus, et une seed dans un test d'intégration). Le `INSERT INTO subscription_invoices ... SELECT ... FROM welloresto_stripe_customers WHERE stripe_customer_id = ?` ne produit donc une ligne que si le `stripe_customer_id` existe déjà dans cette table — table jamais peuplée par le code Go actuel. Aucun appel `sub.New(...)`, `customer.New(...)` ou tout usage du package Stripe Billing/Subscriptions n'a été trouvé dans le repo.

**Conclusion factuelle** : le mécanisme d'abonnement plateforme existe au niveau du schéma SQL et du gestionnaire de webhook, mais dans l'état actuel du code Go, il n'y a **aucun point d'entrée qui crée réellement une facturation/un abonnement Stripe côté plateforme** — le `package_id` sélectionné à la création du marchand ne sert qu'à activer des drapeaux fonctionnels, avec `stripe_subscription_id` toujours vide dans ce flux. Tout le reste du code Stripe du repo (Checkout Sessions, PaymentIntents, Connect, Terminal) concerne exclusivement l'encaissement des commandes des clients finaux du restaurant, via Stripe Connect.

### 5.2. Séparation compte Stripe PLATEFORME vs comptes Stripe CONNECTÉS

**Chargement des clés — une seule clé API pour toute la plateforme.** `internal/config/stripe.go` (intégral) :
```go
package config

import (
	"os"
)

type StripeConfig struct {
	APIKey string
	OnboardingReturnURL string
	OnboardingRefreshURL string
}

func loadStripeConfig() StripeConfig {
	return StripeConfig{
		APIKey:               os.Getenv("STRIPE_API_KEY"),
		OnboardingReturnURL:  os.Getenv("STRIPE_ONBOARDING_RETURN_URL"),
		OnboardingRefreshURL: os.Getenv("STRIPE_ONBOARDING_REFRESH_URL"),
	}
}
```
Il n'existe qu'**une seule variable d'environnement de clé Stripe** (`STRIPE_API_KEY`) — pas de clé séparée pour un compte « plateforme » distinct d'un usage Connect ; le même client Stripe sert aux deux usages. Instancié une fois dans `cmd/api/routes.go:260` :
```go
stripeManager := stripeInternalClient.NewStripeManager(cfg.Stripe.APIKey)
```
et réutilisé partout (Terminal, POS, ScanNOrder, Integrations, Webhook).

**Distinction concrète plateforme vs Connect : le paramètre `Stripe-Account`.** La séparation ne se fait **pas** par une clé API différente, mais uniquement par l'appel ou non de `params.SetStripeAccount(accountID)` sur chaque appel API.

Appels scopés sur un compte connecté (paiements clients finaux) — `internal/infrastructure/stripe/checkout.go:113-147` :
```go
	params := &stripe.CheckoutSessionParams{
		LineItems:  lineItems,
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		ExpiresAt:  stripe.Int64(time.Now().Add(30 * time.Minute).Unix()),
		Metadata: map[string]string{
			"order_id":              orderID,
			"merchant_id":           fmt.Sprintf("%v", merchant.MerchantID),
			"checkout_session_type": sessionType,
		},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			ApplicationFeeAmount: stripe.Int64(fees),
			CaptureMethod:        stripe.String(string(captureMethod)),
		},
	}

	params.SetStripeAccount(*merchant.AccountID)

	return c.client.CheckoutSessions.New(params)
```
(`fees` = commission WelloResto, prélevée via `ApplicationFeeAmount`.) Idem pour `CaptureExistingPaymentAsync`, `RefundOrCancelAsync`, `GetConnectBalance` (`internal/infrastructure/stripe/service.go` et `connect.go`).

Appels **non scopés** (contexte plateforme) : `ProcessPaymentAsync`, `RefundAsync`, et côté webhook `HandleInvoiceCreated`/`HandleInvoicePaid` — aucun `SetStripeAccount`, cohérent avec des objets `Invoice` de facturation côté compte plateforme.

**Onboarding Connect** via `STRIPE_ONBOARDING_RETURN_URL`/`STRIPE_ONBOARDING_REFRESH_URL` : `CreateExpressAccount` (`internal/infrastructure/stripe/connect.go:59-79`), `CreateOnboardingLink` (lignes 44-57), utilisés par `internal/modules/integrations/service.go:361-416` (`CreateStripeOnboardingLink`, `CreateScanNOrderOnboarding`).

**Constat additionnel** : `go.mod` déclare deux versions majeures différentes du SDK Stripe en parallèle — `github.com/stripe/stripe-go/v78 v78.12.0` (utilisée uniquement par `internal/webhook/stripe/service.go`) et `github.com/stripe/stripe-go/v84 v84.2.0` (utilisée par `internal/infrastructure/stripe/*.go`).

### 5.3. Webhooks Stripe traités + état de la vérification de signature

**Aiguillage des événements** — `internal/webhook/stripe/service.go:53-94`, `ProcessEvent` (intégral) :
```go
func (s *StripeWebhookService) ProcessEvent(ctx context.Context, event StripeEvent) error {
	switch event.Type {

	case "checkout.session.completed":
		return s.HandleCheckoutSessionCompleted(ctx, event.Data.Object)

	case "checkout.session.expired":
		return s.HandleCheckoutSessionCanceled(ctx, event.Data.Object)

	case "charge.refunded":
		return s.HandleRefund(ctx, event.Data.Object)

	case "charge.captured":
		return s.HandleRetrieveFees(ctx, event.Data.Object, event.Account)

	case "payment_intent.canceled":
		return s.HandlePaymentIntentUpdated(ctx, event.Data.Object, "CANCELED", event.Account)

	case "payment_intent.succeeded":
		return s.HandlePaymentIntentSucceeded(ctx, event.Data.Object, event.Account)

	case "payment_intent.payment_failed":
		return s.HandlePaymentIntentFailed(ctx, event.Data.Object, event.Account)

	case "payout.paid":
		return s.HandlePayoutPaid(ctx, event.Data.Object, event.Account)

	case "invoice.created":
		return s.HandleInvoiceCreated(ctx, event.Data.Object)

	case "invoice.paid":
		return s.HandleInvoicePaid(ctx, event.Data.Object)

	case "account.updated":
		return s.HandleAccountUpdated(ctx, event.Data.Object)

	default:
		return nil
	}
}
```

**Liste exhaustive des 11 events gérés :**

| Event Stripe | Handler | Ce qu'il déclenche |
|---|---|---|
| `checkout.session.completed` | `HandleCheckoutSessionCompleted` | Insertion paiement, mise à jour commande, notification, email/SMS de confirmation, auto-accept |
| `checkout.session.expired` | `HandleCheckoutSessionCanceled` | `SetOrderDenied` (session expirée/annulée) |
| `charge.refunded` | `HandleRefund` | Désactive le paiement, email de remboursement |
| `charge.captured` | `HandleRetrieveFees` | Récupère le détail des frais Stripe (balance transaction) |
| `payment_intent.canceled` | `HandlePaymentIntentUpdated` | `UPDATE stripe_payments SET payment_intent_status = 'CANCELED'` |
| `payment_intent.succeeded` | `HandlePaymentIntentSucceeded` | Confirmation paiement Terminal Kiosk, ou `UPDATE ... CAPTURED` |
| `payment_intent.payment_failed` | `HandlePaymentIntentFailed` | Marque `FAILED` (uniquement paiements kiosk) |
| `payout.paid` | `HandlePayoutPaid` | Email « virement effectué » |
| `invoice.created` | `HandleInvoiceCreated` | Insertion `subscription_invoices` (conditionnée à l'existence du `stripe_customer_id`) |
| `invoice.paid` | `HandleInvoicePaid` | `subscription_invoices.status = '1'` |
| `account.updated` | `HandleAccountUpdated` | `stripe_accounts.verification_status`, active `scannorder_settings.activated` |

Tout autre event reçu (`default:`) est silencieusement ignoré, sans log ni erreur.

**État EXACT de la vérification de signature du webhook : la vérification de signature est absente à l'exécution.**

`internal/webhook/stripe/http_handler.go` (intégral) :
```go
package stripe

import (
	"encoding/json"
	"io"
	"net/http"
)

type Handler struct {
	service *StripeWebhookService
}

func NewHandler(s *StripeWebhookService) *Handler {
	return &Handler{service: s}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	var event StripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	if err := h.service.ProcessEvent(r.Context(), event); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(http.StatusOK)
}
```
Une méthode `VerifySignature` existe bien sur le service, mais c'est un **stub vide, jamais appelé nulle part dans le code** :
```go
func (s *StripeWebhookService) VerifySignature(ctx context.Context, header http.Header, body []byte) {
	// A implémenter avec webhook.ConstructEvent de la lib stripe-go
}
```
La route est enregistrée sans aucun middleware (contrairement à `/external` qui a `r.Use(authMiddleware)`) — `cmd/api/routes.go:555-564` :
```go
	r.Route("/webhooks", func(r chi.Router) {
		r.Post("/uber-eats", uberWebhookHandler.HandleWebhook)
		r.Post("/deliveroo/orders", deliverooWebhookHandler.HandleOrdersWebhook)
		r.Post("/deliveroo/menu", deliverooMenuWebhookHandler.HandleMenuWebhook)
		r.Get("/deliveroo/menu", deliverooMenuWebhookHandler.HandleMenuWebhook)
		r.Post("/stripe", stripeWebhookHandler.HandleWebhook)
		r.Post("/brevo/sms-reply", brevoSMSReplyHandler.HandleWebhook)
		r.Post("/brevo/events", brevoEventsHandler.HandleWebhook)
	})
```

Confirmations complémentaires : aucune variable d'environnement `STRIPE_WEBHOOK_SECRET` n'existe ; aucun appel à `webhook.ConstructEvent` nulle part ; aucune lecture de l'en-tête `Stripe-Signature`. Par comparaison, le webhook Uber Eats du même repo vérifie effectivement une signature (`internal/webhook/ubereats/handler/http_handler.go:28`, `internal/webhook/ubereats/service/service.go:128-130`) — le pattern existe ailleurs dans le codebase mais n'a pas été implémenté (au-delà du stub vide) pour Stripe.

**Conséquence factuelle** : n'importe quel tiers connaissant l'URL `POST /webhooks/stripe` peut soumettre un JSON arbitraire imitant un `StripeEvent` et déclencher les effets métier associés, puisqu'aucune vérification cryptographique de provenance Stripe n'est effectuée dans l'état actuel du fichier.

### 5.4. Mandat SEPA / prélèvement bancaire européen

**N'existe pas.**

Recherche exhaustive insensible à la casse sur l'ensemble du code Go (`sepa|iban|mandate`) : aucune occurrence de « SEPA » ou « mandate ». La seule occurrence d'« IBAN » est un commentaire de documentation d'une fonction Stripe Connect, sans rapport avec un mandat de prélèvement — configuration du compte bancaire du marchand pour **recevoir** ses virements Stripe Connect (payouts), pas un prélèvement SEPA effectué par la plateforme :
```go
// CreateStripeBankAccountLink generates an account_update link for the merchant to configure IBAN.
func (s *Service) CreateStripeBankAccountLink(ctx context.Context, merchantID string) (string, error) {
```
Aucun `PaymentMethodType` de type `sepa_debit`, aucun `stripe.SetupIntent`, aucune structure ou table liée à un mandat de prélèvement n'a été trouvée dans le code Go, les migrations SQL, ni le dump de données legacy.
---

## 6. Produits et création en masse

### 6.1. Interface de création de produits en masse dans le back-office

**Point d'entrée UI** : `wello-back-office/src/pages/Menu.tsx`, menu déroulant à côté du bouton de création de produit (lignes 335-368) :
```tsx
<DropdownMenuItem
  onClick={() => {
    setImportInitialDoor('manual');
    setImportOpen(true);
  }}
>
  <CopyPlus className="w-4 h-4 mr-2" />
  Créer plusieurs produits
</DropdownMenuItem>
<DropdownMenuItem
  onClick={() => {
    setImportInitialDoor(undefined);
    setImportOpen(true);
  }}
>
  <Upload className="w-4 h-4 mr-2" />
  Importer des produits
</DropdownMenuItem>
```
Les deux entrées ouvrent le même composant, `ProductImportDialog` (`wello-back-office/src/components/menu/import/ProductImportDialog.tsx:619-627`), avec ou sans porte pré-sélectionnée. Le composant (lignes 89-209) est une modale à étapes (`choose` → `provider`/`manual` → `preview` → `done`), pilotée par le hook `useProductImport`.

**Endpoints API appelés** (tous sous `/menu`, gate RBAC `permission.CatalogManage` — seul bloc de `/menu` à porter un contrôle RBAC explicite, `cmd/api/routes.go:801-811`) :
```go
r.With(middleware.RequirePermission(permission.CatalogManage)).
    Post("/import/preview", menuImportH.PreviewImport)
r.With(middleware.RequirePermission(permission.CatalogManage)).
    Post("/import/commit", menuImportH.CommitImport)
r.With(middleware.RequirePermission(permission.CatalogManage)).
    Get("/import/template", menuImportH.DownloadImportTemplate)
```

**Format exact du payload** — deux formats acceptés sur la **même route** `POST /menu/import/preview`, distingués par le `Content-Type` (`internal/modules/menu/import_handler.go:28-56`) :
- **multipart/form-data** (porte fichier) : deux champs, `provider` (string) et `file` (classeur `.xlsx`), 5 Mo max.
- **application/json** (porte saisie manuelle) :
```go
// internal/modules/menu/import_models.go:16-45
type ImportPreviewJSONRequest struct {
    Provider string                     `json:"provider"`
    Products []ImportPreviewJSONProduct `json:"products"`
}

type ImportPreviewJSONProduct struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Category    string `json:"category"`

    PriceIn       int `json:"price"`
    PriceTakeAway int `json:"price_take_away"`
    PriceDelivery int `json:"price_delivery"`

    TvaRateIn       *float64 `json:"tva_in"`
    TvaRateTakeAway *float64 `json:"tva_take_away"`
    TvaRateDelivery *float64 `json:"tva_delivery"`

    Tags []string `json:"tags"`
}
```
Côté front, ce payload est construit par `buildManualPayload` (`wello-back-office/src/lib/manualImport.ts:193-208`), envoyé via `menuImportService.previewFromManual`.

**Rien de tout cela n'écrit en base.** `/menu/import/preview` calcule un dry-run et rend un `token` (TTL Redis). La seule route qui écrit est `POST /menu/import/commit`, avec en corps `{ token, decisions }`, matérialisée en une seule transaction (`MaterializeImportTx`).

À noter : `BulkEditDialog.tsx` et `BulkAssignProductsDialog.tsx` portent aussi le mot « bulk », mais **ne créent pas de produits** : le premier édite en masse des produits existants, le second assigne des produits existants à une catégorie (`PATCH /menu/products/categories/{category_id}/bulk-assign`).

### 6.2. Création de catégories en masse

**N'existe pas.** Aucune route, aucun composant, aucun mécanisme d'import de fichier pour les catégories. La création de catégorie est **strictement unitaire**, via deux points d'entrée UI redondants qui appellent la même fonction :
- `wello-back-office/src/pages/CategoriesTable.tsx:573-600` — dialogue inline avec un seul `<Input>` (nom).
- `wello-back-office/src/components/menu/CreateProductCategoryDialog.tsx:42-120` — composant dédié réutilisé depuis `Menu.tsx`, même principe : un `Input` unique, un bouton `Créer`.

Les deux appellent `menuService.createProductCategory(name: string)` :
```ts
async createProductCategory(name: string): Promise<{ id: string; name: string; order: number }> {
  logAPI('POST', '/menu/products/categories', { name });
  return withMock(
    () => ({ id: `cat_${Date.now()}`, name, order: 99 }),
    async () => {
      const response = await apiClient.post<WelloApiResponse<{ category_id: string; message: string; status: string }>>('/menu/products/categories', { name });
      ...
```
Côté API : `POST /menu/products/categories` → `MenuHandler.CreateProductCategory`, avec un payload qui n'accepte qu'**un seul nom**, pas de tableau :
```go
// internal/modules/menu/models.go:328-331
type CreateProductCategoryPayload struct {
    Name       string `json:"name"`
    MerchantID string `json:"-"`
}
```
Aucune variante « array of names » ou multipart n'existe pour cette route.

### 6.3. Les « trois portes » d'import de produits

Constat préalable : **il n'existe pas d'import depuis un catalogue fournisseur externe** au sens « distributeur alimentaire ». Ce que le code appelle « porte fichier » est en réalité un import depuis un **logiciel de caisse tiers** (un seul provider concret : Zelty) ou depuis le **modèle Wello lui-même réimporté**. `ImportDoorPicker.tsx:19-86` :
```tsx
<h3 className="font-semibold">J'importe depuis ma caisse actuelle</h3>
<p className="text-sm text-muted-foreground">
  Vous avez un export de votre logiciel de caisse, ou un modèle Wello déjà rempli ?
  Envoyez-le, nous vous montrerons ce qui sera créé avant d'enregistrer quoi que ce soit.
</p>
...
<h3 className="font-semibold">Je pars d'un modèle vierge</h3>
<p className="text-sm text-muted-foreground">
  Téléchargez notre fichier Excel, remplissez-le tranquillement, puis revenez
  l'importer par la première porte en choisissant « Modèle Wello Resto rempli ».
</p>
...
<h3 className="font-semibold">Je saisis mes produits à la main</h3>
```
Autrement dit, **la porte 2 (« modèle personnalisé ») n'est pas un canal d'écriture séparé** : c'est un simple téléchargement de fichier vierge, qui doit ensuite être réimporté par la porte 1. Il y a donc bien trois portes *fonctionnelles* pour l'utilisateur, mais seulement **deux mécanismes techniques distincts** côté API (upload fichier vs JSON manuel).

**a) Porte « fichier » (caisse actuelle / modèle rempli)**
- Frontend : `ImportProviderStep.tsx` — sélecteur de provider (`IMPORT_PROVIDERS`, `wello-back-office/src/types/import.ts:27-40`) :
```ts
export type ImportProviderSlug = 'zelty' | 'wello-generic';
export const IMPORT_PROVIDERS: ImportProviderOption[] = [
  { slug: 'wello-generic', label: 'Modèle Wello Resto rempli', description: 'Le modèle vierge téléchargé ici, une fois complété', hasTemplate: true },
  { slug: 'zelty', label: 'Zelty', description: "Export de menu au format Excel produit par Zelty", hasTemplate: false },
];
```
Validation frontend : format `.xlsx`, taille ≤ 5 Mo.
- Backend : `internal/modules/menu/import_handler.go:79-99` (`previewFromMultipart`) → `ImportService.PreviewImportFile` → `importer.Registry.Get(slug)` (deux providers : `NewZeltyProvider()`, `NewWelloGenericProvider()`).
- Format d'entrée : Modèle Wello — colonnes `Nom*`, `Description`, `Catégorie*`, `Prix sur place*`, `Prix emporté`, `Prix livraison`, `TVA sur place`, `TVA emporté`, `TVA livraison`, `Tags` (colonnes obligatoires : Nom/Catégorie/Prix sur place). Zelty — fichier mono-feuille de 8 colonnes (`ID, Type, Nom, Prix, TVA, TVA emporte, TVA livraison, Tags`), routé par la colonne `Type` (`Tag`/`Produit`/`Option`/`Option Value`).
- Validations backend (`internal/modules/menu/importer/values.go:23-84`, `parsePriceCents` ; lignes 89-103, `parseTvaRate`) rejettent tout format invalide avec l'erreur de ligne précise. Colonnes requises vérifiées explicitement (`wello_generic.go:239-245`).

**b) Porte « modèle personnalisé » (template téléchargeable)**
- Frontend : bouton « Télécharger le modèle » → `GET /menu/import/template?provider=wello-generic`.
- Backend : `ImportHandler.DownloadImportTemplate` → `WelloGenericProvider.BuildTemplate` (`internal/modules/menu/importer/template.go:149-220`), génère un classeur `.xlsx` avec `excelize`.
- Ce fichier une fois rempli est réinjecté **par la porte a)** — pas de route ou de logique de parsing dédiée à cette « porte ».

**c) Porte « saisie de masse » (grille en direct)**
- Frontend : `ImportManualStep.tsx` — tableau de lignes (`ManualRow`), une ligne par produit. Les taux de TVA proposés viennent de `menuService.getTvaRates()` (référentiel réel du marchand, pas de saisie libre).
- Validation frontend (`wello-back-office/src/lib/manualImport.ts:114-179`) : nom requis + unicité, catégorie requise, montants numériques, TVA requise pour les 3 canaux.
- Backend : `ImportHandler.previewFromJSON` → `ImportService.PreviewImportManual` → `importer.BuildManualImport` (`internal/modules/menu/importer/manual.go:49-117`), qui revalide nom non vide, unicité, taux non négatifs.

**Étape de prévisualisation — commune aux trois portes.** **Oui, elle existe**, et c'est un point de passage obligé (« Rien n'est enregistré à cette étape », « Rien n'est enregistré tant que vous n'avez pas validé »). L'écran `ImportReviewStep.tsx` affiche, avant tout commit : compteurs (produits à créer, catégories, tags, groupes d'options), classification tags/catégories, résolution des taux de TVA, produits sans catégorie, collisions de nom, produits déjà importés, avertissements — avec un bouton `Importer N produit(s)` désactivé tant que `precheck.canCommit` est faux.

Côté backend, le commit est **rejoué et revalidé intégralement** (rien n'est cru sur parole du client) par `BuildCommitPlan` (`internal/modules/menu/importer/commit_plan.go:149-174`), qui refuse en HTTP 422 avec la liste des `blockers` (`product_needs_category`, `tva_rate_unresolved`, `product_name_collision_unresolved`, `invalid_tva_mapping`, `invalid_category_decision`) **sans écrire une seule ligne** tant qu'il en reste un.

### 6.4. Saisie et stockage du taux de TVA sur un produit

**Le taux de TVA n'est pas une colonne numérique libre.** C'est une référence (`tva_id`) vers une table de taux disponibles, avec libellé et description, propre au marchand et au canal de vente.

**Schéma** (`staging_schema_dump.sql:5749-5760`, cible Postgres de la migration en cours ; aucune migration créant ou seedant cette table n'a été trouvée dans `migrations/` — la table préexiste au dossier de migrations tracké) :
```sql
CREATE TABLE public.tva_categories (
    tva_id integer NOT NULL,
    delivery_type character varying(20) NOT NULL,
    tva_title character varying(30) NOT NULL,
    tva_desc character varying(150) NOT NULL,
    tva_rate real NOT NULL,
    show_in_report boolean DEFAULT true NOT NULL,
    enabled boolean DEFAULT true NOT NULL
);

COMMENT ON COLUMN public.tva_categories.delivery_type IS '0 => in, 1 => delivery, 3=> take away (2 not used because 2 is SNO is "isDelivery" field or orders)';
COMMENT ON COLUMN public.tva_categories.tva_rate IS 'in percent (5 => 5%)';
```
Le commentaire SQL sur `delivery_type` annonce des valeurs numériques mais **c'est faux** : les données réelles portent les chaînes `IN` / `TAKE_AWAY` / `DELIVERY`. Aucun seed par défaut n'est présent dans les fichiers de migration de ce dépôt ; les seules valeurs visibles dans le code sont celles des tests d'intégration, qui ne sont pas des données de production.

**Exposition API** — `GET /pos/tva_rates` (jointure avec la table `labels` pour le nom traduit du canal) :
```go
// internal/modules/pos/repository.go:292-313
func (r *POSRepository) GetTVARates(ctx context.Context, merchantID string) ([]ConsumptionType, error) {
    ...
    query := `
        SELECT
            ` + posCastChar("l.id") + ` as type_id,
            t.delivery_type,
            l.label_value,
            l.label as type_name,
            ` + posCastChar("t.tva_id") + ` as rate_id,
            t.tva_title,
            t.tva_desc,
            t.tva_rate
        FROM labels l
        INNER JOIN tva_categories t ON l.label_value = t.delivery_type
        WHERE l.label_type = 'order_type'
          AND l.lang = 'FR'
          AND t.enabled = TRUE
        ORDER BY l.id ASC, t.tva_rate ASC`
```
```go
// internal/modules/pos/models.go:55-68
type Rate struct {
    ID    string  `json:"id"`
    Value float64 `json:"value"`
    Label string  `json:"label"`
    Description string `json:"description"`
}
type ConsumptionType struct {
    ID           string `json:"id"`
    Name         string `json:"name"`
    DeliveryType string `json:"delivery_type"`
    Rates        []Rate `json:"rates"`
}
```

**Sur la fiche produit**, le taux n'est jamais tapé : il est choisi dans un menu déroulant alimenté par ce référentiel (`wello-back-office/src/components/menu/SimpleProductSheet.tsx:130-160`, `TvaRateSelect`). Le produit stocke un `tva_id` par canal :
```go
// internal/modules/menu/models.go:219-221
TvaInID             string  `json:"tva_in_id"`
TvaDeliveryID       string  `json:"tva_delivery_id"`
TvaTakeAwayID       string  `json:"tva_take_away_id"`
```
Dans le pipeline d'import (§6.3), le fichier/la saisie manuelle transporte un **taux brut** (pourcentage), jamais un `tva_id` — c'est la prévisualisation puis le commit qui résolvent ce taux vers un `tva_id` réel du référentiel `tva_categories` du marchand, avec repli sur le taux le plus bas si `0` est fourni, et blocage (`tva_rate_unresolved`) si aucun taux correspondant n'existe chez le marchand.
---

## 7. Paramètres marchand

*Note méthodologique : l'API n'a pas de module `internal/modules/settings/` ni `internal/modules/merchant/` dédié. La gestion des paramètres marchand est portée par le module `internal/modules/pos/` (fichiers `create_*.go`, `repository.go`, `service.go`, `handler.go`), qui lit/écrit directement les tables `merchant` et `merchant_parameters` (+ tables satellites). Le schéma SQL de référence croise deux sources : le dump MySQL réel `docs/migration-postgres/wello-resto-mysql-ddl.md` (généré le 13/07/2026) et les fichiers `migrations/done/*.sql` qui s'appliquent séquentiellement par-dessus.*

### 7.1. Table(s) de paramétrage d'un marchand

Deux tables portent l'essentiel des paramètres : `merchant` (identité/coordonnées — structure complète en Section 1.1) et `merchant_parameters` (comportement métier, PK = `merchant_id`, relation 1-1 avec `merchant`).

**`merchant_parameters`** (`docs/migration-postgres/wello-resto-mysql-ddl.md:1862-1915`, complétée par `migrations/done/086_merchant_parameters_pos_covers_count_required.up.sql:17-18`) :
```sql
CREATE TABLE `merchant_parameters` (
  `merchant_id` int(11) NOT NULL,
  `manage_on_site` tinyint(1) NOT NULL DEFAULT 1,
  `manage_take_away` tinyint(1) NOT NULL DEFAULT 1,
  `manage_delivery` tinyint(1) NOT NULL DEFAULT 1,
  `last_menu_update` timestamp NOT NULL,
  `concurrent_preparation_capacity` int(11) NOT NULL DEFAULT 1,
  `delivery_fees` int(11) NOT NULL DEFAULT 0,
  `delivery_fees_limit` int(11) NOT NULL DEFAULT 0,
  `delivery_distance_limit` int(11) NOT NULL DEFAULT 5000,
  `minimum_cart_for_delivery_order` int(11) NOT NULL DEFAULT 1000,
  `kitchen_show_only_paid` tinyint(1) NOT NULL DEFAULT 0,
  `kitchen_show_pending_approval` tinyint(1) NOT NULL DEFAULT 0,
  `kitchen_distribution_mode` varchar(30) NOT NULL DEFAULT 'READY_FOR_DISTRIBUTION' COMMENT 'READY_FOR_DISTRIBUTION / DISTRIBUTE',
  `production_display_mode` varchar(20) NOT NULL DEFAULT 'CLASSIC' COMMENT 'CLASSIC, PRODUCT_FOCUS',
  `preparation_time_mode` varchar(20) NOT NULL DEFAULT 'AUTO' COMMENT 'AUTO | MANUAL',
  `preparation_time` int(11) NOT NULL DEFAULT 15 COMMENT 'for MANUAL, in minuts',
  `minimum_preparation_time` int(11) NOT NULL DEFAULT 300 COMMENT 'in seconds',
  `maximum_preparation_time` int(11) NOT NULL DEFAULT 3600 COMMENT 'in seconds',
  `disable_components_under_safety_stock` tinyint(1) NOT NULL DEFAULT 0,
  `service_required_for_ordering` tinyint(1) NOT NULL DEFAULT 0,
  `cash_register_required_for_ordering` tinyint(1) NOT NULL DEFAULT 1,
  `waiter_app_can_cash_in` tinyint(1) NOT NULL DEFAULT 1,
  `waiter_app_can_clock_in` tinyint(1) NOT NULL DEFAULT 0,
  `auto_complete_orders` tinyint(1) NOT NULL DEFAULT 0,
  `auto_complete_orders_delay` int(11) NOT NULL DEFAULT 10,
  `auto_accept_sno_delivery_orders` tinyint(1) NOT NULL DEFAULT 0,
  `auto_accept_sno_take_away_orders` tinyint(1) NOT NULL DEFAULT 0,
  `automatically_add_customer_rewards` tinyint(1) NOT NULL DEFAULT 1,
  `warning_new_order_not_paid` tinyint(1) NOT NULL DEFAULT 1,
  `enable_advance_orders` tinyint(1) NOT NULL DEFAULT 0,
  `advance_order_days` int(11) NOT NULL DEFAULT 3,
  `pager_number_required` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Demande un numéro de bipeur',
  `pos_auto_lock_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `pos_auto_lock_delay_minutes` int(11) NOT NULL DEFAULT 5,
  `pos_upsell_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `customer_form_requirements` longtext CHARACTER SET utf8mb4 COLLATE utf8mb4_bin DEFAULT NULL CHECK (json_valid(`customer_form_requirements`)),
  `enabled_rating` tinyint(1) NOT NULL DEFAULT 0,
  `currency` varchar(5) NOT NULL DEFAULT 'EUR',
  `is_open` tinyint(1) NOT NULL DEFAULT 0,
  `primary_color` varchar(10) NOT NULL DEFAULT '#212529',
  `text_color_on_primary_color` varchar(10) NOT NULL DEFAULT '#ffffff',
  `zoning_type` varchar(20) DEFAULT NULL,
  `radial_cone_count` int(11) NOT NULL DEFAULT 8,
  `radial_zone_ranges` varchar(20) NOT NULL DEFAULT '0-3,3-5,5-999',
  `grid_cell_size_km` int(11) NOT NULL DEFAULT 2,
  `grid_origin_lat` double DEFAULT NULL,
  `grid_origin_lng` double DEFAULT NULL,
  `cardinal_cone_count` int(11) NOT NULL DEFAULT 4,
  `cardinal_zone_ranges` varchar(30) NOT NULL DEFAULT '0-1,1-3,3-999',
  `enable_upsell` tinyint(1) NOT NULL DEFAULT 0,
  `upsell_max_items` int(11) NOT NULL DEFAULT 3,
  `enable_translation` tinyint(1) NOT NULL DEFAULT 0,
  `pos_covers_count_required` boolean NOT NULL DEFAULT false
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_unicode_ci;
```
47 paramètres au total (PK exclue). Deux colonnes n'ont **aucune valeur par défaut SQL** malgré la contrainte NOT NULL : `last_menu_update` (obligatoirement fournie explicitement à l'insertion) et `merchant_id` (PK). Trois colonnes sont nullable sans défaut : `customer_form_requirements`, `zoning_type`, `grid_origin_lat`/`grid_origin_lng`.

**Tables satellites adjacentes** (également « paramètres marchand », par canal) :
- **`merchant_marketing_settings`** (PK `merchant_id`) : `sms_enabled` DEFAULT 1, `sms_unit_price` DEFAULT 7, `email_enabled` DEFAULT 1, `sms_sender_name`, `email_sender_name`, `sms_template`, `email_template`, `tracking_template` DEFAULT `'Votre commande #{order_id} est en cours de livraison. Suivez-la ici : {tracking_url}'`, `messaggio_login`/`messaggio_from` (identifiants SMS en dur en DEFAULT SQL).
- **`scannorder_settings`** (PK `merchant_id`) : ~56 colonnes (activation, branding QR-code, `variable_fees` DEFAULT `0.007`, `fixed_fees` DEFAULT `15`, `commission_rate`, `cgv_link`, `legal_notices_link`, `closed_until`). Complétée par `migrations/done/085_scannorder_extra_prep_time.up.sql` (`extra_prep_minutes`, `extra_prep_until`).
- **`kiosk_settings`** (PK `merchant_id`, `migrations/done/037_kiosk_module.up.sql:76-93`) : `fulfillment_dine_in`/`fulfillment_take_away` DEFAULT TRUE, `force_fulfillment_type`, `pager_number_required` DEFAULT FALSE, `show_allergens` DEFAULT TRUE, `inactivity_timeout_sec` DEFAULT 90, `upsell_enabled` DEFAULT TRUE, `pay_at_counter_enabled` DEFAULT TRUE, `card_payment_enabled` DEFAULT FALSE. Complétée par `migrations/done/061_kiosk_settings_fees.up.sql` (`variable_fees` DEFAULT `0.0070`, `fixed_fees` DEFAULT `15`).
- **`hours_of_operation`** (une ligne PAR CRÉNEAU, pas une table 1-1 par marchand) : `id`, `merchant_id`, `day_of_week_from`/`to`, `hour_from`/`to`, `first_booking_time`/`last_booking_time`, `booking_capacity` DEFAULT 0, `valid_from`/`valid_to`, `enabled` DEFAULT 1.

### 7.2. Moyens d'encaissement (méthodes de paiement)

**Constat central : il n'existe aucune table SQL de moyens de paiement paramétrables**, et aucune contrainte `ENUM` en base. La colonne qui porte le code du moyen de paiement (`mop`) est un simple `varchar` libre :
```
docs/migration-postgres/wello-resto-mysql-ddl.md:487   `mop` varchar(10) NOT NULL,   -- table payments
docs/migration-postgres/wello-resto-mysql-ddl.md:2179  `mop` varchar(20) NOT NULL COMMENT 'Means of payment | CURRENCY or PERCENTAGE for discounts',
```
Les moyens de paiement sont donc **codés en dur** (constantes/enums applicatifs), et **dupliqués indépendamment dans au moins 4 endroits différents** (API + 3 front-ends), avec des listes divergentes.

**a) API Go** — aucune liste unique, codes éparpillés en constantes :
```go
// internal/models/users_models.go:14-16
StripeMOP      = "STRIPE"
TicketRestoMOP = "TR"
CardMOP        = "CB"
```
```go
// internal/models/payment_models.go:3-8
const (
	OperationTypeSale   = "SALE"
	OperationTypeRefund = "REFUND"

	DeliverooMOP = "DELIVEROO"
)
```
```go
// internal/models/request_objects.go:910-912
PaymentUberEats  = "UBER_EATS"
PaymentDeliveroo = "DELIVEROO"
PaymentStripe    = "STRIPE"
```
Le code `"ES"` (Espèces) n'est même pas une constante nommée : écrit en dur dans une comparaison métier (`internal/modules/cash_registers/repository.go:476`, `if mopLine.MOP == "ES" {`). La struct `MOPLine` est dupliquée deux fois (`internal/models/request_objects.go:161-165` et `internal/modules/cash_registers/models.go:89-93`), avec un type `Amount` différent (`int` vs `float64`).

**b) POS Flutter** — enum Dart centralisé, mais propre à ce repo (`lib/models/orders/method_of_payment_enum.dart:1-89`) :
```dart
enum MethodOfPaymentEnum {
  es(serverMop: 'ES', icon: Icons.euro, label: 'Espèce', iconColor: AppColors.paymentEs),
  stripe(serverMop: 'STRIPE', icon: Icons.euro, label: 'En ligne', iconColor: Color(0xFFFAD02C)),
  cb(serverMop: 'CB', icon: Icons.credit_card, label: 'Carte bancaire', iconColor: AppColors.paymentCb),
  tr(serverMop: 'TR', icon: Icons.money, label: 'Ticket restaurant', iconColor: AppColors.paymentTr),
  carteTicketRestaurant(serverMop: 'CARTE TICKET RESTAURANT', icon: Icons.credit_card, label: 'Carte Ticket Restaurant', iconColor: Color(0xFF155FBE)),
  other(serverMop: 'OTHER', icon: Icons.euro, label: 'Autre', iconColor: AppColor.secondaryColor),
  qr(serverMop: 'QR', icon: Icons.qr_code, label: 'QR code'),
  discountByAmount(serverMop: 'CURRENCY', icon: Icons.eco, label: 'Réduction monta.', isDiscount: true),
  discountByPercentage(serverMop: 'PERCENTAGE', icon: Icons.eco, label: 'Réduction monta.', isDiscount: true);
  ...
}
```
Seul un sous-ensemble (4 boutons — CB, ES, TR, QR) est exposé au caissier au moment de l'encaissement, également en dur (`lib/ui/widgets/dialogs/calculator/right_pannel/calculator_menu_view.dart:49-76`). Aucun de ces boutons n'est piloté par un paramètre marchand issu de l'API — la liste affichée au caissier est fixe pour tous les marchands.

**c) Borne kiosk** — deux méthodes seulement, identifiants en dur, mais leur **affichage** est conditionné par deux flags qui viennent bien de l'API (`kiosk_settings.card_payment_enabled` / `pay_at_counter_enabled`) — `lib/presentation/screens/payment_screen.dart:170-207` :
```dart
final cardPaymentEnabled = settings?.cardPaymentEnabled ?? false;
final terminalLocationConfigured = settings?.terminalLocationId != null &&
    settings!.terminalLocationId!.isNotEmpty;
final payAtCounterEnabled = settings?.payAtCounterEnabled ?? true;

final tiles = [
  if (payAtCounterEnabled)
    KioskSelectionTile(icon: Icons.storefront, label: 'Payer en caisse', ...
        onTap: () => _selectMethod(context, orderController, 'pay_at_counter')),
  if (cardPaymentEnabled && terminalLocationConfigured)
    KioskSelectionTile(icon: Icons.credit_card, label: 'Payer par carte', ...
        onTap: () => _selectMethod(context, orderController, 'card')),
];
```
Les identifiants `'card'` / `'pay_at_counter'` sont des chaînes en dur non alignées avec les codes MOP de l'API (`CB`, `ES`) ni avec l'enum POS Flutter — la traduction se fait côté API (`internal/modules/kiosk/service.go:1562-1628`).

**d) Back-office web** — deux listes en dur supplémentaires, différentes des deux précédentes, plus une fonction de normalisation :
```tsx
// src/components/cash/ClosureModal.tsx:47-56
const PRESETS: Preset[] = [
  { code: 'CB', label: 'Carte Bancaire', apiLabel: 'CB' },
  { code: 'CASH', label: 'Espèces', apiLabel: 'ES' },
  { code: 'TR', label: 'Ticket Resto', apiLabel: 'TR' },
  { code: 'CHEQUE', label: 'Chèque', apiLabel: 'CHEQUE' },
  { code: 'OTHER', label: 'Autre', apiLabel: '' },
];
```
```ts
// src/services/cashRegisterService.ts:271-289
const normalizeMopCode = (value: string | undefined): string => {
  const raw = (value ?? '').toUpperCase();
  if (raw === 'ES' || raw === 'CASH') return 'CASH';
  if (raw === 'CB') return 'CB';
  if (raw === 'TR') return 'TR';
  if (raw === 'CHEQUE' || raw === 'CHQ') return 'CHEQUE';
  return raw || 'OTHER';
};
```
Remarque factuelle : `CHEQUE` apparaît dans le back-office mais **n'existe dans aucune des listes de l'API ni du POS Flutter**. À l'inverse, `"STRIPE"`, `"QR"`, `"CARTE TICKET RESTAURANT"`, `"CURRENCY"`, `"PERCENTAGE"` (POS Flutter) n'apparaissent dans aucune liste du back-office.

**Synthèse** : aucun des 4 emplacements ne lit une liste depuis l'API/la base — la colonne `mop` étant un `varchar` libre, il n'existe **aucun appel API centralisé** qui retournerait « la liste des moyens de paiement du marchand ».

### 7.3. Horaires d'ouverture, fuseau horaire, informations légales

**Fuseau horaire** : `merchant.timezone`, `varchar(50) NOT NULL DEFAULT 'Europe/Paris'` — **NOT NULL avec DEFAULT SQL**, jamais bloquant. Non modifiable via l'endpoint de settings simplifié (`POSSettingsInfoPatch` n'a pas de champ Timezone), mais existe dans le modèle « bas niveau » `MerchantSettings.Timezone *string` et pris en compte si le client envoie directement `{"merchant": {"timezone": "..."}}`. Aucune validation applicative (ni Go ni CHECK SQL) que la valeur soit un identifiant IANA valide. **`CreateMerchantRequest` n'a pas de champ `timezone`** — la valeur `'Europe/Paris'` s'applique donc systématiquement à la création, quel que soit le pays réel du marchand.

**Horaires d'ouverture** : stockés dans `hours_of_operation` (une ligne par créneau). Toutes les colonnes de créneau sont `NOT NULL` au niveau de la ligne, mais **aucune contrainte n'impose qu'au moins une ligne existe** pour un marchand donné. **Aucune ligne `hours_of_operation` n'est créée automatiquement à la création d'un marchand** : `InitMerchantSatellites` initialise 7 tables satellites mais ne touche jamais `hours_of_operation`. Le statut ouvert/fermé « manuel » (indépendant des créneaux) est porté par `merchant_parameters.is_open` (DEFAULT 0) — un nouveau marchand démarre donc à **fermé** par défaut. Aucune validation applicative n'exige la présence d'au moins un créneau pour activer un marchand.

**Informations légales** :

| Info | Table.colonne | Type SQL | NULL ? | Validation applicative Go ? |
|---|---|---|---|---|
| Raison sociale | `merchant.fullName` | varchar(50) | NOT NULL, aucun défaut | Oui, à la création |
| SIRET | `merchant.SIRET` | varchar(50) | NOT NULL, aucun défaut | Oui, à la création |
| TVA intracommunautaire | `merchant.vat_number` | varchar(50) | nullable | Aucune |
| Adresse | `merchant.address`/`street_number`/`street`/`zip_code`/`city` | text/varchar NOT NULL | NOT NULL (sauf `country`) | Non |
| Téléphone | `merchant.merchantTel` | varchar(15) | NOT NULL, aucun défaut | Oui, à la création |
| Site web | `merchant.web_site` | varchar(100) | NOT NULL, aucun défaut | Non |
| Email | `merchant.email` | varchar(100) | nullable | Non |

Concernant `vat_number` : **aucun chemin applicatif Go n'écrit jamais cette colonne** en dehors des fichiers de test d'intégration. Elle n'est utilisée qu'en lecture, pour l'en-tête de facture PDF (`internal/modules/pos/accounting/repository.go:89,106`, `internal/modules/order_life_cycle/invoice_pdf.go:16-18`). Il n'existe **aucun endpoint** permettant de la renseigner depuis l'API — elle reste `NULL` pour tout marchand créé via le flux normal, sauf intervention directe en base.

**Validation applicative à la création** — `internal/modules/pos/create_service.go:13-19` :
```go
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
	if strings.TrimSpace(req.FullName) == "" ||
		strings.TrimSpace(req.SIRET) == "" ||
		strings.TrimSpace(req.Tel) == "" ||
		strings.TrimSpace(req.PackageID) == "" {
		return CreateMerchantResponse{}, models.ErrInvalidInput
	}
```
Seuls `FullName`, `SIRET`, `Tel` et `PackageID` sont vérifiés non-vides. **`Address`, `StreetNumber`, `Street`, `ZipCode`, `City`, `WebSite`, `Email` ne sont validés nulle part côté Go**, alors que la plupart sont `NOT NULL` en SQL : une chaîne vide `""` satisfait la contrainte SQL sans satisfaire un besoin métier réel de « champ renseigné ». `timezone`, `lat`, `lng`, `vat_number`, `default_role_id` ne figurent pas dans la liste des colonnes insérées par `InsertMerchant` : ils prennent systématiquement leur valeur `DEFAULT` SQL.

### 7.4. Mécanisme de valeurs par défaut à la création d'un marchand

**Oui, un mécanisme applicatif explicite existe** — pas seulement des colonnes avec `DEFAULT` SQL : le code Go exécute, dans une transaction unique, une série d'`INSERT` explicites dans les tables satellites juste après la création de `merchant` (détail complet dans la Section 8, `InitMerchantSatellites`).

**Lecture factuelle du mécanisme** :
- La ligne `merchant_parameters` d'un nouveau marchand est créée avec **seulement** `merchant_id` et `last_menu_update` explicitement fournis par le Go — les 46 autres colonnes prennent leur valeur via les `DEFAULT` SQL. Il n'y a **aucune valeur métier fixée en dur côté Go** pour `merchant_parameters` : le comportement par défaut d'un nouveau marchand dépend entièrement du schéma SQL, pas d'une politique applicative explicite.
- Trois colonnes `NOT NULL` sans `DEFAULT` SQL sur les tables satellites obligent le Go à fournir une valeur explicite pour que l'`INSERT` ne échoue pas sous Postgres : `scannorder_settings.seo_title/seo_description/seo_keywords/seo_cuisine_type` (`''` explicite), `merchant_parameters.last_menu_update` (horodatage UTC explicite), `bookings_settings.code` (`''` explicite).
- Un rôle RBAC « admin » est également attribué par défaut au marchand via `merchant.default_role_id`, positionné par `SetDefaultRoleID` uniquement s'il est encore `NULL`.
- Aucune ligne n'est créée pour `hours_of_operation` : ce paramètre marchand n'a **aucun mécanisme de valeur par défaut**, ni applicatif ni SQL — un nouveau marchand n'a simplement aucun créneau d'ouverture jusqu'à saisie manuelle.
---

## 8. Création de compte actuelle

### 8.1. Endpoint(s) et chaîne handler → service → repository

**Route** — `cmd/api/routes.go:663-666` :
```go
	// --- POS ---
	r.Route("/pos", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/create", posH.CreateMerchant)
```
Soit **`POST /pos/create`**.

**Handler** — `internal/modules/pos/create_handler.go` (intégral) :
```go
package pos

import (
	"encoding/json"
	"errors"
	"net/http"
	"welloresto-api/internal/models"
)

// CreateMerchant handles POST /pos/create.
func (h *POSHandler) CreateMerchant(w http.ResponseWriter, r *http.Request) {
	var req CreateMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendJSON(w, http.StatusBadRequest, "pos", "create", map[string]string{"error": "invalid_request_body"})
		return
	}

	resp, err := h.service.CreateMerchant(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "user", "create", err)
		return
	}

	models.SendJSON(w, http.StatusCreated, "pos", "create", resp)
}
```

**Payload exact attendu** (`internal/modules/pos/create_models.go`, intégral) :
```go
package pos

// CreateMerchantRequest is the JSON payload for POST /pos/create.
type CreateMerchantRequest struct {
	FullName     string `json:"full_name"`
	Address      string `json:"address"`
	StreetNumber string `json:"street_number"`
	Street       string `json:"street"`
	ZipCode      string `json:"zip_code"`
	City         string `json:"city"`
	Country      string `json:"country"`
	SIRET        string `json:"siret"`
	Tel          string `json:"tel"`
	WebSite      string `json:"web_site"`
	Email        string `json:"email"`
	PackageID    string `json:"package_id"`
	// Optional: if set the user is linked to the new merchant in the same transaction.
	UserID string `json:"user_id,omitempty"`
	// Rights to grant when linking. Ignored if UserID is empty.
	Admin bool `json:"admin"`
}

// CreateMerchantResponse is returned on success (201).
type CreateMerchantResponse struct {
	MerchantID string `json:"merchant_id"`
}
```
Validation minimale : `FullName`, `SIRET`, `Tel`, `PackageID` doivent être non vides ; tout le reste (adresse, ville, email, etc.) est accepté même vide, sans validation de format (pas de vérification d'email valide, pas de vérification que `PackageID` référence une ligne existante dans la table `packages`).

**Câblage DI** (`cmd/api/routes.go:202-203,478`) :
```go
posRepo := posModule.NewPOSRepository(selectedDB)
posService := posModule.NewPOSService(posRepo, notificationService)
...
posH := posModule.NewPOSHandler(posService, r2Client)
```

### 8.2. Ordre exact des opérations et atomicité

**Service** — `internal/modules/pos/create_service.go:11-74`, texte intégral :
```go
// CreateMerchant creates a new merchant with its satellite tables inside a single
// transaction. If req.UserID is non-empty the user is linked in the same transaction.
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
	if strings.TrimSpace(req.FullName) == "" ||
		strings.TrimSpace(req.SIRET) == "" ||
		strings.TrimSpace(req.Tel) == "" ||
		strings.TrimSpace(req.PackageID) == "" {
		return CreateMerchantResponse{}, models.ErrInvalidInput
	}

	merchantToken, err := helpers.GenerateToken(10) // 20-char hex token -> VARCHAR(20)
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	var merchantID string
	err = dbutils.RunInTx(ctx, s.posRepo.database, func(txCtx context.Context) error {
		// Step 1 — create merchant row
		merchantID, err = s.posRepo.InsertMerchant(txCtx, req, merchantToken)
		if err != nil {
			return err
		}

		// Step 2 — create subscription from the requested package
		if err := s.posRepo.InsertSubscription(txCtx, merchantID, strings.TrimSpace(req.PackageID)); err != nil {
			return err
		}

		// Step 3 — initialise companion tables
		if err := s.posRepo.InitMerchantSatellites(txCtx, merchantID); err != nil {
			return err
		}

		// Step 4 — RBAC lot 1 (additive, strictly groundwork): seed the two
		// system roles and point the merchant's default at "admin" (RBAC lot 4
		// decision: every account becomes Administrateur while permissions are
		// not yet exploited from the UI). Runs before step 5 so the optional
		// initial user linkage below has a default_role_id to read;
		// insertUserRightsTx fails explicitly if it is still unset.
		adminRoleID, _, err := s.rolesRepo.EnsureSystemRoles(txCtx, merchantID)
		if err != nil {
			return err
		}
		if err := s.posRepo.SetDefaultRoleID(txCtx, merchantID, adminRoleID); err != nil {
			return err
		}

		// Step 5 — optional user linkage
		if strings.TrimSpace(req.UserID) != "" {
			if _, _, err := s.insertUserRightsTx(txCtx, req.UserID, merchantID, req.Admin, adminRoleID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return CreateMerchantResponse{}, err
	}

	return CreateMerchantResponse{MerchantID: merchantID}, nil
}
```

**Ordre exact d'écriture, table par table** (`internal/modules/pos/create_repository.go`) :

1. `INSERT INTO merchant (fullName, address, street_number, street, zip_code, city, country, SIRET, merchantTel, web_site, email, token)`
2. `INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id)` avec `stripe_subscription_id = ''`
3. `InitMerchantSatellites` — dans cet ordre interne :
   - 2 × `INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag)` (menu standard, menu-only/mywelloresto)
   - `INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type)` avec chaînes vides
   - `INSERT INTO merchant_parameters (merchant_id, last_menu_update)`
   - `INSERT INTO merchant_marketing_settings (merchant_id)`
   - `INSERT INTO haccp_settings (merchant_id, created_at, updated_at)`
   - `INSERT INTO bookings_settings (merchant_id, code)` avec `code = ''`
   - `INSERT INTO cash_desks (merchant_id, name)` avec `name = 'Caisse principale'`
4. `s.rolesRepo.EnsureSystemRoles(txCtx, merchantID)` — crée (si absentes) deux lignes `roles` (système `admin` et `staff`) pour ce `merchant_id`, chacune peuplée de son jeu de permissions de base via `INSERT INTO role_permissions`
5. `UPDATE merchant SET default_role_id = ? WHERE ... AND default_role_id IS NULL` (pointant vers le rôle `admin`)
6. **Optionnel**, si `req.UserID` non vide : `INSERT INTO users_rights (user_id, merchant_id, token, admin, role_id, enabled) VALUES (?, ?, ?, ?, ?, TRUE)`

**Atomicité** : oui, une **vraie transaction SQL** (`sql.Tx` natif), pas de compensation applicative. `internal/utils/dbutils/run_in_tx.go:9-30`, texte intégral :
```go
func RunInTx(ctx context.Context, db *sql.DB, fn func(txCtx context.Context) error) error {
	// Si on est déjà dans une transaction (imbrication), on exécute juste la fonction
	if ExtractTx(ctx) != nil {
		return fn(ctx)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	txCtx := InjectTx(ctx, tx)

	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
```
Toute erreur à n'importe quelle étape (1 à 6) déclenche `tx.Rollback()` et aucune ligne n'est persistée. L'appel imbriqué `EnsureSystemRoles` (étape 4) invoque lui-même `dbutils.RunInTx` en interne, mais le garde `if ExtractTx(ctx) != nil { return fn(ctx) }` fait que ce second appel réutilise la transaction déjà ouverte par `CreateMerchant` plutôt que d'en ouvrir une seconde imbriquée — l'ensemble des 6 étapes est donc bien couvert par une seule et même transaction SQL, tout ou rien.

### 8.3. Exposition publique vs usage interne

**Route** (`cmd/api/routes.go:663-667`) :
```go
	r.Route("/pos", func(r chi.Router) {
		r.Use(authMiddleware)

		r.Post("/create", posH.CreateMerchant)
		r.With(middleware.RequirePermission(permission.StaffManage)).Post("/link-user", posH.LinkUser)
		r.Get("/status", posH.GetPOSStatus)
		r.With(middleware.RequirePermission(permission.POSStatusManage)).Patch("/status", posH.UpdatePOSStatus)
```
**`POST /pos/create` n'est protégée que par `authMiddleware`** — contrairement à sa voisine immédiate `POST /pos/link-user` qui ajoute explicitement `.With(middleware.RequirePermission(permission.StaffManage))`. Aucune vérification RBAC (`RequirePermission`, `RequireAdmin`) n'encadre la création de marchand.

`authMiddleware` (Section 2.3) exige uniquement un **token Bearer valide correspondant à un utilisateur existant** — il ne vérifie ni permission, ni appartenance à un marchand particulier, ni rôle.

**Conséquence factuelle** : `POST /pos/create` est donc atteignable par **tout utilisateur déjà authentifié dans le système** (n'importe quel compte `users` valide, quel que soit son rôle ou le marchand auquel il est actuellement rattaché) — ce n'est ni un endpoint public sans authentification, ni un endpoint réservé à un token admin/interne dédié.

**Il n'existe par ailleurs aucun endpoint d'auto-inscription (« register »/« signup ») pour créer un compte `users`** : recherche `register|signup` dans `cmd/api/routes.go` → aucune occurrence. Le groupe `/auth` n'expose que `login`, `mfa/fallback-sms`, `send-verification`, `verify`, `forgot-password`, `reset-password`, `pin`, `pin/set`, `pin/reset` — jamais de création de compte. La création d'un `users` se fait via `POST /users`, qui exige `authMiddleware` **et** `middleware.RequirePermission(permission.StaffManage)`. Un compte `users` ne peut donc être créé, aujourd'hui, que par un utilisateur déjà `staff.manage` sur un marchand existant — pas en self-service.

### 8.4. Entités connexes créées automatiquement

| Table | Contenu | Toujours créé ? |
|---|---|---|
| `merchant` | La fiche marchand elle-même | Oui |
| `subscriptions` | Abonnement lié au `package_id` fourni, `stripe_subscription_id = ''` | Oui |
| `qrcodes` | 2 lignes (menu standard + menu-only/mywelloresto) | Oui |
| `scannorder_settings` | Ligne par défaut (PK = merchant_id), champs SEO vides | Oui |
| `merchant_parameters` | Ligne par défaut (PK = merchant_id), `last_menu_update` = horodatage courant | Oui |
| `merchant_marketing_settings` | Ligne par défaut (PK = merchant_id) | Oui |
| `haccp_settings` | Ligne par défaut, `created_at`/`updated_at` = horodatage courant | Oui |
| `bookings_settings` | Ligne par défaut, `code = ''` | Oui |
| `cash_desks` | Une caisse nommée `'Caisse principale'` | Oui |
| `roles` | 2 rôles système (« admin », « staff ») pour ce marchand, avec permissions de base | Oui (via `EnsureSystemRoles`) |
| `role_permissions` | Permissions de base attachées à ces 2 rôles | Oui |
| `merchant.default_role_id` | Pointé vers le rôle « admin » nouvellement créé | Oui |
| `users_rights` | Une ligne liant `req.UserID` au marchand, `role_id` = rôle admin (ou le rôle passé), `admin = req.Admin`, `enabled = TRUE` | **Seulement si `req.UserID` non vide** dans la requête |

**Aucun premier compte `users` n'est créé automatiquement** : le payload ne contient aucun champ permettant de créer un utilisateur (nom, mot de passe, email d'un futur admin) — seulement un `user_id` optionnel censé référencer un `users` **déjà existant**. Si `user_id` est omis, le marchand est créé sans aucun utilisateur lié.

**Point critique sur l'exécutabilité réelle de ce flux** : l'étape 4 (`EnsureSystemRoles`/`SetDefaultRoleID`) dépend du schéma RBAC introduit par les migrations `094` à `099` (tables `permissions`, `roles`, `role_permissions`, colonnes `users_rights.role_id` et `merchant.default_role_id`). Comme indiqué dans la Note méthodologique en tête de document, `docs/RBAC_BASCULE.md` déclare explicitement, à la date du 2026-08-27 :

> « Ces six migrations sont déjà appliquées en recette [...]. **En production, aucune des six n'a jamais été jouée** : elles partent de zéro sous ces numéros. »

et ces migrations sont écrites exclusivement en syntaxe PostgreSQL, tandis que `docs/migration-postgres/wello-resto-mysql-ddl.md` (dump MySQL) et le comportement par défaut de connexion (`cmd/api/main.go:24`, « DB MySQL par défaut, Postgres si DB_DIALECT=postgres — migration en cours ») pointent vers un univers disjoint. Ce fait — présent dans les fichiers de migration eux-mêmes, dans `docs/RBAC_BASCULE.md`, et cohérent avec la commande dédiée `cmd/assign_admin_role` (fichier non suivi, `assign_admin_role.exe` visible dans l'état git courant) — est rapporté ici tel qu'il figure dans la documentation du dépôt, sans vérification en base réelle (audit en lecture seule sur le code).

---

## Annexe — Fichiers cités (chemins absolus, dépôt `ib-welloresto-api`)

- `docs/migration-postgres/wello-resto-mysql-ddl.md`
- `docs/migration-postgres/04-schema-postgres-target.sql`
- `docs/RBAC_BASCULE.md`
- `docs/migration-postgres/60-mysql-migrations-status-checklist.md`
- `migrations/done/003_create_availabilities_tables.sql`
- `migrations/done/014_planning_socle.sql`
- `migrations/done/019_add_subscription_feature_flags.sql`
- `migrations/done/031_add_pin_hash_to_users_rights.up.sql`
- `migrations/done/037_kiosk_module.up.sql`
- `migrations/done/054_stripe_accounts_terminal_location_id.up.sql`
- `migrations/done/061_kiosk_settings_fees.up.sql`
- `migrations/done/085_scannorder_extra_prep_time.up.sql`
- `migrations/done/086_merchant_parameters_pos_covers_count_required.up.sql`
- `migrations/done/094_roles_schema.up.sql`
- `migrations/done/095_roles_permissions_catalog.up.sql`
- `migrations/done/097_permission_pos_status_manage.up.sql`
- `migrations/done/099_merchant_default_role_admin.up.sql`
- `migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql`
- `cmd/api/routes.go`
- `cmd/api/main.go`
- `internal/permission/keys_gen.go`
- `internal/middleware/auth.go`
- `internal/middleware/permissions.go`
- `internal/middleware/require_permission.go`
- `internal/modules/auth/handler.go`
- `internal/modules/auth/service.go`
- `internal/modules/auth/repository.go`
- `internal/modules/auth/models.go`
- `internal/modules/auth/permissions.go`
- `internal/modules/roles/repository.go`
- `internal/modules/roles/models.go`
- `internal/modules/pos/create_handler.go`
- `internal/modules/pos/create_models.go`
- `internal/modules/pos/create_service.go`
- `internal/modules/pos/create_repository.go`
- `internal/modules/pos/repository.go`
- `internal/modules/pos/models.go`
- `internal/modules/pos/reports/repository.go`
- `internal/modules/pos/accounting/repository.go`
- `internal/modules/pos/accounting/service.go`
- `internal/modules/cash_registers/repository.go`
- `internal/modules/order_life_cycle/repository.go`
- `internal/modules/order_life_cycle/service.go`
- `internal/modules/order_life_cycle/invoice_pdf.go`
- `internal/modules/receipt/service.go`
- `internal/modules/receipt/repository.go`
- `internal/modules/menu/import_handler.go`
- `internal/modules/menu/import_models.go`
- `internal/modules/menu/import_service.go`
- `internal/modules/menu/import_commit_service.go`
- `internal/modules/menu/importer/values.go`
- `internal/modules/menu/importer/wello_generic.go`
- `internal/modules/menu/importer/zelty.go`
- `internal/modules/menu/importer/manual.go`
- `internal/modules/menu/importer/commit_plan.go`
- `internal/modules/menu/importer/template.go`
- `internal/modules/scannorder/repository.go`
- `internal/modules/scannorder/handler.go`
- `internal/modules/scannorder/service.go`
- `internal/modules/kiosk/repository.go`
- `internal/modules/kiosk/service.go`
- `internal/modules/users/create_service.go`
- `internal/modules/users/create_models.go`
- `internal/modules/users/admin_repository.go`
- `internal/modules/users/admin_models.go`
- `internal/modules/integrations/service.go`
- `internal/infrastructure/stripe/client.go`
- `internal/infrastructure/stripe/service.go`
- `internal/infrastructure/stripe/checkout.go`
- `internal/infrastructure/stripe/connect.go`
- `internal/webhook/stripe/http_handler.go`
- `internal/webhook/stripe/service.go`
- `internal/webhook/stripe/repository.go`
- `internal/webhook/ubereats/handler/http_handler.go`
- `internal/webhook/ubereats/service/service.go`
- `internal/config/stripe.go`
- `internal/config/auth.go`
- `internal/models/request_objects.go`
- `internal/models/payment_models.go`
- `internal/models/users_models.go`
- `internal/models/redis_models.go`
- `internal/helpers/ids.go`
- `internal/helpers/handler_helpers.go`
- `internal/utils/dbutils/run_in_tx.go`
- `internal/utils/security/hash_signing.go`
- `internal/modules/notification/token_manager.go`
- `data-migration/migration_welloresto_data.sql`
- `staging_schema_dump.sql`

Fichiers cités hors du dépôt API (repos satellites, chemins relatifs à leur racine respective) :
- `wello-back-office/src/pages/Menu.tsx`
- `wello-back-office/src/pages/CategoriesTable.tsx`
- `wello-back-office/src/pages/equipe/EquipePage.tsx`
- `wello-back-office/src/components/team/CreateMemberSheet.tsx`
- `wello-back-office/src/components/team/tabs/AccessTab.tsx`
- `wello-back-office/src/components/menu/CreateProductCategoryDialog.tsx`
- `wello-back-office/src/components/menu/SimpleProductSheet.tsx`
- `wello-back-office/src/components/menu/BulkEditDialog.tsx`
- `wello-back-office/src/components/shared/BulkAssignProductsDialog.tsx`
- `wello-back-office/src/components/menu/import/ProductImportDialog.tsx`
- `wello-back-office/src/components/menu/import/ImportProviderStep.tsx`
- `wello-back-office/src/components/menu/import/ImportManualStep.tsx`
- `wello-back-office/src/components/menu/import/ImportReviewStep.tsx`
- `wello-back-office/src/components/menu/import/ImportDoorPicker.tsx`
- `wello-back-office/src/components/cash/ClosureModal.tsx`
- `wello-back-office/src/services/menuService.ts`
- `wello-back-office/src/services/menuImportService.ts`
- `wello-back-office/src/services/cashRegisterService.ts`
- `wello-back-office/src/services/financialReportsService.ts`
- `wello-back-office/src/services/reservationsService.ts`
- `wello-back-office/src/lib/manualImport.ts`
- `wello-back-office/src/lib/importDecisions.ts`
- `wello-back-office/src/types/adminUsers.ts`
- `wello-back-office/src/types/import.ts`
- `wello_resto_flutter/lib/models/orders/method_of_payment_enum.dart`
- `wello_resto_flutter/lib/ui/widgets/dialogs/calculator/right_pannel/calculator_menu_view.dart`
- `wello-kiosk/lib/presentation/screens/payment_screen.dart`
