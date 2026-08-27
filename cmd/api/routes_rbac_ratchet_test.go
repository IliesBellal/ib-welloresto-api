package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestCountUnguardedMutativeRoutes exercises countUnguardedMutativeRoutes
// against small synthetic snippets, isolating the scanner's scoping rules
// from the real routes.go — so a change to the scanner itself is caught here
// first, rather than only showing up as a mysterious ceiling drift in
// TestRBACRatchet.
func TestCountUnguardedMutativeRoutes(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "bare mutative route under authMiddleware group is unguarded",
			src: `
				r.Route("/x", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Post("/create", h.Create)
				})
			`,
			want: 1,
		},
		{
			name: "RequirePermission on the group excludes its routes",
			src: `
				r.Route("/x", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Use(middleware.RequirePermission(permission.StaffManage))
					r.Post("/create", h.Create)
					r.Patch("/{id}", h.Update)
				})
			`,
			want: 0,
		},
		{
			name: "per-route .With(RequireAdmin) excludes just that route",
			src: `
				r.Route("/x", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Post("/create", h.Create)
					r.With(middleware.RequireAdmin()).Delete("/{id}", h.Delete)
				})
			`,
			want: 1,
		},
		{
			name: "no authMiddleware anywhere: public mutative route is not counted",
			src: `
				r.Route("/scannorder", func(r chi.Router) {
					r.Post("/orders", h.CreateOrder)
				})
			`,
			want: 0,
		},
		{
			name: "GET is never counted even when unguarded",
			src: `
				r.Route("/x", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Get("/list", h.List)
				})
			`,
			want: 0,
		},
		{
			name: "nested sub-route inherits the parent group's authMiddleware",
			src: `
				r.Route("/x", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Route("/y", func(r chi.Router) {
						r.Post("/create", h.Create)
					})
				})
			`,
			want: 1,
		},
		{
			name: "chi path parameter braces don't desync block scoping",
			src: `
				r.Route("/floors/{floor_id}/obstacles", func(r chi.Router) {
					r.Use(authMiddleware)
					r.Post("/", h.CreateObstacle)
				})
			`,
			want: 1,
		},
		{
			name: "combined r.With(authMiddleware, RequirePermission) excludes the route",
			src: `
				r.With(authMiddleware, middleware.RequirePermission(permission.StaffManage)).Post("/pin/reset", h.ResetPIN)
			`,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countUnguardedMutativeRoutes(tt.src)
			if got != tt.want {
				t.Errorf("countUnguardedMutativeRoutes() = %d, want %d\nsrc:%s", got, tt.want, tt.src)
			}
		})
	}
}

// unguardedMutativeRouteCeiling is the number of mutative (POST/PUT/PATCH/
// DELETE) routes declared under authMiddleware in cmd/api/routes.go that
// carry neither middleware.RequirePermission nor middleware.RequireAdmin, as
// measured on 2026-08-27 (RBAC lot 8, down from 222 at lot 2.5).
//
// This is not a target — it is the debt this repository already carries
// (menu outside /import, HACCP outside /traceability and /settings, orders
// outside reopen/refund, bookings, cash_register, customers outside /import
// and creation, delivery_sessions, integrations, kiosk device admin, stocks
// outside the component-movement endpoint, floors, locations, printers, ...
// — see docs/RBAC_ROUTES.md), frozen so it can only shrink from here.
// TestRBACRatchet fails if the count goes ABOVE this ceiling — a new
// mutative route added without a guard. It does NOT fail if the count drops
// below it (a route getting guarded is welcome); when that happens the test
// logs a reminder to lower this constant to match, so the ratchet keeps
// tightening instead of going slack in the other direction.
const unguardedMutativeRouteCeiling = 212

// TestRBACRatchet is the counting half of the guard TestRBACCoverage
// (routes_rbac_coverage_test.go) already established: that test pins the
// exact routes RBAC lot 2 decided to gate; this one pins how much of the API
// is left ungated, so the gap can only close, never quietly widen.
//
// Same technique as TestRBACCoverage and the same reason for it: source-text
// scanning, not a live chi.Mux crawl (SetupRoutes is production wiring with
// no seams for standalone construction in a unit test).
func TestRBACRatchet(t *testing.T) {
	src := readRoutesSource(t)
	got := countUnguardedMutativeRoutes(src)

	if got > unguardedMutativeRouteCeiling {
		t.Errorf("found %d unguarded mutative route(s) under authMiddleware, ceiling is %d — "+
			"a new POST/PUT/PATCH/DELETE route was added under authMiddleware without "+
			"middleware.RequirePermission or middleware.RequireAdmin. Either add a guard, "+
			"or if this route genuinely has none by design, raise unguardedMutativeRouteCeiling "+
			"in %s and say why in the commit message.",
			got, unguardedMutativeRouteCeiling, "cmd/api/routes_rbac_ratchet_test.go")
		return
	}
	if got < unguardedMutativeRouteCeiling {
		t.Logf("found %d unguarded mutative route(s) under authMiddleware, ceiling is %d — "+
			"the ratchet loosened. Lower unguardedMutativeRouteCeiling in %s to %d to keep the debt finite.",
			got, unguardedMutativeRouteCeiling, "cmd/api/routes_rbac_ratchet_test.go", got)
	}
}

// mutativeCallRe matches a mutative route registration — with or without a
// preceding .With(...) chain — capturing everything from the start of the
// chain (r.With(...) or bare r) up to the method call, so the caller can
// inspect that prefix for authMiddleware / RequirePermission / RequireAdmin
// without it being confused with the route's handler argument.
var mutativeCallRe = regexp.MustCompile(`\br(\.With\([^)]*\))?\.(Post|Put|Patch|Delete)\(`)

// useLineRe matches an r.Use(...) statement, capturing its argument list.
var useLineRe = regexp.MustCompile(`\br\.Use\(([^)]*)\)`)

// scope tracks, for one { ... } nesting level in routes.go, whether an
// r.Use(...) statement directly inside it wires authMiddleware and/or an RBAC
// guard (RequirePermission/RequireAdmin). Nested blocks inherit their
// ancestors' scopes (see countUnguardedMutativeRoutes) — the same way chi
// itself inherits r.Use middleware into sub-routes.
type scope struct {
	hasAuth bool
	hasRBAC bool
}

// countUnguardedMutativeRoutes walks routes.go's source text tracking brace
// depth (string-literal aware, so a chi path parameter like "{floor_id}"
// never miscounts as a real brace — same technique as extractBracedBlock in
// routes_rbac_coverage_test.go), and for every mutative route call decides:
//
//   - authGuard: true if either the route's own .With(...) chain mentions
//     authMiddleware, or any enclosing { ... } block had a direct
//     r.Use(authMiddleware) (or r.Use(authMiddleware, ...)) statement.
//   - rbacGuard: same, but for RequirePermission/RequireAdmin — via the
//     route's own .With(...) chain, or an enclosing block's
//     r.Use(middleware.RequirePermission(...)) / r.Use(middleware.RequireAdmin()).
//
// A route counts as unguarded when authGuard is true and rbacGuard is false.
// Routes never reached through authMiddleware at all (public routes,
// webhook signature checks, KioskAuth device routes, IP rate-limited /rsv)
// are not counted either way — this mirrors the task scope exactly: routes
// "sous authMiddleware".
func countUnguardedMutativeRoutes(src string) int {
	lines := strings.Split(src, "\n")

	var stack []scope
	push := func() { stack = append(stack, scope{}) }
	pop := func() {
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	markTop := func(auth, rbac bool) {
		if len(stack) == 0 {
			return
		}
		if auth {
			stack[len(stack)-1].hasAuth = true
		}
		if rbac {
			stack[len(stack)-1].hasRBAC = true
		}
	}
	inherited := func() scope {
		var s scope
		for _, fr := range stack {
			s.hasAuth = s.hasAuth || fr.hasAuth
			s.hasRBAC = s.hasRBAC || fr.hasRBAC
		}
		return s
	}

	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		if m := useLineRe.FindStringSubmatch(line); m != nil {
			args := m[1]
			markTop(strings.Contains(args, "authMiddleware"),
				strings.Contains(args, "RequirePermission") || strings.Contains(args, "RequireAdmin"))
		}

		if m := mutativeCallRe.FindStringSubmatchIndex(line); m != nil {
			prefix := line[:m[1]] // everything up to and including the matched ".With(...)?.Post(" etc.
			withAuth := strings.Contains(prefix, "authMiddleware")
			withRBAC := strings.Contains(prefix, "RequirePermission") || strings.Contains(prefix, "RequireAdmin")

			anc := inherited()
			auth := withAuth || anc.hasAuth
			rbac := withRBAC || anc.hasRBAC
			if auth && !rbac {
				count++
			}
		}

		// Update brace depth AFTER inspecting this line, string-literal aware,
		// so an r.Use/mutative-call line that itself opens a brace (route group
		// declarations always do) attributes correctly to its own block, not a
		// child that hasn't been pushed yet.
		inString := false
		var delim byte
		for i := 0; i < len(line); i++ {
			c := line[i]
			if inString {
				if c == delim && (i == 0 || line[i-1] != '\\') {
					inString = false
				}
				continue
			}
			switch c {
			case '"', '`':
				inString = true
				delim = c
			case '{':
				push()
			case '}':
				pop()
			}
		}
	}

	return count
}
