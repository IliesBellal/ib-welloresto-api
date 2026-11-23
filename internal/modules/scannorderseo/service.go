package scannorderseo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.uber.org/zap"
	"golang.org/x/text/unicode/norm"

	"welloresto-api/internal/ai"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// AITaskRegistry : sous-ensemble de ai.Registry utilisé ici (nil = suggestion
// indisponible).
type AITaskRegistry interface {
	GetProviderForTask(task string) (ai.LLMProvider, error)
	TaskConfig(task string) (ai.TaskConfig, bool)
}

// MenuReader lit la carte d'un merchant (menu.MenuService).
type MenuReader interface {
	GetMenuFromMerchantIdWithMarketing(ctx context.Context, merchantID string) (*models.MenuResponse, error)
}

type store interface {
	GetSettings(ctx context.Context, merchantID string) (*settingsRow, error)
	UpdateSettings(ctx context.Context, merchantID string, u update, now time.Time) error
	GetMerchantIdentity(ctx context.Context, merchantID string) (name, city string, err error)
	GetPlaceQuery(ctx context.Context, merchantID string) (*placeQuery, error)
}

type Service struct {
	repo     store
	menu     MenuReader
	registry AITaskRegistry
	places   PlaceFinder // nil : recherche de la fiche Google indisponible
	log      *zap.Logger
	now      func() time.Time
}

func NewService(repo *Repository, menu MenuReader, registry AITaskRegistry, places PlaceFinder, log *zap.Logger) *Service {
	return &Service{repo: repo, menu: menu, registry: registry, places: places, log: log, now: time.Now}
}

// GetSettings rend les réglages enregistrés, la liste fermée des cuisines et
// les bornes de saisie.
func (s *Service) GetSettings(ctx context.Context, merchantID string) (*Settings, error) {
	row, err := s.repo.GetSettings(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	out := &Settings{
		Title:          row.Title,
		Description:    row.Description,
		CuisineTypes:   []string{},
		Links:          helpers.ParseScanNOrderSEOLinks(row.Links),
		CuisineOptions: helpers.ScanNOrderCuisines(),
		Limits: Limits{
			TitleMaxLength:       MaxTitleLen,
			DescriptionMaxLength: MaxDescriptionLen,
			CuisinesMax:          helpers.ScanNOrderMaxCuisines,
			LinksMax:             helpers.ScanNOrderMaxSEOLinks,
		},
		SuggestAvailable: s.suggestAvailable(),
		UpdatedAt:        row.UpdatedAt,
		Google: GoogleProfile{
			PlaceID:   row.GooglePlaceID,
			MapsURL:   row.GoogleMapsURL,
			ReviewURL: row.GoogleReviewURL,
		},
		GoogleSearchAvailable: s.places != nil,
		ReviewRequestEnabled:  row.ReviewRequestEnabled,
		ReviewRequestsSent30d: row.ReviewRequestsSent,
	}
	if out.Links == nil {
		out.Links = []string{}
	}
	// Seuls les codes connus remontent au back-office : un texte libre
	// antérieur à la liste fermée reste publié tant qu'il n'est pas remplacé.
	for _, c := range helpers.ParseScanNOrderCuisines(row.Cuisines) {
		if c.Code != "" {
			out.CuisineTypes = append(out.CuisineTypes, c.Code)
		}
	}
	return out, nil
}

// UpdateSettings valide puis enregistre les champs fournis.
func (s *Service) UpdateSettings(ctx context.Context, merchantID string, req *UpdateRequest) (*Settings, error) {
	u, err := validateUpdate(req)
	if err != nil {
		return nil, err
	}
	if err := s.checkReviewRequest(ctx, merchantID, &u); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateSettings(ctx, merchantID, u, s.now()); err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, merchantID)
}

// checkReviewRequest : la demande d'avis par e-mail suppose un lien d'avis.
// L'activer sans lien est refusé ; effacer le lien d'une demande active la
// coupe.
func (s *Service) checkReviewRequest(ctx context.Context, merchantID string, u *update) error {
	if u.ReviewRequestEnabled == nil && u.GoogleReviewURL == nil {
		return nil
	}
	row, err := s.repo.GetSettings(ctx, merchantID)
	if err != nil {
		return err
	}
	reviewURL, enabled := row.GoogleReviewURL, row.ReviewRequestEnabled
	if u.GoogleReviewURL != nil {
		reviewURL = *u.GoogleReviewURL
	}
	if u.ReviewRequestEnabled != nil {
		enabled = *u.ReviewRequestEnabled
	}
	if !enabled || reviewURL != "" {
		return nil
	}
	if u.ReviewRequestEnabled != nil {
		return &FieldError{Field: "review_request_enabled", Reason: "missing_review_url"}
	}
	off := false
	u.ReviewRequestEnabled = &off
	return nil
}

// validateUpdate normalise la saisie : espaces repliés, cuisines connues sans
// doublon, liens normalisés sans doublon. Refuse plutôt que tronquer : le
// restaurateur doit voir ce qui sera publié.
func validateUpdate(req *UpdateRequest) (update, error) {
	var u update
	if req == nil {
		return u, nil
	}
	if req.Title != nil {
		t := oneLine(*req.Title)
		if utf8.RuneCountInString(t) > MaxTitleLen {
			return u, &FieldError{Field: "title", Reason: "too_long"}
		}
		u.Title = &t
	}
	if req.Description != nil {
		d := oneLine(*req.Description)
		if utf8.RuneCountInString(d) > MaxDescriptionLen {
			return u, &FieldError{Field: "description", Reason: "too_long"}
		}
		u.Description = &d
	}
	if req.CuisineTypes != nil {
		codes := []string{}
		seen := map[string]bool{}
		for _, raw := range *req.CuisineTypes {
			code := strings.TrimSpace(raw)
			if _, ok := helpers.ScanNOrderCuisineByCode(code); !ok {
				return u, &FieldError{Field: "cuisine_types", Reason: "unknown_cuisine", Value: raw}
			}
			if seen[code] {
				continue
			}
			seen[code] = true
			codes = append(codes, code)
		}
		if len(codes) > helpers.ScanNOrderMaxCuisines {
			return u, &FieldError{Field: "cuisine_types", Reason: "too_many"}
		}
		joined := strings.Join(codes, ",")
		u.Cuisines = &joined
	}
	if req.Links != nil {
		links := []string{}
		seen := map[string]bool{}
		for _, raw := range *req.Links {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			n, err := helpers.NormalizeScanNOrderSEOLink(raw)
			if err != nil {
				return u, &FieldError{Field: "links", Reason: "invalid_link", Value: raw}
			}
			key := strings.TrimSuffix(n, "/")
			if seen[key] {
				continue
			}
			seen[key] = true
			links = append(links, n)
		}
		if len(links) > helpers.ScanNOrderMaxSEOLinks {
			return u, &FieldError{Field: "links", Reason: "too_many"}
		}
		stored := ""
		if len(links) > 0 {
			raw, err := json.Marshal(links)
			if err != nil {
				return u, err
			}
			stored = string(raw)
		}
		u.Links = &stored
	}
	if req.GooglePlaceID != nil {
		id := strings.TrimSpace(*req.GooglePlaceID)
		if id != "" && !helpers.ValidGooglePlaceID(id) {
			return u, &FieldError{Field: "google_place_id", Reason: "invalid", Value: *req.GooglePlaceID}
		}
		u.GooglePlaceID = &id
	}
	googleLink := func(field string, raw *string) (*string, error) {
		if raw == nil {
			return nil, nil
		}
		v := strings.TrimSpace(*raw)
		if v == "" {
			return &v, nil
		}
		n, err := helpers.NormalizeGoogleLink(v)
		if err != nil {
			return nil, &FieldError{Field: field, Reason: "invalid_google_link", Value: *raw}
		}
		return &n, nil
	}
	var err error
	if u.GoogleMapsURL, err = googleLink("google_maps_url", req.GoogleMapsURL); err != nil {
		return u, err
	}
	if u.GoogleReviewURL, err = googleLink("google_review_url", req.GoogleReviewURL); err != nil {
		return u, err
	}
	u.ReviewRequestEnabled = req.ReviewRequestEnabled
	return u, nil
}

// SearchGoogle cherche la fiche Google de l'établissement d'après son nom et
// son adresse. Le restaurateur confirme la bonne fiche, puis enregistre.
func (s *Service) SearchGoogle(ctx context.Context, merchantID string) ([]GoogleCandidate, error) {
	if s.places == nil {
		return nil, ErrGoogleSearchUnavailable
	}
	q, err := s.repo.GetPlaceQuery(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	name := oneLine(q.Name)
	address := oneLine(q.Address)
	locality := oneLine(strings.TrimSpace(q.ZipCode + " " + q.City))
	text := name
	if address != "" {
		text += ", " + address
	}
	if locality != "" && !strings.Contains(fold(address), fold(q.City)) {
		text += ", " + locality
	}

	found, err := s.places.FindPlaces(ctx, text, q.Lat, q.Lng)
	if err == nil && len(found) == 0 && q.City != "" {
		// Adresse mal saisie ou différente de celle de Google : nom et ville.
		found, err = s.places.FindPlaces(ctx, name+" "+oneLine(q.City), q.Lat, q.Lng)
	}
	if err != nil {
		s.log.Warn("recherche de la fiche Google impossible", zap.String("merchant_id", merchantID), zap.Error(err))
		return nil, ErrGoogleSearchUnavailable
	}

	out := []GoogleCandidate{}
	for _, p := range found {
		if !helpers.ValidGooglePlaceID(p.PlaceID) {
			continue
		}
		out = append(out, GoogleCandidate{
			PlaceID:   p.PlaceID,
			Name:      p.Name,
			Address:   p.Address,
			MapsURL:   helpers.GoogleMapsPlaceURL(p.PlaceID),
			ReviewURL: helpers.GoogleReviewURL(p.PlaceID),
		})
		if len(out) == 5 {
			break
		}
	}
	return out, nil
}

// fold : minuscules, sans accents, pour comparer une ville et une adresse.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Service) provider() (ai.LLMProvider, error) {
	if s.registry == nil {
		return nil, ErrSuggestDisabled
	}
	p, err := s.registry.GetProviderForTask(seoProfileTask)
	if errors.Is(err, ai.ErrTaskDisabled) {
		return nil, ErrSuggestDisabled
	}
	return p, err
}

func (s *Service) suggestAvailable() bool {
	_, err := s.provider()
	return err == nil
}

// Suggest propose une description et des types de cuisine à partir de la
// carte visible sur ScanNOrder. Rien n'est enregistré.
func (s *Service) Suggest(ctx context.Context, merchantID string) (*Suggestion, error) {
	provider, err := s.provider()
	if err != nil {
		return nil, err
	}
	name, city, err := s.repo.GetMerchantIdentity(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	menu, err := s.menu.GetMenuFromMerchantIdWithMarketing(ctx, merchantID)
	if err != nil {
		return nil, err
	}
	categories, count := promptMenu(menu)
	if count < minProductsForSuggest {
		return nil, ErrMenuTooSmall
	}

	cfg, _ := s.registry.TaskConfig(seoProfileTask)
	resp, err := provider.Complete(ctx, ai.CompletionRequest{
		Task:         seoProfileTask,
		SystemPrompt: seoProfileSystemPrompt,
		UserPrompt:   seoProfileUserPrompt(name, city, categories),
		MaxTokens:    cfg.MaxTokens,
		JSONSchema:   seoProfileSchema,
	})
	if err != nil {
		ai.LogLLMCall(s.log, seoProfileTask, provider.Name(), "", 0, 0, 0, false, err)
		return nil, errors.Join(ErrSuggestFailed, err)
	}
	ai.LogLLMCall(s.log, seoProfileTask, provider.Name(), resp.Model, resp.InputTokens, resp.OutputTokens, resp.LatencyMs, false, nil)
	if resp.StopReason == ai.StopReasonMaxTokens {
		return nil, errors.Join(ErrSuggestFailed, errors.New("réponse tronquée (max_tokens)"))
	}

	var raw Suggestion
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		s.log.Warn("suggestion SEO : réponse illisible", zap.Error(err))
		return nil, errors.Join(ErrSuggestFailed, err)
	}
	out := sanitizeSuggestion(raw)
	if out.Description == "" && len(out.CuisineTypes) == 0 {
		return nil, ErrSuggestFailed
	}
	return &out, nil
}

// sanitizeSuggestion borne la réponse du modèle : description repliée et
// coupée, codes connus, sans doublon, au plus ScanNOrderMaxCuisines.
func sanitizeSuggestion(in Suggestion) Suggestion {
	out := Suggestion{
		Description:  truncateRunes(strings.Trim(oneLine(in.Description), `"«» `), MaxDescriptionLen),
		CuisineTypes: []string{},
	}
	seen := map[string]bool{}
	for _, code := range in.CuisineTypes {
		code = strings.TrimSpace(code)
		if _, ok := helpers.ScanNOrderCuisineByCode(code); !ok || seen[code] {
			continue
		}
		seen[code] = true
		out.CuisineTypes = append(out.CuisineTypes, code)
		if len(out.CuisineTypes) == helpers.ScanNOrderMaxCuisines {
			break
		}
	}
	return out
}

// promptMenu garde les produits visibles sur ScanNOrder (sous-produits des
// groupes compris), au plus promptMaxProducts, descriptions abrégées.
func promptMenu(menu *models.MenuResponse) ([]promptCategory, int) {
	if menu == nil {
		return nil, 0
	}
	var out []promptCategory
	count := 0
	for _, cat := range menu.ProductsTypes {
		pc := promptCategory{Name: cat.CategoryName}
		var add func(products []models.ProductEntry)
		add = func(products []models.ProductEntry) {
			for i := range products {
				p := &products[i]
				if count >= promptMaxProducts {
					return
				}
				// Même règle que la carte ScanNOrder (scannorder.ComputeGetMenu),
				// tous modes confondus : un groupe ou un produit masqué laisse
				// ses sous-produits être évalués un à un.
				visible := p.IsAvailableOnSNO != nil && *p.IsAvailableOnSNO && !p.IsHiddenByStatus()
				if (p.IsProductGroup != nil && *p.IsProductGroup) || !visible {
					add(p.SubProducts)
					continue
				}
				name := oneLine(p.Name)
				if name == "" {
					continue
				}
				desc := ""
				if p.Description != nil {
					desc = truncateRunes(oneLine(*p.Description), promptMaxDescriptionLen)
				}
				pc.Products = append(pc.Products, promptProduct{Name: name, Description: desc})
				count++
			}
		}
		add(cat.Products)
		if len(pc.Products) > 0 {
			out = append(out, pc)
		}
	}
	return out, count
}
