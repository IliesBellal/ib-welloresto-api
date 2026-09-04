// Package analytics serves the 4 direct-SQL analytics tabs (CA, Commandes,
// Règlements, TVA) described in docs/analytics/ (wello-back-office repo,
// PROMPT 03). Every repository query in this package must be built from
// AnalyticsOrdersScope — see its doc comment for why.
package analytics

import (
	"context"
	"strings"
	"time"

	"welloresto-api/internal/modules/auth"
)

// analyticsOrdersScopeWhere is the ONE definition of "revenue" for every
// analytics endpoint in this package — CA, Commandes, Règlements, TVA all
// read the same set of orders.
//
// It deliberately differs from internal/modules/pos/reports's fiscal scope
// (docs/analytics/AUDIT.md §4.3, wello-back-office repo): pos/reports adds
// `brand = 'WELLO_RESTO'` (excludes Uber Eats/Deliveroo) and
// `created_by NOT IN ('-1','SCANNORDER')` (excludes ScanNOrder), because it
// answers a legal/accounting question under an arbitrage currently in
// progress (see PROMPT 03's "hors périmètre" note) — do not fix, work
// around, or copy that filter here. This package answers an analytics
// question: "how much did this establishment sell, across every channel."
// The two will show different numbers for the same period. That is
// intentional, not a bug to reconcile.
//
// `upper(o.brand_status) NOT IN (...)` (not a bare `NOT IN`) matters: 8 rows
// on PROD carry a lowercase brand_status and would otherwise leak through
// (docs/analytics/PERIMETRE.md §1, recalculating AUDIT.md P5 on the PROD
// perimeter).
//
// This is a private string constant, not exported — the only way to use it
// is through AnalyticsOrdersScope below, which forces every caller to also
// supply the merchant scope and the period bounds. There is no lower-level
// building block a query could use to accidentally omit a piece of it.
const analyticsOrdersScopeWhere = `
	o.merchant_id = ANY(?)
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND o.state IN ('CLOSED', 'DONE')
	AND upper(o.brand_status) NOT IN ('DELETED', 'CANCELED')
`

// AnalyticsOrdersScope returns the WHERE fragment (using `?` placeholders,
// rebound to `$N` by dbx.GetDB) and its positional args for the canonical
// analytics revenue scope, applied to alias `o` (orders). merchantIDs is
// always the caller's resolved accessible scope (ResolveAccessibleMerchants)
// or a validated subset of it — never a raw client-supplied value; see
// ValidateRequestedMerchants.
//
// merchant_id = ANY($1) (a Postgres array parameter), not `IN (...)` built
// from N placeholders, and never a bare `= $1`: PERF-INDEX.md §3.1
// (wello-back-office repo) validated the orders(merchant_id, creation_date)
// composite index specifically against this array form at 1/5/20 merchant
// values — at 20 values, without the index the planner falls back to a full
// Seq Scan, which is exactly the volume this product is heading toward.
// Bounds are always [start, end) — end is EXCLUSIVE. A "<= 23:59:59" bound
// (as pos/reports uses) silently drops the last second of the day; never
// reproduce that here.
func AnalyticsOrdersScope(merchantIDs []string, startUTC, endUTC time.Time) (string, []interface{}) {
	return strings.TrimSpace(analyticsOrdersScopeWhere), []interface{}{merchantIDs, startUTC, endUTC}
}

// analyticsCancellationsScopeWhere is the ONE definition of "cancellation"
// for the Annulations tab (PROMPT 10) — the complement of
// analyticsOrdersScopeWhere above, living under the same regime: a private
// constant, reachable only through AnalyticsCancellationsScope, which forces
// every caller to also supply the merchant scope and period bounds.
//
// upper(o.brand_status) = 'CANCELED' only — NOT DENIED, NOT DELETED. Traced
// against the code that writes each value (2026-09-04):
//
//   - CANCELED is written both when staff cancel an order already accepted
//     into the flow (OrdersLifeCycleRepository.DeleteOrderLocal,
//     internal/modules/order_life_cycle/repository.go) and when a
//     marketplace cancels an accepted order from its side
//     (UberRepository.SetOrderStatusCanceled, DeliverooRepository.
//     MarkOrderCanceledLocal) — an order that existed, was accepted, and was
//     later called off. This is what "annulation" means in this tab.
//   - DENIED is written by OrdersLifeCycleRepository.DenyOrderLocal and
//     UberRepository.SetOrderStatusDenied — a refusal AT INTAKE, before the
//     order ever entered production (merchant_approval is set to 'DENIED'
//     alongside it). Verified on staging (PROD scope): DENIED orders carry
//     cancelled_by_type SYSTEM or PLATFORM only, never STAFF or CUSTOMER —
//     consistent with "the kitchen never touched this," which is a
//     different metric (a refusal rate) from a cancellation rate. Excluded.
//   - DELETED is dead code on the write side: no query anywhere in this
//     repository's history (`git log --all -S "brand_status = 'DELETED'"`,
//     `-S "brand_status='DELETED'"`) ever sets it. Every current cancel path
//     writes CANCELED instead (DeleteOrderLocal's name is the tell — it
//     used to presumably write DELETED, now writes CANCELED). Confirmed on
//     staging: every DELETED row's creation_date and last_update predates
//     2024-10-26 — a pure historical artifact from a superseded code path,
//     with 0 rows since. Excluded: counting it would double a status the
//     product no longer produces, for a metric that is exactly 0 going
//     forward.
//
// No `state` filter, unlike analyticsOrdersScopeWhere: the marketplace
// cancel paths above (SetOrderStatusCanceled, MarkOrderCanceledLocal) never
// touch orders.state, so a canceled order's state is whatever it was before
// cancellation, not reliably 'CLOSED'/'DONE'. Constraining on state would
// silently drop cancellations under an assumption the write path does not
// guarantee (verified empirically CLOSED/DONE-only on PROD today, but nothing
// enforces that going forward — this scope must stay correct on its own
// terms, not on today's incidental distribution).
//
// upper(...), same reasoning as analyticsOrdersScopeWhere: brand_status
// carries lowercase rows on PROD (PERIMETRE.md §1).
const analyticsCancellationsScopeWhere = `
	o.merchant_id = ANY(?)
	AND o.creation_date >= ?
	AND o.creation_date < ?
	AND upper(o.brand_status) = 'CANCELED'
`

// AnalyticsCancellationsScope returns the WHERE fragment and args for the
// canonical "cancellation" scope, applied to alias `o` (orders) — the
// Annulations tab's counterpart to AnalyticsOrdersScope. Same calling
// convention: merchantIDs is always the caller's resolved/validated
// accessible scope, bounds are always [start, end).
func AnalyticsCancellationsScope(merchantIDs []string, startUTC, endUTC time.Time) (string, []interface{}) {
	return strings.TrimSpace(analyticsCancellationsScopeWhere), []interface{}{merchantIDs, startUTC, endUTC}
}

// analyticsAllOrdersCreatedScopeWhere is the cancellation rate's denominator:
// every order created in the period, regardless of brand_status or state.
// This is a deliberate choice between two defensible definitions (PROMPT 10
// §3): "cancelled ÷ (cancelled + valid)" restricts the denominator to orders
// that reached a final, countable fate under AnalyticsOrdersScope — but that
// scope already EXCLUDES cancellations by construction (analyticsOrdersScopeWhere
// excludes brand_status CANCELED/DELETED), so "valid" would have to be
// redefined ad hoc just for this ratio, and a DENIED or still-open order
// would fall into neither bucket, silently shrinking the denominator.
// "cancelled ÷ every order created" has none of that: it is the ordinary
// meaning of a cancellation rate (of everything this establishment tried to
// sell in the period, how much fell through), every order in scope lands on
// exactly one side of the ratio, and it needs no second, narrower scope
// definition to exist. CancellationsPeriodTotals names the field
// TotalOrdersCreated (not TotalOrders or TotalValidOrders) specifically so
// the contract itself states the denominator — see its doc comment.
const analyticsAllOrdersCreatedScopeWhere = `
	o.merchant_id = ANY(?)
	AND o.creation_date >= ?
	AND o.creation_date < ?
`

// AnalyticsAllOrdersCreatedScope returns the WHERE fragment and args for
// "every order created in the period" — see analyticsAllOrdersCreatedScopeWhere's
// doc comment for why this, and not AnalyticsOrdersScope, is the
// cancellation rate's denominator.
func AnalyticsAllOrdersCreatedScope(merchantIDs []string, startUTC, endUTC time.Time) (string, []interface{}) {
	return strings.TrimSpace(analyticsAllOrdersCreatedScopeWhere), []interface{}{merchantIDs, startUTC, endUTC}
}

// ResolveAccessibleMerchants returns the establishments this request is
// allowed to read. The token carries exactly one MerchantID (see
// docs/analytics/DROITS.md, wello-back-office repo, §2) — a user covering
// several establishments holds one token per establishment, never a token
// that spans several. This function returns exactly that one establishment.
//
// Do NOT change this to `SELECT merchant_id FROM users_rights WHERE user_id
// = ?`: it is trivial to write and would silently open every establishment
// the underlying user account is linked to, under a token issued for one of
// them — a scope-widening regression, not a feature. Multi-establishment
// access requires an explicit mechanism (a selector, a group token) that
// does not exist yet and is a separate product decision.
func ResolveAccessibleMerchants(ctx context.Context, user *auth.UserLoginRow) ([]string, error) {
	return []string{user.MerchantID}, nil
}

// ErrMerchantNotAccessible is returned by ValidateRequestedMerchants when the
// client asked for an establishment outside its resolved scope. Handlers
// must translate this to HTTP 403 — never filter the request down silently.
var ErrMerchantNotAccessible = errAnalytics("requested merchant_id is outside the accessible scope")

type errAnalytics string

func (e errAnalytics) Error() string { return string(e) }

// ValidateRequestedMerchants checks a client-supplied merchant_ids filter
// against the accessible scope. An empty `requested` means "use the full
// accessible scope" (the common case today, since that scope is always
// exactly one establishment). A non-empty `requested` must be a subset of
// `accessible`, or the request is rejected outright — never silently
// narrowed to the accessible set, which would mask the client's mistake (or
// a compromised token's attempt) instead of surfacing it.
func ValidateRequestedMerchants(requested, accessible []string) ([]string, error) {
	if len(requested) == 0 {
		return accessible, nil
	}

	accessibleSet := make(map[string]struct{}, len(accessible))
	for _, id := range accessible {
		accessibleSet[id] = struct{}{}
	}
	for _, id := range requested {
		if _, ok := accessibleSet[id]; !ok {
			return nil, ErrMerchantNotAccessible
		}
	}
	return requested, nil
}
