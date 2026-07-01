package upsell

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/ai"
	aicache "welloresto-api/internal/ai/cache"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu"

	"go.uber.org/zap"
)

const (
	upsellTask         = "upsell"
	cacheKeyResultFmt  = "upsell:result:%s:%s:%s" // merchantID, cartSignature, orderType
	cacheKeyPatternFmt = "upsell:patterns:%s:%s"  // merchantID, productID
	cacheResultTTL     = 30 * time.Minute
	llmTimeout         = 1500 * time.Millisecond
	maxAvailableForLLM = 50
	maxFreqPairsForLLM = 20
	minLift            = 1.5
)

// UpsellResult is returned by GenerateUpsell to the HTTP handler.
type UpsellResult struct {
	SuggestionID string          `json:"suggestion_id,omitempty"`
	Suggestions  []SuggestedItem `json:"suggestions"`
	Source       string          `json:"source"`
}

// patternEntry is the structure stored in Redis for market-basket patterns.
// Exported so tasks/upsell.go can write it with the same shape.
type PatternEntry struct {
	ProductID  string  `json:"product_id"`
	Name       string  `json:"name"`
	Lift       float64 `json:"lift"`
	Confidence float64 `json:"confidence"`
	Support    float64 `json:"support"`
}

// llmSuggestionItem is the per-item shape the LLM is asked to return.
type llmSuggestionItem struct {
	ProductID string  `json:"product_id"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
}

// llmResponse is the top-level JSON the LLM should return.
type llmResponse struct {
	Suggestions []llmSuggestionItem `json:"suggestions"`
}

// Service orchestrates upsell suggestion generation.
type Service struct {
	repo       *Repository
	menuRepo   *menu.MenuRepository
	aiRegistry *ai.Registry
	aiCache    *aicache.Cache
	logger     *zap.Logger
}

// NewService creates a Service with the required dependencies.
func NewService(
	repo *Repository,
	menuRepo *menu.MenuRepository,
	aiRegistry *ai.Registry,
	aiCache *aicache.Cache,
	logger *zap.Logger,
) *Service {
	return &Service{
		repo:       repo,
		menuRepo:   menuRepo,
		aiRegistry: aiRegistry,
		aiCache:    aiCache,
		logger:     logger,
	}
}

// GenerateUpsell produces upsell suggestions for the given cart.
// It never returns a 5xx-worthy error: all internal failures fall back gracefully.
// The only error it returns is ErrEmptyCart (→ 400 to the caller).
// channel identifies the calling platform (one of the Channel* constants) and
// is persisted on the suggestion row for per-platform analytics.
func (s *Service) GenerateUpsell(ctx context.Context, merchantID string, cartProducts []models.ProductEntry, orderType string, channel string) (*UpsellResult, error) {
	// Wrap the whole flow in a recover guard so the handler is always safe.
	result, err := s.generateUpsellSafe(ctx, merchantID, cartProducts, orderType, channel)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) generateUpsellSafe(ctx context.Context, merchantID string, cartProducts []models.ProductEntry, orderType string, channel string) (res *UpsellResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("upsell: panic recovered in GenerateUpsell",
				zap.String("merchant_id", merchantID),
				zap.Any("panic", r),
			)
			res = &UpsellResult{Source: SourceErrorFallback, Suggestions: []SuggestedItem{}}
			err = nil
		}
	}()

	// ── 4.1 Pre-checks ────────────────────────────────────────────────────────
	if len(cartProducts) == 0 {
		return nil, ErrEmptyCart
	}

	enabled, maxItems, settingsErr := s.repo.GetMerchantUpsellSettings(ctx, merchantID)
	if settingsErr != nil {
		s.logger.Error("upsell: failed to read merchant settings, using defaults",
			zap.String("merchant_id", merchantID),
			zap.Error(settingsErr),
		)
		enabled = false
	}
	if !enabled {
		return &UpsellResult{Source: SourceDisabled, Suggestions: []SuggestedItem{}}, nil
	}

	if !s.aiCache.IsAvailable() {
		s.logger.Warn("upsell: redis unavailable, skipping cache/patterns/llm, falling back to featured products",
			zap.String("merchant_id", merchantID),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, maxItems, channel)
	}

	// ── 4.2 Cart signature & cache check ─────────────────────────────────────
	cartSignature := cartSignatureFrom(cartProducts)
	cacheKey := fmt.Sprintf(cacheKeyResultFmt, merchantID, cartSignature, normalizeOrderType(orderType))

	if cached, hit, _ := s.aiCache.Get(ctx, cacheKey); hit {
		var cachedResult UpsellResult
		if jsonErr := json.Unmarshal([]byte(cached), &cachedResult); jsonErr == nil {
			// Always create a fresh DB row for per-event tracking.
			cachedSource := cachedSourceOf(cachedResult.Source)
			suggID, createErr := s.repo.CreateSuggestion(ctx, CreateSuggestionParams{
				MerchantID:     merchantID,
				CartSignature:  cartSignature,
				SuggestedItems: cachedResult.Suggestions,
				Source:         cachedSource,
				Channel:        channel,
			})
			if createErr != nil {
				s.logger.Error("upsell: failed to persist cached suggestion",
					zap.String("merchant_id", merchantID),
					zap.Error(createErr),
				)
			}
			return &UpsellResult{
				SuggestionID: suggID,
				Suggestions:  cachedResult.Suggestions,
				Source:       cachedSource,
			}, nil
		}
	}

	// ── 4.3 Load candidates ───────────────────────────────────────────────────
	available, err := s.menuRepo.ListAvailableProductsForUpsell(ctx, merchantID)
	if err != nil {
		s.logger.Error("upsell: failed to list available products, using featured fallback",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, maxItems, channel)
	}

	// Build cart set for fast exclusion.
	cartSet := make(map[string]struct{}, len(cartProducts))
	for _, p := range cartProducts {
		cartSet[p.ProductID] = struct{}{}
	}

	// Index candidates (not in cart) by product_id.
	candidateMap := make(map[string]menu.AvailableProduct, len(available))
	for _, ap := range available {
		if _, inCart := cartSet[ap.ProductID]; !inCart {
			candidateMap[ap.ProductID] = ap
		}
	}

	// ── 4.4 Pattern (Apriori from Redis) ─────────────────────────────────────
	// Aggregate scores across all cart products.
	aggregated := make(map[string]float64)
	for _, cp := range cartProducts {
		patKey := fmt.Sprintf(cacheKeyPatternFmt, merchantID, cp.ProductID)
		raw, hit, _ := s.aiCache.Get(ctx, patKey)
		if !hit || raw == "" {
			continue
		}
		var entries []PatternEntry
		if jsonErr := json.Unmarshal([]byte(raw), &entries); jsonErr != nil {
			continue
		}
		for _, e := range entries {
			if _, isCandidate := candidateMap[e.ProductID]; isCandidate {
				aggregated[e.ProductID] += e.Lift
			}
		}
	}

	// Find best lift to decide whether pattern is sufficient.
	bestLift := 0.0
	for _, score := range aggregated {
		if score > bestLift {
			bestLift = score
		}
	}

	if bestLift >= minLift && len(aggregated) >= maxItems {
		// Sort by aggregated lift DESC.
		type scored struct {
			pid   string
			score float64
		}
		ranked := make([]scored, 0, len(aggregated))
		for pid, sc := range aggregated {
			ranked = append(ranked, scored{pid, sc})
		}
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
		if len(ranked) > maxItems {
			ranked = ranked[:maxItems]
		}

		suggestions := make([]SuggestedItem, 0, len(ranked))
		for _, r := range ranked {
			ap := candidateMap[r.pid]
			suggestions = append(suggestions, SuggestedItem{
				ProductID: r.pid,
				Title:     fmt.Sprintf(titleTemplates[hashIndex(r.pid, len(titleTemplates))], ap.Name),
				Score:     normalizeScore(r.score),
				Name:      ap.Name,
				Price:     ap.Price,
				ImageURL:  ap.ImageURL,
			})
		}

		s.enrichWithProductConfig(ctx, merchantID, suggestions)

		return s.persistAndCache(ctx, merchantID, cartSignature, suggestions, SourcePattern, cacheKey, nil, nil, channel)
	}

	// ── 4.5 LLM fallback ─────────────────────────────────────────────────────
	provider, provErr := s.aiRegistry.GetProviderForTask(upsellTask)
	if provErr != nil {
		s.logger.Warn("upsell: LLM provider unavailable, going to featured fallback",
			zap.String("merchant_id", merchantID),
			zap.Error(provErr),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, maxItems, channel)
	}

	taskCfg, _ := s.aiRegistry.TaskConfig(upsellTask)

	// Limit available products for LLM context: prefer categories not in cart.
	llmAvailable := selectLLMCandidates(candidateMap, cartSet, maxAvailableForLLM)

	// Frequent pairs for LLM context (top by lift).
	frequentPairs := buildFrequentPairsForPrompt(aggregated, candidateMap, maxFreqPairsForLLM)

	userPrompt, promptErr := buildUserPrompt(cartProducts, available, llmAvailable, frequentPairs, orderType)
	if promptErr != nil {
		s.logger.Error("upsell: failed to build user prompt",
			zap.String("merchant_id", merchantID),
			zap.Error(promptErr),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, maxItems, channel)
	}

	llmCtx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	completionReq := ai.CompletionRequest{
		Task:         upsellTask,
		SystemPrompt: upsellSystemPrompt,
		UserPrompt:   userPrompt,
		Temperature:  taskCfg.Temperature,
		MaxTokens:    taskCfg.MaxTokens,
		JSONMode:     true,
	}

	resp, llmErr := provider.Complete(llmCtx, completionReq)

	var suggestions []SuggestedItem
	var parseErr error

	if llmErr == nil {
		suggestions, parseErr = parseLLMResponse(resp.Content, candidateMap, maxItems)
		if parseErr != nil {
			// One retry.
			resp2, retryErr := provider.Complete(llmCtx, completionReq)
			if retryErr == nil {
				suggestions, parseErr = parseLLMResponse(resp2.Content, candidateMap, maxItems)
				if parseErr == nil {
					resp = resp2
				}
			}
		}
	}

	if llmErr != nil || parseErr != nil || len(suggestions) == 0 {
		s.logger.Warn("upsell: LLM call failed or produced no valid suggestions, going to featured fallback",
			zap.String("merchant_id", merchantID),
			zap.Error(llmErr),
			zap.Error(parseErr),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, maxItems, channel)
	}

	// Enrich with product metadata.
	for i, sg := range suggestions {
		if ap, ok := candidateMap[sg.ProductID]; ok {
			suggestions[i].Name = ap.Name
			suggestions[i].Price = ap.Price
			suggestions[i].ImageURL = ap.ImageURL
		}
	}

	s.enrichWithProductConfig(ctx, merchantID, suggestions)

	llmProvider := provider.Name()

	return s.persistAndCache(ctx, merchantID, cartSignature, suggestions, SourceLLM, cacheKey, &ai.CompletionResponse{
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		LatencyMs:    resp.LatencyMs,
	}, &llmProvider, channel)
}

// featuredFallback is the last-resort path when both pattern and LLM fail.
// It always persists (even for empty suggestions) to allow analytics.
func (s *Service) featuredFallback(ctx context.Context, merchantID string, cartProducts []models.ProductEntry, maxItems int, channel string) (*UpsellResult, error) {
	cartSet := make(map[string]struct{}, len(cartProducts))
	for _, p := range cartProducts {
		cartSet[p.ProductID] = struct{}{}
	}

	cartSignature := cartSignatureFrom(cartProducts)

	featured, err := s.repo.ListFeaturedProducts(ctx, merchantID, maxItems+len(cartSet))
	if err != nil {
		s.logger.Error("upsell: ListFeaturedProducts failed in fallback",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		featured = []SuggestedItem{}
	}

	// Filter out cart products.
	filtered := make([]SuggestedItem, 0, len(featured))
	for _, sg := range featured {
		if _, inCart := cartSet[sg.ProductID]; !inCart {
			filtered = append(filtered, sg)
		}
		if len(filtered) >= maxItems {
			break
		}
	}

	s.enrichWithProductConfig(ctx, merchantID, filtered)

	suggID, createErr := s.repo.CreateSuggestion(ctx, CreateSuggestionParams{
		MerchantID:     merchantID,
		CartSignature:  cartSignature,
		SuggestedItems: filtered,
		Source:         SourceFeaturedFallback,
		Channel:        channel,
	})
	if createErr != nil {
		s.logger.Error("upsell: failed to persist featured_fallback suggestion",
			zap.String("merchant_id", merchantID),
			zap.Error(createErr),
		)
	}

	s.logger.Warn("upsell: featured_fallback used",
		zap.String("merchant_id", merchantID),
		zap.Int("suggestions_count", len(filtered)),
	)

	return &UpsellResult{
		SuggestionID: suggID,
		Suggestions:  filtered,
		Source:       SourceFeaturedFallback,
	}, nil
}

// persistAndCache writes the suggestion to DB and caches the result.
// llmResp and llmProvider are nil for non-LLM sources.
func (s *Service) persistAndCache(
	ctx context.Context,
	merchantID, cartSignature string,
	suggestions []SuggestedItem,
	source string,
	cacheKey string,
	llmResp *ai.CompletionResponse,
	llmProvider *string,
	channel string,
) (*UpsellResult, error) {
	params := CreateSuggestionParams{
		MerchantID:     merchantID,
		CartSignature:  cartSignature,
		SuggestedItems: suggestions,
		Source:         source,
		Channel:        channel,
	}
	if llmResp != nil {
		tokIn := llmResp.InputTokens
		tokOut := llmResp.OutputTokens
		latMs := int(llmResp.LatencyMs)
		params.LLMProvider = llmProvider
		params.LLMModel = &llmResp.Model
		params.TokensIn = &tokIn
		params.TokensOut = &tokOut
		params.LatencyMs = latMs
	}

	suggID, createErr := s.repo.CreateSuggestion(ctx, params)
	if createErr != nil {
		s.logger.Error("upsell: failed to persist suggestion",
			zap.String("merchant_id", merchantID),
			zap.Error(createErr),
		)
	}

	// Cache the result (ignore errors — cache failure must not block).
	resultForCache := UpsellResult{Suggestions: suggestions, Source: source}
	if rawCache, marshalErr := json.Marshal(resultForCache); marshalErr == nil {
		_ = s.aiCache.Set(ctx, cacheKey, string(rawCache), cacheResultTTL)
	}

	s.logger.Info("upsell suggestion generated",
		zap.String("merchant_id", merchantID),
		zap.String("source", source),
		zap.Int("suggestions_count", len(suggestions)),
	)

	return &UpsellResult{
		SuggestionID: suggID,
		Suggestions:  suggestions,
		Source:       source,
	}, nil
}

// enrichWithProductConfig loads the full product configuration (attributes, options,
// per-channel prices, allergens, tags) for each suggestion so every consumer
// (POS, Kiosk, SNO) can open the product configuration modal without a follow-up
// call. Best-effort and mutates in place: a failed lookup leaves Product nil for
// that item rather than failing the whole suggestion list.
func (s *Service) enrichWithProductConfig(ctx context.Context, merchantID string, suggestions []SuggestedItem) {
	for i := range suggestions {
		product, err := s.menuRepo.GetProduct(ctx, merchantID, suggestions[i].ProductID)
		if err != nil {
			s.logger.Warn("upsell: failed to load product configuration, leaving Product nil",
				zap.String("merchant_id", merchantID),
				zap.String("product_id", suggestions[i].ProductID),
				zap.Error(err),
			)
			continue
		}
		suggestions[i].Product = product
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// cartSignatureFrom computes a stable SHA-256 hex digest from the sorted product IDs
// in the cart. Duplicate product IDs are included as-is (quantities not encoded).
func cartSignatureFrom(products []models.ProductEntry) string {
	ids := make([]string, 0, len(products))
	seen := make(map[string]struct{})
	for _, p := range products {
		if _, ok := seen[p.ProductID]; !ok {
			seen[p.ProductID] = struct{}{}
			ids = append(ids, p.ProductID)
		}
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return fmt.Sprintf("%x", sum)
}

// normalizeOrderType returns a stable, uppercase cache-key segment for orderType.
// Empty/unknown values normalize to "ANY" so dine-in/takeaway/delivery results
// never collide with each other or with the pre-channel cache generation.
func normalizeOrderType(orderType string) string {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case models.OrderTypeIn:
		return models.OrderTypeIn
	case models.OrderTypeTakeAway:
		return models.OrderTypeTakeAway
	case models.OrderTypeDelivery:
		return models.OrderTypeDelivery
	default:
		return "ANY"
	}
}

// channelLabel returns a short human-readable description of the order channel
// for inclusion in the LLM prompt, or "" when orderType is empty/unrecognized —
// in which case the caller omits the field entirely.
func channelLabel(orderType string) string {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case models.OrderTypeIn:
		return "commande sur place"
	case models.OrderTypeTakeAway:
		return "commande à emporter"
	case models.OrderTypeDelivery:
		return "commande en livraison"
	default:
		return ""
	}
}

// cachedSourceOf returns the "cached_*" variant of a source constant.
func cachedSourceOf(source string) string {
	switch source {
	case SourceLLM, SourceCachedLLM:
		return SourceCachedLLM
	case SourcePattern, SourceCachedPattern:
		return SourceCachedPattern
	default:
		return source
	}
}

// hashIndex returns a deterministic index into a slice of length n based on the
// string s. Used to pick a title template for pattern-sourced suggestions.
func hashIndex(s string, n int) int {
	if n <= 0 {
		return 0
	}
	h := 0
	for _, c := range s {
		h = (h*31 + int(c)) & 0x7FFFFFFF
	}
	return h % n
}

// normalizeScore maps an aggregated lift score to the 0.0–1.0 range using a
// simple sigmoid-like clamp (lift 1.5 → ~0.6, lift 5.0 → ~1.0).
func normalizeScore(lift float64) float64 {
	if lift <= 0 {
		return 0
	}
	if lift > 5 {
		return 1.0
	}
	return lift / 5.0
}

// selectLLMCandidates returns up to limit products from candidateMap, preferring
// categories not represented in the cart to maximise complementarity.
func selectLLMCandidates(candidateMap map[string]menu.AvailableProduct, cartSet map[string]struct{}, limit int) []menu.AvailableProduct {
	// Collect cart category IDs.
	// Note: we don't have category info on cartProducts here, so we just take the
	// first `limit` candidates ordered deterministically by name.
	result := make([]menu.AvailableProduct, 0, limit)
	for _, ap := range candidateMap {
		result = append(result, ap)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// buildFrequentPairsForPrompt extracts the top N frequent pairs from the aggregated
// lift map for inclusion in the LLM user prompt.
func buildFrequentPairsForPrompt(aggregated map[string]float64, candidateMap map[string]menu.AvailableProduct, limit int) []map[string]interface{} {
	type pair struct {
		pid  string
		lift float64
	}
	pairs := make([]pair, 0, len(aggregated))
	for pid, lift := range aggregated {
		if ap, ok := candidateMap[pid]; ok {
			pairs = append(pairs, pair{pid: ap.Name, lift: lift})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].lift > pairs[j].lift })
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}

	result := make([]map[string]interface{}, 0, len(pairs))
	for _, p := range pairs {
		result = append(result, map[string]interface{}{
			"suggest": p.pid,
			"lift":    p.lift,
		})
	}
	return result
}

// buildUserPrompt constructs the JSON user prompt sent to the LLM.
func buildUserPrompt(
	cartProducts []models.ProductEntry,
	allAvailable []menu.AvailableProduct,
	llmCandidates []menu.AvailableProduct,
	frequentPairs []map[string]interface{},
	orderType string,
) (string, error) {
	// Build a category lookup from all available products.
	catByID := make(map[string]string, len(allAvailable))
	for _, ap := range allAvailable {
		catByID[ap.ProductID] = ap.CategoryName
	}

	// Deduplicate cart items.
	seen := make(map[string]struct{})
	cartItems := make([]map[string]string, 0, len(cartProducts))
	for _, cp := range cartProducts {
		if _, ok := seen[cp.ProductID]; ok {
			continue
		}
		seen[cp.ProductID] = struct{}{}
		cartItems = append(cartItems, map[string]string{
			"product_id": cp.ProductID,
			"name":       cp.Name,
			"category":   catByID[cp.ProductID],
		})
	}

	availItems := make([]map[string]interface{}, 0, len(llmCandidates))
	for _, ap := range llmCandidates {
		availItems = append(availItems, map[string]interface{}{
			"product_id": ap.ProductID,
			"name":       ap.Name,
			"category":   ap.CategoryName,
			"price":      ap.Price,
		})
	}

	payload := map[string]interface{}{
		"cart":               cartItems,
		"available_products": availItems,
		"frequent_pairs":     frequentPairs,
	}
	// Channel context is omitted entirely when unknown/empty, so the prompt payload
	// is byte-for-byte identical to the pre-channel behavior in that case.
	if label := channelLabel(orderType); label != "" {
		payload["order_channel"] = label
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("upsell: failed to marshal user prompt: %w", err)
	}
	return string(raw), nil
}

// parseLLMResponse parses and validates the LLM JSON response.
// - product_id must exist in candidateMap (anti-hallucination)
// - title must be non-empty and < 100 chars
// - results are limited to maxItems
func parseLLMResponse(content string, candidateMap map[string]menu.AvailableProduct, maxItems int) ([]SuggestedItem, error) {
	var resp llmResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("upsell: LLM response parse failed: %w", err)
	}

	result := make([]SuggestedItem, 0, maxItems)
	for _, item := range resp.Suggestions {
		pid := strings.TrimSpace(item.ProductID)
		title := strings.TrimSpace(item.Title)

		if pid == "" {
			continue
		}
		if _, valid := candidateMap[pid]; !valid {
			// Anti-hallucination: reject product IDs not in the candidate set.
			continue
		}
		if title == "" || len(title) >= 100 {
			continue
		}

		result = append(result, SuggestedItem{
			ProductID: pid,
			Title:     title,
			Score:     item.Score,
		})

		if len(result) >= maxItems {
			break
		}
	}

	if len(result) == 0 {
		return nil, errors.New("upsell: LLM returned no valid suggestions")
	}
	return result, nil
}
