package main

import (
	"os"
	"regexp"
	"testing"

	"welloresto-api/internal/permission"
)

// keyIdentRe matches one `Ident Key = "catalog.key"` line from
// internal/permission/keys_gen.go's const block, e.g.
// `POSTicketReopen      Key = "pos.ticket.reopen"` ->
// ident="POSTicketReopen", key="pos.ticket.reopen".
var keyIdentRe = regexp.MustCompile(`(?m)^\s*(\w+)\s+Key = "([a-z0-9_.]+)"`)

// permissionIdentifiers reads internal/permission/keys_gen.go and returns the
// catalog-key -> Go-identifier mapping it declares, e.g.
// "pos.ticket.reopen" -> "POSTicketReopen". Deriving this from the source
// file itself — rather than hardcoding it here — means a future rename in
// keys_gen.go is picked up automatically instead of silently desyncing a
// second, hand-maintained copy of the same mapping.
func permissionIdentifiers(t *testing.T) map[permission.Key]string {
	t.Helper()

	content, err := os.ReadFile("../../internal/permission/keys_gen.go")
	if err != nil {
		t.Fatalf("read internal/permission/keys_gen.go: %v", err)
	}

	idents := make(map[permission.Key]string)
	for _, m := range keyIdentRe.FindAllStringSubmatch(string(content), -1) {
		idents[permission.Key(m[2])] = m[1]
	}
	return idents
}

// TestRBACPermissionCoverage is the guard-side counterpart of
// internal/permission/keys_gen_test.go: that test keeps the Go catalog
// (keys_gen.go) honest against the DB catalog (the migrations); this one
// keeps the catalog honest against the router — every permission.Key in
// permission.All must guard at least one route in cmd/api/routes.go via
// middleware.RequirePermission, whether posed on a single route (.With(...))
// or on a whole group (r.Use(...)).
//
// This is the RBAC lot 8 ratchet: a permission can sit in the catalog,
// attached to a role (e.g. Administrateur holds every key by construction —
// see internal/modules/roles/repository.go, systemRolePermissions), without
// ever gating a single request. That is a checkbox that lies: it looks like
// an authorization decision but changes nothing at request time. This test
// fails the moment a permission is added to the catalog without wiring a
// route to it in the same change, and would have caught the eight orphaned
// permissions RBAC lot 8 fixed (see docs/RBAC_ROUTES.md and docs/decisions.md)
// before they ever shipped.
func TestRBACPermissionCoverage(t *testing.T) {
	src := readRoutesSource(t)
	idents := permissionIdentifiers(t)

	for _, key := range permission.All {
		ident, ok := idents[key]
		if !ok {
			t.Fatalf("permission.All contains %q but internal/permission/keys_gen.go has no matching %q Go identifier — did the const block's `Ident Key = \"...\"` shape change?", key, key)
			continue
		}

		pattern := regexp.MustCompile(`RequirePermission\(permission\.` + ident + `\)`)
		if !pattern.MatchString(src) {
			t.Errorf("permission %q (permission.%s) is not referenced by any middleware.RequirePermission(...) call in cmd/api/routes.go — every catalog permission must guard at least one route, or it should leave the catalog (see docs/decisions.md for how RBAC lot 8 retired pos.access and pos.discount.apply this way)", key, ident)
		}
	}
}
