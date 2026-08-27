package customers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers/importer"
	"welloresto-api/internal/permission"
)

func newCommitRequest(t *testing.T, body CommitRequest) *http.Request {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/customers/import/commit", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testCustomerImportAuthToken)

	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID:     "u-1",
		MerchantID: testCustomerImportMerchantID,
	})
	return req.WithContext(ctx)
}

// storeWithCustomerSnapshot dépose un snapshot prêt à être consommé par le
// commit, comme la preview l'aurait fait.
func storeWithCustomerSnapshot(t *testing.T, merchantID string, snapshot *importer.PreviewSnapshot) *fakeCustomerImportStore {
	t.Helper()

	payload, err := snapshot.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	store := newFakeCustomerImportStore()
	store.values["customer:import:preview:"+merchantID+":"+"tok-1"] = payload
	return store
}

func TestCustomerCommitImportHappyPathCreate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("attentes SQL non satisfaites: %v", err)
		}
		_ = db.Close()
	}()

	// Email ET telephone renseignes : les trois lookups (email, telephone,
	// mapping) s'executent tous, dans cet ordre, comme l'attend
	// expectCustomerImportPreviewLookups.
	snapshot := &importer.PreviewSnapshot{
		MerchantID: testCustomerImportMerchantID,
		Provider:   importer.ZeltySlug,
		Customers: []importer.CanonicalCustomer{
			{ExternalID: "Z1", Name: "Jean Dupont", Email: strPtr("jean@example.com"), Phone: strPtr("+33612345678")},
		},
		Rows: []importer.PreviewRow{
			{ExternalID: "Z1", Status: importer.StatusCreate, Resolution: importer.ResolutionCreate},
		},
	}
	store := storeWithCustomerSnapshot(t, testCustomerImportMerchantID, snapshot)

	// Lookups frais : aucun rapprochement.
	expectCustomerImportPreviewLookups(mock)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO customer").WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec("INSERT INTO import_customers_mapping").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, store))

	rec := httptest.NewRecorder()
	handler.CommitImport(rec, newCommitRequest(t, CommitRequest{Token: "tok-1"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Data importer.CommitSummary `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("reponse illisible: %v — body=%s", err, rec.Body.String())
	}
	if envelope.Data.Created != 1 {
		t.Fatalf("Created = %d, want 1", envelope.Data.Created)
	}

	key := "customer:import:preview:" + testCustomerImportMerchantID + ":tok-1"
	if _, stillThere := store.values[key]; stillThere {
		t.Fatal("le token doit etre consomme apres un commit reussi")
	}
	found := false
	for _, d := range store.deleted {
		if d == key {
			found = true
		}
	}
	if !found {
		t.Fatalf("Delete n'a pas ete appele sur %q", key)
	}
}

func TestCustomerCommitImportRequiresAuthToken(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/customers/import/commit", bytes.NewBufferString(`{"token":"tok-1"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.CommitImport(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCustomerCommitImportInvalidBody(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/customers/import/commit", bytes.NewBufferString("{pas du json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testCustomerImportAuthToken)
	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{UserID: "u-1", MerchantID: testCustomerImportMerchantID})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.CommitImport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_body") {
		t.Fatalf("body = %s, want invalid_body", rec.Body.String())
	}
}

func TestCustomerCommitImportMissingPreviewToken(t *testing.T) {
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
	handler.CommitImport(rec, newCommitRequest(t, CommitRequest{}))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_preview_token") {
		t.Fatalf("body = %s, want missing_preview_token", rec.Body.String())
	}
}

// Un token inconnu, expiré ou déjà consommé donne 410 : dans les trois cas le
// client doit relancer un import, pas réessayer celui-ci. Aucune lecture ni
// écriture SQL n'est attendue : le snapshot est introuvable avant tout accès
// à la base.
func TestCustomerCommitImportGoneOnUnknownToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("appel SQL inattendu sur un token inconnu: %v", err)
		}
		_ = db.Close()
	}()

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, newFakeCustomerImportStore()))

	rec := httptest.NewRecorder()
	handler.CommitImport(rec, newCommitRequest(t, CommitRequest{Token: "jamais-vu"}))

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410 — body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "preview_expired") {
		t.Fatalf("body = %s, want preview_expired", rec.Body.String())
	}
}

// Un snapshot déposé pour un autre marchand ne doit pas être exploitable,
// même si le token est deviné.
func TestCustomerCommitImportRejectsSnapshotOfAnotherMerchant(t *testing.T) {
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

	snapshot := &importer.PreviewSnapshot{
		MerchantID: "un-autre-marchand",
		Provider:   importer.ZeltySlug,
		Customers:  []importer.CanonicalCustomer{{ExternalID: "Z1"}},
		Rows:       []importer.PreviewRow{{ExternalID: "Z1", Status: importer.StatusCreate, Resolution: importer.ResolutionCreate}},
	}
	store := storeWithCustomerSnapshot(t, testCustomerImportMerchantID, snapshot)

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, store))

	rec := httptest.NewRecorder()
	handler.CommitImport(rec, newCommitRequest(t, CommitRequest{Token: "tok-1"}))

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410 — body=%s", rec.Code, rec.Body.String())
	}
}

// Un lot avec un blocage part en 422 SANS AUCUNE ECRITURE : seules des
// ExpectQuery sont declarees (les lookups frais), aucun ExpectExec — le
// moindre INSERT/UPDATE ferait echouer le test.
func TestCustomerCommitImportBlockersPreventAnyWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("ecriture inattendue alors que le lot est bloque: %v", err)
		}
		_ = db.Close()
	}()

	snapshot := &importer.PreviewSnapshot{
		MerchantID: testCustomerImportMerchantID,
		Provider:   importer.ZeltySlug,
		Customers:  []importer.CanonicalCustomer{{ExternalID: "Z1", Email: strPtr("jean@example.com"), Phone: strPtr("+33612345678")}},
		Rows:       []importer.PreviewRow{{ExternalID: "Z1", Status: importer.StatusCreate, Resolution: importer.ResolutionCreate}},
	}
	store := storeWithCustomerSnapshot(t, testCustomerImportMerchantID, snapshot)

	// Lookups frais : SEULEMENT des lectures, jamais d'ecriture.
	expectCustomerImportPreviewLookups(mock)

	handler := NewCustomerImportHandler(newTestCustomerImportService(db, store))

	// Decision "recreate" sur une ligne jamais mappee : invalid_decision.
	rec := httptest.NewRecorder()
	handler.CommitImport(rec, newCommitRequest(t, CommitRequest{
		Token:     "tok-1",
		Decisions: []importer.CommitRowDecision{{ExternalID: "Z1", Resolution: importer.ResolutionRecreate}},
	}))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 — body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Error    string                   `json:"error"`
			Blockers []importer.CommitBlocker `json:"blockers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("reponse illisible: %v — body=%s", err, rec.Body.String())
	}
	if envelope.Data.Error != "import_not_committable" {
		t.Fatalf("error = %q, want import_not_committable", envelope.Data.Error)
	}
	if len(envelope.Data.Blockers) == 0 {
		t.Fatal("aucun blocage liste")
	}
	if envelope.Data.Blockers[0].Code != importer.BlockerInvalidDecision {
		t.Fatalf("code = %q, want %q", envelope.Data.Blockers[0].Code, importer.BlockerInvalidDecision)
	}

	// Le token n'est pas consomme : l'utilisateur corrige et rejoue.
	if len(store.deleted) != 0 {
		t.Fatalf("token consomme malgre le refus: %v", store.deleted)
	}
}

func TestCustomerCommitImportIsGuardedByCustomerManagementPermission(t *testing.T) {
	cases := []struct {
		name       string
		rights     authpkg.UserRowRights
		wantStatus int
	}{
		{"droit gestion clients", authpkg.UserRowRights{CanManageCustomers: true}, http.StatusBadRequest},
		{"administrateur", authpkg.UserRowRights{Admin: true}, http.StatusBadRequest},
		{"sans droit clients", authpkg.UserRowRights{}, http.StatusForbidden},
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
			guarded := middleware.RequirePermission(permission.CustomersManage)(
				http.HandlerFunc(handler.CommitImport),
			)

			// Corps avec un token vide : un utilisateur autorise va jusqu'au
			// service et se fait refuser en 400 (token manquant), un
			// utilisateur sans droit est arrete avant, en 403.
			req := newCommitRequest(t, CommitRequest{})
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
