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
	"welloresto-api/internal/permission"
	"welloresto-api/internal/timeutil"
)

// ErrInvalidRequest covers every request-shape validation failure (bad or
// missing dates, date_from after date_to, an unrecognized group_by) — all
// map to HTTP 400, see handler.go's writeError.
var ErrInvalidRequest = errAnalytics("invalid analytics request")

// ErrNominativeAccessDenied is returned by requireKeyOnAllMerchants
// (PROMPT 23 Phase 2) when the caller lacks a nominative block's permission
// on at least one of the requested establishments. Deliberately distinct
// from ErrMerchantNotAccessible: that one means "you can't see this
// establishment's analytics at all"; this one means "you can see it, but not
// this specific nominative ranking" — the merged-blocks decision requires
// the block's permission on EVERY selected establishment, never a partial
// ranking that quietly drops the ones the caller can't see (PROMPT 23:
// "Pas de résultat partiel — un classement amputé d'un site sans le dire
// serait pire qu'un bloc absent"). Handler.go maps this to 403, same status
// as ErrMerchantNotAccessible — the frontend already knows to hide a
// nominative block on any 403 here (see GetCancellationsByStaff/
// GetClientsTop/GetUpsellByStaff's doc comments).
var ErrNominativeAccessDenied = errAnalytics("caller lacks the required permission on every selected establishment for this nominative block")

// requireKeyOnAllMerchants enforces PROMPT 23 Phase 2's rule for merged
// nominative blocks: key must be held on EVERY establishment in
// merchantIDs, checked via Repository.HasForMerchant (never just the
// caller's own token merchant, which the route's RequirePermission
// middleware already checked before this service method ever ran).
func (s *Service) requireKeyOnAllMerchants(ctx context.Context, userID string, merchantIDs []string, key permission.Key) error {
	for _, merchantID := range merchantIDs {
		granted, err := s.repo.HasForMerchant(ctx, userID, merchantID, key)
		if err != nil {
			return err
		}
		if !granted {
			return ErrNominativeAccessDenied
		}
	}
	return nil
}

type Service struct {
	repo  *Repository
	redis *redisclient.Client
}

func NewService(repo *Repository, redis *redisclient.Client) *Service {
	return &Service{repo: repo, redis: redis}
}

// GetAccessibleMerchants powers the multi-establishment selector (PROMPT 24
// Phase 1/3): resolves the caller's accessible scope exactly like every other
// tab (ResolveAccessibleMerchants), then labels it with names. No request
// body — the scope is entirely a function of the caller's token, same as
// every other endpoint in this package.
func (s *Service) GetAccessibleMerchants(ctx context.Context) (*AccessibleMerchantsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	if len(accessible) == 0 {
		return &AccessibleMerchantsResponse{Merchants: []AccessibleMerchant{}}, nil
	}

	merchants, err := s.repo.GetMerchantNames(ctx, accessible)
	if err != nil {
		return nil, err
	}
	return &AccessibleMerchantsResponse{Merchants: merchants}, nil
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

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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
		cacheKey := buildCacheKey("orders", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
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

	var byMerchant []OrdersMerchantTotal
	if groupBy == GroupByMerchant {
		byMerchant, err = s.repo.GetOrdersByMerchant(ctx, merchantIDs, currentStartUTC, currentEndUTC)
		if err != nil {
			return nil, err
		}
	}

	resp := &OrdersResponse{
		Scope:          RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
		CurrentPeriod:  currentPeriod,
		PreviousPeriod: previousPeriod,
		PreviousYear:   previousYear,
		Timeline:       timeline,
		ByChannel:      byChannel,
		ByMerchant:     byMerchant,
	}

	s.logInstrumentation(ctx, "orders", merchantIDs, int(periodDays), len(timeline)+len(byChannel)+len(byMerchant), time.Since(started))

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

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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
		cacheKey := buildCacheKey("payments", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
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

	var byMerchant []PaymentsMerchantTotal
	if groupBy == GroupByMerchant {
		byMerchant, err = s.repo.GetPaymentsByMerchant(ctx, merchantIDs, currentStartUTC, currentEndUTC)
		if err != nil {
			return nil, err
		}
	}

	resp := &PaymentsResponse{
		Scope: RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
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
		Timeline:   timeline,
		ByMethod:   byMethod,
		ByMerchant: byMerchant,
	}

	s.logInstrumentation(ctx, "payments", merchantIDs, int(periodDays), len(timeline)+len(byMethod), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("payments", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
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

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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
		cacheKey := buildCacheKey("vat", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
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

	var byMerchant []VATMerchantTotal
	if groupBy == GroupByMerchant {
		byMerchant, err = s.buildVATByMerchant(ctx, merchantIDs, currentStartUTC, currentEndUTC)
		if err != nil {
			return nil, err
		}
	}

	resp := &VATResponse{
		Scope: RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
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
		ByRate:     byRate,
		ByChannel:  byChannel,
		ByMerchant: byMerchant,
	}

	s.logInstrumentation(ctx, "vat", merchantIDs, int(periodDays), len(byRate)+len(byChannel), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("vat", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// buildVATByMerchant is PROMPT 24 Phase 2's per-establishment VAT breakdown.
// Critically, apportionVATByRate/apportionVATByChannel run ONCE PER
// ESTABLISHMENT here, each against that establishment's own TotalHTCents —
// never against the combined scope's total. Apportioning against the
// combined total would produce establishment-level rows whose own parts do
// not sum to that establishment's own total, exactly the defect PROMPT 06 §1
// fixed at the whole-scope level; doing it per establishment is the only way
// to preserve that guarantee once establishments can be compared side by
// side. See TestGetVAT_GroupByMerchant_PartsSumToOwnTotal_Postgres for the
// reconciliation test this guarantees.
func (s *Service) buildVATByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]VATMerchantTotal, error) {
	totals, err := s.repo.GetVATTotalsByMerchant(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	rateShares, err := s.repo.GetVATByRateByMerchant(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	channelShares, err := s.repo.GetVATByChannelByMerchant(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	rateByMerchant := make(map[string][]VATRateShare, len(totals))
	for _, r := range rateShares {
		rateByMerchant[r.MerchantID] = append(rateByMerchant[r.MerchantID], VATRateShare{Rate: r.Rate, TTCCents: r.TTCCents, HTRaw: r.HTRaw})
	}
	channelByMerchant := make(map[string][]VATChannelShare, len(totals))
	for _, c := range channelShares {
		channelByMerchant[c.MerchantID] = append(channelByMerchant[c.MerchantID], VATChannelShare{Channel: c.Channel, TTCCents: c.TTCCents, HTRaw: c.HTRaw})
	}

	result := make([]VATMerchantTotal, 0, len(totals))
	for _, t := range totals {
		result = append(result, VATMerchantTotal{
			MerchantID:    t.MerchantID,
			TotalTTCCents: t.TotalTTCCents,
			TotalHTCents:  t.TotalHTCents,
			TotalVATCents: t.TotalTTCCents - t.TotalHTCents,
			ByRate:        apportionVATByRate(rateByMerchant[t.MerchantID], t.TotalHTCents),
			ByChannel:     apportionVATByChannel(channelByMerchant[t.MerchantID], t.TotalHTCents),
		})
	}
	return result, nil
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

// GetCancellations is the Annulations tab's aggregate entry point (POST
// /analytics/cancellations, permission.ReportsSalesRead) — same scope/
// period/cache shape as GetPayments/GetVAT. Never the nominative breakdown;
// that is GetCancellationsByStaff below, behind a different permission.
func (s *Service) GetCancellations(ctx context.Context, req CancellationsRequest) (*CancellationsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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
		cacheKey := buildCacheKey("cancellations", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp CancellationsResponse
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

	currentPeriod, err := s.cancellationsPeriodTotals(ctx, merchantIDs, req.DateFrom, req.DateTo, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	previousPeriod, err := s.cancellationsPeriodTotals(ctx, merchantIDs, prevFrom.Format("2006-01-02"), prevTo.Format("2006-01-02"), prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}
	previousYear, err := s.cancellationsPeriodTotals(ctx, merchantIDs, lyFrom.Format("2006-01-02"), lyTo.Format("2006-01-02"), lyStartUTC, lyEndUTC)
	if err != nil {
		return nil, err
	}

	byReason, err := s.repo.GetCancellationsByReason(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	byAuthorType, err := s.repo.GetCancellationsByAuthorType(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	byChannel, err := s.repo.GetCancellationsByChannel(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	var byMerchant []CancellationsMerchantTotal
	if groupBy == GroupByMerchant {
		byMerchant, err = s.cancellationsByMerchant(ctx, merchantIDs, currentStartUTC, currentEndUTC)
		if err != nil {
			return nil, err
		}
	}

	resp := &CancellationsResponse{
		Scope:          RevenueScope{MerchantIDs: merchantIDs, GroupBy: groupBy},
		CurrentPeriod:  currentPeriod,
		PreviousPeriod: previousPeriod,
		PreviousYear:   previousYear,
		ByReason:       byReason,
		ByAuthorType:   byAuthorType,
		ByChannel:      byChannel,
		ByMerchant:     byMerchant,
	}

	s.logInstrumentation(ctx, "cancellations", merchantIDs, int(periodDays), len(byReason)+len(byAuthorType)+len(byChannel), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildCacheKey("cancellations", merchantIDs, req.DateFrom, req.DateTo, groupBy, false)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// cancellationsByMerchant merges GetOrdersCreatedCountByMerchant (the rate's
// denominator, PROMPT 24 Phase 2) with GetCancellationsTotalsByMerchant —
// two separate GROUP BY queries over different scopes (every order created
// vs. only cancelled ones), so a merchant with orders but zero cancellations
// in the period appears with CancelledCount 0 rather than being silently
// dropped by an inner join.
func (s *Service) cancellationsByMerchant(ctx context.Context, merchantIDs []string, startUTC, endUTC time.Time) ([]CancellationsMerchantTotal, error) {
	ordersCreated, err := s.repo.GetOrdersCreatedCountByMerchant(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}
	cancellations, err := s.repo.GetCancellationsTotalsByMerchant(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	cancellationsByID := make(map[string]CancellationsMerchantTotals, len(cancellations))
	for _, c := range cancellations {
		cancellationsByID[c.MerchantID] = c
	}

	result := make([]CancellationsMerchantTotal, 0, len(ordersCreated))
	for _, oc := range ordersCreated {
		c := cancellationsByID[oc.MerchantID]
		result = append(result, CancellationsMerchantTotal{
			MerchantID:             oc.MerchantID,
			TotalOrdersCreated:     oc.Count,
			CancelledCount:         c.CancelledCount,
			CancelledAmountCents:   c.CancelledAmountCents,
			InternalCancelledCount: c.InternalCancelledCount,
			PlatformCancelledCount: c.PlatformCancelledCount,
			UnknownCancelledCount:  c.UnknownCancelledCount,
		})
	}
	return result, nil
}

// cancellationsPeriodTotals loads one period's CancellationsPeriodTotals —
// factored out of GetCancellations since it runs three times (current,
// previous, previous year), the same shape as every other tab's per-period
// loop in this file.
func (s *Service) cancellationsPeriodTotals(ctx context.Context, merchantIDs []string, from, to string, startUTC, endUTC time.Time) (CancellationsPeriodTotals, error) {
	ordersCreated, err := s.repo.GetOrdersCreatedCount(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return CancellationsPeriodTotals{}, err
	}
	totals, err := s.repo.GetCancellationsTotals(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return CancellationsPeriodTotals{}, err
	}
	return CancellationsPeriodTotals{
		From:                   from,
		To:                     to,
		TotalOrdersCreated:     ordersCreated,
		CancelledCount:         totals.CancelledCount,
		CancelledAmountCents:   totals.CancelledAmountCents,
		InternalCancelledCount: totals.InternalCancelledCount,
		PlatformCancelledCount: totals.PlatformCancelledCount,
		UnknownCancelledCount:  totals.UnknownCancelledCount,
	}, nil
}

// GetCancellationsByStaff is the nominative ranking's entry point (POST
// /analytics/cancellations/by-staff, permission.ReportsStaffPerformanceRead
// — see routes.go for why this needs its own route rather than living under
// /analytics's shared reports.sales.read group). No cache: the repository
// query itself already runs against the fusible-protected low-priority pool
// like every other query here, and this response is small (one row per
// server with any cancellation, PROD scope tops out at a handful — see
// cancellations.go's staffCancellationMinOrders doc comment) — not worth a
// second cache key namespace for a response this cheap.
func (s *Service) GetCancellationsByStaff(ctx context.Context, req CancellationsByStaffRequest) (*CancellationsByStaffResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}
	if err := s.requireKeyOnAllMerchants(ctx, user.UserID, merchantIDs, permission.ReportsStaffPerformanceRead); err != nil {
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

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	startUTC, endUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)
	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1

	started := time.Now()

	staff, err := s.repo.GetCancellationsByStaff(ctx, merchantIDs, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	resp := &CancellationsByStaffResponse{
		Scope:            RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		From:             req.DateFrom,
		To:               req.DateTo,
		MinOrdersForRate: staffCancellationMinOrders,
		Staff:            staff,
	}

	s.logInstrumentation(ctx, "cancellations_by_staff", merchantIDs, int(periodDays), len(staff), time.Since(started))

	return resp, nil
}

// GetProducts is the Produits tab's entry point (PROMPT 16). Same scope/
// cache shape as the other five tabs, plus pagination and server-side
// sorting — see ProductsRequest's doc comment (models.go) for why those two
// are request fields resolved in SQL rather than client-side truncation/
// sort, and products.go's doc comment for why this still costs only 3-4
// queries against the fusible-protected pool despite the extra shape.
func (s *Service) GetProducts(ctx context.Context, req ProductsRequest) (*ProductsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = ProductsSortQuantity
	}
	if sortBy != ProductsSortQuantity && sortBy != ProductsSortRevenue && sortBy != ProductsSortMargin {
		return nil, ErrInvalidRequest
	}
	sortDir := strings.ToLower(strings.TrimSpace(req.SortDir))
	if sortDir == "" {
		sortDir = "desc"
	}
	if sortDir != "asc" && sortDir != "desc" {
		return nil, ErrInvalidRequest
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = ProductsDefaultPageSize
	}
	if pageSize > ProductsMaxPageSize {
		pageSize = ProductsMaxPageSize
	}

	if s.redis != nil {
		cacheKey := buildProductsCacheKey(merchantIDs, req.DateFrom, req.DateTo, req.CategoryID, sortBy, sortDir, page, pageSize)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp ProductsResponse
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

	categories, err := s.repo.GetProductCategories(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}
	if req.CategoryID != "" {
		found := false
		for _, c := range categories {
			if c.CategoryID == req.CategoryID {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrInvalidRequest
		}
	}

	started := time.Now()

	currentTotals, err := s.repo.GetProductsScopeTotals(ctx, merchantIDs, req.CategoryID, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetProductsScopeTotals(ctx, merchantIDs, req.CategoryID, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}

	aggRows, totalProducts, err := s.repo.GetProductsPage(ctx, merchantIDs, req.CategoryID, sortBy, sortDir, page, pageSize, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	productIDs := make([]string, len(aggRows))
	for i, row := range aggRows {
		productIDs[i] = row.ProductID
	}
	prevRevenueByProduct, err := s.repo.GetProductsPreviousRevenue(ctx, merchantIDs, productIDs, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}

	rows := make([]ProductRow, len(aggRows))
	for i, agg := range aggRows {
		row := ProductRow{
			ProductID: agg.ProductID, Name: agg.Name,
			CategoryID: agg.CategoryID, CategoryName: agg.CategoryName,
			QuantitySold: agg.QuantitySold, RevenueTTCCents: agg.RevenueTTCCents, RevenueHTCents: agg.RevenueHTCents,
			CostKnownQuantity: agg.CostKnownQuantity, CostKnownRevenueTTCCents: agg.CostKnownRevenueTTCCents,
			NoRecipeQuantity: agg.NoRecipeQuantity, IncompleteRecipeQuantity: agg.IncompleteRecipeQuantity,
		}
		if agg.CostPriceCents.Valid {
			cost := agg.CostPriceCents.Int64
			row.CostPriceCents = &cost
			margin := agg.CostKnownRevenueTTCCents - cost
			row.MarginCents = &margin
			if agg.CostKnownRevenueTTCCents > 0 {
				pct := float64(margin) / float64(agg.CostKnownRevenueTTCCents) * 100
				row.MarginPercent = &pct
			}
		}
		if prevRevenue, ok := prevRevenueByProduct[agg.ProductID]; ok && prevRevenue > 0 {
			evo := float64(agg.RevenueTTCCents-prevRevenue) / float64(prevRevenue) * 100
			row.EvolutionPercent = &evo
		}
		rows[i] = row
	}

	coverage := ProductsCostCoverage{
		RevenueTTCCentsTotal:     currentTotals.RevenueTTCCents,
		RevenueTTCCentsCovered:   currentTotals.CostKnownRevenueTTCCents,
		NoRecipeQuantity:         currentTotals.NoRecipeQuantity,
		IncompleteRecipeQuantity: currentTotals.IncompleteRecipeQuantity,
	}
	if currentTotals.RevenueTTCCents > 0 {
		coverage.CoverageRatio = float64(currentTotals.CostKnownRevenueTTCCents) / float64(currentTotals.RevenueTTCCents)
	}
	// coversCoverageThreshold (this file, above) is reused verbatim here —
	// see ProductsCostCoverage's doc comment (models.go) for why PROMPT 16
	// asks for the same materiality bar rather than a new one.
	if coverage.CoverageRatio >= coversCoverageThreshold && currentTotals.CostKnownRevenueTTCCents > 0 {
		margin := currentTotals.CostKnownRevenueTTCCents - currentTotals.CostPriceCents
		coverage.MarginCents = &margin
		pct := float64(margin) / float64(currentTotals.CostKnownRevenueTTCCents) * 100
		coverage.MarginPercent = &pct
	}

	totalPages := 0
	if totalProducts > 0 {
		totalPages = int((totalProducts + int64(pageSize) - 1) / int64(pageSize))
	}

	resp := &ProductsResponse{
		Scope:      RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		CategoryID: req.CategoryID,
		SortBy:     sortBy,
		SortDir:    sortDir,
		CurrentPeriod: ProductsPeriodTotals{
			From: req.DateFrom, To: req.DateTo,
			QuantitySold: currentTotals.QuantitySold, RevenueTTCCents: currentTotals.RevenueTTCCents, RevenueHTCents: currentTotals.RevenueHTCents,
		},
		PreviousPeriod: ProductsPeriodTotals{
			From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02"),
			QuantitySold: prevTotals.QuantitySold, RevenueTTCCents: prevTotals.RevenueTTCCents, RevenueHTCents: prevTotals.RevenueHTCents,
		},
		CostCoverage:        coverage,
		AvailableCategories: categories,
		Pagination: models.PaginationMetadata{
			TotalItems: int(totalProducts), TotalPages: totalPages, CurrentPage: page, Limit: pageSize,
		},
		Rows: rows,
	}

	s.logInstrumentation(ctx, "products", merchantIDs, int(periodDays), len(rows), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildProductsCacheKey(merchantIDs, req.DateFrom, req.DateTo, req.CategoryID, sortBy, sortDir, page, pageSize)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// GetOptions is the Options tab's entry point (PROMPT 17). Same scope/cache/
// pagination shape as GetProducts — see ProductsRequest's and this file's
// GetProducts doc comments — plus the OptionTypes filter (options.go's
// optionTypesFilter), which the mock this replaces accepted but never
// actually applied.
func (s *Service) GetOptions(ctx context.Context, req OptionsRequest) (*OptionsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	optionTypes, ok := optionTypesFilter(req.OptionTypes)
	if !ok {
		return nil, ErrInvalidRequest
	}

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = OptionsSortQuantity
	}
	if sortBy != OptionsSortQuantity && sortBy != OptionsSortRevenue && sortBy != OptionsSortMargin {
		return nil, ErrInvalidRequest
	}
	sortDir := strings.ToLower(strings.TrimSpace(req.SortDir))
	if sortDir == "" {
		sortDir = "desc"
	}
	if sortDir != "asc" && sortDir != "desc" {
		return nil, ErrInvalidRequest
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = OptionsDefaultPageSize
	}
	if pageSize > OptionsMaxPageSize {
		pageSize = OptionsMaxPageSize
	}

	if s.redis != nil {
		cacheKey := buildOptionsCacheKey(merchantIDs, req.DateFrom, req.DateTo, optionTypes, sortBy, sortDir, page, pageSize)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp OptionsResponse
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

	started := time.Now()

	currentTotals, err := s.repo.GetOptionsScopeTotals(ctx, merchantIDs, optionTypes, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	prevTotals, err := s.repo.GetOptionsScopeTotals(ctx, merchantIDs, optionTypes, prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}

	aggRows, totalRows, err := s.repo.GetOptionsPage(ctx, merchantIDs, optionTypes, sortBy, sortDir, page, pageSize, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	// Adoption denominator: each row's own product's total unit sales in
	// scope, bounded to the current page's products (never the full
	// catalog) — see GetOptionsProductTotals' doc comment.
	productIDSet := make(map[string]struct{}, len(aggRows))
	var optionIDs, removedIDs []string
	for _, row := range aggRows {
		productIDSet[row.ProductID] = struct{}{}
		if row.OptionType == OptionTypeRemoved {
			removedIDs = append(removedIDs, row.EntityID)
		} else {
			optionIDs = append(optionIDs, row.EntityID)
		}
	}
	productIDs := make([]string, 0, len(productIDSet))
	for id := range productIDSet {
		productIDs = append(productIDs, id)
	}
	productTotals, err := s.repo.GetOptionsProductTotals(ctx, merchantIDs, productIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	// Basket impact: bounded to the current page's entities, split by source
	// table (options.go's GetOptionsBasketShares/GetOptionsBasketSharesRemoved
	// doc comments explain why removed-ingredient entities need a different
	// join).
	optionShares, err := s.repo.GetOptionsBasketShares(ctx, merchantIDs, optionIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	removedShares, err := s.repo.GetOptionsBasketSharesRemoved(ctx, merchantIDs, removedIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	// scopeOrders is the complementary side of the basket-impact comparison —
	// "every other scope order" for a given entity is derived by subtracting
	// that entity's own share from this whole-scope total, rather than a
	// second per-entity query for the complement (see basketImpactCents).
	scopeOrders, err := s.repo.GetRevenueTotalsTTC(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	rows := make([]OptionRow, len(aggRows))
	for i, agg := range aggRows {
		row := OptionRow{
			EntityID: agg.EntityID, Name: agg.Name, ProductID: agg.ProductID, ProductName: agg.ProductName,
			OptionType:   agg.OptionType,
			QuantitySold: agg.QuantitySold, RevenueTTCCents: agg.RevenueTTCCents,
			CostKnownQuantity: agg.CostKnownQuantity, CostKnownRevenueTTCCents: agg.CostKnownRevenueTTCCents,
			NoRecipeQuantity: agg.NoRecipeQuantity, IncompleteRecipeQuantity: agg.IncompleteRecipeQuantity,
			UnitsWithThis: agg.AdoptionUnits,
		}
		if agg.AttributeName.Valid {
			row.AttributeName = agg.AttributeName.String
		}
		if agg.CostPriceCents.Valid {
			cost := agg.CostPriceCents.Int64
			row.CostPriceCents = &cost
			margin := agg.CostKnownRevenueTTCCents - cost
			row.MarginCents = &margin
			if agg.CostKnownRevenueTTCCents > 0 {
				pct := float64(margin) / float64(agg.CostKnownRevenueTTCCents) * 100
				row.MarginPercent = &pct
			}
		}
		if productTotal, ok := productTotals[agg.ProductID]; ok && productTotal > 0 {
			row.ProductUnitsSold = productTotal
			rate := float64(agg.AdoptionUnits) / float64(productTotal) * 100
			row.AdoptionRate = &rate
		}

		var share OptionBasketShare
		var found bool
		if agg.OptionType == OptionTypeRemoved {
			share, found = removedShares[agg.EntityID]
		} else {
			share, found = optionShares[agg.EntityID]
		}
		if found {
			row.BasketImpactCents = basketImpactCents(share, scopeOrders)
		}

		rows[i] = row
	}

	coverage := OptionsCostCoverage{
		RevenueTTCCentsTotal:     currentTotals.RevenueTTCCents,
		RevenueTTCCentsCovered:   currentTotals.CostKnownRevenueTTCCents,
		NoRecipeQuantity:         currentTotals.NoRecipeQuantity,
		IncompleteRecipeQuantity: currentTotals.IncompleteRecipeQuantity,
	}
	if currentTotals.RevenueTTCCents > 0 {
		coverage.CoverageRatio = float64(currentTotals.CostKnownRevenueTTCCents) / float64(currentTotals.RevenueTTCCents)
	}
	if coverage.CoverageRatio >= coversCoverageThreshold && currentTotals.CostKnownRevenueTTCCents > 0 {
		margin := currentTotals.CostKnownRevenueTTCCents - currentTotals.CostPriceCents
		coverage.MarginCents = &margin
		pct := float64(margin) / float64(currentTotals.CostKnownRevenueTTCCents) * 100
		coverage.MarginPercent = &pct
	}

	totalPages := 0
	if totalRows > 0 {
		totalPages = int((totalRows + int64(pageSize) - 1) / int64(pageSize))
	}

	resp := &OptionsResponse{
		Scope: RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		OptionTypes: optionTypes, SortBy: sortBy, SortDir: sortDir,
		CurrentPeriod: OptionsPeriodTotals{
			From: req.DateFrom, To: req.DateTo,
			QuantitySold: currentTotals.QuantitySold, RevenueTTCCents: currentTotals.RevenueTTCCents,
		},
		PreviousPeriod: OptionsPeriodTotals{
			From: prevFrom.Format("2006-01-02"), To: prevTo.Format("2006-01-02"),
			QuantitySold: prevTotals.QuantitySold, RevenueTTCCents: prevTotals.RevenueTTCCents,
		},
		CostCoverage: coverage,
		Pagination: models.PaginationMetadata{
			TotalItems: int(totalRows), TotalPages: totalPages, CurrentPage: page, Limit: pageSize,
		},
		Rows: rows,
	}

	s.logInstrumentation(ctx, "options", merchantIDs, int(periodDays), len(rows), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildOptionsCacheKey(merchantIDs, req.DateFrom, req.DateTo, optionTypes, sortBy, sortDir, page, pageSize)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// basketImpactCents computes one entity's basket-impact delta: the average
// order TTC of scope orders that contained it, minus the average of every
// other scope order — nil whenever either side has zero orders (an entity
// present in literally every scope order, or, defensively, absent from the
// bounded share map entirely), never a division by zero.
func basketImpactCents(share OptionBasketShare, scopeOrders RevenueTotals) *int64 {
	if share.OrderCount <= 0 {
		return nil
	}
	otherCount := scopeOrders.OrderCount - share.OrderCount
	if otherCount <= 0 {
		return nil
	}
	avgWith := share.OrderPriceSum / share.OrderCount
	avgWithout := (scopeOrders.TotalTTCCents - share.OrderPriceSum) / otherCount
	delta := avgWith - avgWithout
	return &delta
}

// buildOptionsCacheKey mirrors buildProductsCacheKey's shape but includes
// this tab's own dimensions (option_types/sort/page) instead of category.
func buildOptionsCacheKey(merchantIDs []string, dateFrom, dateTo string, optionTypes []string, sortBy, sortDir string, page, pageSize int) string {
	sorted := append([]string(nil), merchantIDs...)
	sort.Strings(sorted)
	sortedTypes := append([]string(nil), optionTypes...)
	sort.Strings(sortedTypes)
	raw := strings.Join(sorted, ",") + "|" + dateFrom + "|" + dateTo + "|" + strings.Join(sortedTypes, ",") + "|" + sortBy + "|" + sortDir + "|" + fmt.Sprint(page) + "|" + fmt.Sprint(pageSize)
	sum := sha256.Sum256([]byte(raw))
	return models.AnalyticsCachePrefix + "options:" + hex.EncodeToString(sum[:])
}

// buildProductsCacheKey mirrors buildCacheKey's shape but includes this
// tab's own dimensions (category/sort/page) instead of group_by/include_ht —
// kept separate rather than overloading buildCacheKey's signature, since
// every other tab shares one contract shape and this is the only paginated
// one.
func buildProductsCacheKey(merchantIDs []string, dateFrom, dateTo, categoryID, sortBy, sortDir string, page, pageSize int) string {
	sorted := append([]string(nil), merchantIDs...)
	sort.Strings(sorted)
	raw := strings.Join(sorted, ",") + "|" + dateFrom + "|" + dateTo + "|" + categoryID + "|" + sortBy + "|" + sortDir + "|" + fmt.Sprint(page) + "|" + fmt.Sprint(pageSize)
	sum := sha256.Sum256([]byte(raw))
	return models.AnalyticsCachePrefix + "products:" + hex.EncodeToString(sum[:])
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

// ---- Clients (PROMPT 18) ----

const (
	ClientsSegmentNew       = "nouveau"
	ClientsSegmentLoyal     = "fidele"
	ClientsSegmentReturning = "recurrent"
	ClientsSegmentInactive  = "inactif"
	ClientsSegmentDormant   = "dormant"
)

// minCustomersForRate reuses staffCancellationMinOrders (cancellations.go)
// verbatim — PROMPT 18 §6 asks for "le seuil de matérialité déjà retenu
// ailleurs," the same instruction PROMPT 16 gave for ProductsCostCoverage's
// coversCoverageThreshold, applied here to a per-customer rate the same way
// Annulations applies it to a per-server rate. Aliasing the identifier
// (rather than copying the literal 30) guarantees the two can never quietly
// drift apart.
const minCustomersForRate = staffCancellationMinOrders

// clientsLoyalOrdersThreshold is PROMPT 18 §4's fidélité threshold: an active
// (not-new) customer needs at least this many LIFETIME orders — not orders
// in this period alone — to be counted "fidèle" rather than merely
// "récurrent."
const clientsLoyalOrdersThreshold = 5

// clientsInactivityDays is PROMPT 18 §4's inactivity threshold: a customer
// counts as "inactif" once their last order is more than this many days
// before the period's own end date — never wall-clock now(), so a report run
// today for a period that ended a year ago gives the same answer as one run
// the day after that period ended (see clients.go's doc comment on why every
// lifetime figure here is anchored to periodEnd). 180 days (~6 months) is a
// deliberately generous bar: a restaurant's regulars naturally order less
// often than a daily-use app's users, so a shorter window would mislabel
// ordinary seasonal customers as churned.
const clientsInactivityDays = 180

// clientsSegmentOrder is the fixed display order for ClientsResponse.Segments
// — new, returning, loyal, then the two "not active this period" buckets —
// so the frontend never has to sort or guess which segment is missing.
var clientsSegmentOrder = []string{
	ClientsSegmentNew, ClientsSegmentReturning, ClientsSegmentLoyal, ClientsSegmentInactive, ClientsSegmentDormant,
}

// computeClientsSegments partitions every CustomerLifetimeRow into exactly
// one of 5 buckets, evaluated in this precedence order (PROMPT 18 §4):
//
//  1. ClientsSegmentNew — first order EVER falls inside [periodStart, periodEnd).
//  2. ClientsSegmentLoyal — not new, >= clientsLoyalOrdersThreshold lifetime
//     orders, last order inside the period (still active).
//  3. ClientsSegmentReturning — not new, not loyal, last order inside the period.
//  4. ClientsSegmentInactive — last order more than clientsInactivityDays
//     before periodEnd.
//  5. ClientsSegmentDormant — everything else: ordered before the period, not
//     recently enough to be active this period, but not old enough yet to
//     count as inactive. Named explicitly rather than dropped — this
//     package's "jamais être exclues en silence" rule (see e.g.
//     CancellationReasonTotal's doc comment), applied here to customers
//     instead of cancellation reasons: a customer who last ordered 90 days
//     before a 12-month window's end is neither this period's customer nor
//     churned, and the tab says so instead of hiding them from every bucket.
//
// Dormant is only reachable when the requested window is SHORTER than
// clientsInactivityDays: if periodEnd-periodStart >= clientsInactivityDays,
// any last order before periodStart is already, by construction, more than
// clientsInactivityDays before periodEnd — there is no gap left between "not
// active this period" and "stale enough to be inactif." For the brief's own
// 12-month verification window, this bucket is always empty; that is
// expected, not a bug (see clients_test.go's completeness test, which uses a
// shorter window specifically to exercise this bucket).
func computeClientsSegments(rows []CustomerLifetimeRow, periodStart, periodEnd time.Time) map[string][]CustomerLifetimeRow {
	buckets := map[string][]CustomerLifetimeRow{
		ClientsSegmentNew:       nil,
		ClientsSegmentLoyal:     nil,
		ClientsSegmentReturning: nil,
		ClientsSegmentInactive:  nil,
		ClientsSegmentDormant:   nil,
	}
	inactivityCutoff := periodEnd.AddDate(0, 0, -clientsInactivityDays)

	for _, row := range rows {
		isNew := !row.FirstOrderDate.Before(periodStart) && row.FirstOrderDate.Before(periodEnd)
		activeInPeriod := !row.LastOrderDate.Before(periodStart) && row.LastOrderDate.Before(periodEnd)

		switch {
		case isNew:
			buckets[ClientsSegmentNew] = append(buckets[ClientsSegmentNew], row)
		case activeInPeriod && row.LifetimeOrders >= clientsLoyalOrdersThreshold:
			buckets[ClientsSegmentLoyal] = append(buckets[ClientsSegmentLoyal], row)
		case activeInPeriod:
			buckets[ClientsSegmentReturning] = append(buckets[ClientsSegmentReturning], row)
		case row.LastOrderDate.Before(inactivityCutoff):
			buckets[ClientsSegmentInactive] = append(buckets[ClientsSegmentInactive], row)
		default:
			buckets[ClientsSegmentDormant] = append(buckets[ClientsSegmentDormant], row)
		}
	}
	return buckets
}

// clientsSegmentCounts turns computeClientsSegments' buckets into the
// response's fixed-order rows — AvgBasketTTCCents is nil whenever the segment
// has zero period orders (always true for inactif/dormant, by construction:
// both buckets are defined by having no order in the period).
func clientsSegmentCounts(buckets map[string][]CustomerLifetimeRow) []ClientsSegmentCount {
	result := make([]ClientsSegmentCount, 0, len(clientsSegmentOrder))
	for _, seg := range clientsSegmentOrder {
		rows := buckets[seg]
		var periodOrders, periodRevenue int64
		for _, row := range rows {
			periodOrders += row.PeriodOrders
			periodRevenue += row.PeriodRevenueCents
		}
		count := ClientsSegmentCount{Segment: seg, Count: int64(len(rows))}
		if periodOrders > 0 {
			avg := periodRevenue / periodOrders
			count.AvgBasketTTCCents = &avg
		}
		result = append(result, count)
	}
	return result
}

// clientsDefinitions states, in the response itself, which reading this tab
// committed to for each term PROMPT 18 §4 flags as admitting several
// defensible interpretations — see ClientsDefinitions' doc comment (models.go).
func clientsDefinitions() ClientsDefinitions {
	return ClientsDefinitions{
		Recurrence: "Part des clients actifs sur la période ayant au moins 2 commandes au total depuis toujours (pas seulement sur la période).",
		Segments: fmt.Sprintf(
			"Nouveau : première commande de tous les temps tombe dans la période. "+
				"Fidèle : actif sur la période et au moins %d commandes au total depuis toujours. "+
				"Récurrent : actif sur la période, moins de %d commandes au total. "+
				"Inactif : dernière commande il y a plus de %d jours (calculé à la date de fin de la période). "+
				"Dormant : dernière commande avant la période mais il y a moins de %d jours — ni actif ni inactif.",
			clientsLoyalOrdersThreshold, clientsLoyalOrdersThreshold, clientsInactivityDays, clientsInactivityDays,
		),
		Frequency: "Nombre moyen de commandes par client actif sur la période (commandes de la période ÷ clients ayant commandé sur la période) — pas l'intervalle moyen entre deux commandes.",
		Inactivity: fmt.Sprintf(
			"Dernière commande il y a plus de %d jours, calculé à la date de fin de la période analysée — pas à la date du jour.",
			clientsInactivityDays,
		),
	}
}

// GetClients is the Clients tab's aggregate entry point (POST
// /analytics/clients, permission.ReportsSalesRead). Never the nominative
// ranking — that is GetClientsTop below, served by a separate, more tightly
// permissioned endpoint (see this file's "Clients" section and models.go's
// "Clients" doc comment).
func (s *Service) GetClients(ctx context.Context, req ClientsRequest) (*ClientsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	channels, ok := ChannelFilter(req.Channels)
	if !ok {
		return nil, ErrInvalidRequest
	}

	if s.redis != nil {
		cacheKey := buildClientsCacheKey("clients", merchantIDs, req.DateFrom, req.DateTo, channels)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp ClientsResponse
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

	startUTC, endUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)
	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1

	started := time.Now()

	coverage, err := s.repo.GetCustomersCoverage(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	lifetimeRows, err := s.repo.GetCustomersLifetimeStats(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	resp := &ClientsResponse{
		Scope:               RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		Channels:             channels,
		From:                 req.DateFrom,
		To:                   req.DateTo,
		Definitions:          clientsDefinitions(),
		MinCustomersForRate:  minCustomersForRate,
	}
	resp.Coverage = ClientsCoverage{
		OrdersWithCustomerID: coverage.OrdersWithCustomer,
		TotalOrders:          coverage.TotalOrders,
	}
	if coverage.TotalOrders > 0 {
		resp.Coverage.CoverageRatio = float64(coverage.OrdersWithCustomer) / float64(coverage.TotalOrders)
	}

	if len(lifetimeRows) == 0 {
		resp.NoIdentifiedCustomers = true
		resp.Segments = clientsSegmentCounts(computeClientsSegments(nil, startUTC, endUTC))
		s.logInstrumentation(ctx, "clients", merchantIDs, int(periodDays), 0, time.Since(started))
		return resp, nil
	}

	var activeCount, newCount, recurringCount, totalPeriodOrders int64
	for _, row := range lifetimeRows {
		isNew := !row.FirstOrderDate.Before(startUTC) && row.FirstOrderDate.Before(endUTC)
		if isNew {
			newCount++
		}
		if row.PeriodOrders > 0 {
			activeCount++
			totalPeriodOrders += row.PeriodOrders
			if row.LifetimeOrders >= 2 {
				recurringCount++
			}
		}
	}

	resp.IdentifiedCustomersInPeriod = activeCount
	resp.NewCustomersCount = newCount
	resp.RecurringCount = recurringCount
	if activeCount > 0 {
		resp.AvgOrdersPerActiveCustomer = float64(totalPeriodOrders) / float64(activeCount)
	}
	if activeCount >= minCustomersForRate {
		resp.SegmentRatesAvailable = true
		rate := float64(recurringCount) / float64(activeCount)
		resp.RecurringRate = &rate
	}

	resp.Segments = clientsSegmentCounts(computeClientsSegments(lifetimeRows, startUTC, endUTC))

	s.logInstrumentation(ctx, "clients", merchantIDs, int(periodDays), len(lifetimeRows), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildClientsCacheKey("clients", merchantIDs, req.DateFrom, req.DateTo, channels)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// GetClientsTop is the nominative ranking's entry point (POST
// /analytics/clients/top, permission.CustomersManage — see routes.go for why
// this needs its own route rather than living under /analytics's shared
// reports.sales.read group). No cache, same reasoning as
// GetCancellationsByStaff: this response carries named customers behind an
// is_sensitive permission, and is cheap enough (bounded to ClientsTopLimit
// rows) that a second cache namespace isn't worth holding PII in Redis for.
func (s *Service) GetClientsTop(ctx context.Context, req ClientsTopRequest) (*ClientsTopResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}
	if err := s.requireKeyOnAllMerchants(ctx, user.UserID, merchantIDs, permission.CustomersManage); err != nil {
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

	channels, ok := ChannelFilter(req.Channels)
	if !ok {
		return nil, ErrInvalidRequest
	}

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	startUTC, endUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)
	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1

	started := time.Now()

	lifetimeRows, err := s.repo.GetCustomersLifetimeStats(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	sort.Slice(lifetimeRows, func(i, j int) bool {
		return lifetimeRows[i].LifetimeValueCents > lifetimeRows[j].LifetimeValueCents
	})

	limit := ClientsTopLimit
	if len(lifetimeRows) < limit {
		limit = len(lifetimeRows)
	}

	var activeCount int64
	for _, row := range lifetimeRows {
		if row.PeriodOrders > 0 {
			activeCount++
		}
	}

	topClients := make([]ClientRow, limit)
	for i := 0; i < limit; i++ {
		row := lifetimeRows[i]
		var avgBasket int64
		if row.LifetimeOrders > 0 {
			avgBasket = row.LifetimeValueCents / row.LifetimeOrders
		}
		topClients[i] = ClientRow{
			CustomerID:         row.CustomerID,
			Name:                row.DisplayName,
			LifetimeValueCents:  row.LifetimeValueCents,
			LifetimeOrders:      row.LifetimeOrders,
			LastOrderDate:       row.LastOrderDate.In(tz).Format("2006-01-02"),
			AvgBasketTTCCents:   avgBasket,
		}
	}

	resp := &ClientsTopResponse{
		Scope:                       RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		Channels:                    channels,
		From:                        req.DateFrom,
		To:                          req.DateTo,
		IdentifiedCustomersInPeriod: activeCount,
		TopClients:                  topClients,
	}

	s.logInstrumentation(ctx, "clients_top", merchantIDs, int(periodDays), len(topClients), time.Since(started))

	return resp, nil
}

// buildClientsCacheKey mirrors buildOptionsCacheKey's shape but includes this
// tab's own dimension (channels) instead of option_types/sort/page. Reused
// verbatim by GetUpsell below (same merchant/date/channels dimensions, no
// tab-specific extra dimension of its own) rather than adding a fourth
// near-identical builder.
func buildClientsCacheKey(endpoint string, merchantIDs []string, dateFrom, dateTo string, channels []string) string {
	sorted := append([]string(nil), merchantIDs...)
	sort.Strings(sorted)
	sortedChannels := append([]string(nil), channels...)
	sort.Strings(sortedChannels)
	raw := strings.Join(sorted, ",") + "|" + dateFrom + "|" + dateTo + "|" + strings.Join(sortedChannels, ",")
	sum := sha256.Sum256([]byte(raw))
	return models.AnalyticsCachePrefix + endpoint + ":" + hex.EncodeToString(sum[:])
}

// ---- Vente additionnelle (PROMPT 19) ----

// upsellTransformationRateAvailable gates
// UpsellSuggestionsTotals.TransformationRateAvailable — factored out as a
// pure function (mirrors staffCancellationRateAvailable, cancellations.go)
// so the threshold behavior is unit-testable without a database.
func upsellTransformationRateAvailable(proposed int64) bool {
	return proposed >= upsellSuggestionsMinProposed
}

// GetUpsell is the Vente additionnelle tab's aggregate entry point (POST
// /analytics/upsell, permission.ReportsSalesRead) — PROMPT 19. Same scope/
// channel-filter shape as GetClients, same current+previous (no previous
// year) period shape as GetProducts/GetOptions, plus the
// InstrumentationActive gate this tab's whole design hinges on — see
// models.go's package doc comment.
func (s *Service) GetUpsell(ctx context.Context, req UpsellRequest) (*UpsellResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	channels, ok := ChannelFilter(req.Channels)
	if !ok {
		return nil, ErrInvalidRequest
	}

	if s.redis != nil {
		cacheKey := buildClientsCacheKey("upsell", merchantIDs, req.DateFrom, req.DateTo, channels)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp UpsellResponse
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

	started := time.Now()

	active, err := s.repo.GetUpsellInstrumentationActive(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}

	currentPeriod, err := s.upsellPeriodTotals(ctx, merchantIDs, channels, req.DateFrom, req.DateTo, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	previousPeriod, err := s.upsellPeriodTotals(ctx, merchantIDs, channels, prevFrom.Format("2006-01-02"), prevTo.Format("2006-01-02"), prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}

	proposed, accepted, err := s.repo.GetUpsellSuggestionsTotals(ctx, merchantIDs, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	suggestions := UpsellSuggestionsTotals{
		From: req.DateFrom, To: req.DateTo,
		ProposedCount: proposed, AcceptedCount: accepted,
		TransformationRateAvailable: upsellTransformationRateAvailable(proposed),
		MinProposedForRate:          upsellSuggestionsMinProposed,
	}

	resp := &UpsellResponse{
		Scope:                 RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		Channels:              channels,
		InstrumentationActive: active,
		CurrentPeriod:         currentPeriod,
		PreviousPeriod:        previousPeriod,
		Suggestions:           suggestions,
	}

	s.logInstrumentation(ctx, "upsell", merchantIDs, int(periodDays), 0, time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildClientsCacheKey("upsell", merchantIDs, req.DateFrom, req.DateTo, channels)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// upsellPeriodTotals loads one period's UpsellPeriodTotals — factored out of
// GetUpsell since it runs twice (current, previous), same shape as
// cancellationsPeriodTotals.
func (s *Service) upsellPeriodTotals(ctx context.Context, merchantIDs, channels []string, from, to string, startUTC, endUTC time.Time) (UpsellPeriodTotals, error) {
	totals, err := s.repo.GetUpsellTotals(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return UpsellPeriodTotals{}, err
	}
	ordersWithUpsell, err := s.repo.GetOrdersWithUpsellCount(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return UpsellPeriodTotals{}, err
	}
	totalOrders, err := s.repo.GetUpsellOrdersTotal(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return UpsellPeriodTotals{}, err
	}
	return UpsellPeriodTotals{
		From: from, To: to,
		UpsellLines: totals.UpsellLines, UpsellRevenueHTCents: totals.UpsellRevenueHTCents,
		OrdersWithUpsellCount: ordersWithUpsell, TotalOrdersCount: totalOrders,
	}, nil
}

// GetUpsellByStaff is the nominative ranking's entry point (POST
// /analytics/upsell/by-staff, permission.ReportsStaffPerformanceRead — same
// split as Annulations/Clients, see cancellations.go's package doc comment).
// No cache, same reasoning as GetCancellationsByStaff/GetClientsTop: cheap,
// small, sensitive.
func (s *Service) GetUpsellByStaff(ctx context.Context, req UpsellByStaffRequest) (*UpsellByStaffResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
	if err != nil {
		return nil, err
	}
	merchantIDs, err := ValidateRequestedMerchants(req.MerchantIDs, accessible)
	if err != nil {
		return nil, err
	}
	if err := s.requireKeyOnAllMerchants(ctx, user.UserID, merchantIDs, permission.ReportsStaffPerformanceRead); err != nil {
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

	channels, ok := ChannelFilter(req.Channels)
	if !ok {
		return nil, ErrInvalidRequest
	}

	tzString, err := s.repo.GetMerchantTimezone(ctx, merchantIDs[0])
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	tz, err := time.LoadLocation(tzString)
	if err != nil {
		return nil, fmt.Errorf("invalid merchant timezone %q: %w", tzString, err)
	}

	startUTC, endUTC := timeutil.LocalDayRangeBounds(dateFrom, dateTo, tz)
	periodDays := dateTo.Sub(dateFrom).Hours()/24 + 1

	started := time.Now()

	active, err := s.repo.GetUpsellInstrumentationActive(ctx, merchantIDs)
	if err != nil {
		return nil, err
	}

	staff, err := s.repo.GetUpsellByStaff(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return nil, err
	}

	resp := &UpsellByStaffResponse{
		Scope:                 RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		Channels:              channels,
		From:                  req.DateFrom,
		To:                    req.DateTo,
		InstrumentationActive: active,
		Staff:                 staff,
	}

	s.logInstrumentation(ctx, "upsell_by_staff", merchantIDs, int(periodDays), len(staff), time.Since(started))

	return resp, nil
}

// ---- Remises (PROMPT 22) ----

// buildDiscountsCacheKey mirrors buildOptionsCacheKey's shape but with
// channels (like buildClientsCacheKey) instead of option_types, plus this
// tab's own sort/page dimensions — kept separate from buildClientsCacheKey
// since (unlike Upsell/Clients) this tab is also paginated.
func buildDiscountsCacheKey(merchantIDs []string, dateFrom, dateTo string, channels []string, sortBy, sortDir string, page, pageSize int) string {
	sorted := append([]string(nil), merchantIDs...)
	sort.Strings(sorted)
	sortedChannels := append([]string(nil), channels...)
	sort.Strings(sortedChannels)
	raw := strings.Join(sorted, ",") + "|" + dateFrom + "|" + dateTo + "|" + strings.Join(sortedChannels, ",") + "|" + sortBy + "|" + sortDir + "|" + fmt.Sprint(page) + "|" + fmt.Sprint(pageSize)
	sum := sha256.Sum256([]byte(raw))
	return models.AnalyticsCachePrefix + "discounts:" + hex.EncodeToString(sum[:])
}

// discountsRateOrNil computes a percentage gated by discountsMinOrdersForRate
// applied to discountedOrders — factored out since GetDiscounts applies the
// exact same gate twice (DiscountRatePercent, OrdersWithDiscountRatePercent),
// see DiscountsPeriodTotals' doc comment (models.go) for why both share one
// materiality bar.
func discountsRateOrNil(discountedOrders int64, numerator, denominator int64) *float64 {
	if discountedOrders < discountsMinOrdersForRate || denominator <= 0 {
		return nil
	}
	pct := float64(numerator) / float64(denominator) * 100
	return &pct
}

// GetDiscounts is the Remises tab's single entry point (POST
// /analytics/discounts, permission.ReportsSalesRead) — no nominative angle
// (PROMPT 22 explicitly: "aucune donnée nominative ici, un seul endpoint
// suffit"), so unlike Annulations/Clients/Upsell this tab has no by-staff
// sibling. Same scope/channel-filter/pagination shape as GetOptions, plus
// the reconstructed/measured split and margin coverage this section's
// models.go doc comment describes.
func (s *Service) GetDiscounts(ctx context.Context, req DiscountsRequest) (*DiscountsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	accessible, err := s.repo.ResolveAccessibleMerchants(ctx, user)
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

	channels, ok := ChannelFilter(req.Channels)
	if !ok {
		return nil, ErrInvalidRequest
	}

	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = DiscountsSortAmount
	}
	if sortBy != DiscountsSortAmount && sortBy != DiscountsSortCount {
		return nil, ErrInvalidRequest
	}
	sortDir := strings.ToLower(strings.TrimSpace(req.SortDir))
	if sortDir == "" {
		sortDir = "desc"
	}
	if sortDir != "asc" && sortDir != "desc" {
		return nil, ErrInvalidRequest
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = DiscountsDefaultPageSize
	}
	if pageSize > DiscountsMaxPageSize {
		pageSize = DiscountsMaxPageSize
	}

	if s.redis != nil {
		cacheKey := buildDiscountsCacheKey(merchantIDs, req.DateFrom, req.DateTo, channels, sortBy, sortDir, page, pageSize)
		if cached, ok := s.redis.Get(ctx, cacheKey); ok {
			var resp DiscountsResponse
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

	started := time.Now()

	currentPeriod, err := s.discountsPeriodTotals(ctx, merchantIDs, channels, req.DateFrom, req.DateTo, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	previousPeriod, err := s.discountsPeriodTotals(ctx, merchantIDs, channels, prevFrom.Format("2006-01-02"), prevTo.Format("2006-01-02"), prevStartUTC, prevEndUTC)
	if err != nil {
		return nil, err
	}

	marginTotals, err := s.repo.GetDiscountsMarginCoverage(ctx, merchantIDs, channels, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}
	marginImpact := DiscountsMarginCoverage{
		DiscountedLinesRevenueTTCCentsTotal:   marginTotals.RevenueTTCCentsTotal,
		DiscountedLinesRevenueTTCCentsCovered: marginTotals.RevenueTTCCentsCovered,
	}
	if marginTotals.RevenueTTCCentsTotal > 0 {
		marginImpact.CoverageRatio = float64(marginTotals.RevenueTTCCentsCovered) / float64(marginTotals.RevenueTTCCentsTotal)
	}
	// coversCoverageThreshold (this file, GetProducts section) reused
	// verbatim — see DiscountsMarginCoverage's doc comment (models.go).
	if marginImpact.CoverageRatio >= coversCoverageThreshold && marginTotals.DiscountCentsCovered > 0 {
		impact := marginTotals.DiscountCentsCovered
		marginImpact.MarginImpactCents = &impact
		marginBeforeDiscount := marginTotals.RevenueTTCCentsCovered - marginTotals.CostCentsCovered + marginTotals.DiscountCentsCovered
		if marginBeforeDiscount > 0 {
			pct := float64(impact) / float64(marginBeforeDiscount) * 100
			marginImpact.MarginImpactPercent = &pct
		}
	}

	rows, totalRows, err := s.repo.GetDiscountsPage(ctx, merchantIDs, channels, sortBy, sortDir, page, pageSize, currentStartUTC, currentEndUTC)
	if err != nil {
		return nil, err
	}

	var measurementCompleteFrom *string
	if earliest, ok, err := s.repo.GetDiscountsMeasurementCompleteFrom(ctx, merchantIDs); err != nil {
		return nil, err
	} else if ok {
		formatted := earliest.Format("2006-01-02")
		measurementCompleteFrom = &formatted
	}

	totalPages := 0
	if totalRows > 0 {
		totalPages = int((totalRows + int64(pageSize) - 1) / int64(pageSize))
	}

	resp := &DiscountsResponse{
		Scope:                   RevenueScope{MerchantIDs: merchantIDs, GroupBy: GroupByNone},
		Channels:                channels,
		MeasurementCompleteFrom: measurementCompleteFrom,
		SortBy:                  sortBy,
		SortDir:                 sortDir,
		CurrentPeriod:           currentPeriod,
		PreviousPeriod:          previousPeriod,
		MarginImpact:            marginImpact,
		Pagination: models.PaginationMetadata{
			TotalItems: int(totalRows), TotalPages: totalPages, CurrentPage: page, Limit: pageSize,
		},
		Rows: rows,
	}

	s.logInstrumentation(ctx, "discounts", merchantIDs, int(periodDays), len(rows), time.Since(started))

	if s.redis != nil {
		if encoded, err := json.Marshal(resp); err == nil {
			cacheKey := buildDiscountsCacheKey(merchantIDs, req.DateFrom, req.DateTo, channels, sortBy, sortDir, page, pageSize)
			s.redis.Set(ctx, cacheKey, string(encoded), models.AnalyticsCacheTTL)
		}
	}

	return resp, nil
}

// discountsPeriodTotals loads one period's DiscountsPeriodTotals — factored
// out of GetDiscounts since it runs twice (current, previous), same shape as
// cancellationsPeriodTotals/upsellPeriodTotals.
func (s *Service) discountsPeriodTotals(ctx context.Context, merchantIDs, channels []string, from, to string, startUTC, endUTC time.Time) (DiscountsPeriodTotals, error) {
	scopeTotals, err := s.repo.GetDiscountsScopeTotals(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return DiscountsPeriodTotals{}, err
	}
	ordersTotals, err := s.repo.GetDiscountsOrdersTotals(ctx, merchantIDs, channels, startUTC, endUTC)
	if err != nil {
		return DiscountsPeriodTotals{}, err
	}

	period := DiscountsPeriodTotals{
		From: from, To: to,
		TotalDiscountedCents:          scopeTotals.TotalAmountCents,
		ReconstructedAmountCents:      scopeTotals.ReconstructedAmountCents,
		MeasuredAmountCents:           scopeTotals.MeasuredAmountCents,
		ReconstructedRedemptionsCount: scopeTotals.ReconstructedRedemptionsCount,
		MeasuredRedemptionsCount:      scopeTotals.MeasuredRedemptionsCount,
		DiscountedOrdersCount:         scopeTotals.DiscountedOrdersCount,
		TotalOrdersCount:              ordersTotals.TotalOrdersCount,
		ReferenceRevenueTTCCents:      ordersTotals.ReferenceRevenueTTCCents,
	}
	period.OrdersWithDiscountRatePercent = discountsRateOrNil(scopeTotals.DiscountedOrdersCount, scopeTotals.DiscountedOrdersCount, ordersTotals.TotalOrdersCount)
	period.DiscountRatePercent = discountsRateOrNil(scopeTotals.DiscountedOrdersCount, scopeTotals.TotalAmountCents, ordersTotals.ReferenceRevenueTTCCents)
	return period, nil
}
