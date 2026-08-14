package customers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers/importer"
)

const (
	testCustomerImportMerchantID = "m-cust-import-1"
	testCustomerImportAuthToken  = "token-cust-import-1"
)

// fakeCustomerImportStore remplace Redis. Le client réel est une struct
// concrète à champ privé : aucune substitution n'est possible sans passer par
// l'interface que le service déclare.
type fakeCustomerImportStore struct {
	values map[string]string
	ttls   map[string]time.Duration
	fail   bool

	deleted []string
}

func newFakeCustomerImportStore() *fakeCustomerImportStore {
	return &fakeCustomerImportStore{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (s *fakeCustomerImportStore) Set(_ context.Context, key, value string, ttl time.Duration) bool {
	if s.fail {
		return false
	}
	s.values[key] = value
	s.ttls[key] = ttl
	return true
}

func (s *fakeCustomerImportStore) Get(_ context.Context, key string) (string, bool) {
	value, ok := s.values[key]
	return value, ok
}

func (s *fakeCustomerImportStore) Delete(_ context.Context, key string) bool {
	_, existed := s.values[key]
	delete(s.values, key)
	s.deleted = append(s.deleted, key)
	return existed
}

// newTestCustomerImportService câble le service sur une base simulée. Le
// dépôt tient les deux rôles (lecture et écriture), comme en production.
func newTestCustomerImportService(db *sql.DB, store customerImportPreviewStore) *CustomerImportService {
	repo := NewCustomerRepository(db)
	return NewCustomerImportService(repo, repo, db, importer.DefaultRegistry(), store)
}

// expectCustomerImportPreviewLookups déclare les trois lectures batchées de
// la preview, sans contraindre les arguments (le contenu exact dépend du
// fichier/de la saisie testée). Rends-les vides : aucun rapprochement, tout
// devient "create" — ce que la plupart des cas testés ici veulent.
func expectCustomerImportPreviewLookups(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT LOWER").
		WillReturnRows(sqlmock.NewRows([]string{"lower_email", "customer_id"}))
	mock.ExpectQuery("SELECT customer_tel, customer_id").
		WillReturnRows(sqlmock.NewRows([]string{"customer_tel", "customer_id"}))
	mock.ExpectQuery("SELECT m.external_id").
		WillReturnRows(sqlmock.NewRows([]string{"external_id", "wello_id", "target_exists"}))
}

func newCustomerImportPreviewHandler(t *testing.T) (*CustomerImportHandler, *fakeCustomerImportStore, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	expectCustomerImportPreviewLookups(mock)

	store := newFakeCustomerImportStore()
	service := newTestCustomerImportService(db, store)

	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("attentes SQL non satisfaites (ou ecriture inattendue): %v", err)
		}
		_ = db.Close()
	}
	return NewCustomerImportHandler(service), store, cleanup
}

func newCustomerImportRequest(t *testing.T, body *bytes.Buffer, contentType string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/customers/import/preview", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+testCustomerImportAuthToken)

	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID:     "u-1",
		MerchantID: testCustomerImportMerchantID,
	})
	return req.WithContext(ctx)
}

func decodeCustomerPreviewResponse(t *testing.T, rec *httptest.ResponseRecorder) *importer.PreviewResult {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		ID   string                  `json:"id"`
		Data *importer.PreviewResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("reponse illisible: %v — body=%s", err, rec.Body.String())
	}
	if envelope.ID != "customers.preview_import" {
		t.Fatalf("id = %q, want %q", envelope.ID, "customers.preview_import")
	}
	if envelope.Data == nil {
		t.Fatal("data absent de la reponse")
	}
	return envelope.Data
}

func TestCustomerPreviewImportJSON(t *testing.T) {
	handler, store, cleanup := newCustomerImportPreviewHandler(t)
	defer cleanup()

	payload, err := json.Marshal(ImportPreviewJSONRequest{
		Customers: []ImportPreviewJSONCustomer{
			{Name: "Jean Dupont", FirstName: "Jean", LastName: "Dupont", Email: "jean.dupont@example.com", Phone: "0612345678"},
			{Name: "Alice Martin", Email: "alice.martin@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newCustomerImportRequest(t, bytes.NewBuffer(payload), "application/json"))

	result := decodeCustomerPreviewResponse(t, rec)

	if result.Summary.ToCreate != 2 {
		t.Fatalf("ToCreate = %d, want 2", result.Summary.ToCreate)
	}
	if result.Summary.Total != 2 {
		t.Fatalf("Total = %d, want 2", result.Summary.Total)
	}
	if result.Token == "" {
		t.Fatal("token absent de la reponse")
	}

	key := "customer:import:preview:" + testCustomerImportMerchantID + ":" + result.Token
	storedPayload, ok := store.values[key]
	if !ok {
		t.Fatalf("aucun snapshot sous la cle %q", key)
	}
	if got := store.ttls[key]; got != 30*time.Minute {
		t.Fatalf("TTL = %v, want 30m", got)
	}

	snapshot, err := importer.DecodePreviewSnapshot(storedPayload)
	if err != nil {
		t.Fatalf("snapshot illisible: %v", err)
	}
	if snapshot.MerchantID != testCustomerImportMerchantID {
		t.Fatalf("snapshot.MerchantID = %q, want %q", snapshot.MerchantID, testCustomerImportMerchantID)
	}
	if snapshot.Provider != importer.ManualSlug {
		t.Fatalf("snapshot.Provider = %q, want %q", snapshot.Provider, importer.ManualSlug)
	}
	if len(snapshot.Customers) != 2 {
		t.Fatalf("snapshot: %d clients, want 2", len(snapshot.Customers))
	}
}

func TestCustomerPreviewImportMultipart(t *testing.T) {
	handler, store, cleanup := newCustomerImportPreviewHandler(t)
	defer cleanup()

	csvContent := "\xEF\xBB\xBFID,Nom,Prenom,Mail,Telephone\nZ1,Dupont,Jean,jean@example.com,0612345678\n"

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("provider", importer.ZeltySlug); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	part, err := form.CreateFormFile("file", "zelty.csv")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(csvContent)); err != nil {
		t.Fatalf("ecriture: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newCustomerImportRequest(t, &body, form.FormDataContentType()))

	result := decodeCustomerPreviewResponse(t, rec)

	if result.Summary.Total != 1 || result.Summary.ToCreate != 1 {
		t.Fatalf("Summary = %+v, want Total=1 ToCreate=1", result.Summary)
	}
	if result.Rows[0].ExternalID != "Z1" {
		t.Fatalf("ExternalID = %q, want Z1", result.Rows[0].ExternalID)
	}
	if result.Token == "" {
		t.Fatal("token absent")
	}
	_ = store
}

// Les erreurs d'entrée sont rejetées avant toute lecture SQL : aucune
// attente n'est déclarée dans ces cas, donc une requête partirait en échec de
// test.
func TestCustomerPreviewImportRejectsBadInput(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        func(t *testing.T) *bytes.Buffer
		wantStatus  int
		wantError   string
	}{
		{
			name: "provider inconnu",
			body: func(t *testing.T) *bytes.Buffer {
				return customerMultipartBody(t, "zelty-v2", []byte("peu importe"))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "unknown_provider",
		},
		{
			name: "provider manquant",
			body: func(t *testing.T) *bytes.Buffer {
				return customerMultipartBody(t, "", []byte("peu importe"))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "missing_provider",
		},
		{
			name: "fichier illisible",
			body: func(t *testing.T) *bytes.Buffer {
				return customerMultipartBody(t, importer.ZeltySlug, []byte("pas un CSV valide\x00\x01\x02"))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_file",
		},
		{
			name:        "corps JSON invalide",
			contentType: "application/json",
			body: func(t *testing.T) *bytes.Buffer {
				return bytes.NewBufferString("{pas du json")
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_body",
		},
		{
			name:        "aucun client saisi",
			contentType: "application/json",
			body: func(t *testing.T) *bytes.Buffer {
				return bytes.NewBufferString(`{"customers":[]}`)
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "no_customers",
		},
		{
			name:        "client sans nom ni contact",
			contentType: "application/json",
			body: func(t *testing.T) *bytes.Buffer {
				return bytes.NewBufferString(`{"customers":[{"name":""}]}`)
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_file_content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer func() {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("appel SQL inattendu sur une entree rejetee: %v", err)
				}
				_ = db.Close()
			}()

			service := newTestCustomerImportService(db, newFakeCustomerImportStore())
			handler := NewCustomerImportHandler(service)

			body := tc.body(t)
			contentType := tc.contentType
			if contentType == "" {
				contentType = customerMultipartContentType
			}

			rec := httptest.NewRecorder()
			handler.PreviewImport(rec, newCustomerImportRequest(t, body, contentType))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d — body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Fatalf("body = %s, want qu'il contienne %q", rec.Body.String(), tc.wantError)
			}
		})
	}
}

func TestCustomerPreviewImportRequiresToken(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/customers/import/preview", bytes.NewBufferString(`{"customers":[]}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// Un dépôt de snapshot en échec ne doit pas produire une preview : le token
// serait inexploitable au commit.
func TestCustomerPreviewImportFailsWhenSnapshotCannotBeStored(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	expectCustomerImportPreviewLookups(mock)

	store := newFakeCustomerImportStore()
	store.fail = true
	handler := NewCustomerImportHandler(newTestCustomerImportService(db, store))

	payload, _ := json.Marshal(ImportPreviewJSONRequest{
		Customers: []ImportPreviewJSONCustomer{{Name: "Jean Dupont", Email: "jean@example.com", Phone: "0612345678"}},
	})

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newCustomerImportRequest(t, bytes.NewBuffer(payload), "application/json"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — body=%s", rec.Code, rec.Body.String())
	}
}

// La preview n'écrit jamais en SQL : seules des ExpectQuery sont déclarées
// dans tous les tests de ce fichier. sqlmock échoue sur tout ExpectExec non
// déclaré, donc le moindre INSERT/UPDATE/DELETE ferait tomber l'ensemble des
// tests ci-dessus par construction — ce test documente explicitement
// l'invariant en le vérifiant une nouvelle fois sur le chemin nominal.
func TestCustomerPreviewImportWritesNoSQL(t *testing.T) {
	handler, _, cleanup := newCustomerImportPreviewHandler(t)
	defer cleanup()

	// Email ET téléphone renseignés : les trois lookups (email, téléphone,
	// mapping) doivent tous s'exécuter, dans cet ordre — c'est ce que
	// newCustomerImportPreviewHandler attend via expectCustomerImportPreviewLookups.
	payload, _ := json.Marshal(ImportPreviewJSONRequest{
		Customers: []ImportPreviewJSONCustomer{{Name: "Jean Dupont", Email: "jean@example.com", Phone: "0612345678"}},
	})

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newCustomerImportRequest(t, bytes.NewBuffer(payload), "application/json"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body=%s", rec.Code, rec.Body.String())
	}
}

func TestCustomerPreviewImportIsGuardedByCustomerManagementPermission(t *testing.T) {
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
			guarded := middleware.RequirePermission(middleware.HasCustomerManagementAccess)(
				http.HandlerFunc(handler.PreviewImport),
			)

			// Un corps volontairement vide : un utilisateur autorisé va
			// jusqu'au service et se fait refuser en 400 (aucun client), un
			// utilisateur sans droit est arrêté avant, en 403.
			req := newCustomerImportRequest(t, bytes.NewBufferString(`{"customers":[]}`), "application/json")
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

const customerMultipartContentType = "multipart/form-data; boundary=customer-import-test-boundary"

func customerMultipartBody(t *testing.T, provider string, content []byte) *bytes.Buffer {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.SetBoundary("customer-import-test-boundary"); err != nil {
		t.Fatalf("SetBoundary: %v", err)
	}
	if provider != "" {
		if err := form.WriteField("provider", provider); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := form.CreateFormFile("file", "customers.csv")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("ecriture: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return &body
}
