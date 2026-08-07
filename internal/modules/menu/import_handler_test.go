package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/menu/importer"
)

const (
	testMerchantID   = "m-import-1"
	testAuthToken    = "token-import-1"
	zeltyFixture2026 = "Zelty Menu OK Pizza - Devant-les-Ponts - 2026-08-04.xlsx"
)

// fakePreviewStore remplace Redis. Le client réel est une struct concrète à
// champ privé : aucune substitution n'est possible sans passer par l'interface
// que le service déclare.
type fakePreviewStore struct {
	values map[string]string
	ttls   map[string]time.Duration
	fail   bool
}

func newFakePreviewStore() *fakePreviewStore {
	return &fakePreviewStore{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (s *fakePreviewStore) Set(_ context.Context, key, value string, ttl time.Duration) bool {
	if s.fail {
		return false
	}
	s.values[key] = value
	s.ttls[key] = ttl
	return true
}

func (s *fakePreviewStore) Get(_ context.Context, key string) (string, bool) {
	value, ok := s.values[key]
	return value, ok
}

// newImportPreviewHandler câble le handler sur une base simulée.
//
// Seuls des ExpectQuery sont déclarés : sqlmock échoue sur tout appel non
// attendu, donc le moindre INSERT, UPDATE ou BEGIN fait tomber le test. C'est
// l'assertion « la preview n'écrit rien », vérifiée par construction.
func newImportPreviewHandler(t *testing.T) (*ImportHandler, *fakePreviewStore, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	expectImportPreviewLookups(mock)

	store := newFakePreviewStore()
	service := NewImportService(NewMenuRepository(db, nil), importer.DefaultRegistry(), store)

	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("attentes SQL non satisfaites (ou écriture inattendue): %v", err)
		}
		_ = db.Close()
	}
	return NewImportHandler(service), store, cleanup
}

// expectImportPreviewLookups décrit les huit lectures de la preview, dans
// l'ordre où LoadImportPreviewLookups les émet.
func expectImportPreviewLookups(mock sqlmock.Sqlmock) {
	tvaRates := sqlmock.NewRows([]string{"tva_id", "delivery_type", "tva_rate"})
	tvaID := 1
	for _, channel := range []importer.TvaChannel{importer.TvaChannelIn, importer.TvaChannelTakeAway, importer.TvaChannelDelivery} {
		for _, rate := range []float64{5.5, 10, 20} {
			// delivery_type est un varchar en base : '0', '1', '3'.
			tvaRates.AddRow(tvaID, strconv.Itoa(int(channel)), rate)
			tvaID++
		}
	}
	mock.ExpectQuery("SELECT tva_id, delivery_type, tva_rate").WillReturnRows(tvaRates)

	mock.ExpectQuery("SELECT categ_id, merchant_categ_id, categ_name").
		WithArgs(testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"categ_id", "merchant_categ_id", "categ_name"}).
			AddRow(41, "41", "NOS PIZZA"))

	mock.ExpectQuery("SELECT tag_id, name").
		WithArgs(testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"tag_id", "name"}))

	mock.ExpectQuery("SELECT product_id, name").
		WithArgs(testMerchantID).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "name"}))

	for _, table := range []string{"import_products_mapping", "import_categories_mapping"} {
		mock.ExpectQuery("SELECT external_id, wello_id FROM " + table).
			WillReturnRows(sqlmock.NewRows([]string{"external_id", "wello_id"}))
	}
	for _, table := range []string{"import_tags_mapping", "import_attributes_mapping"} {
		mock.ExpectQuery("SELECT external_id, wello_id FROM " + table).
			WillReturnRows(sqlmock.NewRows([]string{"external_id", "wello_id"}))
	}
}

func newImportRequest(t *testing.T, body *bytes.Buffer, contentType string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/menu/import/preview", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+testAuthToken)

	ctx := middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
		UserID:     "u-1",
		MerchantID: testMerchantID,
	})
	return req.WithContext(ctx)
}

func decodePreviewResponse(t *testing.T, rec *httptest.ResponseRecorder) *importer.PreviewResult {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — body=%s", rec.Code, rec.Body.String())
	}

	var envelope struct {
		ID   string                  `json:"id"`
		Data *importer.PreviewResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("réponse illisible: %v — body=%s", err, rec.Body.String())
	}
	if envelope.ID != "menu.preview_import" {
		t.Fatalf("id = %q, want %q", envelope.ID, "menu.preview_import")
	}
	if envelope.Data == nil {
		t.Fatal("data absent de la réponse")
	}
	return envelope.Data
}

// assertSnapshotStored relit le snapshot par la clé conventionnelle et rend le
// contenu déposé.
func assertSnapshotStored(t *testing.T, store *fakePreviewStore, token string) *importer.PreviewSnapshot {
	t.Helper()

	key := helpers.GetMenuImportPreviewKey(testMerchantID, token)
	payload, ok := store.Get(context.Background(), key)
	if !ok {
		t.Fatalf("aucun snapshot sous la clé %q", key)
	}
	if got := store.ttls[key]; got != 30*time.Minute {
		t.Fatalf("TTL = %v, want 30m", got)
	}

	snapshot, err := importer.DecodePreviewSnapshot(payload)
	if err != nil {
		t.Fatalf("snapshot illisible: %v", err)
	}
	if snapshot.MerchantID != testMerchantID {
		t.Fatalf("snapshot.MerchantID = %q, want %q", snapshot.MerchantID, testMerchantID)
	}
	return snapshot
}

func TestPreviewImportMultipart(t *testing.T) {
	handler, store, cleanup := newImportPreviewHandler(t)
	defer cleanup()

	fixture, err := os.ReadFile(filepath.Join("importer", "testdata", zeltyFixture2026))
	if err != nil {
		t.Fatalf("lecture de la fixture: %v", err)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("provider", importer.ZeltySlug); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	part, err := form.CreateFormFile("file", zeltyFixture2026)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(fixture); err != nil {
		t.Fatalf("écriture du fichier: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newImportRequest(t, &body, form.FormDataContentType()))

	result := decodePreviewResponse(t, rec)

	if result.Provider != importer.ZeltySlug {
		t.Fatalf("provider = %q, want %q", result.Provider, importer.ZeltySlug)
	}
	if result.Summary.ProductsToCreate != 141 {
		t.Fatalf("ProductsToCreate = %d, want 141", result.Summary.ProductsToCreate)
	}
	if len(result.Tags) != 19 {
		t.Fatalf("libellés = %d, want 19", len(result.Tags))
	}
	if len(result.Attributes) != 12 {
		t.Fatalf("groupes d'options = %d, want 12", len(result.Attributes))
	}
	if result.Summary.UnresolvedTvaRates != 0 {
		t.Fatalf("UnresolvedTvaRates = %d, want 0", result.Summary.UnresolvedTvaRates)
	}
	// La catégorie NOS PIZZA existe déjà chez ce marchand : elle doit être
	// réutilisée, pas recréée en homonyme.
	if result.Summary.CategoriesReused != 1 {
		t.Fatalf("CategoriesReused = %d, want 1", result.Summary.CategoriesReused)
	}

	if result.Token == "" {
		t.Fatal("token absent de la réponse")
	}
	if result.ExpiresAt == "" {
		t.Fatal("expires_at absent de la réponse")
	}
	if _, err := time.Parse(time.RFC3339, result.ExpiresAt); err != nil {
		t.Fatalf("expires_at = %q, want du RFC3339: %v", result.ExpiresAt, err)
	}

	snapshot := assertSnapshotStored(t, store, result.Token)
	if len(snapshot.Import.Products) != 141 {
		t.Fatalf("snapshot: %d produits, want 141", len(snapshot.Import.Products))
	}
	if len(snapshot.Decisions.TvaMapping) == 0 {
		t.Fatal("snapshot: mapping de TVA vide")
	}
}

func TestPreviewImportJSON(t *testing.T) {
	handler, store, cleanup := newImportPreviewHandler(t)
	defer cleanup()

	tenPercent := 10.0
	payload, err := json.Marshal(ImportPreviewJSONRequest{
		Products: []ImportPreviewJSONProduct{
			{
				Name: "Margherita", Description: "Tomate, mozzarella", Category: "NOS PIZZA",
				PriceIn: 990, PriceTakeAway: 990, PriceDelivery: 1150,
				TvaRateIn: &tenPercent, TvaRateTakeAway: &tenPercent, TvaRateDelivery: &tenPercent,
				Tags: []string{"Signature"},
			},
			{Name: "Calzone", Category: "NOS PIZZA", PriceIn: 1290, TvaRateIn: &tenPercent},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newImportRequest(t, bytes.NewBuffer(payload), "application/json"))

	result := decodePreviewResponse(t, rec)

	if result.Provider != importer.ManualSlug {
		t.Fatalf("provider = %q, want %q", result.Provider, importer.ManualSlug)
	}
	if result.Summary.ProductsToCreate != 2 {
		t.Fatalf("ProductsToCreate = %d, want 2", result.Summary.ProductsToCreate)
	}

	// La catégorie est explicite ici, contrairement à Zelty.
	margherita := result.Products[0]
	if margherita.CategorySource != importer.CategorySourceExplicit {
		t.Fatalf("CategorySource = %q, want %q", margherita.CategorySource, importer.CategorySourceExplicit)
	}
	if margherita.NeedsCategory {
		t.Fatal("NeedsCategory = true alors que la catégorie est saisie")
	}
	if margherita.Channels.In.TvaID != 2 || !margherita.Channels.In.Resolved {
		t.Fatalf("canal sur place = %+v, want tva_id 2 résolu", margherita.Channels.In)
	}
	if margherita.Channels.Delivery.Price != 1150 {
		t.Fatalf("prix livraison = %d, want 1150", margherita.Channels.Delivery.Price)
	}
	// NOS PIZZA existe déjà : la saisie doit s'y rattacher.
	if len(result.Categories) != 1 || result.Categories[0].Action != importer.ActionReuseExisting {
		t.Fatalf("catégories = %+v, want une réutilisation", result.Categories)
	}

	snapshot := assertSnapshotStored(t, store, result.Token)
	if snapshot.Provider != importer.ManualSlug {
		t.Fatalf("snapshot.Provider = %q, want %q", snapshot.Provider, importer.ManualSlug)
	}
	if len(snapshot.Import.Products) != 2 {
		t.Fatalf("snapshot: %d produits, want 2", len(snapshot.Import.Products))
	}
}

// Les erreurs d'entrée sont rejetées avant toute lecture : aucune attente SQL
// n'est déclarée dans ces cas, donc une requête partirait en échec de test.
func TestPreviewImportRejectsBadInput(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        func(t *testing.T) *bytes.Buffer
		wantStatus  int
		wantError   string
	}{
		{
			name:        "provider inconnu",
			contentType: "",
			body: func(t *testing.T) *bytes.Buffer {
				return multipartBody(t, "zelty-v2", []byte("peu importe"))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "unknown_provider",
		},
		{
			name:        "provider manquant",
			contentType: "",
			body: func(t *testing.T) *bytes.Buffer {
				return multipartBody(t, "", []byte("peu importe"))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "missing_provider",
		},
		{
			name:        "fichier illisible",
			contentType: "",
			body: func(t *testing.T) *bytes.Buffer {
				return multipartBody(t, importer.ZeltySlug, []byte("Nom;Prix\nMargherita;9,90\n"))
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
			name:        "aucun produit saisi",
			contentType: "application/json",
			body: func(t *testing.T) *bytes.Buffer {
				return bytes.NewBufferString(`{"products":[]}`)
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "no_products",
		},
		{
			name:        "produit sans nom",
			contentType: "application/json",
			body: func(t *testing.T) *bytes.Buffer {
				return bytes.NewBufferString(`{"products":[{"name":"","price":990}]}`)
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
					t.Errorf("appel SQL inattendu sur une entrée rejetée: %v", err)
				}
				_ = db.Close()
			}()

			service := NewImportService(NewMenuRepository(db, nil), importer.DefaultRegistry(), newFakePreviewStore())
			handler := NewImportHandler(service)

			body := tc.body(t)
			contentType := tc.contentType
			if contentType == "" {
				contentType = multipartContentType
			}

			rec := httptest.NewRecorder()
			handler.PreviewImport(rec, newImportRequest(t, body, contentType))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d — body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantError) {
				t.Fatalf("body = %s, want qu'il contienne %q", rec.Body.String(), tc.wantError)
			}
		})
	}
}

func TestPreviewImportRequiresToken(t *testing.T) {
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

	handler := NewImportHandler(NewImportService(NewMenuRepository(db, nil), importer.DefaultRegistry(), newFakePreviewStore()))

	req := httptest.NewRequest(http.MethodPost, "/menu/import/preview", bytes.NewBufferString(`{"products":[]}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// Un dépôt de snapshot en échec ne doit pas produire une preview : le token
// serait inexploitable au commit.
func TestPreviewImportFailsWhenSnapshotCannotBeStored(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()

	expectImportPreviewLookups(mock)

	store := newFakePreviewStore()
	store.fail = true
	handler := NewImportHandler(NewImportService(NewMenuRepository(db, nil), importer.DefaultRegistry(), store))

	tenPercent := 10.0
	payload, _ := json.Marshal(ImportPreviewJSONRequest{
		Products: []ImportPreviewJSONProduct{{Name: "Margherita", Category: "Pizzas", PriceIn: 990, TvaRateIn: &tenPercent}},
	})

	rec := httptest.NewRecorder()
	handler.PreviewImport(rec, newImportRequest(t, bytes.NewBuffer(payload), "application/json"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — body=%s", rec.Code, rec.Body.String())
	}
}

const multipartContentType = "multipart/form-data; boundary=import-test-boundary"

func multipartBody(t *testing.T, provider string, content []byte) *bytes.Buffer {
	t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.SetBoundary("import-test-boundary"); err != nil {
		t.Fatalf("SetBoundary: %v", err)
	}
	if provider != "" {
		if err := form.WriteField("provider", provider); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	part, err := form.CreateFormFile("file", "menu.xlsx")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("écriture: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return &body
}

// La route est la seule du bloc /menu à porter un contrôle RBAC. Ce test
// compose le handler avec le middleware exactement comme cmd/api/routes.go, et
// vérifie qu'un utilisateur sans droit menu n'atteint jamais le service — donc
// pas la base non plus.
func TestPreviewImportIsGuardedByMenuPermission(t *testing.T) {
	cases := []struct {
		name       string
		rights     authpkg.UserRowRights
		wantStatus int
	}{
		{"droit menu", authpkg.UserRowRights{CanManageMenu: true}, http.StatusBadRequest},
		{"administrateur", authpkg.UserRowRights{Admin: true}, http.StatusBadRequest},
		{"sans droit menu", authpkg.UserRowRights{}, http.StatusForbidden},
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

			handler := NewImportHandler(
				NewImportService(NewMenuRepository(db, nil), importer.DefaultRegistry(), newFakePreviewStore()),
			)
			guarded := middleware.RequirePermission(middleware.HasMenuAccess)(
				http.HandlerFunc(handler.PreviewImport),
			)

			// Un corps volontairement vide : un utilisateur autorisé va jusqu'au
			// service et se fait refuser en 400, un utilisateur sans droit est
			// arrêté avant, en 403.
			req := newImportRequest(t, bytes.NewBufferString(`{"products":[]}`), "application/json")
			req = req.WithContext(middleware.WithUser(req.Context(), &authpkg.UserLoginRow{
				UserID:     "u-1",
				MerchantID: testMerchantID,
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
