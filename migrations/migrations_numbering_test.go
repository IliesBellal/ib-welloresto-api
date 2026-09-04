// Package migrations holds no runtime code — this file exists solely to give
// the migration-numbering guard test below a place to run from (migrations
// are applied by hand, per CLAUDE.md, so there is no migration-tool package
// to hang this check off of).
package migrations

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// preExistingCollisions documents numbers that legitimately collide today,
// all predating this test and all already applied — renumbering any of them
// now would rewrite already-shipped history for zero benefit. New collisions
// must not be added to this list; fix the number instead (see PROMPT 11,
// docs/decisions.md, for how the 114/115 collision that prompted this test
// was resolved).
var preExistingCollisions = map[string]bool{
	"024": true, // migrations/done: add_planning_weeks_published_at vs add_users_last_login_at
	"033": true, // migrations/done: add_pos_auto_lock_to_merchant_parameters vs orders_public_id
	"062": true, // migrations/done: kiosks_device_id vs location_varchar_ids
	"103": true, // migrations/todo: permission_catalog_lot10 vs production_ready_delivery_arrival
}

var migrationFilenameRE = regexp.MustCompile(`^(\d+)_(.+?)(?:\.up|\.down)?\.sql$`)

// TestNoDuplicateMigrationNumbers fails when two migration files in the same
// directory share a leading number under different slugs — the exact
// situation PROMPT 11 found twice (114, and pre-existing 103/033/062/024):
// with migrations applied by hand and no tool enforcing order, a duplicate
// number is a migration that silently never gets applied, or gets applied
// twice. It scans migrations/todo (the active, not-yet-guaranteed-applied
// set) and migrations/done (frozen archive, checked once here so nothing new
// ever lands there with a colliding number) separately — a number is allowed
// to appear in both todo and done (a migration graduates from one to the
// other) as long as within each directory it maps to exactly one slug.
func TestNoDuplicateMigrationNumbers(t *testing.T) {
	for _, dir := range []string{"todo", "done"} {
		dir := dir
		t.Run(dir, func(t *testing.T) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("reading migrations/%s: %v", dir, err)
			}

			slugsByNumber := map[string]map[string]bool{}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				m := migrationFilenameRE.FindStringSubmatch(e.Name())
				if m == nil {
					continue
				}
				number, slug := m[1], m[2]
				if slugsByNumber[number] == nil {
					slugsByNumber[number] = map[string]bool{}
				}
				slugsByNumber[number][slug] = true
			}

			var offenders []string
			for number, slugs := range slugsByNumber {
				if len(slugs) <= 1 || preExistingCollisions[number] {
					continue
				}
				names := make([]string, 0, len(slugs))
				for s := range slugs {
					names = append(names, s)
				}
				sort.Strings(names)
				offenders = append(offenders, number+": "+strings.Join(names, " vs "))
			}
			sort.Strings(offenders)

			if len(offenders) > 0 {
				t.Errorf("duplicate migration number(s) in migrations/%s (rename the one not yet applied to prod/staging):\n%s",
					dir, strings.Join(offenders, "\n"))
			}
		})
	}
}
