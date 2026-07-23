# AUDIT - Complement Phase 0: preuves concretes API vs Back-office

Contrainte appliquee: lecture seule des depots, aucune migration/refactor/patch metier.

## 1) Preuves SQL exactes

### 1.1 DDL `employees` (migration source planning)
Source: `migrations/done/014_planning_socle.sql`

```sql
CREATE TABLE IF NOT EXISTS employees (
  id VARCHAR(64) NOT NULL,
  merchant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NULL,
  first_name VARCHAR(150) NOT NULL,
  last_name VARCHAR(150) NOT NULL,
  position VARCHAR(150) NOT NULL,
  job_title VARCHAR(150) NULL,
  email VARCHAR(255) NULL,
  phone VARCHAR(64) NULL,
  role ENUM('employee','manager','admin') NOT NULL DEFAULT 'employee',
  contract_type_code VARCHAR(32) NOT NULL,
  contract_start_date DATE NULL,
  contract_end_date DATE NULL,
  probation_end_date DATE NULL,
  last_medical_checkup_date DATE NULL,
  contract_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00,
  max_weekly_hours DECIMAL(5,2) NOT NULL DEFAULT 35.00,
  required_rest_days INT NOT NULL DEFAULT 2,
  sunday_premium TINYINT(1) NOT NULL DEFAULT 0,
  night_premium TINYINT(1) NOT NULL DEFAULT 0,
  hourly_rate BIGINT NOT NULL DEFAULT 0,
  gross_monthly_salary BIGINT NOT NULL DEFAULT 0,
  employer_charges_pct DECIMAL(5,2) NOT NULL DEFAULT 45.00,
  transport_cost BIGINT NOT NULL DEFAULT 0,
  birth_date DATE NULL,
  gender VARCHAR(32) NULL,
  nationality VARCHAR(80) NULL,
  address VARCHAR(255) NULL,
  hr_comment TEXT NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  deleted_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_employees_merchant_user (merchant_id, user_id),
  KEY idx_employees_merchant_active (merchant_id, active),
  KEY idx_employees_merchant (merchant_id),
  KEY idx_employees_contract_type (contract_type_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

Point constatable: `user_id VARCHAR(64) NULL` (nullable).

### 1.2 DDL `users`
Source: `data-migration/migration_welloresto_data.sql` (extraction terminal, bloc exact)

```sql
CREATE TABLE `users` (
  `user_id` varchar(50) NOT NULL,
  `merchant_id` int(11) DEFAULT NULL,
  `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,  `first_name` varchar(40) NOT NULL COMMENT 'PrÃ©nom',
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
  `token` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL,  `terms_of_use_accepted` tinyint(1) NOT NULL DEFAULT 0,
  `creationDate` datetime NOT NULL DEFAULT current_timestamp(),
  `created_at` timestamp NULL DEFAULT current_timestamp(),
  `lastAccess` datetime DEFAULT NULL COMMENT 'can be deleted (29/05/2026)',
  `last_activity` timestamp NOT NULL DEFAULT current_timestamp(),
  `enabled` int(11) NOT NULL DEFAULT 1,
  `last_login_at` timestamp NULL DEFAULT NULL,
  `last_position_at` datetime DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb3 COLLATE=utf8mb3_bin;
```

### 1.3 DDL table de liaison link/unlink
Source: `data-migration/migration_welloresto_data.sql`

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

## 2) Preuve du endpoint de creation "fiche employe seule"

### 2.1 Endpoint exact + handler
Source route: `cmd/api/routes.go`

```go
r.Get("/employees", planningH.ListEmployees)
r.Post("/employees", planningH.CreateEmployee)
r.Get("/employees/{id}", planningH.GetEmployee)
r.Patch("/employees/{id}", planningH.UpdateEmployee)
r.Delete("/employees/{id}", planningH.DeleteEmployee)
r.Post("/employees/{id}/user-link", planningH.LinkEmployeeUser)
r.Delete("/employees/{id}/user-link", planningH.UnlinkEmployeeUser)
```

Source handler: `internal/modules/planning/employees/handler.go`

```go
func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	var req EmployeeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "create_employee", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.CreateEmployee(r.Context(), req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "create_employee", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "planning", "create_employee", map[string]interface{}{"status": "success", "employee": item})
}
```

### 2.2 Preuve service: `user_id` optionnel
Source: `internal/modules/planning/employees/models.go`

```go
type EmployeeCreateRequest struct {
	UserID                 *string    `json:"user_id,omitempty"`
	MemberID               *string    `json:"-"`
	FirstName              string     `json:"first_name"`
	LastName               string     `json:"last_name"`
	PositionID             string     `json:"position_id"`
	...
}
```

Source: `internal/modules/planning/employees/service.go`

```go
func (s *Service) CreateEmployee(ctx context.Context, req EmployeeCreateRequest) (*Employee, error) {
	...
	if req.UserID != nil && strings.TrimSpace(*req.UserID) == "" {
		return nil, models.ErrPlanningEmployeeUserLinkInvalid
	}
	if req.UserID != nil {
		normalizedUserID := strings.TrimSpace(*req.UserID)
		if err := s.validateEmployeeUserLink(ctx, user.MerchantID, normalizedUserID, ""); err != nil {
			return nil, err
		}
		req.UserID = &normalizedUserID
	}
	...
	return s.repo.CreateEmployee(ctx, user.MerchantID, req)
}
```

### 2.3 Preuve repository: insertion avec `user_id` nullable
Source: `internal/modules/planning/employees/repository.go`

```go
func (r *Repository) CreateEmployee(ctx context.Context, merchantID string, req EmployeeCreateRequest) (*Employee, error) {
	...
	employee := Employee{
		ID:         id,
		MerchantID: merchantID,
		UserID:     req.UserID,
		FirstName:  strings.TrimSpace(req.FirstName),
		LastName:   strings.TrimSpace(req.LastName),
		...
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO employees (
			id, merchant_id, user_id, first_name, last_name, position_id, position_note, job_title, email, phone, role,
			contract_type_code, contract_start_date, contract_end_date, probation_end_date, last_medical_checkup_date,
			contract_hours, max_weekly_hours, required_rest_days, sunday_premium, night_premium,
			hourly_rate, gross_monthly_salary, employer_charges_pct, transport_cost, birth_date, gender, nationality,
			address, hr_comment, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, employee.ID, employee.MerchantID, employee.UserID, employee.FirstName, employee.LastName, ...)
	...
}
```

### 2.4 Endpoint cree sans `user_id` ?
Preuve SQL + modele + service ci-dessus: oui, si `user_id` absent, la validation lien n'est pas executee et l'insert passe `NULL`.

### 2.5 Est-ce appele par test/script/admin/back-office ?
- Test API present sur la couche repository:
  - `internal/modules/planning/employees/postgres_integration_test.go`
  - extrait localise: `emp, err := repo.CreateEmployee(ctx, merchantID, EmployeeCreateRequest{ ... })`
- Test handler/service explicites pour link/unlink present:
  - `internal/modules/planning/employees/user_link_test.go`
- Back-office React: appel present a `planningEmployeesApi.create(...)` dans `src/components/team/tabs/DocumentsTab.tsx` (bouton "Creer une fiche planning").
- Aucun script admin CLI dedie non ambigu trouve dans les extraits inspectes.

## 3) Verification du flow UI back-office (CreateMemberSheet)

Fichier lu en entier: `wello-back-office/src/components/team/CreateMemberSheet.tsx`.

### 3.1 Ce qui se passe quand on clique "Ajouter un membre"
Depuis `src/pages/equipe/EquipePage.tsx`, le bouton ouvre `CreateMemberSheet`.

Dans `CreateMemberSheet.tsx`:

```tsx
const [mode, setMode] = useState<"create" | "link">("create");

useEffect(() => {
  if (open) setMode("create");
}, [open]);
```

Donc ouverture par defaut sur l'onglet `create`.

### 3.2 Appels API exacts et ordre (onglet "Nouveau membre")

```tsx
const mutation = useMutation({
  mutationFn: (payload: CreateUserRequest) => usersApi.create(payload),
  ...
});

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
```

Ordre observe dans ce composant pour "Nouveau membre":
1. Construction d'un unique payload.
2. Un seul appel: `usersApi.create(payload)` -> `POST /users`.
3. Aucun `planningEmployeesApi.create` dans ce flux.

### 3.3 Appels API exacts et ordre (onglet "Lier un existant")

```tsx
const { data: results = [], isFetching } = useQuery({
  queryKey: qk.users.linkableSearch(debounced),
  queryFn: () => usersApi.linkableSearch(debounced),
  enabled: debounced.length >= 2,
});

await usersApi.merchantLink(user.user_id, {
  rights: {
    admin: false,
    login_enabled: true,
    permissions: { ... }
  }
});
```

Ordre observe:
1. Recherche via `GET /users/linkable-search`.
2. Au clic `Lier`: `POST /users/{id}/merchant-link`.

### 3.4 Y a-t-il un choix UI "juste fiche" vs "fiche + compte" ?
Dans `CreateMemberSheet`, non: les 2 choix sont "Nouveau membre" (creation compte) ou "Lier un existant" (liaison compte existant).

### 3.5 Endpoint "employe seul" accessible depuis UI actuelle ?
Oui, mais pas via `CreateMemberSheet`. Il est expose dans `DocumentsTab` si aucun employee lie:

```tsx
const createMutation = useMutation({
  mutationFn: () =>
    planningEmployeesApi.create({
      user_id: userId,
      first_name: firstName ?? "Nouvel",
      last_name: lastName ?? "Employe",
    }),
  ...
});
```

### 3.6 Ecart API vs BO sur payload `planning`
Cote BO (`CreateMemberSheet`), le payload de `usersApi.create` envoie une cle `planning`.

Cote API (`internal/modules/users/create_models.go`), `CreateUserRequest` ne declare pas de champ `planning`:

```go
type CreateUserRequest struct {
	FirstName  string                           `json:"first_name"`
	LastName   string                           `json:"last_name"`
	UserName   string                           `json:"username"`
	Email      string                           `json:"email"`
	Password   string                           `json:"password"`
	Tel        string                           `json:"tel"`
	MerchantID *string                          `json:"merchant_id,omitempty"`
	Admin      bool                             `json:"admin"`
	Rights     *MerchantUserRightsUpsertRequest `json:"rights,omitempty"`
}
```

## 4) Endpoints link/unlink

### 4.1 Code exact des endpoints
Source routes: `cmd/api/routes.go`

```go
r.With(middleware.RequirePermission(middleware.HasUserManagementAccess)).Post("/{id}/merchant-link", usersH.LinkMerchantUser)
...
r.With(middleware.RequirePermission(middleware.IsAdmin)).Delete("/{id}/merchant-link", usersH.UnlinkMerchantUser)
...
r.Post("/employees/{id}/user-link", planningH.LinkEmployeeUser)
r.Delete("/employees/{id}/user-link", planningH.UnlinkEmployeeUser)
```

Source handlers planning: `internal/modules/planning/employees/handler.go`

```go
func (h *Handler) LinkEmployeeUser(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	var req EmployeeUserLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "planning", "link_employee_user", models.ErrInvalidRequestBody)
		return
	}
	item, err := h.svc.LinkEmployeeUser(r.Context(), employeeID, req)
	if err != nil {
		models.SendErrorJSON(w, "planning", "link_employee_user", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "link_employee_user", map[string]interface{}{"status": "success", "employee": item})
}

func (h *Handler) UnlinkEmployeeUser(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(chi.URLParam(r, "id"))
	item, err := h.svc.UnlinkEmployeeUser(r.Context(), employeeID)
	if err != nil {
		models.SendErrorJSON(w, "planning", "unlink_employee_user", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "planning", "unlink_employee_user", map[string]interface{}{"status": "success", "employee": item})
}
```

Source handlers users: `internal/modules/users/admin_handler.go`

```go
func (h *UsersHandler) LinkMerchantUser(w http.ResponseWriter, r *http.Request) {
	var req MerchantUserLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "users", "link_merchant", models.ErrInvalidRequestBody)
		return
	}
	rights, err := h.svc.LinkMerchantUser(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		models.SendErrorJSON(w, "users", "link_merchant", err)
		return
	}
	models.SendJSON(w, http.StatusCreated, "users", "link_merchant", map[string]interface{}{"status": "success", "rights": rights})
}

func (h *UsersHandler) UnlinkMerchantUser(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.UnlinkMerchantUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		models.SendErrorJSON(w, "users", "unlink_merchant", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "users", "unlink_merchant", map[string]interface{}{"status": "success", "result": result})
}
```

Source service/repository users unlink: `internal/modules/users/admin_service.go`, `internal/modules/users/admin_repository.go`

```go
func (s *UsersService) UnlinkMerchantUser(ctx context.Context, userID string) (*MerchantUserUnlinkResult, error) {
	...
	err = dbutils.RunInTx(ctx, s.userRepo.database, func(txCtx context.Context) error {
		cleared, clearErr := s.userRepo.ClearMerchantEmployeeLinks(txCtx, currentUser.MerchantID, userID)
		if clearErr != nil {
			return clearErr
		}
		result.EmployeeLinksCleared = cleared
		unlinked, unlinkErr := s.userRepo.DisableMerchantUserLink(txCtx, currentUser.MerchantID, userID)
		if unlinkErr != nil {
			return unlinkErr
		}
		result.Unlinked = unlinked
		return nil
	})
	...
}
```

```go
func (r *UsersRepository) ClearMerchantEmployeeLinks(ctx context.Context, merchantID, userID string) (int, error) {
	...
	res, err := db.ExecContext(ctx, `
		UPDATE employees
		SET user_id = NULL
		WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
	`, merchantID, strings.TrimSpace(userID))
	...
}

func (r *UsersRepository) DisableMerchantUserLink(ctx context.Context, merchantID, userID string) (bool, error) {
	...
	_, err := db.ExecContext(ctx, `
		UPDATE users_rights
		SET enabled = FALSE
		WHERE merchant_id = ? AND user_id = ? AND enabled = TRUE
	`, merchantID, strings.TrimSpace(userID))
	...
}
```

### 4.2 Exposition back-office
- Expose en BO:
  - `usersApi.merchantLink(...)` depuis `CreateMemberSheet.tsx` (onglet "Lier un existant").
  - `usersApi.deleteMerchantLink(...)` depuis `tabs/SecurityTab.tsx`.
- Non expose en BO (dans les sources inspectees):
  - `planningEmployeesApi.userLink(...)`
  - `planningEmployeesApi.deleteUserLink(...)`
  Ces methodes existent dans `src/services/welloApi.ts` mais aucune invocation trouvee dans `src`.

## 5) Statuts et flags

### 5.1 `enabled`
- Table `users`: `enabled` est `int(11) NOT NULL DEFAULT 1` (DDL ci-dessus).
- Table `users_rights`: `enabled` est `tinyint(1) NOT NULL DEFAULT 1`.
- Valeurs observees dans le code: 0/1 (false/true logique).

Extrait status calcule BO (mapping explicite): `internal/modules/users/admin_repository.go`

```go
func userStatus(item *MerchantUserListItem) string {
	if item.Enabled && item.LoginEnabled {
		return "active"
	} else if item.Enabled && !item.LoginEnabled {
		return "login_disabled"
	}
	return "disabled"
}
```

### 5.2 `login_enabled`
- Table `users_rights`: `login_enabled` est `tinyint(1) NOT NULL DEFAULT 1`.
- Utilise comme garde d'acces dans auth:

```go
WHERE ur.merchant_id = ? AND ur.pin_hash = ? AND ur.enabled = true AND ur.login_enabled = true
```
(Source: `internal/modules/auth/repository.go`)

### 5.3 `mfa_status`
- Table `users`: `mfa_status` est `varchar(25) DEFAULT NULL`.
- Valeurs explicites dans le code:

```go
const (
	MFAStatusPending  = "pending"
	MFAStatusVerified = "verified"
)
```
(Source: `internal/models/mfa_models.go`)

- Ecriture en base:

```go
query := `UPDATE users SET mfa_status = ? WHERE user_id = ?`
```

### 5.4 Combination flags + `user_id NULL` pour "en attente de compte"
Constat factuel sur les sources lues:
- `employees.user_id` peut etre `NULL`.
- `enabled`/`login_enabled` s'appliquent au compte/lien (`users`, `users_rights`), pas a un etat metier explicite de fiche employee "en attente de compte".
- `mfa_status` concerne la verification MFA du compte utilisateur, pas la presence/absence de lien employee.
- Aucun enum/statut explicite "invited"/"pending_account" trouve dans ces tables/extraits.
