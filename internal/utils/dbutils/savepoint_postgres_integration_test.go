//go:build postgres_integration

package dbutils_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/utils/dbutils"
)

// Vérifie sur un vrai Postgres ce que Isolated et Shield garantissent : une requête
// en échec n'invalide pas la transaction, et rien ne change sans WithSavepoints.
func TestSavepoints_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	count := func(t *testing.T, tx *sql.Tx) int {
		t.Helper()
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM wr_sp_test`).Scan(&n); err != nil {
			t.Fatalf("lecture après l'échec isolé : %v", err)
		}
		return n
	}
	// run ouvre une transaction avec une table temporaire (propre à la transaction,
	// rien ne reste en base) contenant la ligne 1.
	run := func(t *testing.T, body func(txCtx context.Context, tx *sql.Tx)) error {
		t.Helper()
		return dbutils.RunInTx(ctx, db, func(txCtx context.Context) error {
			tx := dbutils.ExtractTx(txCtx)
			for _, q := range []string{
				`CREATE TEMP TABLE wr_sp_test (id int PRIMARY KEY) ON COMMIT DROP`,
				`INSERT INTO wr_sp_test VALUES (1)`,
			} {
				if _, err := tx.ExecContext(txCtx, q); err != nil {
					t.Fatalf("préparation : %v", err)
				}
			}
			body(txCtx, tx)
			return nil
		})
	}
	duplicate := func(txCtx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(txCtx, `INSERT INTO wr_sp_test VALUES (1)`) // clé en double
		return err
	}

	t.Run("Isolated : l'échec n'invalide pas la transaction", func(t *testing.T) {
		err := run(t, func(txCtx context.Context, tx *sql.Tx) {
			sp := dbutils.WithSavepoints(txCtx)
			if got := dbutils.Isolated(sp, func() error { return duplicate(sp, tx) }); got == nil {
				t.Error("l'erreur de la requête doit être renvoyée")
			}
			if _, err := tx.ExecContext(sp, `INSERT INTO wr_sp_test VALUES (2)`); err != nil {
				t.Errorf("la transaction doit rester utilisable : %v", err)
			}
			if n := count(t, tx); n != 2 {
				t.Errorf("lignes = %d, want 2 (1 d'avant, 2 d'après ; rien de l'échec)", n)
			}
		})
		if err != nil {
			t.Fatalf("la transaction doit pouvoir être validée : %v", err)
		}
	})

	t.Run("Isolated : un succès est conservé", func(t *testing.T) {
		err := run(t, func(txCtx context.Context, tx *sql.Tx) {
			sp := dbutils.WithSavepoints(txCtx)
			if got := dbutils.Isolated(sp, func() error {
				_, err := tx.ExecContext(sp, `INSERT INTO wr_sp_test VALUES (2)`)
				return err
			}); got != nil {
				t.Errorf("erreur inattendue : %v", got)
			}
			if n := count(t, tx); n != 2 {
				t.Errorf("lignes = %d, want 2", n)
			}
		})
		if err != nil {
			t.Fatalf("commit : %v", err)
		}
	})

	t.Run("Shield : une erreur avalée par la fonction n'invalide pas la transaction", func(t *testing.T) {
		err := run(t, func(txCtx context.Context, tx *sql.Tx) {
			sp := dbutils.WithSavepoints(txCtx)
			dbutils.Shield(sp, func() {
				_ = duplicate(sp, tx) // journalisée puis ignorée, comme les étapes tolérantes
			})
			if _, err := tx.ExecContext(sp, `INSERT INTO wr_sp_test VALUES (2)`); err != nil {
				t.Errorf("la transaction doit rester utilisable : %v", err)
			}
			if n := count(t, tx); n != 2 {
				t.Errorf("lignes = %d, want 2", n)
			}
		})
		if err != nil {
			t.Fatalf("commit : %v", err)
		}
	})

	t.Run("Shield : sans erreur, le travail de la fonction est conservé", func(t *testing.T) {
		err := run(t, func(txCtx context.Context, tx *sql.Tx) {
			sp := dbutils.WithSavepoints(txCtx)
			dbutils.Shield(sp, func() {
				_, _ = tx.ExecContext(sp, `INSERT INTO wr_sp_test VALUES (2)`)
			})
			if n := count(t, tx); n != 2 {
				t.Errorf("lignes = %d, want 2", n)
			}
		})
		if err != nil {
			t.Fatalf("commit : %v", err)
		}
	})

	t.Run("sans WithSavepoints rien ne change : la transaction reste invalidée", func(t *testing.T) {
		var after error
		err := run(t, func(txCtx context.Context, tx *sql.Tx) {
			_ = dbutils.Isolated(txCtx, func() error { return duplicate(txCtx, tx) })
			_, after = tx.ExecContext(txCtx, `INSERT INTO wr_sp_test VALUES (2)`)
		})
		if after == nil {
			t.Error("sans WithSavepoints, Isolated ne doit poser aucun point de sauvegarde")
		}
		if err == nil {
			t.Error("la transaction invalidée ne peut pas être validée")
		}
	})

	t.Run("hors transaction la fonction est simplement appelée", func(t *testing.T) {
		boom := errors.New("boom")
		calls := 0
		sp := dbutils.WithSavepoints(ctx)
		if got := dbutils.Isolated(sp, func() error { calls++; return boom }); !errors.Is(got, boom) {
			t.Errorf("Isolated = %v, want %v", got, boom)
		}
		dbutils.Shield(sp, func() { calls++ })
		if calls != 2 {
			t.Errorf("fn appelée %d fois, want 2", calls)
		}
	})
}
