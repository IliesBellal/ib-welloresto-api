package main

import (
	"os"
	"regexp"
	"testing"
)

// TestRBACCoverage checks that cmd/api/routes.go still declares the
// RequirePermission/RequireAdmin guards RBAC lot 2 put in place, by scanning
// the file's source text rather than building the live router.
//
// Why static text scanning and not a live chi.Mux crawl: SetupRoutes wires
// ~40 real repositories/services directly against a *sql.DB and *config.AppConfig
// with no seams for substitution — it is production wiring, not something
// designed to be constructed standalone in a unit test. Scanning the source
// is also the same technique internal/permission/keys_gen_test.go already
// uses to keep a Go file honest against another file (there, a migration;
// here, itself) — no new pattern introduced.
//
// SCOPE — and why this is not "every mutative route in the API": as of this
// lot, the large majority of mutative (POST/PUT/PATCH/DELETE) routes behind
// authMiddleware carry NO RequirePermission/RequireAdmin guard at all — see
// docs/RBAC_ROUTES.md for the full inventory (menu outside /import, HACCP
// outside /traceability, orders, bookings, cash_register, customers outside
// /import and creation, delivery_sessions, integrations, kiosk device admin,
// stocks, floors, locations, printers, ...). A test asserting "every mutative
// route under authMiddleware has a guard, except this list" would need an
// exception list of well over a hundred entries against the CURRENT
// repository — not a short, line-by-line-justified list, just a restatement
// of the status quo under a different name. Writing it would do one of two
// unwanted things: rubber-stamp routes nobody has reviewed as intentional
// exceptions, or quietly pressure someone into adding RBAC guards this lot
// never asked for (breaking its own "no behavior change" rule).
//
// So this test checks only what RBAC lot 2 actually decided:
//  1. every route this lot explicitly gated (guardedRoutePatterns) still
//     carries that exact guard in the source;
//  2. the /planning group — the one block guarded uniformly at the r.Use
//     level — has no mutative route whose declaration lost the group's
//     inherited guard (a route moved outside the r.Route block, for
//     instance, would not be caught by check 1 above).
//
// Extending this test to the rest of the API is a lot 3 decision (prioritize
// from docs/RBAC_ROUTES.md's "authMiddleware / aucun" rows), not something to
// assume here.
func TestRBACCoverage(t *testing.T) {
	src := readRoutesSource(t)

	for _, g := range guardedRoutePatterns {
		if !g.pattern.MatchString(src) {
			t.Errorf("%s %s: expected guard %s not found in cmd/api/routes.go — did the guard get removed, or the route declaration reshaped enough that this pattern no longer matches it? (pattern: %s)",
				g.method, g.path, g.guard, g.pattern.String())
		}
	}

	assertPlanningGroupFullyGuarded(t, src)
}

func readRoutesSource(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatalf("read routes.go: %v", err)
	}
	return string(content)
}

type guardedRoutePattern struct {
	method  string
	path    string
	guard   string
	pattern *regexp.Regexp
}

func guard(method, path, guardName, rawPattern string) guardedRoutePattern {
	return guardedRoutePattern{method: method, path: path, guard: guardName, pattern: regexp.MustCompile(rawPattern)}
}

// guardedRoutePatterns is the exhaustive list of mutative routes RBAC lot 2
// put a permission.Key or RequireAdmin behind. Cross-check against
// docs/RBAC_ROUTES.md if this list and that document ever disagree — one of
// them is stale and needs updating to match the other.
var guardedRoutePatterns = []guardedRoutePattern{
	guard("POST", "/auth/pin/reset", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Post\(\s*"/pin/reset"\s*,\s*authH\.ResetPIN\)`),
	guard("POST", "/users/", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Post\(\s*"/"\s*,\s*usersH\.CreateUser\)`),
	guard("POST", "/users/create", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Post\(\s*"/create"\s*,\s*usersH\.CreateUser\)`),
	guard("POST", "/users/{id}/merchant-link", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Post\(\s*"/\{id\}/merchant-link"\s*,\s*usersH\.LinkMerchantUser\)`),
	guard("PUT", "/users/{id}/rights", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Put\(\s*"/\{id\}/rights"\s*,\s*usersH\.UpdateMerchantUserRights\)`),
	guard("PATCH", "/users/{id}/member", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Patch\(\s*"/\{id\}/member"\s*,\s*usersH\.PatchMerchantUserMember\)`),
	guard("POST", "/users/{id}/force-reset-password", "RequireAdmin",
		`RequireAdmin\(\)\)\.Post\(\s*"/\{id\}/force-reset-password"\s*,\s*usersH\.ForceResetPassword\)`),
	guard("DELETE", "/users/{id}/merchant-link", "RequireAdmin",
		`RequireAdmin\(\)\)\.Delete\(\s*"/\{id\}/merchant-link"\s*,\s*usersH\.UnlinkMerchantUser\)`),
	guard("POST", "/pos/link-user", "permission.StaffManage",
		`RequirePermission\(permission\.StaffManage\)\)\.Post\(\s*"/link-user"\s*,\s*posH\.LinkUser\)`),
	guard("PATCH", "/pos/status", "permission.POSStatusManage",
		`RequirePermission\(permission\.POSStatusManage\)\)\.Patch\(\s*"/status"\s*,\s*posH\.UpdatePOSStatus\)`),
	guard("POST", "/menu/import/preview", "permission.CatalogManage",
		`RequirePermission\(permission\.CatalogManage\)\)\.\s*Post\(\s*"/import/preview"\s*,\s*menuImportH\.PreviewImport\)`),
	guard("POST", "/menu/import/commit", "permission.CatalogManage",
		`RequirePermission\(permission\.CatalogManage\)\)\.\s*Post\(\s*"/import/commit"\s*,\s*menuImportH\.CommitImport\)`),
	guard("GET", "/menu/import/template", "permission.CatalogManage",
		`RequirePermission\(permission\.CatalogManage\)\)\.\s*Get\(\s*"/import/template"\s*,\s*menuImportH\.DownloadImportTemplate\)`),
	guard("POST", "/customers/import/preview", "permission.CustomersManage",
		`RequirePermission\(permission\.CustomersManage\)\)\.\s*Post\(\s*"/import/preview"\s*,\s*customerImportH\.PreviewImport\)`),
	guard("POST", "/customers/import/commit", "permission.CustomersManage",
		`RequirePermission\(permission\.CustomersManage\)\)\.\s*Post\(\s*"/import/commit"\s*,\s*customerImportH\.CommitImport\)`),
	guard("GET", "/customers/import/template", "permission.CustomersManage",
		`RequirePermission\(permission\.CustomersManage\)\)\.\s*Get\(\s*"/import/template"\s*,\s*customerImportH\.DownloadImportTemplate\)`),
	guard("GET", "/pos/settings/kiosk/devices/{device_id}/admin-pin", "permission.SettingsManage",
		`RequirePermission\(permission\.SettingsManage\)\)\.Get\(\s*"/devices/\{device_id\}/admin-pin"\s*,\s*kioskAdminHandler\.GetAdminPin\)`),
	guard("POST", "/pos/settings/kiosk/devices/{device_id}/regenerate-admin-pin", "permission.SettingsManage",
		`RequirePermission\(permission\.SettingsManage\)\)\.Post\(\s*"/devices/\{device_id\}/regenerate-admin-pin"\s*,\s*kioskAdminHandler\.RegenerateAdminPin\)`),

	// --- RBAC lot 8 (2026-08-27) ---
	guard("PATCH", "/orders/{order_id}/reopen", "permission.POSTicketReopen",
		`RequirePermission\(permission\.POSTicketReopen\)\)\.\s*Patch\(\s*"/\{order_id\}/reopen"\s*,\s*ordersLifeCycleH\.ReopenClosedOrder\)`),
	guard("POST", "/orders/{order_id}/refund", "permission.POSRefund",
		`RequirePermission\(permission\.POSRefund\)\)\.\s*Post\(\s*"/\{order_id\}/refund"\s*,\s*ordersLifeCycleH\.HandleRefund\)`),
	guard("DELETE", "/orders/{order_id}/payments/{payment_id}", "permission.POSRefund",
		`RequirePermission\(permission\.POSRefund\)\)\.\s*Delete\(\s*"/\{payment_id\}"\s*,\s*ordersLifeCycleH\.DeletePayment\)`),
	guard("PUT", "/stocks/components/{component_id}", "permission.InventoryManage",
		`RequirePermission\(permission\.InventoryManage\)\)\.\s*Put\(\s*"/components/\{component_id\}"\s*,\s*stocksH\.RecordComponentMovement\)`),
	guard("PUT", "/haccp/settings", "permission.HACCPManage",
		`RequirePermission\(permission\.HACCPManage\)\)\.\s*Put\(\s*"/settings"\s*,\s*haccpH\.PutSettings\)`),
	guard("POST", "/pos/reports/* (group)", "permission.ReportsSalesRead (r.Use on the sub-group)",
		`r\.Route\(\s*"/reports"\s*,\s*func\(r chi\.Router\) \{\s*r\.Use\(middleware\.RequirePermission\(permission\.ReportsSalesRead\)\)`),
	guard("GET", "/stats/dashboard/summary", "permission.ReportsSalesRead",
		`RequirePermission\(permission\.ReportsSalesRead\)\)\.\s*Get\(\s*"/summary"\s*,\s*statsH\.GetDashboardSummary\)`),
	guard("POST", "/accounting/* (group)", "permission.ReportsFinancialRead (r.Use on the sub-group)",
		`r\.Route\(\s*"/accounting"\s*,\s*func\(r chi\.Router\) \{\s*r\.Use\(authMiddleware\)\s*r\.Use\(middleware\.RequirePermission\(permission\.ReportsFinancialRead\)\)`),
	guard("GET", "/integrations/stripe/balance", "permission.ReportsFinancialRead",
		`RequirePermission\(permission\.ReportsFinancialRead\)\)\.\s*Get\(\s*"/stripe/balance"\s*,\s*integrationsHandler\.GetStripeBalance\)`),
	guard("GET", "/planning/performance", "permission.ReportsFinancialRead",
		`RequirePermission\(permission\.ReportsFinancialRead\)\)\.\s*Get\(\s*"/performance"\s*,\s*planningH\.GetPlanningPerformance\)`),
	guard("POST", "/cash_drawer/open", "permission.POSCashDrawerOpen",
		`RequirePermission\(permission\.POSCashDrawerOpen\)\)\.\s*Post\(\s*"/open"\s*,\s*cashRegisterH\.OpenCashDrawer\)`),
}

// mutativeRouteRe matches a bare (no .With(...) prefix) r.Post/Put/Patch/Delete
// call, the shape every route inside /planning uses (the guard is applied
// once via r.Use at the top of the group, not per-route).
var mutativeRouteRe = regexp.MustCompile(`\br\.(Post|Put|Patch|Delete)\(\s*"([^"]*)"`)

// assertPlanningGroupFullyGuarded extracts the /planning block (brace-depth
// aware, skipping string literals so the {id}/{date}/... chi patterns inside
// route strings don't confuse the brace count) and fails if it finds a
// mutative route declared outside of — or before — the group's r.Use guard,
// or if the group's guard itself has gone missing.
func assertPlanningGroupFullyGuarded(t *testing.T, src string) {
	t.Helper()

	const anchor = `r.Route("/planning", func(r chi.Router) {`
	block := extractBracedBlock(t, src, anchor)

	const wantGuard = `r.Use(middleware.RequirePermission(permission.StaffScheduleManage))`
	guardIdx := indexOf(block, wantGuard)
	if guardIdx == -1 {
		t.Fatalf("/planning group guard not found — expected %q as the first r.Use after authMiddleware", wantGuard)
		return
	}

	for _, m := range mutativeRouteRe.FindAllStringSubmatchIndex(block, -1) {
		routeStart := m[0]
		if routeStart < guardIdx {
			method, path := block[m[2]:m[3]], block[m[4]:m[5]]
			t.Errorf("/planning: %s %q is declared before the group's RequirePermission guard — it would run unguarded", method, path)
		}
	}
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// extractBracedBlock returns the full `{ ... }` block whose opening brace is
// the first one found after anchor, tracking Go string literals (`"`, “ ` “)
// so a `{param}` chi route pattern inside a quoted path never miscounts as a
// real brace.
func extractBracedBlock(t *testing.T, src, anchor string) string {
	t.Helper()

	idx := indexOf(src, anchor)
	if idx == -1 {
		t.Fatalf("anchor not found in routes.go: %q", anchor)
	}
	rest := src[idx:]

	braceStart := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == '{' {
			braceStart = i
			break
		}
	}
	if braceStart == -1 {
		t.Fatalf("no opening brace found after anchor %q", anchor)
	}

	depth := 0
	inString := false
	var delim byte
	for i := braceStart; i < len(rest); i++ {
		c := rest[i]
		if inString {
			if c == delim && rest[i-1] != '\\' {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '`':
			inString = true
			delim = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[braceStart : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces scanning from anchor %q", anchor)
	return ""
}
