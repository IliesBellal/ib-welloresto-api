package locations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type LocationsRepository struct {
	db *sql.DB
}

func NewLocationsRepository(db *sql.DB) *LocationsRepository {
	return &LocationsRepository{db: db}
}

func (r *LocationsRepository) GetLocations(ctx context.Context, merchantID string) (*models.LocationResponse, error) {
	db := dbx.GetDB(ctx, r.db)
	res := &models.LocationResponse{
		Locations: []models.Location{},
		Floors:    []models.Floor{},
		Areas:     []models.Area{},
		Obstacles: []models.Obstacle{},
	}

	// 1. CHARGEMENT DES TABLES (LOCATIONS)
	// On crée une map pour un accès rapide par ID plus tard
	locMap := make(map[string]*models.Location)

	queryLocs := `
		SELECT
			l.location_id, l.location_name, COALESCE(l.location_desc, ''), l.seats,
			l.location_order, l.floor_id, l.shape, l.current_x, l.current_y,
			l.current_width, l.current_height, l.angle, ol.order_id,
			CASE WHEN ol.order_id IS NULL THEN '1' ELSE '0' END as available,
			l.attributes, ol.order_opened_at
		FROM locations l
		LEFT JOIN (
			SELECT DISTINCT ol.location_id, ol.order_id, o.creation_date AS order_opened_at
			FROM order_location ol
			INNER JOIN orders o ON o.order_id = ol.order_id
			WHERE o.state NOT IN ('DELETED','DONE','CANCELED','CLOSED')
			AND o.merchant_id = ?
		) ol ON l.location_id = ol.location_id
		WHERE l.merchant_id = ? AND l.enabled IS TRUE
		ORDER BY l.location_order ASC;`

	rowsLoc, err := db.QueryContext(ctx, queryLocs, merchantID, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsLoc.Close()

	for rowsLoc.Next() {
		var l models.Location
		var attrRaw []byte
		var orderOpenedAt sql.NullTime
		if err := rowsLoc.Scan(
			&l.LocationID, &l.LocationName, &l.LocationDesc, &l.Seats, &l.Order, &l.FloorID,
			&l.Shape, &l.X, &l.Y, &l.W, &l.H, &l.Angle, &l.OpenOrderID, &l.Available,
			&attrRaw, &orderOpenedAt,
		); err != nil {
			return nil, err
		}
		if attrRaw != nil {
			var attrs models.TableAttributes
			if err := json.Unmarshal(attrRaw, &attrs); err == nil {
				l.Attributes = &attrs
			}
		}
		if orderOpenedAt.Valid {
			formatted := orderOpenedAt.Time.UTC().Format(time.RFC3339)
			l.OrderOpenedAt = &formatted
		}
		l.Bookings = []models.Booking{}
		locMap[l.LocationID] = &l
	}

	// 2. CHARGEMENT DES RÉSERVATIONS (BOOKINGS)
	// On gère le fait qu'une résa peut avoir plusieurs tables
	windowStart := "UTC_TIMESTAMP - INTERVAL 5 HOUR"
	windowEnd := "UTC_TIMESTAMP() + INTERVAL 8 HOUR"
	if dbx.ActiveDialect() == dbx.Postgres {
		windowStart = "now() - interval '5 hours'"
		windowEnd = "now() + interval '8 hours'"
	}
	queryBookings := fmt.Sprintf(`
		SELECT
			b.booking_id, b.booking_number, b.status,
			b.sequence_number, b.booking_date_from, b.booking_date_to, b.party_size,
			b.creation_date, b.created_by, b.comment, b.booking_date_from, b.booking_date_to,
			bl.location_id, c.customer_id, c.customer_name, COALESCE(c.customer_tel, '')
		FROM bookings b
		INNER JOIN booked_location bl ON bl.booking_id = b.booking_id
		INNER JOIN customer c ON c.customer_id = b.customer_id
		WHERE b.merchant_id = ? AND b.status = 'ACCEPTED'
		AND b.booking_date_to > %s
		AND b.booking_date_from < %s;`, windowStart, windowEnd)

	rowsBook, err := db.QueryContext(ctx, queryBookings, merchantID)
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
	rowsFloors, err := db.QueryContext(ctx, "SELECT id, name FROM floors WHERE merchant_id = ? AND enabled IS TRUE", merchantID)
	if err == nil {
		defer rowsFloors.Close()
		for rowsFloors.Next() {
			var f models.Floor
			rowsFloors.Scan(&f.ID, &f.Name)
			res.Floors = append(res.Floors, f)
		}
	}

	// 4) AREAS
	rowsAreas, err := db.QueryContext(ctx, `
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

	// 5) OBSTACLES
	obstacles, err := r.GetObstaclesByMerchant(ctx, merchantID)
	if err == nil {
		for _, o := range obstacles {
			res.Obstacles = append(res.Obstacles, models.Obstacle{
				ID:        o.ID,
				FloorID:   o.FloorID,
				Type:      string(o.Type),
				X:         o.X,
				Y:         o.Y,
				Width:     o.Width,
				Height:    o.Height,
				Angle:     o.Angle,
				Direction: o.Direction,
				Enabled:   o.Enabled,
			})
		}
	}

	// FINAL MERGE : Injecter les bookings dans les locations
	for _, loc := range locMap {
		loc.Booking = nextActiveBooking(loc.Bookings)
		res.Locations = append(res.Locations, *loc)
	}

	return res, nil
}

// nextActiveBooking sélectionne, parmi les réservations ACCEPTED déjà
// chargées pour une table (fenêtre calibrée pour le plan de salle live dans
// queryBookings), celle à afficher comme résumé "booking" sur la table dans
// GET /locations : priorité à la réservation future la plus proche ; si
// toutes sont déjà passées (mais encore dans la fenêtre des 5h), on retombe
// sur la plus récente.
func nextActiveBooking(bookings []models.Booking) *models.LocationBooking {
	if len(bookings) == 0 {
		return nil
	}

	now := time.Now().UTC()

	var best models.Booking
	var bestTime time.Time
	haveBest := false
	bestIsFuture := false

	for _, b := range bookings {
		t, err := time.Parse("2006-01-02 15:04:05", b.BookingDateFrom)
		if err != nil {
			continue
		}
		isFuture := t.After(now)

		switch {
		case !haveBest:
			best, bestTime, bestIsFuture, haveBest = b, t, isFuture, true
		case isFuture && !bestIsFuture:
			best, bestTime, bestIsFuture = b, t, true
		case isFuture == bestIsFuture && isFuture && t.Before(bestTime):
			best, bestTime = b, t
		case isFuture == bestIsFuture && !isFuture && t.After(bestTime):
			best, bestTime = b, t
		}
	}

	if !haveBest {
		best = bookings[0]
	}

	startsAt := best.BookingDateFrom
	if t, err := time.Parse("2006-01-02 15:04:05", best.BookingDateFrom); err == nil {
		startsAt = t.UTC().Format(time.RFC3339)
	}

	endsAt := best.BookingDateTo
	if t, err := time.Parse("2006-01-02 15:04:05", best.BookingDateTo); err == nil {
		endsAt = t.UTC().Format(time.RFC3339)
	}

	customerName := ""
	if best.Customer.CustomerName != nil {
		customerName = *best.Customer.CustomerName
	}

	return &models.LocationBooking{
		BookingID:     best.BookingID,
		BookingNumber: best.BookingNumber,
		PartySize:     best.PartySize,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		CustomerName:  customerName,
	}
}

// GetObstaclesByMerchant charge tous les obstacles actifs du plan de salle
// d'un marchand (tous étages confondus), utilisé pour enrichir GetLocations.
func (r *LocationsRepository) GetObstaclesByMerchant(ctx context.Context, merchantID string) ([]Obstacle, error) {
	db := dbx.GetDB(ctx, r.db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, floor_id, type, x, y, width, height, angle, direction
		FROM floor_obstacles
		WHERE merchant_id = ? AND enabled = TRUE`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	obstacles := []Obstacle{}
	for rows.Next() {
		var o Obstacle
		if err := rows.Scan(&o.ID, &o.FloorID, &o.Type, &o.X, &o.Y, &o.Width, &o.Height, &o.Angle, &o.Direction); err != nil {
			return nil, err
		}
		o.Enabled = true
		obstacles = append(obstacles, o)
	}

	return obstacles, nil
}

// GetObstacleByID récupère un obstacle pour vérifier son appartenance au
// marchand (et donc, indirectement, au floor demandé dans l'URL).
func (r *LocationsRepository) GetObstacleByID(ctx context.Context, merchantID, obstacleID string) (Obstacle, error) {
	db := dbx.GetDB(ctx, r.db)

	var o Obstacle
	err := db.QueryRowContext(ctx, `
		SELECT id, floor_id, type, x, y, width, height, angle, direction
		FROM floor_obstacles
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE`, obstacleID, merchantID,
	).Scan(&o.ID, &o.FloorID, &o.Type, &o.X, &o.Y, &o.Width, &o.Height, &o.Angle, &o.Direction)
	if err == sql.ErrNoRows {
		return Obstacle{}, models.ErrNotFound
	}
	if err != nil {
		return Obstacle{}, err
	}
	o.Enabled = true

	return o, nil
}

// FloorExists vérifie que le floor_id appartient bien au marchand (utilisé
// par le service pour valider CreateObstacle).
func (r *LocationsRepository) FloorExists(ctx context.Context, merchantID, floorID string) (bool, error) {
	db := dbx.GetDB(ctx, r.db)

	var exists int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM floors WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`,
		floorID, merchantID,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *LocationsRepository) CreateObstacle(ctx context.Context, merchantID string, req CreateObstacleRequest) (string, error) {
	db := dbx.GetDB(ctx, r.db)

	obstacleID := helpers.GeneratePrefixedID(helpers.ObstacleIDPrefix)

	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO floor_obstacles
		(id, floor_id, merchant_id, type, x, y, width, height, angle, direction, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, %s)`, dbx.UTCNow()),
		obstacleID, req.FloorID, merchantID, req.Type, req.X, req.Y, req.Width, req.Height, req.Angle, req.Direction,
	)
	if err != nil {
		return "", err
	}

	return obstacleID, nil
}

func (r *LocationsRepository) UpdateObstacle(ctx context.Context, merchantID, obstacleID string, req UpdateObstacleRequest) error {
	db := dbx.GetDB(ctx, r.db)

	res, err := db.ExecContext(ctx, `
		UPDATE floor_obstacles SET
			type      = COALESCE(?, type),
			x         = COALESCE(?, x),
			y         = COALESCE(?, y),
			width     = COALESCE(?, width),
			height    = COALESCE(?, height),
			angle     = COALESCE(?, angle),
			direction = COALESCE(?, direction)
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		req.Type, req.X, req.Y, req.Width, req.Height, req.Angle, req.Direction,
		obstacleID, merchantID,
	)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *LocationsRepository) DeleteObstacle(ctx context.Context, merchantID, obstacleID string) error {
	db := dbx.GetDB(ctx, r.db)

	res, err := db.ExecContext(ctx, `
		UPDATE floor_obstacles SET enabled = FALSE
		WHERE id = ? AND merchant_id = ? AND enabled = TRUE`,
		obstacleID, merchantID,
	)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrNotFound
	}

	return nil
}

// CreateArea insère une nouvelle zone-conteneur (floor_area). L'appartenance
// de req.FloorID au marchand est vérifiée en amont par le service via
// FloorExists — floor_areas ne porte pas de colonne merchant_id, seulement
// floor_id (voir migration 050_baseline_floorplan).
func (r *LocationsRepository) CreateArea(ctx context.Context, req CreateAreaRequest) (string, error) {
	db := dbx.GetDB(ctx, r.db)

	// floor_areas.id is an auto-increment identity column — same fix as
	// CreateFloor above (read back the generated id instead of inserting a
	// client-generated prefixed string). Changes the returned id format.
	pointsJSON, _ := json.Marshal(req.Points)

	id, err := db.InsertReturningID(ctx, `
		INSERT INTO floor_areas
		(floor_id, name, stroke_color, color, x, y, points, angle, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE)`,
		"id", req.FloorID, req.Name, req.StrokeColor, req.Color, req.X, req.Y, pointsJSON, req.Angle,
	)
	if err != nil {
		return "", err
	}
	areaID := strconv.FormatInt(id, 10)

	return areaID, nil
}

func (r *LocationsRepository) UpdateArea(ctx context.Context, merchantID, areaID string, req UpdateAreaRequest) error {
	db := dbx.GetDB(ctx, r.db)

	var pointsJSON []byte
	if req.Points != nil {
		pointsJSON, _ = json.Marshal(*req.Points)
	}

	res, err := db.ExecContext(ctx, `
		UPDATE floor_areas SET
			name         = COALESCE(?, name),
			stroke_color = COALESCE(?, stroke_color),
			color        = COALESCE(?, color),
			x            = COALESCE(?, x),
			y            = COALESCE(?, y),
			points       = COALESCE(?, points),
			angle        = COALESCE(?, angle)
		WHERE id = ? AND enabled = TRUE
			AND floor_id IN (SELECT id FROM floors WHERE merchant_id = ?)`,
		req.Name, req.StrokeColor, req.Color, req.X, req.Y, pointsJSON, req.Angle,
		areaID, merchantID,
	)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *LocationsRepository) DeleteArea(ctx context.Context, merchantID, areaID string) error {
	db := dbx.GetDB(ctx, r.db)

	res, err := db.ExecContext(ctx, `
		UPDATE floor_areas SET enabled = FALSE
		WHERE id = ? AND enabled = TRUE
			AND floor_id IN (SELECT id FROM floors WHERE merchant_id = ?)`,
		areaID, merchantID,
	)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrNotFound
	}

	return nil
}

func (r *LocationsRepository) CreateTable(ctx context.Context, merchantID, floorID string, req CreateTableRequest) (string, error) {
	db := dbx.GetDB(ctx, r.db)

	// locations.location_id is an auto-increment identity column — same fix
	// as CreateFloor/CreateArea above (read back the generated id instead of
	// inserting a client-generated prefixed string). Changes the returned id
	// format.
	query := `
		INSERT INTO locations
		(merchant_id, floor_id, location_name, seats, shape, current_x, current_y, current_width, current_height, angle, location_order, enabled, attributes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, TRUE, ?)
	`

	// Obtenir le prochain location_order pour ce floor
	var maxOrder sql.NullInt64
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(location_order), 0) FROM locations WHERE floor_id = ? AND enabled = TRUE`,
		floorID,
	).Scan(&maxOrder)

	nextOrder := int(maxOrder.Int64) + 1

	var attrJSON []byte
	if req.Attributes != nil {
		attrJSON, _ = json.Marshal(req.Attributes)
	}

	id, err := db.InsertReturningID(ctx, query, "location_id",
		merchantID, floorID, req.LocationName, req.Seats, req.Shape,
		req.X, req.Y, req.Width, req.Height, req.Angle, nextOrder, attrJSON,
	)

	if err != nil {
		return "", err
	}

	return strconv.FormatInt(id, 10), nil
}

func (r *LocationsRepository) UpdateTable(ctx context.Context, merchantID, locationID string, req UpdateTableRequest) error {
	db := dbx.GetDB(ctx, r.db)
	angle := req.TableAngle()

	query := `
		UPDATE locations
		SET
			location_name = COALESCE(?, location_name),
			location_order = COALESCE(?, location_order),
			floor_id = COALESCE(?, floor_id),
			seats = COALESCE(?, seats),
			shape = COALESCE(?, shape),
			current_x = COALESCE(?, current_x),
			current_y = COALESCE(?, current_y),
			current_width = COALESCE(?, current_width),
			current_height = COALESCE(?, current_height),
			angle = COALESCE(?, angle),
			enabled = COALESCE(?, enabled),
			attributes = COALESCE(?, attributes)
		WHERE location_id = ? AND merchant_id = ?
	`

	var attrJSON []byte
	if req.Attributes != nil {
		attrJSON, _ = json.Marshal(req.Attributes)
	}

	_, err := db.ExecContext(ctx, query,
		req.LocationName, req.LocationOrder, req.FloorID, req.Seats, req.Shape,
		req.X, req.Y, req.Width, req.Height, angle, req.Enabled, attrJSON,
		locationID, merchantID,
	)

	return err
}

func (r *LocationsRepository) DeleteTable(ctx context.Context, merchantID, locationID string) error {
	db := dbx.GetDB(ctx, r.db)

	query := `
		UPDATE locations
		SET enabled = FALSE
		WHERE location_id = ? AND merchant_id = ?
	`

	_, err := db.ExecContext(ctx, query, locationID, merchantID)
	return err
}

func (r *LocationsRepository) UpdateFloor(ctx context.Context, merchantID, floorID, name string) error {
	db := dbx.GetDB(ctx, r.db)

	res, err := db.ExecContext(ctx,
		`UPDATE floors SET name = ? WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`,
		name, floorID, merchantID,
	)
	if err != nil {
		return err
	}

	// RowsAffected = 0 quand l'étage n'existe pas pour ce marchand… mais aussi
	// quand le nom envoyé est identique : on distingue par une lecture.
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT 1 FROM floors WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`,
			floorID, merchantID,
		).Scan(&exists)
		if err == sql.ErrNoRows {
			return models.ErrFloorNotFound
		}
		return err
	}

	return nil
}

func (r *LocationsRepository) DeleteFloor(ctx context.Context, merchantID, floorID string) error {
	db := dbx.GetDB(ctx, r.db)

	var activeTables int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM locations WHERE floor_id = ? AND merchant_id = ? AND enabled IS TRUE`,
		floorID, merchantID,
	).Scan(&activeTables)
	if err != nil {
		return err
	}
	if activeTables > 0 {
		return models.ErrFloorNotEmpty
	}

	res, err := db.ExecContext(ctx,
		`UPDATE floors SET enabled = FALSE WHERE id = ? AND merchant_id = ? AND enabled IS TRUE`,
		floorID, merchantID,
	)
	if err != nil {
		return err
	}

	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return models.ErrFloorNotFound
	}

	return nil
}

func (r *LocationsRepository) CreateFloor(ctx context.Context, merchantID, name string) (string, error) {
	db := dbx.GetDB(ctx, r.db)

	// floors.id is an auto-increment identity column, not a client-generated
	// prefixed id (unlike most other Wello Resto entities) — MySQL silently
	// coerced the previous prefixed-string insert to 0 and auto-generated a
	// real id anyway, but then returned the WRONG (prefixed-string) id to the
	// caller; Postgres's GENERATED ALWAYS rejects the explicit value outright.
	// Fixed to read back the real generated id. NOTE: this changes the id
	// format returned by this endpoint from a prefixed string ("flr-xxx") to
	// a plain integer string ("42") — frontend clients must be updated to
	// treat floor ids as opaque strings rather than assuming a "flr-" prefix.
	query := `INSERT INTO floors (merchant_id, name, enabled) VALUES (?, ?, TRUE)`

	id, err := db.InsertReturningID(ctx, query, "id", merchantID, name)
	if err != nil {
		return "", err
	}

	return strconv.FormatInt(id, 10), nil
}
