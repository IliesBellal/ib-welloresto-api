package delivery_sessions

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/orders"

	"go.uber.org/zap"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type DeliverySessionsRepository struct {
	db            *sql.DB
	log           *zap.Logger
	ordersFetcher *orders.OrdersFetcher
}

func NewDeliverySessionsRepository(db *sql.DB, log *zap.Logger) *DeliverySessionsRepository {
	temp := orders.NewOrdersFetcher(db, log)
	return &DeliverySessionsRepository{db: db, log: log, ordersFetcher: temp}
}

func (r *DeliverySessionsRepository) GetPendingDeliverySessions(ctx context.Context, merchantID string) ([]DeliverySession, error) {
	// 1. Récupérer les sessions actives
	sessions, err := r.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// S'il n'y a pas de session, on s'arrête là
	if len(sessions) == 0 {
		return []DeliverySession{}, nil
	}

	// 2. OPTIMISATION CRITIQUE : Récupérer les Order IDs AVANT d'appeler le gros constructeur
	// Cela évite de refaire les jointures sessions <-> orders dans les 11 requêtes suivantes.

	// A. Construire la liste des ID de sessions
	sessionIDs := ""
	for i, s := range sessions {
		if i > 0 {
			sessionIDs += ","
		}
		sessionIDs += fmt.Sprintf("'%s'", s.DeliverySessionID) // Ajout des quotes au cas où c'est du string/uuid
	}

	// B. Requête légère pour avoir juste les IDs des commandes
	// On utilise r.db.QueryContext directement car c'est une requête interne simple
	qOrderIDs := fmt.Sprintf(`
		SELECT DISTINCT order_id 
		FROM delivery_session_order 
		WHERE delivery_session_id IN (%s)
	`, sessionIDs)

	rows, err := r.db.QueryContext(ctx, qOrderIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch session order ids: %w", err)
	}
	defer rows.Close()

	var orderIDList []string
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orderIDList = append(orderIDList, oid)
	}

	// Si ces sessions n'ont aucune commande, on retourne les sessions vides
	if len(orderIDList) == 0 {
		return sessions, nil
	}

	// 3. Construire le filtre PAR ORDER ID (MySQL adore ça, c'est instantané)
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
	orders, err := r.ordersFetcher.FetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}

	// 5. Assemblage : Mettre les commandes dans les bonnes sessions

	// Map pour regrouper les commandes par Session ID
	// (On utilise string comme clé car dans tes logs précédents c'était souvent traité comme string)
	ordersBySession := make(map[string][]models.Order)

	for _, o := range orders {
		if o.DeliverySessionID != nil {
			// Conversion de *int64 vers string pour la clé de la map (si nécessaire)
			// Si DeliverySessionID est int64 dans ton struct Order :
			// key := fmt.Sprintf("%d", *o.DeliverySessionID)

			// Si DeliverySessionID est string dans ton struct Order :
			key := *o.DeliverySessionID

			ordersBySession[key] = append(ordersBySession[key], o)
		}
	}

	// On remplit les sessions
	for i := range sessions {
		// On récupère l'ID de la session (c'est un string dans ton struct DeliverySession ci-dessous)
		sID := sessions[i].DeliverySessionID

		if sessionOrders, found := ordersBySession[sID]; found {
			sessions[i].Orders = sessionOrders
		} else {
			sessions[i].Orders = []models.Order{}
		}
	}

	return sessions, nil
}

func (r *DeliverySessionsRepository) fetchDeliverySessions(ctx context.Context, merchantID string, filterStatus string) ([]DeliverySession, error) {
	q := `
       SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
       FROM delivery_session ds
       INNER JOIN users u on u.user_id = ds.user_id
       WHERE ds.merchant_id = ? AND ` + filterStatus

	rows, err := r.db.QueryContext(ctx, q, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []DeliverySession
	for rows.Next() {
		var profilePic, firstName, lastName, planningColor, status, id, userID sql.NullString
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			return nil, err
		}

		// Conversion ID int64 (si besoin)
		// sessID, _ := strconv.ParseInt(id.String, 10, 64)

		ds := DeliverySession{
			DeliverySessionID: id.String, // ou sessID
			Status:            status.String,
			Orders:            []models.Order{},
			DeliveryMan: models.OrderUser{
				UserID: userID.String, FirstName: &firstName.String, LastName: &lastName.String,
				Lat: helpers.NullFloat64Ptr(lat), Lng: helpers.NullFloat64Ptr(lng),
			},
		}
		sessions = append(sessions, ds)
	}
	return sessions, nil
}

func (r *DeliverySessionsRepository) GetDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	qDeliverySessions := `
SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
FROM delivery_session ds
INNER JOIN users u on u.user_id = ds.user_id
WHERE status IN ('1','PENDING')
AND ds.merchant_id = ?`
	rows, err := tx.QueryContext(ctx, qDeliverySessions, merchantID)
	if err != nil {
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

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *DeliverySessionsRepository) StartDeliverySession(ctx context.Context, req *models.DeliverySessionRequest) (*models.DeliverySession, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	userID := req.DeliveryMan.UserID
	merchantID := req.MerchantID

	// 1. Close finished delivery sessions (PHP calls closeFinishedDeliverySession)
	_, _ = tx.ExecContext(ctx, `
		UPDATE delivery_session
		SET status = 'FINISHED'
		WHERE user_id = ? AND status IN ('1','PENDING') AND end_date < UTC_TIMESTAMP
	`, userID)

	// 2. Check if a session is already active
	var existing string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM delivery_session
		WHERE user_id = ? AND status IN ('1','PENDING')
	`, userID).Scan(&existing)

	if err != sql.ErrNoRows {
		return nil, models.ErrDeliverySessionAlreadyActive
	}

	// 3. Insert new delivery_session
	res, err := tx.ExecContext(ctx, `
		INSERT INTO delivery_session (user_id, merchant_id, start_date, distance, duration, status)
		VALUES (?, ?, UTC_TIMESTAMP, ?, ?, 'PENDING')
	`, userID, merchantID, req.Distance, req.Duration)
	if err != nil {
		return nil, err
	}

	sessionID, _ := res.LastInsertId()

	// 4. Insert delivery_session_order
	for i, o := range req.Orders {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO delivery_session_order (delivery_session_id, order_id, priority)
			VALUES (?, ?, ?)
		`, sessionID, o.OrderID, i)
		if err != nil {
			return nil, err
		}
	}

	// 5. Update orders brand_status
	_, err = tx.ExecContext(ctx, `
		UPDATE orders o
		INNER JOIN delivery_session_order dso ON dso.order_id = o.order_id
		SET o.brand_status = 'EN_ROUTE_TO_DROPOFF'
		WHERE dso.delivery_session_id = ?
	`, sessionID)
	if err != nil {
		return nil, err
	}

	// 6. Commit
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 7. Notifications (equivalent PHP)
	/*
		r.sendNewDeliverySessionNotification(merchantID, fmt.Sprint(sessionID))
		for _, o := range req.Orders {
			r.sendDeliveryStartSMS(merchantID, o.OrderID)
		}
	*/

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

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// 1) Revert orders to READY_FOR_HANDOFF
	_, err = tx.ExecContext(ctx, `
        UPDATE orders o
        INNER JOIN delivery_session_order dso ON dso.order_id = o.order_id
        INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
        SET o.brand_status = 'READY_FOR_HANDOFF'
        WHERE ds.id = ?
        AND o.brand_status = 'EN_ROUTE_TO_DROPOFF'
	`, sessionID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// ⚠ PHP version commented out the negative ID update → we skip it, as requested.

	// 2) Mark session as canceled
	_, err = tx.ExecContext(ctx, `
        UPDATE delivery_session
        SET status = 'CANCELED'
        WHERE id = ?
        AND status >= 0
	`, sessionID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// 3) Retrieve merchant_id
	var merchantID string
	err = tx.QueryRowContext(ctx, `
        SELECT merchant_id
        FROM delivery_session
        WHERE id = ?
	`, sessionID).Scan(&merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &models.DeliverySession{
		DeliverySessionID: sessionID,
		MerchantID:        merchantID,
		Status:            "CANCELED",
	}, nil
}

func (r *DeliverySessionsRepository) CloseDeliverySession(ctx context.Context, sessionID string) (*models.DeliverySession, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// 1) Update orders
	_, err = tx.ExecContext(ctx, `
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
		tx.Rollback()
		return nil, err
	}

	// 2) Mark session as DONE
	_, err = tx.ExecContext(ctx, `
        UPDATE delivery_session
        SET status = 'DONE'
        WHERE id = ?
        AND status NOT IN ('-1','CLOSED','CANCELED')
	`, sessionID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// 3) Update payments user_id
	_, err = tx.ExecContext(ctx, `
        UPDATE payments p
        INNER JOIN delivery_session_order dso ON dso.order_id = p.order_id
        INNER JOIN delivery_session ds ON ds.id = dso.delivery_session_id
        SET p.user_id = ds.user_id
        WHERE ds.id = ?
	`, sessionID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// retrieve merchant
	var merchantID string
	err = tx.QueryRowContext(ctx, `
        SELECT merchant_id
        FROM delivery_session
        WHERE id = ?
	`, sessionID).Scan(&merchantID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &models.DeliverySession{
		DeliverySessionID: sessionID,
		MerchantID:        merchantID,
		Status:            "DONE",
	}, nil
}

func (r *DeliverySessionsRepository) GetDeliverySession(ctx context.Context, merchantID, sessionID string) (*models.DeliverySession, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1️⃣ Fetch session basic info
	var session models.DeliverySession
	err = tx.QueryRowContext(ctx, `
		SELECT id, status
		FROM delivery_session
		WHERE id = ?
	`, sessionID).Scan(&session.DeliverySessionID, &session.Status)

	if err != nil {
		return nil, err
	}

	// 2️⃣ Fetch delivery man info
	var deliveryMan models.OrderUser
	err = tx.QueryRowContext(ctx, `
		SELECT DISTINCT usv.user_id, usv.first_name, usv.last_name, usv.lat, usv.lng, usv.status
		FROM user_status_view usv
		INNER JOIN users_rights ur ON ur.id = usv.user_id
		INNER JOIN merchant m ON m.id = ur.merchant_id
		INNER JOIN delivery_session ds ON ds.user_id = usv.user_id
		WHERE ur.merchant_id = ?
		  AND ur.enabled
		  AND ds.id = ?
	`, merchantID, sessionID).Scan(
		&deliveryMan.UserID,
		&deliveryMan.FirstName,
		&deliveryMan.LastName,
		&deliveryMan.Lat,
		&deliveryMan.Lng,
		&deliveryMan.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cannot_find_delivery_man")
	}
	if err != nil {
		return nil, err
	}

	session.DeliveryMan = deliveryMan

	// 3️⃣ Get order IDs in priority order
	rows, err := tx.QueryContext(ctx, `
		SELECT o.order_id
		FROM delivery_session ds
		INNER JOIN delivery_session_order dso ON dso.delivery_session_id = ds.id
		INNER JOIN orders o ON o.order_id = dso.order_id AND ds.merchant_id = o.merchant_id
		WHERE ds.id = ?
		AND ds.merchant_id = ?
		ORDER BY dso.priority ASC
	`, sessionID, merchantID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orderIDs := []string{}
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, oid)
	}

	// 4️⃣ Fetch full order objects using your existing function
	var allOrders []models.Order

	// 5️⃣ Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, oid := range orderIDs {
		filter := fmt.Sprintf(" AND o.order_id = '%s' ", oid)
		orders, err := r.ordersFetcher.FetchAndBuildOrders(context.Background(), merchantID, filter)
		if err != nil {
			return nil, err
		}

		if len(orders) > 0 {
			allOrders = append(allOrders, orders[0])
		}
	}

	session.Orders = allOrders

	return &session, nil
}
