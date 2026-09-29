//go:build postgres_integration

package menu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/menu/importer"
)

// Requiert la migration 161 (menu_import_drafts, menu_import_ai_credits).
// Les marchands de test sont des identifiants « itest-ai-… » sans ligne
// merchant (merchant_id n'a pas de clé étrangère), sauf le test de bout en
// bout qui a besoin d'un vrai marchand pour importer.

func itestAIDraft(merchantID, status string) *AIDraft {
	return &AIDraft{
		ID:             uuid.New().String(),
		MerchantID:     merchantID,
		CreatedBy:      "u-itest-ai",
		Source:         importer.AIPhotoSlug,
		Status:         status,
		ConsumesCredit: true,
		ExpiresAt:      time.Now().Add(aiDraftTTL),
		Pages:          []AIDraftPage{{Photo: 1, R2Key: "menu-import/itest/1.jpg", Status: AIPagePending}},
	}
}

func TestAIDraftRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewAIDraftRepository(db)

	const merchant, other = "itest-ai-m1", "itest-ai-m2"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM menu_import_drafts WHERE merchant_id LIKE 'itest-ai-%'`)
		_, _ = db.ExecContext(ctx, `DELETE FROM menu_import_ai_credits WHERE merchant_id LIKE 'itest-ai-%'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	// --- création, lecture, cloisonnement par marchand ---
	d := itestAIDraft(merchant, AIDraftPending)
	if err := repo.CreateDraft(ctx, d); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if _, err := repo.GetDraft(ctx, other, d.ID); !errors.Is(err, ErrAIDraftNotFound) {
		t.Fatalf("GetDraft d'un autre marchand = %v, want ErrAIDraftNotFound", err)
	}

	// --- une seule extraction en cours par marchand (index unique partiel) ---
	if err := repo.CreateDraft(ctx, itestAIDraft(merchant, AIDraftPending)); !errors.Is(err, ErrAIDraftAlreadyRunning) {
		t.Fatalf("second brouillon en cours = %v, want ErrAIDraftAlreadyRunning", err)
	}
	if err := repo.CreateDraft(ctx, itestAIDraft(other, AIDraftPending)); err != nil {
		t.Fatalf("brouillon d'un autre marchand refusé: %v", err)
	}

	// --- réclamation, progression (jsonb), clôture ---
	if ok, err := repo.ClaimDraft(ctx, d.ID); err != nil || !ok {
		t.Fatalf("ClaimDraft = %v, %v", ok, err)
	}
	if ok, _ := repo.ClaimDraft(ctx, d.ID); ok {
		t.Fatalf("un brouillon en cours ne doit pas être réclamé deux fois")
	}
	output := json.RawMessage(`{"categories":[],"product_groups":[],"products":[],"option_groups":[],"formulas":[],"warnings":["test"]}`)
	pages := []AIDraftPage{{Photo: 1, R2Key: "menu-import/itest/1.jpg", Status: AIPageDone, Output: output,
		Usage: &AIDraftPageUsage{Model: "claude-opus-5-5", InputTokens: 5000, OutputTokens: 900, LatencyMs: 42000, StopReason: "end_turn"}}}
	if err := repo.SavePages(ctx, d.ID, pages); err != nil {
		t.Fatalf("SavePages: %v", err)
	}
	if err := repo.FinishDraft(ctx, d.ID, AIDraftReady, true, ""); err != nil {
		t.Fatalf("FinishDraft: %v", err)
	}
	got, err := repo.GetDraft(ctx, merchant, d.ID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	var gotOutput, wantOutput any
	_ = json.Unmarshal(got.Pages[0].Output, &gotOutput)
	_ = json.Unmarshal(output, &wantOutput)
	if got.Status != AIDraftReady || got.Pages[0].Status != AIPageDone || got.Pages[0].Usage.OutputTokens != 900 ||
		!jsonEqual(gotOutput, wantOutput) || got.Error != "" {
		t.Fatalf("brouillon relu = %+v", got)
	}

	// --- relance bloquée tant qu'une autre lecture du marchand tourne ---
	running := itestAIDraft(merchant, AIDraftPending)
	if err := repo.CreateDraft(ctx, running); err != nil {
		t.Fatalf("CreateDraft (deuxième, après clôture du premier): %v", err)
	}
	if _, err := repo.ClaimDraft(ctx, d.ID); !errors.Is(err, ErrAIDraftAlreadyRunning) {
		t.Fatalf("relance pendant une autre lecture = %v, want ErrAIDraftAlreadyRunning", err)
	}

	// --- crédits : décompte et surcharge ---
	if err := repo.FinishDraft(ctx, running.ID, AIDraftFailed, false, "aucune photo n'a pu être lue"); err != nil {
		t.Fatalf("FinishDraft (échec): %v", err)
	}
	if used, err := repo.CountUsedCredits(ctx, merchant); err != nil || used != 1 {
		t.Fatalf("crédits utilisés = %d, %v ; want 1 (l'échec ne compte pas)", used, err)
	}
	if _, ok, err := repo.GetCreditsOverride(ctx, merchant); err != nil || ok {
		t.Fatalf("surcharge avant pose = %v, %v ; want absente", ok, err)
	}
	for _, credits := range []int{25, 30} { // pose puis mise à jour (upsert)
		if err := repo.SetCreditsOverride(ctx, merchant, credits, "staff-itest"); err != nil {
			t.Fatalf("SetCreditsOverride(%d): %v", credits, err)
		}
	}
	if credits, ok, err := repo.GetCreditsOverride(ctx, merchant); err != nil || !ok || credits != 30 {
		t.Fatalf("surcharge = %d, %v, %v ; want 30", credits, ok, err)
	}

	// --- liste des brouillons ouverts ---
	open, err := repo.ListOpenDrafts(ctx, merchant)
	if err != nil || len(open) != 2 {
		t.Fatalf("brouillons ouverts = %d, %v ; want 2", len(open), err)
	}

	// --- abandon, import, purge ---
	if ok, err := repo.AbandonDraft(ctx, merchant, running.ID); err != nil || !ok {
		t.Fatalf("AbandonDraft = %v, %v", ok, err)
	}
	if err := repo.MarkCommitted(ctx, merchant, d.ID); err != nil {
		t.Fatalf("MarkCommitted: %v", err)
	}
	if got, _ := repo.GetDraft(ctx, merchant, d.ID); got.Status != AIDraftCommitted || got.CommittedAt == nil {
		t.Fatalf("après commit = %+v", got)
	}
	if _, err := db.ExecContext(ctx, `UPDATE menu_import_drafts SET updated_at = now() - interval '31 days' WHERE merchant_id = $1`, merchant); err != nil {
		t.Fatalf("vieillissement: %v", err)
	}
	toPurge, err := repo.ListDraftsToPurge(ctx, aiPhotosPurgeAfter, 100)
	if err != nil {
		t.Fatalf("ListDraftsToPurge: %v", err)
	}
	purgeable := 0
	for _, p := range toPurge {
		if p.MerchantID == merchant {
			purgeable++
			if err := repo.MarkFilesPurged(ctx, p.ID); err != nil {
				t.Fatalf("MarkFilesPurged: %v", err)
			}
		}
	}
	if purgeable != 2 {
		t.Fatalf("brouillons à purger = %d, want 2 (importé + abandonné)", purgeable)
	}
	if again, _ := repo.ListDraftsToPurge(ctx, aiPhotosPurgeAfter, 100); len(again) != 0 {
		t.Fatalf("brouillons encore à purger après purge = %d", len(again))
	}

	// --- reprise des lectures interrompues, expiration ---
	stale := itestAIDraft("itest-ai-m3", AIDraftPending)
	if err := repo.CreateDraft(ctx, stale); err != nil {
		t.Fatalf("CreateDraft (interrompu): %v", err)
	}
	_, _ = repo.ClaimDraft(ctx, stale.ID)
	if _, err := db.ExecContext(ctx, `UPDATE menu_import_drafts SET updated_at = now() - interval '11 minutes' WHERE id = $1`, stale.ID); err != nil {
		t.Fatalf("vieillissement: %v", err)
	}
	if n, err := repo.FailStaleDrafts(ctx, aiStaleAfter); err != nil || n < 1 {
		t.Fatalf("FailStaleDrafts = %d, %v", n, err)
	}
	if got, _ := repo.GetDraft(ctx, "itest-ai-m3", stale.ID); got.Status != AIDraftFailed || got.ConsumesCredit || got.Error == "" {
		t.Fatalf("lecture interrompue = %+v, want failed, crédit rendu, message", got)
	}
	if _, err := db.ExecContext(ctx, `UPDATE menu_import_drafts SET expires_at = now() - interval '1 minute' WHERE id = $1`, stale.ID); err != nil {
		t.Fatalf("échéance: %v", err)
	}
	if n, err := repo.ExpireDrafts(ctx); err != nil || n < 1 {
		t.Fatalf("ExpireDrafts = %d, %v", n, err)
	}
}

// Bout en bout sur la vraie base : lecture (faux modèle, fausses photos),
// preview à partir des lookups réels, commit, brouillon marqué importé.
func TestAIImport_Postgres_EndToEnd(t *testing.T) {
	db := pgtest.Open(t)
	merchantID, _ := itestImportMerchant(t, db, "aie")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM menu_import_drafts WHERE merchant_id = $1`, merchantID)
	})

	provider := &fakeOCRProvider{replies: map[int]fakeOCRReply{1: {content: aiTestPage1}, 2: {content: aiTestPage2}}}
	registry, err := ai.NewRegistry(ai.AIConfig{Tasks: map[string]ai.TaskConfig{
		menuOCRTask: {Provider: "anthropic", Enabled: true, MaxTokens: 16000},
	}}, map[string]ai.LLMProvider{"anthropic": provider})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	imports := itestImportService(db, newFakePreviewStore())
	service := NewAIImportService(NewAIDraftRepository(db), newFakeAIPhotoStore(), registry, imports, zap.NewNop(), 10)

	ctx := itestImportContext(merchantID)
	started, err := service.StartExtraction(ctx, [][]byte{aiTestJPEG, aiTestJPEG})
	if err != nil {
		t.Fatalf("StartExtraction: %v", err)
	}
	service.Wait()

	draft, err := service.GetDraft(ctx, started.ID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if draft.Status != AIDraftReady || draft.Preview == nil || len(draft.Preview.Products) != 4 {
		t.Fatalf("brouillon = statut %q, preview %v", draft.Status, draft.Preview != nil)
	}

	decisions := draft.Preview.Decisions
	decisions.TvaConfirmed = true
	resp, err := imports.CommitImport(ctx, &ImportCommitRequest{Token: draft.Preview.Token, Decisions: &decisions})
	if err != nil {
		t.Fatalf("CommitImport: %v", err)
	}
	if resp.Summary.Products.Created != 4 {
		t.Fatalf("produits créés = %d, want 4", resp.Summary.Products.Created)
	}

	stored, err := NewAIDraftRepository(db).GetDraft(context.Background(), merchantID, started.ID)
	if err != nil || stored.Status != AIDraftCommitted {
		t.Fatalf("brouillon après commit = %+v, %v ; want committed", stored, err)
	}
	if credits, _ := service.Credits(context.Background(), merchantID); credits.Used != 1 {
		t.Fatalf("crédits utilisés = %d, want 1", credits.Used)
	}
	if got := itestCount(t, db,
		`SELECT count(*) FROM products WHERE merchant_Id = $1 AND by_product_of IS NOT NULL`, merchantID); got != 2 {
		t.Fatalf("déclinaisons rattachées = %d, want 2", got)
	}
}

func jsonEqual(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
