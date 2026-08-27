package permission

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"testing"
)

// insertPermissionsRe matches the first quoted string of each VALUES tuple in
// an `INSERT INTO permissions (key, ...) VALUES (...)` statement, e.g.
// ('pos.access',             'pos', ...) -> "pos.access".
var insertPermissionsRe = regexp.MustCompile(`\(\s*'([a-z0-9_.]+)'\s*,\s*'[a-z0-9_]+'\s*,`)

// deletePermissionsStmtRe matches a `DELETE FROM permissions WHERE key ...`
// statement and captures its predicate — either `= '<key>'` or
// `IN (<comma-separated 'key' list>)` — for quotedKeyRe to pull the actual
// key(s) out of. See migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql
// for the precedent this matches.
var deletePermissionsStmtRe = regexp.MustCompile(`(?is)DELETE\s+FROM\s+permissions\s+WHERE\s+key\s*(=\s*'[a-z0-9_.]+'|IN\s*\([^)]*\))`)

// quotedKeyRe pulls every single-quoted key out of a deletePermissionsStmtRe
// predicate capture, whether it held one key (`= '...'`) or several
// (`IN ('a', 'b')`).
var quotedKeyRe = regexp.MustCompile(`'([a-z0-9_.]+)'`)

// migrationPermissionKeys scans every *.up.sql file under migrations/ (both
// todo/ and done/) and returns the catalog's current key set: every key ever
// inserted into `permissions`, minus every key a later migration deleted from
// it (RBAC lot 8 deprecated pos.access and pos.discount.apply this way — see
// migrations/done/100_deprecate_pos_access_and_discount_apply.up.sql — and
// any future deprecation follows the same DELETE FROM permissions shape).
//
// Scanning the whole migrations tree — rather than hardcoding filenames — is
// deliberate: the permission catalog is neither created nor modified by a
// single migration (095 seeds the first 14, 097 adds a 15th, 100 removes two,
// and any future one may add or remove more). Hardcoding filenames here would
// silently stop catching new catalog migrations the day a future lot adds
// one.
func migrationPermissionKeys(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate keys_gen_test.go path via runtime.Caller")
	}
	// internal/permission/keys_gen_test.go -> repo root is two levels up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	migrationsDir := filepath.Join(repoRoot, "migrations")

	inserted := make(map[string]bool)
	deleted := make(map[string]bool)
	var insertOrder []string

	for _, sub := range []string{"todo", "done"} {
		dir := filepath.Join(migrationsDir, sub)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !isUpMigration(name) {
				continue
			}

			content, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}

			if bytesContainInsertIntoPermissions(content) {
				for _, m := range insertPermissionsRe.FindAllStringSubmatch(string(content), -1) {
					key := m[1]
					if !inserted[key] {
						inserted[key] = true
						insertOrder = append(insertOrder, key)
					}
				}
			}

			for _, stmt := range deletePermissionsStmtRe.FindAllStringSubmatch(string(content), -1) {
				for _, km := range quotedKeyRe.FindAllStringSubmatch(stmt[1], -1) {
					deleted[km[1]] = true
				}
			}
		}
	}

	var keys []string
	for _, key := range insertOrder {
		if !deleted[key] {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		t.Fatalf("no permission keys found under %s — did the migrations move, or did the INSERT INTO permissions marker change?", migrationsDir)
	}
	return keys
}

func isUpMigration(filename string) bool {
	return len(filename) > len(".up.sql") && filename[len(filename)-len(".up.sql"):] == ".up.sql"
}

func bytesContainInsertIntoPermissions(content []byte) bool {
	return regexp.MustCompile(`(?i)INSERT\s+INTO\s+permissions\b`).Match(content)
}

// TestAllMatchesMigrationCatalog is the single source-of-truth guard: it
// fails as soon as internal/permission/keys_gen.go (the Go-side catalog) and
// the union of every migration that inserts into `permissions` (the DB-side
// catalog) list different keys, in either direction.
func TestAllMatchesMigrationCatalog(t *testing.T) {
	migrationKeys := migrationPermissionKeys(t)

	got := make([]string, len(All))
	for i, k := range All {
		got[i] = string(k)
	}
	want := append([]string(nil), migrationKeys...)
	sort.Strings(got)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("permission.All has %d keys, migrations declare %d\ngo (sorted):         %v\nmigrations (sorted): %v",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("permission.All diverges from the migrations at index %d: %q (go) vs %q (migrations)\ngo (sorted):         %v\nmigrations (sorted): %v",
				i, got[i], want[i], got, want)
		}
	}
}

// TestNoDuplicateKeys guards against a copy-paste error inside keys_gen.go
// itself (a duplicated constant value would pass TestAllMatchesMigrationCatalog
// as long as the migrations also declared that many entries, but would
// silently collapse two distinct permissions into one).
func TestNoDuplicateKeys(t *testing.T) {
	seen := make(map[Key]bool, len(All))
	for _, key := range All {
		if seen[key] {
			t.Fatalf("duplicate permission key in permission.All: %q", key)
		}
		seen[key] = true
	}
}
