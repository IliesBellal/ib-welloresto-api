package translation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/ai"
	aicache "welloresto-api/internal/ai/cache"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

const (
	translationTask      = "menu_translation"
	maxMerchantLanguages = 4

	l1TTL = 30 * 24 * time.Hour
	l2TTL = 90 * 24 * time.Hour
)

// Menu is the translation service input/output payload.
type Menu = models.MenuResponse

// Service translates menu textual fields and caches results.
type Service struct {
	repo     *Repository
	registry *ai.Registry
	cache    *aicache.Cache
}

func NewService(repo *Repository, registry *ai.Registry, cache *aicache.Cache) *Service {
	return &Service{
		repo:     repo,
		registry: registry,
		cache:    cache,
	}
}

// ListMerchantLanguages returns languages configured for a merchant.
func (s *Service) ListMerchantLanguages(ctx context.Context, merchantID string) ([]MerchantLanguage, error) {
	return s.repo.ListMerchantLanguages(ctx, merchantID)
}

// SetMerchantLanguage toggles language activation for a merchant.
// Activation is represented by row presence; deactivation by row deletion.
func (s *Service) SetMerchantLanguage(ctx context.Context, merchantID string, langCode string, enabled bool) error {
	normLang := strings.ToLower(strings.TrimSpace(langCode))
	if enabled {
		return s.repo.ActivateLanguageForMerchantWithLimit(ctx, merchantID, normLang, maxMerchantLanguages)
	}

	if err := s.repo.DeactivateLanguageForMerchant(ctx, merchantID, normLang); err != nil {
		return err
	}

	return s.InvalidateMerchantLanguageCache(ctx, merchantID, normLang)
}

// TranslationRequest is sent to the LLM as JSON user prompt.
type TranslationRequest struct {
	Items []TranslationRequestItem `json:"items"`
}

type TranslationRequestItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// TranslationResponse is the expected JSON format returned by the LLM.
type TranslationResponse struct {
	Translations []TranslationResponseItem `json:"translations"`
}

type TranslationResponseItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type itemCacheValue struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type categoryCachePlan struct {
	CatID string
	L2Key string
}

type itemCachePlan struct {
	ItemID          string
	L2Key           string
	HasDescription  bool
	NameTargetID    string
	DescTargetID    string
	DescriptionText string
}

// TranslateMenu translates menu category/item textual fields with a two-level cache.
// If any step fails after an L1 miss, it returns the original menu and an error.
func (s *Service) TranslateMenu(ctx context.Context, merchantID, langCode string, menu Menu) (Menu, error) {
	log := logger.FromContext(ctx)

	if !s.cache.IsAvailable() {
		log.Warn("translation: Redis cache unavailable — skipping translation, returning original menu",
			zap.String("merchant_id", merchantID),
			zap.String("lang_code", langCode),
		)
		return menu, nil
	}

	menuHash := buildMenuHash(menu)
	normLang := strings.ToLower(strings.TrimSpace(langCode))
	l1Key := fmt.Sprintf("translation:menu:%s:%s:%s", merchantID, normLang, menuHash)

	if s.cache.IsAvailable() {
		cachedMenuRaw, found, err := s.cache.Get(ctx, l1Key)
		if err != nil {
			return menu, fmt.Errorf("translation: L1 cache read failed: %w", err)
		}
		if found {
			var translated Menu
			if err := json.Unmarshal([]byte(cachedMenuRaw), &translated); err != nil {
				return menu, fmt.Errorf("translation: L1 cache payload invalid: %w", err)
			}
			log.Info("translation cache hit",
				zap.Bool("cache_hit", true),
				zap.String("layer", "L1"),
				zap.String("merchant_id", merchantID),
				zap.String("lang_code", langCode),
			)
			return translated, nil
		}
	}

	log.Info("translation cache miss",
		zap.Bool("cache_hit", false),
		zap.String("layer", "L1"),
		zap.String("merchant_id", merchantID),
		zap.String("lang_code", langCode),
	)

	translated := menu
	translations := make(map[string]string)
	toTranslate := make([]TranslationRequestItem, 0)
	expectedIDs := make(map[string]struct{})

	catPlans := make([]categoryCachePlan, 0)
	itemPlans := make([]itemCachePlan, 0)

	for catIdx := range translated.ProductsTypes {
		cat := translated.ProductsTypes[catIdx]
		catID := safeCategoryID(cat, catIdx)
		catText := categoryName(cat)
		catTargetID := fmt.Sprintf("cat_%s_name", catID)
		catHash := sha256Hex(catText + "|" + catID)
		catL2Key := fmt.Sprintf("translation:cat:%s:%s:%s", merchantID, normLang, catHash)

		catHit := false
		if s.cache.IsAvailable() {
			cachedCat, found, err := s.cache.Get(ctx, catL2Key)
			if err != nil {
				return menu, fmt.Errorf("translation: L2 category cache read failed: %w", err)
			}
			if found {
				v := strings.TrimSpace(cachedCat)
				if v == "" {
					return menu, fmt.Errorf("translation: L2 category cache payload empty for %s", catTargetID)
				}
				translations[catTargetID] = v
				catHit = true
				log.Info("translation cache hit",
					zap.Bool("cache_hit", true),
					zap.String("layer", "L2"),
					zap.String("element_type", "category"),
					zap.String("element_id", catTargetID),
				)
			}
		}

		if !catHit {
			toTranslate = append(toTranslate, TranslationRequestItem{ID: catTargetID, Text: catText})
			expectedIDs[catTargetID] = struct{}{}
			catPlans = append(catPlans, categoryCachePlan{CatID: catID, L2Key: catL2Key})
		}

		for prodIdx := range translated.ProductsTypes[catIdx].Products {
			item := translated.ProductsTypes[catIdx].Products[prodIdx]
			itemID := safeItemID(item, catIdx, prodIdx)

			itemNameID := fmt.Sprintf("item_%s_name", itemID)
			itemDescID := fmt.Sprintf("item_%s_desc", itemID)

			descText := derefString(item.Description)
			hasDesc := strings.TrimSpace(descText) != ""

			itemHash := sha256Hex(item.Name + "|" + descText + "|" + itemID)
			itemL2Key := fmt.Sprintf("translation:item:%s:%s:%s", merchantID, normLang, itemHash)

			itemHit := false
			if s.cache.IsAvailable() {
				cachedItemRaw, found, err := s.cache.Get(ctx, itemL2Key)
				if err != nil {
					return menu, fmt.Errorf("translation: L2 item cache read failed: %w", err)
				}
				if found {
					var cached itemCacheValue
					if err := json.Unmarshal([]byte(cachedItemRaw), &cached); err != nil {
						return menu, fmt.Errorf("translation: L2 item cache payload invalid: %w", err)
					}
					if strings.TrimSpace(cached.Name) == "" {
						return menu, fmt.Errorf("translation: L2 item cache missing name for %s", itemID)
					}
					if hasDesc && (cached.Description == nil || strings.TrimSpace(*cached.Description) == "") {
						return menu, fmt.Errorf("translation: L2 item cache missing description for %s", itemID)
					}

					translations[itemNameID] = cached.Name
					if hasDesc && cached.Description != nil {
						translations[itemDescID] = *cached.Description
					}

					itemHit = true
					log.Info("translation cache hit",
						zap.Bool("cache_hit", true),
						zap.String("layer", "L2"),
						zap.String("element_type", "item"),
						zap.String("element_id", itemID),
					)
				}
			}

			if !itemHit {
				toTranslate = append(toTranslate, TranslationRequestItem{ID: itemNameID, Text: item.Name})
				expectedIDs[itemNameID] = struct{}{}

				if hasDesc {
					toTranslate = append(toTranslate, TranslationRequestItem{ID: itemDescID, Text: descText})
					expectedIDs[itemDescID] = struct{}{}
				}

				itemPlans = append(itemPlans, itemCachePlan{
					ItemID:          itemID,
					L2Key:           itemL2Key,
					HasDescription:  hasDesc,
					NameTargetID:    itemNameID,
					DescTargetID:    itemDescID,
					DescriptionText: descText,
				})
			}
		}
	}

	if len(toTranslate) > 0 {
		log.Info("translation LLM call required",
			zap.Int("pending_count", len(toTranslate)),
			zap.String("merchant_id", merchantID),
			zap.String("lang_code", langCode),
		)

		llmMap, err := s.callLLM(ctx, toTranslate, langCode)
		if err != nil {
			return menu, err
		}

		for id := range expectedIDs {
			text, ok := llmMap[id]
			if !ok {
				return menu, fmt.Errorf("translation: missing translated id %q", id)
			}
			translations[id] = text
		}

		if s.cache.IsAvailable() {
			for _, cp := range catPlans {
				v := translations[fmt.Sprintf("cat_%s_name", cp.CatID)]
				if err := s.cache.Set(ctx, cp.L2Key, v, l2TTL); err != nil {
					return menu, fmt.Errorf("translation: L2 category cache write failed: %w", err)
				}
			}

			for _, ip := range itemPlans {
				name := translations[ip.NameTargetID]
				payload := itemCacheValue{Name: name}
				if ip.HasDescription {
					d := translations[ip.DescTargetID]
					payload.Description = &d
				}

				raw, err := json.Marshal(payload)
				if err != nil {
					return menu, fmt.Errorf("translation: failed to marshal L2 item payload: %w", err)
				}
				if err := s.cache.Set(ctx, ip.L2Key, string(raw), l2TTL); err != nil {
					return menu, fmt.Errorf("translation: L2 item cache write failed: %w", err)
				}
			}
		}
	}

	for catIdx := range translated.ProductsTypes {
		catID := safeCategoryID(translated.ProductsTypes[catIdx], catIdx)
		if t, ok := translations[fmt.Sprintf("cat_%s_name", catID)]; ok {
			applyCategoryName(&translated.ProductsTypes[catIdx], t)
		}

		for prodIdx := range translated.ProductsTypes[catIdx].Products {
			itemID := safeItemID(translated.ProductsTypes[catIdx].Products[prodIdx], catIdx, prodIdx)

			if t, ok := translations[fmt.Sprintf("item_%s_name", itemID)]; ok {
				translated.ProductsTypes[catIdx].Products[prodIdx].Name = t
			}

			descID := fmt.Sprintf("item_%s_desc", itemID)
			if t, ok := translations[descID]; ok {
				v := t
				translated.ProductsTypes[catIdx].Products[prodIdx].Description = &v
			}
		}
	}

	if s.cache.IsAvailable() {
		raw, err := json.Marshal(translated)
		if err != nil {
			return menu, fmt.Errorf("translation: failed to marshal L1 payload: %w", err)
		}
		if err := s.cache.Set(ctx, l1Key, string(raw), l1TTL); err != nil {
			return menu, fmt.Errorf("translation: L1 cache write failed: %w", err)
		}
	}

	return translated, nil
}

func (s *Service) callLLM(ctx context.Context, items []TranslationRequestItem, langCode string) (map[string]string, error) {
	log := logger.FromContext(ctx)

	provider, err := s.registry.GetProviderForTask(translationTask)
	if err != nil {
		return nil, fmt.Errorf("translation: provider resolution failed: %w", err)
	}

	langName, err := s.resolveLanguageName(ctx, langCode)
	if err != nil {
		return nil, fmt.Errorf("translation: resolve language name failed: %w", err)
	}

	userPayload, err := json.Marshal(TranslationRequest{Items: items})
	if err != nil {
		return nil, fmt.Errorf("translation: user prompt marshal failed: %w", err)
	}

	systemPrompt := buildSystemPrompt(langName)
	userPrompt := string(userPayload)
	maxTokens := calculateMaxTokens(systemPrompt, userPrompt)

	req := ai.CompletionRequest{
		Task:         translationTask,
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Temperature:  0.3,
		MaxTokens:    maxTokens,
		JSONMode:     true,
	}

	expected := make(map[string]struct{}, len(items))
	for _, it := range items {
		expected[it.ID] = struct{}{}
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		ai.LogLLMCall(log, translationTask, provider.Name(), "", 0, 0, 0, false, err)
		return nil, fmt.Errorf("translation: provider %s call failed: %w", provider.Name(), err)
	}
	ai.LogLLMCall(log, translationTask, provider.Name(), resp.Model, resp.InputTokens, resp.OutputTokens, resp.LatencyMs, false, nil)

	parsed, parseErr := parseTranslationResponse(resp.Content)
	if parseErr != nil {
		log.Warn("translation response parsing failed, retrying once",
			zap.String("provider", provider.Name()),
			zap.Error(parseErr),
		)

		retryResp, retryErr := provider.Complete(ctx, req)
		if retryErr != nil {
			ai.LogLLMCall(log, translationTask, provider.Name(), "", 0, 0, 0, false, retryErr)
			return nil, fmt.Errorf("translation: provider %s retry failed: %w", provider.Name(), retryErr)
		}
		ai.LogLLMCall(log, translationTask, provider.Name(), retryResp.Model, retryResp.InputTokens, retryResp.OutputTokens, retryResp.LatencyMs, false, nil)

		parsed, parseErr = parseTranslationResponse(retryResp.Content)
		if parseErr != nil {
			return nil, fmt.Errorf("translation: invalid JSON response after retry: %w", parseErr)
		}
	}

	validated, err := validateTranslationResponse(parsed, expected)
	if err != nil {
		return nil, fmt.Errorf("translation: invalid response payload: %w", err)
	}

	return validated, nil
}

func buildSystemPrompt(langName string) string {
	return fmt.Sprintf(`Tu es un traducteur professionnel spécialisé en gastronomie française.
Tu traduis du français vers %s.

Règles strictes :
- Ne traduis JAMAIS ces termes gastronomiques français, garde-les identiques : %s.
- Pour les noms de produits, sois concis et fidèle.
- Pour les descriptions, adapte culturellement (par exemple 'saignant' → terminologie locale standard).
- Conserve le ton et le niveau de langue de l'original (tutoiement vs vouvoiement). Si l'original est neutre/ambigu, utilise le vouvoiement par défaut.
- Préserve la précision sur les allergènes et ingrédients.
- Ne rajoute AUCUN commentaire, métadonnée ou explication.

Tu reçois une liste d'éléments à traduire avec un id stable.
Tu retournes UNIQUEMENT un JSON valide, sans markdown, de la forme :
{
  "translations": [
    {"id": "<id reçu>", "text": "<traduction>"},
    ...
  ]
}

Tous les ids reçus DOIVENT être présents dans la réponse.
Aucun id supplémentaire ne doit être ajouté.`, langName, formatProtectedTerms())
}

func (s *Service) resolveLanguageName(ctx context.Context, langCode string) (string, error) {
	langs, err := s.repo.ListAvailableLanguages(ctx)
	if err != nil {
		return "", err
	}

	norm := strings.ToLower(strings.TrimSpace(langCode))
	for _, l := range langs {
		if strings.ToLower(strings.TrimSpace(l.Code)) == norm {
			return l.Name, nil
		}
	}

	return strings.ToUpper(langCode), nil
}

func parseTranslationResponse(raw string) (*TranslationResponse, error) {
	var payload TranslationResponse
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func validateTranslationResponse(payload *TranslationResponse, expected map[string]struct{}) (map[string]string, error) {
	out := make(map[string]string, len(payload.Translations))

	for _, tr := range payload.Translations {
		id := strings.TrimSpace(tr.ID)
		text := strings.TrimSpace(tr.Text)

		if id == "" {
			return nil, fmt.Errorf("translation contains empty id")
		}
		if text == "" {
			return nil, fmt.Errorf("translation for id %q is empty", id)
		}
		if _, ok := expected[id]; !ok {
			return nil, fmt.Errorf("unexpected id %q in response", id)
		}
		out[id] = text
	}

	for id := range expected {
		if _, ok := out[id]; !ok {
			return nil, fmt.Errorf("missing id %q in response", id)
		}
	}

	return out, nil
}

func calculateMaxTokens(systemPrompt, userPrompt string) int {
	estInput := estimateTokens(systemPrompt + userPrompt)
	maxTokens := int(math.Ceil(float64(estInput) * 1.5))
	if maxTokens > 4000 {
		return 4000
	}
	if maxTokens < 64 {
		return 64
	}
	return maxTokens
}

func estimateTokens(text string) int {
	if text == "" {
		return 1
	}
	return int(math.Ceil(float64(len(text)) / 4.0))
}

func buildMenuHash(menu Menu) string {
	parts := make([]string, 0)

	for catIdx, cat := range menu.ProductsTypes {
		catID := safeCategoryID(cat, catIdx)
		parts = append(parts, "cat|"+catID+"|"+categoryName(cat))

		for prodIdx, p := range cat.Products {
			itemID := safeItemID(p, catIdx, prodIdx)
			parts = append(parts,
				"item|"+itemID+"|name|"+p.Name,
				"item|"+itemID+"|desc|"+derefString(p.Description),
			)
		}
	}

	sort.Strings(parts)
	return sha256Hex(strings.Join(parts, "||"))
}

func safeCategoryID(cat models.ProductCategory, catIdx int) string {
	if cat.CategoryID != nil && strings.TrimSpace(*cat.CategoryID) != "" {
		return strings.TrimSpace(*cat.CategoryID)
	}
	if strings.TrimSpace(cat.Category) != "" {
		return "name_" + strings.TrimSpace(cat.Category)
	}
	if strings.TrimSpace(cat.CategoryName) != "" {
		return "name_" + strings.TrimSpace(cat.CategoryName)
	}
	return fmt.Sprintf("idx_%d", catIdx)
}

func safeItemID(item models.ProductEntry, catIdx, prodIdx int) string {
	if strings.TrimSpace(item.ProductID) != "" {
		return strings.TrimSpace(item.ProductID)
	}
	return fmt.Sprintf("idx_%d_%d", catIdx, prodIdx)
}

func categoryName(cat models.ProductCategory) string {
	if strings.TrimSpace(cat.CategoryName) != "" {
		return cat.CategoryName
	}
	return cat.Category
}

func applyCategoryName(cat *models.ProductCategory, translated string) {
	if strings.TrimSpace(cat.CategoryName) != "" {
		cat.CategoryName = translated
		return
	}
	cat.Category = translated
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// InvalidateMerchantLanguageCache removes all cached translations for a given merchant + language.
// Should be called by admin handlers whenever a language is disabled or removed for a merchant.
// Uses SCAN + DEL in batches — never blocks Redis with KEYS.
func (s *Service) InvalidateMerchantLanguageCache(ctx context.Context, merchantID, langCode string) error {
	if s.cache == nil {
		return nil
	}

	log := logger.FromContext(ctx)
	normLang := strings.ToLower(strings.TrimSpace(langCode))

	log.Info("translation cache invalidation started",
		zap.String("merchant_id", merchantID),
		zap.String("lang_code", normLang),
	)

	patterns := []string{
		fmt.Sprintf("translation:menu:%s:%s:*", merchantID, normLang),
		fmt.Sprintf("translation:item:%s:%s:*", merchantID, normLang),
		fmt.Sprintf("translation:cat:%s:%s:*", merchantID, normLang),
	}

	totalDeleted := 0
	for _, pattern := range patterns {
		n, err := s.cache.DeleteByPattern(ctx, pattern)
		if err != nil {
			log.Error("translation cache invalidation failed",
				zap.String("merchant_id", merchantID),
				zap.String("lang_code", normLang),
				zap.String("pattern", pattern),
				zap.Error(err),
			)
			return fmt.Errorf("translation: cache invalidation failed for pattern %q: %w", pattern, err)
		}
		log.Info("translation cache invalidation pattern done",
			zap.String("pattern", pattern),
			zap.Int("keys_deleted", n),
		)
		totalDeleted += n
	}

	log.Info("translation cache invalidation completed",
		zap.String("merchant_id", merchantID),
		zap.String("lang_code", normLang),
		zap.Int("total_keys_deleted", totalDeleted),
	)

	return nil
}
