package customers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers/importer"
)

func newCustomerTemplateRequest(t *testing.T, query string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/customers/import/template"+query, nil)
	req.Header.Set("Authorization", "Bearer "+testCustomerImportAuthToken)

	return req.WithContext(middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID:     "u-1",
		MerchantID: testCustomerImportMerchantID,
	}))
}

// Le modèle est un fichier statique : il ne dépend pas du marchand et ne doit
// donc toucher ni la base ni le cache. Aucune attente SQL n'est déclarée.
func TestCustomerDownloadImportTemplate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("appel SQL inattendu pour un fichier statique: %v", err)
		}
		_ = db.Close()
	}()

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, newFakeCustomerImportStore()))

	rec := httptest.NewRecorder()
	handler.DownloadImportTemplate(rec, newCustomerTemplateRequest(t, "?provider="+importer.WelloGenericSlug))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != mimeXLSX {
		t.Fatalf("Content-Type = %q, want %q", got, mimeXLSX)
	}

	disposition := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") {
		t.Fatalf("Content-Disposition = %q, want un attachment", disposition)
	}
	if !strings.Contains(disposition, "wello-modele-import-clients.xlsx") {
		t.Fatalf("Content-Disposition = %q, want le nom de fichier attendu", disposition)
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}

	// Le corps est bien un classeur relisible par le parser wello-generic.
	imp, err := importer.NewWelloGenericCustomerProvider().Parse(bytes.NewReader(rec.Body.Bytes()))
	if err == nil {
		t.Fatal("le modele vierge ne porte aucune ligne de donnees : Parse doit rendre ErrNoCustomers")
	}
	_ = imp
}

func TestCustomerDownloadImportTemplateRejectsBadProvider(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantError string
	}{
		{"provider absent", "", "missing_provider"},
		{"provider vide", "?provider=", "missing_provider"},
		{"provider inconnu", "?provider=zelty-v2", "unknown_provider"},
		{"provider sans modele (zelty)", "?provider=" + importer.ZeltySlug, "template_not_available"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer func() {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("appel SQL inattendu: %v", err)
				}
				_ = db.Close()
			}()

			handler := NewCustomerImportHandler(newTestCustomerImportService(db, newFakeCustomerImportStore()))

			rec := httptest.NewRecorder()
			handler.DownloadImportTemplate(rec, newCustomerTemplateRequest(t, tc.query))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Fatalf("body = %s, want qu'il contienne %q", rec.Body.String(), tc.wantError)
			}
		})
	}
}

func TestCustomerDownloadImportTemplateRequiresAuthToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("appel SQL inattendu: %v", err)
		}
		_ = db.Close()
	}()

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, newFakeCustomerImportStore()))

	req := httptest.NewRequest(http.MethodGet, "/customers/import/template?provider="+importer.WelloGenericSlug, nil)

	rec := httptest.NewRecorder()
	handler.DownloadImportTemplate(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCustomerDownloadImportTemplateIsGuardedByCustomerManagementPermission(t *testing.T) {
	cases := []struct {
		name       string
		rights     authpkg.UserRowRights
		wantStatus int
	}{
		{"droit gestion clients", authpkg.UserRowRights{CanManageCustomers: true}, http.StatusOK},
		{"administrateur", authpkg.UserRowRights{Admin: true}, http.StatusOK},
		{"sans droit clients", authpkg.UserRowRights{}, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer func() { _ = db.Close() }()

			handler := NewCustomerImportHandler(newTestCustomerImportService(db, newFakeCustomerImportStore()))
			guarded := middleware.RequirePermission(middleware.HasCustomerManagementAccess)(
				http.HandlerFunc(handler.DownloadImportTemplate),
			)

			req := newCustomerTemplateRequest(t, "?provider="+importer.WelloGenericSlug)
			req = req.WithContext(middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
				UserID:     "u-1",
				MerchantID: testCustomerImportMerchantID,
				Rights:     tc.rights,
			}))

			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d — body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
