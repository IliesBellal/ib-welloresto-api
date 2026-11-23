package scannorderseo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/models"
)

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func TestValidateUpdate(t *testing.T) {
	links := []string{"instagram.com/pizzeria", "", "https://instagram.com/pizzeria/", "https://g.page/r/abc"}
	cuisines := []string{"pizza", "italien", "pizza"}
	u, err := validateUpdate(&UpdateRequest{
		Title:        strp("  Da Mario\n Pizzeria  "),
		Description:  strp("Pizzas   au feu de bois.\n\nCalzones."),
		CuisineTypes: &cuisines,
		Links:        &links,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *u.Title != "Da Mario Pizzeria" || *u.Description != "Pizzas au feu de bois. Calzones." {
		t.Fatalf("textes non repliés : %q / %q", *u.Title, *u.Description)
	}
	if *u.Cuisines != "pizza,italien" {
		t.Fatalf("Cuisines = %q", *u.Cuisines)
	}
	var stored []string
	if err := json.Unmarshal([]byte(*u.Links), &stored); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://instagram.com/pizzeria", "https://g.page/r/abc"}; !reflect.DeepEqual(stored, want) {
		t.Fatalf("Links = %v, want %v", stored, want)
	}

	// listes vides : effacement
	empty := []string{}
	u, err = validateUpdate(&UpdateRequest{CuisineTypes: &empty, Links: &empty})
	if err != nil || *u.Cuisines != "" || *u.Links != "" || u.Title != nil {
		t.Fatalf("effacement : %+v, %v", u, err)
	}
}

func TestValidateUpdate_Errors(t *testing.T) {
	tooMany := []string{"pizza", "burger", "kebab", "tacos"}
	unknown := []string{"pizza", "martien"}
	badLink := []string{"pas un lien"}
	cases := map[string]struct {
		req   UpdateRequest
		field string
	}{
		"titre trop long":         {UpdateRequest{Title: strp(strings.Repeat("é", MaxTitleLen+1))}, "title"},
		"description trop longue": {UpdateRequest{Description: strp(strings.Repeat("a", MaxDescriptionLen+1))}, "description"},
		"trop de cuisines":        {UpdateRequest{CuisineTypes: &tooMany}, "cuisine_types"},
		"cuisine inconnue":        {UpdateRequest{CuisineTypes: &unknown}, "cuisine_types"},
		"lien invalide":           {UpdateRequest{Links: &badLink}, "links"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := validateUpdate(&tc.req)
			var fe *FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("err = %v, want FieldError sur %s", err, tc.field)
			}
		})
	}
	// pile à la limite : accepté
	if _, err := validateUpdate(&UpdateRequest{Title: strp(strings.Repeat("é", MaxTitleLen))}); err != nil {
		t.Fatalf("titre à la limite refusé : %v", err)
	}
}

func TestSanitizeSuggestion(t *testing.T) {
	got := sanitizeSuggestion(Suggestion{
		Description:  "  « Pizzeria à Metz :   pizzas au feu de bois. »  ",
		CuisineTypes: []string{"pizza", "martien", "pizza", "italien", "desserts", "glacier"},
	})
	if got.Description != "Pizzeria à Metz : pizzas au feu de bois." {
		t.Fatalf("Description = %q", got.Description)
	}
	if want := []string{"pizza", "italien", "desserts"}; !reflect.DeepEqual(got.CuisineTypes, want) {
		t.Fatalf("CuisineTypes = %v, want %v", got.CuisineTypes, want)
	}
	long := sanitizeSuggestion(Suggestion{Description: strings.Repeat("mot ", 200)})
	if n := len([]rune(long.Description)); n > MaxDescriptionLen+1 {
		t.Fatalf("description non bornée : %d caractères", n)
	}
}

func testMenu() *models.MenuResponse {
	return &models.MenuResponse{ProductsTypes: []models.ProductCategory{
		{CategoryName: "Pizzas", Products: []models.ProductEntry{
			{Name: "Margherita", Description: strp("Tomate, mozzarella,\n basilic"), IsAvailableOnSNO: boolp(true)},
			{Name: "Reine", IsAvailableOnSNO: boolp(true)},
			{Name: "Cachée", IsAvailableOnSNO: boolp(false)},
			{Name: "Masquée", IsAvailableOnSNO: boolp(true), Status: "not_available"},
		}},
		{CategoryName: "Menus", Products: []models.ProductEntry{
			{Name: "Menu midi", IsProductGroup: boolp(true), IsAvailableOnSNO: boolp(true), SubProducts: []models.ProductEntry{
				{Name: "Menu pizza", IsAvailableOnSNO: boolp(true)},
			}},
		}},
		{CategoryName: "Vide", Products: []models.ProductEntry{{Name: "Hors ligne", IsAvailableOnSNO: boolp(false)}}},
	}}
}

func TestPromptMenu(t *testing.T) {
	cats, count := promptMenu(testMenu())
	if count != 3 || len(cats) != 2 {
		t.Fatalf("promptMenu = %d produits, %d catégories ; want 3, 2 : %+v", count, len(cats), cats)
	}
	if cats[0].Products[0].Description != "Tomate, mozzarella, basilic" {
		t.Fatalf("description non repliée : %q", cats[0].Products[0].Description)
	}
	if cats[1].Products[0].Name != "Menu pizza" {
		t.Fatalf("sous-produit du groupe absent : %+v", cats[1])
	}
	prompt := seoProfileUserPrompt("Da Mario", "Metz", cats)
	for _, want := range []string{"Établissement : Da Mario", "Ville : Metz", "## Pizzas", "- Margherita : Tomate, mozzarella, basilic", "- Reine\n"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("invite sans %q :\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Cachée") || strings.Contains(prompt, "Masquée") {
		t.Fatalf("produit invisible envoyé à l'IA :\n%s", prompt)
	}
}

func TestSeoProfileSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(seoProfileSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatal("additionalProperties doit valoir false (structured outputs)")
	}
	if !strings.Contains(seoProfileSystemPrompt, "pizza : Pizza — Pizzeria") {
		t.Fatal("liste des cuisines absente de l'invite système")
	}
}

// ---- Suggest, avec un faux fournisseur ----

type fakeStore struct {
	row     settingsRow
	updated *update
}

func (f *fakeStore) GetSettings(context.Context, string) (*settingsRow, error) { return &f.row, nil }
func (f *fakeStore) UpdateSettings(_ context.Context, _ string, u update, _ time.Time) error {
	f.updated = &u
	return nil
}
func (f *fakeStore) GetMerchantIdentity(context.Context, string) (string, string, error) {
	return "Da Mario", "Metz", nil
}

type fakeMenu struct{ menu *models.MenuResponse }

func (f fakeMenu) GetMenuFromMerchantIdWithMarketing(context.Context, string) (*models.MenuResponse, error) {
	return f.menu, nil
}

type fakeProvider struct {
	content string
	err     error
	got     ai.CompletionRequest
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Complete(_ context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	p.got = req
	if p.err != nil {
		return nil, p.err
	}
	return &ai.CompletionResponse{Content: p.content, StopReason: "end_turn"}, nil
}

type fakeRegistry struct {
	provider ai.LLMProvider
	disabled bool
}

func (r fakeRegistry) GetProviderForTask(string) (ai.LLMProvider, error) {
	if r.disabled {
		return nil, ai.ErrTaskDisabled
	}
	return r.provider, nil
}
func (r fakeRegistry) TaskConfig(string) (ai.TaskConfig, bool) {
	return ai.TaskConfig{MaxTokens: 8000}, true
}

func newTestService(store *fakeStore, menu *models.MenuResponse, reg AITaskRegistry) *Service {
	return &Service{repo: store, menu: fakeMenu{menu}, registry: reg, log: zap.NewNop(), now: time.Now}
}

func TestSuggest(t *testing.T) {
	p := &fakeProvider{content: `{"description":"Pizzeria à Metz : margherita et reine.","cuisine_types":["pizza","martien"]}`}
	svc := newTestService(&fakeStore{}, testMenu(), fakeRegistry{provider: p})

	got, err := svc.Suggest(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Pizzeria à Metz : margherita et reine." || !reflect.DeepEqual(got.CuisineTypes, []string{"pizza"}) {
		t.Fatalf("Suggest = %+v", got)
	}
	if p.got.Task != seoProfileTask || len(p.got.JSONSchema) == 0 || p.got.MaxTokens != 8000 || !strings.Contains(p.got.UserPrompt, "Margherita") {
		t.Fatalf("requête IA inattendue : %+v", p.got)
	}
}

func TestSuggest_Errors(t *testing.T) {
	ok := &fakeProvider{content: `{"description":"x","cuisine_types":[]}`}
	tiny := &models.MenuResponse{ProductsTypes: []models.ProductCategory{{CategoryName: "A", Products: []models.ProductEntry{{Name: "Seul", IsAvailableOnSNO: boolp(true)}}}}}

	cases := map[string]struct {
		svc  *Service
		want error
	}{
		"tâche fermée":      {newTestService(&fakeStore{}, testMenu(), fakeRegistry{disabled: true}), ErrSuggestDisabled},
		"sans registre":     {newTestService(&fakeStore{}, testMenu(), nil), ErrSuggestDisabled},
		"carte trop petite": {newTestService(&fakeStore{}, tiny, fakeRegistry{provider: ok}), ErrMenuTooSmall},
		"refus du modèle":   {newTestService(&fakeStore{}, testMenu(), fakeRegistry{provider: &fakeProvider{err: ai.ErrRefused}}), ErrSuggestFailed},
		"JSON illisible":    {newTestService(&fakeStore{}, testMenu(), fakeRegistry{provider: &fakeProvider{content: "pas du json"}}), ErrSuggestFailed},
		"réponse vide":      {newTestService(&fakeStore{}, testMenu(), fakeRegistry{provider: &fakeProvider{content: `{"description":"","cuisine_types":["martien"]}`}}), ErrSuggestFailed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.svc.Suggest(context.Background(), "42"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGetSettings(t *testing.T) {
	store := &fakeStore{row: settingsRow{
		Title: "Da Mario", Cuisines: "pizza,Cuisine du marché", Links: `["https://instagram.com/damario"]`,
	}}
	svc := newTestService(store, nil, fakeRegistry{disabled: true})
	got, err := svc.GetSettings(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.CuisineTypes, []string{"pizza"}) || !reflect.DeepEqual(got.Links, []string{"https://instagram.com/damario"}) {
		t.Fatalf("GetSettings = %+v", got)
	}
	if got.SuggestAvailable || len(got.CuisineOptions) == 0 || got.Limits.CuisinesMax != 3 {
		t.Fatalf("GetSettings méta = %+v", got)
	}
	empty, _ := newTestService(&fakeStore{}, nil, nil).GetSettings(context.Background(), "42")
	if empty.Links == nil || empty.CuisineTypes == nil {
		t.Fatal("listes nil : le back-office attend des tableaux")
	}
}
