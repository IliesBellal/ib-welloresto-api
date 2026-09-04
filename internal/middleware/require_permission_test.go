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

// newUsersRouteRouter mirrors how cmd/api/routes.go wires the two routes RBAC
// lot 11 phase 4 moved off RequireAdmin: RequirePermission(permission.StaffManage)
// applied via .With(...) on a single route, terminal handler just answers 200.
func newUsersRouteRouter(method, path string) chi.Router {
	r := chi.NewRouter()
	handler := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	guarded := r.With(RequirePermission(permission.StaffManage))
	switch method {
	case http.MethodPost:
		guarded.Post(path, handler)
	case http.MethodDelete:
		guarded.Delete(path, handler)
	}
	return r
}

func doUsersRouteRequest(r chi.Router, method, path string, user *auth.UserLoginRow) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(WithUser(req.Context(), user))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

// TestRequirePermission_ForceResetPasswordAndMerchantLinkDelete_MovedOffRequireAdmin
// is the RBAC lot 11 phase 4 test the spec calls for: POST
// /users/{id}/force-reset-password and DELETE /users/{id}/merchant-link, both
// migrated from middleware.RequireAdmin() (removed) to
// RequirePermission(permission.StaffManage), are denied without staff.manage
// and granted with it — exactly like every other staff.manage route, proven
// here rather than just asserted by the static source scan in
// cmd/api/routes_rbac_coverage_test.go.
func TestRequirePermission_ForceResetPasswordAndMerchantLinkDelete_MovedOffRequireAdmin(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		routePath   string // as declared in cmd/api/routes.go
		requestPath string // concrete path to send the test request to
	}{
		{"POST /users/{id}/force-reset-password", http.MethodPost, "/users/{id}/force-reset-password", "/users/u-2/force-reset-password"},
		{"DELETE /users/{id}/merchant-link", http.MethodDelete, "/users/{id}/merchant-link", "/users/u-2/merchant-link"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/denied without staff.manage", func(t *testing.T) {
			r := newUsersRouteRouter(tc.method, tc.routePath)
			user := &auth.UserLoginRow{UserID: "u-1", MerchantID: "m-1"} // RoleID nil, every boolean false
			rr := doUsersRouteRequest(r, tc.method, tc.requestPath, user)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for a user without staff.manage, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})

		t.Run(tc.name+"/granted with staff.manage", func(t *testing.T) {
			r := newUsersRouteRouter(tc.method, tc.routePath)
			roleID := "role-1"
			user := &auth.UserLoginRow{
				UserID: "u-1", MerchantID: "m-1",
				RoleID: &roleID, Permissions: []string{string(permission.StaffManage)},
			}
			rr := doUsersRouteRequest(r, tc.method, tc.requestPath, user)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 for a user with staff.manage, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
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
