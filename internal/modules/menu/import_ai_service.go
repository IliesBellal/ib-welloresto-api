package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/menu/importer"
)

var (
	// ErrAIImportDisabled : tâche menu_ocr fermée (AI_TASK_MENU_OCR_ENABLED),
	// ou stockage R2 privé indisponible.
	ErrAIImportDisabled = errors.New("import_ai_disabled")
	// ErrAICreditsExhausted : plus de crédit d'import photo.
	ErrAICreditsExhausted = errors.New("import_ai_no_credits")
	// ErrAIDraftNotRetryable : rien à relancer (aucune photo en échec, ou
	// lecture en cours).
	ErrAIDraftNotRetryable = errors.New("import_ai_draft_not_retryable")
)

const (
	aiDraftTTL          = 30 * 24 * time.Hour // brouillon conservé 30 jours (Q8)
	aiPhotosPurgeAfter  = 30 * 24 * time.Hour // photos purgées 30 jours après import ou expiration (Q8)
	aiStaleAfter        = 10 * time.Minute    // lecture sans progrès : interrompue
	aiMaintenanceEvery  = 5 * time.Minute
	aiPurgeBatch        = 50
	aiPhotoURLTTL       = time.Hour
	aiReadConcurrency   = 3 // photos lues en parallèle par brouillon
	aiPhotoContentType  = "image/jpeg"
	aiPhotoKeyPrefix    = "menu-import"
	aiDraftErrNoPhoto   = "aucune photo n'a pu être lue"
	aiDraftErrNoProduct = "aucun produit n'a été lu sur les photos"
)

type aiDraftStore interface {
	CreateDraft(ctx context.Context, d *AIDraft) error
	GetDraft(ctx context.Context, merchantID, id string) (*AIDraft, error)
	ListOpenDrafts(ctx context.Context, merchantID string) ([]AIDraft, error)
	ClaimDraft(ctx context.Context, id string) (bool, error)
	SavePages(ctx context.Context, id string, pages []AIDraftPage) error
	FinishDraft(ctx context.Context, id, status string, consumesCredit bool, errMsg string) error
	MarkCommitted(ctx context.Context, merchantID, id string) error
	AbandonDraft(ctx context.Context, merchantID, id string) (bool, error)
	CountUsedCredits(ctx context.Context, merchantID string) (int, error)
	GetCreditsOverride(ctx context.Context, merchantID string) (int, bool, error)
	SetCreditsOverride(ctx context.Context, merchantID string, credits int, updatedBy string) error
	FailStaleDrafts(ctx context.Context, staleAfter time.Duration) (int64, error)
	ExpireDrafts(ctx context.Context) (int64, error)
	ListDraftsToPurge(ctx context.Context, purgeAfter time.Duration, limit int) ([]AIDraft, error)
	MarkFilesPurged(ctx context.Context, id string) error
}

// AIPhotoStore est le bucket R2 privé (*r2.Client).
type AIPhotoStore interface {
	UploadPrivateFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	GetFile(ctx context.Context, key string) ([]byte, error)
	GenerateSignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	DeleteFile(ctx context.Context, key string) error
}

// AITaskRegistry est le registre IA (*ai.Registry).
type AITaskRegistry interface {
	GetProviderForTask(task string) (ai.LLMProvider, error)
	TaskConfig(task string) (ai.TaskConfig, bool)
}

// AIImportService porte la lecture de carte par photo (porte IA de l'import
// produits) : dépôt des photos, lecture asynchrone par l'IA (une requête par
// photo), brouillon durable, crédits, et preview d'import ordinaire une fois
// les photos lues. Le commit reste celui de l'import (POST /menu/import/commit).
type AIImportService struct {
	drafts   aiDraftStore
	photos   AIPhotoStore
	registry AITaskRegistry
	imports  *ImportService
	log      *zap.Logger

	defaultCredits int

	// running suit les lectures lancées, pour que les tests puissent les
	// attendre (Wait).
	running sync.WaitGroup
}

// NewAIImportService branche aussi le marquage « importé » du brouillon sur
// le commit de l'import. photos peut être nil (R2 privé indisponible) : la
// porte IA répond alors ErrAIImportDisabled.
func NewAIImportService(drafts aiDraftStore, photos AIPhotoStore, registry AITaskRegistry, imports *ImportService, log *zap.Logger, defaultCredits int) *AIImportService {
	s := &AIImportService{
		drafts:         drafts,
		photos:         photos,
		registry:       registry,
		imports:        imports,
		log:            log,
		defaultCredits: defaultCredits,
	}
	imports.OnDraftCommitted = s.markCommitted
	return s
}

// provider rend le provider de la tâche menu_ocr, ou ErrAIImportDisabled.
func (s *AIImportService) provider() (ai.LLMProvider, error) {
	if s.photos == nil || s.registry == nil {
		return nil, ErrAIImportDisabled
	}
	provider, err := s.registry.GetProviderForTask(menuOCRTask)
	if errors.Is(err, ai.ErrTaskDisabled) {
		return nil, ErrAIImportDisabled
	}
	return provider, err
}

// Credits rend le solde de crédits d'import photo du marchand.
func (s *AIImportService) Credits(ctx context.Context, merchantID string) (AICredits, error) {
	total := s.defaultCredits
	override, ok, err := s.drafts.GetCreditsOverride(ctx, merchantID)
	if err != nil {
		return AICredits{}, err
	}
	if ok {
		total = override
	}
	used, err := s.drafts.CountUsedCredits(ctx, merchantID)
	if err != nil {
		return AICredits{}, err
	}
	remaining := total - used
	if remaining < 0 {
		remaining = 0
	}
	return AICredits{Total: total, Used: used, Remaining: remaining}, nil
}

// StartExtraction dépose les photos (JPEG déjà normalisés par le
// back-office), crée le brouillon et lance la lecture en arrière-plan.
// Le crédit est réservé dès la création ; il est rendu si aucune photo n'a
// pu être lue (échec technique ou refus).
func (s *AIImportService) StartExtraction(ctx context.Context, photos [][]byte) (*AIDraftResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.provider(); err != nil {
		return nil, err
	}

	credits, err := s.Credits(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if credits.Remaining <= 0 {
		return nil, ErrAICreditsExhausted
	}

	draft := &AIDraft{
		ID:             uuid.New().String(),
		MerchantID:     user.MerchantID,
		CreatedBy:      user.UserID,
		Source:         importer.AIPhotoSlug,
		Status:         AIDraftPending,
		ConsumesCredit: true,
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(aiDraftTTL),
	}
	for i := range photos {
		draft.Pages = append(draft.Pages, AIDraftPage{
			Photo:  i + 1,
			R2Key:  fmt.Sprintf("%s/%s/%s/%d.jpg", aiPhotoKeyPrefix, user.MerchantID, draft.ID, i+1),
			Status: AIPagePending,
		})
	}
	if err := s.drafts.CreateDraft(ctx, draft); err != nil {
		return nil, err
	}

	byPhoto := make(map[int][]byte, len(photos))
	for i, data := range photos {
		page := draft.Pages[i]
		if _, err := s.photos.UploadPrivateFile(ctx, page.R2Key, bytes.NewReader(data), aiPhotoContentType); err != nil {
			s.finish(draft.ID, AIDraftFailed, false, "dépôt des photos impossible, réessayez")
			return nil, fmt.Errorf("import photo : dépôt de la photo %d: %w", page.Photo, err)
		}
		byPhoto[page.Photo] = data
	}

	if _, err := s.drafts.ClaimDraft(ctx, draft.ID); err != nil {
		return nil, err
	}
	draft.Status = AIDraftProcessing
	s.launch(draft, byPhoto)

	return s.response(ctx, draft, nil), nil
}

// RetryDraft relit les photos en échec d'un brouillon (gratuit : le crédit
// de l'extraction est déjà compté, ou rendu si rien n'avait été lu).
func (s *AIImportService) RetryDraft(ctx context.Context, id string) (*AIDraftResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.provider(); err != nil {
		return nil, err
	}

	draft, err := s.drafts.GetDraft(ctx, user.MerchantID, id)
	if err != nil {
		return nil, err
	}
	failed := 0
	for _, p := range draft.Pages {
		if p.Status != AIPageDone {
			failed++
		}
	}
	if failed == 0 || (draft.Status != AIDraftReady && draft.Status != AIDraftFailed) || draft.FilesPurgedAt != nil {
		return nil, ErrAIDraftNotRetryable
	}

	claimed, err := s.drafts.ClaimDraft(ctx, draft.ID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, ErrAIDraftNotRetryable
	}
	draft.Status = AIDraftProcessing
	s.launch(draft, nil)

	return s.response(ctx, draft, nil), nil
}

// launch lit les photos du brouillon (déjà réclamé) en arrière-plan.
func (s *AIImportService) launch(draft *AIDraft, byPhoto map[int][]byte) {
	s.running.Add(1)
	go func() {
		defer s.running.Done()
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("import photo : panique pendant la lecture",
					zap.String("draft_id", draft.ID), zap.Any("recover", rec))
				s.finish(draft.ID, AIDraftFailed, false, "erreur interne pendant la lecture")
			}
		}()
		s.read(draft, byPhoto)
	}()
}

// Wait attend la fin des lectures lancées (tests).
func (s *AIImportService) Wait() { s.running.Wait() }

// read lit chaque photo pas encore lue, jusqu'à aiReadConcurrency à la fois,
// enregistre la progression après chaque photo, puis clôt le brouillon.
func (s *AIImportService) read(draft *AIDraft, byPhoto map[int][]byte) {
	ctx := context.Background()

	provider, err := s.provider()
	if err != nil {
		s.finish(draft.ID, AIDraftFailed, false, "import photo désactivé")
		return
	}
	cfg, _ := s.registry.TaskConfig(menuOCRTask)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, aiReadConcurrency)
	)
	total := len(draft.Pages)
	for i := range draft.Pages {
		if draft.Pages[i].Status == AIPageDone {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			page := draft.Pages[i]
			mu.Unlock()

			data, ok := byPhoto[page.Photo]
			if !ok {
				var err error
				if data, err = s.photos.GetFile(ctx, page.R2Key); err != nil {
					s.log.Warn("import photo : relecture R2 impossible", zap.String("draft_id", draft.ID), zap.Error(err))
					page.Status, page.Error = AIPageFailed, "photo introuvable, déposez-la à nouveau"
				}
			}
			if data != nil {
				page = s.readPhoto(ctx, provider, cfg, draft.ID, page, data, total)
			}

			mu.Lock()
			draft.Pages[i] = page
			if err := s.drafts.SavePages(ctx, draft.ID, draft.Pages); err != nil {
				s.log.Error("import photo : enregistrement de la progression", zap.String("draft_id", draft.ID), zap.Error(err))
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	done := draft.photosDone()
	if done == 0 {
		// Rien de lu : échec technique ou refus, le crédit est rendu.
		s.finish(draft.ID, AIDraftFailed, false, aiDraftErrNoPhoto)
		return
	}
	s.finish(draft.ID, AIDraftReady, true, "")
}

// readPhoto envoie une photo au modèle et rend la page mise à jour. Les
// messages d'erreur sont destinés au restaurateur ; le détail technique va
// dans les journaux.
func (s *AIImportService) readPhoto(ctx context.Context, provider ai.LLMProvider, cfg ai.TaskConfig, draftID string, page AIDraftPage, data []byte, total int) AIDraftPage {
	resp, err := provider.Complete(ctx, ai.CompletionRequest{
		Task:         menuOCRTask,
		SystemPrompt: menuOCRSystemPrompt,
		UserPrompt:   menuOCRUserPrompt(page.Photo, total),
		MaxTokens:    cfg.MaxTokens,
		Images:       []ai.Image{{MediaType: aiPhotoContentType, Data: data}},
		JSONSchema:   importer.AIMenuPageSchema,
	})

	logFields := []zap.Field{zap.String("draft_id", draftID), zap.Int("photo", page.Photo)}
	switch {
	case err != nil:
		ai.LogLLMCall(s.log.With(logFields...), menuOCRTask, provider.Name(), "", 0, 0, 0, false, err)
	default:
		ai.LogLLMCall(s.log.With(logFields...), menuOCRTask, provider.Name(), resp.Model, resp.InputTokens, resp.OutputTokens, resp.LatencyMs, false, nil)
		page.Usage = &AIDraftPageUsage{
			Model:        resp.Model,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			LatencyMs:    resp.LatencyMs,
			StopReason:   resp.StopReason,
		}
	}

	page.Status, page.Error, page.Refused, page.Output = AIPageFailed, "", false, nil
	switch {
	case errors.Is(err, ai.ErrRefused):
		page.Refused = true
		page.Error = "cette photo n'a pas pu être analysée"
	case err != nil:
		page.Error = "lecture impossible pour le moment, relancez cette photo"
	case resp.StopReason == ai.StopReasonMaxTokens:
		page.Error = "photo trop chargée, lecture incomplète : reprenez cette partie de la carte en deux photos"
	default:
		if _, decodeErr := importer.DecodeAIMenuPage(resp.Content); decodeErr != nil {
			s.log.Warn("import photo : réponse IA illisible", append(logFields, zap.Error(decodeErr))...)
			page.Error = "lecture illisible, relancez cette photo"
			break
		}
		page.Status = AIPageDone
		page.Output = json.RawMessage(resp.Content)
	}
	return page
}

func (s *AIImportService) finish(id, status string, consumesCredit bool, errMsg string) {
	if err := s.drafts.FinishDraft(context.Background(), id, status, consumesCredit, errMsg); err != nil {
		s.log.Error("import photo : clôture du brouillon", zap.String("draft_id", id), zap.Error(err))
	}
}

// GetDraft rend l'état du brouillon et, s'il est prêt, une preview d'import
// ordinaire (avec son jeton de commit, redéposé à chaque ouverture).
func (s *AIImportService) GetDraft(ctx context.Context, id string) (*AIDraftResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	draft, err := s.drafts.GetDraft(ctx, user.MerchantID, id)
	if err != nil {
		return nil, err
	}
	if draft.Status != AIDraftReady {
		return s.response(ctx, draft, nil), nil
	}

	pages := make([]importer.AIMenuPage, 0, len(draft.Pages))
	for _, p := range draft.Pages {
		if p.Status != AIPageDone {
			continue
		}
		page, err := importer.DecodeAIMenuPage(string(p.Output))
		if err != nil {
			return nil, err
		}
		pages = append(pages, *page)
	}

	imp, err := importer.BuildAIMenuImport(pages)
	if errors.Is(err, importer.ErrNoProducts) {
		resp := s.response(ctx, draft, nil)
		resp.Error = aiDraftErrNoProduct
		return resp, nil
	}
	if err != nil {
		return nil, err
	}

	preview, err := s.imports.buildAndStoreForDraft(ctx, imp, draft.ID)
	if err != nil {
		return nil, err
	}
	return s.response(ctx, draft, preview), nil
}

// ListDrafts rend les brouillons encore exploitables et le solde de crédits.
func (s *AIImportService) ListDrafts(ctx context.Context) (*AIDraftsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	drafts, err := s.drafts.ListOpenDrafts(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	credits, err := s.Credits(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	out := &AIDraftsResponse{Drafts: make([]AIDraftSummary, 0, len(drafts)), Credits: credits}
	for i := range drafts {
		out.Drafts = append(out.Drafts, drafts[i].summary())
	}
	return out, nil
}

// AbandonDraft expire un brouillon à la demande du marchand. Le crédit reste
// décompté si des photos avaient été lues.
func (s *AIImportService) AbandonDraft(ctx context.Context, id string) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	ok, err := s.drafts.AbandonDraft(ctx, user.MerchantID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAIDraftNotFound
	}
	return nil
}

// SetCredits pose le nombre total de crédits d'un marchand (staff Wello).
func (s *AIImportService) SetCredits(ctx context.Context, merchantID string, credits int) (AICredits, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return AICredits{}, err
	}
	if err := s.drafts.SetCreditsOverride(ctx, merchantID, credits, user.UserID); err != nil {
		return AICredits{}, err
	}
	return s.Credits(ctx, merchantID)
}

func (s *AIImportService) markCommitted(ctx context.Context, merchantID, draftID string) {
	if err := s.drafts.MarkCommitted(ctx, merchantID, draftID); err != nil {
		s.log.Warn("import photo : brouillon non marqué importé", zap.String("draft_id", draftID), zap.Error(err))
	}
}

func (s *AIImportService) response(ctx context.Context, draft *AIDraft, preview *importer.PreviewResult) *AIDraftResponse {
	resp := &AIDraftResponse{
		ID:          draft.ID,
		Status:      draft.Status,
		Error:       draft.Error,
		CreatedAt:   draft.CreatedAt,
		ExpiresAt:   draft.ExpiresAt,
		PhotosTotal: len(draft.Pages),
		PhotosDone:  draft.photosDone(),
		Preview:     preview,
	}
	for _, p := range draft.Pages {
		photo := AIDraftPhotoResponse{Photo: p.Photo, Status: p.Status, Error: p.Error}
		if draft.FilesPurgedAt == nil && s.photos != nil {
			if url, err := s.photos.GenerateSignedURL(ctx, p.R2Key, aiPhotoURLTTL); err == nil {
				photo.URL = url
			}
		}
		resp.Photos = append(resp.Photos, photo)
	}
	return resp
}

// StartMaintenance lance, jusqu'à l'annulation de ctx, le nettoyage périodique
// des brouillons : lectures interrompues, brouillons périmés, photos à
// purger. Inactif tant que la tâche menu_ocr est fermée (la table peut ne pas
// encore exister).
func (s *AIImportService) StartMaintenance(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(aiMaintenanceEvery)
		defer ticker.Stop()
		for {
			s.Maintain(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Maintain exécute un passage de nettoyage.
func (s *AIImportService) Maintain(ctx context.Context) {
	if _, err := s.provider(); err != nil {
		return
	}

	if n, err := s.drafts.FailStaleDrafts(ctx, aiStaleAfter); err != nil {
		s.log.Warn("import photo : reprise des lectures interrompues", zap.Error(err))
	} else if n > 0 {
		s.log.Warn("import photo : lectures interrompues passées en échec", zap.Int64("brouillons", n))
	}

	if _, err := s.drafts.ExpireDrafts(ctx); err != nil {
		s.log.Warn("import photo : expiration des brouillons", zap.Error(err))
	}

	drafts, err := s.drafts.ListDraftsToPurge(ctx, aiPhotosPurgeAfter, aiPurgeBatch)
	if err != nil {
		s.log.Warn("import photo : liste des photos à purger", zap.Error(err))
		return
	}
	for _, d := range drafts {
		purged := true
		for _, p := range d.Pages {
			if err := s.photos.DeleteFile(ctx, p.R2Key); err != nil {
				s.log.Warn("import photo : suppression R2", zap.String("key", p.R2Key), zap.Error(err))
				purged = false
			}
		}
		if purged {
			if err := s.drafts.MarkFilesPurged(ctx, d.ID); err != nil {
				s.log.Warn("import photo : purge non notée", zap.String("draft_id", d.ID), zap.Error(err))
			}
		}
	}
}
