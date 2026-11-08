package appreleases

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"welloresto-api/internal/models"

	"github.com/go-chi/chi/v5"
)

type fakeRepo struct {
	byAppID map[string]LatestRelease
	err     error
	asked   []string
}

func (f *fakeRepo) LatestPublic(_ context.Context, appID string) (int, time.Time, string, error) {
	f.asked = append(f.asked, appID)
	if f.err != nil {
		return 0, time.Time{}, "", f.err
	}
	rel, ok := f.byAppID[appID]
	if !ok {
		return 0, time.Time{}, "", models.ErrNotFound
	}
	return rel.VersionCode, rel.ReleaseDate, rel.DownloadURL, nil
}

const caisseURL = "https://storage.welloresto.fr/apps/reception/app-reception-136.apk"

func newTestRouter(repo *fakeRepo) http.Handler {
	h := NewHandler(NewService(repo))
	r := chi.NewRouter()
	r.Get("/v1/public/apps/{app}/download", h.Download)
	return r
}

func caisseRepo() *fakeRepo {
	return &fakeRepo{byAppID: map[string]LatestRelease{
		"WR_RECEPTION": {VersionCode: 136, ReleaseDate: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), DownloadURL: caisseURL},
	}}
}

func do(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestService_MapsPublicNamesToAppIDs(t *testing.T) {
	cases := map[string]string{"caisse": "WR_RECEPTION", "borne": "kiosk", "cds": "cds", " CAISSE ": "WR_RECEPTION"}
	for name, wantAppID := range cases {
		repo := &fakeRepo{}
		_, _ = NewService(repo).Latest(context.Background(), name)
		if len(repo.asked) != 1 || repo.asked[0] != wantAppID {
			t.Errorf("Latest(%q) a interrogé %v, attendu [%s]", name, repo.asked, wantAppID)
		}
	}
}

func TestService_UnknownAppNeverReachesDatabase(t *testing.T) {
	// Les app_id internes ou de test ne doivent pas être exposés, même
	// passés tels quels dans l'URL.
	for _, name := range []string{"WR_RECEPTION", "WR_DELIVERY", "kiosk-test", "itest-app", ""} {
		repo := &fakeRepo{}
		_, err := NewService(repo).Latest(context.Background(), name)
		if !errors.Is(err, models.ErrNotFound) {
			t.Errorf("Latest(%q) err = %v, attendu ErrNotFound", name, err)
		}
		if len(repo.asked) != 0 {
			t.Errorf("Latest(%q) a interrogé la base (%v)", name, repo.asked)
		}
	}
}

func TestDownload_UnknownAppIs404(t *testing.T) {
	rec := do(t, newTestRouter(caisseRepo()), "/v1/public/apps/inconnue/download")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, attendu 404", rec.Code)
	}
}

func TestDownload_RedirectsToApkWithoutCache(t *testing.T) {
	rec := do(t, newTestRouter(caisseRepo()), "/v1/public/apps/caisse/download")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, attendu 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != caisseURL {
		t.Errorf("Location = %q, attendu %q", got, caisseURL)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, attendu no-store", got)
	}
}

func TestDownload_NoVersionIsReadable404(t *testing.T) {
	rec := do(t, newTestRouter(caisseRepo()), "/v1/public/apps/cds/download")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, attendu 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q : le visiteur doit lire un message, pas du JSON", ct)
	}
	if rec.Header().Get("Location") != "" {
		t.Error("aucune redirection attendue")
	}
}

func TestDownload_DatabaseErrorIs500(t *testing.T) {
	repo := &fakeRepo{err: errors.New("connexion perdue")}
	rec := do(t, newTestRouter(repo), "/v1/public/apps/caisse/download")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, attendu 500", rec.Code)
	}
}
