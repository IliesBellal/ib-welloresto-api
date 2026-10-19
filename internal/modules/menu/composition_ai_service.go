package menu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu/importer"
)

// Composition d'un produit existant à partir de sa description
// (docs/composition-ia.md). Deux temps :
//   - PreviewCompositions appelle l'IA et rend, par produit, les ingrédients
//     proposés à côté de sa composition actuelle. C'est elle qui consomme un
//     crédit d'import photo (un par génération, rendu si l'IA n'a rien lu) ;
//   - ApplyCompositions écrit ce que le restaurateur a gardé. Aucun appel IA :
//     les ingrédients sont renvoyés par le client, qui pourrait de toute façon
//     les créer par les routes ordinaires.

var (
	// ErrCompositionAIDisabled : tâche menu_composition fermée
	// (AI_TASK_MENU_COMPOSITION_ENABLED) ou registre IA indisponible.
	ErrCompositionAIDisabled = errors.New("composition_ai_disabled")
	// ErrCompositionInvalidRequest : sélection vide, trop grande ou mal formée.
	ErrCompositionInvalidRequest = errors.New("invalid_request")
	// ErrCompositionTooManyProducts : plus de compositionMaxProducts produits.
	ErrCompositionTooManyProducts = errors.New("too_many_products")
	// ErrCompositionNothingToRead : aucun produit sélectionné n'a de
	// description (aucun crédit consommé).
	ErrCompositionNothingToRead = errors.New("no_description")
	// ErrCompositionAIFailed : aucun lot n'a pu être lu (crédit rendu).
	ErrCompositionAIFailed = errors.New("composition_ai_failed")
)

const (
	compositionSource             = "ai_composition"
	compositionMaxProducts        = 100
	compositionMaxIngredients     = 40
	compositionBatchSize          = 20
	compositionConcurrency        = 3
	compositionRunTTL             = time.Hour
	compositionModeAdd            = "add"
	compositionModeReplace        = "replace"
	compositionStatusProposed     = "proposed"
	compositionStatusNoDesc       = "no_description"
	compositionStatusGroup        = "group"
	compositionStatusFailed       = "failed"
	compositionStatusNoIngredient = "no_ingredient"
)

// ---- contrat HTTP ----

type AICompositionPreviewRequest struct {
	ProductIDs []string `json:"product_ids"`
}

type AICompositionPreviewResponse struct {
	Products []AICompositionPreviewProduct `json:"products"`
	Credits  AICredits                     `json:"credits"`
}

// AICompositionPreviewProduct : Status vaut proposed (ingrédients lus),
// no_ingredient (description lue, aucun ingrédient cité), no_description,
// group (produit groupe, sans composition) ou failed (lecture IA impossible
// pour ce lot).
type AICompositionPreviewProduct struct {
	ProductID   string                      `json:"product_id"`
	Name        string                      `json:"name"`
	Description string                      `json:"description"`
	Status      string                      `json:"status"`
	Current     []AICompositionCurrentLine  `json:"current"`
	Proposed    []AICompositionProposedLine `json:"proposed"`
}

type AICompositionCurrentLine struct {
	ComponentID string  `json:"component_id"`
	Name        string  `json:"name"`
	Quantity    float64 `json:"quantity"`
}

// AICompositionProposedLine : ComponentID est l'ingrédient existant de même
// nom (casse, accents, pluriel ignorés), vide s'il sera créé ; InComposition
// dit s'il figure déjà dans la composition du produit.
type AICompositionProposedLine struct {
	Name          string `json:"name"`
	Category      string `json:"category"`
	Unit          string `json:"unit"`
	ComponentID   string `json:"component_id,omitempty"`
	InComposition bool   `json:"in_composition"`
}

type AICompositionApplyRequest struct {
	Products []AICompositionApplyProduct `json:"products"`
}

// AICompositionApplyProduct : Mode add garde la composition actuelle et ajoute
// les ingrédients choisis qui n'y sont pas ; replace la ramène exactement aux
// ingrédients choisis (une ligne existante choisie garde sa quantité).
type AICompositionApplyProduct struct {
	ProductID   string                    `json:"product_id"`
	Mode        string                    `json:"mode"`
	Ingredients []AICompositionIngredient `json:"ingredients"`
}

type AICompositionIngredient struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Unit     string `json:"unit"`
}

type AICompositionApplyResult struct {
	ProductsUpdated            int `json:"products_updated"`
	ProductsUnchanged          int `json:"products_unchanged"`
	ProductsSkipped            int `json:"products_skipped"`
	ComponentsCreated          int `json:"components_created"`
	ComponentCategoriesCreated int `json:"component_categories_created"`
	IngredientsSkipped         int `json:"ingredients_skipped"`
}

// ---- service ----

type compositionCreditStore interface {
	CreateDraft(ctx context.Context, d *AIDraft) error
	FinishDraft(ctx context.Context, id, status string, consumesCredit bool, errMsg string) error
	CountUsedCredits(ctx context.Context, merchantID string) (int, error)
	GetCreditsOverride(ctx context.Context, merchantID string) (int, bool, error)
	FailStaleDrafts(ctx context.Context, staleAfter time.Duration) (int64, error)
	UnitIDsByCode(ctx context.Context) (map[string]string, error)
}

type compositionRepository interface {
	LoadCompositionProducts(ctx context.Context, merchantID string, ids []string) ([]compositionProduct, error)
	LoadCompositionComponents(ctx context.Context, merchantID string) ([]compositionComponent, error)
	ApplyAICompositions(ctx context.Context, merchantID string, items []AICompositionApplyProduct, unitIDs map[string]string) (*AICompositionApplyResult, error)
}

type CompositionAIService struct {
	credits        compositionCreditStore
	repo           compositionRepository
	registry       AITaskRegistry
	log            *zap.Logger
	defaultCredits int
}

func NewCompositionAIService(credits compositionCreditStore, repo compositionRepository, registry AITaskRegistry, log *zap.Logger, defaultCredits int) *CompositionAIService {
	return &CompositionAIService{credits: credits, repo: repo, registry: registry, log: log, defaultCredits: defaultCredits}
}

func (s *CompositionAIService) provider() (ai.LLMProvider, error) {
	if s.registry == nil {
		return nil, ErrCompositionAIDisabled
	}
	provider, err := s.registry.GetProviderForTask(menuCompositionTask)
	if errors.Is(err, ai.ErrTaskDisabled) {
		return nil, ErrCompositionAIDisabled
	}
	return provider, err
}

// PreviewCompositions lit la description des produits choisis et propose leur
// composition. Les produits sans description et les groupes ne partent pas à
// l'IA ; si aucun n'y part, rien n'est consommé.
func (s *CompositionAIService) PreviewCompositions(ctx context.Context, req AICompositionPreviewRequest) (*AICompositionPreviewResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	provider, err := s.provider()
	if err != nil {
		return nil, err
	}
	ids, err := cleanProductIDs(req.ProductIDs)
	if err != nil {
		return nil, err
	}

	products, err := s.repo.LoadCompositionProducts(ctx, user.MerchantID, ids)
	if err != nil {
		return nil, err
	}
	if len(products) != len(ids) {
		return nil, models.ErrForbidden
	}

	var toRead []compositionAIInput
	byRef := map[string]int{}
	for i, p := range products {
		if p.IsGroup || strings.TrimSpace(p.Description) == "" {
			continue
		}
		ref := fmt.Sprintf("p%d", len(toRead)+1)
		byRef[ref] = i
		toRead = append(toRead, compositionAIInput{Ref: ref, Name: p.Name, Description: p.Description})
	}
	if len(toRead) == 0 {
		return nil, ErrCompositionNothingToRead
	}

	credits, err := aiCredits(ctx, s.credits, user.MerchantID, s.defaultCredits)
	if err != nil {
		return nil, err
	}
	if credits.Remaining <= 0 {
		return nil, ErrAICreditsExhausted
	}

	// Une génération interrompue (redémarrage) ne doit pas bloquer la
	// suivante : l'index unique n'autorise qu'une lecture IA en cours.
	if _, err := s.credits.FailStaleDrafts(ctx, aiStaleAfter); err != nil {
		s.log.Warn("composition IA : reprise des lectures interrompues", zap.Error(err))
	}
	run := &AIDraft{
		ID:             uuid.New().String(),
		MerchantID:     user.MerchantID,
		CreatedBy:      user.UserID,
		Source:         compositionSource,
		Status:         AIDraftProcessing,
		ConsumesCredit: true,
		Pages:          []AIDraftPage{},
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(compositionRunTTL),
	}
	if err := s.credits.CreateDraft(ctx, run); err != nil {
		return nil, err
	}

	read, readErr := s.read(ctx, provider, toRead)
	if readErr != nil {
		s.finishRun(run.ID, AIDraftExpired, false, readErr.Error())
		return nil, ErrCompositionAIFailed
	}
	s.finishRun(run.ID, AIDraftCommitted, true, "")

	components, err := s.repo.LoadCompositionComponents(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	resp := &AICompositionPreviewResponse{Products: buildCompositionPreview(products, byRef, read, components)}
	if resp.Credits, err = aiCredits(ctx, s.credits, user.MerchantID, s.defaultCredits); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *CompositionAIService) finishRun(id, status string, consumesCredit bool, errMsg string) {
	if err := s.credits.FinishDraft(context.Background(), id, status, consumesCredit, errMsg); err != nil {
		s.log.Error("composition IA : clôture de la génération", zap.String("run_id", id), zap.Error(err))
	}
}

// compositionRead : ingrédients lus par ref ; une ref absente appartient à un
// lot en échec.
type compositionRead map[string][]importer.AIIngredient

// read envoie les produits par lots de compositionBatchSize, jusqu'à
// compositionConcurrency lots à la fois. Erreur seulement si aucun lot n'a pu
// être lu.
func (s *CompositionAIService) read(ctx context.Context, provider ai.LLMProvider, inputs []compositionAIInput) (compositionRead, error) {
	cfg, _ := s.registry.TaskConfig(menuCompositionTask)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, compositionConcurrency)
		out     = compositionRead{}
		okCount int
		lastErr error
	)
	for start := 0; start < len(inputs); start += compositionBatchSize {
		batch := inputs[start:min(start+compositionBatchSize, len(inputs))]
		wg.Add(1)
		sem <- struct{}{}
		go func(batch []compositionAIInput) {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := s.readBatch(ctx, provider, cfg, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lastErr = err
				return
			}
			okCount++
			for ref, ings := range result {
				out[ref] = ings
			}
		}(batch)
	}
	wg.Wait()

	if okCount == 0 {
		if lastErr == nil {
			lastErr = errors.New("aucun lot lu")
		}
		return nil, lastErr
	}
	return out, nil
}

func (s *CompositionAIService) readBatch(ctx context.Context, provider ai.LLMProvider, cfg ai.TaskConfig, batch []compositionAIInput) (compositionRead, error) {
	resp, err := provider.Complete(ctx, ai.CompletionRequest{
		Task:         menuCompositionTask,
		SystemPrompt: menuCompositionSystemPrompt,
		UserPrompt:   menuCompositionUserPrompt(batch),
		MaxTokens:    cfg.MaxTokens,
		JSONSchema:   importer.AICompositionSchema,
	})
	if err != nil {
		ai.LogLLMCall(s.log, menuCompositionTask, provider.Name(), "", 0, 0, 0, false, err)
		return nil, err
	}
	ai.LogLLMCall(s.log, menuCompositionTask, provider.Name(), resp.Model, resp.InputTokens, resp.OutputTokens, resp.LatencyMs, false, nil)
	if resp.StopReason == ai.StopReasonMaxTokens {
		return nil, errors.New("réponse tronquée (max_tokens)")
	}
	decoded, err := importer.DecodeAIComposition(resp.Content)
	if err != nil {
		s.log.Warn("composition IA : réponse illisible", zap.Error(err))
		return nil, err
	}

	known := make(map[string]struct{}, len(batch))
	for _, in := range batch {
		known[in.Ref] = struct{}{}
	}
	out := compositionRead{}
	for _, p := range decoded.Products {
		if _, ok := known[p.Ref]; ok {
			out[p.Ref] = importer.CleanIngredients(p.Ingredients, compositionMaxIngredients)
		}
	}
	// Une ref oubliée par le modèle a été lue sans ingrédient.
	for ref := range known {
		if _, ok := out[ref]; !ok {
			out[ref] = nil
		}
	}
	return out, nil
}

// buildCompositionPreview assemble la réponse, dans l'ordre des produits lus.
func buildCompositionPreview(products []compositionProduct, byRef map[string]int, read compositionRead, components []compositionComponent) []AICompositionPreviewProduct {
	componentByKey := make(map[string]compositionComponent, len(components))
	for _, c := range components {
		if key := importer.IngredientKey(c.Name); key != "" {
			if _, dup := componentByKey[key]; !dup {
				componentByKey[key] = c
			}
		}
	}
	refByIndex := make(map[int]string, len(byRef))
	for ref, i := range byRef {
		refByIndex[i] = ref
	}

	out := make([]AICompositionPreviewProduct, 0, len(products))
	for i, p := range products {
		entry := AICompositionPreviewProduct{
			ProductID:   p.ID,
			Name:        p.Name,
			Description: p.Description,
			Current:     []AICompositionCurrentLine{},
			Proposed:    []AICompositionProposedLine{},
		}
		inComposition := map[string]struct{}{}
		for _, l := range p.Lines {
			entry.Current = append(entry.Current, AICompositionCurrentLine{ComponentID: l.ComponentID, Name: l.Name, Quantity: l.Quantity})
			inComposition[l.ComponentID] = struct{}{}
			inComposition["key:"+importer.IngredientKey(l.Name)] = struct{}{}
		}

		ref, sent := refByIndex[i]
		ings, readOK := read[ref]
		switch {
		case p.IsGroup:
			entry.Status = compositionStatusGroup
		case !sent:
			entry.Status = compositionStatusNoDesc
		case !readOK:
			entry.Status = compositionStatusFailed
		case len(ings) == 0:
			entry.Status = compositionStatusNoIngredient
		default:
			entry.Status = compositionStatusProposed
			for _, ing := range ings {
				line := AICompositionProposedLine{Name: ing.Name, Category: ing.Category, Unit: ing.Unit}
				key := importer.IngredientKey(ing.Name)
				if c, ok := componentByKey[key]; ok {
					line.ComponentID = c.ID
					line.Name = c.Name
					_, line.InComposition = inComposition[c.ID]
				}
				if _, ok := inComposition["key:"+key]; ok {
					line.InComposition = true
				}
				entry.Proposed = append(entry.Proposed, line)
			}
		}
		out = append(out, entry)
	}
	return out
}

// ApplyCompositions écrit les compositions gardées en relecture.
func (s *CompositionAIService) ApplyCompositions(ctx context.Context, req AICompositionApplyRequest) (*AICompositionApplyResult, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if len(req.Products) == 0 {
		return nil, ErrCompositionInvalidRequest
	}
	if len(req.Products) > compositionMaxProducts {
		return nil, ErrCompositionTooManyProducts
	}
	seen := map[string]struct{}{}
	for _, p := range req.Products {
		if !menuNumericID(p.ProductID) || (p.Mode != compositionModeAdd && p.Mode != compositionModeReplace) ||
			len(p.Ingredients) > compositionMaxIngredients {
			return nil, ErrCompositionInvalidRequest
		}
		if _, dup := seen[p.ProductID]; dup {
			return nil, ErrCompositionInvalidRequest
		}
		seen[p.ProductID] = struct{}{}
	}

	unitIDs, err := s.credits.UnitIDsByCode(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ApplyAICompositions(ctx, user.MerchantID, req.Products, unitIDs)
}

// planCompositionChanges rend les lignes à retirer (requires.id) et les
// ingrédients à ajouter. add : rien n'est retiré, seuls les ingrédients
// choisis absents sont ajoutés. replace : les lignes dont l'ingrédient n'est
// pas choisi sont retirées. Une ligne gardée n'est jamais réécrite.
func planCompositionChanges(current []compositionLine, chosen []compositionComponent, mode string) (toDelete []string, toInsert []compositionComponent) {
	present := make(map[string]struct{}, len(current))
	for _, l := range current {
		present[l.ComponentID] = struct{}{}
	}
	wanted := make(map[string]struct{}, len(chosen))
	for _, c := range chosen {
		if _, dup := wanted[c.ID]; dup {
			continue
		}
		wanted[c.ID] = struct{}{}
		if _, ok := present[c.ID]; !ok {
			toInsert = append(toInsert, c)
		}
	}
	if mode == compositionModeReplace {
		for _, l := range current {
			if _, ok := wanted[l.ComponentID]; !ok {
				toDelete = append(toDelete, l.RequireID)
			}
		}
	}
	return toDelete, toInsert
}

// cleanProductIDs valide et dédoublonne la sélection.
func cleanProductIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !menuNumericID(id) {
			return nil, ErrCompositionInvalidRequest
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	switch {
	case len(out) == 0:
		return nil, ErrCompositionInvalidRequest
	case len(out) > compositionMaxProducts:
		return nil, ErrCompositionTooManyProducts
	}
	return out, nil
}
