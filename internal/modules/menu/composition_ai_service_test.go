package menu

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu/importer"
)

// fakeCompositionRepo garde les produits et ingrédients en mémoire.
type fakeCompositionRepo struct {
	products   []compositionProduct
	components []compositionComponent
	applied    []AICompositionApplyProduct
}

func (f *fakeCompositionRepo) LoadCompositionProducts(_ context.Context, _ string, ids []string) ([]compositionProduct, error) {
	var out []compositionProduct
	for _, p := range f.products {
		for _, id := range ids {
			if p.ID == id {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (f *fakeCompositionRepo) LoadCompositionComponents(context.Context, string) ([]compositionComponent, error) {
	return f.components, nil
}

func (f *fakeCompositionRepo) ApplyAICompositions(_ context.Context, _ string, items []AICompositionApplyProduct, _ map[string]string) (*AICompositionApplyResult, error) {
	f.applied = items
	return &AICompositionApplyResult{ProductsUpdated: len(items)}, nil
}

// fakeCompositionProvider répond à chaque lot en citant un ingrédient par
// produit, nommé d'après la description ; err fait échouer tous les lots.
type fakeCompositionProvider struct {
	mu    sync.Mutex
	calls []ai.CompletionRequest
	err   error
}

var compositionRefLine = regexp.MustCompile(`(?m)^ref : (p\d+)\nnom : .*\ndescription : (.*)$`)

func (p *fakeCompositionProvider) Name() string { return "anthropic" }

func (p *fakeCompositionProvider) Complete(_ context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req)
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	var products []string
	for _, m := range compositionRefLine.FindAllStringSubmatch(req.UserPrompt, -1) {
		products = append(products, fmt.Sprintf(`{"ref":%q,"ingredients":[{"name":%q,"category":"vegetables","unit":"G"}]}`, m[1], m[2]))
	}
	return &ai.CompletionResponse{Content: `{"products":[` + strings.Join(products, ",") + `]}`, StopReason: "end_turn"}, nil
}

type compositionTestEnv struct {
	service  *CompositionAIService
	credits  *fakeAIDraftStore
	repo     *fakeCompositionRepo
	provider *fakeCompositionProvider
}

func newCompositionTestEnv(t *testing.T, enabled bool) *compositionTestEnv {
	t.Helper()
	provider := &fakeCompositionProvider{}
	registry, err := ai.NewRegistry(ai.AIConfig{Tasks: map[string]ai.TaskConfig{
		menuCompositionTask: {Provider: "anthropic", Enabled: enabled, MaxTokens: 8000},
	}}, map[string]ai.LLMProvider{"anthropic": provider})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	repo := &fakeCompositionRepo{
		products: []compositionProduct{
			{ID: "1", Name: "Burger", Description: "Tomate", Lines: []compositionLine{{RequireID: "100", ComponentID: "7", Name: "Tomates", Quantity: 30}}},
			{ID: "2", Name: "Salade", Description: "Roquette"},
			{ID: "3", Name: "Coca", Description: ""},
			{ID: "4", Name: "Menu", Description: "Burger + boisson", IsGroup: true},
		},
		components: []compositionComponent{{ID: "7", Name: "Tomates", UnitID: "2"}},
	}
	credits := newFakeAIDraftStore()
	env := &compositionTestEnv{credits: credits, repo: repo, provider: provider}
	env.service = NewCompositionAIService(credits, repo, registry, zap.NewNop(), 2)
	return env
}

func TestCompositionAI_PreviewProposesAndConsumesOneCredit(t *testing.T) {
	env := newCompositionTestEnv(t, true)

	resp, err := env.service.PreviewCompositions(aiTestContext(), AICompositionPreviewRequest{ProductIDs: []string{"1", "2", "3", "4", "2"}})
	if err != nil {
		t.Fatalf("PreviewCompositions: %v", err)
	}

	// Un seul appel, sans le produit sans description ni le groupe.
	if len(env.provider.calls) != 1 {
		t.Fatalf("appels IA = %d, want 1", len(env.provider.calls))
	}
	call := env.provider.calls[0]
	if call.Task != menuCompositionTask || string(call.JSONSchema) != string(importer.AICompositionSchema) || call.SystemPrompt != menuCompositionSystemPrompt {
		t.Errorf("requête IA inattendue : tâche %q", call.Task)
	}
	if strings.Contains(call.UserPrompt, "Coca") || strings.Contains(call.UserPrompt, "Menu") {
		t.Errorf("produits sans description ou groupes envoyés à l'IA : %q", call.UserPrompt)
	}

	status := map[string]string{}
	for _, p := range resp.Products {
		status[p.ProductID] = p.Status
	}
	if want := map[string]string{"1": "proposed", "2": "proposed", "3": "no_description", "4": "group"}; !reflect.DeepEqual(status, want) {
		t.Errorf("statuts = %v, want %v", status, want)
	}

	// « Tomate » retrouve l'ingrédient existant « Tomates », déjà dans la
	// composition du burger ; « Roquette » sera créé.
	burger := resp.Products[0]
	if len(burger.Current) != 1 || burger.Proposed[0].ComponentID != "7" || burger.Proposed[0].Name != "Tomates" || !burger.Proposed[0].InComposition {
		t.Errorf("burger = %+v", burger)
	}
	salade := resp.Products[1]
	if salade.Proposed[0].ComponentID != "" || salade.Proposed[0].InComposition || salade.Proposed[0].Name != "Roquette" {
		t.Errorf("salade = %+v", salade)
	}

	if resp.Credits.Used != 1 || resp.Credits.Remaining != 1 {
		t.Errorf("crédits = %+v, want 1 utilisé sur 2", resp.Credits)
	}
}

func TestCompositionAI_PreviewSplitsIntoBatches(t *testing.T) {
	env := newCompositionTestEnv(t, true)
	env.repo.products = nil
	var ids []string
	for i := 1; i <= 45; i++ {
		id := fmt.Sprint(i)
		ids = append(ids, id)
		env.repo.products = append(env.repo.products, compositionProduct{ID: id, Name: "P" + id, Description: "Ingrédient " + id})
	}

	resp, err := env.service.PreviewCompositions(aiTestContext(), AICompositionPreviewRequest{ProductIDs: ids})
	if err != nil {
		t.Fatalf("PreviewCompositions: %v", err)
	}
	if len(env.provider.calls) != 3 {
		t.Errorf("appels IA = %d, want 3 lots (20 + 20 + 5)", len(env.provider.calls))
	}
	for _, p := range resp.Products {
		if p.Status != compositionStatusProposed || p.Proposed[0].Name != "Ingrédient "+p.ProductID {
			t.Fatalf("produit %s = %+v : ingrédients mal rattachés", p.ProductID, p)
		}
	}
	if resp.Credits.Used != 1 {
		t.Errorf("crédits utilisés = %d, want 1 par génération, pas par lot", resp.Credits.Used)
	}
}

func TestCompositionAI_PreviewFailureRefundsCredit(t *testing.T) {
	env := newCompositionTestEnv(t, true)
	env.provider.err = errors.New("panne")

	if _, err := env.service.PreviewCompositions(aiTestContext(), AICompositionPreviewRequest{ProductIDs: []string{"1"}}); !errors.Is(err, ErrCompositionAIFailed) {
		t.Fatalf("err = %v, want ErrCompositionAIFailed", err)
	}
	if used, _ := env.credits.CountUsedCredits(context.Background(), aiTestMerchant); used != 0 {
		t.Errorf("crédits utilisés = %d, want 0 (rendu)", used)
	}
	for _, d := range env.credits.drafts {
		if d.Source != compositionSource || d.Status != AIDraftExpired {
			t.Errorf("génération = source %q statut %q, want ai_composition / expired", d.Source, d.Status)
		}
	}
}

func TestCompositionAI_PreviewGuards(t *testing.T) {
	ctx := aiTestContext()
	cases := []struct {
		name    string
		enabled bool
		ids     []string
		setup   func(env *compositionTestEnv)
		want    error
	}{
		{name: "tâche fermée", enabled: false, ids: []string{"1"}, want: ErrCompositionAIDisabled},
		{name: "sélection vide", enabled: true, ids: nil, want: ErrCompositionInvalidRequest},
		{name: "identifiant invalide", enabled: true, ids: []string{"1 OR 1=1"}, want: ErrCompositionInvalidRequest},
		{name: "produit d'un autre marchand", enabled: true, ids: []string{"1", "99"}, want: models.ErrForbidden},
		{name: "rien à lire", enabled: true, ids: []string{"3", "4"}, want: ErrCompositionNothingToRead},
		{name: "plus de crédit", enabled: true, ids: []string{"1"}, setup: func(env *compositionTestEnv) {
			_ = env.credits.SetCreditsOverride(context.Background(), aiTestMerchant, 0, "staff")
		}, want: ErrAICreditsExhausted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newCompositionTestEnv(t, tc.enabled)
			if tc.setup != nil {
				tc.setup(env)
			}
			if _, err := env.service.PreviewCompositions(ctx, AICompositionPreviewRequest{ProductIDs: tc.ids}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(env.provider.calls) != 0 {
				t.Errorf("l'IA a été appelée")
			}
			if used, _ := env.credits.CountUsedCredits(context.Background(), aiTestMerchant); used != 0 {
				t.Errorf("crédit consommé")
			}
		})
	}

	t.Run("trop de produits", func(t *testing.T) {
		env := newCompositionTestEnv(t, true)
		ids := make([]string, compositionMaxProducts+1)
		for i := range ids {
			ids[i] = fmt.Sprint(i + 1)
		}
		if _, err := env.service.PreviewCompositions(ctx, AICompositionPreviewRequest{ProductIDs: ids}); !errors.Is(err, ErrCompositionTooManyProducts) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCompositionAI_ApplyValidatesRequest(t *testing.T) {
	env := newCompositionTestEnv(t, true)
	ctx := aiTestContext()
	ing := []AICompositionIngredient{{Name: "Tomate", Category: "vegetables", Unit: "G"}}

	bad := []AICompositionApplyRequest{
		{},
		{Products: []AICompositionApplyProduct{{ProductID: "1", Mode: "merge", Ingredients: ing}}},
		{Products: []AICompositionApplyProduct{{ProductID: "abc", Mode: "add", Ingredients: ing}}},
		{Products: []AICompositionApplyProduct{{ProductID: "1", Mode: "add"}, {ProductID: "1", Mode: "replace"}}},
	}
	for i, req := range bad {
		if _, err := env.service.ApplyCompositions(ctx, req); !errors.Is(err, ErrCompositionInvalidRequest) {
			t.Errorf("cas %d : err = %v, want ErrCompositionInvalidRequest", i, err)
		}
	}

	ok := AICompositionApplyRequest{Products: []AICompositionApplyProduct{{ProductID: "1", Mode: "replace", Ingredients: ing}}}
	if _, err := env.service.ApplyCompositions(ctx, ok); err != nil || len(env.repo.applied) != 1 {
		t.Fatalf("ApplyCompositions = %v, appliqué %d", err, len(env.repo.applied))
	}
	if len(env.provider.calls) != 0 {
		t.Errorf("l'application ne doit pas appeler l'IA")
	}
}

func TestPlanCompositionChanges(t *testing.T) {
	current := []compositionLine{
		{RequireID: "100", ComponentID: "7"},
		{RequireID: "101", ComponentID: "8"},
	}
	chosen := []compositionComponent{{ID: "8"}, {ID: "9"}, {ID: "9"}}

	del, ins := planCompositionChanges(current, chosen, compositionModeAdd)
	if len(del) != 0 || len(ins) != 1 || ins[0].ID != "9" {
		t.Errorf("add : retirées %v, ajoutées %v ; want rien retiré, 9 ajouté", del, ins)
	}

	del, ins = planCompositionChanges(current, chosen, compositionModeReplace)
	if !reflect.DeepEqual(del, []string{"100"}) || len(ins) != 1 || ins[0].ID != "9" {
		t.Errorf("replace : retirées %v, ajoutées %v ; want 100 retiré (7 non choisi), 9 ajouté, 8 gardé tel quel", del, ins)
	}

	del, ins = planCompositionChanges(current, nil, compositionModeReplace)
	if len(del) != 2 || len(ins) != 0 {
		t.Errorf("replace sans choix : retirées %v, want toutes", del)
	}
}
