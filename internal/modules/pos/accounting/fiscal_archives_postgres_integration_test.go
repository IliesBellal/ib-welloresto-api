//go:build postgres_integration

package accounting

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type fakeArchiveStorage struct{ uploads int }

func (f *fakeArchiveStorage) UploadPrivateFile(_ context.Context, key string, file io.Reader, _ string) (string, error) {
	_, _ = io.Copy(io.Discard, file)
	f.uploads++
	return key, nil
}

func (f *fakeArchiveStorage) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://signed.example/" + key, nil
}

// Routes des archives fiscales (conformité caisse lot D, phase 4) :
// génération à la demande, liste, lien tracé au journal d'audit, cloisonnement
// par établissement.
func TestFiscalArchivesRoutes_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const siret = "siret-itest-arch-routes"

	cleanup := func(mid string) {
		for _, q := range []string{
			`DELETE FROM fiscal_archives WHERE merchant_id = $1`,
			`DELETE FROM fiscal_closures WHERE merchant_id = $1`,
			`DELETE FROM audit_logs WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id::text = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var old int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = $1 LIMIT 1`, siret).Scan(&old); err == nil {
		cleanup(strconv.FormatInt(old, 10))
	}
	var mid int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, creation_date)
		VALUES ('ITest Archive Routes', 'a', '1', 's', '75001', 'Paris', $1, 'https://x', '06', 'mt-arch-routes', 'Europe/Paris', '2025-01-01')
		RETURNING id`, siret).Scan(&mid); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID := strconv.FormatInt(mid, 10)
	t.Cleanup(func() { cleanup(merchantID) })

	// Jours clos : du 1er au 10 septembre 2026 (aucune vente : archive vide,
	// mais valide).
	loc, _ := time.LoadLocation("Europe/Paris")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := fiscal.CloseDueDays(ctx, db, merchantID, "Europe/Paris", &from, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 11, 12, 0, 0, 0, loc)); err != nil {
		t.Fatalf("CloseDueDays: %v", err)
	}

	svc := NewAccountingService(NewAccountingRepository(db), nil)
	userCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "itest-arch-user", MerchantID: merchantID})
	storage := &fakeArchiveStorage{}

	// Périodes refusées avant tout travail.
	for _, req := range []GenerateFiscalArchiveRequest{
		{DateFrom: "2026-09-05", DateTo: "2026-09-01"},
		{DateFrom: "2026-08-01", DateTo: "2026-09-01"}, // 32 jours
		{DateFrom: "01/09/2026", DateTo: "2026-09-02"},
	} {
		if _, err := svc.GenerateFiscalArchive(userCtx, req, storage); !errors.Is(err, models.ErrFiscalPeriodInvalid) {
			t.Fatalf("%+v: err = %v, want ErrFiscalPeriodInvalid", req, err)
		}
	}
	// Jours non clos.
	if _, err := svc.GenerateFiscalArchive(userCtx, GenerateFiscalArchiveRequest{DateFrom: "2026-09-09", DateTo: "2026-09-12"}, storage); !errors.Is(err, models.ErrFiscalPeriodNotClosed) {
		t.Fatalf("not closed: err = %v", err)
	}
	// Stockage indisponible.
	if _, err := svc.GenerateFiscalArchive(userCtx, GenerateFiscalArchiveRequest{DateFrom: "2026-09-01", DateTo: "2026-09-02"}, nil); !errors.Is(err, models.ErrFiscalArchiveStorage) {
		t.Fatalf("no storage: err = %v", err)
	}
	// Une génération en cours pour l'établissement : la seconde est refusée.
	release, ok, err := fiscal.TryLock(ctx, db, "fiscal:archives-on-demand:"+merchantID)
	if err != nil || !ok {
		t.Fatalf("TryLock: ok=%v err=%v", ok, err)
	}
	if _, err := svc.GenerateFiscalArchive(userCtx, GenerateFiscalArchiveRequest{DateFrom: "2026-09-01", DateTo: "2026-09-02"}, storage); !errors.Is(err, models.ErrFiscalArchiveBusy) {
		t.Fatalf("busy: err = %v", err)
	}
	release()
	if storage.uploads != 0 {
		t.Fatalf("refused requests must not upload, got %d", storage.uploads)
	}

	gen, err := svc.GenerateFiscalArchive(userCtx, GenerateFiscalArchiveRequest{DateFrom: "2026-09-01", DateTo: "2026-09-10"}, storage)
	if err != nil {
		t.Fatalf("GenerateFiscalArchive: %v", err)
	}
	if gen.Archive.Kind != "PERIOD" || gen.Archive.GeneratedBy != "itest-arch-user" || gen.Archive.PeriodEnd != "2026-09-10" || storage.uploads != 1 {
		t.Fatalf("archive: %+v (uploads %d)", gen.Archive, storage.uploads)
	}

	list, err := svc.ListFiscalArchives(userCtx)
	if err != nil || len(list.Archives) != 1 || list.Archives[0].ID != gen.Archive.ID {
		t.Fatalf("ListFiscalArchives = (%+v, %v)", list, err)
	}

	link, err := svc.FiscalArchiveLink(userCtx, gen.Archive.ID, storage)
	if err != nil || link.SHA256 != gen.Archive.SHA256 || link.DownloadURL == "" {
		t.Fatalf("FiscalArchiveLink = (%+v, %v)", link, err)
	}
	var n int
	var user string
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), max(user_id) FROM audit_logs
		WHERE merchant_id = $1 AND action = 'FISCAL_ARCHIVE_DOWNLOAD' AND resource_type = 'fiscal_archives' AND resource_id = $2
		  AND hash IS NOT NULL AND signature IS NOT NULL`,
		merchantID, strconv.FormatInt(gen.Archive.ID, 10)).Scan(&n, &user); err != nil || n != 1 || user != "itest-arch-user" {
		t.Fatalf("audit entry: n=%d user=%q err=%v", n, user, err)
	}

	// Autre établissement : archive invisible.
	otherCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "x", MerchantID: "itest-arch-other"})
	if _, err := svc.FiscalArchiveLink(otherCtx, gen.Archive.ID, storage); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("other merchant link: err = %v", err)
	}
	if other, err := svc.ListFiscalArchives(otherCtx); err != nil || len(other.Archives) != 0 {
		t.Fatalf("other merchant list = (%+v, %v)", other, err)
	}

}
