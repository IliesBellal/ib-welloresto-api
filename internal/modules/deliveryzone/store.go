package deliveryzone

import (
	"context"
	"database/sql"

	"welloresto-api/internal/database/dbx"
)

// LoadSettings lit la découpe de l'établissement et sa position. Un
// établissement sans ligne merchant_parameters a une découpe vide.
func LoadSettings(ctx context.Context, database *sql.DB, merchantID string) (Settings, error) {
	db := dbx.GetDB(ctx, database)

	var (
		zoningType, cardinalRanges, radialRanges sql.NullString
		cardinalCount, radialCount, cellSize     sql.NullInt64
		originLat, originLng, merchLat, merchLng sql.NullFloat64
	)
	err := db.QueryRowContext(ctx, `
		SELECT mp.zoning_type, mp.cardinal_cone_count, mp.cardinal_zone_ranges,
		       mp.radial_cone_count, mp.radial_zone_ranges, mp.grid_cell_size_km,
		       mp.grid_origin_lat, mp.grid_origin_lng, m.lat, m.lng
		FROM merchant_parameters mp
		LEFT JOIN merchant m ON m.id::text = mp.merchant_id
		WHERE mp.merchant_id = ?
		LIMIT 1`, merchantID,
	).Scan(&zoningType, &cardinalCount, &cardinalRanges, &radialCount, &radialRanges, &cellSize,
		&originLat, &originLng, &merchLat, &merchLng)
	if err == sql.ErrNoRows {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, err
	}

	s := Settings{
		ZoningType:         zoningType.String,
		CardinalConeCount:  int(cardinalCount.Int64),
		CardinalZoneRanges: cardinalRanges.String,
		RadialConeCount:    int(radialCount.Int64),
		RadialZoneRanges:   radialRanges.String,
		GridCellSizeKm:     float64(cellSize.Int64),
	}
	if p, ok := ValidPoint(nullFloatPtr(originLat), nullFloatPtr(originLng)); ok {
		s.GridOrigin = &p
	}
	if p, ok := ValidPoint(nullFloatPtr(merchLat), nullFloatPtr(merchLng)); ok {
		s.Merchant = &p
	}
	return s, nil
}

// RefreshCustomerZone recalcule customer_zone_code à partir des coordonnées
// enregistrées du client (customer_lat / customer_lng, déjà mises à jour par
// l'enregistrement de la commande) et de la découpe actuelle de
// l'établissement. Sans position valide, hors découpe ou sans découpe, la zone
// est effacée : un ancien code ne doit pas survivre à un changement d'adresse.
// Renvoie le code écrit ("" = effacé).
func RefreshCustomerZone(ctx context.Context, database *sql.DB, merchantID, customerID string) (string, error) {
	db := dbx.GetDB(ctx, database)

	var lat, lng sql.NullFloat64
	err := db.QueryRowContext(ctx,
		`SELECT customer_lat, customer_lng FROM customer WHERE customer_id = ? AND merchant_id = ?`,
		customerID, merchantID,
	).Scan(&lat, &lng)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	code := ""
	if p, ok := ValidPoint(nullFloatPtr(lat), nullFloatPtr(lng)); ok {
		settings, err := LoadSettings(ctx, database, merchantID)
		if err != nil {
			return "", err
		}
		code = settings.Code(p)
	}

	var value interface{}
	if code != "" {
		value = code
	}
	_, err = db.ExecContext(ctx,
		`UPDATE customer SET customer_zone_code = ?
		 WHERE customer_id = ? AND merchant_id = ? AND customer_zone_code IS DISTINCT FROM ?`,
		value, customerID, merchantID, value,
	)
	if err != nil {
		return "", err
	}
	return code, nil
}

func nullFloatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}
