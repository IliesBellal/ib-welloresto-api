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

// CancellationsRequest mirrors PaymentsRequest/VATRequest's shape — no
// GroupBy (this tab has no merchant breakdown), no IncludeHT (nothing here
// needs the HT recompute).
type CancellationsRequest struct {
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
	MerchantIDs []string `json:"merchant_ids,omitempty"`
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
