package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"welloresto-api/internal/modules/auth"
	"welloresto-api/internal/permission"

	"github.com/go-chi/chi/v5"
)

// newGuardedRouter mirrors exactly how cmd/api/routes.go wires
// PATCH /pos/status: RequirePermission applied via .With(...) on a single
// route, terminal handler just answers 200.
func newGuardedRouter(key permission.Key) chi.Router {
	r := chi.NewRouter()
	r.With(RequirePermission(key)).Patch("/pos/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

func doPatch(r chi.Router, user *auth.UserLoginRow) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/pos/status", nil)
	req = req.WithContext(WithUser(req.Context(), user))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// TestRequirePermission_POSStatusManage_DeniedWithoutIt is the test the RBAC
// lot 2 spec calls for verbatim: PATCH /pos/status returns 403 for a user who
// has neither the historical access_wrreception boolean nor a role granting
// pos.status.manage.
func TestRequirePermission_POSStatusManage_DeniedWithoutIt(t *testing.T) {
	r := newGuardedRouter(permission.POSStatusManage)

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"} // RoleID nil, every boolean false
	rr := doPatch(r, user)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a user without pos.status.manage, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestRequirePermission_POSStatusManage_GrantedByHistoricalBoolean confirms
// the flip side isn't accidentally broken: a user with the historical
// access_wrreception=true boolean (and no role) still gets through, exactly
// as before this lot's route guard existed.
func TestRequirePermission_POSStatusManage_GrantedByHistoricalBoolean(t *testing.T) {
	r := newGuardedRouter(permission.POSStatusManage)

	user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"}
	user.Rights.AccessReception = true

	rr := doPatch(r, user)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for a user with access_wrreception=true, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestRequirePermission_POSStatusManage_GrantedByRole confirms the role-mode
// path too: a user with role_id set and pos.status.manage in Permissions gets
// through even with every historical boolean false.
func TestRequirePermission_POSStatusManage_GrantedByRole(t *testing.T) {
	r := newGuardedRouter(permission.POSStatusManage)

	roleID := "role-1"
	user := &auth.UserLoginRow{
		UserID:      "u-1",
		MerchantID:  "m-1",
		RoleID:      &roleID,
		Permissions: []string{string(permission.POSStatusManage)},
	}

	rr := doPatch(r, user)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for a user whose role grants pos.status.manage, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestRequirePermission_NoUser_Returns401 covers the case GetUser(r) finds
// nothing — should never happen behind authMiddleware in production, but
// RequirePermission must fail closed, not panic, if it ever does.
func TestRequirePermission_NoUser_Returns401(t *testing.T) {
	r := newGuardedRouter(permission.POSStatusManage)

	req := httptest.NewRequest(http.MethodPatch, "/pos/status", nil) // no WithUser
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no authenticated user, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}
