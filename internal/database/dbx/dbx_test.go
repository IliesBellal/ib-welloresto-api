package dbx

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestActiveDialect_DefaultsToMySQL(t *testing.T) {
	t.Setenv(EnvDialect, "")
	if got := ActiveDialect(); got != MySQL {
		t.Fatalf("expected mysql by default, got %s", got)
	}
}

func TestActiveDialect_Postgres(t *testing.T) {
	for _, v := range []string{"postgres", "POSTGRES", "postgresql", "pgx"} {
		t.Setenv(EnvDialect, v)
		if got := ActiveDialect(); got != Postgres {
			t.Fatalf("DB_DIALECT=%s: expected postgres, got %s", v, got)
		}
	}
}

func TestRebind_MySQL_Unchanged(t *testing.T) {
	t.Setenv(EnvDialect, "mysql")
	q := "SELECT name FROM t WHERE a = ? AND b = ?"
	if got := Rebind(q); got != q {
		t.Fatalf("mysql query must be unchanged, got %q", got)
	}
}

func TestRebind_Postgres_Dollar(t *testing.T) {
	t.Setenv(EnvDialect, "postgres")
	got := Rebind("SELECT name FROM t WHERE a = ? AND b = ?")
	want := "SELECT name FROM t WHERE a = $1 AND b = $2"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// TestQueryContext_BothDialects vérifie que le wrapper envoie au driver la
// requête telle quelle en MySQL et rebindée en Postgres.
func TestQueryContext_BothDialects(t *testing.T) {
	cases := []struct {
		dialect  string
		expected string
	}{
		{"mysql", "SELECT ?"},
		{"postgres", "SELECT $1"},
	}

	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			t.Setenv(EnvDialect, tc.dialect)

			mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer mockDB.Close()

			mock.ExpectQuery(tc.expected).
				WithArgs(1).
				WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))

			db := GetDB(context.Background(), mockDB)
			rows, err := db.QueryContext(context.Background(), "SELECT ?", 1)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			rows.Close()

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("driver did not receive the expected query: %v", err)
			}
		})
	}
}

func TestIsDuplicateEntry(t *testing.T) {
	if !IsDuplicateEntry(&mysql.MySQLError{Number: 1062}) {
		t.Fatal("mysql 1062 must be detected")
	}
	if !IsDuplicateEntry(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("postgres 23505 must be detected")
	}
	if IsDuplicateEntry(errors.New("boom")) {
		t.Fatal("generic error must not be detected")
	}
}
