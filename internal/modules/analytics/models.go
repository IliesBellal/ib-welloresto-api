package analytics

import "welloresto-api/internal/models"

// RevenueRequest is the payload for POST /analytics/revenue.
type RevenueRequest struct {
	// DateFrom/DateTo are local calendar days (YYYY-MM-DD) in the
	// establishment's own timezone, both inclusive.
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
	// MerchantIDs restricts the request to a subset of the caller's
	// accessible scope — every establishment where they hold
	// permission.POSAnalytics (Repository.ResolveAccessibleMerchants, PROMPT
	// 23). Empty means "the full accessible scope". A value outside the
	// accessible scope is a 403, never silently dropped.
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// GroupBy is "none" (cumulé, default) or "merchant" (comparé — one
	// ByMerchant row per establishment, PROMPT 23 Phase 3). Kept in the
	// contract since PROMPT 03, before there was more than one establishment
	// to group by; now load-bearing.
	GroupBy string `json:"group_by,omitempty"`
	// IncludeHT toggles the HT recomputation (see repository.go's doc
	// comment on why HT can't come from orders.ht for marketplace orders).
	// Defaults to true. Exposed as a request flag — not just a server
	// constant — so HT can be turned off from the frontend without a
	// backend deploy if its measured cost (docs/analytics/MESURES.md) turns
	// out too high for regular use.
	IncludeHT *bool `json:"include_ht,omitempty"`
}

const (
	GroupByNone     = "none"
	GroupByMerchant = "merchant"
)

// ---- Accessible establishments (GET /analytics/merchants, PROMPT 24 Phase 1) ----

// AccessibleMerchantsResponse names every establishment in the caller's
// accessible scope (Repository.ResolveAccessibleMerchants — everywhere the
// user holds permission.POSAnalytics via an active users_rights link). The
// frontend needs names, not just the IDs already echoed in every tab's
// Scope.MerchantIDs, to render the multi-establishment selector (PROMPT 24
// Phase 3). Deliberately its own endpoint rather than reusing
// auth.AuthRepository.GetMerchants: that query has no pos.analytics check and
// no enabled/login_enabled filter, so it would leak establishments outside
// this exact scope.
type AccessibleMerchantsResponse struct {
	Merchants []AccessibleMerchant `json:"merchants"`
}

type AccessibleMerchant struct {
	MerchantID string `json:"merchant_id"`
	Name       string `json:"name"`
}

// RevenueResponse never leaks which tables or filters produced it — no field
// name here should let the frontend infer the computation is direct SQL
// rather than a future pre-aggregated table.
type RevenueResponse struct {
	Scope          RevenueScope           `json:"scope"`
	CurrentPeriod  RevenuePeriodTotals    `json:"current_period"`
	PreviousPeriod RevenuePeriodTotals    `json:"previous_period"`
	PreviousYear   RevenuePeriodTotals    `json:"previous_year"`
	Timeline       []RevenueDayPoint      `json:"timeline"`
	ByChannel      []RevenueChannelTotal  `json:"by_channel"`
	ByMerchant     []RevenueMerchantTotal `json:"by_merchant,omitempty"`
	// HTComputed is false when IncludeHT was false — current_period /
	// previous_period / previous_year's TotalHTCents are then nil. Lets the
	// frontend distinguish "HT not requested" from "HT is genuinely zero".
	HTComputed bool `json:"ht_computed"`
}

type RevenueScope struct {
	MerchantIDs []string `json:"merchant_ids"`
	GroupBy     string   `json:"group_by"`
}

type RevenuePeriodTotals struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
	// TotalHTCents is nil when HT wasn't computed for this response — see
	// RevenueResponse.HTComputed. Never a silent 0 standing in for "unknown".
	TotalHTCents *int64 `json:"total_ht_cents,omitempty"`
	OrderCount   int64  `json:"order_count"`
}

// RevenueDayPoint is one point of the CA timeline, in the establishment's
// local calendar day. ByChannel only contains channels that actually had
// revenue that day — AUDIT.md I4 flagged the mock's timeline carrying 3
// series while the chart plots 7 (or vice-versa for Orders); a real
// (channel -> cents) map has no such mismatch, and the frontend does not
// need to know the full channel list to render what's present.
type RevenueDayPoint struct {
	LocalDay          string           `json:"local_day"`
	TotalTTCCents     int64            `json:"total_ttc_cents"`
	ByChannelTTCCents map[string]int64 `json:"by_channel_ttc_cents"`
}

type RevenueChannelTotal struct {
	Channel       string `json:"channel"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
	OrderCount    int64  `json:"order_count"`
}

type RevenueMerchantTotal struct {
	MerchantID    string `json:"merchant_id"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
	OrderCount    int64  `json:"order_count"`
}

// ---- Commandes (POST /analytics/orders) ----

// OrdersRequest mirrors RevenueRequest's shape (DateFrom/DateTo/MerchantIDs/
// GroupBy) — same contract as the CA tab, no IncludeHT (nothing here needs
// the HT recompute). GroupBy was accepted and echoed back in Scope.GroupBy
// since PROMPT 03 but never actually validated or computed until PROMPT 23
// Phase 3 (Repository.GetOrdersByMerchant) — see OrdersResponse.ByMerchant.
type OrdersRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	GroupBy     string   `json:"group_by,omitempty"`
}

type OrdersResponse struct {
	Scope          RevenueScope          `json:"scope"`
	CurrentPeriod  OrdersPeriodTotals    `json:"current_period"`
	PreviousPeriod OrdersPeriodTotals    `json:"previous_period"`
	PreviousYear   OrdersPeriodTotals    `json:"previous_year"`
	Timeline       []OrdersDayPoint      `json:"timeline"`
	ByChannel      []OrdersChannelTotal  `json:"by_channel"`
	ByMerchant     []OrdersMerchantTotal `json:"by_merchant,omitempty"`
}

// OrdersPeriodTotals carries covers as nilable fields, never a silent zero:
// places_settings is unset on 99.9% of PROD orders (PERIMETRE.md), so "0
// covers" and "covers not entered" must stay distinguishable. Both
// TotalCovers and AvgBasketPerCoverCents are nil together, gated by
// CoversDataAvailable — mirrors RevenuePeriodTotals.TotalHTCents/HTComputed.
// CoversDataAvailable requires more than one stray order carrying a value —
// see service.go's coversCoverageThreshold doc comment (a 0.12%-of-orders
// sample, observed on staging's biggest merchant, is noise, not coverage).
type OrdersPeriodTotals struct {
	From                   string `json:"from"`
	To                     string `json:"to"`
	OrderCount             int64  `json:"order_count"`
	AvgBasketTTCCents      int64  `json:"avg_basket_ttc_cents"`
	CoversDataAvailable    bool   `json:"covers_data_available"`
	TotalCovers            *int64 `json:"total_covers,omitempty"`
	AvgBasketPerCoverCents *int64 `json:"avg_basket_per_cover_cents,omitempty"`
}

type OrdersDayPoint struct {
	LocalDay        string           `json:"local_day"`
	TotalOrders     int64            `json:"total_orders"`
	ByChannelOrders map[string]int64 `json:"by_channel_orders"`
}

type OrdersChannelTotal struct {
	Channel    string `json:"channel"`
	OrderCount int64  `json:"order_count"`
}

// OrdersMerchantTotal is one row of OrdersResponse.ByMerchant, populated only
// when group_by=merchant (PROMPT 23 Phase 3) — mirrors RevenueMerchantTotal's
// shape (COUNT/SUM on integer columns, no derived figure), so, unlike a VAT
// per-merchant breakdown would need, these always sum back to the ungrouped
// CurrentPeriod totals exactly, with no apportionment required.
type OrdersMerchantTotal struct {
	MerchantID    string `json:"merchant_id"`
	OrderCount    int64  `json:"order_count"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
}

// ---- Règlements (POST /analytics/payments) ----

type PaymentsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// GroupBy is "none" (cumulé, default) or "merchant" (comparé — one
	// ByMerchant row per establishment). Added in PROMPT 24 Phase 2 — this
	// tab had no grouping at all before (only CA/Commandes did, since PROMPT
	// 03/23).
	GroupBy string `json:"group_by,omitempty"`
}

type PaymentsResponse struct {
	Scope          RevenueScope            `json:"scope"`
	CurrentPeriod  PaymentsPeriodTotals    `json:"current_period"`
	PreviousPeriod PaymentsPeriodTotals    `json:"previous_period"`
	PreviousYear   PaymentsPeriodTotals    `json:"previous_year"`
	Timeline       []PaymentsDayPoint      `json:"timeline"`
	ByMethod       []PaymentMethodTotal    `json:"by_method"`
	ByMerchant     []PaymentsMerchantTotal `json:"by_merchant,omitempty"`
}

type PaymentsPeriodTotals struct {
	From             string `json:"from"`
	To               string `json:"to"`
	TotalAmountCents int64  `json:"total_amount_cents"`
	PaymentCount     int64  `json:"payment_count"`
}

type PaymentsDayPoint struct {
	LocalDay            string           `json:"local_day"`
	TotalAmountCents    int64            `json:"total_amount_cents"`
	ByMethodAmountCents map[string]int64 `json:"by_method_amount_cents"`
}

// PaymentsMerchantTotal is one row of PaymentsResponse.ByMerchant, populated
// only when group_by=merchant (PROMPT 24 Phase 2) — mirrors
// RevenueMerchantTotal/OrdersMerchantTotal's shape (plain COUNT/SUM on
// payments.amount, no derived figure), so, like those, rows sum back to
// CurrentPeriod exactly with no apportionment required. No previous-period/
// year comparison, same posture as those two.
type PaymentsMerchantTotal struct {
	MerchantID       string `json:"merchant_id"`
	TotalAmountCents int64  `json:"total_amount_cents"`
	PaymentCount     int64  `json:"payment_count"`
}

// PaymentMethodTotal.Method is one of the 7 canonical mop values (CB, ES,
// STRIPE, TR, CURRENCY, UBER_EATS, DELIVEROO) or PaymentMethodOther
// (payment_methods.go) — never the maquette's "mobile", which no real mop
// value maps to (DROITS.md/AUDIT.md P14).
type PaymentMethodTotal struct {
	Method           string `json:"method"`
	TotalAmountCents int64  `json:"total_amount_cents"`
	PaymentCount     int64  `json:"payment_count"`
}

// ---- TVA (POST /analytics/vat) ----

type VATRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// GroupBy is "none" (cumulé, default) or "merchant" (comparé). Added in
	// PROMPT 24 Phase 2. In compared mode, ByMerchant's largest-remainder
	// apportionment (apportionVATByRate/apportionVATByChannel) runs PER
	// ESTABLISHMENT, against that establishment's own TotalHTCents — never
	// against the combined scope's total — or an establishment's own by_rate/
	// by_channel parts would fail to sum to its own total, reintroducing
	// exactly the defect PROMPT 06 §1 fixed, just at the establishment level
	// instead of the whole-scope level. See VATMerchantTotal's doc comment.
	GroupBy string `json:"group_by,omitempty"`
}

// VATResponse is the canonical analytics view of VAT across every brand and
// channel — NOT a fiscal document, and deliberately not reconciled with
// pos/reports/tva (different scope: that endpoint restricts to WELLO_RESTO
// and excludes ScanNOrder, under an accounting arbitrage in progress; this
// one answers "how much VAT did this establishment's sales carry," across
// every channel). The frontend must label this distinction — see
// VATAnalyticsTab.tsx.
//
// PROMPT 09 lot 3 (C5): this endpoint now includes delivery fee VAT
// (orders.delivery_fees, via tva_id=-1 — see GetVATTotals' doc comment,
// repository.go), because a restaurateur checking VAT collected expects to
// see it. Before this change, delivery fees were absent from this endpoint
// entirely; pos/reports already included them (its own tva_id=-1 UNION ALL
// branch), which was previously the single component pulling pos/reports'
// total UP rather than down.
//
// The gap with pos/reports/tva is fully named and, for merchant 212 (PROD's
// largest) over a 12-month window (verified read-only against staging,
// 2026-09-04), fully chiffré to the cent:
//   - non-WELLO_RESTO orders (Uber Eats/Deliveroo) — product lines AND their
//     own delivery fees, both excluded by pos/reports
//   - ScanNOrder orders (created_by IN ('-1','SCANNORDER')) within
//     WELLO_RESTO — product lines AND their own delivery fees, both
//     excluded by pos/reports
//   - state='DONE' vs pos/reports' state='CLOSED'-only filter, within
//     WELLO_RESTO/non-ScanNOrder — product lines AND delivery fees (0 on
//     this merchant/window)
//   - tva.show_in_report=false product lines within pos/reports' own
//     WELLO_RESTO/CLOSED/non-ScanNOrder scope (0 on this merchant/window —
//     the only show_in_report=false category, tva_id=-1, never reaches this
//     branch: it has no orderitems rows, only the delivery-fee branch below)
//
// delivery_fees WITHIN pos/reports' own WELLO_RESTO/CLOSED/non-ScanNOrder
// scope is deliberately NOT a separate line in this reconciliation anymore:
// both endpoints now include it (verified: 108,000 cents / 360 orders on
// this merchant/window, identical on both sides), so it nets to zero rather
// than needing to be named. The four buckets above sum exactly to the
// analytics-minus-pos/reports TTC delta — no residual, verified by
// recomputing both totals independently rather than assumed from the
// buckets alone.
type VATResponse struct {
	Scope          RevenueScope       `json:"scope"`
	CurrentPeriod  VATPeriodTotals    `json:"current_period"`
	PreviousPeriod VATPeriodTotals    `json:"previous_period"`
	PreviousYear   VATPeriodTotals    `json:"previous_year"`
	ByRate         []VATRateTotal     `json:"by_rate"`
	ByChannel      []VATChannelTotal  `json:"by_channel"`
	ByMerchant     []VATMerchantTotal `json:"by_merchant,omitempty"`
}

type VATPeriodTotals struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
	TotalHTCents  int64  `json:"total_ht_cents"`
	TotalVATCents int64  `json:"total_vat_cents"`
}

// VATRateTotal.Rate is tva_categories.tva_rate as stored (20, 10, 5.5, ...).
// A delivery fee (tva_id=-1's rate, currently 20%) lands in the same row as
// any product line taxed at that same rate — grouped by rate value, not by
// tva_id — even though tva_id=-1 itself is enabled=false/show_in_report=false
// in the referential; see GetVATTotals' doc comment (repository.go) for why
// that category is joined unconditionally on both flags anyway.
//
// The sum of BaseHTCents (and, since VATCents = TTC-HT and TTC is always
// exact, the sum of VATCents too) across every row here is GUARANTEED to
// equal VATPeriodTotals.TotalHTCents (resp. TotalVATCents) to the cent —
// service.go's apportionVATByRate derives each row's BaseHTCents from the
// group's raw (unrounded) HT sum via a largest-remainder apportionment
// against the already-computed period total (apportion.go), instead of
// letting each bucket round its own sum independently. A fiscal breakdown
// whose lines don't add up to the total next to them reads as a bug to
// whoever's reconciling it, even though a few cents of drift would be normal
// accounting rounding elsewhere (PROMPT 06 §1) — so here it's corrected, not
// documented away.
type VATRateTotal struct {
	Rate        float64 `json:"rate"`
	BaseHTCents int64   `json:"base_ht_cents"`
	VATCents    int64   `json:"vat_cents"`
}

// VATChannelTotal carries the same reconciliation guarantee as
// VATRateTotal — see its doc comment.
type VATChannelTotal struct {
	Channel       string `json:"channel"`
	BaseHTCents   int64  `json:"base_ht_cents"`
	VATCents      int64  `json:"vat_cents"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
}

// VATMerchantTotal is one row of VATResponse.ByMerchant, populated only when
// group_by=merchant (PROMPT 24 Phase 2) — the current period's VAT totals
// AND breakdowns for one establishment. Unlike RevenueMerchantTotal/
// OrdersMerchantTotal (plain COUNT/SUM, nothing to reconcile), VAT's HT is
// DERIVED per line, so ByRate/ByChannel here are apportioned via
// apportionVATByRate/apportionVATByChannel against THIS establishment's own
// TotalHTCents — not the combined scope's. That is the whole point of this
// type existing separately from a bare per-merchant total: an establishment
// selected for comparison must see its own by_rate/by_channel parts sum to
// its own total, exactly like the single-establishment view already
// guarantees for the combined scope (VATRateTotal/VATChannelTotal's doc
// comments).
type VATMerchantTotal struct {
	MerchantID    string            `json:"merchant_id"`
	TotalTTCCents int64             `json:"total_ttc_cents"`
	TotalHTCents  int64             `json:"total_ht_cents"`
	TotalVATCents int64             `json:"total_vat_cents"`
	ByRate        []VATRateTotal    `json:"by_rate"`
	ByChannel     []VATChannelTotal `json:"by_channel"`
}

// ---- Annulations (POST /analytics/cancellations, POST /analytics/cancellations/by-staff) ----
//
// Two endpoints, not one — PROMPT 10 §2: middleware.RequirePermission takes
// exactly one permission.Key (DROITS.md §3.1/§6.1, wello-back-office repo),
// and this tab mixes two different sensitivities: aggregate volume/rate/
// reasons (reports.sales.read, same key as the other 4 tabs) and a nominative
// per-server ranking (reports.staff_performance.read — new, is_sensitive,
// see migrations/todo/115_permission_reports_staff_performance_read.up.sql).
// A single endpoint could not carry both guards at once; splitting the
// route was the only option, not a preference.

// CancellationsRequest mirrors PaymentsRequest/VATRequest's shape. GroupBy
// (PROMPT 24 Phase 2) applies only to this aggregate endpoint — the
// nominative per-server ranking (CancellationsByStaffRequest, a separate
// endpoint/permission below) stays merged regardless of mode, per PROMPT 24's
// explicit decision ("le bloc nominatif reste fusionné quel que soit le
// mode"). No IncludeHT (nothing here needs the HT recompute).
type CancellationsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	GroupBy     string   `json:"group_by,omitempty"`
}

// CancellationsResponse is the aggregate view: volume, rate, amount, reasons,
// channels, author typology. Never the per-server ranking — that is
// CancellationsByStaffResponse, served by a separate, more tightly
// permissioned endpoint (see this file's "Annulations" section header).
//
// None of this response's breakdowns (ByReason/ByAuthorType/ByChannel) need
// apportion.go's largest-remainder apportionment the way VATRateTotal/
// VATChannelTotal do: those exist because HT is DERIVED from TTC via a
// per-line division by a tax rate, so per-group rounding can drift from the
// period total. Every number here — order counts, orders.price cents — is a
// direct, un-derived COUNT/SUM, so a GROUP BY's parts sum to the ungrouped
// total exactly, by ordinary arithmetic, with no separate reconciliation
// step required. PROMPT 10 §3's "toutes les ventilations somment exactement
// à leur total" holds here for free.
type CancellationsResponse struct {
	Scope          RevenueScope                  `json:"scope"`
	CurrentPeriod  CancellationsPeriodTotals     `json:"current_period"`
	PreviousPeriod CancellationsPeriodTotals     `json:"previous_period"`
	PreviousYear   CancellationsPeriodTotals     `json:"previous_year"`
	ByReason       []CancellationReasonTotal     `json:"by_reason"`
	ByAuthorType   []CancellationAuthorTypeTotal `json:"by_author_type"`
	ByChannel      []CancellationChannelTotal    `json:"by_channel"`
	ByMerchant     []CancellationsMerchantTotal  `json:"by_merchant,omitempty"`
}

// CancellationsMerchantTotal is one row of CancellationsResponse.ByMerchant,
// populated only when group_by=merchant (PROMPT 24 Phase 2) — mirrors
// CancellationsPeriodTotals' current-period fields exactly. No
// previous-period/year comparison and no apportionment needed (same posture
// as RevenueMerchantTotal/OrdersMerchantTotal: every field here is a direct
// COUNT/SUM, not derived, so parts sum to CurrentPeriod exactly by ordinary
// arithmetic).
type CancellationsMerchantTotal struct {
	MerchantID             string `json:"merchant_id"`
	TotalOrdersCreated     int64  `json:"total_orders_created"`
	CancelledCount         int64  `json:"cancelled_count"`
	CancelledAmountCents   int64  `json:"cancelled_amount_cents"`
	InternalCancelledCount int64  `json:"internal_cancelled_count"`
	PlatformCancelledCount int64  `json:"platform_cancelled_count"`
	UnknownCancelledCount  int64  `json:"unknown_cancelled_count"`
}

// CancellationsPeriodTotals. TotalOrdersCreated is the cancellation rate's
// denominator — see AnalyticsAllOrdersCreatedScope's doc comment (scope.go)
// for why "every order created in the period," not "cancelled ÷ valid," is
// the definition this tab commits to. The backend deliberately never emits a
// pre-divided rate field: CancelledCount / TotalOrdersCreated is the whole
// computation, and shipping only the two integers means there is no
// separately-rounded percentage to keep consistent with anything else — the
// same reasoning OrdersPeriodTotals already applies by never emitting an
// average that isn't reconstructable from its parts.
//
// InternalCancelledCount (STAFF+CUSTOMER+SYSTEM) and PlatformCancelledCount
// (PLATFORM) are PROMPT 10 §3's central cut: a Uber Eats/Deliveroo-initiated
// cancellation says nothing about this establishment's own operations, and
// blending it into one rate makes the number unreadable as an operational
// signal. UnknownCancelledCount (cancelled_by_type IS NULL, ~7-9% of
// CANCELED orders on PROD as of 2026-09-04 — see cancellations.go's
// GetCancellationsTotals doc comment for the verified figure) is exposed
// explicitly rather than folded into either bucket — never a silent
// exclusion, same rule VATResponse/RevenueDayPoint already apply to their
// own "doesn't fit a known bucket" rows. The three always sum to
// CancelledCount exactly (a three-way COUNT FILTER partition of the same
// rows, not an apportionment).
type CancellationsPeriodTotals struct {
	From               string `json:"from"`
	To                 string `json:"to"`
	TotalOrdersCreated int64  `json:"total_orders_created"`
	CancelledCount     int64  `json:"cancelled_count"`
	// CancelledAmountCents sums orders.price on the cancelled orders in this
	// period — see cancellations.go's GetCancellationsTotals doc comment for
	// why this number is shown (distribution checked against staging, not
	// mostly-zero carts) with the caveat the frontend must carry: it is the
	// ticket price recorded on the order at cancellation time, not a
	// verified "money that would otherwise have been collected" figure.
	CancelledAmountCents   int64 `json:"cancelled_amount_cents"`
	InternalCancelledCount int64 `json:"internal_cancelled_count"`
	PlatformCancelledCount int64 `json:"platform_cancelled_count"`
	UnknownCancelledCount  int64 `json:"unknown_cancelled_count"`
}

// CancellationReasonTotal.ReasonID is an identifier, never a label — PROMPT
// 10 §5's audit finding on the maquette (English slugs filtered against
// French labels, so the filter structurally never matched) applies equally
// to "filter on a label that can be renamed": a renamed motif must not
// mutate past analytics. ReasonID is one of:
//   - the numeric deletion_reasons.deletion_reason_id as a string, when
//     orders.deletion_reason_id matched a real catalog row;
//   - "uncatalogued:<raw value>" when deletion_reason_id carries a value
//     that doesn't match any catalog row — e.g. a varchar(11)-truncated
//     code like "KIOSM_CUSTO" (see cancellations.go's GetCancellationsByReason
//     doc comment) or a stray quoted literal ("'3'") from a second, distinct
//     write-path bug found while building this tab;
//   - "none" when deletion_reason_id is NULL or blank.
//
// Never dropped silently — the last two cases are PROMPT 10 §3's "jamais
// être exclues en silence" rule, applied to reasons the way the brief
// already applies it to cancelled_by_type.
type CancellationReasonTotal struct {
	ReasonID string `json:"reason_id"`
	Label    string `json:"label"`
	Count    int64  `json:"count"`
}

// CancellationAuthorTypeTotal.AuthorType is one of the raw orders.
// cancelled_by_type values (STAFF, CUSTOMER, SYSTEM, PLATFORM) or
// CancellationAuthorUnknown for NULL — see cancellations.go.
type CancellationAuthorTypeTotal struct {
	AuthorType  string `json:"author_type"`
	Count       int64  `json:"count"`
	AmountCents int64  `json:"amount_cents"`
}

// CancellationChannelTotal reuses channelCaseExpr (channels.go), the same
// channel derivation every other tab in this package uses.
type CancellationChannelTotal struct {
	Channel     string `json:"channel"`
	Count       int64  `json:"count"`
	AmountCents int64  `json:"amount_cents"`
}

// ---- Annulations — bloc nominatif (POST /analytics/cancellations/by-staff) ----

// CancellationsByStaffRequest mirrors CancellationsRequest.
type CancellationsByStaffRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
}

// CancellationsByStaffResponse carries no period comparison (previous
// period/year) — a nominative ranking is read as "who, this period," not as
// a trend the way the aggregate KPIs are; adding two more rankings the
// frontend almost certainly wouldn't render was not worth the extra queries
// against a fusible-protected pool.
//
// MinOrdersForRate is served in the contract, not hardcoded on the frontend,
// so the threshold can change here without a client redeploy — same reason
// RevenueRequest.IncludeHT is a request-visible flag rather than a bare
// server constant.
type CancellationsByStaffResponse struct {
	Scope            RevenueScope           `json:"scope"`
	From             string                 `json:"from"`
	To               string                 `json:"to"`
	MinOrdersForRate int64                  `json:"min_orders_for_rate"`
	Staff            []StaffCancellationRow `json:"staff"`
}

// StaffCancellationRow.OrdersCreated is the "effectif" PROMPT 10 §4 requires
// before a rate is meaningful — every order UserID created in the period
// (any brand_status/state), not just their cancellations. RateAvailable is
// false whenever OrdersCreated < MinOrdersForRate: the frontend must then
// render CancelledCount and OrdersCreated as raw numbers ("4 annulations sur
// 62 commandes"), never a computed percentage — mirrors
// OrdersPeriodTotals.CoversDataAvailable's nilable-gate pattern already in
// this package (service.go's coversCoverageThreshold), applied here to
// protect a named person from a ratio computed on a handful of orders
// (PROMPT 10 §4: "un taux d'annulation calculé sur 3 commandes n'est pas un
// indicateur, c'est du bruit — et présenté dans un classement nominatif,
// c'est un bruit qui désigne quelqu'un").
//
// UserID is CancellationUnattributedUserID for the one synthetic row that
// carries every STAFF-type cancellation whose orders.created_by does not
// match a real users.user_id (~11% of STAFF cancellations on PROD as of
// 2026-09-04 — see cancellations.go's GetCancellationsByStaff doc comment).
// That row always has OrdersCreated 0 and RateAvailable false — there is no
// effectif to divide by for an unidentified author — and exists so
// SUM(CancelledCount) across this endpoint reconciles exactly to the
// aggregate endpoint's ByAuthorType STAFF row (PROMPT 10 §6's cross-endpoint
// coherence check), instead of quietly dropping the unattributable share.
type StaffCancellationRow struct {
	UserID         string `json:"user_id"`
	Name           string `json:"name"`
	OrdersCreated  int64  `json:"orders_created"`
	CancelledCount int64  `json:"cancelled_count"`
	RateAvailable  bool   `json:"rate_available"`
}

// ---- Produits (POST /analytics/products) ----
//
// PROMPT 16: the sixth direct-SQL analytics tab. QuantitySold/RevenueTTCCents/
// RevenueHTCents are as reliable as every other tab's numbers (same
// AnalyticsOrdersScope, same htLineExpr HT recompute as CA/TVA). Cost and
// margin are NOT: they come from orderitems.cost_price_unit (PROMPT 07 lot
// 1), NULL on the large majority of lines today — NULL because the product
// has no recipe at all (costing.ReasonNoRecipe, the normal case) or because
// its recipe is missing a purchase price somewhere (costing.
// ReasonIncompleteRecipe, a setup defect worth surfacing) — never a silent 0,
// which would read as "free" and therefore "100% margin".
//
// The single rule this file's cost/margin fields all obey (PROMPT 16's own
// name for it: "le piège de l'agrégation partielle") is that a margin is
// NEVER a complete revenue sum divided by a partial cost sum. Both ProductRow
// and the aggregate ProductsCostCoverage restrict a margin's numerator AND
// denominator to exactly the same subset of lines — those carrying a known
// cost_price_unit — and separately expose how much of the revenue that
// subset represents, so a caller can never print the margin number without
// also having the coverage figure it depends on.

// ProductsRequest is the payload for POST /analytics/products. Unlike the
// other five tabs, this one is paginated and server-sorted: PROMPT 16 §3
// singles out the maquette's hardcoded "10 rows, client-side sort" as a
// defect not to reproduce, so paging and ordering are both request fields
// resolved in SQL, never truncated/sorted after the fact by the caller.
type ProductsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// CategoryID filters to one productcateg.merchant_categ_id, validated by
	// the service against GetProductCategories's own result for this scope —
	// never trusted blindly. Empty means every category.
	CategoryID string `json:"category_id,omitempty"`
	// SortBy is one of ProductsSortQuantity (default), ProductsSortRevenue,
	// ProductsSortMargin.
	SortBy string `json:"sort_by,omitempty"`
	// SortDir is "desc" (default) or "asc".
	SortDir string `json:"sort_dir,omitempty"`
	// Page is 1-based; defaults to 1.
	Page int `json:"page,omitempty"`
	// PageSize defaults to ProductsDefaultPageSize, capped at ProductsMaxPageSize.
	PageSize int `json:"page_size,omitempty"`
}

const (
	ProductsSortQuantity = "quantity"
	ProductsSortRevenue  = "revenue_ttc"
	ProductsSortMargin   = "margin"

	ProductsDefaultPageSize = 50
	ProductsMaxPageSize     = 200
)

// ProductCategoryOption is one entry of ProductsResponse.AvailableCategories —
// read from productcateg for the accessible scope, never the maquette's
// hardcoded entrees/plats/desserts/boissons list (PROMPT 16 §3).
type ProductCategoryOption struct {
	CategoryID string `json:"category_id"`
	Name       string `json:"name"`
}

// ProductsPeriodTotals mirrors every other tab's period-totals shape — see
// e.g. RevenuePeriodTotals. No previous-year figure here: PROMPT 16 asks for
// "évolution par rapport à la période précédente" only, not a 3-period
// comparison like the other five tabs.
type ProductsPeriodTotals struct {
	From            string `json:"from"`
	To              string `json:"to"`
	QuantitySold    int64  `json:"quantity_sold"`
	RevenueTTCCents int64  `json:"revenue_ttc_cents"`
	RevenueHTCents  int64  `json:"revenue_ht_cents"`
}

// ProductsCostCoverage is the tab's aggregate margin block. MarginCents/
// MarginPercent are nil whenever CoverageRatio falls below
// coversCoverageThreshold (service.go) — the same 20% bar OrdersPeriodTotals.
// CoversDataAvailable already applies, reused here verbatim per PROMPT 16's
// explicit instruction to apply "le seuil de matérialité déjà retenu
// ailleurs" rather than invent a new one. Below that bar there is no
// aggregate margin printed anywhere in this response — only
// RevenueTTCCentsCovered/RevenueTTCCentsTotal/CoverageRatio, so the caller can
// still say "marge connue sur X% du CA" instead of showing nothing at all.
type ProductsCostCoverage struct {
	RevenueTTCCentsTotal     int64    `json:"revenue_ttc_cents_total"`
	RevenueTTCCentsCovered   int64    `json:"revenue_ttc_cents_covered"`
	CoverageRatio            float64  `json:"coverage_ratio"`
	MarginCents              *int64   `json:"margin_cents,omitempty"`
	MarginPercent            *float64 `json:"margin_percent,omitempty"`
	NoRecipeQuantity         int64    `json:"no_recipe_quantity"`
	IncompleteRecipeQuantity int64    `json:"incomplete_recipe_quantity"`
}

// ProductRow is one line of the paginated table. CostPriceCents/MarginCents/
// MarginPercent are nil whenever CostKnownQuantity is 0 — every line sold for
// this product in the period was NO_RECIPE and/or INCOMPLETE_RECIPE. When
// non-nil, they are computed over exactly the CostKnownQuantity/
// CostKnownRevenueTTCCents subset, never over QuantitySold/RevenueTTCCents —
// the partial-aggregation rule applied at row granularity, not just in the
// aggregate. CostKnownQuantity is always present alongside them so a
// partially-costed product (some lines priced, some not — a recipe completed
// mid-period) is never confused with a fully-costed one.
type ProductRow struct {
	ProductID       string `json:"product_id"`
	Name            string `json:"name"`
	CategoryID      string `json:"category_id"`
	CategoryName    string `json:"category_name"`
	QuantitySold    int64  `json:"quantity_sold"`
	RevenueTTCCents int64  `json:"revenue_ttc_cents"`
	RevenueHTCents  int64  `json:"revenue_ht_cents"`

	CostKnownQuantity        int64    `json:"cost_known_quantity"`
	CostKnownRevenueTTCCents int64    `json:"cost_known_revenue_ttc_cents"`
	CostPriceCents           *int64   `json:"cost_price_cents,omitempty"`
	MarginCents              *int64   `json:"margin_cents,omitempty"`
	MarginPercent            *float64 `json:"margin_percent,omitempty"`
	NoRecipeQuantity         int64    `json:"no_recipe_quantity"`
	IncompleteRecipeQuantity int64    `json:"incomplete_recipe_quantity"`

	// EvolutionPercent compares RevenueTTCCents to the previous period's
	// revenue for this SAME product_id — nil when the product had no
	// previous-period sales at all (a new or newly-popular product; never a
	// divide-by-zero masquerading as +∞% or a meaningless -100%).
	EvolutionPercent *float64 `json:"evolution_percent,omitempty"`
}

// ProductsResponse. CategoryID/SortBy/SortDir echo back the applied
// (defaulted/validated) request — never just what the client sent, since a
// blank/invalid value is resolved server-side (see service.go).
type ProductsResponse struct {
	Scope               RevenueScope              `json:"scope"`
	CategoryID          string                    `json:"category_id"`
	SortBy              string                    `json:"sort_by"`
	SortDir             string                    `json:"sort_dir"`
	CurrentPeriod       ProductsPeriodTotals       `json:"current_period"`
	PreviousPeriod      ProductsPeriodTotals       `json:"previous_period"`
	CostCoverage        ProductsCostCoverage       `json:"cost_coverage"`
	AvailableCategories []ProductCategoryOption    `json:"available_categories"`
	Pagination          models.PaginationMetadata  `json:"pagination"`
	Rows                []ProductRow               `json:"rows"`
}

// ---- Options (POST /analytics/options) ----
//
// PROMPT 17: same template as Produits (PROMPT 16) for cost/margin — see
// ProductRow's doc comment for the partial-aggregation rule reused here
// verbatim. Two structurally different sources feed one contract: selected
// configurable options (order_item_configuration + configurable_attribute_
// options, split into "paid"/"free" by extra_price) and ingredient removals
// (the `without` table, always "removed") — see options.go's doc comment.
//
// Amounts are integer cents everywhere (*_cents), never euros — PROMPT 17 §3
// flags this tab's old mock as the one place in the whole maquette that
// documented amounts in centimes while rendering them as euros with no
// conversion. This contract has no such split: every amount field name ends
// in _cents and is formatted at render, same convention as every other tab.
type OptionsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// OptionTypes filters to a subset of paid/free/removed. Empty means all
	// three — the same default the frontend's filter UI starts with. Unlike
	// the mock this replaces, this filter is actually applied in SQL (see
	// options.go's optionTypesFilter) rather than accepted and ignored.
	OptionTypes []string `json:"option_types,omitempty"`
	// SortBy is one of OptionsSortQuantity (default), OptionsSortRevenue,
	// OptionsSortMargin.
	SortBy string `json:"sort_by,omitempty"`
	// SortDir is "desc" (default) or "asc".
	SortDir string `json:"sort_dir,omitempty"`
	// Page is 1-based; defaults to 1.
	Page int `json:"page,omitempty"`
	// PageSize defaults to OptionsDefaultPageSize, capped at OptionsMaxPageSize.
	PageSize int `json:"page_size,omitempty"`
}

const (
	OptionTypePaid    = "paid"
	OptionTypeFree    = "free"
	OptionTypeRemoved = "removed"

	OptionsSortQuantity = "quantity"
	OptionsSortRevenue  = "revenue_ttc"
	OptionsSortMargin   = "margin"

	OptionsDefaultPageSize = 50
	OptionsMaxPageSize     = 200
)

// OptionsPeriodTotals mirrors ProductsPeriodTotals's shape, for the subset of
// rows matching the request's OptionTypes filter.
type OptionsPeriodTotals struct {
	From string `json:"from"`
	To   string `json:"to"`
	// QuantitySold is the CA-driving instance count (order_item_configuration.
	// quantity × orderitems.quantity for options, orderitems.quantity for
	// removed ingredients) — NOT the same denominator adoption rates use at
	// row level. See OptionRow's doc comment.
	QuantitySold    int64 `json:"quantity_sold"`
	RevenueTTCCents int64 `json:"revenue_ttc_cents"`
}

// OptionsCostCoverage mirrors ProductsCostCoverage's shape and materiality
// gate (coversCoverageThreshold, reused verbatim — service.go) — see its doc
// comment for why margin is only ever printed over the cost-known subset,
// with the coverage ratio always shown alongside it.
type OptionsCostCoverage struct {
	RevenueTTCCentsTotal     int64    `json:"revenue_ttc_cents_total"`
	RevenueTTCCentsCovered   int64    `json:"revenue_ttc_cents_covered"`
	CoverageRatio            float64  `json:"coverage_ratio"`
	MarginCents              *int64   `json:"margin_cents,omitempty"`
	MarginPercent            *float64 `json:"margin_percent,omitempty"`
	NoRecipeQuantity         int64    `json:"no_recipe_quantity"`
	IncompleteRecipeQuantity int64    `json:"incomplete_recipe_quantity"`
}

// OptionRow is one line of the paginated table — grain is (option or removed
// ingredient, product it was sold with), since a shared attribute group
// (product_configurable_attribute) can attach the same option to more than
// one product; each row reports adoption against its own product only.
//
// QuantitySold and UnitsWithThis are DIFFERENT numbers on purpose — see
// options.go's doc comment. QuantitySold is the volume/CA figure (how many
// times this was added, options.go's "units"). UnitsWithThis is the adoption
// numerator: how many of this PRODUCT's own units (ProductUnitsSold, the same
// definition ProductRow.QuantitySold uses for this product) included this at
// least once. AdoptionRate = UnitsWithThis / ProductUnitsSold, nil whenever
// ProductUnitsSold is 0 — never a division by zero, per PROMPT 17's
// verification requirement.
//
// CostPriceCents/MarginCents/MarginPercent are nil whenever CostKnownQuantity
// is 0 — for OptionType "removed" this is ALWAYS the case: the `without`
// table carries no cost snapshot at all (no cost_price_unit column exists for
// it), so cost is structurally not applicable, not merely unresolved — still
// rendered as nil/"—", never a fabricated 0. For OptionType "paid"/"free",
// nil means every selection of this option in the period was NO_RECIPE
// and/or INCOMPLETE_RECIPE — see NoRecipeQuantity/IncompleteRecipeQuantity.
//
// RevenueTTCCents is 0 (a real, deterministic value, never nil) for "free"
// and "removed" rows: a free modification and an ingredient removal
// structurally never generate CA.
//
// BasketImpactCents is nil when either side of the comparison (orders with
// vs. without this entity, within the same scope) is empty — see
// options.go's GetOptionsBasketShares doc comment.
type OptionRow struct {
	EntityID      string `json:"entity_id"`
	Name          string `json:"name"`
	AttributeName string `json:"attribute_name,omitempty"`
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	OptionType    string `json:"option_type"`

	QuantitySold int64 `json:"quantity_sold"`

	ProductUnitsSold int64    `json:"product_units_sold"`
	UnitsWithThis    int64    `json:"units_with_this"`
	AdoptionRate     *float64 `json:"adoption_rate,omitempty"`

	RevenueTTCCents int64 `json:"revenue_ttc_cents"`

	BasketImpactCents *int64 `json:"basket_impact_cents,omitempty"`

	CostKnownQuantity        int64    `json:"cost_known_quantity"`
	CostKnownRevenueTTCCents int64    `json:"cost_known_revenue_ttc_cents"`
	CostPriceCents           *int64   `json:"cost_price_cents,omitempty"`
	MarginCents              *int64   `json:"margin_cents,omitempty"`
	MarginPercent            *float64 `json:"margin_percent,omitempty"`
	NoRecipeQuantity         int64    `json:"no_recipe_quantity"`
	IncompleteRecipeQuantity int64    `json:"incomplete_recipe_quantity"`
}

// OptionsResponse. OptionTypes/SortBy/SortDir echo back the applied
// (defaulted/validated) request, same convention as ProductsResponse.
type OptionsResponse struct {
	Scope          RevenueScope              `json:"scope"`
	OptionTypes    []string                  `json:"option_types"`
	SortBy         string                    `json:"sort_by"`
	SortDir        string                    `json:"sort_dir"`
	CurrentPeriod  OptionsPeriodTotals       `json:"current_period"`
	PreviousPeriod OptionsPeriodTotals       `json:"previous_period"`
	CostCoverage   OptionsCostCoverage       `json:"cost_coverage"`
	Pagination     models.PaginationMetadata `json:"pagination"`
	Rows           []OptionRow               `json:"rows"`
}

// ---- Clients (POST /analytics/clients, POST /analytics/clients/top) ----
//
// PROMPT 18. Two endpoints, not one — same reason as Annulations (PROMPT 10,
// see cancellations.go's package doc comment): middleware.RequirePermission
// takes exactly one permission.Key, and this tab mixes an aggregate view
// (new customers, recurring rate, segments, frequency —
// permission.ReportsSalesRead, same door as the other six tabs) with a
// nominative ranking (name, lifetime value, last visit, avg basket —
// permission.CustomersManage, is_sensitive). A 403 on the second endpoint
// must hide that block on the frontend, never break the rest of the tab.
//
// See clients.go's doc comment for the three calculation traps this tab is
// built to avoid (never reading customer.customer_nb_orders/
// customer_total_spent/last_order_date; "nouveau" and "valeur vie" computed
// over the customer's WHOLE history, never truncated to the requested
// period; customer.creation_date never used as a first-order date).

// ClientsRequest is the payload for POST /analytics/clients.
type ClientsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// Channels filters to a subset of the canonical channel keys (channels.go).
	// Empty means every channel. PROMPT 18 §1: this is the tab's central
	// control, not a comfort filter — without it, a "best customers" ranking
	// would be overwhelmingly Uber Eats/Deliveroo accounts, since 86% of
	// direct (Wello Resto) orders carry no identified customer while
	// marketplace orders almost always do. This is this package's first
	// channel INPUT filter — see ChannelFilter's doc comment (channels.go).
	Channels []string `json:"channels,omitempty"`
}

// ClientsCoverage is PROMPT 18 §1's mandatory disclosure: whatever channel
// filter is applied, the screen must always say what share of the period's
// orders this analysis actually covers — a nominative "top clients" list or
// a segment breakdown built on 14% of orders is useful only if that 14% is
// stated next to it, never implied to be the whole customer base.
type ClientsCoverage struct {
	OrdersWithCustomerID int64   `json:"orders_with_customer_id"`
	TotalOrders          int64   `json:"total_orders"`
	CoverageRatio        float64 `json:"coverage_ratio"`
}

// ClientsDefinitions states, in the contract itself, which of several
// defensible readings this response committed to for each ambiguous term
// PROMPT 18 §4 lists — so the frontend can (and should) surface the exact
// wording next to the number it describes, rather than the frontend or a
// future reader having to guess which interpretation was chosen. See
// service.go's computeClientsSegments/newCustomersWindow for where each of
// these is actually implemented.
type ClientsDefinitions struct {
	Recurrence string `json:"recurrence"`
	Segments   string `json:"segments"`
	Frequency  string `json:"frequency"`
	Inactivity string `json:"inactivity"`
}

// ClientsSegmentCount is one row of the nouveau/récurrent/fidèle/inactif/
// dormant partition — see ClientsSegment* constants (service.go) and
// computeClientsSegments's doc comment for exactly how a customer lands in
// one bucket. AvgBasketTTCCents is nil for a segment with zero orders in the
// period (inactif and dormant, by construction — an inactive customer placed
// no order in the window, so there is no basket to average).
type ClientsSegmentCount struct {
	Segment           string `json:"segment"`
	Count             int64  `json:"count"`
	AvgBasketTTCCents *int64 `json:"avg_basket_ttc_cents,omitempty"`
}

// ClientsResponse. SegmentRatesAvailable/MinCustomersForRate/RecurringRate
// all gate on the SAME threshold and the SAME denominator
// (IdentifiedCustomersInPeriod) — PROMPT 18 §6 asks for "le seuil déjà en
// usage sur les autres onglets," reused here verbatim from Annulations'
// staffCancellationMinOrders (cancellations.go) rather than a new number:
// below it, every rate in this response is nil and only absolute counts are
// meant to be shown, with IdentifiedCustomersInPeriod always visible as the
// reference volume next to them (never a rate alone).
type ClientsResponse struct {
	Scope       RevenueScope       `json:"scope"`
	Channels    []string           `json:"channels"`
	From        string             `json:"from"`
	To          string             `json:"to"`
	Coverage    ClientsCoverage    `json:"coverage"`
	Definitions ClientsDefinitions `json:"definitions"`

	// NoIdentifiedCustomers is true when this merchant/channel/period
	// combination has zero orders carrying a customer_id — every other
	// numeric field is then a real, honest 0, not a computed-on-nothing
	// artifact, and the frontend must render a clear message instead of an
	// empty chart (PROMPT 18's Vérification requirement).
	NoIdentifiedCustomers bool `json:"no_identified_customers"`

	IdentifiedCustomersInPeriod int64 `json:"identified_customers_in_period"`
	// NewCustomersCount is PROMPT 18 §3's central figure: customers whose
	// FIRST ORDER EVER falls inside [DateFrom, DateTo] — never customers
	// whose first order IN THIS WINDOW was found by restricting the MIN() to
	// the window itself (that reading is systematically wrong, worse the
	// shorter the window — see clients.go's doc comment).
	NewCustomersCount int64 `json:"new_customers_count"`

	RecurringCount      int64    `json:"recurring_count"`
	RecurringRate       *float64 `json:"recurring_rate,omitempty"`
	MinCustomersForRate int64    `json:"min_customers_for_rate"`

	// AvgOrdersPerActiveCustomer is this response's chosen reading of
	// "fréquence d'achat" — see ClientsDefinitions.Frequency.
	AvgOrdersPerActiveCustomer float64 `json:"avg_orders_per_active_customer"`

	SegmentRatesAvailable bool                  `json:"segment_rates_available"`
	Segments              []ClientsSegmentCount `json:"segments"`
}

// ---- Clients — bloc nominatif (POST /analytics/clients/top) ----

// ClientsTopRequest mirrors ClientsRequest's shape — same date/scope/channel
// contract, so the two calls the frontend makes for this tab (see
// CancellationsAnalyticsTab.tsx's precedent) always describe the same
// analysis.
type ClientsTopRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	Channels    []string `json:"channels,omitempty"`
}

// ClientsTopLimit bounds the nominative ranking to the top N customers by
// lifetime value — a "Top clients" leaderboard, not a paginated directory of
// every identified customer (which GetCustomersLifetimeStats already builds
// server-side for the aggregate endpoint, but which this endpoint does not
// expose in full: only PROMPT 18's four nominative fields, for the top
// ClientsTopLimit).
const ClientsTopLimit = 20

// ClientRow is one line of the Top Clients table. LifetimeValueCents and
// AvgBasketTTCCents are both computed over the customer's WHOLE history
// (never just the requested period) — same "depuis toujours" reading as
// NewCustomersCount, since a leaderboard ranked by lifetime value would be
// incoherent if its own average basket were period-bounded. AvgBasketTTCCents
// is LifetimeValueCents / LifetimeOrders, both already guaranteed non-zero
// denominators for any row that appears here (a row exists only because the
// customer placed at least one qualifying order ever).
type ClientRow struct {
	CustomerID         string `json:"customer_id"`
	Name               string `json:"name"`
	LifetimeValueCents int64  `json:"lifetime_value_cents"`
	LifetimeOrders     int64  `json:"lifetime_orders"`
	LastOrderDate      string `json:"last_order_date"`
	AvgBasketTTCCents  int64  `json:"avg_basket_ttc_cents"`
}

// ClientsTopResponse. IdentifiedCustomersInPeriod is repeated from
// ClientsResponse (not just "how many rows below") so this endpoint's own
// reference volume is visible even if a caller only ever hits this route —
// same reasoning CancellationsByStaffResponse.MinOrdersForRate is served in
// the contract rather than assumed known from the other endpoint.
type ClientsTopResponse struct {
	Scope                       RevenueScope `json:"scope"`
	Channels                    []string     `json:"channels"`
	From                        string       `json:"from"`
	To                          string       `json:"to"`
	IdentifiedCustomersInPeriod int64        `json:"identified_customers_in_period"`
	TopClients                  []ClientRow  `json:"top_clients"`
}

// ---- Vente additionnelle (POST /analytics/upsell, POST /analytics/upsell/by-staff) ----
//
// PROMPT 19. Replaces the old GET /stats/upsell (internal/modules/stats,
// guarded by permission.POSAnalytics alone, front-end page gate only — see
// routes.go) with this package's template: direct SQL, unified contract,
// channel filter, staff ranking split into its own permission (same split as
// Annulations/Clients — see cancellations.go's package doc comment). The SQL
// itself (upsellLineHTExpr, the orderitems/orders/products/tva_categories/
// extra join) is carried over from stats.StatsRepository verbatim — the
// traceability audit (docs/audits/audit_upsell_traceability.md, §G) called it
// a correctly-wired reference query; see upsell.go's doc comment for exactly
// what changed structurally and what did not.
//
// The central fact this whole tab is built around: orderitems.is_upsell is
// false on every row in this system — 0 of 77,454 lines, verified against
// staging 2026-09-05 — not because no upsell happens, but because only the
// POS channel actually writes true. Kiosk and ScanNOrder both have working
// upsell UIs and both already link upsell_suggestions to the order
// (suggestion_id transmitted, Tracker fires), but neither serializes
// is_upsell on the order item itself yet (the traceability audit's gaps 1/2
// — a client-side fix, deliberately a separate, later lot: this PROMPT does
// not touch any is_upsell write path). Every field below DERIVED FROM
// orderitems.is_upsell — CurrentPeriod/PreviousPeriod, and
// UpsellByStaffResponse.Staff — is gated by InstrumentationActive: false
// today, meaning the screen must show a "data not collected" message in
// place of those numbers, never a bare 0. See
// GetUpsellInstrumentationActive's doc comment (upsell.go) for the exact
// detection rule, which flips automatically, with no redeploy, the day any
// channel starts writing true.
//
// Suggestions (the transformation-rate block) is the one exception: it reads
// upsell_suggestions, a completely different write path
// (upsell.Tracker.TrackAsync/RecordAcceptance, fires whenever an order
// references a suggestion_id, independent of is_upsell) that already works
// on every channel today — verified against staging 2026-09-05: 313
// suggestions system-wide, 287 of them on the test establishment (merchant
// 2), over the last 4 months. It is shown regardless of
// InstrumentationActive — see UpsellSuggestionsTotals' doc comment.
type UpsellRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// Channels filters the orderitems-derived numbers only (CurrentPeriod/
	// PreviousPeriod and, on the nominative endpoint, Staff) — never
	// Suggestions, which reads upsell_suggestions.channel (POS/SNO/KIOSK, a
	// different taxonomy from the dine_in/takeaway/... keys this filter
	// validates against, see channels.go) and carries no per-channel
	// breakdown in this contract. Empty means every channel.
	Channels []string `json:"channels,omitempty"`
}

// UpsellResponse is the aggregate view (permission.ReportsSalesRead). Never
// the nominative per-server ranking — that is UpsellByStaffResponse, served
// by a separate, more tightly permissioned endpoint.
type UpsellResponse struct {
	Scope    RevenueScope `json:"scope"`
	Channels []string     `json:"channels"`

	// InstrumentationActive gates CurrentPeriod/PreviousPeriod: false means
	// "pas de donnée collectée," never "zéro vente additionnelle" — see this
	// section's package doc comment and GetUpsellInstrumentationActive
	// (upsell.go). The frontend's primary message on this tab as long as this
	// is false must say so explicitly, in place of the KPI tiles — not a
	// footnote next to a row of zeros.
	InstrumentationActive bool `json:"instrumentation_active"`

	CurrentPeriod  UpsellPeriodTotals `json:"current_period"`
	PreviousPeriod UpsellPeriodTotals `json:"previous_period"`

	Suggestions UpsellSuggestionsTotals `json:"suggestions"`
}

// UpsellPeriodTotals.TotalOrdersCount is the rate's denominator, stated
// explicitly rather than left implicit (PROMPT 19 §4's requirement): every
// order in this package's canonical AnalyticsOrdersScope (state CLOSED/DONE,
// brand_status not DELETED/CANCELED — the same scope CA/Commandes/TVA use),
// restricted to the requested channel filter. OrdersWithUpsellCount /
// TotalOrdersCount is the whole computation the frontend needs for "taux de
// commandes avec au moins un upsell" — no pre-divided rate is shipped, same
// convention as CancellationsPeriodTotals. UpsellLines/
// UpsellRevenueHTCents/OrdersWithUpsellCount are always 0 today and
// meaningless until UpsellResponse.InstrumentationActive is true — the
// frontend must not render them as real zeros before that; TotalOrdersCount
// alone is real regardless (it does not depend on is_upsell at all).
type UpsellPeriodTotals struct {
	From                  string `json:"from"`
	To                    string `json:"to"`
	UpsellLines           int64  `json:"upsell_lines"`
	UpsellRevenueHTCents  int64  `json:"upsell_revenue_ht_cents"`
	OrdersWithUpsellCount int64  `json:"orders_with_upsell_count"`
	TotalOrdersCount      int64  `json:"total_orders_count"`
}

// UpsellSuggestionsTotals reads upsell_suggestions directly. ProposedCount is
// every suggestion generated in the period (COUNT(*), created_at-scoped);
// AcceptedCount is the subset carrying accepted_items IS NOT NULL, written by
// upsell.Tracker.RecordAcceptance only when an order was actually created
// referencing that suggestion's id (internal/modules/upsell/tracker.go) — the
// transformation rate PROMPT 19 §4 asks for, and the one metric in this
// response that is NOT gated by UpsellResponse.InstrumentationActive: it is a
// genuinely working, non-zero signal today (287 proposed / 1 accepted on the
// test establishment over the last 4 months, verified against staging
// 2026-09-05), entirely independent of orderitems.is_upsell.
//
// TransformationRateAvailable gates the rate on the same materiality bar
// every other per-something rate in this package uses
// (staffCancellationMinOrders, aliased here as upsellSuggestionsMinProposed —
// upsell.go) — below it only ProposedCount/AcceptedCount are shown, never a
// computed percentage, same nilable-rate convention as
// StaffCancellationRow.RateAvailable. staff_member_id (migration 044) is
// never read here: it exists in the schema but is written nowhere in the
// codebase (traceability audit §B) — there is no per-server attribution to
// build on this table yet.
type UpsellSuggestionsTotals struct {
	From                        string `json:"from"`
	To                          string `json:"to"`
	ProposedCount               int64  `json:"proposed_count"`
	AcceptedCount               int64  `json:"accepted_count"`
	TransformationRateAvailable bool   `json:"transformation_rate_available"`
	MinProposedForRate          int64  `json:"min_proposed_for_rate"`
}

// ---- Vente additionnelle — bloc nominatif (POST /analytics/upsell/by-staff) ----

// UpsellByStaffRequest mirrors UpsellRequest's shape.
type UpsellByStaffRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	Channels    []string `json:"channels,omitempty"`
}

// UpsellByStaffResponse carries no rate — CA upsell par serveur is a
// leaderboard of raw lines/revenue, same shape the old stats.
// UpsellServerStat already used; there is no per-server "taux" in this
// contract to gate on an effectif threshold (unlike Annulations/Clients).
// InstrumentationActive mirrors UpsellResponse's own field: Staff is always
// [] today, and the screen must say why, never render a silently-empty
// table.
type UpsellByStaffResponse struct {
	Scope                 RevenueScope     `json:"scope"`
	Channels              []string         `json:"channels"`
	From                  string           `json:"from"`
	To                    string           `json:"to"`
	InstrumentationActive bool             `json:"instrumentation_active"`
	Staff                 []UpsellStaffRow `json:"staff"`
}

// UpsellStaffRow mirrors the old stats.UpsellServerStat field-for-field
// (server_id/server_name renamed user_id/name for consistency with this
// package's own StaffCancellationRow/ClientRow naming).
type UpsellStaffRow struct {
	UserID               string `json:"user_id"`
	Name                 string `json:"name"`
	UpsellLines          int64  `json:"upsell_lines"`
	UpsellRevenueHTCents int64  `json:"upsell_revenue_ht_cents"`
}

// ---- Remises (POST /analytics/discounts) ----
//
// PROMPT 22. Reads discount_redemptions exclusively (PROMPT 21's table de
// liaison commande×remise) — the maquette this replaces described a product
// that never existed, on three points corrected here rather than reproduced:
//
//   - No discount_type. Promotion/Happy Hour/Geste commercial/Fidélité/Codes
//     promo have no column anywhere — grouping is by discount_id
//     (discounts.discount_id_new) alone, never by discount_name/
//     discount_code, so renaming a discount can never split or merge its
//     history (DiscountRow's own doc comment).
//   - No cart-discount section. applyCartDiscount does not exist anywhere in
//     this codebase (PROMPT 21's own audit) — not a broken write path, a
//     feature never built. This response has nothing "panier"-shaped, not
//     even an empty one (an empty section would read as "zero cart
//     discounts," which is a different, false claim).
//   - Margin impact is at the contract, null in practice: see
//     DiscountsMarginCoverage's doc comment.
//
// The other fact every number here must respect: the 545 rows PROMPT 21
// migration 119 reconstructed from historical base_price/price mismatches
// are a FLOOR, not a total (they only catch discounts still detectable after
// the fact), and discount_redemptions has carried live, complete writes
// (upsertOrderItemDiscountRedemption) only since PROMPT 21 shipped. Every
// amount that could mix the two eras is split Reconstructed*/Measured*
// rather than left as one ambiguous figure, and MeasurementCompleteFrom
// names the point after which this tab's numbers are complete rather than a
// floor — see its own doc comment. This is deliberately carried in the data
// (is_reconstructed, per PROMPT 22's own instruction), not a footnote.
type DiscountsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// Channels reuses ChannelFilter (channels.go, PROMPT 18) verbatim — empty
	// means every channel.
	Channels []string `json:"channels,omitempty"`
	// SortBy is one of DiscountsSortAmount (default) or DiscountsSortCount —
	// applies to the répartition-par-remise table (Rows), resolved and
	// counted server-side like Produits/Options, never client-truncated.
	SortBy   string `json:"sort_by,omitempty"`
	SortDir  string `json:"sort_dir,omitempty"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}

const (
	DiscountsSortAmount = "amount"
	DiscountsSortCount  = "count"

	DiscountsDefaultPageSize = 50
	DiscountsMaxPageSize     = 200
)

// discountsMinOrdersForRate reuses staffCancellationMinOrders (cancellations.go)
// verbatim — PROMPT 22's own instruction is to apply "le seuil de matérialité
// déjà retenu ailleurs" rather than invent a new one, same as PROMPT 16/18/19
// before it. With 545 lignes reconstituées in total and live capture only
// just starting, DiscountedOrdersCount will typically sit well under this bar
// for a while — see DiscountsPeriodTotals' doc comment for what stays visible
// below it regardless.
const discountsMinOrdersForRate = staffCancellationMinOrders

// DiscountsResponse. SortBy/SortDir echo back the applied (defaulted/
// validated) request, same convention as ProductsResponse.
type DiscountsResponse struct {
	Scope    RevenueScope `json:"scope"`
	Channels []string     `json:"channels"`

	// MeasurementCompleteFrom is the earliest created_at (YYYY-MM-DD, this
	// merchant scope, all time — not bounded to the requested period) among
	// live-written (is_reconstructed=false) discount_redemptions rows: the
	// point from which this tab stops being a best-effort reconstruction and
	// starts being a complete, direct record of every discount granted. nil
	// when this scope has never had a single live write yet — every figure
	// below is then a floor with no date to anchor a "complete since"
	// statement, and the frontend must say so rather than default to
	// silence.
	MeasurementCompleteFrom *string `json:"measurement_complete_from,omitempty"`

	SortBy  string `json:"sort_by"`
	SortDir string `json:"sort_dir"`

	CurrentPeriod  DiscountsPeriodTotals `json:"current_period"`
	PreviousPeriod DiscountsPeriodTotals `json:"previous_period"`

	MarginImpact DiscountsMarginCoverage `json:"margin_impact"`

	Pagination models.PaginationMetadata `json:"pagination"`
	Rows       []DiscountRow             `json:"rows"`
}

// DiscountsPeriodTotals.TotalDiscountedCents = ReconstructedAmountCents +
// MeasuredAmountCents, always — the floor/complete split this section's doc
// comment requires, carried on every period rather than only globally.
//
// DiscountRatePercent's denominator is ReferenceRevenueTTCCents: this
// period's whole CA TTC under the same channel filter, ALL orders whether
// discounted or not — "quelle part de ce qui a été encaissé a été remisée."
// The alternative denominator (CA of discounted orders alone) is equally
// defensible and deliberately NOT used here — see docs/decisions.md's PROMPT
// 22 entry for the tradeoff. Both DiscountRatePercent and
// OrdersWithDiscountRatePercent are nil below discountsMinOrdersForRate
// DiscountedOrdersCount (PROMPT 22's seuil de matérialité): the raw cents/
// counts/reference volumes stay visible regardless, so the frontend can
// still render "X sur Y" instead of nothing.
type DiscountsPeriodTotals struct {
	From string `json:"from"`
	To   string `json:"to"`

	TotalDiscountedCents          int64 `json:"total_discounted_cents"`
	ReconstructedAmountCents      int64 `json:"reconstructed_amount_cents"`
	MeasuredAmountCents           int64 `json:"measured_amount_cents"`
	ReconstructedRedemptionsCount int64 `json:"reconstructed_redemptions_count"`
	MeasuredRedemptionsCount      int64 `json:"measured_redemptions_count"`

	DiscountedOrdersCount int64 `json:"discounted_orders_count"`
	TotalOrdersCount      int64 `json:"total_orders_count"`
	// OrdersWithDiscountRatePercent is nil below discountsMinOrdersForRate.
	OrdersWithDiscountRatePercent *float64 `json:"orders_with_discount_rate_percent,omitempty"`

	ReferenceRevenueTTCCents int64 `json:"reference_revenue_ttc_cents"`
	// DiscountRatePercent is nil below discountsMinOrdersForRate — see this
	// type's doc comment for the denominator it uses when present.
	DiscountRatePercent *float64 `json:"discount_rate_percent,omitempty"`
}

// DiscountsMarginCoverage mirrors ProductsCostCoverage's contract exactly
// (PROMPT 16's "piège de l'agrégation partielle," same discipline applied
// here): MarginImpactCents/Percent are nil below coversCoverageThreshold —
// today that is always true, cost_price_unit being null on the near-totality
// of orderitems (PROMPT 07 lot 1) — so the tab shows "non disponible," never
// a zero that would read as "this discount costs nothing in margin."
//
// Scoped to PRODUCT_LINE redemptions only: a CART-scope discount spans a
// whole order, not one costed orderitems line, so it cannot be attributed to
// a cost_price_unit at all and is excluded from this coverage's numerator
// AND denominator (never folded in as a fake zero-cost line). In practice
// this is not a real restriction today — cart-scope redemptions do not exist
// yet (applyCartDiscount is unbuilt) — but the query is written to stay
// correct the day that changes.
type DiscountsMarginCoverage struct {
	DiscountedLinesRevenueTTCCentsTotal   int64    `json:"discounted_lines_revenue_ttc_cents_total"`
	DiscountedLinesRevenueTTCCentsCovered int64    `json:"discounted_lines_revenue_ttc_cents_covered"`
	CoverageRatio                         float64  `json:"coverage_ratio"`
	MarginImpactCents                     *int64   `json:"margin_impact_cents,omitempty"`
	MarginImpactPercent                   *float64 `json:"margin_impact_percent,omitempty"`
}

// DiscountRow is one row of the répartition-par-remise table, grouped by
// discount_id (discounts.discount_id_new) — never discount_name/
// discount_code, so a rename never fragments or merges a discount's history.
// DiscountName reflects the discount's CURRENT row (LEFT JOIN: a
// soft-deleted discount, discounts.enabled=false, still shows its historical
// redemptions in full, with IsDeleted=true and its last known name — never
// silently dropped from "laquelle me coûte le plus" just because it was
// deleted since).
type DiscountRow struct {
	DiscountID   int64  `json:"discount_id"`
	DiscountName string `json:"discount_name"`
	IsDeleted    bool   `json:"is_deleted"`

	TotalAmountCents int64 `json:"total_amount_cents"`
	RedemptionsCount int64 `json:"redemptions_count"`

	ReconstructedAmountCents int64 `json:"reconstructed_amount_cents"`
	MeasuredAmountCents      int64 `json:"measured_amount_cents"`
}
