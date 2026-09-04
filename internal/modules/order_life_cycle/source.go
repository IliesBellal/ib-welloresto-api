package order_life_cycle

import (
	"strings"

	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

// Source values written to orders.order_source / customer.acquisition_source
// (A6 / A6b). Kept as string constants rather than a Go type since both
// columns are plain varchar with a CHECK constraint carrying the same list —
// see migrations/todo/114_write_path_instrumentation.up.sql.
const (
	SourceWelloRestoPOS = "WELLO_RESTO_POS"
	SourceKiosk         = "KIOSK"
	SourceScanNOrder    = "SCANNORDER"
	SourceUberEats      = models.BrandUberEats
	SourceDeliveroo     = models.BrandDeliveroo
)

// resolveOrderSource derives orders.order_source (A6) from brand + created_by
// at order-creation time — the same signals already used ad hoc across the
// codebase (e.g. internal/modules/integrations/repository.go's KPI queries),
// now centralized in one place instead of re-derived per query. Mirrors the
// backfill rule in migrations/todo/114_write_path_instrumentation.up.sql —
// keep both in sync if either changes. Returns nil (written as SQL NULL)
// rather than guessing when the combination isn't recognized.
func resolveOrderSource(brand string, createdBy *string) *string {
	switch brand {
	case models.BrandUberEats:
		return helpers.StringPtr(SourceUberEats)
	case models.BrandDeliveroo:
		return helpers.StringPtr(SourceDeliveroo)
	case models.BrandWelloResto, "":
		// "" matches setOrderDefaults' own rule (brand defaults to
		// WELLO_RESTO) — upsertCustomer calls this before setOrderDefaults
		// has run for a brand-new WELLO_RESTO order, so brand can still be
		// empty at that point.
		if createdBy == nil {
			return nil
		}
		switch *createdBy {
		case "KIOSK":
			return helpers.StringPtr(SourceKiosk)
		case "SCANNORDER", "-1":
			// "-1" is a historical ScanNOrder marker, superseded by the
			// literal "SCANNORDER" but still written by some legacy rows —
			// internal/modules/pos/accounting/repository.go already treats
			// both as equivalent in its own filters.
			return helpers.StringPtr(SourceScanNOrder)
		}
		if isRealStaffID(*createdBy) {
			return helpers.StringPtr(SourceWelloRestoPOS)
		}
	}
	return nil
}

// isRealStaffID reports whether createdBy looks like an authenticated staff
// user id (numeric, or the newer "user-<uuid>" form) rather than one of the
// known channel sentinels ("KIOSK", "SCANNORDER") or a data anomaly ("0",
// empty).
func isRealStaffID(createdBy string) bool {
	if createdBy == "" || createdBy == "0" {
		return false
	}
	if strings.HasPrefix(createdBy, "user-") {
		return true
	}
	for _, r := range createdBy {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

