package locations

import (
	"context"
	"database/sql"
	"encoding/json"
	"welloresto-api/internal/models"
)

type LocationsRepository struct {
	db *sql.DB
}

func NewLocationsRepository(db *sql.DB) *LocationsRepository {
	return &LocationsRepository{db: db}
}

func (r *LocationsRepository) GetLocations(ctx context.Context, merchantID string) (*models.LocationResponse, error) {
	res := &models.LocationResponse{
		Locations: []models.Location{},
		Floors:    []models.Floor{},
		Areas:     []models.Area{},
	}

	// 1. CHARGEMENT DES TABLES (LOCATIONS)
	// On crée une map pour un accès rapide par ID plus tard
	locMap := make(map[string]*models.Location)

	queryLocs := `
		SELECT 
			l.location_id, l.location_name, COALESCE(l.location_desc, ''), l.seats, 
			l.location_order, l.floor_id, l.shape, l.current_x, l.current_y,
			l.current_width, l.current_height, l.angle, ol.order_id,
			CASE WHEN ol.order_id IS NULL THEN '1' ELSE '0' END as available 
		FROM locations l
		LEFT JOIN (
			SELECT DISTINCT ol.location_id, ol.order_id
			FROM order_location ol
			INNER JOIN orders o ON o.order_id = ol.order_id
			WHERE o.state NOT IN ('DELETED','DONE','CANCELED','CLOSED')
			AND o.merchant_id = ?
		) ol ON l.location_id = ol.location_id
		WHERE l.merchant_id = ? AND l.enabled IS TRUE
		ORDER BY l.location_order ASC;`

	rowsLoc, err := r.db.QueryContext(ctx, queryLocs, merchantID, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsLoc.Close()

	for rowsLoc.Next() {
		var l models.Location
		if err := rowsLoc.Scan(
			&l.LocationID, &l.LocationName, &l.LocationDesc, &l.Seats, &l.Order, &l.FloorID,
			&l.Shape, &l.X, &l.Y, &l.W, &l.H, &l.Angle, &l.OpenOrderID, &l.Available,
		); err != nil {
			return nil, err
		}
		l.Bookings = []models.Booking{}
		locMap[l.LocationID] = &l
	}

	// 2. CHARGEMENT DES RÉSERVATIONS (BOOKINGS)
	// On gère le fait qu'une résa peut avoir plusieurs tables
	queryBookings := `
		SELECT 
			b.booking_id, b.booking_number, b.status, 
			b.sequence_number, b.booking_date_from, b.booking_date_to, b.party_size,
			b.creation_date, b.created_by, b.comment, b.booking_date_from, b.booking_date_to,
			bl.location_id, c.customer_id, c.customer_name, COALESCE(c.customer_tel, '')
		FROM bookings b
		INNER JOIN booked_location bl ON bl.booking_id = b.booking_id
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.merchant_id = ? AND b.status = 'ACCEPTED'
		AND b.booking_date_to > UTC_TIMESTAMP - INTERVAL 5 HOUR;`

	rowsBook, err := r.db.QueryContext(ctx, queryBookings, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsBook.Close()

	uniqueBookings := make(map[string]*models.Booking)

	for rowsBook.Next() {
		var bID, bNum, bStatus, bFrom, bTo, bCreated, bBy, bLocID string
		var bSeq, bSize int
		var bComment, bStart, bEnd, cID, cName, cTel *string

		err := rowsBook.Scan(
			&bID, &bNum, &bStatus, &bSeq, &bFrom, &bTo, &bSize,
			&bCreated, &bBy, &bComment, &bStart, &bEnd,
			&bLocID, &cID, &cName, &cTel,
		)
		if err != nil {
			return nil, err
		}

		// Si c'est la première fois qu'on voit cette réservation
		if _, exists := uniqueBookings[bID]; !exists {
			uniqueBookings[bID] = &models.Booking{
				BookingID:       bID,
				BookingNumber:   bNum,
				Status:          bStatus,
				SequenceNumber:  bSeq,
				BookingDateFrom: bFrom,
				BookingDateTo:   bTo,
				PartySize:       bSize,
				CreationDate:    bCreated,
				CreatedBy:       bBy,
				Comment:         bComment,
				StartDate:       bStart,
				EndDate:         bEnd,
				Customer:        models.Customer{CustomerID: cID, CustomerName: cName, CustomerTel: cTel},
				Merchant:        models.MerchantBookingParams{MerchantID: merchantID},
				Locations:       []models.Location{},
			}
		}

		// On ajoute la table (si elle existe dans locMap) à la réservation
		if loc, ok := locMap[bLocID]; ok {
			// On ajoute la table à la réservation
			uniqueBookings[bID].Locations = append(uniqueBookings[bID].Locations, *loc)
			// Et on ajoute la réservation à la table (pour la vue par table)
			loc.Bookings = append(loc.Bookings, *uniqueBookings[bID])
		}
	}

	// 3) FLOORS
	rowsFloors, err := r.db.QueryContext(ctx, "SELECT id, name FROM floors WHERE merchant_id = ? AND enabled IS TRUE", merchantID)
	if err == nil {
		defer rowsFloors.Close()
		for rowsFloors.Next() {
			var f models.Floor
			rowsFloors.Scan(&f.ID, &f.Name)
			res.Floors = append(res.Floors, f)
		}
	}

	// 4) AREAS
	rowsAreas, err := r.db.QueryContext(ctx, `
		SELECT fa.id, fa.floor_id, fa.name, fa.points, fa.x, fa.y, fa.angle, fa.stroke_color, fa.color
		FROM floor_areas fa
		INNER JOIN floors f ON f.id = fa.floor_id
		WHERE f.merchant_id = ? AND fa.enabled IS TRUE AND f.enabled IS TRUE`, merchantID)
	if err == nil {
		defer rowsAreas.Close()
		for rowsAreas.Next() {
			var a models.Area
			var pts []byte
			rowsAreas.Scan(&a.ID, &a.FloorID, &a.Name, &pts, &a.X, &a.Y, &a.Angle, &a.StrokeColor, &a.Color)
			a.Points = json.RawMessage(pts)
			res.Areas = append(res.Areas, a)
		}
	}

	// FINAL MERGE : Injecter les bookings dans les locations
	for _, loc := range locMap {
		res.Locations = append(res.Locations, *loc)
	}

	return res, nil
}

func (r *LocationsRepository) UpdateLocationCoordinates(ctx context.Context, merchantID, locationID string, x, y float64) error {

	query := `
        UPDATE locations
        SET current_x = ?, current_y = ?
        WHERE location_id = ? AND merchant_id = ?
    `

	_, err := r.db.ExecContext(ctx, query, x, y, locationID, merchantID)
	return err
}
