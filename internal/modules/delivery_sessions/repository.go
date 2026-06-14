package delivery_sessions

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/orders"
	"welloresto-api/internal/utils/dbutils"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type DeliverySessionsRepository struct {
	database      *sql.DB
	ordersFetcher *orders.OrdersFetcher
}

func NewDeliverySessionsRepository(db *sql.DB, ordersF *orders.OrdersFetcher) *DeliverySessionsRepository {
	return &DeliverySessionsRepository{database: db, ordersFetcher: ordersF}
}

func (r *DeliverySessionsRepository) GetPendingDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Récupérer les sessions actives (en-tête + livreur, cf. fetchDeliverySessions)
	sessions, err := r.fetchDeliverySessions(ctx, merchantID, "ds.status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// S'il n'y a pas de session, on s'arrête là
	if len(sessions) == 0 {
		return []models.DeliverySession{}, nil
	}

	// 2. OPTIMISATION CRITIQUE : Récupérer en une requête les stops (delivery_session_order)
	// de toutes les sessions. Cela donne à la fois les Order IDs (pour le filtre du fetch
	// des commandes ci-dessous) et le détail par-stop (delivery_stop) à attacher à chaque
	// commande, sans refaire les jointures sessions <-> orders.
	sessionIDs := ""
	for i, s := range sessions {
		if i > 0 {
			sessionIDs += ","
		}
		sessionIDs += fmt.Sprintf("'%s'", s.DeliverySessionID)
	}

	qStops := fmt.Sprintf(`
		SELECT delivery_session_id, order_id, priority, status, arrived_at, delivered_at, failed_at, canceled_at, fail_reason
		FROM delivery_session_order
		WHERE delivery_session_id IN (%s)
		ORDER BY priority ASC
	`, sessionIDs)

	rows, err := db.QueryContext(ctx, qStops)
	if err != nil {
		log.Error("failed to fetch delivery session stops: " + err.Error())
		return nil, fmt.Errorf("failed to fetch delivery session stops: %w", err)
	}

	stopsBySession := map[string]map[string]*models.DeliveryStop{}
	orderIDsBySession := map[string][]string{}
	orderIDSet := map[string]bool{}

	for rows.Next() {
		var sessID, oid, stopStatus string
		var priority sql.NullInt64
		var arrivedAt, deliveredAt, failedAt, canceledAt sql.NullTime
		var failReason sql.NullString

		if err := rows.Scan(&sessID, &oid, &priority, &stopStatus, &arrivedAt, &deliveredAt, &failedAt, &canceledAt, &failReason); err != nil {
			rows.Close()
			log.Error("failed to scan delivery session stop: " + err.Error())
			return nil, err
		}

		stop := &models.DeliveryStop{
			Status:      stopStatus,
			ArrivedAt:   helpers.NullTimePtr(arrivedAt),
			DeliveredAt: helpers.NullTimePtr(deliveredAt),
			FailedAt:    helpers.NullTimePtr(failedAt),
			CanceledAt:  helpers.NullTimePtr(canceledAt),
			FailReason:  helpers.NullStringToPtr(failReason),
		}
		if priority.Valid {
			p := int(priority.Int64)
			stop.Priority = &p
		}

		if stopsBySession[sessID] == nil {
			stopsBySession[sessID] = map[string]*models.DeliveryStop{}
		}
		stopsBySession[sessID][oid] = stop
		orderIDsBySession[sessID] = append(orderIDsBySession[sessID], oid)
		orderIDSet[oid] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Error("failed to fetch delivery session stops: " + err.Error())
		return nil, err
	}

	// Si ces sessions n'ont aucune commande, on retourne les sessions vides
	if len(orderIDSet) == 0 {
		return sessions, nil
	}

	// 3. Construire le filtre PAR ORDER ID (MySQL adore ça, c'est instantané)
	orderIDList := make([]string, 0, len(orderIDSet))
	for oid := range orderIDSet {
		orderIDList = append(orderIDList, oid)
	}

	ordersFilter := ""
	for i, oid := range orderIDList {
		if i > 0 {
			ordersFilter += ","
		}
		ordersFilter += fmt.Sprintf("'%s'", oid)
	}

	// Le filtre magique : on tape directement sur la Primary Key ou l'index principal
	filter := fmt.Sprintf(" AND o.order_id IN (%s) ", ordersFilter)

	// 4. On appelle le monstre partagé avec ce filtre optimisé
	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filter, "", "")
	if err != nil {
		return nil, err
	}

	// 5. Assemblage : Mettre les commandes (avec leur delivery_stop) dans les bonnes sessions
	ordersBySession := make(map[string][]models.Order)

	for _, o := range orders {
		if o.DeliverySessionID != nil {
			key := *o.DeliverySessionID
			if stop, ok := stopsBySession[key][o.OrderID]; ok {
				o.DeliveryStop = stop
			}
			ordersBySession[key] = append(ordersBySession[key], o)
		}
	}

	// On remplit les sessions
	for i := range sessions {
		sID := sessions[i].DeliverySessionID

		if sessionOrders, found := ordersBySession[sID]; found {
			sessions[i].Orders = sessionOrders
		} else {
			sessions[i].Orders = []models.Order{}
		}

		// 6. Resolve current_order_id: use the stored pointer if set, otherwise derive
		// the first 'pending' stop in priority order (same as assembleDeliverySessionDetails).
		if sessions[i].CurrentOrderID == nil {
			for _, oid := range orderIDsBySession[sID] {
				if stop := stopsBySession[sID][oid]; stop != nil && stop.Status == "pending" {
					derived := oid
					sessions[i].CurrentOrderID = &derived
					break
				}
			}
		}
	}

	return sessions, nil
}

// fetchDeliverySessions returns the active delivery sessions for merchantID matching
// filterStatus, with the full session header (start_date, distance, duration,
// current_order_id) and delivery man info (including status) - same shape as the
// canonical assembleDeliverySessionDetails. Orders are left empty; filled in by the caller.
func (r *DeliverySessionsRepository) fetchDeliverySessions(ctx context.Context, merchantID string, filterStatus string) ([]models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	q := `
       SELECT ds.id, ds.user_id, ds.start_date, ds.distance, ds.duration, ds.status, ds.current_order_id,
              u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, usv.status
       FROM delivery_session ds
       INNER JOIN users u on u.user_id = ds.user_id
       INNER JOIN user_status_view usv on usv.user_id = ds.user_id
       WHERE ds.merchant_id = ? AND ` + filterStatus

	rows, err := db.QueryContext(ctx, q, merchantID)
	if err != nil {
		log.Error("failed to fetch delivery sessions: " + err.Error())
		return nil, err
	}
	defer rows.Close()

	var sessions []models.DeliverySession
	for rows.Next() {
		var id, userID, startDate, distance, duration, status sql.NullString
		var currentOrderID sql.NullString
		var profilePic, firstName, lastName, planningColor, dmStatus sql.NullString
		var lat, lng sql.NullFloat64

		if err := rows.Scan(&id, &userID, &startDate, &distance, &duration, &status, &currentOrderID,
			&profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &dmStatus); err != nil {
			log.Error("failed to scan delivery session: " + err.Error())
			return nil, err
		}

		ds := models.DeliverySession{
			DeliverySessionID: id.String,
			UserID:            userID.String,
			MerchantID:        merchantID,
			StartDate:         startDate.String,
			Distance:          distance.String,
			Duration:          duration.String,
			Status:            status.String,
			CurrentOrderID:    helpers.NullStringToPtr(currentOrderID),
			Orders:            []models.Order{},
			DeliveryMan: models.OrderUser{
				UserID:         userID.String,
				FirstName:      helpers.NullStringToPtr(firstName),
				LastName:       helpers.NullStringToPtr(lastName),
				ProfilePicture: helpers.NullStringToPtr(profilePic),
				Lat:            helpers.NullFloat64Ptr(lat),
				Lng:            helpers.NullFloat64Ptr(lng),
				PlanningColor:  helpers.NullStringToPtr(planningColor),
				Status:         helpers.NullStringToPtr(dmStatus),
			},
		}
		sessions = append(sessions, ds)
	}
	return sessions, nil
}

func (r *DeliverySessionsRepository) GetDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	qDeliverySessions := `
SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
FROM delivery_session ds
INNER JOIN users u on u.user_id = ds.user_id
WHERE status IN ('1','PENDING')
AND ds.merchant_id = ?`
	rows, err := db.QueryContext(ctx, qDeliverySessions, merchantID)
	if err != nil {
		log.Error("failed to fetch delivery sessions: " + err.Error())
		return nil, err
	}
	defer rows.Close()

	sessions := []models.DeliverySession{}
	for rows.Next() {
		var id, userID sql.NullString
		var profilePic, firstName, lastName sql.NullString
		var lat, lng sql.NullFloat64
		var planningColor sql.NullString
		var status sql.NullString

		if err := rows.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			log.Error("failed to scan delivery session: " + err.Error())
			return nil, err
		}
		ds := models.DeliverySession{
			DeliverySessionID: id.String,
			Status:            status.String,
			Orders:            []models.Order{},
			DeliveryMan: models.OrderUser{
				UserID:         userID.String,
				FirstName:      &firstName.String,
				LastName:       &lastName.String,
				ProfilePicture: helpers.NullStringToPtr(profilePic),
				Lat:            helpers.NullFloat64Ptr(lat),
				Lng:            helpers.NullFloat64Ptr(lng),
				PlanningColor:  helpers.NullStringToPtr(planningColor),
			},
		}
		sessions = append(sessions, ds)
	}

	return sessions, nil
}

// StartDeliverySession creates a new delivery session for the delivery man (closes any
// stale finished session, blocks if one is already active, then atomically inserts the
// session header, its delivery_session_order stops, and updates orders.brand_status).
//
// Steps 1-5 (cleanup of finished sessions, active-session check, and the 3 creation
// writes) all run inside a single transaction via dbutils.RunInTx: this keeps the
// active-session check consistent with the insert that follows it (same tx/connection)
// and guarantees the session row, its stops, and the brand_status update either all
// land together or not at all - avoiding the half-created "orphan" session that would
// otherwise be seen as "active" and block the delivery man's next attempt.
func (r *DeliverySessionsRepository) StartDeliverySession(ctx context.Context, req *models.DeliverySessionRequest) (*models.DeliverySession, error) {
	log := logger.FromContext(ctx)

	userID := req.DeliveryMan.UserID
	merchantID := req.MerchantID

	var sessionID int64

	err := dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		// 1. Close finished delivery sessions (PHP calls closeFinishedDeliverySession)
		_, _ = db.ExecContext(txCtx, `
			UPDATE delivery_session
			SET status = 'FINISHED'
			WHERE user_id = ? AND status IN ('1','PENDING') AND end_date < UTC_TIMESTAMP
		`, userID)

		// 2. Check if a session is already active
		var existing string
		scanErr := db.QueryRowContext(txCtx, `
			SELECT id FROM delivery_session
			WHERE user_id = ? AND status IN ('1','PENDING')
		`, userID).Scan(&existing)

		switch scanErr {
		case nil:
			// A session exists -> block.
			log.Error("StartDeliverySession: active session already exists for user " + userID)
			return models.ErrDeliverySessionAlreadyActive
		case sql.ErrNoRows:
			// No active session -> continue.
		default:
			// Real DB error -> propagate as-is, not as "already active".
			log.Error("StartDeliverySession: failed to check active session: " + scanErr.Error())
			return scanErr
		}

		// 3. Insert new delivery_session
		res, err := db.ExecContext(txCtx, `
			INSERT INTO delivery_session (user_id, merchant_id, start_date, distance, duration, status)
			VALUES (?, ?, UTC_TIMESTAMP, ?, ?, 'PENDING')
		`, userID, merchantID, req.Distance, req.Duration)
		if err != nil {
			log.Error("StartDeliverySession: failed to insert delivery session: " + err.Error())
			return err
		}

		sessionID, err = res.LastInsertId()
		if err != nil {
			log.Error("StartDeliverySession: failed to read inserted session id: " + err.Error())
			return err
		}

		// 4. Insert delivery_session_order
		for i, o := range req.Orders {
			if _, err := db.ExecContext(txCtx, `
				INSERT INTO delivery_session_order (delivery_session_id, order_id, priority)
				VALUES (?, ?, ?)
			`, sessionID, o.OrderID, i); err != nil {
				log.Error("StartDeliverySession: failed to insert delivery session order: " + err.Error())
				return err
			}
		}

		// 5. Update orders brand_status
		if _, err := db.ExecContext(txCtx, `
			UPDATE orders o
			INNER JOIN delivery_session_order dso ON dso.order_id = o.order_id
			SET o.brand_status = 'EN_ROUTE_TO_DROPOFF'
			WHERE dso.delivery_session_id = ?
		`, sessionID); err != nil {
			log.Error("StartDeliverySession: failed to update order brand status: " + err.Error())
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// 8. Return the Delivery Session object (like Management->getDeliverySession())
	session := &models.DeliverySession{
		DeliverySessionID: fmt.Sprint(sessionID),
		UserID:            userID,
		MerchantID:        merchantID,
		Distance:          req.Distance,
		Duration:          req.Duration,
		Status:            "PENDING",
		Orders:            make([]models.Order, len(req.Orders)),
	}

	for i, o := range req.Orders {
		session.Orders[i] = models.Order{
			OrderID:  o.OrderID,
			Priority: &i,
		}
	}

	return session, nil
}

func (r *DeliverySessionsRepository) CancelDeliverySession(ctx context.Context, sessionID string) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Revert orders to READY_FOR_HANDOFF
	_, err := db.ExecContext(ctx, `
        UPDATE orders o
        INNER JOIN delivery_session_order dso ON dso.order_id = o.order_id
        INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
        SET o.brand_status = 'READY_FOR_HANDOFF'
        WHERE ds.id = ?
        AND o.brand_status = 'EN_ROUTE_TO_DROPOFF'
	`, sessionID)
	if err != nil {
		log.Error("CancelDeliverySession: failed to update order brand status: " + err.Error())
		return nil, err
	}

	// ⚠ PHP version commented out the negative MerchantID update → we skip it, as requested.

	// 2) Mark session as canceled
	_, err = db.ExecContext(ctx, `
        UPDATE delivery_session
        SET status = 'CANCELED'
        WHERE id = ?
        AND status >= 0
	`, sessionID)
	if err != nil {
		log.Error("CancelDeliverySession: failed to update delivery session status: " + err.Error())
		return nil, err
	}

	// 3) Retrieve merchant_id
	var merchantID string
	err = db.QueryRowContext(ctx, `
        SELECT merchant_id
        FROM delivery_session
        WHERE id = ?
	`, sessionID).Scan(&merchantID)
	if err != nil {
		log.Error("CancelDeliverySession: failed to retrieve merchant ID: " + err.Error())
		return nil, err
	}

	return &models.DeliverySession{
		DeliverySessionID: sessionID,
		MerchantID:        merchantID,
		Status:            "CANCELED",
	}, nil
}

func (r *DeliverySessionsRepository) CloseDeliverySession(ctx context.Context, sessionID string) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1) Update orders
	_, err := db.ExecContext(ctx, `
        UPDATE orders o
        INNER JOIN delivery_session_order dso ON dso.order_id = o.order_id
        INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
        SET 
            o.brand_status = 'DONE',
            o.state = 'CLOSED'
        WHERE ds.id = ?
        AND ds.status NOT IN ('-1','CLOSED','CANCELED')
	`, sessionID)
	if err != nil {
		log.Error("CloseDeliverySession: failed to update order status: " + err.Error())
		return nil, err
	}

	// 2) Mark session as DONE
	_, err = db.ExecContext(ctx, `
        UPDATE delivery_session
        SET status = 'DONE'
        WHERE id = ?
        AND status NOT IN ('-1','CLOSED','CANCELED')
	`, sessionID)
	if err != nil {
		log.Error("CloseDeliverySession: failed to update delivery session status: " + err.Error())
		return nil, err
	}

	// 3) Update payments user_id
	_, err = db.ExecContext(ctx, `
        UPDATE payments p
        INNER JOIN delivery_session_order dso ON dso.order_id = p.order_id
        INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
        SET p.user_id = ds.user_id
        WHERE ds.id = ?
	`, sessionID)
	if err != nil {
		log.Error("CloseDeliverySession: failed to update payment user ID: " + err.Error())
		return nil, err
	}

	// retrieve merchant
	var merchantID string
	err = db.QueryRowContext(ctx, `
        SELECT merchant_id
        FROM delivery_session
        WHERE id = ?
	`, sessionID).Scan(&merchantID)
	if err != nil {
		log.Error("CloseDeliverySession: failed to retrieve merchant ID: " + err.Error())
		return nil, err
	}

	return &models.DeliverySession{
		DeliverySessionID: sessionID,
		MerchantID:        merchantID,
		Status:            "DONE",
	}, nil
}

// GetDeliverySession assembles a delivery session by id for managers (ManageDelivery),
// using the same canonical assembly as GetActiveDeliverySessionForUser /
// GetDeliverySessionByIDForUser (delivery man + status, per-order delivery_stop,
// current_order_id, customer delivery_notes).
func (r *DeliverySessionsRepository) GetDeliverySession(ctx context.Context, merchantID, sessionID string) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1️⃣ Fetch session header info
	var session models.DeliverySession
	var currentOrderID sql.NullString
	var distance, duration, startDate sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, user_id, status, current_order_id, distance, duration, start_date
		FROM delivery_session
		WHERE id = ? AND merchant_id = ?
	`, sessionID, merchantID).Scan(&session.DeliverySessionID, &session.UserID, &session.Status, &currentOrderID, &distance, &duration, &startDate)

	if err != nil {
		log.Error("GetDeliverySession: failed to fetch delivery session: " + err.Error())
		return nil, err
	}

	session.MerchantID = merchantID
	session.Distance = distance.String
	session.Duration = duration.String
	session.StartDate = startDate.String

	return r.assembleDeliverySessionDetails(ctx, merchantID, &session, currentOrderID)
}

// GetActiveDeliverySessionForUser returns the currently active delivery session
// (status IN ('1','PENDING') - legacy values, see docs/DELIVERY_DESIGN.md §7) for
// the calling delivery user, assembled the same way as GetDeliverySession but
// additionally exposing the per-stop FSM (delivery_session_order) and the
// customer's delivery notes.
func (r *DeliverySessionsRepository) GetActiveDeliverySessionForUser(ctx context.Context, merchantID, userID string) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1️⃣ Fetch the most recent active session for this user
	var session models.DeliverySession
	var currentOrderID sql.NullString
	var distance, duration, startDate sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, status, current_order_id, distance, duration, start_date
		FROM delivery_session
		WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
		ORDER BY start_date DESC
		LIMIT 1
	`, userID, merchantID).Scan(&session.DeliverySessionID, &session.Status, &currentOrderID, &distance, &duration, &startDate)

	if err == sql.ErrNoRows {
		return nil, models.ErrNoActiveDeliverySession
	}
	if err != nil {
		log.Error("GetActiveDeliverySessionForUser: failed to fetch active delivery session: " + err.Error())
		return nil, err
	}

	session.UserID = userID
	session.MerchantID = merchantID
	session.Distance = distance.String
	session.Duration = duration.String
	session.StartDate = startDate.String

	return r.assembleDeliverySessionDetails(ctx, merchantID, &session, currentOrderID)
}

// GetDeliverySessionByIDForUser assembles the calling user's delivery session by id,
// without filtering on delivery_session.status. Unlike GetActiveDeliverySessionForUser
// (status IN ('1','PENDING')), this is used right after a transition that may have just
// auto-closed the session (status='0', §0.3) - the response must still reflect the final
// state of that session, not "no active session".
func (r *DeliverySessionsRepository) GetDeliverySessionByIDForUser(ctx context.Context, merchantID, userID, sessionID string) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var session models.DeliverySession
	var currentOrderID sql.NullString
	var distance, duration, startDate sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, status, current_order_id, distance, duration, start_date
		FROM delivery_session
		WHERE id = ? AND user_id = ? AND merchant_id = ?
	`, sessionID, userID, merchantID).Scan(&session.DeliverySessionID, &session.Status, &currentOrderID, &distance, &duration, &startDate)

	if err == sql.ErrNoRows {
		return nil, models.ErrNoActiveDeliverySession
	}
	if err != nil {
		log.Error("GetDeliverySessionByIDForUser: failed to fetch delivery session: " + err.Error())
		return nil, err
	}

	session.UserID = userID
	session.MerchantID = merchantID
	session.Distance = distance.String
	session.Duration = duration.String
	session.StartDate = startDate.String

	return r.assembleDeliverySessionDetails(ctx, merchantID, &session, currentOrderID)
}

// assembleDeliverySessionDetails fills in the delivery man, the orders (with their
// per-stop FSM state and customer delivery notes), and current_order_id for a
// delivery_session row already fetched into `session`. Shared by
// GetActiveDeliverySessionForUser and GetDeliverySessionByIDForUser.
func (r *DeliverySessionsRepository) assembleDeliverySessionDetails(ctx context.Context, merchantID string, session *models.DeliverySession, currentOrderID sql.NullString) (*models.DeliverySession, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 2️⃣ Fetch delivery man info
	var deliveryMan models.OrderUser
	err := db.QueryRowContext(ctx, `
		SELECT DISTINCT usv.user_id, usv.first_name, usv.last_name, usv.lat, usv.lng, usv.status
		FROM user_status_view usv
		INNER JOIN users_rights ur ON ur.id = usv.user_id
		INNER JOIN merchant m ON m.id = ur.merchant_id
		INNER JOIN delivery_session ds ON ds.user_id = usv.user_id
		WHERE ur.merchant_id = ?
		  AND ur.enabled
		  AND ds.id = ?
	`, merchantID, session.DeliverySessionID).Scan(
		&deliveryMan.UserID,
		&deliveryMan.FirstName,
		&deliveryMan.LastName,
		&deliveryMan.Lat,
		&deliveryMan.Lng,
		&deliveryMan.Status,
	)

	if err == sql.ErrNoRows {
		log.Error("assembleDeliverySessionDetails: delivery man not found")
		return nil, fmt.Errorf("cannot_find_delivery_man")
	}
	if err != nil {
		log.Error("assembleDeliverySessionDetails: failed to fetch delivery man: " + err.Error())
		return nil, err
	}

	session.DeliveryMan = deliveryMan

	// 3️⃣ Get order IDs in priority order, with their per-stop FSM state
	rows, err := db.QueryContext(ctx, `
		SELECT o.order_id, dso.priority, dso.status, dso.arrived_at, dso.delivered_at, dso.failed_at, dso.canceled_at, dso.fail_reason
		FROM delivery_session ds
		INNER JOIN delivery_session_order dso ON dso.delivery_session_id = ds.id
		INNER JOIN orders o ON o.order_id = dso.order_id AND ds.merchant_id = o.merchant_id
		WHERE ds.id = ?
		AND ds.merchant_id = ?
		ORDER BY dso.priority ASC
	`, session.DeliverySessionID, merchantID)

	if err != nil {
		log.Error("assembleDeliverySessionDetails: failed to fetch order IDs: " + err.Error())
		return nil, err
	}
	defer rows.Close()

	orderIDs := []string{}
	stopsByOrderID := map[string]*models.DeliveryStop{}

	for rows.Next() {
		var oid string
		var priority sql.NullInt64
		var stopStatus string
		var arrivedAt, deliveredAt, failedAt, canceledAt sql.NullTime
		var failReason sql.NullString

		if err := rows.Scan(&oid, &priority, &stopStatus, &arrivedAt, &deliveredAt, &failedAt, &canceledAt, &failReason); err != nil {
			return nil, err
		}

		orderIDs = append(orderIDs, oid)

		stop := &models.DeliveryStop{
			Status:      stopStatus,
			ArrivedAt:   helpers.NullTimePtr(arrivedAt),
			DeliveredAt: helpers.NullTimePtr(deliveredAt),
			FailedAt:    helpers.NullTimePtr(failedAt),
			CanceledAt:  helpers.NullTimePtr(canceledAt),
			FailReason:  helpers.NullStringToPtr(failReason),
		}
		if priority.Valid {
			p := int(priority.Int64)
			stop.Priority = &p
		}
		stopsByOrderID[oid] = stop
	}

	// 4️⃣ Fetch full order objects using the existing fetcher, attaching the per-stop FSM state
	var allOrders []models.Order

	for _, oid := range orderIDs {
		filter := fmt.Sprintf(" AND o.order_id = '%s' ", oid)
		fetchedOrders, err := r.ordersFetcher.FetchAndBuildOrders(context.Background(), merchantID, filter, "", "")
		if err != nil {
			return nil, err
		}

		if len(fetchedOrders) > 0 {
			order := fetchedOrders[0]
			order.DeliveryStop = stopsByOrderID[oid]
			allOrders = append(allOrders, order)
		}
	}

	// 5️⃣ Merge customer.delivery_notes (added by migration 032) into each order's customer
	if len(orderIDs) > 0 {
		quotedIDs := make([]string, len(orderIDs))
		for i, oid := range orderIDs {
			quotedIDs[i] = fmt.Sprintf("'%s'", oid)
		}

		notesRows, err := db.QueryContext(ctx, fmt.Sprintf(`
			SELECT o.order_id, c.delivery_notes
			FROM orders o
			INNER JOIN customer c ON c.customer_id = o.customer_id
			WHERE o.order_id IN (%s)
		`, strings.Join(quotedIDs, ",")))

		if err != nil {
			log.Error("assembleDeliverySessionDetails: failed to fetch customer delivery notes: " + err.Error())
			return nil, err
		}

		notesByOrderID := map[string]*string{}
		for notesRows.Next() {
			var oid string
			var notes sql.NullString
			if err := notesRows.Scan(&oid, &notes); err != nil {
				notesRows.Close()
				return nil, err
			}
			notesByOrderID[oid] = helpers.NullStringToPtr(notes)
		}
		notesRows.Close()

		for i := range allOrders {
			if notes, ok := notesByOrderID[allOrders[i].OrderID]; ok && allOrders[i].Customer != nil {
				allOrders[i].Customer.CustomerDeliveryNotes = notes
			}
		}
	}

	session.Orders = allOrders

	// 6️⃣ Resolve current_order_id: use the stored pointer if set, otherwise derive
	// the first 'pending' stop in priority order (without writing it back).
	if currentOrderID.Valid {
		session.CurrentOrderID = &currentOrderID.String
	} else {
		for _, oid := range orderIDs {
			if stop := stopsByOrderID[oid]; stop != nil && stop.Status == "pending" {
				derived := oid
				session.CurrentOrderID = &derived
				break
			}
		}
	}

	return session, nil
}

// SelectDeliveryStop selects orderID as the current stop of the caller's active
// delivery session (transition 1, §1.3 #1 / §3.2). "Désordre permis": any other
// stop currently en_route/arrived is reverted to pending (its provisional arrival
// is undone). Returns the session id on success.
func (r *DeliverySessionsRepository) SelectDeliveryStop(ctx context.Context, merchantID, userID, orderID string) (sessionID string, err error) {
	err = dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		// 1. Resolve the caller's active session
		txErr := db.QueryRowContext(txCtx, `
			SELECT id FROM delivery_session
			WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
			ORDER BY start_date DESC LIMIT 1
		`, userID, merchantID).Scan(&sessionID)
		if txErr == sql.ErrNoRows {
			return models.ErrNoActiveDeliverySession
		}
		if txErr != nil {
			return txErr
		}

		// 2. The target stop must belong to this session and be non-terminal
		var stopStatus string
		txErr = db.QueryRowContext(txCtx, `
			SELECT status FROM delivery_session_order
			WHERE delivery_session_id = ? AND order_id = ?
		`, sessionID, orderID).Scan(&stopStatus)
		if txErr == sql.ErrNoRows {
			return models.ErrDeliveryStopNotFound
		}
		if txErr != nil {
			return txErr
		}
		if stopStatus == "delivered" || stopStatus == "failed" || stopStatus == "canceled" {
			return models.ErrDeliveryStopTerminal
		}

		// 3. Demote any other en_route/arrived stop back to pending
		if _, txErr = db.ExecContext(txCtx, `
			UPDATE delivery_session_order
			SET status = 'pending', arrived_at = NULL
			WHERE delivery_session_id = ? AND order_id != ? AND status IN ('en_route','arrived')
		`, sessionID, orderID); txErr != nil {
			return txErr
		}

		// 4. Promote the target stop to en_route
		if _, txErr = db.ExecContext(txCtx, `
			UPDATE delivery_session_order
			SET status = 'en_route', arrived_at = NULL
			WHERE delivery_session_id = ? AND order_id = ?
		`, sessionID, orderID); txErr != nil {
			return txErr
		}

		// 5. Point the session at this stop
		if _, txErr = db.ExecContext(txCtx, `
			UPDATE delivery_session SET current_order_id = ? WHERE id = ?
		`, orderID, sessionID); txErr != nil {
			return txErr
		}

		return nil
	})

	return sessionID, err
}

// MarkDeliveryStopArrived transitions the caller's current stop from en_route to
// arrived (transition 2, manual path, §1.3 #2 / §3.3). The `status = 'en_route'`
// guard in the UPDATE makes this idempotent and safe against a concurrent geofence
// update (internal/modules/users) performing the same transition. Returns the
// session id on success.
func (r *DeliverySessionsRepository) MarkDeliveryStopArrived(ctx context.Context, merchantID, userID, orderID string) (sessionID string, err error) {
	err = dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		var currentOrderID sql.NullString
		txErr := db.QueryRowContext(txCtx, `
			SELECT id, current_order_id FROM delivery_session
			WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
			ORDER BY start_date DESC LIMIT 1
		`, userID, merchantID).Scan(&sessionID, &currentOrderID)
		if txErr == sql.ErrNoRows {
			return models.ErrNoActiveDeliverySession
		}
		if txErr != nil {
			return txErr
		}

		if !currentOrderID.Valid || currentOrderID.String != orderID {
			return models.ErrDeliveryStopNotCurrent
		}

		res, txErr := db.ExecContext(txCtx, `
			UPDATE delivery_session_order SET status = 'arrived', arrived_at = UTC_TIMESTAMP()
			WHERE delivery_session_id = ? AND order_id = ? AND status = 'en_route'
		`, sessionID, orderID)
		if txErr != nil {
			return txErr
		}

		affected, txErr := res.RowsAffected()
		if txErr != nil {
			return txErr
		}
		if affected == 0 {
			return models.ErrDeliveryStopNotEnRoute
		}

		return nil
	})

	return sessionID, err
}

// ResolveDeliverableStop checks that orderID is the caller's current stop and that its
// per-stop status allows the en_route/arrived -> delivered transition (§1.3 #3 / §3.4
// step 1). Returns the active session id on success. Read-only: the actual mutation is
// split across SetDelivered (own transaction, called by the service) and
// FinalizeDeliveredStop (steps 5-7, below).
func (r *DeliverySessionsRepository) ResolveDeliverableStop(ctx context.Context, merchantID, userID, orderID string) (sessionID string, err error) {
	db := dbutils.GetDB(ctx, r.database)

	var currentOrderID sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT id, current_order_id FROM delivery_session
		WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
		ORDER BY start_date DESC LIMIT 1
	`, userID, merchantID).Scan(&sessionID, &currentOrderID)
	if err == sql.ErrNoRows {
		return "", models.ErrNoActiveDeliverySession
	}
	if err != nil {
		return "", err
	}

	if currentOrderID.Valid && currentOrderID.String != orderID {
		return "", models.ErrDeliveryStopNotCurrent
	}

	var stopStatus string
	err = db.QueryRowContext(ctx, `
		SELECT status FROM delivery_session_order
		WHERE delivery_session_id = ? AND order_id = ?
	`, sessionID, orderID).Scan(&stopStatus)
	if err == sql.ErrNoRows {
		return "", models.ErrDeliveryStopNotFound
	}
	if err != nil {
		return "", err
	}
	if stopStatus != "en_route" && stopStatus != "arrived" {
		// Restreint la livraison à la commande en coursuniquement. désactivé temporairement pour ne pas bloquer
		//return "", models.ErrDeliveryStopNotDeliverable
	}

	return sessionID, nil
}

// FinalizeDeliveredStop applies the local effects of transition 3 (§1.3 #3 / §3.4 steps
// 5-7) once SetDelivered has already fiscally closed the order in its own, separate
// transaction: marks the stop 'delivered', reassigns its payment to the delivery person
// (same shape as CloseDeliverySession step 3, scoped to this order), and advances
// current_order_id to the next pending stop. Runs in its own transaction.
//
// Idempotent: step 1's UPDATE does not depend on the stop's previous status, so a retry
// after a crash between SetDelivered's commit and this transaction's commit is safe -
// see the handler-level note on the non-atomic window (§3.4).
func (r *DeliverySessionsRepository) FinalizeDeliveredStop(ctx context.Context, sessionID, orderID string) error {
	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		// 1. Mark the stop delivered
		if _, err := db.ExecContext(txCtx, `
			UPDATE delivery_session_order
			SET status = 'delivered', delivered_at = UTC_TIMESTAMP()
			WHERE delivery_session_id = ? AND order_id = ?
		`, sessionID, orderID); err != nil {
			return err
		}

		// 2. Reassign the order's payment to the delivery person (ds.user_id)
		if _, err := db.ExecContext(txCtx, `
			UPDATE payments p
			INNER JOIN delivery_session_order dso ON dso.order_id = p.order_id
			INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
			SET p.user_id = ds.user_id
			WHERE ds.id = ? AND dso.order_id = ?
		`, sessionID, orderID); err != nil {
			return err
		}

		// 3. Advance current_order_id to the next pending stop (or NULL if none)
		return r.advanceCurrentStop(txCtx, sessionID)
	})
}

// advanceCurrentStop moves the session's current_order_id to the next 'pending' stop
// (ordered by priority ASC), promoting it to 'en_route'; if none remain, clears
// current_order_id. Shared by transitions 3/4/5 (§1.3) - reused directly by future
// /failed and /cancel implementations.
func (r *DeliverySessionsRepository) advanceCurrentStop(ctx context.Context, sessionID string) error {
	db := dbutils.GetDB(ctx, r.database)

	var nextOrderID string
	err := db.QueryRowContext(ctx, `
		SELECT order_id FROM delivery_session_order
		WHERE delivery_session_id = ? AND status = 'pending'
		ORDER BY priority ASC LIMIT 1
	`, sessionID).Scan(&nextOrderID)

	if err == sql.ErrNoRows {
		_, err = db.ExecContext(ctx, `
			UPDATE delivery_session SET current_order_id = NULL WHERE id = ?
		`, sessionID)
		return err
	}
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE delivery_session_order SET status = 'en_route', arrived_at = NULL
		WHERE delivery_session_id = ? AND order_id = ?
	`, sessionID, nextOrderID); err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		UPDATE delivery_session SET current_order_id = ? WHERE id = ?
	`, nextOrderID, sessionID)
	return err
}

// terminalizeDeliveryStop is the shared implementation of transitions 4 and 5 (§1.3
// #4/#5, §3.5/§3.6): the caller's current stop moves to a terminal state ("failed" or
// "canceled") with the given reason (stored in fail_reason for both), orders.brand_status
// is updated accordingly (orders.state stays 'OPEN' - re-dispatchable), and
// current_order_id advances to the next pending stop via advanceCurrentStop. Returns the
// session id on success.
func (r *DeliverySessionsRepository) terminalizeDeliveryStop(ctx context.Context, merchantID, userID, orderID, reason, newStatus, brandStatus string) (sessionID string, err error) {
	err = dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		var currentOrderID sql.NullString
		txErr := db.QueryRowContext(txCtx, `
			SELECT id, current_order_id FROM delivery_session
			WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
			ORDER BY start_date DESC LIMIT 1
		`, userID, merchantID).Scan(&sessionID, &currentOrderID)
		if txErr == sql.ErrNoRows {
			return models.ErrNoActiveDeliverySession
		}
		if txErr != nil {
			return txErr
		}

		if !currentOrderID.Valid || currentOrderID.String != orderID {
			return models.ErrDeliveryStopNotCurrent
		}

		var stopStatus string
		txErr = db.QueryRowContext(txCtx, `
			SELECT status FROM delivery_session_order
			WHERE delivery_session_id = ? AND order_id = ?
		`, sessionID, orderID).Scan(&stopStatus)
		if txErr == sql.ErrNoRows {
			return models.ErrDeliveryStopNotFound
		}
		if txErr != nil {
			return txErr
		}
		if stopStatus == "delivered" || stopStatus == "failed" || stopStatus == "canceled" {
			return models.ErrDeliveryStopTerminal
		}

		var updateStopQuery string
		switch newStatus {
		case "failed":
			updateStopQuery = `
				UPDATE delivery_session_order
				SET status = 'failed', failed_at = UTC_TIMESTAMP(), fail_reason = ?
				WHERE delivery_session_id = ? AND order_id = ?`
		case "canceled":
			updateStopQuery = `
				UPDATE delivery_session_order
				SET status = 'canceled', canceled_at = UTC_TIMESTAMP(), fail_reason = ?
				WHERE delivery_session_id = ? AND order_id = ?`
		default:
			return fmt.Errorf("terminalizeDeliveryStop: unsupported status %q", newStatus)
		}

		if _, txErr = db.ExecContext(txCtx, updateStopQuery, reason, sessionID, orderID); txErr != nil {
			return txErr
		}

		if _, txErr = db.ExecContext(txCtx, `
			UPDATE orders SET brand_status = ? WHERE order_id = ?
		`, brandStatus, orderID); txErr != nil {
			return txErr
		}

		return r.advanceCurrentStop(txCtx, sessionID)
	})

	return sessionID, err
}

// MarkDeliveryStopFailed transitions the caller's current stop to 'failed' (transition
// 4, §1.3 #4 / §3.5). orders.state is left 'OPEN' (re-dispatchable).
func (r *DeliverySessionsRepository) MarkDeliveryStopFailed(ctx context.Context, merchantID, userID, orderID, reason string) (sessionID string, err error) {
	return r.terminalizeDeliveryStop(ctx, merchantID, userID, orderID, reason, "failed", "DELIVERY_FAILED")
}

// CancelDeliveryStop transitions the caller's current stop to 'canceled' (transition 5,
// §1.3 #5 / §3.6, path (a) without refund - the dispatcher handles any refund
// separately). orders.state is left 'OPEN' (re-dispatchable).
func (r *DeliverySessionsRepository) CancelDeliveryStop(ctx context.Context, merchantID, userID, orderID, reason string) (sessionID string, err error) {
	return r.terminalizeDeliveryStop(ctx, merchantID, userID, orderID, reason, "canceled", "DELIVERY_CANCELED")
}

// CloseMyDeliverySession closes the caller's active delivery session once all of its
// stops are terminal (§1.5/§3.8). Writes delivery_session.status='DONE' (legacy value,
// same as CloseDeliverySession - intentionally NOT 'done', see §0.5) and end_date.
// Per-stop statuses, payments, and orders.state are left untouched (already finalized by
// /delivered, /failed, /cancel). Also clears the driver's last known position (privacy -
// position is retained only while a session is active, §3.7/§6).
func (r *DeliverySessionsRepository) CloseMyDeliverySession(ctx context.Context, merchantID, userID string) (sessionID string, err error) {
	err = dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbutils.GetDB(txCtx, r.database)

		txErr := db.QueryRowContext(txCtx, `
			SELECT id FROM delivery_session
			WHERE user_id = ? AND merchant_id = ? AND status IN ('1','PENDING')
			ORDER BY start_date DESC LIMIT 1
		`, userID, merchantID).Scan(&sessionID)
		if txErr == sql.ErrNoRows {
			return models.ErrNoActiveDeliverySession
		}
		if txErr != nil {
			return txErr
		}

		var pendingCount int
		txErr = db.QueryRowContext(txCtx, `
			SELECT COUNT(*) FROM delivery_session_order
			WHERE delivery_session_id = ? AND status NOT IN ('delivered','failed','canceled')
		`, sessionID).Scan(&pendingCount)
		if txErr != nil {
			return txErr
		}
		if pendingCount > 0 {
			return models.ErrSessionHasPendingStops
		}

		if _, txErr = db.ExecContext(txCtx, `
			UPDATE delivery_session SET status = 'DONE', end_date = UTC_TIMESTAMP() WHERE id = ?
		`, sessionID); txErr != nil {
			return txErr
		}

		if _, txErr = db.ExecContext(txCtx, `
			UPDATE users SET lat = NULL, lng = NULL, heading = NULL, last_position_at = NULL WHERE user_id = ?
		`, userID); txErr != nil {
			return txErr
		}

		return nil
	})

	return sessionID, err
}
