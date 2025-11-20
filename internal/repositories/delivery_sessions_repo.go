package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type DeliverySessionsRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewDeliverySessionsRepository(db *sql.DB, log *zap.Logger) *DeliverySessionsRepository {
	return &DeliverySessionsRepository{db: db, log: log}
}

// GetPendingDeliverySessions : Optimisé pour éviter les timeouts
func (r *DeliverySessionsRepository) GetPendingDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	// On instancie le repo orders (nécessaire pour le constructeur partagé)
	ordersRepo := NewOrdersRepository(r.db, r.log)

	// 1. Récupérer les sessions actives
	sessions, err := r.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// S'il n'y a pas de session, on s'arrête là
	if len(sessions) == 0 {
		return []models.DeliverySession{}, nil
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
	orders, err := ordersRepo.fetchAndBuildOrders(ctx, merchantID, filter)
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

// fetchDeliverySessions : Helper pour récupérer les sessions seules
func (r *DeliverySessionsRepository) fetchDeliverySessions(ctx context.Context, merchantID string, filterStatus string) ([]models.DeliverySession, error) {
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

	var sessions []models.DeliverySession
	for rows.Next() {
		var profilePic, firstName, lastName, planningColor, status, id, userID sql.NullString
		var lat, lng sql.NullFloat64
		if err := rows.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			return nil, err
		}

		// Conversion ID int64 (si besoin)
		// sessID, _ := strconv.ParseInt(id.String, 10, 64)

		ds := models.DeliverySession{
			DeliverySessionID: id.String, // ou sessID
			Status:            status.String,
			Orders:            []models.Order{},
			DeliveryMan: models.OrderUser{
				UserID: userID.String, FirstName: &firstName.String, LastName: &lastName.String,
				Lat: nullFloat64Ptr(lat), Lng: nullFloat64Ptr(lng),
			},
		}
		sessions = append(sessions, ds)
	}
	return sessions, nil
}

// GetPendingOrders returns orders + delivery sessions
// GetDeliverySessions returns only the delivery sessions (same retrieval as above but returns sessions only)
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
				ProfilePicture: nullStringToPtr(profilePic),
				Lat:            nullFloat64Ptr(lat),
				Lng:            nullFloat64Ptr(lng),
				PlanningColor:  nullStringToPtr(planningColor),
			},
		}
		sessions = append(sessions, ds)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sessions, nil
}
