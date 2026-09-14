package middleware

import "net/http"

// RequirePlatformAdmin gates the internal, cross-tenant /v1/admin/*
// endpoints (subscription_overrides, LOT B B1d) behind users.is_platform_staff
// (migration 138) — deliberately NOT permission.Key-based like
// RequirePermission, since these routes act on a merchant the caller isn't
// necessarily a member of (merchant scoping comes from the {id} path param,
// not from the caller's own token). users.is_platform_staff is orthogonal to
// every merchant-scoped RBAC right; a merchant admin (even one with
// settings.manage) is not a platform admin, and vice versa.
func RequirePlatformAdmin(next http.Handler) http.Handler {
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

		if !user.IsPlatformStaff {
			renderError(w, r, "access_denied", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
