package upsell

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/ai"
	aicache "welloresto-api/internal/ai/cache"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/menu"

	"go.uber.org/zap"
)

const (
	upsellTask          = "upsell"
	cacheKeyResultFmt   = "upsell:result:%s:%s:%s:%s" // merchantID, cartSignature, orderType, channel
	cacheKeyPatternFmt  = "upsell:patterns:%s:%s"     // merchantID, productID
	cacheKeyLowPriceFmt = "upsell:lowprice:%s"        // merchantID
	cacheResultTTL      = 30 * time.Minute
	llmTimeout          = 1500 * time.Millisecond
	maxAvailableForLLM  = 50
	maxFreqPairsForLLM  = 20
	minLift             = 1.5
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
	schedules  ScheduleAvailability
	logger     *zap.Logger
}

// NewService creates a Service with the required dependencies.
func NewService(
	repo *Repository,
	menuRepo *menu.MenuRepository,
	aiRegistry *ai.Registry,
	aiCache *aicache.Cache,
	schedules ScheduleAvailability,
	logger *zap.Logger,
) *Service {
	return &Service{
		repo:       repo,
		menuRepo:   menuRepo,
		aiRegistry: aiRegistry,
		aiCache:    aiCache,
		schedules:  schedules,
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
		cart := s.resolveCart(ctx, merchantID, cartProducts)
		unavailable := s.unavailableNow(ctx, merchantID, channel)
		return s.featuredFallback(ctx, merchantID, cartProducts, mergeSets(cart.excluded, unavailable), maxItems, channel)
	}

	// ── 4.2 Cart signature & cache check ─────────────────────────────────────
	// The channel is part of the key: each channel filters its own products
	// (availability flag, schedules), so two channels may get different lists.
	cartSignature := cartSignatureFrom(cartProducts)
	cacheKey := fmt.Sprintf(cacheKeyResultFmt, merchantID, cartSignature, normalizeOrderType(orderType), channel)

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
	// Variants in the cart are resolved to their product group: patterns are
	// stored per group, and a group must not be suggested when one of its
	// variants is already in the cart.
	cart := s.resolveCart(ctx, merchantID, cartProducts)
	// Products the channel cannot sell right now are filtered out before any
	// selection, so that the next steps fill their slot
	// (docs/UPSELL_COMPLETION.md, D9).
	unavailable := s.unavailableNow(ctx, merchantID, channel)

	available, err := s.menuRepo.ListAvailableProductsForUpsell(ctx, merchantID)
	if err != nil {
		s.logger.Error("upsell: failed to list available products, using featured fallback",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		return s.featuredFallback(ctx, merchantID, cartProducts, mergeSets(cart.excluded, unavailable), maxItems, channel)
	}

	// Index candidates (not in cart, sellable on the channel now) by product_id.
	candidateMap := make(map[string]menu.AvailableProduct, len(available))
	for _, ap := range available {
		if _, inCart := cart.excluded[ap.ProductID]; inCart {
			continue
		}
		if _, outOfSchedule := unavailable[ap.ProductID]; outOfSchedule {
			continue
		}
		if !availableOnChannel(ap, channel) {
			continue
		}
		candidateMap[ap.ProductID] = ap
	}

	// ── 4.4 Pattern (Apriori from Redis) ─────────────────────────────────────
	// Aggregate scores across all cart products.
	aggregated := make(map[string]float64)
	for _, key := range cart.patternKeys {
		patKey := fmt.Sprintf(cacheKeyPatternFmt, merchantID, key)
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

	// Find best lift to decide whether patterns are trustworthy.
	bestLift := 0.0
	for _, score := range aggregated {
		if score > bestLift {
			bestLift = score
		}
	}

	// Patterns are kept even when there are fewer than maxItems of them; the
	// remaining slots are completed by low-price best sellers, then by the LLM
	// (docs/UPSELL_COMPLETION.md, D2 and D10).
	var suggestions []SuggestedItem
	if bestLift >= minLift {
		suggestions = rankPatternSuggestions(aggregated, candidateMap, maxItems)
	}

	// ── 4.5 Low-price best sellers ───────────────────────────────────────────
	if remaining := maxItems - len(suggestions); remaining > 0 {
		lowPrice := s.lowPriceSuggestions(ctx, merchantID, withoutSuggested(candidateMap, suggestions), remaining)
		suggestions = appendUnique(suggestions, lowPrice, maxItems)
	}

	// ── 4.6 LLM completion ───────────────────────────────────────────────────
	var llmResp *ai.CompletionResponse
	var llmProvider *string
	if remaining := maxItems - len(suggestions); remaining > 0 {
		llmItems, resp, provider := s.llmSuggestions(ctx, merchantID, cartProducts, cart.groupOf, available,
			withoutSuggested(candidateMap, suggestions), aggregated, orderType, remaining)
		if len(llmItems) > 0 {
			suggestions = appendUnique(suggestions, llmItems, maxItems)
			llmResp, llmProvider = resp, provider
		}
	}

	// No further completion: featured (is_popular) products are mostly main
	// dishes, and the list stays short rather than offering expensive items
	// (docs/UPSELL_COMPLETION.md, arbitrage (c)).
	source := sourceOf(suggestions)
	if len(suggestions) == 0 {
		// Empty lists are not cached, so the next call tries every step again
		// (e.g. after a transient LLM failure or the nightly recomputation).
		cacheKey = ""
	}

	s.enrichWithProductConfig(ctx, merchantID, suggestions)

	return s.persistAndCache(ctx, merchantID, cartSignature, suggestions, source, cacheKey, llmResp, llmProvider, channel)
}

// llmSuggestions asks the LLM for at most limit products among candidates.
// Any failure (LLM disabled or unavailable, timeout, invalid answer) is logged
// and yields no item, so the list simply stays shorter.
func (s *Service) llmSuggestions(
	ctx context.Context,
	merchantID string,
	cartProducts []models.ProductEntry,
	groupOf map[string]string,
	available []menu.AvailableProduct,
	candidates map[string]menu.AvailableProduct,
	aggregated map[string]float64,
	orderType string,
	limit int,
) ([]SuggestedItem, *ai.CompletionResponse, *string) {
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	provider, provErr := s.aiRegistry.GetProviderForTask(upsellTask)
	if provErr != nil {
		if errors.Is(provErr, ai.ErrTaskDisabled) {
			s.logger.Warn("upsell: LLM fallback disabled via config (AI_TASK_UPSELL_ENABLED=false), skipping LLM completion",
				zap.String("merchant_id", merchantID),
			)
		} else {
			s.logger.Warn("upsell: LLM provider unavailable, skipping LLM completion",
				zap.String("merchant_id", merchantID),
				zap.Error(provErr),
			)
		}
		return nil, nil, nil
	}

	taskCfg, _ := s.aiRegistry.TaskConfig(upsellTask)

	// Limit available products for LLM context.
	llmAvailable := selectLLMCandidates(candidates, maxAvailableForLLM)

	// Frequent pairs for LLM context (top by lift).
	frequentPairs := buildFrequentPairsForPrompt(aggregated, candidates, maxFreqPairsForLLM)

	userPrompt, promptErr := buildUserPrompt(cartProducts, groupOf, available, llmAvailable, frequentPairs, orderType)
	if promptErr != nil {
		s.logger.Error("upsell: failed to build user prompt",
			zap.String("merchant_id", merchantID),
			zap.Error(promptErr),
		)
		return nil, nil, nil
	}

	llmCtx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	completionReq := ai.CompletionRequest{
		Task:         upsellTask,
		SystemPrompt: strings.ReplaceAll(upsellSystemPrompt, "{MAX_ITEMS}", strconv.Itoa(limit)),
		UserPrompt:   userPrompt,
		Temperature:  taskCfg.Temperature,
		MaxTokens:    taskCfg.MaxTokens,
		JSONMode:     true,
	}

	resp, llmErr := provider.Complete(llmCtx, completionReq)

	var suggestions []SuggestedItem
	var parseErr error

	if llmErr == nil {
		suggestions, parseErr = parseLLMResponse(resp.Content, candidates, limit)
		if parseErr != nil {
			// One retry.
			resp2, retryErr := provider.Complete(llmCtx, completionReq)
			if retryErr == nil {
				suggestions, parseErr = parseLLMResponse(resp2.Content, candidates, limit)
				if parseErr == nil {
					resp = resp2
				}
			}
		}
	}

	if llmErr != nil || parseErr != nil || len(suggestions) == 0 {
		s.logger.Warn("upsell: LLM call failed or produced no valid suggestions, skipping LLM completion",
			zap.String("merchant_id", merchantID),
			zap.Error(llmErr),
			zap.Error(parseErr),
		)
		return nil, nil, nil
	}

	// Enrich with product metadata.
	for i, sg := range suggestions {
		if ap, ok := candidates[sg.ProductID]; ok {
			suggestions[i].Name = ap.Name
			suggestions[i].Price = ap.Price
			suggestions[i].ImageURL = ap.ImageURL
		}
		suggestions[i].Origin = OriginLLM
	}

	providerName := provider.Name()
	return suggestions, &ai.CompletionResponse{
		Model:        resp.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		LatencyMs:    resp.LatencyMs,
	}, &providerName
}

// lowPriceSuggestions returns at most limit products of the merchant's
// low-price best sellers list (computed nightly, see
// tasks.RecomputeUpsellPatterns) that are still candidates. A missing or
// unreadable list yields no item.
func (s *Service) lowPriceSuggestions(ctx context.Context, merchantID string, candidates map[string]menu.AvailableProduct, limit int) []SuggestedItem {
	raw, hit, _ := s.aiCache.Get(ctx, fmt.Sprintf(cacheKeyLowPriceFmt, merchantID))
	if !hit || raw == "" {
		return nil
	}
	var entries []LowPriceEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		s.logger.Warn("upsell: unreadable low-price list, skipping it",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		return nil
	}
	return lowPriceFromEntries(entries, candidates, limit)
}

// featuredSuggestions returns at most limit featured (is_popular) products that
// are orderable on channel and not in excluded. A failure is logged and yields
// no item.
func (s *Service) featuredSuggestions(ctx context.Context, merchantID string, channel string, excluded map[string]struct{}, limit int) []SuggestedItem {
	featured, err := s.repo.ListFeaturedProducts(ctx, merchantID, channel, limit+len(excluded))
	if err != nil {
		s.logger.Error("upsell: ListFeaturedProducts failed",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		return []SuggestedItem{}
	}

	result := make([]SuggestedItem, 0, limit)
	for _, sg := range featured {
		if len(result) >= limit {
			break
		}
		if _, skip := excluded[sg.ProductID]; !skip {
			result = append(result, sg)
		}
	}
	return result
}

// featuredFallback is the last-resort path when patterns and the LLM cannot
// even be tried (Redis down, candidate listing failed).
// It always persists (even for empty suggestions) to allow analytics.
func (s *Service) featuredFallback(ctx context.Context, merchantID string, cartProducts []models.ProductEntry, excluded map[string]struct{}, maxItems int, channel string) (*UpsellResult, error) {
	suggestions := s.featuredSuggestions(ctx, merchantID, channel, excluded, maxItems)

	s.enrichWithProductConfig(ctx, merchantID, suggestions)

	s.logger.Warn("upsell: featured_fallback used",
		zap.String("merchant_id", merchantID),
		zap.Int("suggestions_count", len(suggestions)),
	)

	return s.persistAndCache(ctx, merchantID, cartSignatureFrom(cartProducts), suggestions, SourceFeaturedFallback, "", nil, nil, channel)
}

// unavailableNow returns the products hidden right now by a schedule
// availability, on the SNO and Kiosk channels only: the POS ignores schedules,
// as in its own catalogue. A failed lookup is logged and filters nothing.
func (s *Service) unavailableNow(ctx context.Context, merchantID string, channel string) map[string]struct{} {
	if s.schedules == nil || (channel != ChannelSNO && channel != ChannelKiosk) {
		return nil
	}
	unavailable, err := s.schedules.GetUnavailableProductsAt(ctx, merchantID, time.Now())
	if err != nil {
		s.logger.Warn("upsell: schedule availability lookup failed, not filtering on schedules",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		return nil
	}
	set := make(map[string]struct{}, len(unavailable))
	for pid := range unavailable {
		set[pid] = struct{}{}
	}
	return set
}

// availableOnChannel reports whether ap may be offered on channel: SNO and
// Kiosk each have an availability flag, the POS sells everything.
func availableOnChannel(ap menu.AvailableProduct, channel string) bool {
	switch channel {
	case ChannelSNO:
		return ap.IsAvailableOnSNO
	case ChannelKiosk:
		return ap.IsAvailableOnKiosk
	default:
		return true
	}
}

// cartView is the cart as seen by the suggestion steps, with variants resolved
// to their product group (docs/UPSELL_COMPLETION.md, D1).
type cartView struct {
	// excluded holds the cart product ids and the groups of the cart variants:
	// none of them may be suggested.
	excluded map[string]struct{}
	// patternKeys holds, without duplicates, the ids patterns are stored under:
	// the group for a variant, the product itself otherwise.
	patternKeys []string
	// groupOf maps each cart variant to its product group.
	groupOf map[string]string
}

// resolveCart builds the cartView of cartProducts. When the variant lookup
// fails, the raw cart ids are used, as before variants were resolved.
func (s *Service) resolveCart(ctx context.Context, merchantID string, cartProducts []models.ProductEntry) cartView {
	ids := make([]string, 0, len(cartProducts))
	seen := make(map[string]struct{}, len(cartProducts))
	for _, p := range cartProducts {
		if _, ok := seen[p.ProductID]; ok || p.ProductID == "" {
			continue
		}
		seen[p.ProductID] = struct{}{}
		ids = append(ids, p.ProductID)
	}

	groupOf, err := s.repo.GetProductGroups(ctx, merchantID, ids)
	if err != nil {
		s.logger.Warn("upsell: failed to resolve cart variants to their groups, using raw cart ids",
			zap.String("merchant_id", merchantID),
			zap.Error(err),
		)
		groupOf = nil
	}
	return buildCartView(cartProducts, groupOf)
}

func buildCartView(cartProducts []models.ProductEntry, groupOf map[string]string) cartView {
	view := cartView{
		excluded: make(map[string]struct{}, len(cartProducts)),
		groupOf:  groupOf,
	}
	seenKeys := make(map[string]struct{}, len(cartProducts))
	for _, p := range cartProducts {
		view.excluded[p.ProductID] = struct{}{}
		key := p.ProductID
		if group, ok := groupOf[p.ProductID]; ok {
			view.excluded[group] = struct{}{}
			key = group
		}
		if _, seen := seenKeys[key]; !seen {
			seenKeys[key] = struct{}{}
			view.patternKeys = append(view.patternKeys, key)
		}
	}
	return view
}

// persistAndCache writes the suggestion to DB and caches the result.
// llmResp and llmProvider are nil when the LLM did not contribute.
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
	// An empty cacheKey means the result must not be cached.
	if cacheKey != "" {
		resultForCache := UpsellResult{Suggestions: suggestions, Source: source}
		if rawCache, marshalErr := json.Marshal(resultForCache); marshalErr == nil {
			_ = s.aiCache.Set(ctx, cacheKey, string(rawCache), cacheResultTTL)
		}
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
	case SourceLowPrice, SourceCachedLowPrice:
		return SourceCachedLowPrice
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

// rankPatternSuggestions turns aggregated pattern scores into at most maxItems
// suggestions, best score first. Ties are broken by product id so the list
// (and therefore the cached result) is deterministic.
func rankPatternSuggestions(aggregated map[string]float64, candidateMap map[string]menu.AvailableProduct, maxItems int) []SuggestedItem {
	type scored struct {
		pid   string
		score float64
	}
	ranked := make([]scored, 0, len(aggregated))
	for pid, sc := range aggregated {
		if _, ok := candidateMap[pid]; ok {
			ranked = append(ranked, scored{pid, sc})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].pid < ranked[j].pid
	})
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
			Origin:    OriginPattern,
		})
	}
	return suggestions
}

// appendUnique appends the items of extra whose product is not already in
// list, stopping once list holds maxItems items.
func appendUnique(list, extra []SuggestedItem, maxItems int) []SuggestedItem {
	seen := make(map[string]struct{}, len(list))
	for _, item := range list {
		seen[item.ProductID] = struct{}{}
	}
	for _, item := range extra {
		if len(list) >= maxItems {
			break
		}
		if _, dup := seen[item.ProductID]; dup {
			continue
		}
		seen[item.ProductID] = struct{}{}
		list = append(list, item)
	}
	return list
}

// withoutSuggested returns a copy of candidateMap without the products already
// in suggestions.
func withoutSuggested(candidateMap map[string]menu.AvailableProduct, suggestions []SuggestedItem) map[string]menu.AvailableProduct {
	result := make(map[string]menu.AvailableProduct, len(candidateMap))
	for pid, ap := range candidateMap {
		result[pid] = ap
	}
	for _, sg := range suggestions {
		delete(result, sg.ProductID)
	}
	return result
}

// mergeSets returns a new set holding the keys of a and b.
func mergeSets(a, b map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		result[k] = struct{}{}
	}
	for k := range b {
		result[k] = struct{}{}
	}
	return result
}

// lowPriceFromEntries turns the nightly low-price list into at most limit
// suggestions, keeping its order (best sellers first) and only the products
// that are still candidates (available, sellable on the channel, not in the
// cart, not already suggested).
func lowPriceFromEntries(entries []LowPriceEntry, candidates map[string]menu.AvailableProduct, limit int) []SuggestedItem {
	result := make([]SuggestedItem, 0, limit)
	for _, e := range entries {
		if len(result) >= limit {
			break
		}
		ap, ok := candidates[e.ProductID]
		if !ok {
			continue
		}
		result = append(result, SuggestedItem{
			ProductID: e.ProductID,
			Title:     fmt.Sprintf(titleTemplates[hashIndex(e.ProductID, len(titleTemplates))], ap.Name),
			Score:     0.5,
			Name:      ap.Name,
			Price:     ap.Price,
			ImageURL:  ap.ImageURL,
			Origin:    OriginLowPrice,
		})
	}
	return result
}

// sourceOf names the first step that contributed to the list: items keep the
// order of the steps (patterns, low-price best sellers, LLM).
func sourceOf(suggestions []SuggestedItem) string {
	if len(suggestions) == 0 {
		return SourceNone
	}
	switch suggestions[0].Origin {
	case OriginPattern:
		return SourcePattern
	case OriginLowPrice:
		return SourceLowPrice
	case OriginLLM:
		return SourceLLM
	default:
		return SourceFeaturedFallback
	}
}

// selectLLMCandidates returns up to limit products from candidateMap, ordered
// deterministically by name.
func selectLLMCandidates(candidateMap map[string]menu.AvailableProduct, limit int) []menu.AvailableProduct {
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
// groupOf maps cart variants to their product group, which carries the
// category (variants are not in allAvailable).
func buildUserPrompt(
	cartProducts []models.ProductEntry,
	groupOf map[string]string,
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
		category, ok := catByID[cp.ProductID]
		if !ok {
			category = catByID[groupOf[cp.ProductID]]
		}
		cartItems = append(cartItems, map[string]string{
			"product_id": cp.ProductID,
			"name":       cp.Name,
			"category":   category,
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
