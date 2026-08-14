package customers

import (
	"context"
	"fmt"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestLoadExistingByEmailsEmptyInputSkipsQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	got, err := repo.LoadExistingByEmails(context.Background(), "merchant-1", nil)
	if err != nil {
		t.Fatalf("LoadExistingByEmails: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %v, want vide", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("une requete a ete emise pour une liste vide: %v", err)
	}
}

func TestLoadExistingByEmailsSingleChunk(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	mock.ExpectQuery("SELECT LOWER").
		WithArgs("merchant-1", "jean@example.com", "alice@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"lower_email", "customer_id"}).
			AddRow("jean@example.com", 42).
			AddRow("alice@example.com", 7))

	got, err := repo.LoadExistingByEmails(context.Background(), "merchant-1", []string{"jean@example.com", "alice@example.com"})
	if err != nil {
		t.Fatalf("LoadExistingByEmails: %v", err)
	}
	if got["jean@example.com"] != 42 || got["alice@example.com"] != 7 {
		t.Fatalf("got = %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("attentes non satisfaites: %v", err)
	}
}

// Au-delà de lookupChunkSize, la clause IN doit être découpée en plusieurs
// requêtes séquentielles, dont les résultats se fusionnent dans une seule map.
func TestLoadExistingByEmailsChunksLargeInput(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	const total = lookupChunkSize*2 + 1 // 3 chunks : 900, 900, 1
	emails := make([]string, total)
	for i := range emails {
		emails[i] = fmt.Sprintf("user%d@example.com", i)
	}

	// Chunk 1 : les lookupChunkSize premiers emails.
	mock.ExpectQuery("SELECT LOWER").
		WillReturnRows(sqlmock.NewRows([]string{"lower_email", "customer_id"}).
			AddRow(emails[0], 1))
	// Chunk 2.
	mock.ExpectQuery("SELECT LOWER").
		WillReturnRows(sqlmock.NewRows([]string{"lower_email", "customer_id"}).
			AddRow(emails[lookupChunkSize], 2))
	// Chunk 3 : le seul email restant.
	mock.ExpectQuery("SELECT LOWER").
		WillReturnRows(sqlmock.NewRows([]string{"lower_email", "customer_id"}).
			AddRow(emails[total-1], 3))

	got, err := repo.LoadExistingByEmails(context.Background(), "merchant-1", emails)
	if err != nil {
		t.Fatalf("LoadExistingByEmails: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got = %d entrees, want 3 (une par chunk)", len(got))
	}
	if got[emails[0]] != 1 || got[emails[lookupChunkSize]] != 2 || got[emails[total-1]] != 3 {
		t.Fatalf("fusion incorrecte des chunks: %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("attentes non satisfaites (nombre de requetes inattendu): %v", err)
	}
}

func TestLoadExistingByPhonesEmptyInputSkipsQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	got, err := repo.LoadExistingByPhones(context.Background(), "merchant-1", []string{})
	if err != nil {
		t.Fatalf("LoadExistingByPhones: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %v, want vide", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("une requete a ete emise pour une liste vide: %v", err)
	}
}

func TestLoadExistingByPhonesSingleChunk(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	mock.ExpectQuery("SELECT customer_tel, customer_id").
		WithArgs("merchant-1", "+33612345678").
		WillReturnRows(sqlmock.NewRows([]string{"customer_tel", "customer_id"}).
			AddRow("+33612345678", 99))

	got, err := repo.LoadExistingByPhones(context.Background(), "merchant-1", []string{"+33612345678"})
	if err != nil {
		t.Fatalf("LoadExistingByPhones: %v", err)
	}
	if got["+33612345678"] != 99 {
		t.Fatalf("got = %v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("attentes non satisfaites: %v", err)
	}
}

func TestLoadImportMappingsEmptyInputSkipsQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	got, err := repo.LoadImportMappings(context.Background(), "merchant-1", "zelty", nil)
	if err != nil {
		t.Fatalf("LoadImportMappings: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %v, want vide", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("une requete a ete emise pour une liste vide: %v", err)
	}
}

func TestLoadImportMappingsDistinguishesStaleFromLive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	repo := NewCustomerRepository(db)

	mock.ExpectQuery("SELECT m.external_id, m.wello_id").
		WithArgs("merchant-1", "zelty", "Z1", "Z2").
		WillReturnRows(sqlmock.NewRows([]string{"external_id", "wello_id", "target_exists"}).
			AddRow("Z1", 10, true).
			AddRow("Z2", 20, false))

	got, err := repo.LoadImportMappings(context.Background(), "merchant-1", "zelty", []string{"Z1", "Z2"})
	if err != nil {
		t.Fatalf("LoadImportMappings: %v", err)
	}
	if got["Z1"].CustomerID != 10 || !got["Z1"].TargetExists {
		t.Fatalf("Z1 = %+v, want CustomerID=10 TargetExists=true", got["Z1"])
	}
	if got["Z2"].CustomerID != 20 || got["Z2"].TargetExists {
		t.Fatalf("Z2 = %+v, want CustomerID=20 TargetExists=false", got["Z2"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("attentes non satisfaites: %v", err)
	}
}

func TestChunkStrings(t *testing.T) {
	if got := chunkStrings(nil, 2); got != nil {
		t.Fatalf("chunkStrings(nil) = %v, want nil", got)
	}

	got := chunkStrings([]string{"a", "b", "c", "d", "e"}, 2)
	want := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if len(got) != len(want) {
		t.Fatalf("chunkStrings = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("chunk %d = %v, want %v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Fatalf("chunk %d = %v, want %v", i, got[i], want[i])
			}
		}
	}
}
