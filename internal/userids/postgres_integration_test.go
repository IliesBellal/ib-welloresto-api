//go:build postgres_integration

package userids

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Renumérotation contre un vrai Postgres, restreinte aux utilisateurs du
// test (scope) : la base de staging porte de vrais "user-<uuid>" qui ne
// doivent pas être renumérotés par un test.
//
// Prérequis : migrations 186 (users_user_id_seq) et 187 (user_id_remap).
//
//	POSTGRES_URL=... go test -tags postgres_integration ./internal/userids/
func TestApply_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	requireMigrations(t, db)

	const (
		merchantID = "itest-renum-m1"
		legacyA    = "user-itest-renum-a"
		legacyB    = "user-itest-renum-b"
	)
	only := scope{legacyA, legacyB}

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM users_rights WHERE merchant_id = $1`,
			`DELETE FROM users_devices WHERE merchant_id = $1`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
		} {
			if _, err := db.ExecContext(ctx, q, merchantID); err != nil {
				t.Logf("cleanup %q: %v", q, err)
			}
		}
		for _, q := range []string{
			`DELETE FROM users WHERE user_id IN (SELECT user_id FROM user_id_remap WHERE legacy_user_id IN ($1, $2)) OR user_id IN ($1, $2)`,
			`DELETE FROM user_id_remap WHERE legacy_user_id IN ($1, $2)`,
		} {
			if _, err := db.ExecContext(ctx, q, legacyA, legacyB); err != nil {
				t.Logf("cleanup %q: %v", q, err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(desc, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %s: %v", desc, err)
		}
	}
	// a créé avant b : reçoit le plus petit nouvel id.
	mustExec("user a", `INSERT INTO users (user_id, name, first_name, last_name, password, email, token, created_at)
		VALUES ($1, 'itest-renum-a', 'A', 'Renum', 'x', 'itest-renum-a@example.com', 'itest-renum-tok-a', now() - interval '2 days')`, legacyA)
	mustExec("user b", `INSERT INTO users (user_id, name, first_name, last_name, password, email, token, created_at)
		VALUES ($1, 'itest-renum-b', 'B', 'Renum', 'x', 'itest-renum-b@example.com', 'itest-renum-tok-b', now() - interval '1 day')`, legacyB)
	mustExec("users_rights a", `INSERT INTO users_rights (user_id, merchant_id, token, enabled) VALUES ($1, $2, 'itest-renum-rt-a', TRUE)`, legacyA, merchantID)
	mustExec("users_devices a", `INSERT INTO users_devices (user_id, merchant_id, app, device_id, fcm_token, last_used) VALUES ($1, $2, 'POS', 'itest-renum-dev-a', 'tok', now())`, legacyA, merchantID)
	// Paiements : un non signé (réécrit), un signé (gardé), une sentinelle (jamais touchée).
	mustExec("payment unsealed", `INSERT INTO payments (merchant_id, user_id, order_id, amount, mop) VALUES ($1, $2, 990001, 100, 'CASH')`, merchantID, legacyA)
	mustExec("payment sealed", `INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, hash, signature) VALUES ($1, $2, 990002, 200, 'CASH', 'itest-h', 'itest-sig')`, merchantID, legacyA)
	mustExec("payment sentinel", `INSERT INTO payments (merchant_id, user_id, order_id, amount, mop) VALUES ($1, 'KIOSK', 990003, 300, 'CB')`, merchantID)
	mustExec("audit unsealed", `INSERT INTO audit_logs (id, user_id, merchant_id, action, hash) VALUES ('itest-renum-al-1', $1, $2, 'itest', 'itest-h')`, legacyB, merchantID)
	mustExec("audit sealed", `INSERT INTO audit_logs (id, user_id, merchant_id, action, hash, signature) VALUES ('itest-renum-al-2', $1, $2, 'itest', 'itest-h', 'itest-sig')`, legacyB, merchantID)

	// --- Plan : rien n'est écrit ---
	planned, err := plan(ctx, db, only)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planned.Users) != 2 || planned.Users[legacyA] != "" || planned.Users[legacyB] != "" {
		t.Fatalf("plan users = %v, want a et b sans nouvel id", planned.Users)
	}
	byCol := counts(planned)
	expectCount(t, "plan", byCol, "users_rights.user_id", 1, 0)
	expectCount(t, "plan", byCol, "users_devices.user_id", 1, 0)
	expectCount(t, "plan", byCol, "payments.user_id", 1, 1)
	expectCount(t, "plan", byCol, "audit_logs.user_id", 1, 1)
	if got := scalar(t, db, `SELECT count(*) FROM user_id_remap WHERE legacy_user_id IN ($1, $2)`, legacyA, legacyB); got != "0" {
		t.Fatalf("plan ne doit rien écrire, user_id_remap = %s ligne(s)", got)
	}

	// --- Apply ---
	rep, err := apply(ctx, db, only)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	newA, newB := rep.Users[legacyA], rep.Users[legacyB]
	if !IsNumeric(newA) || !IsNumeric(newB) {
		t.Fatalf("nouveaux ids non numériques : a=%q b=%q", newA, newB)
	}
	na, _ := strconv.ParseInt(newA, 10, 64)
	nb, _ := strconv.ParseInt(newB, 10, 64)
	if na >= nb {
		t.Fatalf("a (créé avant) doit recevoir le plus petit id : a=%d b=%d", na, nb)
	}

	expectValue(t, db, "users a", `SELECT count(*) FROM users WHERE user_id = $1 AND email = 'itest-renum-a@example.com'`, "1", newA)
	expectValue(t, db, "users legacy gone", `SELECT count(*) FROM users WHERE user_id IN ($1, $2)`, "0", legacyA, legacyB)
	expectValue(t, db, "users_rights", `SELECT user_id FROM users_rights WHERE merchant_id = $1`, newA, merchantID)
	expectValue(t, db, "users_devices", `SELECT user_id FROM users_devices WHERE merchant_id = $1`, newA, merchantID)
	expectValue(t, db, "payment unsealed", `SELECT user_id FROM payments WHERE merchant_id = $1 AND order_id = 990001`, newA, merchantID)
	expectValue(t, db, "payment sealed keeps legacy", `SELECT user_id FROM payments WHERE merchant_id = $1 AND order_id = 990002`, legacyA, merchantID)
	expectValue(t, db, "payment sentinel untouched", `SELECT user_id FROM payments WHERE merchant_id = $1 AND order_id = 990003`, "KIOSK", merchantID)
	expectValue(t, db, "audit unsealed", `SELECT user_id FROM audit_logs WHERE id = 'itest-renum-al-1'`, newB)
	expectValue(t, db, "audit sealed keeps legacy", `SELECT user_id FROM audit_logs WHERE id = 'itest-renum-al-2'`, legacyB)
	byCol = counts(rep)
	expectCount(t, "apply", byCol, "payments.user_id", 1, 1)
	expectCount(t, "apply", byCol, "audit_logs.user_id", 1, 1)

	// --- Filet d'entrée ---
	if got := Resolve(ctx, db, legacyA); got != newA {
		t.Fatalf("Resolve(%q) = %q, want %q", legacyA, got, newA)
	}
	if got := Resolve(ctx, db, "user-itest-renum-unknown"); got != "user-itest-renum-unknown" {
		t.Fatalf("Resolve d'un ancien id inconnu doit le renvoyer tel quel, got %q", got)
	}

	// --- Rejouable : aucune nouvelle correspondance, mais une écriture
	// tardive avec l'ancien id (session encore en cache) est rattrapée. ---
	mustExec("stale device", `INSERT INTO users_devices (user_id, merchant_id, app, device_id, fcm_token, last_used) VALUES ($1, $2, 'POS', 'itest-renum-dev-a2', 'tok', now())`, legacyA, merchantID)
	again, err := apply(ctx, db, only)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(again.Users) != 0 {
		t.Fatalf("second apply ne doit renuméroter personne, got %v", again.Users)
	}
	expectCount(t, "second apply", counts(again), "users_devices.user_id", 1, 0)
	expectValue(t, db, "stale device rewritten", `SELECT count(*) FROM users_devices WHERE merchant_id = $1 AND user_id = $2`, "2", merchantID, newA)
	expectValue(t, db, "remap unchanged", `SELECT user_id FROM user_id_remap WHERE legacy_user_id = $1`, newA, legacyA)
}

func requireMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	var seq, remap bool
	if err := db.QueryRow(`SELECT to_regclass('public.users_user_id_seq') IS NOT NULL, to_regclass('public.user_id_remap') IS NOT NULL`).Scan(&seq, &remap); err != nil {
		t.Fatalf("check migrations: %v", err)
	}
	if !seq || !remap {
		t.Skipf("migrations 186/187 non appliquées sur cette base (users_user_id_seq=%v, user_id_remap=%v)", seq, remap)
	}
}

func counts(rep Report) map[string]ColumnCount {
	out := map[string]ColumnCount{}
	for _, c := range rep.Columns {
		out[c.Column.String()] = c
	}
	return out
}

func expectCount(t *testing.T, step string, byCol map[string]ColumnCount, col string, rewrite, kept int64) {
	t.Helper()
	c := byCol[col]
	if c.Rewrite != rewrite || c.KeptSealed != kept {
		t.Fatalf("%s %s : rewrite=%d kept=%d, want rewrite=%d kept=%d", step, col, c.Rewrite, c.KeptSealed, rewrite, kept)
	}
}

func scalar(t *testing.T, db *sql.DB, q string, args ...interface{}) string {
	t.Helper()
	var v string
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return v
}

func expectValue(t *testing.T, db *sql.DB, desc, q, want string, args ...interface{}) {
	t.Helper()
	if got := scalar(t, db, q, args...); got != want {
		t.Fatalf("%s = %q, want %q", desc, got, want)
	}
}
