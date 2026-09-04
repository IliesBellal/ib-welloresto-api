package analytics

// RevenueRequest is the payload for POST /analytics/revenue.
type RevenueRequest struct {
	// DateFrom/DateTo are local calendar days (YYYY-MM-DD) in the
	// establishment's own timezone, both inclusive.
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
	// MerchantIDs restricts the request to a subset of the caller's
	// accessible scope. Empty means "the full accessible scope" — today
	// always exactly one establishment (see ResolveAccessibleMerchants).
	// A value outside the accessible scope is a 403, never silently dropped.
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	// GroupBy is "none" (default) or "merchant". Both return the same data
	// today since the accessible scope is always one establishment — kept in
	// the contract now so a later multi-establishment opening needs no
	// client change.
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
// the HT recompute).
type OrdersRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
	GroupBy     string   `json:"group_by,omitempty"`
}

type OrdersResponse struct {
	Scope          RevenueScope         `json:"scope"`
	CurrentPeriod  OrdersPeriodTotals   `json:"current_period"`
	PreviousPeriod OrdersPeriodTotals   `json:"previous_period"`
	PreviousYear   OrdersPeriodTotals   `json:"previous_year"`
	Timeline       []OrdersDayPoint     `json:"timeline"`
	ByChannel      []OrdersChannelTotal `json:"by_channel"`
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

// ---- Règlements (POST /analytics/payments) ----

type PaymentsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
}

type PaymentsResponse struct {
	Scope          RevenueScope         `json:"scope"`
	CurrentPeriod  PaymentsPeriodTotals `json:"current_period"`
	PreviousPeriod PaymentsPeriodTotals `json:"previous_period"`
	PreviousYear   PaymentsPeriodTotals `json:"previous_year"`
	Timeline       []PaymentsDayPoint   `json:"timeline"`
	ByMethod       []PaymentMethodTotal `json:"by_method"`
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
}

// VATResponse is the canonical analytics view of VAT across every brand and
// channel — NOT a fiscal document, and deliberately not reconciled with
// pos/reports/tva (different scope: that endpoint restricts to WELLO_RESTO
// and excludes ScanNOrder, under an accounting arbitrage in progress; this
// one answers "how much VAT did this establishment's sales carry," across
// every channel). The frontend must label this distinction — see
// VATAnalyticsTab.tsx.
//
// The gap with pos/reports/tva is fully named and, for merchant 212 (PROD's
// largest) over a 12-month window (verified read-only against staging,
// 2026-09-03), fully chiffré to the cent:
//   - non-WELLO_RESTO orders (Uber Eats/Deliveroo), excluded by pos/reports
//   - ScanNOrder orders (created_by IN ('-1','SCANNORDER')), excluded by
//     pos/reports, within WELLO_RESTO
//   - orders.delivery_fees: never in this endpoint's TTC/HT at all (see
//     GetVATTotals' doc comment) but added by pos/reports via its own
//     tva_id=-1 UNION — this pulls pos/reports' total UP, so it narrows the
//     analytics-vs-pos/reports gap rather than widening it, unlike the two
//     filters above
//   - state='DONE' vs pos/reports' state='CLOSED'-only filter (0 on this
//     merchant/window)
//   - tva.show_in_report=false lines within pos/reports' own WELLO_RESTO/
//     CLOSED/non-ScanNOrder scope (0 on this merchant/window — the only
//     show_in_report=false category, tva_id=-1, is reached exclusively
//     through the delivery_fees line above)
//
// (excluded by pos/reports) - (delivery_fees, added by pos/reports) exactly
// equals the analytics-minus-pos/reports TTC delta — no residual.
type VATResponse struct {
	Scope          RevenueScope      `json:"scope"`
	CurrentPeriod  VATPeriodTotals   `json:"current_period"`
	PreviousPeriod VATPeriodTotals   `json:"previous_period"`
	PreviousYear   VATPeriodTotals   `json:"previous_year"`
	ByRate         []VATRateTotal    `json:"by_rate"`
	ByChannel      []VATChannelTotal `json:"by_channel"`
}

type VATPeriodTotals struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TotalTTCCents int64  `json:"total_ttc_cents"`
	TotalHTCents  int64  `json:"total_ht_cents"`
	TotalVATCents int64  `json:"total_vat_cents"`
}

// VATRateTotal.Rate is tva_categories.tva_rate as stored (20, 10, 5.5, ...).
// Includes tva_id=-1 (delivery fees, 20%) even though it's disabled/hidden
// from pos/reports (enabled=false, show_in_report=false) — see GetVATTotals'
// doc comment (repository.go) for the decision and its reasoning.
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
