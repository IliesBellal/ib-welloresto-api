# Audit — Parcours d'inscription, d'abonnement et de conformité (Phase 0)

**Audit en lecture seule.** Aucune modification de code, aucune migration, aucun refactoring, aucune recommandation de solution — uniquement l'état des lieux factuel du code et du schéma tels qu'observés le **2026-09-08**, sur le dépôt `ib-welloresto-api` (branche `main`) et les dépôts satellites de l'écosystème WelloResto (`wello-back-office`, `wello_resto_flutter`, `wello-kiosk`).

## Note méthodologique préalable

Une première version de cet audit existait déjà dans ce dépôt (`docs/audit-parcours-onboarding.md`, committée le 2026-08-28) et a été intégralement remplacée par le présent document, à la demande explicite de l'utilisateur, pour la raison suivante : sa prémisse méthodologique centrale est devenue caduque. Cette première version s'appuyait sur un export MySQL de production (`docs/migration-postgres/wello-resto-mysql-ddl.md`, dump du 2026-07-13) comme source de vérité du schéma, en tenant le raisonnement suivant : au 2026-08-27 (`docs/RBAC_BASCULE.md`), MySQL restait la base de production réelle et l'instance Postgres n'était qu'un environnement de recette pour la bascule en cours.

**Ce point de départ n'est plus vrai.** Le fichier `CLAUDE.md` de ce dépôt indique désormais explicitement : *« PostgreSQL is the only database engine in production (confirmed 2026-09-01) — the MySQL → Postgres migration [...] is complete; MySQL is no longer live anywhere. »* La bascule décrite comme en attente le 2026-08-27 a donc eu lieu dans l'intervalle.

**Source du schéma utilisée pour ce document** : deux sources convergentes, toutes deux Postgres —
1. **Introspection live** de la base Postgres de staging (`RENDER_STAGING_DATABASE_URL`), interrogée directement via `information_schema.columns`, `pg_constraint` et `pg_indexes` le 2026-09-08 — colonnes, types, nullabilité, valeurs par défaut, contraintes (`NOT NULL`, `PRIMARY KEY`, `FOREIGN KEY`, `CHECK`, `UNIQUE`) et index réels, table par table.
2. Le fichier `data-migration/postgres_ddl.sql` mentionné par l'utilisateur comme référence DDL Postgres est présent dans l'arborescence mais **vide** au moment de l'audit (0 octet) — il ne contient donc aucune information exploitable ; l'introspection live décrite ci-dessus s'y substitue intégralement et couvre le même besoin (structure exacte des tables citées dans ce document).

Sauf mention contraire explicite, toute structure de table citée dans ce document provient de cette introspection live du 2026-09-08, et non plus d'un dump MySQL. Le système RBAC (`roles`/`permissions`/`role_permissions`, `users_rights.role_id`, `merchant.default_role_id`) décrit dans les sections 2 et 3 est un schéma Postgres réellement en place sur l'instance staging interrogée — la question de son état en production au sens strict relève désormais de la configuration de déploiement, plus d'une divergence de moteur de base de données.

---

## 1. Modèle de données — identité et rattachement

### 1.1. Structure exacte de la table `merchant`

Précision terminologique : la table s'appelle **`merchant`, au singulier** (pas `merchants`). Tout le code Go y lit/écrit directement (`FROM merchant`, `INSERT INTO merchant`, `UPDATE merchant SET`), par exemple `internal/modules/pos/create_repository.go`, `internal/modules/auth/repository.go`, `internal/modules/bookings/repository.go`, `internal/modules/kiosk/repository.go`.

Structure réelle, introspection live de la base Postgres staging (`information_schema.columns` + `pg_constraint`, requête exécutée le 2026-09-08) :

```sql
merchant (
  id                integer                   NOT NULL   -- PK
  brand_id          varchar(35)               NULL       -- lien vers brands.brand_id, voir 1.3 ; aucune contrainte FK déclarée
  fullname          varchar(50)               NOT NULL
  address           text                      NOT NULL
  street_number     varchar(25)               NOT NULL
  street            varchar(255)              NOT NULL
  zip_code          varchar(6)                NOT NULL
  city              varchar(255)              NOT NULL
  country           varchar(255)              NOT NULL   DEFAULT 'France'
  lat               double precision          NULL       DEFAULT 0
  lng               double precision          NULL       DEFAULT 0
  timezone          varchar(50)               NOT NULL   DEFAULT 'Europe/Paris'
  logo              text                      NULL
  logo_url          text                      NULL
  handicap_access   boolean                   NOT NULL   DEFAULT false
  siret             varchar(50)               NOT NULL   -- obligatoire, pas de contrainte de format (longueur libre, pas de CHECK)
  vat_number        varchar(50)               NULL       -- facultatif
  web_site          varchar(100)              NOT NULL
  email             varchar(100)              NULL
  merchanttel       varchar(15)               NOT NULL
  token             varchar(20)               NOT NULL
  creation_date     timestamptz               NOT NULL   DEFAULT now()
  is_active         boolean                   NOT NULL   DEFAULT true
  default_role_id   varchar(64)               NULL       -- FK -> roles(id)
)
PRIMARY KEY (id)
FOREIGN KEY (default_role_id) REFERENCES roles(id)
```

Point d'architecture général qui vaut pour tout le schéma, pas seulement `merchant` : `merchant.id` est un **entier** (clé primaire numérique), alors que dans la quasi-totalité des 90+ autres tables qui référencent un marchand, la colonne s'appelle `merchant_id` et est de type **`varchar(64)`** — c'est-à-dire la représentation textuelle de cet entier, jamais un entier natif ni une vraie contrainte `FOREIGN KEY (merchant_id) REFERENCES merchant(id)`. Aucune des tables inspectées (`users`, `users_rights`, `employees`, `products`, `cash_registers`, `merchant_parameters`, etc.) ne porte de contrainte FK déclarée vers `merchant.id`. Le rattachement à un marchand est donc garanti par convention applicative, pas par le schéma relationnel.

**Champ « état »/« statut »** : un seul champ de ce type existe sur `merchant` : `is_active boolean NOT NULL DEFAULT true`. Il n'existe **aucun** champ `status`, `onboarding_step`, `stage` ou équivalent modélisant une progression de création de compte. Il n'y a pas non plus de colonne de type énuméré multi-états (`pending`/`active`/`suspended`/…) : `is_active` est un simple booléen actif/inactif.

### 1.2. Table `establishments`

**N'existe pas.** La liste complète des 206 tables du schéma public de la base Postgres staging (introspection live, `information_schema.tables`) ne contient aucune table `establishments` ni `establishment`. Recherche du mot « establishment » (insensible à la casse) dans tout le dépôt `ib-welloresto-api` : le mot n'apparaît que comme terme anglais générique dans des commentaires et des messages d'erreur — jamais comme nom de table, de module Go, de type ou d'endpoint. Aucun module `internal/modules/establishment*` n'existe. C'est un synonyme informel de « merchant » utilisé ponctuellement dans la prose, jamais une entité de base de données.

### 1.3. Notion de marque / enseigne / franchise

Il existe une **table réelle et active `brands`**, distincte de la paire de colonnes homonymes `orders.brand`/`orders.brand_status`.

**a) `brands`** — introspection live :

```sql
brands (
  brand_id       varchar(35)   NOT NULL   -- PK
  name           varchar(50)   NOT NULL
  slug           varchar(50)   NOT NULL
  logo_url       varchar(255)  NOT NULL
  banner_url     varchar(255)  NOT NULL
  description    varchar(255)  NOT NULL
  creation_date  timestamptz   NOT NULL   DEFAULT now()
)
PRIMARY KEY (brand_id)
```

`merchant.brand_id` (varchar(35), nullable) pointe vers cette table — un marchand peut donc appartenir à une enseigne regroupant plusieurs établissements. Cette relation est **effectivement exploitée en lecture** par un endpoint public :
- Route : `cmd/api/routes.go:715` — `r.Get("/brands/{brand_slug}", scannHandler.GetBrand)`, dans `r.Route("/scannorder", ...)`, sans authentification.
- Handler : `internal/modules/scannorder/handler.go:131-152`.
- Service : `internal/modules/scannorder/service.go` — `GetBrand` appelle `s.repo.GetMerchantsByBrandSlug(ctx, slug, lat, lng)`.
- Repository : `internal/modules/scannorder/repository.go:792-919` — requêtes `SELECT ... FROM brands WHERE slug = ?` (ligne 798) puis `... INNER JOIN merchant m ON m.brand_id = b.brand_id ...` (lignes 845, 880, 914) pour lister les établissements d'une enseigne, avec filtrage géographique optionnel.

**Mais `merchant.brand_id` et la table `brands` ne sont jamais écrits par du code applicatif de production.** Recherche exhaustive de `brand_id` et de `FROM brands`/`INTO brands`/`UPDATE brands` dans `internal/` : les seuls résultats hors tests sont les lectures de `internal/modules/scannorder/repository.go` et `internal/modules/scannorder/models.go:157` listées ci-dessus. Le seul `INSERT INTO brands` du dépôt se trouve dans une fixture de test d'intégration (`internal/modules/scannorder/postgres_integration_test.go:76`). Aucun endpoint, service ou repository ne permet de créer une enseigne ou d'y rattacher un marchand — cette donnée n'existe en base que si elle y a été insérée manuellement.

**b) `orders.brand` / `orders.brand_status` — homonyme sans rapport**, propre à la commande et non au marchand. Introspection live de `orders` :

```sql
orders.brand          varchar   NOT NULL  DEFAULT 'WELLO_RESTO'
orders.brand_status   varchar   NOT NULL  -- pas de défaut
orders.brand_order_id     varchar  NULL
orders.brand_order_num    varchar  NULL
```

`orders.brand` identifie le **canal de vente** de la commande (valeurs attendues : `WELLO_RESTO`, ou une marketplace de livraison type `UBER_EATS`/`DELIVEROO` — à confirmer précisément dans `internal/modules/orders/repository.go`, qui référence ces constantes), et `orders.brand_status` porte le **statut de la commande tel que rapporté par cette plateforme externe** (ex. le statut Uber Eats/Deliveroo de la commande), pas un statut d'enseigne. Il n'y a donc **aucun lien** entre `orders.brand`/`orders.brand_status` et la table `brands` / `merchant.brand_id` : c'est une pure homonymie de vocabulaire entre deux concepts métier différents (canal de commande vs. groupe d'établissements).

Recherche complémentaire de « enseigne », « franchise », « groupe » dans `internal/` : aucune occurrence pertinente en dehors de faux positifs (le mot anglais « group » dans des contextes SQL `GROUP BY` ou des noms de colonnes comme `is_product_group`). Il n'existe donc pas d'autre mécanisme de regroupement de marchands que la table `brands` décrite ci-dessus.

### 1.4. Structure de `users`

Introspection live :

```sql
users (
  user_id                 varchar(64)   NOT NULL   -- PK
  merchant_id              varchar(64)   NULL       -- voir 1.7 : ne borne pas le rattachement réel, qui passe par users_rights
  name                      varchar(255)  NOT NULL   -- UNIQUE (uq_users_name)
  first_name                varchar(40)   NOT NULL
  last_name                 varchar(40)   NOT NULL
  password                  varchar(255)  NOT NULL
  pin_code                  varchar(6)    NULL
  mfa_type                  varchar(25)   NULL
  mfa_status                varchar(25)   NULL
  mfa_verified_at            timestamptz   NULL
  mfa_otp_sent_at            timestamptz   NULL
  mfa_secret                 varchar(50)   NULL
  username                  varchar(20)   NULL
  email                      varchar(255)  NOT NULL   -- AUCUNE contrainte d'unicité (voir plus bas)
  email_verified_at          timestamptz   NULL
  dob, tel, tel_verified_at, address, street_number, street, city, country, zip_code, lat, lng
  heading                    integer       NOT NULL   DEFAULT 0
  profile_picture            text          NULL
  planning_color             varchar(11)   NOT NULL   DEFAULT '#28B2FC'
  isreception                boolean       NOT NULL   DEFAULT false
  iswaiter                   boolean       NOT NULL   DEFAULT false
  isdelivery                 integer       NOT NULL   DEFAULT 0
  admin                      boolean       NOT NULL   DEFAULT false
  access_id                  integer       NULL
  waiter_device_token / reception_device_token / delivery_device_token   varchar(255)   NULL
  token                      varchar(64)   NOT NULL
  terms_of_use_accepted      boolean       NOT NULL   DEFAULT false
  creationdate                timestamptz   NOT NULL   DEFAULT now()
  created_at                  timestamptz   NULL       DEFAULT now()   -- doublon de creationdate, colonne plus récente
  lastaccess                  timestamptz   NULL
  last_activity                timestamptz   NOT NULL   DEFAULT now()
  enabled                     boolean       NOT NULL   DEFAULT true
  last_login_at                timestamptz   NULL
  last_position_at             timestamptz   NULL
)
PRIMARY KEY (user_id)
UNIQUE INDEX uq_users_name ON (name)
```

**Unicité sur l'e-mail : elle n'existe pas.** Il n'y a, dans tout le schéma Postgres staging, aucun index unique ni contrainte `UNIQUE` sur `users.email` — le seul index unique de la table porte sur `name`. Confirmation côté code : `internal/modules/users/create_service.go:15-78` (`CreateUser`) valide que `Email` n'est pas vide (ligne 20), hache le mot de passe, génère un `user_id`, puis insère directement via `s.userRepo.CreateUser(...)` (`internal/modules/users/create_repository.go:13`, `INSERT INTO users`) — **sans jamais interroger la table pour un e-mail déjà existant.** Rien n'empêche donc, ni en base ni en code, la création de plusieurs comptes `users` avec le même e-mail.

**Champs liés à l'authentification externe** : aucun. Il n'y a pas de colonne `google_id`, `oauth_provider`, `external_auth_id` ou équivalent sur `users`. Voir 2.9 pour la recherche exhaustive d'authentification par fournisseur externe dans tout l'écosystème.

### 1.5. Structure de `users_rights`

Introspection live :

```sql
users_rights (
  id                            integer       NOT NULL   -- PK
  user_id                       varchar(64)   NULL
  merchant_id                   varchar(64)   NOT NULL
  token                         varchar(255)  NOT NULL
  enabled                       boolean       NOT NULL   DEFAULT true
  access_wrreception            boolean       NOT NULL   DEFAULT true
  position_id                   varchar(64)   NULL
  position_note                 text          NULL
  job_title                     varchar(150)  NULL
  role                          varchar(32)   NOT NULL   DEFAULT 'employee'   -- ancien système, chaîne libre
  contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date, contract_hours,
  max_weekly_hours, required_rest_days, sunday_premium, night_premium, hourly_rate, gross_monthly_salary,
  employer_charges_pct, transport_cost, hr_comment                          -- champs RH dupliqués avec ceux d'`employees` (voir 1.6)
  manage_menu / manage_plannings / manage_users / manage_settings / manage_haccp   boolean   NOT NULL   DEFAULT false
  view_reports / view_financials / manage_customers                          boolean   NOT NULL   DEFAULT false
  admin                          boolean       NOT NULL   DEFAULT false
  print_merchant_cash_report / open_cash_drawer                             boolean   NOT NULL   DEFAULT false
  last_login_at                  timestamptz   NOT NULL   DEFAULT now()
  login_enabled                  boolean       NOT NULL   DEFAULT true
  pin_hash                       varchar(64)   NULL
  role_id                        varchar(64)   NULL       -- FK -> roles(id), nouveau système
)
PRIMARY KEY (id)
FOREIGN KEY (role_id) REFERENCES roles(id)
INDEX idx_users_rights_role_id ON (role_id)
```

**Stockage des permissions : deux systèmes coexistent sur cette même table**, ni JSON ni bitmask :
1. **Un ensemble de colonnes booléennes « à plat »** (`manage_menu`, `manage_plannings`, `manage_users`, `manage_settings`, `manage_haccp`, `view_reports`, `view_financials`, `manage_customers`, `admin`, `print_merchant_cash_report`, `open_cash_drawer`), plus la colonne `role varchar(32)` (chaîne libre, ex. `'employee'`) — c'est le système historique.
2. **Un système normalisé plus récent** : `role_id` (FK vers `roles.id`), où `roles` est reliée à `permissions` via la table de jointure `role_permissions` (`role_id`, `permission_key` → `permissions.key`). Le détail de l'usage effectif de chacun des deux systèmes (lequel est encore lu au moment de l'autorisation d'une requête, lequel est mort) est traité en section 3.2.

**Gestion du PIN** : la table porte `pin_hash varchar(64)` (nullable) — un PIN haché. Notez que la table `users` (section 1.4) porte séparément une colonne `pin_code varchar(6)` (nullable) — deux colonnes de PIN distinctes existent donc dans le schéma, une sur `users` et une sur `users_rights`. Le détail de laquelle est effectivement utilisée par `/auth/pin` et sous quelle forme (haché ou non) est traité en section 2.6.

**Rattachement au marchand** : `merchant_id varchar(64) NOT NULL` — c'est cette table, et non `users.merchant_id`, qui porte le rattachement effectif d'un utilisateur à un marchand (voir 1.7).

### 1.6. Structure de `employees` et son lien vers `users`

Introspection live (colonnes principales) :

```sql
employees (
  id                 varchar(64)   NOT NULL   -- PK
  merchant_id        varchar(64)   NOT NULL
  user_id            varchar(64)   NULL       -- nullable, aucune contrainte FK déclarée vers users.user_id
  member_id          bigint        NULL
  first_name / last_name   varchar(150)  NOT NULL
  position_id        varchar(64)   NOT NULL
  ... (contrat de travail : contract_type_code, contract_hours, hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, etc.)
  active / enabled   boolean       NOT NULL   DEFAULT true
  deleted_at         timestamptz   NULL       -- soft delete dédié (en plus de enabled)
  created_at / updated_at   timestamptz
)
PRIMARY KEY (id)
UNIQUE (merchant_id, user_id)     -- uq_employees_uq_employees_merchant_user
UNIQUE (merchant_id, member_id)   -- uq_employees_uq_employees_merchant_member
```

`user_id` est **nullable et sans contrainte `FOREIGN KEY`** déclarée vers `users.user_id` — confirmé par l'introspection live des contraintes de la table (`pg_constraint` ne liste aucune `FOREIGN KEY` sur `employees`, uniquement des `NOT NULL`, la clé primaire et les deux index uniques ci-dessus).

Concernant l'historique de cette nullabilité : la migration d'origine de la table, `migrations/done/014_planning_socle.sql:115-118` (syntaxe MySQL, dossier `done/`), déclare déjà `user_id VARCHAR(64) NULL` **dès la création de la table** :
```sql
CREATE TABLE IF NOT EXISTS employees (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NULL,
  ...
```
Aucune autre migration du dépôt (`migrations/done/` ni `migrations/todo/`) ne contient d'`ALTER TABLE employees ALTER COLUMN user_id` ou équivalent MySQL (`MODIFY user_id`). Sur la seule base des fichiers de migration présents dans ce dépôt, `user_id` est donc nullable **depuis la création de la table**, pas à la suite d'une modification ultérieure documentée dans `migrations/`. Un `employee` peut donc exister sans être lié à aucun compte `users` (une fiche RH sans accès de connexion) — c'est un état permis et non un vestige transitoire visible dans l'historique des migrations du dépôt.

L'unicité `(merchant_id, user_id)` (et non une unicité globale sur `user_id` seul) confirme structurellement qu'un même `user_id` peut apparaître dans plusieurs lignes `employees` pour des `merchant_id` différents — voir 1.7.

### 1.7. Un utilisateur peut-il être rattaché à plusieurs marchands ?

**Oui, structurellement et en pratique — le rattachement se fait via `users_rights`, pas via `users.merchant_id`.**

**Par le schéma** : `users_rights.merchant_id` est `NOT NULL` mais il n'existe **aucune contrainte d'unicité sur `user_id` seul** dans `users_rights` — un même `user_id` peut donc apparaître dans plusieurs lignes `users_rights`, une par marchand. `employees` suit le même principe : unicité `(merchant_id, user_id)`, pas sur `user_id` seul (voir 1.6). `users.merchant_id`, en comparaison, est nullable et **ne sert donc pas de source de vérité** pour le rattachement — c'est un champ hérité, à confirmer/nuancer par la section 2 (login) sur son usage réel.

**Par le code** :
- `internal/modules/users/create_service.go:56-72` (`CreateUser`) : dans une même transaction, crée la ligne `users`, puis — seulement si un `merchantID` est fourni — appelle `s.userRepo.UpsertMerchantUserRights(txCtx, userID, merchantID, rightsToken, rights)`. Un utilisateur peut donc être créé sans aucun marchand, ou avec un premier marchand.
- `internal/modules/users/admin_repository.go:251-262` (`UpsertMerchantUserRights`) : la requête `SELECT id, enabled FROM users_rights WHERE merchant_id = ? AND user_id = ? ORDER BY id DESC LIMIT 1` cherche une ligne existante **par la paire (merchant_id, user_id)**, pas par `user_id` seul — un appel ultérieur de cette même fonction avec un `merchantID` différent pour le même `userID` crée une **nouvelle** ligne `users_rights` au lieu d'écraser la précédente. C'est le mécanisme exact par lequel un même utilisateur est rattaché à plusieurs marchands.
- `internal/modules/auth/repository.go:577-609` (`RotateRightsTokensForUser`) : le commentaire de la fonction dit explicitement *« issues a fresh session token for every merchant link of a user »* et la requête `SELECT id, token FROM users_rights WHERE user_id = ?` (ligne 587, sans filtre `merchant_id`) itère sur **toutes** les lignes `users_rights` d'un `user_id` donné pour en faire tourner le token — preuve directe, en code de production (pas seulement en test), que la fonctionnalité multi-marchand est réellement exercée et pas seulement permise par le schéma.

Le détail de la façon dont le flux `/auth/login` choisit — ou fait choisir à l'utilisateur — le marchand actif lorsqu'il en existe plusieurs est traité en section 2.1.

## 2. Authentification

### 2.1 Flux complet de `/auth/login`

**Constat majeur : il n'y a AUCUN JWT dans ce backend.** Aucune bibliothèque JWT n'est importée (`go.mod`/`go.sum` : aucune occurrence de `jwt`, `internal/modules/notification/token_manager.go` est le seul fichier matchant `jwt` en recherche insensible à la casse et il s'agit d'un JWT signé RS256 pour l'API FCM/Google, sans rapport avec l'authentification utilisateur). Le mécanisme réel est un **token opaque persistant**, généré par `crypto/rand`, stocké en clair dans la colonne `users_rights.token` (varchar(255)), sans expiration ni claims d'aucune sorte.

**Route** : `cmd/api/routes.go:589-591`
```go
r.Route("/auth", func(r chi.Router) {
    r.Get("/login", authH.Login)
    r.Post("/login", authH.Login)
```

**Handler** — `internal/modules/auth/handler.go:21-47` :
```go
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

Il existe un doublon mort déclaré juste en dessous, `LoginOld` (`handler.go:50-67`), toujours enregistré mais jamais raccroché à une route (`grep` sur `routes.go` ne montre aucun `LoginOld`) — c'est du code mort conservé dans le fichier.

**Service** — `internal/modules/auth/service.go:308-361`, fonction `Login` :
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
			err = s.repo.UpdateMFAStatus(ctx, user.UserID, models.MFAStatusPending)
			if err != nil { ... }
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

**Repository** — `internal/modules/auth/repository.go:218` (`Login`) exécute une jointure massive (`users` × `users_rights` × `merchant` × `roles` × `merchant_parameters` × `subscriptions` × `packages` × `scannorder_settings` × `integration_uber_eats` × `integration_uber_direct` × `integration_deliveroo`, 74 colonnes) puis :

```go
WHERE
    (
        (UPPER(u.name)=UPPER(?) AND u.name <> '' AND u.name IS NOT NULL)
        OR (UPPER(u.email)=UPPER(?) AND u.email <> '' AND u.email IS NOT NULL)
        OR ur.token = ?
    )
LIMIT 1;
```

Puis (`repository.go:403-417`) :
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

C'est-à-dire : le login accepte **trois voies** — nom d'utilisateur + mot de passe, email + mot de passe, **ou directement le token existant** (`ur.token = ?`, sans mot de passe) — c'est ce troisième chemin qui est réutilisé par `AuthenticatePIN` (voir §2.6) et par `ConfirmPasswordReset`. Il existe une migration automatique et silencieuse des mots de passe legacy (non préfixés `$2`, donc non bcrypt) vers bcrypt **au coût 10** (`bcrypt.DefaultCost`, `helpers.HashPassword` dans `services_helpers.go`), distincte du coût 12 utilisé partout ailleurs (`helpers.HashUserPassword`, `internal/helpers/password.go:13-22`) — anomalie documentée explicitement dans `docs/PASSWORD_RESET.md` (§D13) et laissée telle quelle.

**"Claims", durée de validité, secret** : ces trois notions n'existent pas dans ce système.
- Pas de claims : le "token" est une chaîne hex aléatoire opaque (`helpers.GenerateToken(30)` → 60 caractères hex, `internal/helpers/ids.go:77-83`, utilisant `crypto/rand`), sans structure ni payload encodé.
- Pas de durée de validité intrinsèque : `users_rights.token` n'a ni colonne `expires_at` ni TTL en base — il reste valide indéfiniment jusqu'à ce qu'il soit explicitement régénéré (rotation, voir §2.2).
- Pas de secret de signature : il n'y a rien à signer/vérifier, la validité est un `SELECT ... WHERE ur.token = ?` en base (`GetUserByToken`, `repository.go:42-165`), avec un cache Redis à durée de vie propre (`models.UserCacheTTL = 60 * time.Minute`, `internal/models/redis_models.go:10`, clé `user:token:v2:` + token, `redis_models.go:27`) qui est un **cache**, pas la source de vérité.

**Réponse `buildLoginResponse`** (`service.go:402-628`) construit un objet massif : `Session` (token, merchant_id, statut MFA), `User`, `Merchant` (+ `Settings`), `Access` (booléens dérivés de `Has()`), `Capabilities` (modules/order-types/actions/integrations dérivés des permissions + des flags d'abonnement), `Permissions` (liste brute des clés du catalogue accordées si `role_id` est renseigné), `Integrations`, `SNOSettings`, et un objet `Legacy` marqué explicitement comme `// Deprecated compatibility payload for existing clients` (`service.go:496-548`).

### 2.2 Gestion du "refresh token"

**Il n'existe pas de refresh token au sens JWT/OAuth** dans le flux d'authentification utilisateur. Le token émis à `/auth/login` :

- est stocké en base dans `users_rights.token` (une ligne par lien `user_id ↔ merchant_id`, donc un utilisateur multi-établissements a **un token distinct par établissement** — voir `GetMerchants`, `repository.go:793-827`, qui liste tous les `(merchant, token)` d'un utilisateur) ;
- est mis en cache Redis sous `user:token:v2:<token>` avec un TTL de 60 minutes (`models.UserCacheTTL`) — mais ce TTL ne fait qu'expirer le **cache**, pas le token : `GetUserByToken` retombe sur la base en cas de miss (`repository.go:391-393`, log explicite "No user found for..."), donc le token reste utilisable indéfiniment tant que la ligne `users_rights` existe et est `enabled = TRUE`/`login_enabled = TRUE` ;
- n'est **jamais rafraîchi automatiquement** par le client : il n'y a pas d'endpoint `/auth/refresh` pour les utilisateurs humains.

La seule vraie "rotation" de ce token se produit dans des cas métier précis :
1. **Réinitialisation de mot de passe réussie** (`RotateRightsTokensForUser`, `repository.go:584-628`) : régénère le token de **toutes** les lignes `users_rights` de l'utilisateur (tous établissements) et purge les entrées Redis correspondantes — déconnexion totale.
2. **Force-reset admin** (`ForceResetPassword` côté module `users`, cité dans `docs/PASSWORD_RESET.md` §D10) : rotationne les tokens de tous les autres établissements, en conservant celui de la session courante.
3. **Changement de rôle / permissions** (`invalidateTokens`, `internal/modules/roles/service.go:606-623`) : ne régénère **pas** le token, purge seulement la clé Redis — l'utilisateur reste connecté avec le même token, seul le cache est vidé (rechargement des droits à la requête suivante).

Il existe en revanche un vrai mécanisme de **refresh** pour un objet distinct : le **token d'appareil kiosk**. `cmd/api/routes.go:1621` enregistre `POST /kiosk/auth/token/refresh → kioskHandler.RefreshDeviceToken`, géré par un `KioskAuthService.ValidateAccessToken` (`internal/middleware/kiosk_auth.go:29-31`) totalement distinct du middleware d'authentification utilisateur (`middleware.Auth`). C'est un mécanisme séparé pour l'authentification des bornes physiques, sans rapport avec l'authentification des comptes `users`/`users_rights`.

Il n'y a ni cookie de session ni token stocké côté navigateur/app géré par le backend : le stockage du token côté client (localStorage, secure storage mobile, etc.) est entièrement du ressort des clients (`wello-back-office`, `wello_resto_flutter`), hors périmètre de ce backend.

### 2.3 Code intégral de `authMiddleware` (`Auth`)

`internal/middleware/auth.go`, fonction `Auth` (l.29-122) — le middleware réellement appliqué (`authMiddleware := middleware.Auth(&authService)`, `cmd/api/routes.go:200`) :

```go
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Clé typée pour le contexte — évite les collisions avec d'autres valeurs du contexte
type contextKey string

const userContextKey contextKey = "authenticatedUser"

var ErrUnunauthenticated = errors.New("utilisateur non authentifié")

// AuthService est l'interface que ton authService doit satisfaire
type AuthService interface {
	GetUserByToken(ctx context.Context, token string) (*auth.UserLoginRow, error)
	UpdateMFAStatus(ctx context.Context, userID string, status string) error
	IsMFAVerificationRequired(ctx context.Context, user *auth.UserLoginRow) bool
}

// Auth est le middleware d'authentification principal
func Auth(service AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "bearer ") {
				token = authHeader[7:]
			}
			token = strings.TrimSpace(token)

			if token == "" {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"format token invalide"}`, http.StatusUnauthorized)
				return
			}

			// 3. Récupérer le user (inchangé)
			user, err := service.GetUserByToken(r.Context(), token)
			if err != nil || user == nil {
				SetCORSHeaders(w, r)
				http.Error(w, `{"error":"token invalide ou expiré"}`, http.StatusUnauthorized)
				return
			}

			// --- NOUVELLE LOGIQUE MFA ---
			isBackoffice := r.Header.Get("X-App-Source") == "backoffice"

			if isBackoffice && service.IsMFAVerificationRequired(r.Context(), user) {
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

			// 4. Injecter le user (inchangé)
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(r *http.Request) *auth.UserLoginRow {
	user, _ := r.Context().Value(userContextKey).(*auth.UserLoginRow)
	return user
}

func UserFromContext(ctx context.Context) (*auth.UserLoginRow, error) {
	user, ok := ctx.Value(userContextKey).(*auth.UserLoginRow)
	if !ok || user == nil {
		return nil, ErrUnunauthenticated
	}
	return user, nil
}

func WithUser(ctx context.Context, user *auth.UserLoginRow) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func MustGetUser(w http.ResponseWriter, r *http.Request) (*auth.UserLoginRow, bool) {
	user := GetUser(r)
	if user == nil {
		http.Error(w, `{"error":"utilisateur non authentifié"}`, http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}
```

Points factuels notables :
- Le middleware **appelle directement `net/http` (`http.Error`)** pour les rejets 401 liés au token, au lieu de passer par `models.SendErrorJSON`, sauf pour le cas MFA qui, lui, utilise `models.SendJSON`.
- Une redirection MFA vers un blocage total est appliquée **uniquement si** `X-App-Source: backoffice` — le POS/kiosk n'est donc jamais bloqué par le MFA à ce niveau middleware, seulement au login (`isBackoffice` dans `Login`).
- Le commentaire `// --- NOUVELLE LOGIQUE MFA ---` et les noms de variables (`// ✅ IMPORTANT`, emojis dans les logs du service) trahissent un style de développement assisté par IA / itératif, cohérent sur tout le module `auth`.

### 2.4 `RequirePermission` — et l'absence de `AnyOf`/`AllOf`

**`AnyOf`/`AllOf` n'existent plus dans le code.** Une recherche exhaustive (`grep -rn "AnyOf\|AllOf" --include=*.go .`) ne retourne qu'une seule occurrence, dans un **commentaire** de `internal/middleware/require_permission.go:64`, qui documente leur suppression :

> « RBAC lot 2 : la signature est passée de `RequirePermission(...PermissionFunc)` (logique AND sur plusieurs prédicats combinables via `AnyOf`/`AllOf`) à `RequirePermission(key permission.Key)` — une seule clé du catalogue. Aucune route réelle ne combinait plusieurs prédicats au moment de la bascule […], donc rien ne s'est perdu. »

Il n'existe donc **aucun combinateur** actif aujourd'hui : chaque route protégée ne peut exiger qu'**une seule** clé `permission.Key`. Code intégral de `internal/middleware/require_permission.go` :

```go
package middleware

import (
	"net/http"

	"welloresto-api/internal/middleware/rbacobserve"
	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"

	"github.com/go-chi/chi/v5"
)

// rbacObserver is nil unless EnableRBACObservation is called from
// SetupRoutes (gated by the RBAC_OBSERVE env var, default off).
var rbacObserver *rbacobserve.Observer

func EnableRBACObservation(o *rbacobserve.Observer) {
	rbacObserver = o
}

func observeDecision(r *http.Request, user *auth.UserLoginRow, key permission.Key, granted bool) {
	if rbacObserver == nil {
		return
	}
	rbacObserver.Observe(rbacobserve.Observation{
		MerchantID:    user.MerchantID,
		UserID:        user.UserID,
		PermissionKey: string(key),
		Route:         r.Method + " " + routePattern(r),
		Granted:       granted,
	})
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}

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

func renderError(w http.ResponseWriter, r *http.Request, code string, status int) {
	SetCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + code + `"}`))
}
```

Le commentaire au-dessus de la fonction mentionne aussi qu'une garde distincte, `RequireAdmin` (« détient tous les droits », indépendante du catalogue), a été **retirée en RBAC lot 11 phase 4** — ses deux derniers appelants (`POST /users/{id}/force-reset-password`, `DELETE /users/{id}/merchant-link`) sont passés sous `RequirePermission(permission.StaffManage)`. `RequireAdmin` n'existe donc plus du tout dans le code actuel.

Un module d'observation optionnel (`internal/middleware/rbacobserve/`) enregistre, si `RBAC_OBSERVE=true`, chaque décision d'accès (accordée ou refusée) de façon asynchrone — c'est le mécanisme derrière la table `access_observation` mentionnée dans les faits déjà connus du schéma ; il est désactivé par défaut (`rbacObserver` nil).

### 2.5 Extraction et propagation de `merchant_id`

`merchant_id` n'est **jamais lu depuis un paramètre de requête ou le corps** dans les couches protégées : il vient exclusivement de l'utilisateur authentifié injecté dans le contexte par `middleware.Auth` (voir §2.3), sous la forme du champ `UserLoginRow.MerchantID` (`internal/modules/auth/models.go:195`), lui-même issu de `users_rights.merchant_id` (colonne du login SQL, `repository.go:88` / `repository.go:158`).

Le chemin d'usage typique — handler → service → repository — illustré par `GET /users` (`ListMerchantUsers`) :

**Handler**, `internal/modules/users/admin_handler.go:16-28` — ne touche pas au `merchant_id` du tout, il délègue entièrement au service :
```go
func (h *UsersHandler) ListMerchantUsers(w http.ResponseWriter, r *http.Request) {
	filters, err := parseMerchantUserListFilters(r)
	if err != nil {
		models.SendErrorJSON(w, "users", "list", err)
		return
	}
	items, metadata, err := h.svc.ListMerchantUsers(r.Context(), filters)
	...
}
```

**Service**, `internal/modules/users/admin_service.go:18-31` — extrait `merchant_id` du contexte via `middleware.UserFromContext` et le passe explicitement au repository :
```go
func (s *UsersService) ListMerchantUsers(ctx context.Context, filters MerchantUserListFilters) ([]MerchantUserListItem, models.PaginationMetadata, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.PaginationMetadata{}, models.ErrUnauthorized
	}
	pagination := normalizeUsersPagination(filters.Page, filters.PageSize)
	filters.Page = pagination.CurrentPage
	filters.PageSize = pagination.Limit
	items, totalItems, err := s.userRepo.ListMerchantUsers(ctx, user.MerchantID, filters)
	...
}
```

**Repository**, `internal/modules/users/admin_repository.go:13-25` — filtre la requête SQL directement sur ce paramètre :
```go
func (r *UsersRepository) ListMerchantUsers(ctx context.Context, merchantID string, filters MerchantUserListFilters) ([]MerchantUserListItem, int, error) {
	db := dbx.GetDB(ctx, r.database)
	baseQuery := `
		FROM users_rights ur
		INNER JOIN users u ON u.user_id = ur.user_id
		LEFT JOIN (...) employee_link ON ...
		WHERE ur.merchant_id = ? AND ur.enabled = TRUE
	`
	args := []interface{}{merchantID}
	...
```

C'est le motif identique partout dans le code : `middleware.UserFromContext(ctx)` (ou `middleware.GetUser(r)` côté handler) extrait `*auth.UserLoginRow`, on lit `.MerchantID`, on le passe explicitement en paramètre de fonction jusqu'à une clause `WHERE merchant_id = ?` (ou l'équivalent `WHERE ur.merchant_id = ?`) en SQL — il n'y a pas de middleware de scoping automatique par tenant : chaque repository doit inclure manuellement le filtre. C'est cohérent avec la note déjà connue « il n'y a pas d'accès cross-tenant dans les flux normaux », mais cela signifie aussi qu'un repository qui **oublierait** ce filtre romprait l'isolation — rien dans l'architecture ne le garantit structurellement, chaque requête SQL doit être auditée individuellement pour ce risque.

### 2.6 `/auth/pin` — implémentation et délégation à `Login()`

Route : `cmd/api/routes.go:600` — `r.With(authMiddleware).Post("/pin", authH.AuthPIN)`. Elle **exige déjà un token valide** (« anchor token » — la session d'un utilisateur quelconque déjà connecté sur l'établissement, typiquement une tablette POS restée ouverte).

**Handler** — `internal/modules/auth/handler.go:185-213` :
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

**Service** — `AuthenticatePIN`, `internal/modules/auth/service.go:118-145`, **délègue explicitement à `Login()`** à la toute fin :
```go
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

Le PIN (4 chiffres, `PINLength = 4`, `internal/modules/auth/models.go:16`) est haché avec un poivre (`security.HashPIN(pin, s.pepper)`, poivre = variable d'environnement `PIN_PEPPER`, requise au démarrage — `config.go:76: log.Fatal("PIN_PEPPER is not set")`) et comparé côté base (`GetUserByPIN`, `merchant_id` scopé sur celui de l'ancre — donc un PIN n'est valable que pour rechercher un employé **du même établissement**). Une fois l'employé trouvé, `Login()` est rappelé avec le token permanent de cet employé (`employee.Token`) et un payload vide — c'est exactement le chemin `loggedByToken := token != "" && token == data.Token` de `repository.Login` décrit en §2.1, qui court-circuite toute vérification de mot de passe. La réponse de `/auth/pin` est donc, par construction, **identique** à celle de `/auth/login`.

Anti-brute-force : `checkLockout`/`incrementLockout`/`resetLockout` (`service.go:170-209`) — verrou en Redis, clé `PINLockoutPrefix + anchorToken` (donc **par ancre**, pas par employé visé), backoff exponentiel après `PINMaxAttempts = 5` échecs (`PINLockoutBase = 30s`, doublé tous les 5 essais supplémentaires, plafonné à 480s), TTL Redis `PINLockoutTTL = 1h`.

`SetPIN` (`/auth/pin/set`, self-service) et `ResetPIN` (`/auth/pin/reset`, protégée par `RequirePermission(permission.StaffManage)`) sont des endpoints distincts qui **ne délèguent pas** à `Login()` — ils gèrent uniquement `users_rights.pin_hash` (voir `handler.go:219-281`, `service.go:147-168`).

### 2.7 Flux "mot de passe oublié"

**Existe, et est intégralement implémenté** — dépôt riche en documentation (`docs/PASSWORD_RESET.md`, journal de décisions D1 à D16, déjà en partie cité ci-dessus). La table `password_resets` (Postgres uniquement, jamais existé côté MySQL — décision D8) **est bien utilisée** par le code.

**Routes publiques** (`cmd/api/routes.go:597-598`) :
```go
r.Post("/forgot-password", authH.ForgotPassword)
r.Post("/reset-password", authH.ResetPassword)
```

**Étape 1 — `POST /auth/forgot-password`** (`internal/modules/auth/handler.go:304-325`) :
```go
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

`SendPasswordResetLink` (`service.go:984-1026`) : throttle par IP (Redis, `PasswordResetIPThrottleMax = 20`/h, best-effort — `service.go:1034-1051`), puis `RequestPasswordReset` (`service.go:890-926`) qui : résout le compte (`GetUserForPasswordReset` — nom OU email, compte **activé** et avec un email non vide, `repository.go:482-513`), applique un rate-limit **par compte** en SQL (`CountPasswordResetsSince`, `PasswordResetMaxPerHour = 5`), génère un token clair de 32 octets (`PasswordResetTokenBytes`, 64 caractères hex), insère `sha256(token)` en base (`InsertPasswordReset`, jamais le clair) avec `expires_at = now() + 30min` (`PasswordResetTTL`), puis envoie l'email via Brevo (`s.email.SendPasswordReset`, template `internal/infrastructure/mailer/templates/password_reset.html`) si `PASSWORD_RESET_BASE_URL` est configurée. **Toute** branche d'échec (compte inconnu, désactivé, throttlé, non configuré) renvoie `200` de façon indiscernable — anti-énumération volontaire, documentée (D15).

**Étape 2 — `POST /auth/reset-password`** (`handler.go:331-354`) → `ConfirmPasswordReset` (`service.go:932-975`) :
```go
func (s *AuthService) ConfirmPasswordReset(ctx context.Context, token, newPassword string) error {
	if strings.TrimSpace(token) == "" {
		return ErrInvalidResetToken
	}
	if err := helpers.ValidatePassword(newPassword); err != nil {
		return err
	}

	userID, err := s.repo.ConsumePasswordResetToken(ctx, hashResetToken(token))
	if err != nil {
		return err
	}

	hash, err := helpers.HashUserPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, userID, hash); err != nil {
		return err
	}

	oldTokens, err := s.repo.RotateRightsTokensForUser(ctx, userID)
	if err != nil {
		log.Error("🔑 Password reset succeeded for user " + userID + " but session rotation FAILED...")
		return nil
	}

	if s.redis != nil {
		for _, old := range oldTokens {
			s.redis.Delete(ctx, models.UserCachePrefix+old)
		}
	}
	return nil
}
```

`ConsumePasswordResetToken` (`repository.go:555-575`) effectue un CAS SQL atomique via `UPDATE ... RETURNING` — usage unique garanti sans fenêtre de concurrence :
```sql
UPDATE password_resets
SET used_at = now()
WHERE token_hash = ?
  AND used_at IS NULL
  AND expires_at > now()
RETURNING user_id
```
Le mot de passe (règle unique : ≥8 caractères, `helpers.ValidatePassword`, `PasswordMinLength = 8`) est validé **avant** la consommation du token — un mot de passe refusé ne brûle pas le lien. Le succès régénère **tous** les tokens `users_rights` de l'utilisateur (tous établissements) et purge le cache Redis correspondant : déconnexion totale.

Client web : `wello-back-office/src/pages/ForgotPassword.tsx` et `ResetPassword.tsx` (routes publiques dans `App.tsx`, hors `ProtectedRoute`), lien « Mot de passe oublié ? » sur `Login.tsx`. Client POS : `wello_resto_flutter/lib/ui/widgets/dialogs/reinit_password_dialog.dart` — le POS ne fait que déclencher l'email, la saisie du nouveau mot de passe se fait uniquement sur le back-office (décision D1).

Purge : cron quotidien à 5h (`internal/tasks/password_resets.go`, cité dans `docs/PASSWORD_RESET.md`), rétention 7 jours, actif sur tous les environnements (cf. politique cron globale déjà connue de ce dépôt — pas de gate par `ENV`).

**État de déploiement documenté** : migration `078_password_resets` appliquée en dev et en staging, mais **NON appliquée en production** au moment de la rédaction du document (`docs/PASSWORD_RESET.md` §6) — à vérifier si cet état a changé depuis, la doc datant du 2026-08-02.

### 2.8 Vérification d'adresse e-mail

**Existe**, mais sous forme d'un mécanisme OTP générique (`SendVerificationCode`/`ConfirmVerification`), pas d'un lien de confirmation par email cliquable.

Routes : `cmd/api/routes.go:593-594` :
```go
r.Post("/send-verification", authH.SendVerification)
r.Post("/verify", authH.VerifyCode)
```

**`SendVerification`** (`handler.go:126-150`) → `SendVerificationCode` (`service.go:798-832`) : requiert un token valide (utilisateur déjà authentifié), génère un OTP à 6 chiffres (`helpers.GenerateOTP`), le stocke en clair en Redis sous `verify_email:<token>` ou `verify_sms:<token>` (`helpers.GetVerificationCacheKey`, TTL `OTPCacheTTL = 5min`), et l'envoie par email (mode `EMAIL`) via Brevo ou par SMS (mode `SMS`/`TEL`).

**`VerifyCode`** (`handler.go:153-179`) → `ConfirmVerification` (`service.go:835-851`) :
```go
func (s *AuthService) ConfirmVerification(ctx context.Context, token string, mode string, codeSaisi string) error {
	if strings.ToUpper(mode) == "MFA" {
		return s.VerifyMFA(ctx, token, codeSaisi)
	}
	cacheKey := helpers.GetVerificationCacheKey(mode, token)

	storedCode, found := s.redis.Get(ctx, cacheKey)
	if !found || storedCode != codeSaisi {
		return errors.New("code invalide ou expiré")
	}

	_ = s.redis.Delete(ctx, cacheKey)

	return s.repo.MarkAsVerified(ctx, token, mode)
}
```

**`MarkAsVerified`** (`repository.go:977-1010`) est bien le point d'écriture réel de `users.email_verified_at` :
```go
func (r *AuthRepository) MarkAsVerified(ctx context.Context, token string, mode string) error {
	db := dbx.GetDB(ctx, r.database)
	var column string

	switch strings.ToUpper(mode) {
	case "EMAIL":
		column = "email_verified_at"
	case "SMS", "TEL":
		column = "tel_verified_at"
	default:
		return errors.New("mode de vérification invalide")
	}

	query := fmt.Sprintf(`
		UPDATE users u
		SET %s = NOW()
		WHERE EXISTS (SELECT 1 FROM users_rights ur WHERE ur.user_id = u.user_id AND ur.token = ?)`, column)

	result, err := db.ExecContext(ctx, query, token)
	...
}
```

**Conclusion factuelle** : `users.email_verified_at` **est bien écrite par du code réel**, via `POST /auth/verify` avec `mode: "email"` — mais ce flux nécessite que le client déclenche explicitement `send-verification` puis `verify` (ce n'est pas un lien cliquable envoyé automatiquement à la création du compte ; rien dans `create_service.go` n'appelle `SendVerificationCode`). Aucune preuve dans le code que ce flux est aujourd'hui déclenché automatiquement quelque part côté serveur (création de compte, changement d'email) — il semble n'exister qu'en tant qu'action explicite initiée côté client. Une recherche complémentaire dans `wello-back-office` et `wello_resto_flutter` serait nécessaire pour confirmer si ces écrans sont réellement branchés côté UI (hors périmètre vérifié ici).

### 2.9 Authentification par fournisseur externe (Google Sign-In / OAuth / SSO)

**N'existe pas** pour l'authentification des utilisateurs, dans aucun des quatre dépôts.

- `internal/config/google.go` : `GOOGLE_API_KEY` est une clé pour l'API Google **Maps** (géocodage), sans lien avec un flux OAuth de connexion :
  ```go
  type GoogleConfig struct {
      APIKey string
  }
  func loadGoogle() GoogleConfig {
      return GoogleConfig{APIKey: os.Getenv("GOOGLE_API_KEY")}
  }
  ```
  Toutes les autres occurrences de "google" dans le code (`internal/modules/googlemaps/`, `internal/modules/deliverytime/estimate.go`, `internal/tasks/delivery_time.go`, etc.) relèvent de Google Maps/Places, pas d'authentification.
- Recherche exhaustive de "oauth"/"sso" dans le code Go : les seules occurrences sont dans `internal/modules/deliveroo/`, `internal/modules/ubereats/`, `internal/webhook/ubereats/` — il s'agit de l'**OAuth serveur-à-serveur** utilisé pour s'authentifier auprès des API Uber Eats / Deliveroo (intégrations livraison), jamais d'un OAuth pour authentifier un utilisateur final de la plateforme.
- La table `external_tokens` (confirmée en introspection live sur staging) ne contient **que** deux `token_type` : `uber_eats_bearer_token` et `uber_eats_bearer_token_sandbox`. Colonnes : `token_type varchar` (clé), `access_token text`, `expires_at timestamptz`. C'est un cache de jetons d'accès aux API partenaires (Uber Eats), utilisé par `internal/modules/ubereats/repository.go:207-230` (`SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?` / upsert), et référencé en commentaire dans `internal/modules/deliveroo/repository.go:28` comme piste "pour simplifier" mais pas encore implémenté pour Deliveroo (`// For simplicity call token endpoint each time (or store in external_tokens table)`) — Deliveroo n'a donc **pas** de token caché dans cette table aujourd'hui, à la différence d'Uber Eats. Rien à voir avec une connexion Google/SSO utilisateur.
- Aucun composant "Se connecter avec Google" ni bouton OAuth n'a été identifié dans `wello-back-office/src/pages/Login.tsx` (le seul écran de login trouvé dans ce dépôt lors de la recherche du flux mot de passe oublié, §2.7) — l'écran ne propose que nom d'utilisateur/email + mot de passe.

En résumé : le seul mécanisme d'authentification pour les comptes `users` sur toute la plateforme (API, back-office, POS Flutter, kiosk) est le couple identifiant/mot de passe (ou PIN, ou token existant), sans aucune fédération d'identité externe.

---

## 3. Permissions

### 3.1 Liste exhaustive des permissions

Le catalogue est déclaré en Go dans `internal/permission/keys_gen.go` et répliqué en base dans la table `permissions` — les deux sont maintenus synchronisés par un test dédié (`internal/permission/keys_gen_test.go`, `TestAllMatchesMigrationCatalog`) qui scanne l'ensemble des migrations `*.up.sql` pour reconstruire le catalogue attendu et échoue si le fichier Go diverge. **Confirmé par introspection live sur le Postgres de staging** : les deux listes contiennent exactement les **18 mêmes clés**, dans le même ordre `sort_order`.

| `sort_order` | Clé (`permission.Key`) | Domaine | Sensible | Libellé (`permissions.label`) |
|---|---|---|---|---|
| 15 | `pos.status.manage` | pos | non | Ouvrir et fermer l'établissement |
| 20 | `pos.ticket.reopen` | pos | **oui** | Rouvrir un ticket clôturé |
| 40 | `pos.refund` | pos | **oui** | Rembourser une vente |
| 50 | `pos.cash_drawer.open` | pos | **oui** | Ouvrir le tiroir-caisse hors encaissement |
| 55 | `pos.analytics` | pos | non | Consulter les analyses de vente |
| 60 | `catalog.manage` | catalog | **oui** | Gérer les produits, les tarifs et les cartes |
| 70 | `inventory.manage` | inventory | non | Gérer les stocks et les inventaires |
| 80 | `haccp.manage` | haccp | non | Gérer le suivi HACCP |
| 90 | `customers.manage` | customers | **oui** | Gérer et exporter les fiches clients |
| 100 | `staff.manage` | staff | **oui** | Gérer les employés, les postes, les rôles et les droits |
| 110 | `staff.schedule.manage` | staff | non | Gérer le planning et les pointages |
| 120 | `reports.sales.read` | reports | non | Consulter et exporter les rapports de vente |
| 130 | `reports.financial.read` | reports | **oui** | Consulter et exporter les rapports financiers |
| 135 | `reports.staff_performance.read` | reports | **oui** | Consulter les analyses nominatives par salarié |
| 140 | `settings.manage` | settings | **oui** | Paramétrer l'établissement |
| 150 | `bookings.manage` | bookings | non | Paramétrer les réservations |
| 160 | `platforms.manage` | platforms | non | Gérer les canaux et plateformes |
| 170 | `kiosk.manage` | kiosk | non | Gérer les bornes Kiosk |
| 180 | `seating_plan.manage` | seating_plan | non | Gérer le plan de salle |

Aucune ligne `deprecated_at` non nulle trouvée en base à ce jour.

**Clés déjà retirées du catalogue** (dead code documenté, ne plus jamais réutiliser) : `pos.access` et `pos.discount.apply`, supprimées par la migration `100_deprecate_pos_access_and_discount_apply.up.sql` (RBAC lot 8, 2026-08-27, `internal/permission/keys_gen.go:11-21`). Le commentaire du fichier précise que ni l'une ni l'autre ne gardait de route réelle au moment de leur retrait.

**Trois clés du catalogue n'ont jamais eu d'équivalent booléen "legacy"** : `pos.ticket.reopen`, `pos.refund`, `inventory.manage` — historiquement, seul `Rights.Admin` les accordait (`internal/modules/auth/permissions.go:11-13`).

**Champs booléens "legacy" retirés purement et simplement** (aucun fallback, aucune permission ne les remplace) lors du "dead-rights cleanup" du 2026-08-27 : `AccessWaiter`, `AccessDelivery`, `CanExportReports`, `CanExportFinancials`, `CanExportCustomers` — plus aucune trace dans `UserRowRights` (`internal/modules/auth/models.go:126-149`) ni dans `legacyPermissionFallback`.

### 3.2 Rôles / profils de permissions — utilisation réelle

**Oui**, le concept existe et est activement utilisé. Résolution des droits effectifs au moment d'une requête (`internal/modules/auth/permissions.go`, fonction `Has`) :

```go
func (u *UserLoginRow) Has(key permission.Key) bool {
	if u.RoleID != nil {
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
```

Deux mondes coexistent explicitement (commenté dans le code, `permissions.go:38-47`) :
- **`RoleID` renseigné** (`users_rights.role_id` non nul) : les droits viennent **uniquement** de `Permissions` — la liste chargée par `attachRolePermissions`/`loadRolePermissions` (`repository.go:432-462`, une seconde requête `SELECT permission_key FROM role_permissions WHERE role_id = ?`, filtrée ensuite par `permission.FilterValid`). Les colonnes booléennes `manage_*`/`admin` de `users_rights` sont **totalement ignorées** dans ce cas — **même si elles contredisent le rôle**.
- **`RoleID` nul** : on retombe intégralement sur les colonnes booléennes historiques via `legacyPermissionFallback` (`permissions.go:22-33`), avec `Rights.Admin` qui court-circuite tout en premier.

**État réel en base (staging, introspection live)** : `users_rights.role_id` est renseigné pour **58 lignes sur 59** — un seul lien encore en "monde legacy". La bascule est donc, en pratique, quasiment terminée sur cet environnement.

**Le court-circuit "admin toujours vrai" a été retiré du rôle admin lui-même** (RBAC lot 11 phase 3, commenté `permissions.go:49-61`) : un utilisateur avec `RoleID` pointant vers le rôle système `admin` n'obtient ses droits que par les lignes réellement présentes dans `role_permissions` pour ce rôle — pas par un `if role == admin { return true }`. Cela n'est jugé sûr par les auteurs que parce qu'un invariant testé (`TestSystemAdminRolesContainFullCatalog_Postgres`) et une tâche de réconciliation automatique (`internal/tasks/rbac.go`) garantissent que le rôle admin de chaque établissement porte l'intégralité du catalogue. Le rôle admin reste néanmoins **immuable et non supprimable** en écriture applicative (garde G4, `models.ErrRoleImmutable` — voir `roles/service.go:224-227`, `:285-289`, `:396-398`).

**Résolution "quel rôle a créé ce grant" pour l'affichage** : `HasAdminRole()` (`permissions.go:82-104`) est un accesseur *display-only*, distinct de `Has()` — utilisé par le champ `admin` de la réponse de login et par `is_admin` de `GET /me/permissions`. Un ancien `IsAdmin()` qui lisait `Rights.Admin` brut a été supprimé en même temps que `RequireAdmin` (RBAC lot 11 phase 4) — c'était un chemin d'autorisation redondant hors du catalogue.

**Résolution effective d'une requête HTTP** : `RequirePermission(key)` (§2.4) appelle `user.Has(key)` directement sur l'objet `*auth.UserLoginRow` déjà chargé et mis en cache par le middleware d'authentification — il n'y a pas de re-résolution de rôle par requête au-delà de ce qui a été chargé au login/à la mise en cache (TTL 60 min, `UserCacheTTL`) : un changement de permissions du rôle prend effet immédiatement pour les nouvelles sessions/re-authentifications, et pour les sessions déjà en cache uniquement après invalidation explicite (`roles.Service.invalidateTokens`, appelée systématiquement à chaque mutation d'un rôle — création, permissions, archivage, changement d'assignation).

**Service `internal/modules/roles/`** expose : `ListPermissionCatalog` (regroupe par domaine), `MyPermissions` (permissions effectives + rôle du user courant, via `Has()` — garantit la cohérence avec ce que `RequirePermission` déciderait), `ListRoles`/`GetRole`/`CreateRole`/`UpdateRole`/`ArchiveRole`, `ReplacePermissions` (remplacement intégral, pas un diff), `SetUserRole`, `SetMerchantDefaultRole`. Garde-fous métier codés en dur :
- **G1** — impossible de modifier ses propres permissions de rôle ou sa propre assignation de rôle (`ErrRoleSelfModification`).
- **G2** — impossible de retirer `staff.manage` d'un rôle (ou de réassigner le dernier détenteur) si cela laisserait l'établissement sans aucun détenteur actif de `staff.manage` (`ErrRoleStaffManageRequired`).
- **G4** — le rôle système `admin` est immuable (nom, description, permissions, archivage tous bloqués).
- **G5/G6** — un rôle avec des détenteurs actifs ne peut pas être archivé ; le rôle `staff` ne peut pas être archivé tant qu'il est le rôle par défaut de l'établissement (`merchant.default_role_id`).

**Colonnes "legacy" `users_rights.manage_*`/`admin` — mortes ou vivantes ?** Réponse factuelle nuancée :
- **Vivantes en lecture** dans `legacyPermissionFallback` — mais seulement pour les utilisateurs sans `role_id` (1 sur 59 en staging aujourd'hui).
- **Vivantes en lecture directe, hors `Has()`**, à un seul endroit relevé dans le code exploré : `access.Permissions.PrintMerchantCashReport` dans `buildLoginResponse` (`service.go:433`) lit `user.Rights.PrintMerchantCashReport` directement — commentaire explicite : « n'a pas d'équivalent dans le catalogue et reste lu directement sur la colonne historique » (`service.go:429-431`). C'est la seule permission "legacy" qui n'a **pas** été portée dans `permission.All` et qui continue donc, par construction, à ignorer le système de rôles pour tout utilisateur (y compris avec `role_id` renseigné).
- **Écrites encore aujourd'hui** à la création (`defaultMerchantUserRights`/`UpsertMerchantUserRights`, `internal/modules/users/admin_repository.go:251-...`) et lors des mises à jour manuelles de droits (`PUT /users/{id}/rights`) — donc pas du code mort côté écriture non plus, même si leur lecture est court-circuitée dès qu'un `role_id` est assigné.
- Le champ `Capabilities.Actions.PrintMerchantCashReport` de la réponse de login (`service.go:470-478`) est, lui, une **constante `true`** — commentaire : la garde `CanPrintCashReport()` a été décommissionnée (RBAC lot 12) et « ce qu'elle gardait est ouvert à tous pour l'instant », le champ JSON restant émis uniquement pour compatibilité avec les clients qui le parsent déjà.

### 3.3 Attribution des droits à la création d'un utilisateur — back-office

**Deux composants distincts** de création existent dans `wello-back-office`, non unifiés :

1. `src/components/users/UserCreateSheet.tsx` (170 lignes), monté depuis `src/pages/Users.tsx:71` — formulaire minimal (prénom/nom/email/téléphone uniquement), **aucun champ de droits ni de rôle**, `usersService.createUser` appelle simplement `POST /users` sans objet `rights`.
2. `src/components/team/CreateMemberSheet.tsx` (435 lignes), monté depuis `src/pages/equipe/EquipePage.tsx:366` — c'est l'écran réellement utilisé pour la gestion d'équipe et le seul qui expose des champs de droits. **Code (résumé fidèle, structure JSX répétitive condensée) ci-dessous.**

```tsx
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import {
  Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Search, Link2, AlertCircle, UserPlus } from "lucide-react";

import { usersApi, planningPositionsApi, planningRefsApi } from "@/services/welloApi";
import { qk } from "@/lib/queryKeys";
import type { LinkableUser, CreateUserRequest } from "@/types/adminUsers";

const SENTINEL_NONE = "__none__";

export function CreateMemberSheet({ open, onOpenChange, onSuccess }: CreateMemberSheetProps) {
  const [mode, setMode] = useState<"create" | "link">("create");
  useEffect(() => { if (open) setMode("create"); }, [open]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full sm:max-w-xl !p-0 overflow-hidden flex flex-col">
        <div className="shrink-0 border-b border-border bg-background px-6 pt-6 pb-4">
          <SheetHeader>
            <SheetTitle>Ajouter un membre</SheetTitle>
            <SheetDescription>
              Créez un nouveau compte ou liez un utilisateur existant à cet établissement.
            </SheetDescription>
          </SheetHeader>
        </div>
        <Tabs value={mode} onValueChange={(v) => setMode(v as "create" | "link")} className="flex min-h-0 flex-1 flex-col px-6 pb-6">
          <TabsList className="mt-4 grid w-full grid-cols-2 shrink-0">
            <TabsTrigger value="create"><UserPlus className="h-4 w-4 mr-2" />Nouveau membre</TabsTrigger>
            <TabsTrigger value="link"><Link2 className="h-4 w-4 mr-2" />Lier un existant</TabsTrigger>
          </TabsList>
          <TabsContent value="create" className="mt-4 flex-1 min-h-0 overflow-y-auto">
            <CreateForm onSuccess={() => { onSuccess?.(); onOpenChange(false); }} />
          </TabsContent>
          <TabsContent value="link" className="mt-4 flex-1 min-h-0 overflow-y-auto">
            <LinkForm onSuccess={() => { onSuccess?.(); onOpenChange(false); }} />
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  );
}

function CreateForm({ onSuccess }: { onSuccess: () => void }) {
  const queryClient = useQueryClient();
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
  const [error, setError] = useState<string | null>(null);

  const { data: positions = [] } = useQuery({ queryKey: qk.planningPositions.all, queryFn: () => planningPositionsApi.list() });
  const { data: contractTypes = [] } = useQuery({ queryKey: qk.planningRefs.contractTypes, queryFn: () => planningRefsApi.contractTypes() });

  const mutation = useMutation({
    mutationFn: (payload: CreateUserRequest) => usersApi.create(payload),
    onSuccess: () => {
      toast.success("Membre créé");
      queryClient.invalidateQueries({ queryKey: qk.users.all });
      onSuccess();
    },
    onError: (err) => {
      const msg = err instanceof Error ? err.message : "Erreur lors de la création";
      setError(msg);
      toast.error(msg);
    },
  });

  const handleSubmit = () => {
    setError(null);
    if (!firstName.trim() || !lastName.trim() || !email.trim()) {
      setError("Prénom, nom et email sont obligatoires.");
      return;
    }
    const payload: CreateUserRequest = {
      first_name: firstName.trim(),
      last_name: lastName.trim(),
      email: email.trim(),
      tel: tel.trim() || undefined,
      password: password ? password : undefined,
      rights: { admin, login_enabled: loginEnabled },
      planning: {
        ...(positionId ? { position_id: positionId } : {}),
        ...(role ? { role } : {}),
        ...(contractTypeCode ? { contract_type_code: contractTypeCode } : {}),
      },
    };
    mutation.mutate(payload);
  };

  return (
    <div className="space-y-5 py-2">
      {/* Identité : Prénom*, Nom*, Email*, Téléphone, Mot de passe (facultatif, généré si vide) */}
      {/* Accès : Switch "Administrateur" (booléen legacy rights.admin), Switch "Connexion activée" */}
      {/* Planning (optionnel) : Poste (position_id), Rôle (select statique "employee"/"manager"/"admin" — champ planning.role, PAS le role_id RBAC), Type de contrat */}
      ...
      <div className="flex justify-end pt-4 border-t border-border">
        <Button onClick={handleSubmit} disabled={mutation.isPending}>
          {mutation.isPending ? "Création…" : "Créer le membre"}
        </Button>
      </div>
    </div>
  );
}

// LinkForm: recherche un utilisateur existant (usersApi.linkableSearch),
// puis usersApi.merchantLink(user.user_id, { rights: { admin: false, login_enabled: true,
//   permissions: { access_reception: false, print_merchant_cash_report: false, open_cash_drawer: false,
//     manage_menu: false, manage_plannings: false, manage_users: false, manage_settings: false,
//     manage_haccp: false, view_reports: false, view_financials: false, manage_customers: false } } })
// — droits legacy tous à false par défaut ("droits minimaux"), à ajuster ensuite dans l'onglet "Droits".
```

*(Le bloc "Identité/Accès/Planning" complet — trois `<Card>` avec les champs listés en commentaire ci-dessus — fait `src/components/team/CreateMemberSheet.tsx:171-322` dans le fichier réel ; reproduit en résumé ici pour rester lisible, le détail JSX étant strictement répétitif.)*

**Constats factuels sur cet écran** :

1. **Aucun sélecteur de rôle RBAC (`role_id`) n'est présent dans ce formulaire de création.** Le seul champ nommé "Rôle" (`positionId`/`role` dans la section "Planning") est un `<Select>` **statique**, à trois valeurs codées en dur (`"employee" | "manager" | "admin"`, `CreateMemberSheet.tsx:283-286`), envoyé dans `payload.planning.role` — ce n'est **pas** le `role_id` de la table `roles` (RBAC), c'est le champ `employees.role` (l'enum `employees_role_enum` déjà documenté dans le schéma connu), une simple catégorisation RH/planning, sans effet sur les permissions.
2. Le seul contrôle de droits exposé à la création est le switch booléen **"Administrateur"** (`rights.admin`, colonne legacy `users_rights.admin`) et **"Connexion activée"** (`rights.login_enabled`) — aucun réglage granulaire par domaine (menu, planning, HACCP, etc.) n'est proposé ici.
3. **L'attribution effective du rôle RBAC (`role_id`) à la création n'est donc pas pilotée par l'utilisateur du back-office** : côté backend, `UpsertMerchantUserRights` (`internal/modules/users/admin_repository.go:303-311`) assigne automatiquement `merchant.default_role_id` à toute nouvelle ligne `users_rights` créée par `INSERT` — commentaire du code : « role_id comes from merchant.default_role_id (RBAC lot 4), never hardcoded — fails explicitly (`models.ErrMerchantDefaultRoleNotSet`) rather than inserting a new row with no role_id. » Le rôle assigné par défaut n'est donc pas choisi au moment de la création dans l'UI, mais hérité silencieusement du paramétrage de l'établissement.
4. **L'assignation/changement explicite de rôle RBAC se fait dans un écran séparé, après création** : l'onglet "Droits" de la fiche employé (`wello-back-office/src/components/team/tabs/AccessTab.tsx`), qui appelle `PUT /users/{id}/role` (`usersApi.updateRole`, `src/services/welloApi.ts:273-278`, lui-même routé côté API vers `rolesH.SetUserRole` — `cmd/api/routes.go:624`). Le commentaire d'en-tête de ce fichier confirme explicitement l'architecture cible :
   ```
   * E3 — replaces the old flat permission-toggle grid (RightsTab) entirely.
   * A single role selector, plus a read-only preview of what that role
   * grants — so an admin sees what they're assigning without opening the
   * roles screen separately.
   *
   * Deliberately its own tab, never merged with "Contrat" (which owns the
   * planning position — the "poste"): keeping RBAC role assignment and job
   * position in separate forms is the only thing preventing the two concepts
   * from blurring together again.
   ```
   C'est-à-dire : un ancien écran "grille de permissions à bascules" (`RightsTab`) a été explicitement remplacé par un unique sélecteur de rôle avec aperçu en lecture seule des permissions qu'il accorde — mais cette UI n'est **jamais présentée pendant le flux de création**, uniquement en édition ultérieure d'une fiche existante (`AccessTab.tsx:36-74`, chargement de `detail.role_id` via `GET /users/{id}` puis `rolesApi.list()`/`rolesApi.get()` pour l'aperçu, mutation `usersApi.updateRole(userId, roleId)` au clic).

En résumé pour 3.3 : la création d'un utilisateur dans le back-office ne permet de fixer que deux leviers de droits historiques (`admin` booléen, `login_enabled`) ; le rôle RBAC réel est assigné automatiquement (rôle par défaut de l'établissement) puis doit être changé manuellement, a posteriori, via un onglet "Droits" distinct du formulaire de création.
## 4. Fiscalité et registre de caisse

### 4.1. Ouverture / clôture d'un registre de caisse

#### 4.1.1. Tables SQL concernées

Six tables portent le registre de caisse, confirmées par introspection live sur le Postgres de staging. Aucune n'est créée par un fichier sous `migrations/done/` ou `migrations/todo/` — leur DDL n'est visible que dans le snapshot `docs/migration-postgres/04-schema-postgres-target.sql` (vérifié : `migrations/todo/` et `migrations/done/` ne contiennent aucun fichier dont le nom évoque `cash`, `receipt`, `order` ou `payment`).

**`cash_desks`** (`docs/migration-postgres/04-schema-postgres-target.sql:463`) — la caisse physique (le « poste »), pas la session.

**`cash_registers`** (`docs/migration-postgres/04-schema-postgres-target.sql:504`) — la session de caisse (ouverture/clôture), avec les 3 colonnes du chaînage fiscal (`hash varchar(64)`, `signature text`, `previous_hash varchar(64)`). Deux états successifs et distincts : `closed` (le rapport Z est calculé, mais `cash_registers_custom_items` reste modifiable) puis `enclosed` (verrouillage définitif).

**`cash_registers_items`** (`:548`) — snapshot automatique des ventes par moyen de paiement, figé à la clôture.

**`cash_registers_custom_items`** (`:531`) — ajustements manuels saisis par le restaurateur.

**`device_link`** (`:1105`) — liaison appareil secondaire → caisse principale.

**Tables présentes dans le schéma mais mortes côté Go** — aucune occurrence d'`INSERT INTO` dans tout le dépôt (grep exhaustif) : `cash_reports` (`:564`), `cash_funds` (`:478`), `sub_cash_registers` (`:3743`, référencée uniquement en lecture).

#### 4.1.2. Endpoints HTTP

Enregistrés dans `cmd/api/routes.go` :

| Méthode | Route | Handler | Ligne |
|---|---|---|---|
| POST | `/cash_register/open` | `cashRegisterH.OpenCashRegister` | `routes.go:1489` |
| GET / POST | `/cash_register/history` | `cashRegisterH.GetHistory` | `routes.go:1490-1491` |
| POST / DELETE | `/cash_register/link` | `cashRegisterH.HandleLinkDevice` / `HandleUnlinkDevice` | `routes.go:1492-1493` |
| GET | `/cash_register/{cash_register_id}/` | `cashRegisterH.GetCashRegisterHistoryByID` | `routes.go:1496` |
| GET | `/cash_register/{cash_register_id}/summary` | `cashRegisterH.GetCashRegisterSummary` | `routes.go:1497` |
| GET | `/cash_register/{cash_register_id}/tva-details` | `cashRegisterH.GetCashRegisterTVADetails` | `routes.go:1498` |
| PATCH | `/cash_register/{cash_register_id}/close` | `cashRegisterH.CloseCashRegister` | `routes.go:1499` |
| PATCH | `/cash_register/{cash_register_id}/enclose` | `cashRegisterH.EncloseCashRegister` | `routes.go:1500` |
| POST | `/cash_register/{cash_register_id}/custom_items` | `cashRegisterH.AddCustomItem` | `routes.go:1502` |
| DELETE | `/cash_register/{cash_register_id}/custom_items/{item_id}` | `cashRegisterH.DeleteCustomItem` | `routes.go:1503` |
| POST | `/cash_drawer/open` | `cashRegisterH.OpenCashDrawer` (garde RBAC `permission.POSCashDrawerOpen`) | `routes.go:1415-1425` |

**Séquence complète — ouverture** (`internal/modules/cash_registers/handler.go:26-48` → `service.go:19-30` → `repository.go:31-85`) :
1. `OpenCashRegister` (repository) vérifie qu'aucune caisse n'est déjà ouverte pour le `device_id` (`WHERE end_date IS NULL AND device_id = ? AND merchant_id = ?`, `repository.go:37-44`) ; si oui, renvoie `"device_already_opened_cash_register"` sans erreur.
2. Sinon, `INSERT INTO cash_registers (cash_desk_id, device_id, user_id, merchant_id, cash_fund, start_date, closure_comment) VALUES (..., '')` (`repository.go:61-71`) — `closure_comment` est rempli en chaîne vide car `NOT NULL` sans défaut en Postgres.
3. Retourne `cash_register_id`. **Aucun calcul de hash à l'ouverture** — le chaînage n'intervient qu'à la fermeture.

**Séquence complète — fermeture** (`handler.go:50-88` → `service.go:32-40` → `repository.go:388-527`), détaillée intégralement en §4.1.4a.

#### 4.1.3. Séquence de numérotation

Pas une seule séquence, mais **quatre chaînages distincts et indépendants**, plus une numérotation d'affichage séparée :

1. **`orders.order_num`** — numéro affiché au client/marchand, **pas une séquence fiscale continue** : elle se réinitialise à 1 dès que le dernier numéro atteint 99 — `internal/modules/order_life_cycle/repository.go:1899-1931` :
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
2. **`cash_registers.cash_register_id`** — simple PK auto-incrémentée (`GENERATED ALWAYS AS IDENTITY`), pas de logique métier de continuité.
3. **`receipts.receipt_number`** — numérotation fiscale séquentielle annuelle au format `F-YYYY-NNNNNN` (§4.1.5).
4. **`payments`** — pas de numéro de séquence visible, uniquement chaînage par hash.

#### 4.1.4. Chaînage cryptographique — quatre chaînes de hash indépendantes, plus un angle mort

Le dépôt implémente un chaînage SHA-256 + signature HMAC sur **quatre tables séparément**, chacune avec sa propre requête « dernier hash » et sa propre formule de payload. La primitive de signature est commune :

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
La clé `FISCAL_SIGNING_KEY` est lue directement via `os.Getenv` — elle **n'est référencée nulle part dans `internal/config/`** (pas de validation au démarrage, contrairement à `GOOGLE_API_KEY`/`R2_PRIVATE_BUCKET`) : si la variable est absente, `key` vaut un slice vide et `SignHash` continue de produire une signature HMAC valide avec une clé vide, sans erreur ni avertissement.

**a) Chaînage `cash_registers` (clôture de caisse)** — `internal/modules/cash_registers/repository.go:485-527`, dans `CloseCashRegister` (fonction complète : `repository.go:388-527`) :
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

	// 7. LOGIQUE FISCALE : Calcul du nouveau Hash
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
C'est **la seule des quatre chaînes à poser un marqueur de genèse explicite** (le littéral `"GENESIS_HASH"`) quand aucune caisse précédente n'existe pour le marchand. Ce que signe le hash : `cash_register_id | merchant_id | fond_de_caisse_final(%.2f) | hash_précédent`.

**b) Chaînage `orders` — trois points d'écriture, dont un sans hash**

Trois fonctions de `internal/modules/order_life_cycle/repository.go` font passer une commande à `state = 'CLOSED'`, mais **une seule sur trois n'écrit pas dans la chaîne** :

- **`SetDeliveredLocal`** (`repository.go:871-947`, bloc fiscal `:916-947`) — livraison normale, `brand_status = 'CLOSED'` :
```go
	// 1.bis : RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal pour Orders)
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

- **`DeleteOrderLocal`** (`repository.go:786-840`, bloc fiscal `:797-830`) — annulation d'une commande **déjà comptée** (livrée puis annulée a posteriori), `brand_status = 'CANCELED'`, **écrit quand même un hash de clôture** :
```go
	// 1.bis : RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal pour Orders)
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

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET deletion_reason_id = ?,
            deletion_comment = ?,
            last_update = `+dbx.UTCNow()+`,
            state = 'CLOSED',
            brand_status = 'CANCELED',
            delivered_on = `+dbx.UTCNow()+`,
			previous_hash = ?,
			hash = ?,
			signature = ?,
			cancelled_by_type = ?
        WHERE order_id = ?`,
		reasonID, comment, prevHash, newHash, signature, classifyCancelledByType(userID), orderID,
	)
```
Note factuelle : ici l'argument passé au `previous_hash` de l'`UPDATE` est `prevHash` (le `sql.NullString`, qui écrit SQL `NULL` en absence de précédent) et non `prevHash.String` (chaîne vide `""`) comme dans `SetDeliveredLocal` — divergence de code entre les deux points d'écriture de la même chaîne. Aucun marqueur de genèse explicite (type `"GENESIS_HASH"`) dans les deux cas : le premier hash de la chaîne `orders` d'un marchand est calculé à partir d'un `prevHash.String` valant `""`.

- **`DenyOrderLocal`** (`repository.go:680-715`) — refus d'une commande par le marchand **avant toute préparation/livraison** (appelée depuis `SetOrderDenied`/`DenyOrder`, `internal/modules/order_life_cycle/service.go:784-867`, uniquement si `OrderStillOpen` renvoie `true`, i.e. `state = 'OPEN'`) fait elle aussi passer la commande à `state = 'CLOSED'` (`brand_status = 'DENIED'`), **sans jamais toucher `hash`/`previous_hash`/`signature`** :
```go
func (r *OrdersLifeCycleRepository) DenyOrderLocal(ctx context.Context, orderID, deletionReasonID, comment, userID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx, `
        UPDATE orders
        SET last_update = `+dbx.UTCNow()+`,
            brand_status = 'DENIED',
            merchant_approval = 'DENIED',
            state = 'CLOSED',
            deletion_reason_id = ?,
            deletion_comment = ?,
            cancelled_by_type = ?
        WHERE order_id = ?`,
		deletionReasonID, comment, classifyCancelledByType(userID), orderID,
	)
	...
```
**Constat factuel** : trois fonctions distinctes amènent une commande à `state = 'CLOSED'` ; seules deux écrivent dans la chaîne de hash `orders`. Une commande `DENIED` atteint `state = 'CLOSED'` sans jamais recevoir de `hash`/`signature`/`previous_hash` — elle reste `NULL` sur ces trois colonnes en base.

**c) Chaînage `payments` (chaque encaissement)** — `internal/modules/order_life_cycle/repository.go:146-263` (`AddPaymentAndReturnID`), bloc fiscal `:173-205` :
```go
	// 2. RÉCUPÉRATION DU HASH PRÉCÉDENT (Chaînage Fiscal)
	var prevHash sql.NullString
	_ = db.QueryRowContext(ctx, `
		SELECT hash FROM payments 
		WHERE merchant_id = ? 
		ORDER BY payment_date DESC LIMIT 1 
		FOR UPDATE
	`, payment.MerchantID).Scan(&prevHash)

	now := time.Now().UTC()
	paymentDate := now.Format(time.RFC3339)

	// Calcul du hash du nouveau paiement
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
Schéma `payments` confirmé (`docs/migration-postgres/04-schema-postgres-target.sql:2692-2711`) : `hash varchar(64)`, `signature text`, `previous_hash varchar(64)`, `operation_type varchar(20) NOT NULL DEFAULT 'SALE'` (seules valeurs utilisées dans le code : `SALE`, `REFUND` — `internal/models/payment_models.go:4-5`).

**d) Chaînage `receipts` (reçu fiscal — détail en §4.1.5)** — hash + numérotation combinés.

#### 4.1.5. Le « reçu fiscal » (`receipts`) — chaînage + numérotation séquentielle annuelle

Table `receipts` (`docs/migration-postgres/04-schema-postgres-target.sql:3349-3372` env.) :
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

Génération complète — `internal/modules/receipt/service.go:30-73` (`GenerateFiscalReceipt`) :
```go
func (s *receiptService) GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error {
	lastNumber, lastHash, err := s.repo.GetLastReceiptData(ctx, *order.MerchantID)
	if err != nil {
		return fmt.Errorf("failed to get last receipt data: %w", err)
	}

	newNumber := s.generateNextReceiptNumber(lastNumber)
	...
	// Formule du chaînage: H_n = SHA256(H_{n-1} | ReceiptNumber | TotalTTC | Date)
	payload := fmt.Sprintf("%s|%s|%d|%s", lastHash, newNumber, order.TTC, now.Format(time.RFC3339))
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash)
	...
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

Verrouillage anti-doublon — `internal/modules/receipt/repository.go:26-51` (`GetLastReceiptData`, `FOR UPDATE`, retourne `"", ""` pour le premier reçu du marchand).

Déclenchement : `GenerateFiscalReceipt` est appelé depuis `DeliverOrder` (`internal/modules/order_life_cycle/service.go`), **immédiatement après** `SetDeliveredLocal` (chaîne (b) ci-dessus). Un reçu d'avoir (`GenerateRefundReceipt`) est généré en cas de remboursement. Aucune génération de reçu fiscal n'est déclenchée par `DeleteOrderLocal` ni `DenyOrderLocal` — cohérent, aucun ticket n'a été émis pour ces deux flux.

### 4.2. Initialisation de la séquence fiscale pour un nouveau marchand

**Réponse : initialisation paresseuse (lazy), pas d'initialisation explicite à la création du marchand.**

`InsertMerchant` (`internal/modules/pos/create_repository.go:13-36`) n'écrit ni `hash`, ni `is_active`, ni aucune séquence — seulement les colonnes d'identité (`fullName`, `SIRET`, `email`, `token`...). `InitMerchantSatellites` (`create_repository.go:58-134`) crée les entités satellites (2 QR codes, `scannorder_settings`, `merchant_parameters`, `merchant_marketing_settings`, `haccp_settings`, `bookings_settings`) et **une seule ligne `cash_desks`** (`INSERT INTO cash_desks (merchant_id, name) VALUES (?, 'Caisse principale')`, `create_repository.go:126-131`).

**Aucune ligne n'est insérée dans `cash_registers`, `orders`, `payments` ou `receipts`** à la création du marchand. Conséquence :
- premier appel `GetNextOrderNum` → `sql.ErrNoRows` → `"1"` ;
- premier `CloseCashRegister` du marchand → retombe sur le littéral `"GENESIS_HASH"` (seule chaîne à poser un marqueur explicite) ;
- premier `GetLastReceiptData` → `"", ""` → `generateNextReceiptNumber("")` produit `F-<année>-000001` ;
- première ligne `payments` → même mécanisme, `prevHash.String == ""`, sans marqueur.

### 4.3. Notion de « mise en service » / « activation » / « go live »

**N'existe pas.** Recherche exhaustive (`onboarding`, `go_live`, `activation`, `activated_at`, `live_at`, `first_ticket`) : aucune occurrence pertinente hors du module `integrations` (onboarding Stripe Connect, KYC du prestataire de paiement — sans rapport avec la conformité fiscale caisse).

Le seul flag qui s'en approche est `merchant.is_active` (`docs/migration-postgres/04-schema-postgres-target.sql:2208`, `boolean NOT NULL DEFAULT true`) :
- `InsertMerchant` ne le renseigne jamais explicitement → la valeur par défaut SQL `true` s'applique dès la création.
- Modifiable via `UpdateMerchant` (`internal/modules/pos/repository.go:1078`, `updates = append(updates, "is_active = ?")`).
- Simple bouton marche/arrêt générique, sans lien avec la première caisse ouverte, le premier ticket émis, ni aucune notion fiscale.

Aucune colonne `activated_at`, `go_live_at`, `production_since`, aucun statut `"DRAFT"`/`"LIVE"`/`"ONBOARDING"` sur `merchant`, `cash_desks`, `cash_registers` ou tables associées.

### 4.4. Mode formation / mode école / mode test

**N'existe pas, sous aucune forme.** Reconfirmé par recherche exhaustive (`training`, `school`, `test_mode`, `demo`, `sandbox`) sur tout `internal/` :
- `payments.operation_type` n'a que deux constantes — `internal/models/payment_models.go:4-5` : `OperationTypeSale = "SALE"`, `OperationTypeRefund = "REFUND"`. Pas de `TRAINING`/`TEST`/`DEMO`.
- Aucune colonne `is_test`, `is_training`, `training_mode`, `sandbox`, `demo` sur `orders`, `payments`, `cash_registers`, ou `merchant`.
- `orders.brand_status` est une colonne texte libre, sans énumération Go dédiée. Toutes les valeurs effectivement écrites par le code ont été recensées (`CLOSED`, `CANCELED`, `DELETED`, `DENIED`, `PENDING`, `PENDING_APPROVAL`, `ACCEPTED`, `CONFIRMED`, `SCHEDULED`, `EN_ROUTE_TO_DROPOFF`, `READY_FOR_HANDOFF`/`READY_FOR_TAKE_AWAY`, `FAILED`, `DELIVERING`, `PENDING_CARD_PAYMENT`) — jamais de valeur liée à un mode formation/test.
- Les occurrences de « sandbox » trouvées concernent exclusivement les environnements de test des API **externes** Deliveroo/Uber Eats (`internal/modules/deliveroo/client.go`, `client_old.go`, `handler.go:115`), sans rapport avec la caisse ou les tickets de vente.

Conséquence : la question « comment ces tickets sont-ils exclus des totaux » est sans objet.

### 4.5. Attestation de conformité (NF525 ou équivalent)

**N'existe pas.** Aucun endpoint, aucune génération de PDF, aucun texte statique ne produit un document de type « certificat d'inaltérabilité » ou « attestation de conformité logicielle ». Le sigle « NF525 » n'apparaît que dans des **commentaires de code Go**, jamais dans une sortie produite :
```
internal/modules/delivery_sessions/service.go:177   — // Conformite NF525 : la fermeture de chaque commande passe par
internal/modules/delivery_sessions/service.go:305   — // (payment check, NF525 hash, state='CLOSED', possible session auto-close to 'done',
internal/modules/delivery_sessions/postgres_integration_test.go:266 — // ouvert via order_life_cycle.SetDelivered — hash NF525, signature, audit —
internal/modules/order_life_cycle/service.go:1192   — // SendInvoiceByEmail génère la facture PDF de la commande à partir du Receipt déjà figé (NF525, ...)
internal/modules/order_life_cycle/invoice_pdf.go:14  — // buildInvoicePDF génère le PDF de facture à partir du Receipt déjà figé (NF525) — aucun recalcul de montant.
```
Ces commentaires désignent le mécanisme de chaînage de hash (§4.1) comme référence de conformité interne, jamais un document remis au marchand/à l'administration.

Le seul PDF généré et lié à une vente est une **facture client** (`buildInvoicePDF`/`SendInvoiceByEmail`) et un **PDF de rapport Z de caisse** (`ExportRegisterPDF`, `internal/modules/pos/accounting/handler.go:96`, `service.go:391`, exposé en `POST /accounting/registers/{register_id}/export-pdf` — `cmd/api/routes.go:838`) — un rapport de clôture, pas une attestation de conformité logicielle.

### 4.6. Exclusion des tickets des exports comptables

Les filtres d'exclusion sont répétés quasi identiquement à travers plusieurs modules (`cash_registers`, `pos/reports`, `pos/accounting`, `stats`, `analytics/upsell`) — aucun n'a de filtre « formation/test » (cohérent avec §4.4) ; les exclusions portent uniquement sur l'état métier et le canal de la commande.

**Module `pos/reports`** (`internal/modules/pos/reports/repository.go:70-97`, `GetTVAReportData`, et `:209-235`, `GetPaymentsReportData`) :
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

**Module `pos/accounting`** (`internal/modules/pos/accounting/repository.go:183-233`, `GetTVAData`) — mêmes cinq exclusions. Même filtre pour `GetPaymentsData` (`:300`, `brand_status NOT IN` à `:320`), `GetTrustedEnclosedRegisterIDs` (`:377`, `:461`) et `GetVATAggregationRows` (`:637`, `:711` et `:737`).

**Module `stats`** (`internal/modules/stats/repository.go:261,369,493`) et **`analytics/upsell`** (`internal/modules/analytics/upsell.go:75,137`) : identique, `brand_status NOT IN ('DELETED', 'CANCELED')`.

**Ce que ces filtres excluent réellement** :
- `o.state <> 'CLOSED'` → commande non finalisée.
- `o.brand_status IN ('DELETED', 'CANCELED')` → commande supprimée ou annulée.
- `o.created_by IN ('-1', 'SCANNORDER')` → canal ScanNOrder ou identifiant système, comptabilisé ailleurs.
- `o.brand <> 'WELLO_RESTO'` → commandes Uber Eats/Deliveroo, hors périmètre de ce rapport précis.
- `tva.show_in_report` → catégories de TVA marquées hors reporting.

**Constat — statut `DENIED` absent de la liste d'exclusion fiscale/comptable.** Une commande refusée par le marchand (`brand_status = 'DENIED'`, via `DenyOrderLocal`, §4.1.4b) atteint `state = 'CLOSED'` (`internal/modules/order_life_cycle/repository.go:689`). Or **aucun** des filtres ci-dessus n'exclut `'DENIED'` — la liste d'exclusion s'arrête systématiquement à `('DELETED', 'CANCELED')`, dans `cash_registers/repository.go:129,156,177`, `pos/reports/repository.go:77,94,231`, `pos/accounting/repository.go:216,231,320,461,711,737`, `stats/repository.go:261,369,493` et `analytics/upsell.go:75,137`.

À titre de comparaison, un autre module de ce même dépôt **connaît et exclut explicitement** ce statut ailleurs dans le code : `internal/modules/integrations/repository.go:14` définit `const kpiExcludedStatuses = "('CANCELED','DENIED','ONLINE_PAYMENT_PENDING')"`, utilisée pour les KPI/tableaux de bord (`:60,68,216,224,365,374`). La liste utilisée par les rapports fiscaux/comptables (`('DELETED','CANCELED')`) est donc **plus étroite** que celle utilisée pour les KPI internes, alors que la commande `DENIED` n'a par ailleurs jamais reçu de hash de clôture (§4.1.4b) et n'a — par construction du flux (`OrderStillOpen` doit être vrai, donc `state='OPEN'`, avant l'appel à `DenyOrder`) — jamais été payée. Fait constaté sans jugement sur son impact chiffré réel (dépendant de la fréquence des refus marchands et du contenu `orderitems`/`price` associé à ces commandes).

Un mécanisme distinct, dédié au rapport de « réel » de caisse (`GetTrustedEnclosedRegisterIDs`/`GetRealPaymentsData`, `internal/modules/pos/accounting/repository.go:377-608`), exclut des **registres de caisse entiers** dont l'instantané figé à la clôture diverge d'un recalcul live des paiements, et exclut les codes MOP `STRIPE`/`UBER_EATS`/`DELIVEROO`.

### Synthèse des faits marquants — Section 4

1. **Quatre chaînages de hash indépendants** (`cash_registers`, `orders`, `payments`, `receipts`), chacun avec sa propre requête « dernier hash » et sa propre formule de payload — pas un système unifié.
2. **Un troisième point de clôture de commande (`DenyOrderLocal`) échappe totalement à la chaîne `orders`** : une commande refusée par le marchand atteint `state = 'CLOSED'` sans jamais recevoir `hash`/`previous_hash`/`signature`.
3. **Deux numérotations différentes coexistent** : `orders.order_num` (affichage client, boucle 1→99, pas continue) et `receipts.receipt_number` (`F-YYYY-NNNNNN`, séquentielle, remise à `000001` chaque année civile).
4. **Aucune séquence fiscale n'est initialisée à la création du marchand** — tout est amorcé au premier événement réel (lazy init), avec un marqueur de genèse explicite (`"GENESIS_HASH"`) uniquement pour la chaîne `cash_registers`.
5. **Aucun jalon « mise en service »/« go live »** n'existe ; `merchant.is_active` est un simple flag actif/inactif à `true` par défaut dès la création.
6. **Aucun mode formation/test/école** n'existe dans le modèle de données ou le code.
7. **Aucune attestation de conformité NF525** n'est générée ; « NF525 » n'apparaît que comme référence en commentaire de code.
8. **La liste d'exclusion des rapports fiscaux/comptables (`'DELETED','CANCELED'`) omet le statut `'DENIED'`**, alors qu'un autre module du même dépôt (`internal/modules/integrations`, KPI) exclut explicitement ce même statut sous le nom `kpiExcludedStatuses`.
9. **`FISCAL_SIGNING_KEY` n'est validée nulle part au démarrage** (`internal/config/`) — son absence ne provoque ni erreur ni avertissement, seulement une signature HMAC calculée avec une clé vide.
10. Les tables fiscales (`cash_registers`, `payments`, `orders`.hash/signature/previous_hash, `receipts`) n'ont **aucune trace dans `migrations/`** — leur DDL n'est visible que dans le dump `docs/migration-postgres/04-schema-postgres-target.sql`.
## 5. Abonnement et facturation

### 5.1. Gestion de l'abonnement du marchand à la plateforme (par opposition aux paiements clients finaux)

**Réponse courte, confirmée par introspection live de la base Postgres staging (2026-09-08) : le mécanisme existe au niveau schéma, mais dans le code Go actuel il ne sert que de système de feature-flags (droits d'accès aux modules) — aucune facturation Stripe réelle n'est déclenchée par ce chemin.**

**Structure exacte live** :

```
subscriptions:
  id, stripe_subscription_id varchar(150) NOT NULL, merchant_id varchar(64) NOT NULL, package_id integer NOT NULL,
  planning_enabled boolean NOT NULL DEFAULT false, haccp_enabled boolean NOT NULL DEFAULT false,
  stock_enabled boolean NOT NULL DEFAULT false, scannorder_enabled boolean NOT NULL DEFAULT true,
  bookings_enabled boolean NOT NULL DEFAULT false, kiosks_enabled boolean NOT NULL DEFAULT false,
  max_kiosks integer NOT NULL DEFAULT 0, delivery_enabled boolean NOT NULL DEFAULT true
  PK composite (id, merchant_id, package_id)

packages:
  id, package_name varchar(50) NOT NULL, stripe_price_id varchar(200) NOT NULL, trial_period_days integer DEFAULT 0,
  allow_waiter_account boolean DEFAULT false, allow_delivery_account boolean DEFAULT false,
  scannorder_ready boolean DEFAULT true, stock_management integer DEFAULT 0, hr_management boolean DEFAULT false,
  planning_enabled/haccp_enabled/stock_enabled/scannorder_enabled(default true)/bookings_enabled/kiosks_enabled/delivery_enabled(default true) boolean
```

**Aucune contrainte `FOREIGN KEY` n'existe entre `subscriptions.package_id` et `packages.id`** — vérifié exhaustivement via `information_schema.table_constraints`/`key_column_usage`/`constraint_column_usage` sur les deux tables : requête vide, aucune ligne retournée. Le lien `package_id -> packages.id` est donc **purement applicatif**, jamais garanti par le SGBD.

**Preuve concrète de cette absence de contrôle, observée en donnée live sur staging** : `packages` contient les lignes `id ∈ {0, 1, 3, 4, 5, 6, 100, 101, 102, 103}` (10 plans : Developpers, Essentiel, Standard, Premium, Association, Deis, Premium, Standard Delivery, Premium Waiter, Pointage — certains avec `stripe_price_id` vide). `subscriptions` (30 lignes au total) contient une ligne avec `package_id = -4`, qui **ne correspond à aucune ligne de `packages`** — une souscription orpheline, sans plan valide, silencieusement tolérée par le schéma comme par le code.

Répartition live des `package_id` dans `subscriptions` (staging) :
```
package_id=-4  count=1   (orphelin — aucune ligne packages correspondante)
package_id=0   count=2
package_id=1   count=1
package_id=3   count=6
package_id=4   count=9
package_id=5   count=1
package_id=100 count=2
package_id=101 count=5
package_id=102 count=2
package_id=103 count=1
```

**Deux colonnes de `packages` sont mortes côté API Go** : `allow_waiter_account`/`allow_delivery_account`. Recherche exhaustive (`allow_waiter_account|allow_delivery_account|AllowWaiterAccount|AllowDeliveryAccount`) : aucune occurrence dans `internal/` — seule trace, `docs/decisions.md:2248`, qui documente leur **retrait** du dépôt `wello-back-office` (`src/types/auth.ts`) comme fallback mort, en miroir de la dépréciation de 5 colonnes RBAC legacy sur `users_rights`. Ces deux colonnes existent donc en base sans plus aucun lecteur ni écrivain applicatif connu.

**Ce que fait le code Go à la création d'un marchand** — endpoint `POST /pos/create` :
- Route : `cmd/api/routes.go:756-760`, dans `r.Route("/pos", ...)` avec `r.Use(authMiddleware)` (ligne 758) — nécessite donc un token valide, mais **aucun `RequirePermission` spécifique** n'encadre `r.Post("/create", posH.CreateMerchant)` (ligne 760), contrairement à la ligne suivante `r.With(middleware.RequirePermission(permission.StaffManage)).Post("/link-user", ...)`.
- Handler → Service `internal/modules/pos/create_service.go:13-42` (`CreateMerchant`) :
```go
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
	if strings.TrimSpace(req.FullName) == "" ||
		strings.TrimSpace(req.SIRET) == "" ||
		strings.TrimSpace(req.Tel) == "" ||
		strings.TrimSpace(req.PackageID) == "" {
		return CreateMerchantResponse{}, models.ErrInvalidInput
	}
	...
	// Step 2 — create subscription from the requested package
	if err := s.posRepo.InsertSubscription(txCtx, merchantID, strings.TrimSpace(req.PackageID)); err != nil {
		return err
	}
	...
```
La seule validation sur `req.PackageID` est **la non-vacuité de la chaîne** — aucune vérification que cette valeur existe réellement comme ligne dans `packages` (ce qui explique et confirme la ligne orpheline `package_id=-4` observée en base).

- Repository `internal/modules/pos/create_repository.go:38-55` (intégral) :
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
`stripe_subscription_id` est explicitement forcé à `''` — **aucun appel à l'API Stripe Billing/Subscriptions n'a lieu à cette étape**. Confirmé par une recherche exhaustive du package `github.com/stripe/stripe-go/*/sub` (ou tout usage de `subscription.New`) : aucune occurrence dans tout le dépôt.

**Aucune fonction de mise à jour n'existe** pour changer ensuite le `package_id`/l'abonnement d'un marchand existant : recherche exhaustive de `UpdateSubscription`, `ChangePackage`, `UpgradePlan`, `UPDATE subscriptions SET package_id` — aucune occurrence. Le choix du plan est donc figé définitivement à la création du marchand par ce chemin de code.

**Lecture des `*_enabled`/`max_kiosks` — c'est bien un mécanisme de feature-gating réel, avec une logique de priorité `subscription > package > défaut codé en dur`.** La requête partagée par `GetUserByToken`/`GetUserByPIN`/`Login` (répétée à l'identique trois fois dans `internal/modules/auth/repository.go:113-119`, `:319-320` et `:727-728`, ainsi que dans `internal/modules/users/repository.go:245-246`) :
```sql
LEFT JOIN subscriptions s ON s.merchant_id = %[1]s
LEFT JOIN packages p ON p.id = s.package_id
...
COALESCE(p.scannorder_ready, FALSE),
COALESCE(p.stock_management, 0),
COALESCE(p.hr_management, FALSE),
COALESCE(s.planning_enabled, p.planning_enabled, p.hr_management, FALSE) AS planning_enabled,
COALESCE(s.haccp_enabled, p.haccp_enabled, TRUE) AS haccp_enabled,
COALESCE(s.stock_enabled, p.stock_enabled, CASE WHEN p.stock_management > 0 THEN TRUE ELSE FALSE END) AS stock_enabled,
COALESCE(s.scannorder_enabled, p.scannorder_enabled, p.scannorder_ready, FALSE) AS scannorder_enabled,
COALESCE(s.bookings_enabled, p.bookings_enabled, TRUE) AS bookings_enabled,
COALESCE(s.kiosks_enabled, p.kiosks_enabled, TRUE) AS kiosks_enabled,
COALESCE(s.delivery_enabled, p.delivery_enabled, TRUE) AS delivery_enabled,
```
(`internal/modules/auth/repository.go:110-119`, requête `GetUserByToken`.) Autrement dit : la valeur de `subscriptions` (override par établissement) prime sur celle de `packages` (défaut du plan), elle-même primant sur une valeur littérale codée en dur en dernier recours.

**Ces flags sont ensuite réellement utilisés pour restreindre l'accès à des fonctionnalités, à deux niveaux :**

1. **Restitution au front (gating côté client)** — `internal/modules/auth/service.go:447-461` (`buildLoginResponse`) :
```go
Modules: LoginCapabilityModulesResponse{
    Menu:       user.HasMenuAccess(),
    Planning:   user.HasPlanningAccess() && user.PlanningEnabled,
    Users:      user.HasUserManagementAccess(),
    Settings:   user.HasSettingsAccess(),
    HACCP:      user.HasHACCPAccess() && user.HACCPEnabled,
    Bookings:   user.BookingsEnabled,
    Kiosks:     user.KiosksEnabled,
    Delivery:   user.DeliveryEnabled,
    Reports:    user.HasReportsViewAccess(),
    Financials: user.HasFinancialsViewAccess(),
    Customers:  user.HasCustomerManagementAccess(),
    Stock:      user.StockEnabled,
    HR:         user.HrManagement,
    ScanNOrder: user.ScanNOrderEnabled,
},
```
`Planning`/`HACCP` combinent un droit RBAC (`Has*Access()`) **ET** le flag d'abonnement (`&&`) ; `Bookings`/`Kiosks`/`Delivery`/`Stock`/`ScanNOrder` ne dépendent que du flag d'abonnement seul, sans permission RBAC associée.

2. **Enforcement côté serveur réel (pas seulement cosmétique)**, à deux endroits identifiés :
   - `internal/modules/kiosk/repository.go:365-379` (`GetMerchantMaxKiosks`, `SELECT max_kiosks FROM subscriptions WHERE merchant_id = ?`) est appelé à trois reprises dans `internal/modules/kiosk/service.go` — `GenerateEnrollmentCode` (ligne 583), l'enrôlement d'une borne (ligne 108), et la réactivation d'une borne existante (ligne 757) — à chaque fois combiné à `GetActiveKioskCount` pour **bloquer réellement** la création/activation d'une borne au-delà du quota souscrit. Ce n'est donc pas un simple affichage : c'est une vraie limite serveur.
   - `internal/modules/users/service.go:86` : `if ctxUser, ctxErr := middleware.UserFromContext(ctx); ctxErr == nil && !ctxUser.DeliveryEnabled { return nil }` — sans souscription au module Livraison, le suivi de position du livreur (position courante, historique, geofence d'arrivée, relais Uber BYOC) est silencieusement no-opé côté serveur.
   - `internal/modules/delivery_sessions/service.go:159-163` : sans `DeliveryEnabled`, le SMS de suivi client lors d'une session de livraison n'est pas envoyé (la notification WebSocket interne au POS reste active, elle).

   Les autres flags (`PlanningEnabled`, `HACCPEnabled`, `StockEnabled`, `BookingsEnabled`, `ScanNOrderEnabled`) ne sont, en l'état de la recherche exhaustive menée, **consommés que dans la construction de la réponse de login** (`buildLoginResponse`) — aucun autre point du code (handler/middleware) ne les relit pour bloquer un endpoint métier correspondant (ex. rien n'empêche côté serveur d'appeler l'API HACCP même si `HACCPEnabled` vaut `false` — seule l'UI front est censée masquer l'onglet).

**Note additionnelle sur un homonyme** : le module `scannorder` porte aussi un champ nommé `DeliveryEnabled` (`internal/modules/scannorder/models.go:78,189`, `internal/modules/scannorder/repository.go:86,945`), mais c'est un réglage de zone de livraison ScanNOrder **par établissement** (`scannorder_settings`/paramètres de commande en ligne), sans rapport avec le flag d'abonnement `subscriptions.delivery_enabled` — même pattern d'homonymie déjà relevé en §1.3 (`orders.brand`/`merchant.brand_id`).

**`subscription_invoices` : code d'écriture présent (`internal/webhook/stripe/repository.go:350-379`, intégral), mais jamais réellement alimenté.** Confirmé en donnée live : `SELECT count(*) FROM subscription_invoices` sur staging retourne **0 ligne**.
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
Le `INSERT ... SELECT ... FROM welloresto_stripe_customers WHERE stripe_customer_id = ?` ne produit une ligne que si l'événement Stripe `invoice.created` porte un `stripe_customer_id` déjà présent dans `welloresto_stripe_customers`. Or **aucun code Go du dépôt (hors tests d'intégration) n'insère jamais de ligne dans `welloresto_stripe_customers`** — recherche exhaustive confirmée : les seules occurrences hors le `SELECT` ci-dessus sont un `DELETE`/`INSERT` de nettoyage dans `internal/webhook/stripe/postgres_integration_test.go:40,72`. Pourtant, **la table contient 5 lignes en donnée live sur staging** (`SELECT count(*) FROM welloresto_stripe_customers` = 5) : ces lignes ont donc été insérées par un autre moyen que le code Go actuel — très probablement manuellement en base ou par un système antérieur (PHP historique), hors périmètre de ce dépôt, cohérent avec le même constat déjà documenté en §1.3 pour `merchant.brand_id`/`stripe_accounts.terminal_location_id` (des colonnes vivantes en lecture mais jamais écrites par l'API Go).

**Les tâches cron utilisent `subscriptions` uniquement comme filtre d'existence, pas pour lire les flags** :
- `internal/tasks/orders.go:28-32` : `SELECT m.id FROM merchant m INNER JOIN merchant_parameters mp ... INNER JOIN subscriptions s ON ... INNER JOIN packages p ON p.id = s.package_id WHERE mp.auto_complete_orders AND ...` — un marchand sans ligne `subscriptions` est exclu du traitement, mais aucune colonne `*_enabled` n'est lue dans cette requête.
- `internal/tasks/products.go:28-29` (`UpdatePopularProducts`) et `internal/tasks/upsell.go:39-40` (`RecomputeUpsellPatterns`) : même pattern, `INNER JOIN subscriptions s ON s.merchant_id = ...` comme simple filtre de présence.

**Conclusion factuelle 5.1** : le système d'abonnement plateforme (`subscriptions`/`packages`/`subscription_invoices`/`welloresto_stripe_customers`) existe intégralement au niveau du schéma SQL (y compris une colonne `delivery_enabled` — migration `migrations/done/089_delivery_module_flag.up.sql`, ajoutée aux deux tables `packages` et `subscriptions` avec défaut `true`). Dans le code Go, ce système fonctionne **exclusivement comme un mécanisme de feature-flags** — avec deux garde-fous serveur réels confirmés (`max_kiosks`, `delivery_enabled` côté position/SMS livreur) et le reste purement déclaratif dans la réponse de login. Il n'y a **aucun point d'entrée qui crée, modifie ou facture réellement un abonnement Stripe** côté plateforme : `package_id` n'est jamais validé contre `packages` (aucune FK, aucune vérification applicative — la ligne orpheline `package_id=-4` observée en base le démontre), n'est jamais modifiable après la création du marchand, et `stripe_subscription_id` reste vide dans ce flux. Le reste du code Stripe du dépôt (Checkout Sessions, PaymentIntents, Connect, Terminal) concerne exclusivement l'encaissement des commandes des clients finaux du restaurant.

### 5.2. Séparation compte Stripe PLATEFORME vs comptes Stripe CONNECTÉS

**Une seule clé API Stripe pour toute la plateforme — pas de clé "plateforme" distincte d'une clé "Connect".** `internal/config/stripe.go` (intégral) :
```go
package config

import (
	"os"
)

type StripeConfig struct {
	APIKey string
	// OnboardingReturnURL is the front-end URL Stripe redirects to after onboarding completes.
	OnboardingReturnURL string
	// OnboardingRefreshURL is the front-end URL Stripe redirects to when the onboarding link expires.
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
Une seule variable d'environnement de clé (`STRIPE_API_KEY`), instanciée en un unique `StripeManager` — `cmd/api/routes.go:271` : `stripeManager := stripeInternalClient.NewStripeManager(cfg.Stripe.APIKey)` — puis injecté et réutilisé tel quel dans `ordersLifeCycleService` (ligne 325), `scannService` (ligne 352), `integrationsService` (ligne 358), `terminalService` (ligne 278-282), et `stripeWebhookService` reçoit séparément `cfg.Stripe.APIKey` en clair (ligne 371) pour son propre `stripe.Key = stripeKey` global (`internal/webhook/stripe/service.go:40`).

**La distinction plateforme/Connect ne se fait donc PAS via des identifiants différents, mais uniquement via l'appel — ou non — de `params.SetStripeAccount(accountID)` sur chaque appel API individuel.** C'est le paramètre `Stripe-Account` (en-tête HTTP sous le capot du SDK) qui bascule un appel du contexte "compte plateforme WelloResto" vers le contexte "compte connecté du marchand".

**Appels scopés sur le compte connecté du marchand** (paiements clients finaux, `internal/infrastructure/stripe/`) :
- `CreateCheckoutSession` — `internal/infrastructure/stripe/checkout.go:128-147` :
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
(`fees` = commission WelloResto prélevée via `ApplicationFeeAmount`, un modèle de "charge directe" sur le compte connecté.)
- `CaptureExistingPaymentAsync` (`internal/infrastructure/stripe/service.go:14-49`) et `RefundOrCancelAsync` (`:53-115`) : chaque appel (`PaymentIntents.Capture`, `PaymentIntents.Get`, `PaymentIntents.Cancel`, `Refunds.New`) reçoit systématiquement `params.SetStripeAccount(req.AccountID)`.
- `GetConnectBalance` (`internal/infrastructure/stripe/connect.go:137-160`) : `params.SetStripeAccount(accountID)` avant `s.client.Balance.Get(params)`.
- Le module Terminal (`internal/infrastructure/stripe/terminal.go`) scope aussi systématiquement ses appels (`:93`, `:134`, `:208` — `params.SetStripeAccount(accountID)`), pour la création de `ConnectionToken`, de `PaymentIntent` carte présente, et l'annulation associée.

**Appels NON scopés (contexte compte plateforme)** :
- `ProcessPaymentAsync` (`internal/infrastructure/stripe/service.go:117-161`) et `RefundAsync` (`:163-192`) : aucun `SetStripeAccount` — ces deux fonctions créent un `PaymentIntent`/`Refund` directement sur le compte Stripe plateforme. **Fait notable : recherche exhaustive de leurs appelants (`grep ProcessPaymentAsync|RefundAsync` hors définition/interface) — aucun appelant trouvé dans tout `internal/`.** Ces deux fonctions sont déclarées dans l'interface (`internal/infrastructure/stripe/interface.go:19-22`) et implémentées, mais actuellement **jamais invoquées** — du code mort qui, s'il était un jour rebranché sans y ajouter un `SetStripeAccount`, débiterait/rembourserait le compte Stripe de la plateforme elle-même plutôt que celui d'un marchand.
- Côté webhook, `HandleInvoiceCreated`/`HandleInvoicePaid` (`internal/webhook/stripe/service.go:630-651`) : aucun appel à l'API Stripe (uniquement des écritures SQL), cohérent avec des objets `Invoice` de facturation plateforme.

**Onboarding Stripe Connect** — utilise les mêmes URLs de config (`STRIPE_ONBOARDING_RETURN_URL`/`STRIPE_ONBOARDING_REFRESH_URL`) : `CreateOnboardingLink` (`internal/infrastructure/stripe/connect.go:44-57`), `CreateExpressAccount` (`:59-79`), `CreateBankAccountLink` (`:115-133`, pour configurer l'IBAN de réception des virements Connect), tous invoqués depuis `internal/modules/integrations/service.go:391-474` (`GetStripeStatus`, `CreateStripeOnboardingLink`, `CreateScanNOrderOnboarding`, `GetStripeBankAccounts`, `CreateStripeBankAccountLink`, `GetStripeBalance`).

**Constat additionnel** : `go.mod:21-22` déclare toujours deux versions majeures différentes du SDK Stripe en parallèle :
```
github.com/stripe/stripe-go/v78 v78.12.0   -- utilisée uniquement par internal/webhook/stripe/service.go
github.com/stripe/stripe-go/v84 v84.2.0    -- utilisée par internal/infrastructure/stripe/*.go
```

### 5.3. Webhooks Stripe traités et état de la vérification de signature

**Aiguillage des événements** — `internal/webhook/stripe/service.go:54-94`, `ProcessEvent` (intégral) :
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
		// En PHP c'était retrieveFees. On gère les frais ici.
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

**Liste exhaustive des 11 types d'événements traités :**

| Event Stripe | Handler (`internal/webhook/stripe/service.go`) | Effet |
|---|---|---|
| `checkout.session.completed` | `HandleCheckoutSessionCompleted` (:97-246) | Insertion paiement en transaction, mise à jour statut commande, invalidation cache Redis, notification WebSocket, email/SMS de confirmation client, auto-accept éventuel |
| `checkout.session.expired` | `HandleCheckoutSessionCanceled` (:249-281) | `SetOrderDenied` (motif "Session de paiement expirée ou annulée") |
| `charge.refunded` | `HandleRefund` (:533-591) | `DisablePayment`, email de remboursement au client |
| `charge.captured` | `HandleRetrieveFees` (:284-330) | Appel API Stripe `balancetransaction.Get` (scopé `SetStripeAccount(connectedAccountID)`) pour calculer `wello_resto_total_fees`/`stripe_total_fees` |
| `payment_intent.canceled` | `HandlePaymentIntentUpdated` (:340-348) | `UPDATE stripe_payments SET payment_intent_status = 'CANCELED'` |
| `payment_intent.succeeded` | `HandlePaymentIntentSucceeded` (:369-382) | Si `metadata.channel == "kiosk"` : confirmation de commande Terminal Kiosk (`ConfirmKioskCardPayment`) ; sinon `UPDATE ... 'CAPTURED'` (flux Checkout en ligne) |
| `payment_intent.payment_failed` | `HandlePaymentIntentFailed` (:393-412) | Uniquement paiements Terminal Kiosk : marque `stripe_payments` `'FAILED'`, notification WebSocket |
| `payout.paid` | `HandlePayoutPaid` (:594-627) | Résout le marchand via `GetMerchantByStripeAccountID`, email "virement effectué" |
| `invoice.created` | `HandleInvoiceCreated` (:630-642) | `INSERT` conditionnel dans `subscription_invoices` (voir §5.1 — n'aboutit jamais en pratique sur staging) |
| `invoice.paid` | `HandleInvoicePaid` (:644-651) | `subscription_invoices.status = '1'` |
| `account.updated` | `HandleAccountUpdated` (:655-698) | `stripe_accounts.verification_status`, active `scannorder_settings.activated` si `DetailsSubmitted && ChargesEnabled` |

Tout autre type d'événement (`default:`) est **silencieusement ignoré**, sans log ni erreur.

**État exact de la vérification de signature : absente à l'exécution.**

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
Le body est lu, désérialisé en JSON, et directement transmis à `ProcessEvent` — **aucun appel à une fonction de vérification de signature nulle part dans cette chaîne.**

Une méthode `VerifySignature` existe bien sur le service, mais c'est un stub vide, jamais appelé — `internal/webhook/stripe/service.go:700-702` (intégral) :
```go
func (s *StripeWebhookService) VerifySignature(ctx context.Context, header http.Header, body []byte) {
	// A implémenter avec webhook.ConstructEvent de la lib stripe-go
}
```
Recherche exhaustive confirmée : `s.service.VerifySignature` ou `stripeWebhookService.VerifySignature` **n'apparaît nulle part** dans `internal/webhook/stripe/http_handler.go`, ni dans `cmd/api/routes.go`, ni ailleurs — c'est une méthode orpheline, jamais référencée hors de sa propre définition.

**La route est enregistrée sans aucun middleware d'authentification/vérification** — `cmd/api/routes.go:571-579` (bloc `/webhooks` intégral) :
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
Contrairement à `/external` (`cmd/api/routes.go:582-583` : `r.Route("/external", func(r chi.Router) { r.Use(authMiddleware) ...`), aucun `r.Use(...)` n'encadre le bloc `/webhooks` entier ni la route `/stripe` en particulier.

**Confirmations complémentaires, exhaustives** :
- Aucune variable d'environnement `STRIPE_WEBHOOK_SECRET` (recherche `grep -rn "STRIPE_WEBHOOK_SECRET"` sur tout le dépôt Go : 0 occurrence).
- Aucun appel à `webhook.ConstructEvent` (la fonction standard `stripe-go` de vérification HMAC de signature) nulle part dans le dépôt — la seule occurrence de la chaîne "ConstructEvent" est le commentaire cité ci-dessus, jamais du code exécuté.
- Aucune lecture de l'en-tête `Stripe-Signature` nulle part.

**Comparaison avec le webhook Uber Eats du même dépôt — nuance importante.** Le webhook Uber Eats **calcule** bien une vérification de signature, mais ne **bloque** pas non plus la requête en cas d'échec — c'est un contrôle purement journalisé, pas un rejet. `internal/webhook/ubereats/handler/http_handler.go:21-43` (intégral) :
```go
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	h.service.VerifySignature(r.Context(), r.Header, body)

	var event models.UberWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if err := h.service.ProcessEvent(r.Context(), event); err != nil {
		log.Println("[UBER EATS] processing error:", err)
		http.Error(w, "processing error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
```
Le retour de `VerifySignature` (fonction sans valeur de retour, `internal/webhook/ubereats/service/service.go:128-136`) n'est de toute façon pas exploitable pour interrompre le traitement :
```go
func (s *Service) VerifySignature(ctx context.Context, headers http.Header, body []byte) {
	sig := headers.Get("X-Uber-Signature")
	ok := ueClient.VerifySignature(body, sig, s.signatureSecret)
	log := logger.FromContext(ctx)

	if !ok {
		log.Error("[UBER EATS] Invalid signature")
	}
}
```
`ProcessEvent` s'exécute donc juste après, **quel que soit le résultat de la vérification** — Uber Eats calcule et journalise un mismatch de signature, mais ne rejette pas la requête pour autant. Le webhook Stripe, lui, ne calcule même pas cette vérification (le stub n'est pas appelé) : c'est un manque plus complet que celui d'Uber Eats, mais aucun des deux webhooks du dépôt ne rejette effectivement une requête sur signature invalide à ce jour.

**Conséquence factuelle** : n'importe quel tiers connaissant l'URL `POST /webhooks/stripe` peut soumettre un JSON arbitraire imitant la structure `StripeEvent` et déclencher l'un des 11 effets métier ci-dessus (y compris `checkout.session.completed`, qui insère un paiement et modifie le statut d'une commande, ou `account.updated`, qui modifie `stripe_accounts.verification_status`) — aucune vérification cryptographique de provenance Stripe n'est effectuée dans l'état actuel du code.

### 5.4. Mandat SEPA / prélèvement bancaire européen

**N'existe pas.**

Recherche exhaustive insensible à la casse sur l'ensemble du dépôt (`sepa|iban|mandate`) : aucune occurrence de « SEPA ». Le mot « mandate » n'apparaît que dans un commentaire sans rapport (`internal/modules/planning/settings/models.go:20` : « French law does not mandate a standard Sunday pay premium... » — verbe anglais générique, aucun rapport avec un mandat de prélèvement).

Les deux seules occurrences d'« IBAN » sont des commentaires de documentation d'une fonctionnalité Stripe Connect, sans rapport avec un prélèvement SEPA effectué par la plateforme — il s'agit de la configuration du **compte bancaire de réception** du marchand pour ses virements Stripe Connect (payouts), pas d'un mandat de débit :
```go
// internal/infrastructure/stripe/connect.go:115-116
// CreateBankAccountLink generates an AccountLink (type: account_update) to allow the merchant
// to configure their IBAN/bank account on the Stripe Connect dashboard.
func (s *StripeManager) CreateBankAccountLink(accountID, returnURL, refreshURL string) (string, error) {
```
```go
// internal/modules/integrations/service.go:467
// CreateStripeBankAccountLink generates an account_update link for the merchant to configure IBAN.
func (s *Service) CreateStripeBankAccountLink(ctx context.Context, merchantID string) (string, error) {
```

Aucun `PaymentMethodType` de type `sepa_debit`, aucun `stripe.SetupIntent`, aucune table ou structure liée à un mandat de prélèvement n'a été trouvée — ni dans le code Go, ni dans les migrations SQL (`migrations/`), ni dans le schéma live introspecté sur staging (aucune table dont le nom contient « sepa » ou « mandate » parmi les tables présentes en base).
## 6. Produits et création en masse

### 6.1 Interface de création de produits en masse dans le back-office

Il n'existe **pas** de composant dédié uniquement à « la création en masse » isolé du reste : la création groupée de produits est l'une des **trois portes** d'un unique assistant (« wizard ») d'import, monté depuis la page `wello-back-office/src/pages/Menu.tsx:14,24,103,348,357,626` via le composant `ProductImportDialog` (`src/components/menu/import/ProductImportDialog.tsx`), piloté par le hook `useProductImport` (`src/hooks/useProductImport.ts`).

La porte « saisie en masse » proprement dite est :
- Écran de choix : `src/components/menu/import/ImportDoorPicker.tsx:71-86` (carte « Je saisis mes produits à la main »)
- Grille de saisie : `src/components/menu/import/ImportManualStep.tsx`
- Une ligne de grille : `src/components/menu/import/manual/ImportManualRow.tsx`
- Logique pure (validation, construction du payload) : `src/lib/manualImport.ts`

**Champs présentés à l'utilisateur** (`ImportManualStep.tsx:96-113`, une ligne = un produit, deux niveaux par cellule) :
- Nom * (obligatoire) et Description (même cellule, nom au-dessus)
- Catégorie * (obligatoire, saisie libre avec autocomplétion `<datalist>` alimentée par les catégories déjà saisies dans la grille + les catégories existantes du menu — `manualCategorySuggestions`, `manualImport.ts:210-228`)
- Trois blocs « Sur place / À emporter / En livraison », chacun avec **Prix (en euros, ex. « 9,50 ») puis TVA** (sélectionnée, pas tapée, parmi les taux réellement configurés chez le marchand — voir 6.4)
- Pas de champ tags dans la grille — ils s'ajoutent ensuite depuis la fiche produit (`manualImport.ts:204-207`)

Validation côté client (`manualImport.ts:114-179`) avant tout appel réseau : nom requis et unique dans la grille (insensible à la casse), catégorie requise, champs prix numériques valides, **TVA requise sur les trois canaux** (alors que l'API l'accepte vide — commentaire explicite ligne 8-14 : « un taux absent... laisse le canal Available mais non résolu côté preview, sans jamais apparaître dans tva_rates » — le front resserre donc la règle par rapport au contrat API).

**Endpoint appelé** : `POST /menu/import/preview` en `application/json` (et non un endpoint de création directe — voir 6.3, c'est un *dry-run*), via `menuImportService.previewFromManual()` (`src/services/menuImportService.ts`) → `useProductImport.submitManual` → `manualPreviewMutation`.

**Format exact du payload** (`buildManualPayload`, `manualImport.ts:193-208`, type `ImportManualProductPayload` défini `src/types/import.ts:316-327`, miroir de `menu.ImportPreviewJSONProduct` côté Go, `internal/modules/menu/import_models.go:31-45`) :

```json
{
  "provider": "manual",
  "products": [
    {
      "name": "Pizza Margherita",
      "description": "Tomate, mozzarella, basilic",
      "category": "Pizzas",
      "price": 950,
      "price_take_away": 950,
      "price_delivery": 1050,
      "tva_in": 10,
      "tva_take_away": 5.5,
      "tva_delivery": 5.5,
      "tags": []
    }
  ]
}
```

Points notables sur ce format :
- Les prix sont en **centimes** (`price`, `price_take_away`, `price_delivery`), convertis euros → centimes côté back-office au moment de l'envoi (`manualImport.ts:184-191` : « C'est ici, et seulement ici, que les euros deviennent des centimes »), exactement comme `CreateProductPayload` (création unitaire, voir 6.4).
- La TVA (`tva_in`, `tva_take_away`, `tva_delivery`) est envoyée en **taux pourcentage brut** (`float64`, ex. `5.5`), **pas** en `tva_id` — c'est la prévisualisation côté serveur qui résout le taux vers un `tva_categories.tva_id` (`import_models.go:26-30`). C'est une différence structurelle avec la fiche de création unitaire de produit, qui envoie directement des `tva_*_id`.
- `category` est un **nom de catégorie**, pas un identifiant — la preview réutilise une catégorie existante homonyme ou en propose la création (`import_models.go` commentaire de champ, `src/types/import.ts:314-315`).
- `provider` vaut `"manual"` (`importer.ManualSlug`, `internal/modules/menu/importer/manual.go:12`), utilisé uniquement comme clé de traçabilité/idempotence dans les tables `import_*_mapping`.

Ce payload n'écrit **rien** en base : il déclenche uniquement un calcul de prévisualisation (voir 6.3 pour la suite du parcours — écran de vérification puis `POST /menu/import/commit`).

### 6.2 Création de catégories en masse

**La création de catégories en masse n'existe pas** comme fonctionnalité autonome équivalente à 6.1. Deux mécanismes coexistent :

**a) Création unitaire, un formulaire minimal** — page `src/pages/CategoriesTable.tsx:159-627`. Le bouton « Nouvelle catégorie » (`CategoriesTable.tsx:448-451`) ouvre une boîte de dialogue à **un seul champ texte** (`CategoriesTable.tsx:574-600`, le nom), sans TVA, sans image, sans ordre — ces attributs se règlent ensuite via des actions séparées (upload d'image `PUT /products/categories/{category_id}/image`, réordonnancement par glisser-déposer avec `dnd-kit` puis `PATCH /display-orders`). Le hook `useCategoryData.createProductCategory` (`src/hooks/useCategoryData.ts:65-67`) appelle `menuService.createProductCategory(name)` (`src/services/menuService.ts:981-996`) :

```
POST /menu/products/categories
{ "name": "Pizzas" }
```

Côté API : `menuH.CreateProductCategory` (`internal/modules/menu/handler.go:545-573`) → `MenuService.CreateProductCategory` (`internal/modules/menu/service.go:345-358`, injecte `MerchantID` depuis le token) → `MenuRepository.CreateProductCategory` (`internal/modules/menu/repository.go:3997+`). Payload Go : `CreateProductCategoryPayload{ Name string; MerchantID string }` (`internal/modules/menu/models.go:337-340`) — un seul champ exposé côté client. Le repository capitalise la première lettre, calcule `categ_order` comme `MAX(categ_order)+1` pour le marchand, et insère avec `merchant_categ_id = ''` explicite (`repository.go:4018-4021`, commentaire sur la stricte-mode Postgres vs. MySQL non strict). **Aucune route bulk n'existe** pour `productcateg` (`grep` sur `cmd/api/routes.go` ne fait ressortir qu'une seule route `POST /products/categories`, sans pendant `/bulk` ou `/batch`).

**b) Création indirecte et massive via l'import de produits (porte 6.3)** — c'est en réalité **là** que se trouve la seule voie de création de plusieurs catégories en une opération : le pipeline d'import (fichier, saisie manuelle, ou copie d'un autre établissement) détecte les libellés de catégorie non résolus dans le catalogue source et les crée automatiquement lors du commit, en même temps que les produits qui les référencent (`CanonicalCategory`, `internal/modules/menu/importer/models.go:95-100` ; comptage `categories_to_create` dans `ImportPreviewSummary`, `src/types/import.ts:68-69`). Mais ceci n'est jamais exposé comme un écran « créer des catégories » indépendant — c'est un effet de bord du commit d'import produits.

### 6.3 Les « trois portes » d'import de produits

Le brief anticipait une porte « import fournisseur (Uber Eats/Deliveroo) ». **Ce n'est pas ce que le code implémente.** Il existe bien un système « trois portes », mais les trois portes réelles, telles que documentées dans le code lui-même (`internal/modules/menu/importer/models.go:1-14`, doc du package : « Trois portes d'entree convergent vers un seul pipeline : un provider tiers (Zelty en premier), un template .xlsx defini par Wello, et un formulaire de saisie en masse cote back-office ») sont :

1. **Un export d'un logiciel de caisse tiers** — aujourd'hui uniquement **Zelty** (éditeur de caisse français), pas Uber Eats/Deliveroo.
2. **Le modèle Wello Resto (.xlsx) téléchargé puis rempli** — le « modèle personnalisé/gabarit » attendu par le brief.
3. **La saisie de masse** (formulaire, couverte en 6.1).

Le code a en réalité **une quatrième porte**, non prévue dans le brief : la **copie du catalogue d'un autre établissement** (« autre établissement »), exposée dans l'écran de choix comme quatrième carte (`ImportDoorPicker.tsx:88-103` : « Je copie un autre établissement »).

L'intégration Uber Eats/Deliveroo (`internal/modules/ubereats/`, `internal/modules/deliveroo/`, et `internal/modules/menu/mapper_ubereats.go`, `mapper_deliveroo.go`) est un mécanisme **totalement distinct** : elle sert à **pousser** (`PUT`) le menu Wello déjà existant vers ces plateformes de livraison — DTOs `DeliverooMenu`/`UberEatsMenu` construits à partir des produits Wello (`mapper_ubereats.go:1-16`, `mapper_deliveroo.go:1-30`) — et non à importer un catalogue depuis elles. Les tables `import_products_mapping` etc. mentionnées dans le brief comme hypothèse de staging d'import sont bien, comme suspecté, de simples **tables de correspondance d'identifiants** (external_id ↔ wello_id) pour rejouabilité/idempotence de *tout* import (fichier, saisie, ou autre établissement) — pas propres à Uber Eats/Deliveroo, et pas un mécanisme de staging pré-commit.

#### Pipeline commun

Toutes les portes convergent vers un même pipeline en 3 temps, orchestré par `internal/modules/menu/import_service.go` :
1. **Parse** → `*importer.IntermediateImport` (représentation neutre, `models.go:29-59`)
2. **Preview** (dry-run) → `importer.BuildPreview` (`internal/modules/menu/importer/preview.go`), dépose un **snapshot en cache Redis sous un token à durée de vie limitée** (`ImportPreviewTTL`), ne touche jamais la base (`import_handler.go:36-37` : « Aucune écriture : ni en base, ni sur le menu »)
3. **Commit** (seul point d'écriture) → `importer.BuildCommitPlan` + `MaterializeImportTx` dans une transaction unique

Endpoints (`cmd/api/routes.go:896-910`, tous sous `permission.CatalogManage`) :
```
POST /menu/import/preview                 (multipart OU JSON selon Content-Type)
POST /menu/import/commit
GET  /menu/import/template?provider=...
POST /menu/import/preview-from-merchant   (porte "autre établissement")
```

#### Porte 1 — « J'importe depuis ma caisse actuelle » (fichier)

Composants : `ImportProviderStep.tsx`. Deux providers enregistrés dans `importer.DefaultRegistry()` (`internal/modules/menu/importer/provider.go:47-52`) : `NewZeltyProvider()` et `NewWelloGenericProvider()`. Le front-office liste ces deux options sous un seul écran (`src/types/import.ts:27-40`, `IMPORT_PROVIDERS`), avec les libellés « Modèle Wello Resto rempli » et « Zelty ».

`PreviewImport` distingue le mode par `Content-Type` (`internal/modules/menu/import_handler.go:30-56`) :
```go
// Deux modes sur la même route, distingués par le Content-Type :
//   - multipart/form-data : champs "provider" et "file", pour un export d'un
//     éditeur tiers ou le template Wello ;
//   - application/json : produits saisis directement, pour le formulaire de
//     masse du back-office.
```
Champs multipart (`import_models.go:11-14`) : `provider`, `file`. Taille max 5 Mo (`maxImportFileSize`, `import_models.go:5-8`, vérifiée aussi côté client `useProductImport.ts:140-146`).

Format Zelty (`internal/modules/menu/importer/zelty.go:1-80`) : classeur `.xlsx` mono-feuille « au format long », 12 colonnes, sections Tag/Produit/Option/Option Value discriminées par la colonne « Type » ; un seul prix par produit (recopié sur les 3 canaux) ; aucune description, aucune image, aucun lien produit↔option, aucun min/max de groupe d'options (posés par défaut dans `applyDefaults`, `models.go:216-221`). Pas de modèle téléchargeable pour Zelty (`hasTemplate: false`) — l'utilisateur produit ce fichier depuis son propre logiciel de caisse.

Format Wello générique (`internal/modules/menu/importer/wello_generic.go`) : tabulaire, une ligne d'en-tête + une ligne par produit, colonnes reconnues par alias insensibles à la casse/accents (`welloGenericAliases`, lignes 71-92) : Nom*, Description, Catégorie*, Prix sur place*/emporté/livraison, TVA sur place/emporté/livraison, Tags. Colonnes obligatoires : Nom, Catégorie, Prix sur place (`welloGenericRequired`, lignes 62-66) ; une catégorie vide n'est **pas** un rejet du fichier — elle est réclamée à l'écran de vérification (commentaire ligne 182-184).

#### Porte 2 — « Je pars d'un modèle vierge » (le gabarit)

`ImportDoorPicker.tsx:45-69` : bouton « Télécharger le modèle » → `GET /menu/import/template?provider=wello-generic` (`import_handler.go:220-270`, `DownloadImportTemplate`), servi en pièce jointe (`Content-Disposition: attachment`, MIME `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`). Généré par `WelloGenericProvider.BuildTemplate` (interface `TemplateProvider`, `import_service.go:226-236` : seul un provider qui implémente cette interface expose un modèle — Zelty ne l'implémente pas, d'où `ErrImportTemplateUnavailable` si on tente `?provider=zelty`). Une fois rempli, ce fichier se ré-importe **par la porte 1**, en sélectionnant « Modèle Wello Resto rempli » (`ImportDoorPicker.tsx:50-55` : « revenez l'importer par la première porte ») — ce n'est donc pas une porte d'entrée distincte côté API, seulement côté UX (téléchargement puis retour vers la porte 1). Aucune validation ni preview n'a lieu au moment du téléchargement ; la validation intervient au ré-upload, comme tout fichier de la porte 1.

#### Porte 3 — Saisie de masse

Voir 6.1. Construit directement un `IntermediateImport` sans fichier (`importer.BuildManualImport`, `internal/modules/menu/importer/manual.go`), slug `"manual"`.

#### Porte 4 (hors brief) — « Je copie un autre établissement »

`ImportMerchantSourceStep.tsx`, `POST /menu/import/preview-from-merchant`, service `import_merchant_service.go:33-80`. Contrôle d'accès explicite et non mis en cache à chaque appel (`HasRightsOnMerchant(ctx, userID, sourceMerchantID)`, ligne 63) ; erreur générique `ErrSourceMerchantNotFound` renvoyée en **404** aussi bien pour un ID inexistant, un marchand sur lequel l'utilisateur n'a pas de droits, ou une tentative de copie de soi-même sur soi-même — pour ne jamais confirmer l'existence d'un marchand à un appelant non autorisé (commentaire lignes 12-18, 55-61). Seule porte à porter la composition (recettes, `Components`), le rattachement d'options aux produits (`AttributeExternalIDs`) et la disponibilité réelle par canal (`AvailableIn/TakeAway/Delivery`) — les trois autres portes ne les fournissent jamais (`models.go:150-163`).

#### Validations et preview avant commit (commune aux 4 portes)

`importer.BuildPreview` (`preview.go`) calcule, sans écrire :
- Résolution TVA (taux brut → `tva_categories.tva_id`, table globale — voir 6.4), avec compteur `unresolved_tva_rates`
- Détection des collisions de nom avec un produit existant (`ImportPreviewNameCollision`, arbitrage `skip` / `import_anyway`)
- Détection des produits déjà importés précédemment (mapping `import_*_mapping` existant), avec un contrôle de fraîcheur (`mapping_stale` : le mapping pointe vers une entité Wello supprimée depuis, cf. `liveImportedEntities`, `preview.go:134-148`)
- Classification tag → catégorie ou tag Wello (`TagClassification`)
- Catégorisation d'un produit sans catégorie explicite (`needs_category`)
- Génération de `warnings` typés (`tva_rate_unresolved`, `product_needs_category`, `product_name_collision`, `label_dropped`, `tag_synthesized`, etc., `preview.go:44-52`)

Le commit (`ImportService.CommitImport`, `import_commit_service.go:46-97`) recharge les données depuis la base au moment de l'écriture (pas depuis le snapshot figé, pour refléter tout changement survenu entre-temps), reconstruit un `CommitPlan` via `importer.BuildCommitPlan`, et **refuse intégralement** (aucune ligne écrite) si des `CommitBlocker` subsistent — HTTP 422 avec la liste des blocages (`ImportNotCommittableError`, `import_commit_service.go:24-32`, `import_handler.go:204-209`). Un token de preview expiré ou déjà consommé renvoie un HTTP 410 (`import_handler.go:196-202`, pas 404, pour signaler explicitement au client qu'il doit relancer un import plutôt que réessayer).

Il y a donc bien une étape de prévisualisation avant écriture, pour les 4 portes sans exception — c'est le cœur explicite de l'architecture (« le seul effet de bord est le dépôt du snapshot en cache », `import_handler.go:36-37`).

### 6.4 Saisie et stockage du taux de TVA sur un produit

**Stockage** (confirmé par le schéma introspecté, réutilisé tel quel) : `products.tva_in_id`, `products.tva_delivery_id`, `products.tva_take_away_id` (int, `NOT NULL DEFAULT 0`), chacun une FK applicative (non déclarée en contrainte SQL, jointe uniquement en `INNER JOIN` dans le code, ex. `internal/modules/menu/repository.go:988-990,1073-1075,1551-1553,1635-1637,2677-2679`) vers `tva_categories.tva_id`.

**`tva_categories` est bien un référentiel global, partagé par tous les marchands — confirmé côté code, pas seulement supposé.** Preuve directe : `POSRepository.GetTVARates(ctx, merchantID)` (`internal/modules/pos/repository.go:292-`) reçoit un paramètre `merchantID`, mais **ne l'utilise dans aucune clause de la requête** :

```go
func (r *POSRepository) GetTVARates(ctx context.Context, merchantID string) ([]ConsumptionType, error) {
    ...
    query := `
        SELECT ...
        FROM labels l
        INNER JOIN tva_categories t ON l.label_value = t.delivery_type
        WHERE l.label_type = 'order_type'
          AND l.lang = 'FR'
          AND t.enabled = TRUE
        ORDER BY l.id ASC, t.tva_rate ASC`
```
(`internal/modules/pos/repository.go:298-313`) — aucun `WHERE ... merchant_id = ?` nulle part. Tous les marchands reçoivent exactement le même jeu de taux. Le back-office lui-même le documente comme tel dans son cache de requêtes : `src/lib/queryKeys.ts:13-19` — « Référentiel global (`GET /pos/tva_rates`), stable et partagé ». Le code d'import confirme également : `internal/modules/menu/importer/preview.go:61-63` — « La table est globale (pas de merchant_id) : un couple (taux, canal) suffit à désigner un tva_id ».

Le canal de vente est porté par `tva_categories.delivery_type`, joint avec la table globale `labels` (`label_type = 'order_type'`, `lang = 'FR'`) pour obtenir le libellé traduit affiché à l'utilisateur (« Sur place », « À emporter », « En livraison »). Le commentaire SQL de la colonne `delivery_type` annoncerait des valeurs numériques (0/1/3) mais c'est **faux** — les données réelles portent les chaînes `'IN'`, `'TAKE_AWAY'`, `'DELIVERY'` ; c'est explicitement documenté dans le code comme une divergence entre le commentaire de schéma et la réalité (`internal/modules/menu/importer/models.go:261-266` : « Il est faux — les donnees portent 'IN', 'TAKE_AWAY' et 'DELIVERY', ce que confirment la jointure ... et le back-office »).

**Où l'utilisateur choisit ce taux :**

- **Fiche de création/édition unitaire de produit** — `src/components/menu/SimpleProductSheet.tsx`. Un composant `TvaRateSelect` (ligne 130+) par canal, alimenté par `useProductEditData(open)` (`src/hooks/useProductEditData.ts:7,22`) qui appelle `menuService.getTvaRates()` → `GET /pos/tva_rates` (le même endpoint global, `menuService.ts:288-292`), puis filtré côté client par `delivery_type` (`findTvaRates('IN' | 'TAKE_AWAY' | 'DELIVERY')`, `SimpleProductSheet.tsx:231-235`). L'utilisateur choisit un `<Select>` de taux réels (ex. 5,5 % / 10 % / 20 %), jamais une saisie libre. Le payload envoyé (`ProductCreatePayload`, `src/types/menu.ts:320-333`) porte directement les identifiants résolus :
  ```ts
  tva_in_id: string;
  tva_take_away_id: string;
  tva_delivery_id: string;
  ```
  vers `POST /menu/products` (`menuService.createProduct`, `menuService.ts:1047-1059`). Les trois taux sont obligatoires à la création (validation front, `SimpleProductSheet.tsx:559-566`).

- **Saisie de masse (porte 3, 6.1)** — même référentiel `GET /pos/tva_rates` (`ImportManualStep.tsx:46-50`, requête react-query `qk.menuTvaRates.all`), filtré par canal, mais le payload transmis à l'API porte le **taux en pourcentage brut** (`tva_in`, `tva_take_away`, `tva_delivery`, `float64|null`) et non un `tva_id` — c'est la preview serveur (`importer.BuildPreview`) qui le résout ensuite en `tva_id`, exposé au restaurateur dans l'écran de vérification (`ImportTvaResolution.tsx`) où il peut corriger le mapping avant validation.

- **Fichiers importés (portes 1 et 2)** : le taux est lu tel quel dans le fichier (colonne « TVA » / « TVA emporte » / « TVA livraison », `wello_generic.go:36-38`, ou colonnes fixes de l'export Zelty, `zelty.go:22-24`), toujours en pourcentage brut, jamais en `tva_id` — résolution identique à la saisie manuelle, dans la preview.

En résumé : la saisie **unitaire** d'un produit choisit directement un `tva_id` existant dans le référentiel global ; les **trois portes d'import en masse** manipulent un taux en pourcentage et laissent la résolution vers `tva_id` à l'étape de prévisualisation — un utilisateur ne peut donc jamais créer de nouveau taux de TVA depuis aucun de ces parcours : le référentiel `tva_categories` n'est modifiable par aucune route découverte dans `internal/modules/menu/` ni `internal/modules/pos/` (aucun `POST`/`PATCH` sur `tva_categories` n'apparaît dans `cmd/api/routes.go`) — ce qui est cohérent avec son caractère de table globale, hors du périmètre applicatif d'un marchand.
## 7. Paramètres marchand

*Note méthodologique : l'API n'a pas de module `internal/modules/settings/` ni `internal/modules/merchant/` dédié. La gestion des paramètres marchand est portée par le module `internal/modules/pos/` (fichiers `create_*.go`, `repository.go`, `service.go`, `handler.go`), qui lit/écrit directement les tables `merchant` et `merchant_parameters` (+ tables satellites).*

### 7.1. Table(s) de paramétrage d'un marchand

**`merchant_parameters`** (PK `merchant_id`, relation 1-1 avec `merchant`) — liste complète des colonnes (schéma Postgres staging, introspection live) :

| Colonne | Type | Défaut |
|---|---|---|
| `manage_on_site` | bool | `true` |
| `manage_take_away` | bool | `true` |
| `manage_delivery` | bool | `true` |
| `last_menu_update` | timestamptz | **NOT NULL, aucun défaut** |
| `concurrent_preparation_capacity` | int | `1` |
| `delivery_fees` | int | `0` |
| `delivery_fees_limit` | int | `0` |
| `delivery_distance_limit` | int | `5000` |
| `minimum_cart_for_delivery_order` | int | `1000` |
| `kitchen_show_only_paid` | bool | `false` |
| `kitchen_show_pending_approval` | bool | `false` |
| `kitchen_distribution_mode` | varchar | `'READY_FOR_DISTRIBUTION'` |
| `production_display_mode` | varchar | `'CLASSIC'` |
| `preparation_time_mode` | varchar | `'AUTO'` |
| `preparation_time` | int | `15` |
| `minimum_preparation_time` | int | `300` |
| `maximum_preparation_time` | int | `3600` |
| `disable_components_under_safety_stock` | bool | `false` |
| `service_required_for_ordering` | bool | `false` |
| `cash_register_required_for_ordering` | bool | `true` |
| `waiter_app_can_cash_in` | bool | `true` |
| `waiter_app_can_clock_in` | bool | `false` |
| `auto_complete_orders` | bool | `false` |
| `auto_complete_orders_delay` | int | `10` |
| `auto_accept_sno_delivery_orders` | bool | `false` |
| `auto_accept_sno_take_away_orders` | bool | `false` |
| `automatically_add_customer_rewards` | bool | `true` |
| `warning_new_order_not_paid` | bool | `true` |
| `enable_advance_orders` | bool | `false` |
| `advance_order_days` | int | `3` |
| `pager_number_required` | bool | `false` |
| `pos_auto_lock_enabled` | bool | `false` |
| `pos_auto_lock_delay_minutes` | int | `5` |
| `pos_upsell_enabled` | bool | `false` |
| `customer_form_requirements` | jsonb | `NULL` (nullable) |
| `enabled_rating` | bool | `false` |
| `currency` | varchar(5) | `'EUR'` |
| `is_open` | bool | `false` |
| `primary_color` | varchar | `'#212529'` |
| `text_color_on_primary_color` | varchar | `'#ffffff'` |
| `zoning_type` | varchar | `NULL` (nullable) |
| `radial_cone_count` | int | `8` |
| `radial_zone_ranges` | varchar | `'0-3,3-5,5-999'` |
| `grid_cell_size_km` | int | `2` |
| `grid_origin_lat` / `grid_origin_lng` | double | `NULL` (nullable) |
| `cardinal_cone_count` | int | `4` |
| `cardinal_zone_ranges` | varchar | `'0-1,1-3,3-999'` |
| `enable_upsell` | bool | `false` |
| `upsell_max_items` | int | `3` |
| `enable_translation` | bool | `false` |
| `pos_covers_count_required` | bool | `false` |

Édité dans le back-office via `src/components/settings/EstablishmentTab.tsx` (onglets « Général », « Prise de commande », « Production », « Livraison », « Sécurité », « Horaires d'ouvertures »), champs déclarés dans `src/config/settingsConfig.ts` (`establishmentTimingsFields`, `establishmentOrderingFields`, `establishmentProductionDisplayFields`, `establishmentSecurityFields`) et `src/types/settings.ts` (`EstablishmentSettings`), via `src/services/settingsService.ts::getEstablishmentSettings/updateEstablishmentSettings` → `GET/PATCH /pos/settings` (`cmd/api/routes.go:771-772`, gérées par `handler.go::GetSettings`/`UpdateMerchantSettings`, sans permission RBAC dédiée — seulement `authMiddleware`).

**`merchant`** — identité/coordonnées, voir 7.3.

**Tables satellites** (une ligne par `merchant_id`, PK = `merchant_id`, sauf mention contraire) :

- **`merchant_marketing_settings`** — gouverne les notifications SMS/email au client (activation, expéditeur, gabarits, prix unitaire SMS, identifiants Messaggio). Colonnes : `sms_enabled` (défaut 1), `sms_unit_price` (défaut 7), `email_enabled` (défaut 1), `sms_sender_name`, `email_sender_name`, `sms_template`, `email_template`, `tracking_template` (défaut `'Votre commande #{order_id} est en cours de livraison. Suivez-la ici : {tracking_url}'`), `messaggio_login`/`messaggio_from`. **Aucun composant back-office trouvé** — recherche exhaustive (`sms_enabled`, `email_enabled`, `messaggio`, `tracking_template`, `sms_sender_name`, `email_sender_name`, tout composant nommé « marketing ») dans `wello-back-office/src` : zéro résultat pertinent (seuls des faux positifs liés au menu/catégories marketing). Cette table n'a donc aujourd'hui **aucune interface d'édition connue** — modification uniquement en base directe.
- **`scannorder_settings`** (~56 colonnes) — gouverne la commande en ligne QR-code/scan&order : activation par mode (livraison/à emporter/sur place), branding, SEO, frais (`variable_fees` défaut `0.007`, `fixed_fees` défaut `15`), `commission_rate`, `cgv_link`, `legal_notices_link`, `closed_until`, temps de préparation additionnel (`extra_prep_minutes`/`extra_prep_until`, migration `085`). Édité dans `src/pages/ScanNOrder.tsx` (via `src/services/onlineOrdersService.ts::getOnlineOrdersConfig/updateOnlineOrdersConfig`).
- **`kiosk_settings`** — gouverne le comportement des bornes kiosk : modes de service (`fulfillment_dine_in`/`fulfillment_take_away`), `pager_number_required`, `show_allergens`, `inactivity_timeout_sec` (défaut 90), `upsell_enabled`, `pay_at_counter_enabled` (défaut true), `card_payment_enabled` (défaut false), frais (`variable_fees`/`fixed_fees`, migration `061`). Édité dans `src/pages/kiosks/KioskSettingsPage.tsx`, via `PUT /pos/settings/kiosk/settings` (`cmd/api/routes.go:1681`), protégé par `middleware.RequirePermission(permission.KioskManage)` — la lecture (`GET /pos/settings/kiosk/settings`, ligne 1659) n'a en revanche aucune permission dédiée, seulement `authMiddleware`.
- **`haccp_settings`** (~25 booléens) — gouverne les exigences de conformité HACCP (traçabilité, contrôles obligatoires par étape). Édité dans `src/pages/haccp/Settings.tsx` (via `src/services/haccpService.ts`).
- **`planning_settings`** — paramètres légaux du planning RH (repos minimal journalier, fenêtres/multiplicateurs de nuit, multiplicateur jour férié). Édité via `src/components/team/planning/PlanningSettingsModal.tsx` (page `src/pages/equipe/PlanningPage.tsx` / `EquipeSettings.tsx`).
- **`bookings_settings`** — durée de réservation, tailles de tablée min/max, liste d'attente, SMS de réservation. Édité dans `src/pages/reservations/Settings.tsx` (via `src/services/reservationsService.ts`).
- **`hours_of_operation`** (une ligne PAR CRÉNEAU, pas 1-1 par marchand) : `id`, `merchant_id`, `day_of_week_from`/`to`, `hour_from`/`to`, `first_booking_time`/`last_booking_time`, `booking_capacity` (défaut 0), `valid_from`/`valid_to`, `enabled` (défaut 1). Édité dans `EstablishmentTab.tsx` onglet « Horaires d'ouvertures » (composant `OpeningHours`), via `POST/PATCH/DELETE /pos/settings/hours_of_operations[/…]`.
- **Périodes de fermeture (« vacances »)** — table satellite distincte, non anticipée dans le brief : gérée par le composant `VacationPeriods` du même onglet, via `GET/POST/PATCH/DELETE /pos/settings/vacations[/…]` (`cmd/api/routes.go:777-780`, handlers `ListPlanningVacationPeriods`/`CreatePlanningVacationPeriod`/…, migration `083_planning_vacation_periods`). Distincte des congés RH du module `planning`.

### 7.2. Moyens d'encaissement (méthodes de paiement)

**Il n'existe aucune table SQL de moyens de paiement paramétrables**, aucune contrainte `ENUM` en base. La colonne `mop` (`payments.mop varchar(10) NOT NULL`, `varchar(20)` pour les remises) est un `varchar` libre. Les moyens de paiement sont **codés en dur, dupliqués indépendamment dans au moins 4 endroits**, avec des listes divergentes :

**a) API Go** — pas de liste unique, constantes éparpillées :
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
`"ES"` (Espèces) n'est même pas une constante nommée (`internal/modules/cash_registers/repository.go:476`, `if mopLine.MOP == "ES" {`). La struct `MOPLine` est dupliquée (`internal/models/request_objects.go:161-165` et `internal/modules/cash_registers/models.go:89-93`) avec un type `Amount` différent (`int` vs `float64`). Le module `analytics` a sa propre liste canonique séparée, `internal/modules/analytics/payment_methods.go:10-21` : `CB, ES, STRIPE, TR, CURRENCY, UBER_EATS, DELIVEROO, other` — avec un commentaire explicite indiquant que `payments.mop` porte en réalité **14 valeurs brutes distinctes en production** (7 moyens de paiement réels + marqueurs de geste commercial `PERCENTAGE`/`DISCOUNT` stockés comme des moyens de paiement + un artefact webhook `STRIPE_WEB_HOOK` + une valeur `'1'` aberrante).

**b) POS Flutter** — enum Dart centralisé mais propre à ce dépôt (`lib/models/orders/method_of_payment_enum.dart:6-89`) : `es/STRIPE/cb/tr/carteTicketRestaurant/other/qr/discountByAmount/discountByPercentage`. Seul un sous-ensemble (CB, ES, TR, QR) est exposé au caissier, en dur (`lib/ui/widgets/dialogs/calculator/right_pannel/calculator_menu_view.dart:49-76`) — non piloté par un paramètre marchand.

**c) Borne kiosk** — deux méthodes, affichage conditionné par deux flags qui viennent bien de l'API (`kiosk_settings.card_payment_enabled`/`pay_at_counter_enabled`) — `lib/presentation/screens/payment_screen.dart:173,179` (`cardPaymentEnabled ?? false`, `payAtCounterEnabled ?? true`). Identifiants `'card'`/`'pay_at_counter'` non alignés avec les codes MOP de l'API ni l'enum POS Flutter (traduction faite côté API, `internal/modules/kiosk/service.go:1562-1628`).

**d) Back-office** — deux listes en dur de plus (`src/components/cash/ClosureModal.tsx:50-56`, `PRESETS` = CB/CASH→ES/TR/CHEQUE/OTHER ; `src/services/cashRegisterService.ts:271-289`, `normalizeMopCode`). `CHEQUE` n'existe dans aucune des listes de l'API ni du POS Flutter ; inversement `STRIPE`, `QR`, `CARTE TICKET RESTAURANT`, `CURRENCY`, `PERCENTAGE` (POS Flutter) n'apparaissent dans aucune liste du back-office.

**Synthèse** : aucun des 4 emplacements ne lit une liste depuis l'API — il n'existe aucun appel centralisé « liste des moyens de paiement du marchand ».

### 7.3. Horaires d'ouverture, fuseau horaire, informations légales

**Fuseau horaire** : `merchant.timezone varchar(50) NOT NULL DEFAULT 'Europe/Paris'` — NOT NULL avec défaut SQL, jamais bloquant. `CreateMerchantRequest` (création) n'a pas de champ `timezone` : la valeur `'Europe/Paris'` s'applique systématiquement à la création, quel que soit le pays réel du marchand. Le modèle bas niveau `models.MerchantSettings` (`internal/models/request_objects.go:640-662`) porte un champ `Timezone *string` et **l'API l'écrit bien** si fourni (`internal/modules/pos/repository.go:1049-1052`, `UpdateMerchant`) — mais **le formulaire back-office (`EstablishmentInfo`, `src/types/settings.ts:22-40`, et `establishmentInfoFields`, `src/config/settingsConfig.ts:11-21`) n'expose aucun champ timezone** : la modification n'est possible qu'en appelant l'API bas niveau directement, jamais depuis l'UI de settings actuelle.

**Horaires d'ouverture** : `hours_of_operation`, toutes colonnes NOT NULL au niveau de la ligne, mais aucune contrainte n'impose qu'au moins une ligne existe pour un marchand. **Aucune ligne n'est créée automatiquement à la création** : `InitMerchantSatellites` initialise 7 tables satellites (voir 8.4) mais ne touche jamais `hours_of_operation`. Le statut ouvert/fermé « manuel » (`merchant_parameters.is_open`, défaut `false`) fait qu'un nouveau marchand démarre **fermé**.

**Informations légales** :

| Info | Table.colonne | NULL ? | Validation Go à la création | Éditable en back-office ? |
|---|---|---|---|---|
| Raison sociale | `merchant.fullName` | NOT NULL, aucun défaut | Oui | Oui (`establishmentInfoFields`, champ `name`) |
| SIRET | `merchant.SIRET` | NOT NULL, aucun défaut | Oui | **Non — `readOnly: true`** (`settingsConfig.ts:14`) : affiché mais non modifiable après création |
| TVA intracom | `merchant.vat_number` | **nullable** | Aucune | **Non — aucun champ dans l'UI ni dans `MerchantSettings` (le modèle bas niveau lui-même n'a pas de champ `VatNumber`)** |
| Adresse | `merchant.address`/`street_number`/`street`/`zip_code`/`city` | NOT NULL (sauf `country`) | Non | Oui (`AddressAutocomplete`) |
| Téléphone | `merchant.merchantTel` | NOT NULL, aucun défaut | Oui | Oui |
| Site web | `merchant.web_site` | NOT NULL, aucun défaut | Non | Non exposé dans `EstablishmentTab` (présent seulement à la création, `CreateEstablishmentDialog`) |
| Email | `merchant.email` | nullable | Non | Non exposé dans `EstablishmentTab` |

Concernant `vat_number` : **aucun chemin applicatif Go n'écrit jamais cette colonne** hors tests d'intégration. Lue uniquement pour l'en-tête de facture PDF (`internal/modules/pos/accounting/repository.go:89,106`). Aucun endpoint ne permet de la renseigner — elle reste `NULL` pour tout marchand créé via le flux normal.

**Validation applicative à la création** — `internal/modules/pos/create_service.go:14-19` :
```go
if strings.TrimSpace(req.FullName) == "" ||
    strings.TrimSpace(req.SIRET) == "" ||
    strings.TrimSpace(req.Tel) == "" ||
    strings.TrimSpace(req.PackageID) == "" {
    return CreateMerchantResponse{}, models.ErrInvalidInput
}
```
Seuls `FullName`, `SIRET`, `Tel`, `PackageID` sont vérifiés non-vides. `Address`, `StreetNumber`, `Street`, `ZipCode`, `City`, `WebSite`, `Email` ne sont validés nulle part côté Go, bien que la plupart soient `NOT NULL` en SQL — une chaîne vide satisfait la contrainte SQL sans satisfaire un besoin métier de « champ renseigné ». `timezone`, `lat`, `lng`, `vat_number`, `default_role_id` ne figurent pas dans les colonnes insérées par `InsertMerchant` : ils prennent systématiquement leur valeur `DEFAULT` SQL. Côté back-office, `CreateEstablishmentDialog.tsx` impose un minimum plus strict que l'API ne l'exige : `full_name`, `siret`, `tel` obligatoires via Zod (`formSchema`, lignes 37-47) — cohérent avec la validation Go — mais `email`, `address`, `zip_code`, `city`, `web_site` restent optionnels dans le formulaire, exactement comme côté API.

### 7.4. Mécanisme de valeurs par défaut à la création d'un marchand

**Oui, un mécanisme applicatif explicite existe**, pas seulement des `DEFAULT` SQL : le Go exécute, dans une transaction unique, une série d'`INSERT` explicites dans les tables satellites juste après la création de `merchant` (détail complet en 8.2, `InitMerchantSatellites`).

- La ligne `merchant_parameters` d'un nouveau marchand ne reçoit explicitement que `merchant_id` et `last_menu_update` — les ~52 autres colonnes prennent leur valeur `DEFAULT` SQL. Aucune valeur métier n'est fixée en dur côté Go pour `merchant_parameters`.
- Trois colonnes `NOT NULL` sans `DEFAULT` SQL obligent le Go à fournir une valeur explicite : `scannorder_settings.seo_title/seo_description/seo_keywords/seo_cuisine_type` (`''`), `merchant_parameters.last_menu_update` (horodatage UTC explicite), `bookings_settings.code` (`''`).
- Un rôle RBAC « admin » est attribué par défaut via `merchant.default_role_id`, positionné par `SetDefaultRoleID` uniquement s'il est encore `NULL` — et **redressé en continu** par une tâche cron `@hourly` (`cmd/api/tasks.go:81`, `taskManager.ReconcileSystemRolePermissions`, logique partagée avec `cmd/seed_system_roles`, `internal/modules/roles/repository.go::ReconcileSystemRoles`) : un établissement dont le rôle admin serait incomplet (nouvelle clé de permission ajoutée au catalogue après sa création, par exemple) est réparé automatiquement dans l'heure, sans intervention manuelle.
- Aucune ligne n'est créée pour `hours_of_operation` ni pour les « vacances » (§7.1) : aucun mécanisme de valeur par défaut, applicatif ou SQL — un nouveau marchand n'a aucun créneau d'ouverture jusqu'à saisie manuelle.

---

## 8. Création de compte actuelle

### 8.1. Endpoint(s) et chaîne handler → service → repository

**Route** — `cmd/api/routes.go:757-760` :
```go
r.Route("/pos", func(r chi.Router) {
    r.Use(authMiddleware)

    r.Post("/create", posH.CreateMerchant)
```
Soit **`POST /pos/create`**.

**Handler** — `internal/modules/pos/create_handler.go` :
```go
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

**Payload exact** (`internal/modules/pos/create_models.go`) :
```go
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
    UserID string `json:"user_id,omitempty"` // optionnel : lie l'utilisateur dans la même transaction
    Admin bool `json:"admin"`
}
type CreateMerchantResponse struct {
    MerchantID string `json:"merchant_id"`
}
```
Validation minimale : `FullName`, `SIRET`, `Tel`, `PackageID` non vides ; tout le reste accepté même vide, sans validation de format (pas de vérification d'email, pas de vérification que `PackageID` référence une ligne existante dans `packages`).

**Consommateur confirmé côté produit** : `wello-back-office/src/components/dashboard/CreateEstablishmentDialog.tsx` — un vrai bouton « Nouvel établissement » du tableau de bord, dont le commentaire de code (ligne 30) référence explicitement ce document d'audit (`/** IDs de la table packages — voir docs/audit-parcours-onboarding.md. */`). Appelle `authService.createMerchant({ ..., user_id: authData.user.id, admin: true })` — **c'est-à-dire le `user_id` de l'utilisateur back-office actuellement connecté, quel qu'il soit**, avec `admin: true`.

### 8.2. Ordre exact des opérations et atomicité

**Service** — `internal/modules/pos/create_service.go:13-74`, texte intégral :
```go
func (s *POSService) CreateMerchant(ctx context.Context, req CreateMerchantRequest) (CreateMerchantResponse, error) {
    if strings.TrimSpace(req.FullName) == "" ||
        strings.TrimSpace(req.SIRET) == "" ||
        strings.TrimSpace(req.Tel) == "" ||
        strings.TrimSpace(req.PackageID) == "" {
        return CreateMerchantResponse{}, models.ErrInvalidInput
    }

    merchantToken, err := helpers.GenerateToken(10) // 20-char hex token → VARCHAR(20)
    if err != nil {
        return CreateMerchantResponse{}, err
    }

    var merchantID string
    err = dbutils.RunInTx(ctx, s.posRepo.database, func(txCtx context.Context) error {
        // Step 1 — create merchant row
        merchantID, err = s.posRepo.InsertMerchant(txCtx, req, merchantToken)
        if err != nil { return err }

        // Step 2 — create subscription from the requested package
        if err := s.posRepo.InsertSubscription(txCtx, merchantID, strings.TrimSpace(req.PackageID)); err != nil { return err }

        // Step 3 — initialise companion tables
        if err := s.posRepo.InitMerchantSatellites(txCtx, merchantID); err != nil { return err }

        // Step 4 — RBAC lot 1: seed the two system roles and point the
        // merchant's default at "admin" (every account becomes
        // Administrateur while permissions are not yet exploited from the UI).
        adminRoleID, _, err := s.rolesRepo.EnsureSystemRoles(txCtx, merchantID)
        if err != nil { return err }
        if err := s.posRepo.SetDefaultRoleID(txCtx, merchantID, adminRoleID); err != nil { return err }

        // Step 5 — optional user linkage
        if strings.TrimSpace(req.UserID) != "" {
            if _, _, err := s.insertUserRightsTx(txCtx, req.UserID, merchantID, req.Admin, adminRoleID); err != nil { return err }
        }
        return nil
    })
    if err != nil { return CreateMerchantResponse{}, err }
    return CreateMerchantResponse{MerchantID: merchantID}, nil
}
```

**Ordre exact d'écriture, table par table** (`internal/modules/pos/create_repository.go:1-207`) :
1. `INSERT INTO merchant (fullName, address, street_number, street, zip_code, city, country, SIRET, merchantTel, web_site, email, token)` — `country` défaut `"France"` si vide.
2. `INSERT INTO subscriptions (merchant_id, package_id, stripe_subscription_id)` avec `stripe_subscription_id = ''` explicite.
3. `InitMerchantSatellites` — dans cet ordre interne :
   - 2× `INSERT INTO qrcodes (merchant_id, code, menu_only, mywelloresto_flag)` (menu standard, puis menu-only/mywelloresto)
   - `INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type)` (chaînes vides)
   - `INSERT INTO merchant_parameters (merchant_id, last_menu_update)`
   - `INSERT INTO merchant_marketing_settings (merchant_id)`
   - `INSERT INTO haccp_settings (merchant_id, created_at, updated_at)`
   - `INSERT INTO bookings_settings (merchant_id, code)` (`code = ''`)
   - `INSERT INTO cash_desks (merchant_id, name)` (`name = 'Caisse principale'`)
4. `EnsureSystemRoles` — crée (si absentes) 2 lignes `roles` (« admin », « staff ») pour ce `merchant_id`, peuplées via `INSERT INTO role_permissions`.
5. `UPDATE merchant SET default_role_id = ? WHERE id = ? AND default_role_id IS NULL` (pointant vers « admin »).
6. **Optionnel**, si `req.UserID` non vide : `INSERT INTO users_rights (user_id, merchant_id, token, admin, role_id, enabled) VALUES (?, ?, ?, ?, ?, TRUE)`.

**Atomicité : oui, une vraie transaction SQL native** (`sql.Tx`), aucune compensation applicative. `internal/utils/dbutils/run_in_tx.go:9-30` :
```go
func RunInTx(ctx context.Context, db *sql.DB, fn func(txCtx context.Context) error) error {
    if ExtractTx(ctx) != nil { return fn(ctx) }  // imbrication : réutilise la tx déjà ouverte
    tx, err := db.BeginTx(ctx, nil)
    if err != nil { return err }
    txCtx := InjectTx(ctx, tx)
    if err := fn(txCtx); err != nil { _ = tx.Rollback(); return err }
    return tx.Commit()
}
```
Toute erreur à n'importe quelle étape (1 à 6) déclenche `tx.Rollback()`, aucune ligne n'est persistée. L'appel imbriqué `EnsureSystemRoles` (étape 4) invoque lui-même `dbutils.RunInTx`, mais le garde `ExtractTx(ctx) != nil` fait qu'il réutilise la transaction déjà ouverte — les 6 étapes sont donc bien couvertes par une seule transaction, tout ou rien.

### 8.3. Exposition publique vs usage interne

**Route** (`cmd/api/routes.go:757-763`) :
```go
r.Route("/pos", func(r chi.Router) {
    r.Use(authMiddleware)
    r.Post("/create", posH.CreateMerchant)
    r.With(middleware.RequirePermission(permission.StaffManage)).Post("/link-user", posH.LinkUser)
    r.Get("/status", posH.GetPOSStatus)
    r.With(middleware.RequirePermission(permission.POSStatusManage)).Patch("/status", posH.UpdatePOSStatus)
```
**`POST /pos/create` n'est protégée que par `authMiddleware`** — contrairement à sa voisine `POST /pos/link-user` qui ajoute `.With(middleware.RequirePermission(permission.StaffManage))`. Aucune vérification RBAC n'encadre la création de marchand. `authMiddleware` exige uniquement un token Bearer valide correspondant à un `users` existant — ni permission, ni rattachement à un marchand particulier, ni rôle.

**Conséquence factuelle, confirmée par le produit lui-même et pas seulement par le code** : `POST /pos/create` est atteignable par tout utilisateur déjà authentifié, et c'est **effectivement exploité en self-service** — `wello-back-office/src/components/dashboard/CreateEstablishmentDialog.tsx` expose un bouton « Nouvel établissement » accessible depuis le tableau de bord à n'importe quel utilisateur connecté au back-office, qui crée l'établissement en s'auto-attribuant `user_id: authData.user.id, admin: true`. Ce n'est ni un endpoint public sans authentification, ni un endpoint réservé à un rôle admin/interne dédié : **tout compte back-office existant peut créer autant de nouveaux établissements qu'il le souhaite et en devenir administrateur**, sans validation ni approbation d'un tiers.

**Aucun endpoint d'auto-inscription (« register »/« signup ») pour créer un compte `users`** : recherche `register|signup` dans `cmd/api/routes.go` → aucune occurrence. Le groupe `/auth` n'expose que `login`, `mfa/fallback-sms`, `send-verification`, `verify`, `forgot-password`, `reset-password`, `pin`, `pin/set`, `pin/reset`. La création d'un `users` se fait via `POST /users`, qui exige `authMiddleware` **et** `middleware.RequirePermission(permission.StaffManage)` — pas de self-service pour un compte `users`, seulement pour un nouvel établissement rattaché à un compte `users` déjà existant.

### 8.4. Entités connexes créées automatiquement

| Table | Contenu | Toujours créé ? |
|---|---|---|
| `merchant` | La fiche marchand elle-même | Oui |
| `subscriptions` | Abonnement lié au `package_id` fourni, `stripe_subscription_id = ''` | Oui |
| `qrcodes` | 2 lignes (menu standard + menu-only/mywelloresto) | Oui |
| `scannorder_settings` | Ligne par défaut, champs SEO vides | Oui |
| `merchant_parameters` | Ligne par défaut, `last_menu_update` = horodatage courant | Oui |
| `merchant_marketing_settings` | Ligne par défaut | Oui |
| `haccp_settings` | Ligne par défaut, `created_at`/`updated_at` = horodatage courant | Oui |
| `bookings_settings` | Ligne par défaut, `code = ''` | Oui |
| `cash_desks` | Une caisse nommée `'Caisse principale'` | Oui |
| `roles` | 2 rôles système (« admin », « staff ») avec permissions de base | Oui (`EnsureSystemRoles`) |
| `role_permissions` | Permissions de base attachées à ces 2 rôles | Oui |
| `merchant.default_role_id` | Pointé vers le rôle « admin » nouvellement créé | Oui |
| `users_rights` | Lien `req.UserID` ↔ marchand, `role_id` = admin (ou rôle passé), `admin = req.Admin`, `enabled = TRUE` | **Seulement si `req.UserID` non vide** |

**Jamais créés automatiquement** : `hours_of_operation` (aucune ligne, §7.1/7.4), périodes de « vacances » (aucune ligne), `merchant.vat_number` (reste `NULL`), `merchant.timezone` (reste au défaut SQL `'Europe/Paris'`, jamais recalculé selon le pays réel), premier compte `users` (le payload ne contient aucun champ nom/mot de passe/email d'un futur admin — seulement un `user_id` optionnel référençant un `users` **déjà existant** ; si omis, le marchand est créé sans aucun utilisateur lié).

**État du chantier RBAC en production** : le `CLAUDE.md` du dépôt confirme (2026-09-01) que **PostgreSQL est le seul moteur de base de données en production** — la bascule MySQL→Postgres documentée sous `docs/migration-postgres/` est terminée, MySQL n'est plus live nulle part. Le commentaire `cmd/api/main.go:25` (« DB (MySQL par défaut, Postgres si DB_DIALECT=postgres — migration en cours) ») est désormais **un commentaire obsolète dans le code**, non représentatif du déploiement réel.

L'application effective des migrations RBAC (`094`-`099`, catalogue de permissions, `roles`, `role_permissions`, `default_role_id`) **en production** reste, à la date de cet audit (2026-09-08), non confirmée par preuve directe dans le dépôt : `docs/DEPLOIEMENT_PROD.md` (2026-09-07, qui remplace et archive `docs/RBAC_DEPLOIEMENT_PROD.md`) est un **runbook de déploiement** — une procédure à exécuter, outillée par `cmd/diagnose_migrations` (lecture seule) — et non un rapport d'exécution. Aucun fichier du dépôt ne documente son passage effectif sur la base de production à ce jour. Le dernier état constaté avec preuve (`docs/migration-postgres/67-migration-status-audit.md`) indique explicitement que le sous-ensemble RBAC (`094-100`, `103a`, `110`) « n'y est jamais passé », d'après `docs/RBAC_DEPLOIEMENT_PROD.md`, et que le reste du périmètre (`087`, `101`-`109`, `111`, `114`-`116`) est « INDÉTERMINABLE sans accès direct ». Ce fait est rapporté tel qu'il figure dans la documentation du dépôt à la date de cet audit, sans accès en direct à la base de production (hors du périmètre de cette vérification en lecture seule).

Fait notable indépendant de la question du déploiement en production : la réconciliation des rôles système n'est pas qu'un geste ponctuel (`cmd/seed_system_roles`) mais tourne aussi en tâche de fond `@hourly` (`cmd/api/tasks.go:81`, `ReconcileSystemRolePermissions`) — ce mécanisme réduit le risque qu'un établissement reste durablement avec un rôle admin incomplet une fois le socle RBAC effectivement en place.

---

## Annexe — Portée et limites de cet audit

- **Bases de données consultées** : introspection live du Postgres de **staging** (`RENDER_STAGING_DATABASE_URL`) le 2026-09-08 — schéma (`information_schema`, `pg_constraint`, `pg_indexes`) et, ponctuellement, des comptages/échantillons de données réelles (ex. répartition des `package_id` dans `subscriptions`, nombre de lignes dans `subscription_invoices`/`welloresto_stripe_customers`, ratio `role_id` renseigné dans `users_rights`). Aucun accès direct à la base de **production** n'a été utilisé — quand un fait dépend spécifiquement de l'état de production (ex. l'application effective des migrations RBAC), le document le signale explicitement et cite sa source documentaire plutôt que d'affirmer un état vérifié en direct.
- **Dépôts consultés** : `ib-welloresto-api` (API Go, branche `main`), `wello-back-office` (back-office React/TS), `wello_resto_flutter` (POS Flutter), `wello-kiosk` (borne Flutter). `wello-resto-scannorder` (front client scan&order) n'a été consulté que ponctuellement, pour des vérifications croisées.
- **Méthode** : lecture de code et requêtes SQL en lecture seule exclusivement — aucune modification de code, aucune migration, aucune donnée modifiée. Chaque affirmation factuelle de ce document est sourcée par un chemin de fichier et, autant que possible, un numéro de ligne ; « n'existe pas » signifie qu'une recherche exhaustive (grep multi-mots-clés, revue de la liste complète des tables/modules) n'a donné aucun résultat pertinent au moment de la rédaction.
- **Ce document remplace intégralement** la version précédente de `docs/audit-parcours-onboarding.md` (committée le 2026-08-28), dont la prémisse méthodologique (MySQL comme moteur de production réel, Postgres comme environnement de recette de bascule) est devenue caduque après la confirmation, le 2026-09-01, que la bascule Postgres est terminée en production. Les constats structurels de fond de cette version antérieure (schéma des tables, absence de JWT, absence d'auth externe, etc.) se sont pour l'essentiel confirmés à l'identique lors de cette nouvelle passe — les écarts significatifs entre les deux versions sont signalés explicitement dans le corps du texte ci-dessus (notamment : un troisième point de clôture de commande sans hash en §4.1.4, un gap dans la liste d'exclusion fiscale en §4.6, et la confirmation que `POST /pos/create` est aujourd'hui un vrai bouton self-service exploité en production, en §8.3).
- **Aucune proposition de solution** n'a été formulée nulle part dans ce document, conformément à la consigne — chaque constat s'arrête à la description de l'état actuel.
