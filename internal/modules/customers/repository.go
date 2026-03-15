package customers

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
)

type CustomersRepository struct {
	db *sql.DB
}

func NewCustomerRepository(db *sql.DB) *CustomersRepository {
	return &CustomersRepository{db: db}
}

func (r *CustomersRepository) UpdateOrCreateCustomer(ctx context.Context, tx *sql.Tx, c *models.Customer) (*string, error) {

	// Liste des colonnes vraiment existantes et autorisées
	allowed := map[string]bool{
		"customer_name":                         true,
		"customer_first_name":                   true,
		"customer_last_name":                    true,
		"customer_tel":                          true,
		"customer_address":                      true,
		"customer_email":                        true,
		"customer_lat":                          true,
		"customer_lng":                          true,
		"customer_door_number":                  true,
		"customer_floor_number":                 true,
		"customer_additional_address":           true,
		"customer_business_name":                true,
		"customer_birthdate":                    true,
		"customer_additional_info":              true,
		"customer_temporary_address":            true,
		"customer_temporary_lat":                true,
		"customer_temporary_lng":                true,
		"customer_temporary_door_number":        true,
		"customer_temporary_floor_number":       true,
		"customer_temporary_additional_address": true,
		"advertising_consent":                   true,
	}

	// -------------------------------------------------------
	// 🔵 MODE UPDATE
	// -------------------------------------------------------
	if c.CustomerID != nil && *c.CustomerID != "" {

		var setParts []string
		var args []interface{}

		// pour chaque champ autorisé → ajouter au SET si != nil
		for col := range allowed {
			v := extractFieldValue(c, col)
			if v != nil {
				setParts = append(setParts, col+" = ?")
				args = append(args, v)
			}
		}

		// rien à modifier → fin
		if len(setParts) == 0 {
			return c.CustomerID, nil
		}

		query := `
            UPDATE customer
            SET ` + strings.Join(setParts, ", ") + `
            WHERE customer_id = ? AND merchant_id = ?
        `
		args = append(args, *c.CustomerID, c.MerchantID)

		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		// DO NOT COMMIT OTHERWISE ORDER CREATION WILL NOT WORK
		// tx.Commit()
		return c.CustomerID, nil
	}

	// -------------------------------------------------------
	// 🟢 MODE INSERT
	// -------------------------------------------------------
	var cols []string
	var placeholders []string
	var values []interface{}

	for col := range allowed {
		cols = append(cols, col)
		placeholders = append(placeholders, "?")
		values = append(values, extractFieldValue(c, col))
	}

	// merchant_id obligatoire
	cols = append(cols, "merchant_id")
	placeholders = append(placeholders, "?")
	values = append(values, c.MerchantID)

	query := `
        INSERT INTO customer (` + strings.Join(cols, ", ") + `)
        VALUES (` + strings.Join(placeholders, ", ") + `)
    `
	res, err := tx.ExecContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}

	id, _ := res.LastInsertId()
	// DO NOT COMMIT OTHERWISE ORDER CREATION WILL NOT WORK
	// tx.Commit()

	return helpers.Int64ToStringPtr(id), nil
}

func extractFieldValue(c *models.Customer, field string) interface{} {

	switch field {

	case "customer_name":
		if c.CustomerName != nil && *c.CustomerName != "" {
			return helpers.Ucfirst(*c.CustomerName)
		}
		return nil

	case "advertising_consent":
		if c.AdvertisingConsent != nil {
			return *c.AdvertisingConsent
		}
		return false

	case "customer_first_name":
		if c.CustomerFirstName != nil && *c.CustomerFirstName != "" {
			return helpers.Ucfirst(*c.CustomerFirstName)
		}
		return nil

	case "customer_last_name":
		if c.CustomerLastName != nil && *c.CustomerLastName != "" {
			return helpers.Ucfirst(*c.CustomerLastName)
		}
		return nil

	case "customer_tel":
		if c.CustomerTel != nil && *c.CustomerTel != "" {
			return helpers.NormalizePhoneNumber(*c.CustomerTel, "FR")
		}
		return nil

	// FLOATS
	case "customer_lat":
		if c.CustomerLat != nil {
			return *c.CustomerLat
		}
		return nil

	case "customer_lng":
		if c.CustomerLng != nil {
			return *c.CustomerLng
		}
		return nil

	case "customer_temporary_lat":
		if c.CustomerTemporaryLat != nil {
			return *c.CustomerTemporaryLat
		}
		return nil

	case "customer_temporary_lng":
		if c.CustomerTemporaryLng != nil {
			return *c.CustomerTemporaryLng
		}
		return nil

	// STRINGS génériques
	default:
		ptr := getStringField(c, field)
		if ptr != nil && *ptr != "" {
			return *ptr
		}
		return nil
	}
}

func getStringField(c *models.Customer, name string) *string {

	switch name {
	case "customer_address":
		return c.CustomerAddress
	case "customer_email":
		return c.CustomerEmail
	case "customer_door_number":
		return c.CustomerDoorNumber
	case "customer_floor_number":
		return c.CustomerFloorNumber
	case "customer_additional_address":
		return c.CustomerAdditionalAddress
	case "customer_business_name":
		return c.CustomerBusinessName
	case "customer_birthdate":
		return c.CustomerBirthdate
	case "customer_additional_info":
		return c.CustomerAdditionalInfo
	case "customer_temporary_address":
		return c.CustomerTemporaryAddress
	case "customer_temporary_door_number":
		return c.CustomerTemporaryDoorNumber
	case "customer_temporary_floor_number":
		return c.CustomerTemporaryFloorNumber
	case "customer_temporary_additional_address":
		return c.CustomerTemporaryAdditionalAddress
	}

	return nil
}

func (r *CustomersRepository) GetCustomerLoyalty(ctx context.Context, customerID string) (*CustomerLoyalty, error) {

	loyalty := &CustomerLoyalty{}

	// progress
	rows, err := r.db.QueryContext(ctx, `
        SELECT 
            p.id, 
            COALESCE(clp.current_value, 0), 
            COALESCE(clp.last_update, UTC_TIMESTAMP),
            p.type,
            p.target_value,
            p.name,
            p.description
        FROM customer c
        INNER JOIN customer_loyalty_programs p ON p.merchant_id = c.merchant_id
        LEFT JOIN customer_loyalty_progress clp ON clp.loyalty_program_id = p.id AND clp.customer_id = c.customer_id
        WHERE c.customer_id = ?
    `, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var p LoyaltyProgress
		if err := rows.Scan(
			&p.LoyaltyProgramID,
			&p.CurrentValue,
			&p.LastUpdate,
			&p.Type,
			&p.TargetValue,
			&p.Name,
			&p.Description,
		); err != nil {
			return nil, err
		}
		loyalty.LoyaltyProgress = append(loyalty.LoyaltyProgress, p)
	}

	// rewards
	rows2, err := r.db.QueryContext(ctx, `
        SELECT cr.customer_id, cr.reward_id, cr.loyalty_program_id, cr.creation_date, cr.reward_type, cr.reward_value, cr.is_used
        FROM customer_rewards cr
        WHERE cr.customer_id = ?
    `, customerID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var rwd LoyaltyReward
		if err := rows2.Scan(
			&rwd.CustomerID,
			&rwd.RewardID,
			&rwd.LoyaltyProgramID,
			&rwd.CreationDate,
			&rwd.RewardType,
			&rwd.RewardValue,
			&rwd.IsUsed,
		); err != nil {
			return nil, err
		}
		loyalty.AvailableRewards = append(loyalty.AvailableRewards, rwd)
	}

	return loyalty, nil
}

func (r *CustomersRepository) UpdateLoyaltyProgress(ctx context.Context, req *LoyaltyProgressUpdateRequest) (int, error) {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var targetValue int
	var rewardType string
	var rewardValue int
	var rewardOrderType string

	// 1. Fetch program
	err = tx.QueryRowContext(ctx, `
        SELECT target_value, reward_type, reward_value, rewards_order_type
        FROM customer_loyalty_programs
        WHERE id = ?
    `, req.LoyaltyProgramID).Scan(&targetValue, &rewardType, &rewardValue, &rewardOrderType)
	if err != nil {
		return 0, err
	}

	// 2. fetch progress
	var progressID string
	var oldValue int

	err = tx.QueryRowContext(ctx, `
        SELECT id, current_value 
        FROM customer_loyalty_progress
        WHERE customer_id = ? AND loyalty_program_id = ?
    `, req.CustomerID, req.LoyaltyProgramID).Scan(&progressID, &oldValue)

	exists := (err == nil)

	oldRewards := oldValue / targetValue
	newRewards := req.CurrentValue / targetValue
	rewardToCreate := newRewards - oldRewards

	if exists {
		_, err = tx.ExecContext(ctx, `
            UPDATE customer_loyalty_progress
            SET current_value = ?, last_update = UTC_TIMESTAMP
            WHERE id = ?
        `, req.CurrentValue, progressID)
		if err != nil {
			return 0, err
		}
	} else {
		_, err = tx.ExecContext(ctx, `
            INSERT INTO customer_loyalty_progress (customer_id, loyalty_program_id, current_value)
            VALUES (?, ?, ?)
        `, req.CustomerID, req.LoyaltyProgramID, req.CurrentValue)
		if err != nil {
			return 0, err
		}
	}

	// rewards creation
	if rewardToCreate > 0 {
		for i := 0; i < rewardToCreate; i++ {
			_, err = tx.ExecContext(ctx, `
                INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_order_type, reward_value, creation_date)
                VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP)
            `, req.CustomerID, req.LoyaltyProgramID, rewardType, rewardOrderType, rewardValue)
			if err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return rewardToCreate, nil
}

func (r *CustomersRepository) UpdateLoyaltyReward(ctx context.Context, req *LoyaltyRewardUpdateRequest) error {

	_, err := r.db.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = ?, usage_date = UTC_TIMESTAMP
        WHERE reward_id = ?
    `, req.IsUsed, req.RewardID)

	return err
}

func (r *CustomersRepository) SearchCustomers(ctx context.Context, merchantID, term string) ([]CustomerSearchResult, error) {

	likeTerm := "%" + term + "%"
	var results []CustomerSearchResult

	rows, err := r.db.QueryContext(ctx, `
    SELECT 
        customer_id,
        customer_name,
		customer_last_name,
		customer_first_name,
        customer_tel,
        customer_address,
        customer_email,
        customer_nb_orders,
        customer_total_spent,
        creation_date,
        customer_code,
		advertising_consent,
		customer_brand
    FROM customer
    WHERE merchant_id = ?
      AND enabled = true
      AND customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')
      AND (
            customer_code = ?
         OR UPPER(customer_tel) LIKE UPPER(?)
         OR UPPER(customer_name) LIKE UPPER(?)
		 OR UPPER(customer_first_name) LIKE UPPER(?)
		 OR UPPER(customer_last_name) LIKE UPPER(?)
      )
`, merchantID, term, likeTerm, likeTerm, likeTerm, likeTerm)

	if err != nil {
		return results, err
	}
	defer rows.Close()

	for rows.Next() {
		var c CustomerSearchResult
		var creationDate sql.NullTime
		err := rows.Scan(
			&c.CustomerID,
			&c.CustomerName,
			&c.CustomerLastName,
			&c.CustomerFirstName,
			&c.CustomerTel,
			&c.CustomerAddress,
			&c.CustomerEmail,
			&c.CustomerNbOrders,
			&c.CustomerTotalSpent,
			&creationDate,
			&c.CustomerCode,
			&c.AdvertisingConsent,
			&c.CustomerBrand,
		)

		c.CreationDate = helpers.NullTimeToNullUnixInt(creationDate)
		if err != nil {
			logger.FromContext(ctx).Info("Error while scanning customers " + err.Error())
			continue // Ou return err, selon ton besoin
		}

		c.MatchScore = computeScore(term, &c)
		results = append(results, c)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].MatchScore > results[j].MatchScore
	})

	return results, nil
}

func normalizeStr(s *string) string {
	if s == nil {
		return ""
	}
	val := strings.TrimSpace(strings.ToUpper(*s))
	val = strings.ReplaceAll(val, " ", "")
	return val
}

func computeScore(term string, c *CustomerSearchResult) int {
	score := 0
	term = normalizeStr(&term)

	code := normalizeStr(c.CustomerCode)
	tel := normalizeStr(c.CustomerTel)
	lastName := normalizeStr(&c.CustomerLastName)
	firstName := normalizeStr(c.CustomerFirstName)

	// Correspondances exactes
	if code == term {
		score += 500
	}
	if tel == term {
		score += 300
	}
	if lastName == term {
		score += 200
	}
	if firstName == term {
		score += 200
	}

	// Correspondances partielles
	if strings.Contains(lastName, term) {
		score += 80
	}
	if strings.Contains(firstName, term) {
		score += 80
	}

	if strings.Contains(tel, term) {
		score += 120
	}

	// Bonus complétude profil
	if c.CustomerEmail != nil && *c.CustomerEmail != "" {
		score += 40
	}
	if c.CustomerTel != nil && *c.CustomerTel != "" {
		score += 40
	}
	if c.CustomerName != "" {
		score += 40
	}
	if c.CustomerAddress != nil && *c.CustomerAddress != "" {
		score += 40
	}

	score += c.CustomerNbOrders

	return score
}

func (r *CustomersRepository) ReactivateRewards(ctx context.Context, orderID string) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = false,
            usage_date = NULL,
            used_on_order_id = NULL
        WHERE used_on_order_id = ?`,
		orderID,
	)
	return err
}
