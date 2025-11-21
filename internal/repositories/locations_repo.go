package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type LocationsRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewLocationsRepository(db *sql.DB, log *zap.Logger) *LocationsRepository {
	return &LocationsRepository{db: db, log: log}
}

func (r *LocationsRepository) GetLocations(ctx context.Context, merchantID string) ([]models.Location, error) {
	r.log.Info("GetLocations START", zap.String("merchant_id", merchantID))

	// ---------------------------------------------
	// 1) LOCATIONS + TABLES OUVERTES
	// ---------------------------------------------
	queryLocations := fmt.Sprintf(`
		SELECT DISTINCT 
			l.location_id, l.location_name, l.location_desc, l.seats, 
			l.location_order, l.floor_id, l.shape, l.current_x, l.current_y,
			l.current_width, l.current_height, l.angle, ol.order_id,
			CASE WHEN ol.order_id IS NULL THEN '1' ELSE '0' END as available 
		FROM locations l
		LEFT JOIN (
			SELECT DISTINCT ol.location_id, ol.order_id
			FROM order_location ol
			INNER JOIN orders o ON o.order_id = ol.order_id
			WHERE o.state NOT IN ('DELETED','DONE','CANCELED','CLOSED')
			AND o.merchant_id = %s
		) ol ON l.location_id = ol.location_id
		WHERE l.merchant_id = %s
		AND l.enabled IS TRUE
		ORDER BY l.location_order ASC;
	`, merchantID, merchantID)

	rowsLoc, err := r.db.QueryContext(ctx, queryLocations)
	if err != nil {
		r.log.Error("locations query error", zap.Error(err))
		return nil, err
	}
	defer rowsLoc.Close()

	locations := []models.Location{}

	for rowsLoc.Next() {
		var l models.Location
		err := rowsLoc.Scan(
			&l.LocationID, &l.LocationName, &l.LocationDesc, &l.Seats, &l.Order, &l.FloorID,
			&l.Shape, &l.X, &l.Y, &l.W, &l.H, &l.Angle, &l.OpenOrderID, &l.Available,
		)
		if err != nil {
			return nil, err
		}
		locations = append(locations, l)
	}

	// ---------------------------------------------
	// 2) BOOKINGS
	// ---------------------------------------------
	queryBookings := fmt.Sprintf(`
		SELECT 
			b.booking_id, b.booking_number, b.comment, b.party_size, 
			bl.location_id, b.booking_date_from, b.booking_date_to, 
			b.booking_duration,
			c.customer_id, c.customer_name, c.customer_tel
		FROM bookings b
		INNER JOIN booked_location bl ON bl.booking_id = b.booking_id
		INNER JOIN locations l ON l.location_id = bl.location_id
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.merchant_id = %s
		AND b.status IN ('ACCEPTED')
		AND b.booking_date_to > UTC_TIMESTAMP - INTERVAL 5 HOUR;
	`, merchantID)

	rowsBook, err := r.db.QueryContext(ctx, queryBookings)
	if err != nil {
		r.log.Error("bookings query error", zap.Error(err))
		return nil, err
	}
	defer rowsBook.Close()

	bookings := []map[string]interface{}{}

	for rowsBook.Next() {
		var (
			bookingID, bookingNumber, comment, locationID string
			partySize                                     int
			dateFrom, dateTo                              sql.NullString
			duration                                      sql.NullString
			customerID, customerName, customerTel         string
		)

		err := rowsBook.Scan(
			&bookingID, &bookingNumber, &comment, &partySize,
			&locationID, &dateFrom, &dateTo, &duration,
			&customerID, &customerName, &customerTel,
		)
		if err != nil {
			return nil, err
		}

		bookings = append(bookings, map[string]interface{}{
			"booking_id":        bookingID,
			"booking_number":    bookingNumber,
			"comment":           comment,
			"party_size":        partySize,
			"location_id":       locationID,
			"booking_date_from": dateFrom.String,
			"booking_date_to":   dateTo.String,
			"customer": map[string]interface{}{
				"customer_id":   customerID,
				"customer_name": customerName,
				"customer_tel":  customerTel,
			},
		})
	}

	// ---------------------------------------------
	// 3) FLOORS
	// ---------------------------------------------
	rowsFloors, err := r.db.QueryContext(ctx, `
		SELECT id, name
		FROM floors
		WHERE merchant_id = ?
		AND enabled IS TRUE;
	`, merchantID)

	if err != nil {
		r.log.Error("floors query error", zap.Error(err))
		return nil, err
	}
	defer rowsFloors.Close()

	floors := []map[string]interface{}{}
	for rowsFloors.Next() {
		var id string
		var name string
		rowsFloors.Scan(&id, &name)

		floors = append(floors, map[string]interface{}{
			"id":   id,
			"name": name,
		})
	}

	// ---------------------------------------------
	// 4) AREAS
	// ---------------------------------------------
	rowsAreas, err := r.db.QueryContext(ctx, `
		SELECT fa.id, fa.floor_id, fa.name, fa.points, fa.x, fa.y, fa.angle, fa.stroke_color, fa.color
		FROM floor_areas fa
		INNER JOIN floors f ON f.id = fa.floor_id
		WHERE f.merchant_id = ?
		AND fa.enabled IS TRUE
		AND f.enabled IS TRUE;
	`, merchantID)

	if err != nil {
		r.log.Error("areas query error", zap.Error(err))
		return nil, err
	}
	defer rowsAreas.Close()

	areas := []map[string]interface{}{}
	for rowsAreas.Next() {
		var id, floorID, name, points, x, y, angle, strokeColor, color sql.NullString

		rowsAreas.Scan(&id, &floorID, &name, &points, &x, &y, &angle, &strokeColor, &color)

		var pointsParsed interface{}
		json.Unmarshal([]byte(points.String), &pointsParsed)

		areas = append(areas, map[string]interface{}{
			"id":           id.String,
			"floor_id":     floorID.String,
			"name":         name.String,
			"points":       pointsParsed,
			"x":            x.String,
			"y":            y.String,
			"angle":        angle.String,
			"stroke_color": strokeColor.String,
			"color":        color.String,
		})
	}

	// ---------------------------------------------
	// FINAL MERGE : locations + bookings associés
	// ---------------------------------------------
	finalLocations := []map[string]interface{}{}

	for _, l := range locations {
		locBookings := []map[string]interface{}{}
		for _, b := range bookings {
			if b["location_id"] == l.LocationID {
				locBookings = append(locBookings, b)
			}
		}

		finalLocations = append(finalLocations, map[string]interface{}{
			"location_id":    l.LocationID,
			"location_name":  l.LocationName,
			"location_desc":  l.LocationDesc,
			"seats":          l.Seats,
			"available":      l.Available,
			"location_order": l.Order,
			"floor_id":       l.FloorID,
			"shape":          l.Shape.String,
			"current_x":      l.X.String,
			"current_y":      l.Y.String,
			"current_width":  l.W.String,
			"current_height": l.H.String,
			"angle":          l.Angle.String,
			"open_order_id":  l.OpenOrderID.String,
			"bookings":       locBookings,
		})
	}

	// ---------------------------------------------
	// RETURN JSON IDENTIQUE À PHP
	// ---------------------------------------------

	/*
		return map[string]interface{}{
			"locations": finalLocations,
			"floors":    floors,
			"areas":     areas,
			"bookings":  bookings,
		}, nil

	*/

	return nil, nil
}
