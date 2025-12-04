package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type CustomersRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewCustomerRepository(db *sql.DB, log *zap.Logger) *CustomersRepository {
	return &CustomersRepository{db: db, log: log}
}

func normalizePhoneNumber(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, ".", "")
	phone = strings.ReplaceAll(phone, "-", "")
	return phone
}

func ucfirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (r *CustomersRepository) UpdateOrCreateCustomer(ctx context.Context, c *models.Customer) (string, error) {

	// Liste des colonnes vraiment existantes et autorisées
	allowed := map[string]bool{
		"customer_name":                         true,
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
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

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
			return *c.CustomerID, nil
		}

		query := `
            UPDATE customer
            SET ` + strings.Join(setParts, ", ") + `
            WHERE customer_id = ? AND merchant_id = ?
        `
		args = append(args, *c.CustomerID, c.MerchantID)

		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return "", err
		}

		tx.Commit()
		return *c.CustomerID, nil
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
		return "", err
	}

	id, _ := res.LastInsertId()
	tx.Commit()

	return fmt.Sprintf("%d", id), nil
}

func extractFieldValue(c *models.Customer, field string) interface{} {

	switch field {

	case "customer_name":
		if c.CustomerName != nil && *c.CustomerName != "" {
			return ucfirst(*c.CustomerName)
		}
		return nil

	case "customer_tel":
		if c.CustomerTel != nil && *c.CustomerTel != "" {
			return normalizePhoneNumber(*c.CustomerTel)
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

func (r *CustomersRepository) GetCustomerLoyalty(ctx context.Context, customerID string) (*models.CustomerLoyalty, error) {

	loyalty := &models.CustomerLoyalty{}

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
		var p models.LoyaltyProgress
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
        SELECT reward_id, loyalty_program_id, creation_date, reward_type, reward_value, is_used
        FROM customer_rewards
        WHERE customer_id = ?
    `, customerID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var rwd models.LoyaltyReward
		if err := rows2.Scan(
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

func (r *CustomersRepository) UpdateLoyaltyProgress(ctx context.Context, req *models.LoyaltyProgressUpdateRequest) (int, error) {

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

func (r *CustomersRepository) UpdateLoyaltyReward(ctx context.Context, req *models.LoyaltyRewardUpdateRequest) error {

	_, err := r.db.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = ?, usage_date = UTC_TIMESTAMP
        WHERE reward_id = ?
    `, req.IsUsed, req.RewardID)

	return err
}

func (r *CustomersRepository) SearchCustomers(ctx context.Context, merchantID string, p *models.CustomerSearchRequest) ([]models.CustomerSearchResult, error) {

	name := normalizeStr(p.Name)
	tel := normalizeStr(p.Tel)
	address := normalizeStr(p.Address)
	code := normalizeStr(p.Code)

	likeName := "%" + name + "%"
	likeTel := "%" + tel + "%"
	likeAddress := "%" + address + "%"

	rows, err := r.db.QueryContext(ctx, `
        SELECT 
            customer_id,
            customer_name,
            customer_tel,
            customer_address,
            customer_email,
            customer_nb_orders,
            customer_total_spent,
            creation_date,
            customer_code
        FROM customer
        WHERE merchant_id = ?
          AND enabled = 1
          AND customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')
          AND (
                customer_code = ?
             OR customer_tel LIKE ?
             OR customer_name LIKE ?
             OR customer_address LIKE ?
          )
        LIMIT 50
    `, merchantID, code, likeTel, likeName, likeAddress)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.CustomerSearchResult

	for rows.Next() {
		var c models.CustomerSearchResult
		rows.Scan(
			&c.CustomerID,
			&c.CustomerName,
			&c.CustomerTel,
			&c.CustomerAddress,
			&c.CustomerEmail,
			&c.CustomerNbOrders,
			&c.CustomerTotalSpent,
			&c.CreationDate,
			&c.CustomerCode,
		)

		// 🔥 Match scoring ultra rapide
		c.MatchScore = computeScore(name, tel, address, code, &c)

		results = append(results, c)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].MatchScore > results[j].MatchScore
	})

	return results, nil
}

func normalizeStr(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func computeScore(name, tel, address, code string, c *models.CustomerSearchResult) int {
	score := 0

	// Correspondance exacte → priorité
	if normalizeStr(c.CustomerCode) == code {
		score += 500
	}
	if normalizeStr(c.CustomerTel) == tel {
		score += 300
	}
	if normalizeStr(c.CustomerName) == name {
		score += 200
	}

	// Similarité simple
	if strings.Contains(normalizeStr(c.CustomerName), name) {
		score += 80
	}
	if strings.Contains(normalizeStr(c.CustomerTel), tel) {
		score += 120
	}
	if strings.Contains(normalizeStr(c.CustomerAddress), address) {
		score += 40
	}

	return score
}
