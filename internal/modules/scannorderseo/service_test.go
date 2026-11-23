package scannorderseo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
func (f *fakeStore) GetPlaceQuery(context.Context, string) (*placeQuery, error) {
	return &placeQuery{Name: "Da Mario", Address: "3 rue Serpenoise", ZipCode: "57000", City: "Metz", Lat: 49.1, Lng: 6.17}, nil
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

// ---- Fiche Google ----

func TestValidateUpdate_Google(t *testing.T) {
	on := true
	u, err := validateUpdate(&UpdateRequest{
		GooglePlaceID:        strp(" ChIJN1t_tDeuEmsRUsoyG83frY4 "),
		GoogleMapsURL:        strp("maps.app.goo.gl/AbC123"),
		GoogleReviewURL:      strp("https://g.page/r/CabcDEF123/review"),
		ReviewRequestEnabled: &on,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *u.GooglePlaceID != "ChIJN1t_tDeuEmsRUsoyG83frY4" || *u.GoogleMapsURL != "https://maps.app.goo.gl/AbC123" ||
		*u.GoogleReviewURL != "https://g.page/r/CabcDEF123/review" || !*u.ReviewRequestEnabled {
		t.Fatalf("update = %+v", u)
	}
	// Effacement : chaînes vides acceptées.
	if u, err := validateUpdate(&UpdateRequest{GoogleReviewURL: strp(" ")}); err != nil || *u.GoogleReviewURL != "" {
		t.Fatalf("effacement = %+v, %v", u, err)
	}
	for field, req := range map[string]UpdateRequest{
		"google_review_url": {GoogleReviewURL: strp("https://evil.example/avis")},
		"google_maps_url":   {GoogleMapsURL: strp("https://instagram.com/x")},
		"google_place_id":   {GooglePlaceID: strp("<script>")},
	} {
		_, err := validateUpdate(&req)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s : err = %v", field, err)
		}
	}
}

func TestUpdateSettings_ReviewRequestNeedsReviewURL(t *testing.T) {
	on := true
	// Activer sans lien d'avis : refusé.
	store := &fakeStore{}
	_, err := newTestService(store, nil, nil).UpdateSettings(context.Background(), "42", &UpdateRequest{ReviewRequestEnabled: &on})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "review_request_enabled" || fe.Reason != "missing_review_url" || store.updated != nil {
		t.Fatalf("activation sans lien : err = %v, écrit = %v", err, store.updated)
	}

	// Activer en donnant le lien dans la même requête : accepté.
	store = &fakeStore{}
	if _, err := newTestService(store, nil, nil).UpdateSettings(context.Background(), "42", &UpdateRequest{
		ReviewRequestEnabled: &on, GoogleReviewURL: strp("https://g.page/r/Cabc/review"),
	}); err != nil || !*store.updated.ReviewRequestEnabled {
		t.Fatalf("activation avec lien : %v, %+v", err, store.updated)
	}

	// Effacer le lien d'une demande active : la demande est coupée.
	store = &fakeStore{row: settingsRow{GoogleReviewURL: "https://g.page/r/Cabc/review", ReviewRequestEnabled: true}}
	if _, err := newTestService(store, nil, nil).UpdateSettings(context.Background(), "42", &UpdateRequest{GoogleReviewURL: strp("")}); err != nil ||
		store.updated.ReviewRequestEnabled == nil || *store.updated.ReviewRequestEnabled {
		t.Fatalf("effacement du lien : %v, %+v", err, store.updated)
	}
}

type fakeFinder struct {
	calls   []string
	results [][]foundPlace
	err     error
}

func (f *fakeFinder) FindPlaces(_ context.Context, text string, _, _ float64) ([]foundPlace, error) {
	f.calls = append(f.calls, text)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.calls) <= len(f.results) {
		return f.results[len(f.calls)-1], nil
	}
	return nil, nil
}

func TestSearchGoogle(t *testing.T) {
	finder := &fakeFinder{results: [][]foundPlace{
		nil, // nom + adresse : rien
		{{PlaceID: "ChIJN1t_tDeuEmsRUsoyG83frY4", Name: "Da Mario", Address: "3 Rue Serpenoise, 57000 Metz"}, {PlaceID: "bad"}},
	}}
	svc := newTestService(&fakeStore{}, nil, nil)
	svc.places = finder

	got, err := svc.SearchGoogle(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Da Mario, 3 rue Serpenoise, 57000 Metz", "Da Mario Metz"}; !reflect.DeepEqual(finder.calls, want) {
		t.Fatalf("recherches = %q, want %q", finder.calls, want)
	}
	if len(got) != 1 || got[0].ReviewURL != "https://search.google.com/local/writereview?placeid=ChIJN1t_tDeuEmsRUsoyG83frY4" ||
		got[0].MapsURL != "https://www.google.com/maps/place/?q=place_id:ChIJN1t_tDeuEmsRUsoyG83frY4" {
		t.Fatalf("candidats = %+v", got)
	}

	svc.places = &fakeFinder{err: errors.New("REQUEST_DENIED")}
	if _, err := svc.SearchGoogle(context.Background(), "42"); !errors.Is(err, ErrGoogleSearchUnavailable) {
		t.Fatalf("refus Google : %v", err)
	}
	svc.places = nil
	if _, err := svc.SearchGoogle(context.Background(), "42"); !errors.Is(err, ErrGoogleSearchUnavailable) {
		t.Fatalf("sans clé : %v", err)
	}
}

func TestPlacesClient(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		if query.Get("input") == "refus" {
			_, _ = w.Write([]byte(`{"status":"REQUEST_DENIED","error_message":"This API project is not authorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"OK","candidates":[{"place_id":"ChIJ123456789","name":"Da Mario","formatted_address":"3 Rue Serpenoise, 57000 Metz"}]}`))
	}))
	defer srv.Close()

	c := &placesClient{apiKey: "k", endpoint: srv.URL, http: srv.Client()}
	got, err := c.FindPlaces(context.Background(), "Da Mario Metz", 49.1, 6.17)
	if err != nil || len(got) != 1 || got[0].PlaceID != "ChIJ123456789" || got[0].Address != "3 Rue Serpenoise, 57000 Metz" {
		t.Fatalf("FindPlaces = %+v, %v", got, err)
	}
	if query.Get("inputtype") != "textquery" || query.Get("fields") != "place_id,name,formatted_address" ||
		query.Get("locationbias") != "circle:2000@49.100000,6.170000" || query.Get("key") != "k" {
		t.Fatalf("requête = %v", query)
	}
	if _, err := c.FindPlaces(context.Background(), "refus", 0, 0); err == nil || !strings.Contains(err.Error(), "REQUEST_DENIED") {
		t.Fatalf("refus : %v", err)
	}
	if NewPlacesClient(" ") != nil {
		t.Fatal("sans clé : client attendu nil")
	}
}
