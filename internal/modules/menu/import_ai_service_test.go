package menu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/menu/importer"
)

// ---- faux objets ----

type fakeAIDraftStore struct {
	mu        sync.Mutex
	drafts    map[string]*AIDraft
	overrides map[string]int
	purged    []string
}

func newFakeAIDraftStore() *fakeAIDraftStore {
	return &fakeAIDraftStore{drafts: map[string]*AIDraft{}, overrides: map[string]int{}}
}

func (f *fakeAIDraftStore) copyOf(d *AIDraft) *AIDraft {
	c := *d
	c.Pages = append([]AIDraftPage(nil), d.Pages...)
	return &c
}

func (f *fakeAIDraftStore) CreateDraft(_ context.Context, d *AIDraft) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, other := range f.drafts {
		if other.MerchantID == d.MerchantID && (other.Status == AIDraftPending || other.Status == AIDraftProcessing) {
			return ErrAIDraftAlreadyRunning
		}
	}
	f.drafts[d.ID] = f.copyOf(d)
	return nil
}

func (f *fakeAIDraftStore) GetDraft(_ context.Context, merchantID, id string) (*AIDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.drafts[id]
	if !ok || d.MerchantID != merchantID {
		return nil, ErrAIDraftNotFound
	}
	return f.copyOf(d), nil
}

func (f *fakeAIDraftStore) ListOpenDrafts(_ context.Context, merchantID string) ([]AIDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []AIDraft
	for _, d := range f.drafts {
		if d.MerchantID == merchantID && d.Status != AIDraftCommitted && d.Status != AIDraftExpired {
			out = append(out, *f.copyOf(d))
		}
	}
	return out, nil
}

func (f *fakeAIDraftStore) ClaimDraft(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.drafts[id]
	if d == nil || (d.Status != AIDraftPending && d.Status != AIDraftReady && d.Status != AIDraftFailed) {
		return false, nil
	}
	d.Status = AIDraftProcessing
	return true, nil
}

func (f *fakeAIDraftStore) SavePages(_ context.Context, id string, pages []AIDraftPage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drafts[id].Pages = append([]AIDraftPage(nil), pages...)
	return nil
}

func (f *fakeAIDraftStore) FinishDraft(_ context.Context, id, status string, consumes bool, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.drafts[id]
	d.Status, d.ConsumesCredit, d.Error = status, consumes, errMsg
	return nil
}

func (f *fakeAIDraftStore) MarkCommitted(_ context.Context, merchantID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d := f.drafts[id]; d != nil && d.MerchantID == merchantID {
		d.Status = AIDraftCommitted
	}
	return nil
}

func (f *fakeAIDraftStore) AbandonDraft(_ context.Context, merchantID, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.drafts[id]
	if d == nil || d.MerchantID != merchantID || d.Status == AIDraftProcessing {
		return false, nil
	}
	d.Status = AIDraftExpired
	return true, nil
}

func (f *fakeAIDraftStore) CountUsedCredits(_ context.Context, merchantID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.drafts {
		if d.MerchantID == merchantID && d.ConsumesCredit {
			n++
		}
	}
	return n, nil
}

func (f *fakeAIDraftStore) GetCreditsOverride(_ context.Context, merchantID string) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.overrides[merchantID]
	return c, ok, nil
}

func (f *fakeAIDraftStore) SetCreditsOverride(_ context.Context, merchantID string, credits int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[merchantID] = credits
	return nil
}

func (f *fakeAIDraftStore) FailStaleDrafts(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeAIDraftStore) ExpireDrafts(context.Context) (int64, error) { return 0, nil }

func (f *fakeAIDraftStore) ListDraftsToPurge(context.Context, time.Duration, int) ([]AIDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []AIDraft
	for _, d := range f.drafts {
		if d.Status == AIDraftCommitted && d.FilesPurgedAt == nil {
			out = append(out, *f.copyOf(d))
		}
	}
	return out, nil
}

func (f *fakeAIDraftStore) MarkFilesPurged(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	f.drafts[id].FilesPurgedAt = &now
	f.purged = append(f.purged, id)
	return nil
}

type fakeAIPhotoStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newFakeAIPhotoStore() *fakeAIPhotoStore { return &fakeAIPhotoStore{files: map[string][]byte{}} }

func (f *fakeAIPhotoStore) UploadPrivateFile(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	data, _ := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = data
	return "https://r2/" + key, nil
}

func (f *fakeAIPhotoStore) GetFile(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("absent")
	}
	return data, nil
}

func (f *fakeAIPhotoStore) GenerateSignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://r2/signed/" + key, nil
}

func (f *fakeAIPhotoStore) DeleteFile(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, key)
	return nil
}

// fakeOCRProvider répond selon le n° de photo lu dans la consigne utilisateur.
type fakeOCRProvider struct {
	mu      sync.Mutex
	replies map[int]fakeOCRReply
	calls   []ai.CompletionRequest
}

type fakeOCRReply struct {
	content    string
	stopReason string
	err        error
}

func (p *fakeOCRProvider) Name() string { return "anthropic" }

func (p *fakeOCRProvider) Complete(_ context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	var photo, total int
	_, _ = fmt.Sscanf(req.UserPrompt, "Photo %d sur %d", &photo, &total)
	p.mu.Lock()
	p.calls = append(p.calls, req)
	reply, ok := p.replies[photo]
	p.mu.Unlock()
	if !ok {
		return nil, errors.New("photo inattendue")
	}
	if reply.err != nil {
		return nil, reply.err
	}
	stop := reply.stopReason
	if stop == "" {
		stop = "end_turn"
	}
	return &ai.CompletionResponse{Content: reply.content, StopReason: stop, Model: "claude-opus-5-5", InputTokens: 5000, OutputTokens: 800}, nil
}

func (p *fakeOCRProvider) set(photo int, reply fakeOCRReply) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replies[photo] = reply
}

type fakeImportLookupsReader struct{}

func (fakeImportLookupsReader) LoadImportPreviewLookups(context.Context, string, string) (importer.PreviewLookups, error) {
	var rates []importer.TvaRateRow
	id := 1
	for _, channel := range importer.AllTvaChannels {
		for _, rate := range []float64{5.5, 10, 20} {
			rates = append(rates, importer.TvaRateRow{TvaID: id, Channel: channel, Rate: rate})
			id++
		}
	}
	return importer.PreviewLookups{TvaRates: rates, Imported: importer.ImportedEntities{
		Products: map[string]int{}, Categories: map[string]int{}, Tags: map[string]string{}, Attributes: map[string]string{},
	}}, nil
}

const (
	aiTestMerchant = "m-ai-1"
	// page1 : un groupe Coca-Cola à deux déclinaisons ; page2 : un burger.
	aiTestPage1 = `{"categories":[{"ref":"c1","name":"Boissons"}],"product_groups":[{"ref":"g1","category_ref":"c1","name":"Coca-Cola"}],
"products":[{"ref":"p1","category_ref":"c1","group_ref":"g1","name":"Coca-Cola Zero","description":"","price_cents":350,"option_group_refs":[],"kind":"soft_drink_sealed","confidence":"high","issues":[]},
{"ref":"p2","category_ref":"c1","group_ref":"g1","name":"Coca-Cola Cherry","description":"","price_cents":380,"option_group_refs":[],"kind":"soft_drink_sealed","confidence":"high","issues":[]}],
"option_groups":[],"formulas":[],"warnings":[]}`
	aiTestPage2 = `{"categories":[{"ref":"c1","name":"Burgers"}],"product_groups":[],
"products":[{"ref":"p1","category_ref":"c1","group_ref":null,"name":"Classique","description":"","price_cents":1250,"option_group_refs":[],"kind":"food","confidence":"high","issues":[]}],
"option_groups":[],"formulas":[],"warnings":[]}`
)

var aiTestJPEG = []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00fake-jpeg-body")

type aiTestEnv struct {
	service  *AIImportService
	drafts   *fakeAIDraftStore
	photos   *fakeAIPhotoStore
	provider *fakeOCRProvider
	store    *fakePreviewStore
	imports  *ImportService
}

func newAITestEnv(t *testing.T, enabled bool) *aiTestEnv {
	t.Helper()
	provider := &fakeOCRProvider{replies: map[int]fakeOCRReply{
		1: {content: aiTestPage1},
		2: {content: aiTestPage2},
	}}
	registry, err := ai.NewRegistry(ai.AIConfig{Tasks: map[string]ai.TaskConfig{
		menuOCRTask: {Provider: "anthropic", Enabled: enabled, MaxTokens: 16000},
	}}, map[string]ai.LLMProvider{"anthropic": provider})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	store := newFakePreviewStore()
	imports := NewImportService(fakeImportLookupsReader{}, nil, importer.DefaultRegistry(), store, nil, nil, nil, nil)
	env := &aiTestEnv{drafts: newFakeAIDraftStore(), photos: newFakeAIPhotoStore(), provider: provider, store: store, imports: imports}
	env.service = NewAIImportService(env.drafts, env.photos, registry, imports, zap.NewNop(), 10)
	return env
}

func aiTestContext() context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{UserID: "u-ai-1", MerchantID: aiTestMerchant})
}

// ---- service ----

func TestAIImport_ExtractionThenPreview(t *testing.T) {
	env := newAITestEnv(t, true)
	ctx := aiTestContext()

	started, err := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG, aiTestJPEG})
	if err != nil {
		t.Fatalf("StartExtraction: %v", err)
	}
	if started.Status != AIDraftProcessing || started.PhotosTotal != 2 {
		t.Fatalf("réponse de démarrage = %+v", started)
	}
	env.service.Wait()

	// Chaque photo est partie seule, image + schéma, sans température.
	if len(env.provider.calls) != 2 {
		t.Fatalf("appels IA = %d, want 2 (un par photo)", len(env.provider.calls))
	}
	call := env.provider.calls[0]
	if len(call.Images) != 1 || call.Images[0].MediaType != "image/jpeg" || len(call.JSONSchema) == 0 || call.Temperature != 0 || call.MaxTokens != 16000 {
		t.Errorf("requête IA = images %d, schéma %d octets, température %v, max %d", len(call.Images), len(call.JSONSchema), call.Temperature, call.MaxTokens)
	}

	got, err := env.service.GetDraft(ctx, started.ID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if got.Status != AIDraftReady || got.PhotosDone != 2 || got.Preview == nil {
		t.Fatalf("brouillon = statut %q, %d photos lues, preview %v", got.Status, got.PhotosDone, got.Preview != nil)
	}
	if got.Preview.Provider != importer.AIPhotoSlug || len(got.Preview.Products) != 4 {
		t.Errorf("preview = porte %q, %d produits, want ai_photo et 4 (groupe + 3)", got.Preview.Provider, len(got.Preview.Products))
	}
	if got.Photos[0].URL == "" {
		t.Errorf("photo sans lien signé pour la relecture")
	}

	// Le snapshot de preview porte le brouillon, pour le marquer importé au commit.
	snapshot, err := env.imports.LoadPreviewSnapshot(ctx, aiTestMerchant, got.Preview.Token)
	if err != nil || snapshot.DraftID != started.ID {
		t.Fatalf("snapshot = %+v, %v ; want DraftID %s", snapshot, err, started.ID)
	}

	credits, _ := env.service.Credits(ctx, aiTestMerchant)
	if credits.Used != 1 || credits.Remaining != 9 {
		t.Errorf("crédits = %+v, want 1 utilisé sur 10", credits)
	}
}

func TestAIImport_CommitHookMarksDraftCommitted(t *testing.T) {
	env := newAITestEnv(t, true)
	if env.imports.OnDraftCommitted == nil {
		t.Fatal("NewAIImportService doit brancher OnDraftCommitted")
	}
	started, _ := env.service.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG})
	env.service.Wait()

	env.imports.OnDraftCommitted(context.Background(), aiTestMerchant, started.ID)
	if env.drafts.drafts[started.ID].Status != AIDraftCommitted {
		t.Errorf("statut après commit = %q, want committed", env.drafts.drafts[started.ID].Status)
	}
}

func TestAIImport_DisabledTaskOrStorage(t *testing.T) {
	env := newAITestEnv(t, false)
	if _, err := env.service.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG}); !errors.Is(err, ErrAIImportDisabled) {
		t.Errorf("tâche fermée : err = %v, want ErrAIImportDisabled", err)
	}

	enabled := newAITestEnv(t, true)
	noStorage := NewAIImportService(enabled.drafts, nil, nil, enabled.imports, zap.NewNop(), 10)
	if _, err := noStorage.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG}); !errors.Is(err, ErrAIImportDisabled) {
		t.Errorf("sans stockage ni registre : err = %v, want ErrAIImportDisabled", err)
	}
}

func TestAIImport_CreditsExhausted(t *testing.T) {
	env := newAITestEnv(t, true)
	ctx := aiTestContext()
	env.drafts.overrides[aiTestMerchant] = 1

	if _, err := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG}); err != nil {
		t.Fatalf("première extraction: %v", err)
	}
	env.service.Wait()
	if _, err := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG}); !errors.Is(err, ErrAICreditsExhausted) {
		t.Errorf("err = %v, want ErrAICreditsExhausted", err)
	}
}

func TestAIImport_NothingReadRefundsCredit(t *testing.T) {
	env := newAITestEnv(t, true)
	env.provider.set(1, fakeOCRReply{err: fmt.Errorf("anthropic: %w", ai.ErrRefused)})

	started, _ := env.service.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG})
	env.service.Wait()

	d := env.drafts.drafts[started.ID]
	if d.Status != AIDraftFailed || d.ConsumesCredit || !d.Pages[0].Refused {
		t.Errorf("brouillon = statut %q, crédit %v, refus %v ; want failed, crédit rendu, refus noté", d.Status, d.ConsumesCredit, d.Pages[0].Refused)
	}
}

func TestAIImport_PartialFailureThenRetry(t *testing.T) {
	env := newAITestEnv(t, true)
	ctx := aiTestContext()
	env.provider.set(2, fakeOCRReply{content: `{"categories":[`, stopReason: ai.StopReasonMaxTokens})

	started, _ := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG, aiTestJPEG})
	env.service.Wait()

	got, _ := env.service.GetDraft(ctx, started.ID)
	if got.Status != AIDraftReady || got.Photos[1].Status != AIPageFailed || !strings.Contains(got.Photos[1].Error, "trop chargée") {
		t.Fatalf("après lecture partielle = %+v", got.Photos)
	}
	if got.Preview == nil || len(got.Preview.Products) != 3 {
		t.Fatalf("preview partielle attendue avec la photo 1 seule")
	}

	// Relance : seule la photo 2 repart, relue depuis R2 ; gratuite.
	env.provider.set(2, fakeOCRReply{content: aiTestPage2})
	callsBefore := len(env.provider.calls)
	if _, err := env.service.RetryDraft(ctx, started.ID); err != nil {
		t.Fatalf("RetryDraft: %v", err)
	}
	env.service.Wait()
	if len(env.provider.calls) != callsBefore+1 {
		t.Errorf("appels à la relance = %d, want 1 (photo 2 seule)", len(env.provider.calls)-callsBefore)
	}
	got, _ = env.service.GetDraft(ctx, started.ID)
	if got.PhotosDone != 2 || len(got.Preview.Products) != 4 {
		t.Errorf("après relance = %d photos lues, %d produits", got.PhotosDone, len(got.Preview.Products))
	}
	if credits, _ := env.service.Credits(ctx, aiTestMerchant); credits.Used != 1 {
		t.Errorf("crédits utilisés après relance = %d, want 1", credits.Used)
	}

	if _, err := env.service.RetryDraft(ctx, started.ID); !errors.Is(err, ErrAIDraftNotRetryable) {
		t.Errorf("relance sans photo en échec : err = %v, want ErrAIDraftNotRetryable", err)
	}
}

func TestAIImport_UnreadableModelOutput(t *testing.T) {
	env := newAITestEnv(t, true)
	env.provider.set(1, fakeOCRReply{content: "pas du json"})

	started, _ := env.service.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG})
	env.service.Wait()
	if p := env.drafts.drafts[started.ID].Pages[0]; p.Status != AIPageFailed || !strings.Contains(p.Error, "illisible") {
		t.Errorf("photo = %+v, want échec « illisible »", p)
	}
}

func TestAIImport_OneRunningExtractionPerMerchant(t *testing.T) {
	env := newAITestEnv(t, true)
	ctx := aiTestContext()
	env.drafts.drafts["running"] = &AIDraft{ID: "running", MerchantID: aiTestMerchant, Status: AIDraftProcessing}

	if _, err := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG}); !errors.Is(err, ErrAIDraftAlreadyRunning) {
		t.Errorf("err = %v, want ErrAIDraftAlreadyRunning", err)
	}
}

func TestAIImport_MaintenancePurgesPhotos(t *testing.T) {
	env := newAITestEnv(t, true)
	started, _ := env.service.StartExtraction(aiTestContext(), [][]byte{aiTestJPEG})
	env.service.Wait()
	env.imports.OnDraftCommitted(context.Background(), aiTestMerchant, started.ID)

	env.service.Maintain(context.Background())

	if len(env.photos.files) != 0 {
		t.Errorf("photos restantes après purge = %d", len(env.photos.files))
	}
	if len(env.drafts.purged) != 1 {
		t.Errorf("brouillons notés purgés = %v", env.drafts.purged)
	}
}

// ---- handler ----

func aiMultipartRequest(t *testing.T, photos ...[]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for i, data := range photos {
		part, _ := mw.CreateFormFile(aiFormPhotosField, fmt.Sprintf("photo%d.jpg", i+1))
		_, _ = part.Write(data)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/menu/import/ai", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req.WithContext(aiTestContext())
}

func TestAIImportHandler_ValidatesPhotos(t *testing.T) {
	h := NewAIImportHandler(newAITestEnv(t, true).service)

	many := make([][]byte, aiMaxPhotos+1)
	for i := range many {
		many[i] = aiTestJPEG
	}
	cases := map[string]struct {
		req  *http.Request
		code string
	}{
		"aucune photo":   {aiMultipartRequest(t), "missing_photos"},
		"trop de photos": {aiMultipartRequest(t, many...), "too_many_photos"},
		"pas un JPEG":    {aiMultipartRequest(t, []byte("\x89PNG\r\n\x1a\nfake")), "photo_not_jpeg"},
	}
	for name, tc := range cases {
		rec := httptest.NewRecorder()
		h.StartAIImport(rec, tc.req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s : %d %s, want 400 %s", name, rec.Code, rec.Body.String(), tc.code)
		}
	}
}

func TestAIImportHandler_StatusCodes(t *testing.T) {
	disabled := NewAIImportHandler(newAITestEnv(t, false).service)
	rec := httptest.NewRecorder()
	disabled.StartAIImport(rec, aiMultipartRequest(t, aiTestJPEG))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("tâche fermée : %d, want 503", rec.Code)
	}

	env := newAITestEnv(t, true)
	env.drafts.overrides[aiTestMerchant] = 0
	rec = httptest.NewRecorder()
	NewAIImportHandler(env.service).StartAIImport(rec, aiMultipartRequest(t, aiTestJPEG))
	if rec.Code != http.StatusPaymentRequired {
		t.Errorf("sans crédit : %d, want 402", rec.Code)
	}

	ok := newAITestEnv(t, true)
	rec = httptest.NewRecorder()
	NewAIImportHandler(ok.service).StartAIImport(rec, aiMultipartRequest(t, aiTestJPEG))
	ok.service.Wait()
	if rec.Code != http.StatusAccepted {
		t.Errorf("extraction lancée : %d %s, want 202", rec.Code, rec.Body.String())
	}
}

// Le snapshot de preview d'un brouillon est stocké sous la clé habituelle.
func TestAIImport_PreviewStoredUnderImportKey(t *testing.T) {
	env := newAITestEnv(t, true)
	ctx := aiTestContext()
	started, _ := env.service.StartExtraction(ctx, [][]byte{aiTestJPEG})
	env.service.Wait()
	got, _ := env.service.GetDraft(ctx, started.ID)
	if _, ok := env.store.values[helpers.GetMenuImportPreviewKey(aiTestMerchant, got.Preview.Token)]; !ok {
		t.Errorf("snapshot absent de la clé de preview d'import")
	}
}
