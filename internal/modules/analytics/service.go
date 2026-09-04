package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/timeutil"
)

// ErrInvalidRequest covers every request-shape validation failure (bad or
// missing dates, date_from after date_to, an unrecognized group_by) — all
// map to HTTP 400, see handler.go's writeError.
var ErrInvalidRequest = errAnalytics("invalid analytics request")

type Service struct {
	repo  *Repository
	redis *redisclient.Client
}

func NewService(repo *Repository, redis *redisclient.Client) *Service {
	return &Service{repo: repo, redis: redis}
}

// GetRevenue is the CA tab's single entry point: resolves and validates the
// merchant scope, loads the establishment's timezone, computes the three
// periods (current, previous, N-1) server-side, and returns the full
// contract in one response — see PROMPT 03 Partie 2 ("pas trois appels").
func (s *Service) GetRevenue(ctx context.Context, req RevenueRequest) (*RevenueResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}

	groupBy := req.GroupBy
	if groupBy == "" {
		groupBy = GroupByNone
	}
	if groupBy != GroupByNone && groupBy != GroupByMerchant {
		return nil, ErrInvalidRequest
	}

	includeHT := true
	if req.IncludeHT != nil {
		includeHT = *req.IncludeHT
	}

	dateFrom, err := time.Parse("2006-01-02", req.DateFrom)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	dateTo, err := time.Parse("2006-01-02", req.DateTo)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if dateTo.Before(dateFrom) {
		return nil, ErrInvalidRequest
	}

	// Cache write happens once the response is built, at the bottom of this method.
	if s.redis != nil {
		cacheKey := buildCacheKey("revenue", merchantIDs, req.DateFrom, req.DateTo, groupBy, includeHT)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp RevenueResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	// Timezone: any merchant in a single-establishment scope works — every
	// establishment in this system has its own row, but the accessible scope
	// is always exactly one today, so there is no cross-establishment
	// ambiguity to resolve here yet.
	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	currentStartUTC, currentEndUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)

	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1
	prevTo := dateFrom.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -int(periodDays)+1)
	prevStartUTC, prevEndUTC := timeutil.LocalDayRangeBounds(prevFrom, prevTo, tz)

	lyFrom := dateFrom.AddDate(-1, 0, 0)
	lyTo := dateTo.AddDate(-1, 0, 0)
	lyStartUTC, lyEndUTC := timeutil.LocalDayRangeBounds(lyFrom, lyTo, tz)

	started := time.Now()

	currentTotals, err := s.repo.GetRevenueTotalsTTC(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetRevenueTotalsTTC(ctx, merchantIDs, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}
	lyTotals, err := s.repo.GetRevenueTotalsTTC(ctx, merchantIDs, lyStartUTC, lyEndUTC)
	if err != nil {
		return nil, err
	}

	currentPeriod := RevenuePeriodTotals{
		From: req.DateFrom, To: req.DateTo,
		TotalTTCCents: currentTotals.TotalTTCCents, OrderCount: currentTotals.OrderCount,
	}
	previousPeriod := RevenuePeriodTotals{
		From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02"),
		TotalTTCCents: prevTotals.TotalTTCCents, OrderCount: prevTotals.OrderCount,
	}
	previousYear := RevenuePeriodTotals{
		From: lyFrom.Format("2006-01-02"), To: lyTo.Format("2006-01-02"),
		TotalTTCCents: lyTotals.TotalTTCCents, OrderCount: lyTotals.OrderCount,
	}

	if includeHT {
		if htCents, err := s.repo.GetRevenueTotalsHT(ctx, merchantIDs, currentStartUTC, currentEndUTC); err == nil {
			currentPeriod.TotalHTCents = &htCents
		} else {
			return nil, err
		}
		if htCents, err := s.repo.GetRevenueTotalsHT(ctx, merchantIDs, prevStartUTC, prevEndUTC); err == nil {
			previousPeriod.TotalHTCents = &htCents
		} else {
			return nil, err
		}
		if htCents, err := s.repo.GetRevenueTotalsHT(ctx, merchantIDs, lyStartUTC, lyEndUTC); err == nil {
			previousYear.TotalHTCents = &htCents
		} else {
			return nil, err
		}
	}

	timeline, err := s.repo.GetRevenueTimeline(ctx, merchantIDs, tzString, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	byChannel, err := s.repo.GetRevenueByChannel(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	var byMerchant []RevenueMerchantTotal
	if groupBy == GroupByMerchant {
		byMerchant, err = s.repo.GetRevenueByMerchant(ctx, merchantIDs, currentStartUTC, currentEndUTC)
		if err != nil {
			return nil, err
		}
	}

	resp := &RevenueResponse{
		Scope:          RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
		CurrentPeriod:  currentPeriod,
		PreviousPeriod: previousPeriod,
		PreviousYear:   previousYear,
		Timeline:       timeline,
		ByChannel:      byChannel,
		ByMerchant:     byMerchant,
		HTComputed:     includeHT,
	}

	s.logInstrumentation(ctx, "revenue", merchantIDs, int(periodDays), len(timeline)+len(byChannel)+len(byMerchant), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("revenue", merchantIDs, req.DateFrom, req.DateTo, groupBy, includeHT)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// GetOrders is the Commandes tab's entry point — same scope/period/cache
// shape as GetRevenue, see its doc comment.
func (s *Service) GetOrders(ctx context.Context, req OrdersRequest) (*OrdersResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}

	dateFrom, err := time.Parse("2006-01-02", req.DateFrom)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	dateTo, err := time.Parse("2006-01-02", req.DateTo)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if dateTo.Before(dateFrom) {
		return nil, ErrInvalidRequest
	}

	if s.redis != nil {
		cacheKey := buildCacheKey("orders", merchantIDs, req.DateFrom, req.DateTo, req.GroupBy, false)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp OrdersResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	currentStartUTC, currentEndUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)

	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1
	prevTo := dateFrom.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -int(periodDays)+1)
	prevStartUTC, prevEndUTC := timeutil.LocalDayRangeBounds(prevFrom, prevTo, tz)

	lyFrom := dateFrom.AddDate(-1, 0, 0)
	lyTo := dateTo.AddDate(-1, 0, 0)
	lyStartUTC, lyEndUTC := timeutil.LocalDayRangeBounds(lyFrom, lyTo, tz)

	started := time.Now()

	currentTotals, err := s.repo.GetOrdersTotals(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetOrdersTotals(ctx, merchantIDs, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}
	lyTotals, err := s.repo.GetOrdersTotals(ctx, merchantIDs, lyStartUTC, lyEndUTC)
	if err != nil {
		return nil, err
	}

	currentPeriod := ordersPeriodTotals(req.DateFrom, req.DateTo, currentTotals)
	previousPeriod := ordersPeriodTotals(prevFrom.Format("2006-01-02"), prevTo.Format("2006-01-02"), prevTotals)
	previousYear := ordersPeriodTotals(lyFrom.Format("2006-01-02"), lyTo.Format("2006-01-02"), lyTotals)

	timeline, err := s.repo.GetOrdersTimeline(ctx, merchantIDs, tzString, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	byChannel, err := s.repo.GetOrdersByChannel(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	groupBy := req.GroupBy
	if groupBy == "" {
		groupBy = GroupByNone
	}

	resp := &OrdersResponse{
		Scope:          RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
		CurrentPeriod:  currentPeriod,
		PreviousPeriod: previousPeriod,
		PreviousYear:   previousYear,
		Timeline:       timeline,
		ByChannel:      byChannel,
	}

	s.logInstrumentation(ctx, "orders", merchantIDs, int(periodDays), len(timeline)+len(byChannel), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("orders", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// coversCoverageThreshold gates CoversDataAvailable on more than "at least
// one order recorded a value" — merchant 212 (PROD, the biggest merchant in
// this system) has covers on 12 of 9,694 orders in a 12-month window
// (≈0.12%, verified read-only against staging), almost certainly leftover
// demo/test entries rather than real coverage. A bare ">0" check would
// display a precise-looking "22 couverts" / "X€ par couvert" built from that
// noise — exactly the misleadingly-precise number the "never a silent zero"
// rule exists to prevent, just inverted (misleadingly non-zero instead of
// misleadingly zero). 20% is a deliberately low bar — PERIMETRE.md already
// called 12.5% sur-place coverage "quasi absente" on the old MySQL dataset —
// so it masks near-empty sampling noise without hiding a merchant that is
// genuinely, if imperfectly, recording covers.
const coversCoverageThreshold = 0.2

// ordersPeriodTotals turns the raw repository aggregate into the nilable-covers
// contract — see OrdersPeriodTotals's doc comment.
func ordersPeriodTotals(from, to string, totals OrdersTotals) OrdersPeriodTotals {
	period := OrdersPeriodTotals{
		From:       from,
		To:         to,
		OrderCount: totals.OrderCount,
	}
	if totals.OrderCount > 0 {
		period.AvgBasketTTCCents = totals.TotalTTCCents / totals.OrderCount
	}
	if totals.OrderCount > 0 && float64(totals.OrdersWithCovers)/float64(totals.OrderCount) >= coversCoverageThreshold {
		period.CoversDataAvailable = true
		covers := totals.TotalCovers
		period.TotalCovers = &covers
		perCover := totals.TTCCentsOfOrdersWithCovers / totals.TotalCovers
		period.AvgBasketPerCoverCents = &perCover
	}
	return period
}

// GetPayments is the Règlements tab's entry point.
func (s *Service) GetPayments(ctx context.Context, req PaymentsRequest) (*PaymentsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}

	dateFrom, err := time.Parse("2006-01-02", req.DateFrom)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	dateTo, err := time.Parse("2006-01-02", req.DateTo)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if dateTo.Before(dateFrom) {
		return nil, ErrInvalidRequest
	}

	if s.redis != nil {
		cacheKey := buildCacheKey("payments", merchantIDs, req.DateFrom, req.DateTo, GroupByNone, false)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp PaymentsResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	currentStartUTC, currentEndUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)

	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1
	prevTo := dateFrom.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -int(periodDays)+1)
	prevStartUTC, prevEndUTC := timeutil.LocalDayRangeBounds(prevFrom, prevTo, tz)

	lyFrom := dateFrom.AddDate(-1, 0, 0)
	lyTo := dateTo.AddDate(-1, 0, 0)
	lyStartUTC, lyEndUTC := timeutil.LocalDayRangeBounds(lyFrom, lyTo, tz)

	started := time.Now()

	currentTotals, err := s.repo.GetPaymentsTotals(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetPaymentsTotals(ctx, merchantIDs, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}
	lyTotals, err := s.repo.GetPaymentsTotals(ctx, merchantIDs, lyStartUTC, lyEndUTC)
	if err != nil {
		return nil, err
	}

	timeline, err := s.repo.GetPaymentsTimeline(ctx, merchantIDs, tzString, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	byMethod, err := s.repo.GetPaymentsByMethod(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	resp := &PaymentsResponse{
		Scope: RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		CurrentPeriod: PaymentsPeriodTotals{
			From: req.DateFrom, To: req.DateTo,
			TotalAmountCents: currentTotals.TotalAmountCents, PaymentCount: currentTotals.PaymentCount,
		},
		PreviousPeriod: PaymentsPeriodTotals{
			From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02"),
			TotalAmountCents: prevTotals.TotalAmountCents, PaymentCount: prevTotals.PaymentCount,
		},
		PreviousYear: PaymentsPeriodTotals{
			From: lyFrom.Format("2006-01-02"), To: lyTo.Format("2006-01-02"),
			TotalAmountCents: lyTotals.TotalAmountCents, PaymentCount: lyTotals.PaymentCount,
		},
		Timeline: timeline,
		ByMethod: byMethod,
	}

	s.logInstrumentation(ctx, "payments", merchantIDs, int(periodDays), len(timeline)+len(byMethod), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("payments", merchantIDs, req.DateFrom, req.DateTo, GroupByNone, false)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// GetVAT is the TVA tab's entry point — canonical analytics VAT view, not a
// fiscal document (see VATResponse's doc comment).
func (s *Service) GetVAT(ctx context.Context, req VATRequest) (*VATResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}

	dateFrom, err := time.Parse("2006-01-02", req.DateFrom)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	dateTo, err := time.Parse("2006-01-02", req.DateTo)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if dateTo.Before(dateFrom) {
		return nil, ErrInvalidRequest
	}

	if s.redis != nil {
		cacheKey := buildCacheKey("vat", merchantIDs, req.DateFrom, req.DateTo, GroupByNone, false)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp VATResponse
			if err := json.Unmarshal([]byte(cached), &resp); err == nil {
				return &resp, nil
			}
		}
	}

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	currentStartUTC, currentEndUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)

	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1
	prevTo := dateFrom.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -int(periodDays)+1)
	prevStartUTC, prevEndUTC := timeutil.LocalDayRangeBounds(prevFrom, prevTo, tz)

	lyFrom := dateFrom.AddDate(-1, 0, 0)
	lyTo := dateTo.AddDate(-1, 0, 0)
	lyStartUTC, lyEndUTC := timeutil.LocalDayRangeBounds(lyFrom, lyTo, tz)

	started := time.Now()

	currentTotals, err := s.repo.GetVATTotals(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetVATTotals(ctx, merchantIDs, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}
	lyTotals, err := s.repo.GetVATTotals(ctx, merchantIDs, lyStartUTC, lyEndUTC)
	if err != nil {
		return nil, err
	}

	byRateShares, err := s.repo.GetVATByRate(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	byChannelShares, err := s.repo.GetVATByChannel(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	byRate := apportionVATByRate(byRateShares, currentTotals.TotalHTCents)
	byChannel := apportionVATByChannel(byChannelShares, currentTotals.TotalHTCents)

	resp := &VATResponse{
		Scope: RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		CurrentPeriod: VATPeriodTotals{
			From: req.DateFrom, To: req.DateTo,
			TotalTTCCents: currentTotals.TotalTTCCents, TotalHTCents: currentTotals.TotalHTCents,
			TotalVATCents: currentTotals.TotalTTCCents - currentTotals.TotalHTCents,
		},
		PreviousPeriod: VATPeriodTotals{
			From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02"),
			TotalTTCCents: prevTotals.TotalTTCCents, TotalHTCents: prevTotals.TotalHTCents,
			TotalVATCents: prevTotals.TotalTTCCents - prevTotals.TotalHTCents,
		},
		PreviousYear: VATPeriodTotals{
			From: lyFrom.Format("2006-01-02"), To: lyTo.Format("2006-01-02"),
			TotalTTCCents: lyTotals.TotalTTCCents, TotalHTCents: lyTotals.TotalHTCents,
			TotalVATCents: lyTotals.TotalTTCCents - lyTotals.TotalHTCents,
		},
		ByRate:    byRate,
		ByChannel: byChannel,
	}

	s.logInstrumentation(ctx, "vat", merchantIDs, int(periodDays), len(byRate)+len(byChannel), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("vat", merchantIDs, req.DateFrom, req.DateTo, GroupByNone, false)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// apportionVATByRate turns GetVATByRate's raw (unrounded) HT shares into the
// response's VATRateTotal rows, using apportionCents so BaseHTCents always
// sums to totalHTCents exactly (PROMPT 06 §1) — TTC is already exact
// per-row, so VATCents (TTC-HT) reconciles to TotalVATCents for free once HT
// does.
func apportionVATByRate(shares []VATRateShare, totalHTCents int64) []VATRateTotal {
	htRaw := make([]float64, len(shares))
	for i, s := range shares {
		htRaw[i] = s.HTRaw
	}
	htParts := apportionCents(totalHTCents, htRaw)

	result := make([]VATRateTotal, len(shares))
	for i, s := range shares {
		result[i] = VATRateTotal{
			Rate:        s.Rate,
			BaseHTCents: htParts[i],
			VATCents:    s.TTCCents - htParts[i],
		}
	}
	return result
}

// apportionVATByChannel mirrors apportionVATByRate for the by-channel
// breakdown.
func apportionVATByChannel(shares []VATChannelShare, totalHTCents int64) []VATChannelTotal {
	htRaw := make([]float64, len(shares))
	for i, s := range shares {
		htRaw[i] = s.HTRaw
	}
	htParts := apportionCents(totalHTCents, htRaw)

	result := make([]VATChannelTotal, len(shares))
	for i, s := range shares {
		result[i] = VATChannelTotal{
			Channel:       s.Channel,
			BaseHTCents:   htParts[i],
			VATCents:      s.TTCCents - htParts[i],
			TotalTTCCents: s.TTCCents,
		}
	}
	return result
}

// logInstrumentation is the measurement PROMPT 03 §1.6 asks every analytics
// query to record, until it justifies the decision to pre-aggregate:
// endpoint, size of the merchant scope, window width, rows rendered,
// duration. Deliberately a structured log line (captured by Render/whatever
// aggregates zap output) rather than a new Postgres table: writing
// measurement data through the same fusible-protected instance we're trying
// to protect would defeat the point, and api_request_logs (duration_ms,
// migration 088, already applied) already covers the generic HTTP-level
// latency — this adds the analytics-specific dimensions that table doesn't
// have.
func (s *Service) logInstrumentation(ctx context.Context, endpoint string, merchantIDs []string, windowDays, rowsRendered int, duration time.Duration) {
	logger.FromContext(ctx).Info("analytics_query",
		zap.String("endpoint", endpoint),
		zap.Int("merchant_count", len(merchantIDs)),
		zap.Int("window_days", windowDays),
		zap.Int("rows_rendered", rowsRendered),
		zap.Int64("duration_ms", duration.Milliseconds()),
	)
}

// buildCacheKey includes every dimension that changes the response —
// merchant scope, window, group_by, include_ht — so two requests that differ
// on any of them never collide. merchantIDs is sorted first since scope
// today is a single ID but the key must stay stable once it isn't.
func buildCacheKey(endpoint string, merchantIDs []string, dateFrom, dateTo, groupBy string, includeHT bool) string {
	sorted := append([]string(nil), merchantIDs...)
	sort.Strings(sorted)
	raw := strings.Join(sorted, ",") + "|" + dateFrom + "|" + dateTo + "|" + groupBy + "|" + fmt.Sprint(includeHT)
	sum := sha256.Sum256([]byte(raw))
	return models.AnalyticsCachePrefix + endpoint + ":" + hex.EncodeToString(sum[:])
}
