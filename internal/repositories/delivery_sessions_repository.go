package repositories

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type DeliverySessionsRepository struct {
	db *sql.DB
}

func NewDeliverySessionsRepository(db *sql.DB) *DeliverySessionsRepository {
	return &DeliverySessionsRepository{db: db}
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
