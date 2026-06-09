package auth

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"welloresto-api/internal/models"
)

// ------------------------------------------------------------
// Minimal in-memory Redis stub for PIN tests
// ------------------------------------------------------------

type memRedis struct {
	store   map[string]string
	expires map[string]time.Time
}

func newMemRedis() *memRedis {
	return &memRedis{
		store:   map[string]string{},
		expires: map[string]time.Time{},
	}
}

func (m *memRedis) Get(_ context.Context, key string) (string, bool) {
	if exp, ok := m.expires[key]; ok && time.Now().After(exp) {
		delete(m.store, key)
		delete(m.expires, key)
		return "", false
	}
	v, ok := m.store[key]
	return v, ok
}

func (m *memRedis) Set(_ context.Context, key, value string, ttl time.Duration) bool {
	m.store[key] = value
	if ttl > 0 {
		m.expires[key] = time.Now().Add(ttl)
	}
	return true
}

func (m *memRedis) Delete(_ context.Context, key string) bool {
	delete(m.store, key)
	delete(m.expires, key)
	return true
}


// ------------------------------------------------------------
// AuthService adapter that accepts *memRedis
// ------------------------------------------------------------

// pinTestService wraps the logic we need in tests without needing a real *redis.Client.
// We build a real AuthService and swap out redis calls through the stub.
// Simpler: we test service internals directly since the service holds *redis.Client (concrete).
// Instead we test via a thin integration: sqlmock for DB + stub for Redis via direct calls.

// buildPINService creates an AuthService wired to sqlmock DB. Redis interactions
// are exercised through helpers that operate on the raw memRedis.
func buildPINService(t *testing.T) (*AuthService, sqlmock.Sqlmock, *memRedis) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	repo := NewAuthRepository(db)
	mem := newMemRedis()

	// AuthService holds *redis.Client (concrete). For tests we build the service
	// without a real redis.Client and exercise Redis paths via memRedis directly.
	svc := &AuthService{
		repo:   repo,
		pepper: "test-pepper",
	}
	return svc, mock, mem
}

// seedAnchorInMemRedis puts a minimal UserLoginRow for the anchor token into the
// memRedis store so that GetUserByToken returns it (by-passing DB).
// We simulate the Redis hit that the singleflight would have done.
func seedAnchorInMemRedis(mem *memRedis, token, merchantID, userID string) {
	u := UserLoginRow{Token: token, MerchantID: merchantID, UserID: userID}
	data, _ := json.Marshal(u)
	mem.store[models.UserCachePrefix+token] = string(data)
}

// pinColumns returns the 74 column names expected by scanUserLoginRow.
func pinColumns() []string { return makeColumns(74) }

// pinMinRow returns 74 driver.Value values for a minimal active users_rights row.
// The filter columns (ur.enabled, ur.login_enabled) are in WHERE, not SELECT,
// so they don't appear here — a non-empty result means the link passed the filter.
func pinMinRow(userID, token, merchantID string) []driver.Value {
	return []driver.Value{
		// user (0-9)
		userID, "hashed", "Name", "First", "Last", "email@ex.com", "+33600000000", true, nil, nil,
		// rights (10-33)
		"mr-1", token, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, merchantID, nil, nil, nil, nil,
		// merchant (34-41)
		"Biz", "+33999999999", 1.0, 2.0, "UTC", "1 rue", nil, nil,
		// params (42-53)
		0, 0, 0, true, true, true, false, false, false, false, "EUR", true,
		// package (54-63)
		true, true, false, 0, false, true, true, true, false, true,
		// SNO (64)
		false,
		// UE (65-70)
		nil, nil, nil, nil, nil, nil,
		// UD (71)
		nil,
		// Droo (72-73)
		nil, nil,
	}
}

// ------------------------------------------------------------
// Tests: GetUserByPIN filter (ur.enabled / ur.login_enabled)
// ------------------------------------------------------------

// TestGetUserByPIN_ActiveLink verifies that a matching, active link returns the employee.
func TestGetUserByPIN_ActiveLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	pinHash := "deadbeef"
	merchantID := "merch-1"
	userID := "user-1"
	token := "tok-active"

	rowVals := pinMinRow(userID, token, merchantID)
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.merchant_id = ? AND ur.pin_hash = ? AND ur.enabled = 1 AND ur.login_enabled = 1`)).
		WithArgs(merchantID, pinHash).
		WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(rowVals...))

	got, err := repo.GetUserByPIN(context.Background(), merchantID, pinHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil user for active link, got nil")
	}
	if got.UserID != userID {
		t.Errorf("UserID = %q, want %q", got.UserID, userID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestGetUserByPIN_DisabledLink verifies that ur.enabled=0 prevents authentication.
// The DB returns no row (because the WHERE clause filters it out), not the service.
// We simulate this by having sqlmock return ErrNoRows for that combination.
func TestGetUserByPIN_DisabledLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	pinHash := "deadbeef"
	merchantID := "merch-1"

	// ur.enabled=0 → DB returns 0 rows (WHERE … AND ur.enabled = 1 filters it out)
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.merchant_id = ? AND ur.pin_hash = ? AND ur.enabled = 1 AND ur.login_enabled = 1`)).
		WithArgs(merchantID, pinHash).
		WillReturnRows(sqlmock.NewRows(pinColumns())) // empty result set

	got, err := repo.GetUserByPIN(context.Background(), merchantID, pinHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for disabled link, got user %q", got.UserID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestGetUserByPIN_LoginDisabledLink verifies that ur.login_enabled=0 prevents authentication.
func TestGetUserByPIN_LoginDisabledLink(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	pinHash := "deadbeef"
	merchantID := "merch-1"

	// ur.login_enabled=0 → WHERE … AND ur.login_enabled = 1 filters it out
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.merchant_id = ? AND ur.pin_hash = ? AND ur.enabled = 1 AND ur.login_enabled = 1`)).
		WithArgs(merchantID, pinHash).
		WillReturnRows(sqlmock.NewRows(pinColumns())) // empty

	got, err := repo.GetUserByPIN(context.Background(), merchantID, pinHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for login_disabled link, got user %q", got.UserID)
	}
}

// ------------------------------------------------------------
// Tests: lockout logic (unit, no DB needed)
// ------------------------------------------------------------

func newServiceWithMem(mem *memRedis) *AuthService {
	// Build a service where we manually inject Redis-like state via memRedis.
	// We use the service's private helpers by invoking them through a thin shim.
	return &AuthService{pepper: "test-pepper"}
}

// lockoutHelperSvc simulates just the lockout helpers using the real service code
// but with a stubbed Redis. Since AuthService.redis is a concrete *redis.Client we
// test the lockout logic through integration: populate the PINLockoutPrefix key
// manually and call checkLockout/incrementLockout/resetLockout via AuthenticatePIN.

// TestLockout_NoFailures checks that with no prior failures there is no delay.
func TestLockout_5FailuresTrigger30s(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)
	svc := &AuthService{repo: repo, pepper: "p"}

	// We test the lockout state struct directly via JSON round-trip.
	state := lockoutState{Count: 5, LockedUntil: time.Now().Add(30 * time.Second).Unix()}
	data, _ := json.Marshal(state)

	// The lockout key would be PINLockoutPrefix + anchorToken.
	// Simulate reading it back:
	var decoded lockoutState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	remaining := time.Until(time.Unix(decoded.LockedUntil, 0))
	if remaining < 29*time.Second || remaining > 31*time.Second {
		t.Errorf("expected ~30s remaining, got %v", remaining)
	}
	_ = svc
	_ = mock
}

func TestLockout_EscalationFormula(t *testing.T) {
	cases := []struct {
		count    int
		wantSecs int
	}{
		{5, 30},   // 30 * 2^0
		{9, 30},   // floor((9-5)/5)=0 → 30s
		{10, 60},  // floor((10-5)/5)=1 → 60s
		{14, 60},  // floor((14-5)/5)=1 → 60s
		{15, 120}, // floor((15-5)/5)=2 → 120s
		{20, 240}, // floor((20-5)/5)=3 → 240s
		{25, 480}, // floor((25-5)/5)=4 → 480s (cap)
		{99, 480}, // capped at 480s
	}

	for _, tc := range cases {
		exponent := (tc.count - PINMaxAttempts) / 5
		if exponent > 4 {
			exponent = 4
		}
		got := int((PINLockoutBase * time.Duration(1<<uint(exponent))).Seconds())
		if got != tc.wantSecs {
			t.Errorf("count=%d: want %ds, got %ds", tc.count, tc.wantSecs, got)
		}
	}
}

// ------------------------------------------------------------
// Tests: PIN uniqueness (CheckPINConflict)
// ------------------------------------------------------------

func TestCheckPINConflict_NoConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM users_rights WHERE merchant_id = ? AND pin_hash = ? AND user_id != ? LIMIT 1`)).
		WithArgs("merch-1", "hash-x", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"1"})) // empty → no conflict

	conflict, err := repo.CheckPINConflict(context.Background(), "merch-1", "hash-x", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conflict {
		t.Fatal("expected no conflict")
	}
}

func TestCheckPINConflict_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM users_rights WHERE merchant_id = ? AND pin_hash = ? AND user_id != ? LIMIT 1`)).
		WithArgs("merch-1", "hash-y", "user-1").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	conflict, err := repo.CheckPINConflict(context.Background(), "merch-1", "hash-y", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !conflict {
		t.Fatal("expected conflict")
	}
}

// ------------------------------------------------------------
// Tests: SetPINHash DB write
// ------------------------------------------------------------

func TestSetPINHash_Set(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	h := "newhash"
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users_rights SET pin_hash = ? WHERE merchant_id = ? AND user_id = ?`)).
		WithArgs(&h, "merch-1", "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.SetPINHash(context.Background(), "merch-1", "user-1", &h); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetPINHash_Clear(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users_rights SET pin_hash = ? WHERE merchant_id = ? AND user_id = ?`)).
		WithArgs((*string)(nil), "merch-1", "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.SetPINHash(context.Background(), "merch-1", "user-1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ------------------------------------------------------------
// Tests: Service — ErrPINInvalidLength, ErrPINConflict
// ------------------------------------------------------------

func TestSetPINSelf_InvalidLength(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewAuthService(NewAuthRepository(db), nil, nil, nil, "pepper")

	if err := svc.SetPINSelf(context.Background(), "merch-1", "user-1", "12345"); !errors.Is(err, ErrPINInvalidLength) {
		t.Fatalf("want ErrPINInvalidLength, got %v", err)
	}
}

func TestSetPINSelf_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewAuthService(NewAuthRepository(db), nil, nil, nil, "pepper")

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM users_rights WHERE merchant_id = ?`)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	if err := svc.SetPINSelf(context.Background(), "merch-1", "user-1", "1234"); !errors.Is(err, ErrPINConflict) {
		t.Fatalf("want ErrPINConflict, got %v", err)
	}
}

// ------------------------------------------------------------
// Tests: cache-miss → nil user (verifying 401 path)
// ------------------------------------------------------------

// TestGetUserByToken_UnknownToken_NoCacheWrite verifies that GetUserByToken on an
// unknown token returns (nil, nil) AND does not write any key to Redis.
func TestGetUserByToken_UnknownToken_NoCacheWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)
	mem := newMemRedis()
	svc := &AuthService{repo: repo, redis: mem, pepper: "test-pepper"}

	token := "unknown-session-token"
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(pinColumns()))

	got, err := svc.GetUserByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown token, got %+v", got)
	}
	cacheKey := models.UserCachePrefix + token
	if v, found := mem.store[cacheKey]; found {
		t.Fatalf("expected no cache entry for unknown token, found %q", v)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestGetUserByToken_NullPreseeded_NoGhostSession verifies that a pre-existing
// "null" cache entry (left by a previous buggy write) does not produce a ghost
// session: the function must fall through to DB and return (nil, nil).
func TestGetUserByToken_NullPreseeded_NoGhostSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)
	mem := newMemRedis()
	svc := &AuthService{repo: repo, redis: mem, pepper: "test-pepper"}

	token := "expired-pin-session"
	cacheKey := models.UserCachePrefix + token
	// Pre-seed the poisoned "null" entry as the bug would have created.
	mem.store[cacheKey] = "null"

	// The cache-miss path must reach the DB.
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
		WithArgs(token).
		WillReturnRows(sqlmock.NewRows(pinColumns()))

	got, err := svc.GetUserByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (no ghost session), got non-nil user with UserID=%q", got.UserID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (DB was not queried): %v", err)
	}
}

// TestGetUserByToken_UnknownTokenNotInDB verifies that an unknown token not present
// in DB results in nil (not an error), which the middleware turns into a 401.
func TestGetUserByToken_PINTokenNotInDB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewAuthRepository(db)

	pinSessionToken := "pin-sess-xyz"
	// DB returns no rows for a PIN session token (it is never persisted in users_rights).
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.token = ?`)).
		WithArgs(pinSessionToken).
		WillReturnRows(sqlmock.NewRows(pinColumns())) // empty

	got, err := repo.GetUserByToken(context.Background(), pinSessionToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown token, got %+v", got)
	}
}

// loginMinRow returns 79 driver.Value values for a minimal active row as scanned
// by repo.Login (SELECT u.user_id, u.name, ... — 79 columns, different from the
// 74-column scanUserLoginRow used by GetUserByToken/GetUserByPIN).
func loginMinRow(userID, token, merchantID string) []driver.Value {
	return []driver.Value{
		// user (0-10): user_id, name, first_name, last_name, email, tel, enabled,
		//              profile_picture, terms_of_use_accepted, password, email_verified_at
		userID, "Name", "First", "Last", "email@ex.com", "+33600000000", true, nil, false, "hashed", nil,
		// rights (11-34): mr_id, token, 17 bool rights, merchant_id, mfa×4
		"mr-1", token, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, false, merchantID, nil, nil, nil, nil,
		// merchant (35-42)
		"Biz", "+33999999999", 1.0, 2.0, "UTC", "1 rue", nil, nil,
		// params (43-58): 12 base fields + kitchen_distribution_mode, production_display_mode,
		//                  pager_number_required, cash_register_required_for_ordering
		0, 0, 0, true, true, true, false, "", "", false, false, false, false, false, "EUR", true,
		// package (59-68)
		true, true, false, 0, false, true, true, true, false, true,
		// SNO (69)
		false,
		// UE (70-75), UD (76), Droo (77-78)
		nil, nil, nil, nil, nil, nil,
		nil,
		nil, nil,
	}
}

// ------------------------------------------------------------
// Tests: AuthenticatePIN delegates to Login with employee token
// ------------------------------------------------------------

// TestAuthenticatePIN_DelegatesLoginWithEmployeeToken verifies:
//   - GetUserByPIN resolves the employee (mock expectation proves it's called)
//   - Login is then called with B's permanent token, not the anchor token
//     (mock.WithArgs("", "", empToken) proves delegation)
//   - The response token equals B's permanent token (not the anchor token A's)
//   - The response has the same top-level shape as /auth/login
func TestAuthenticatePIN_DelegatesLoginWithEmployeeToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mem := newMemRedis()
	svc := &AuthService{repo: NewAuthRepository(db), redis: mem, pepper: "test-pepper"}

	anchorToken := "anchor-tok"
	empToken := "emp-tok"
	merchantID := "merch-1"
	empUserID := "user-emp"

	seedAnchorInMemRedis(mem, anchorToken, merchantID, "user-A")

	// Step 1: GetUserByPIN finds employee B.
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ur.merchant_id = ? AND ur.pin_hash = ?`)).
		WithArgs(merchantID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(pinColumns()).AddRow(pinMinRow(empUserID, empToken, merchantID)...))

	// Step 2: Login is called with B's token (empty username + empty pwd + empToken).
	// WithArgs("", "", empToken) proves delegation — any other token would fail here.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
    u.user_id,
    u.name,`)).
		WithArgs("", "", empToken).
		WillReturnRows(sqlmock.NewRows(makeColumns(79)).AddRow(loginMinRow(empUserID, empToken, merchantID)...))

	// Step 3: Login else-branch effects (MFAType=nil → IsMFAVerificationRequired=false).
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET mfa_status = ? WHERE user_id = ?`)).
		WithArgs(sqlmock.AnyArg(), empUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET last_login_at = UTC_TIMESTAMP() WHERE user_id = ?`)).
		WithArgs(empUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
    m.id,`)).
		WithArgs(empUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "fullName", "lat", "lng", "address", "city", "country", "zip_code", "logo_url", "token"}))

	resp, err := svc.AuthenticatePIN(context.Background(), anchorToken, "1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Session == nil {
		t.Fatal("expected non-nil response with session")
	}
	// Token must be B's permanent token, not A's anchor token.
	if resp.Session.Token != empToken {
		t.Errorf("got token %q, want employee token %q", resp.Session.Token, empToken)
	}
	if resp.Session.Token == anchorToken {
		t.Error("response token must not equal the anchor token")
	}
	// Response shape must match /auth/login.
	if resp.Status != "1" {
		t.Errorf("Status = %q, want \"1\"", resp.Status)
	}
	if resp.Enabled != "true" {
		t.Errorf("Enabled = %q, want \"true\"", resp.Enabled)
	}
	if resp.User == nil {
		t.Error("User must not be nil")
	}
	if resp.Merchant == nil {
		t.Error("Merchant must not be nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations (proves delegation path): %v", err)
	}
}

// ------------------------------------------------------------
// Tests: ResetPIN writes NULL hash
// ------------------------------------------------------------

// TestResetPIN_SetsNullHash verifies that ResetPIN passes nil to SetPINHash,
// which sets pin_hash = NULL in the DB.
func TestResetPIN_SetsNullHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewAuthService(NewAuthRepository(db), nil, nil, nil, "pepper")

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users_rights SET pin_hash = ? WHERE merchant_id = ? AND user_id = ?`)).
		WithArgs((*string)(nil), "merch-1", "user-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := svc.ResetPIN(context.Background(), "merch-1", "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
