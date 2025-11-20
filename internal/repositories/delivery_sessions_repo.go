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

// GetPendingDeliverySessions : Récupère les sessions + leurs commandes
func (r *DeliverySessionsRepository) GetPendingDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	ordersRepo := NewOrdersRepository(r.db, r.log)

	// 1. Récupérer les sessions actives
	sessions, err := r.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return []models.DeliverySession{}, nil
	}

	// 2. Récupérer SEULEMENT les commandes liées à ces sessions
	sessionIDs := ""
	for i, s := range sessions {
		if i > 0 {
			sessionIDs += ","
		}
		// Attention : si s.DeliverySessionID est un string dans ton struct, retire le fmt.Sprintf("%d")
		sessionIDs += fmt.Sprintf("%v", s.DeliverySessionID)
	}

	// Filtre : Commandes liées aux sessions trouvées
	filter := fmt.Sprintf(" AND ds.id IN (%s) ", sessionIDs)

	// On appelle le constructeur partagé
	orders, err := ordersRepo.fetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}

	// 3. Assemblage : Mettre les commandes dans les bonnes sessions

	// A. On regroupe les commandes par SessionID dans une map
	ordersBySession := make(map[string][]models.Order)

	for _, o := range orders {
		// Adapte cette vérification selon si DeliverySessionID est un *int64 (pointeur) ou int64
		var sessID string

		// CAS 1 : Si c'est un pointeur (*int64)
		if o.DeliverySessionID != nil {
			sessID = *o.DeliverySessionID
			ordersBySession[sessID] = append(ordersBySession[sessID], o)
		}

		// CAS 2 : Si c'est direct (int64) et que 0 veut dire "pas de session"
		// if o.DeliverySessionID != 0 {
		//    ordersBySession[o.DeliverySessionID] = append(ordersBySession[o.DeliverySessionID], o)
		// }
	}

	// B. On assigne les groupes de commandes aux sessions correspondantes
	// IMPORTANT : On utilise l'index 'i' pour modifier l'élément original du slice 'sessions'
	for i := range sessions {
		// On caste l'ID de session selon ton modèle (ici je suppose int64 ou string converti)
		// Supposons que sessions[i].DeliverySessionID est int64 (ou string qu'on a utilisé en clé de map)

		// Récupération de l'ID de la session (adapte le type si besoin)
		sID := sessions[i].DeliverySessionID
		// Si c'est un string dans session et int64 dans order, fais la conversion ici.
		// Si c'est int64 partout, c'est direct :

		if sessionOrders, found := ordersBySession[sID]; found {
			sessions[i].Orders = sessionOrders
		} else {
			// Initialiser à vide pour éviter "null" dans le JSON
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
