package analytics

// Canonical channel keys. A "channel" does not exist as a column — it is
// derived from brand × order_type (channelCaseExpr below) — and this is the
// one place that derivation happens. AUDIT.md I1 (wello-back-office repo)
// flagged the same channel being called `restaurant` in one tab and
// `dine_in` in another: every analytics response uses these keys, and only
// these keys, so the frontend never needs a second naming.
const (
	ChannelDineIn            = "dine_in"
	ChannelTakeaway          = "takeaway"
	ChannelDelivery          = "delivery"
	ChannelUberEatsTakeaway  = "ubereats_takeaway"
	ChannelUberEatsDelivery  = "ubereats_delivery"
	ChannelDeliverooTakeaway = "deliveroo_takeaway"
	ChannelDeliverooDelivery = "deliveroo_delivery"
	// ChannelUnknown covers order_type IS NULL (0 cases since 2024-11 per
	// AUDIT.md, but historical orders carry it) and any brand/order_type
	// combination that doesn't map to one of the 7 channels above (e.g. a
	// marketplace order with a missing or unexpected order_type).
	ChannelUnknown = "unknown"
)

// Channels lists every canonical key in a fixed display order, for
// responses that enumerate all channels (e.g. filling zero-value entries so
// the frontend never has to guess which series exist).
var Channels = []string{
	ChannelDineIn,
	ChannelTakeaway,
	ChannelDelivery,
	ChannelUberEatsTakeaway,
	ChannelUberEatsDelivery,
	ChannelDeliverooTakeaway,
	ChannelDeliverooDelivery,
	ChannelUnknown,
}

// channelCaseExpr is the single SQL derivation of a channel from
// orders.brand and orders.order_type, aliased `o`. Every analytics query
// that breaks revenue down by channel must use this exact expression —
// duplicating the CASE inline elsewhere would risk the two silently
// diverging (the failure mode I1 already documents once).
const channelCaseExpr = `
	CASE
		WHEN o.brand = 'UBER_EATS' AND o.order_type = 'DELIVERY' THEN 'ubereats_delivery'
		WHEN o.brand = 'UBER_EATS' AND o.order_type = 'TAKE_AWAY' THEN 'ubereats_takeaway'
		WHEN o.brand = 'UBER_EATS' THEN 'unknown'
		WHEN o.brand = 'DELIVEROO' AND o.order_type = 'DELIVERY' THEN 'deliveroo_delivery'
		WHEN o.brand = 'DELIVEROO' AND o.order_type = 'TAKE_AWAY' THEN 'deliveroo_takeaway'
		WHEN o.brand = 'DELIVEROO' THEN 'unknown'
		WHEN o.order_type = 'IN' THEN 'dine_in'
		WHEN o.order_type = 'TAKE_AWAY' THEN 'takeaway'
		WHEN o.order_type = 'DELIVERY' THEN 'delivery'
		ELSE 'unknown'
	END
`

// ChannelFilter validates the caller's requested channels against the 8
// canonical Channels keys, defaulting to all of them when empty — same
// validate-or-reject shape as optionTypesFilter (options.go): an unknown
// value is a 400, never a silent drop.
//
// PROMPT 18 (Clients tab) is this package's first channel INPUT filter —
// channelCaseExpr existed only as an output/grouping dimension before (every
// ByChannel query in this package groups by it, none filter by it). This
// reuses that exact derivation as a WHERE predicate (`(channelCaseExpr) =
// ANY(?)`) instead of inventing a second one, and the same Channels
// referential wello-back-office's channels.ts already mirrors — so any tab
// after this one that needs a channel filter reuses ChannelFilter directly
// rather than writing a third variant.
func ChannelFilter(requested []string) ([]string, bool) {
	if len(requested) == 0 {
		return append([]string(nil), Channels...), true
	}
	valid := make(map[string]bool, len(Channels))
	for _, c := range Channels {
		valid[c] = true
	}
	seen := make(map[string]bool, len(requested))
	for _, c := range requested {
		if !valid[c] {
			return nil, false
		}
		seen[c] = true
	}
	out := make([]string, 0, len(seen))
	for _, c := range Channels {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out, true
}
