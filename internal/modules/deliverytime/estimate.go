// Package deliverytime exposes the merchant-level rolling average delivery
// travel time (average_delivery_time), the fallback used when an order
// doesn't carry a live per-address travel estimate (Google Maps on the POS,
// OSRM on ScanNOrder). Mirrors the internal/modules/distributiontime pattern.
package deliverytime

import (
	"context"
	"database/sql"

	"welloresto-api/internal/database/dbx"
)

// AverageSeconds returns the merchant's rolling average delivery travel time
// in seconds, computed periodically by TasksManager.UpdateAverageDeliveryTime.
// found=false when the merchant has no average_delivery_time row yet (no
// delivery order with a captured travel time in the rolling window).
func AverageSeconds(ctx context.Context, database *sql.DB, merchantID string) (int, bool, error) {
	db := dbx.GetDB(ctx, database)

	var seconds sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT delivery_time_seconds FROM average_delivery_time WHERE merchant_id = ?`,
		merchantID,
	).Scan(&seconds)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !seconds.Valid {
		return 0, false, nil
	}
	return int(seconds.Int64), true, nil
}
