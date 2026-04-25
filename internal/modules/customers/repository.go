package customers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type CustomersRepository struct {
	database *sql.DB
}

func NewCustomerRepository(db *sql.DB) *CustomersRepository {
	return &CustomersRepository{database: db}
}

func (r *CustomersRepository) UpdateOrCreateCustomer(ctx context.Context, c *models.Customer) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)

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

		_, err := db.ExecContext(ctx, query, args...)
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
	res, err := db.ExecContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}

	id, _ := res.LastInsertId()

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

func (r *CustomersRepository) GetCustomerLoyalty(ctx context.Context, customerID, merchantID string) (*CustomerLoyalty, error) {

	loyalty := &CustomerLoyalty{}

	// progress
	rows, err := r.database.QueryContext(ctx, `
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
        WHERE c.customer_id = ? AND c.merchant_id = ? AND p.enabled = 1
    `, customerID, merchantID)
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
	rows2, err := r.database.QueryContext(ctx, `
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

func (r *CustomersRepository) UpdateLoyaltyProgress(ctx context.Context, req *LoyaltyProgressUpdateRequest, merchantID string) (int, error) {
	db := dbutils.GetDB(ctx, r.database)

	/*
		tx, err := r.database.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
	*/

	var targetValue int
	var rewardType string
	var rewardValue int
	var rewardOrderType string

	// 1. Fetch program
	err := db.QueryRowContext(ctx, `
        SELECT target_value, reward_type, reward_value, rewards_order_type
        FROM customer_loyalty_programs
        WHERE id = ? AND merchant_id = ? AND enabled = 1
    `, req.LoyaltyProgramID, merchantID).Scan(&targetValue, &rewardType, &rewardValue, &rewardOrderType)
	if err != nil {
		return 0, err
	}

	// 2. fetch progress
	var progressID string
	var oldValue int

	err = db.QueryRowContext(ctx, `
        SELECT id, current_value 
        FROM customer_loyalty_progress
        WHERE customer_id = ? AND loyalty_program_id = ?
    `, req.CustomerID, req.LoyaltyProgramID).Scan(&progressID, &oldValue)

	exists := (err == nil)

	oldRewards := oldValue / targetValue
	newRewards := req.CurrentValue / targetValue
	rewardToCreate := newRewards - oldRewards

	if exists {
		_, err = db.ExecContext(ctx, `
            UPDATE customer_loyalty_progress
            SET current_value = ?, last_update = UTC_TIMESTAMP
            WHERE id = ?
        `, req.CurrentValue, progressID)
		if err != nil {
			return 0, err
		}
	} else {
		_, err = db.ExecContext(ctx, `
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
			_, err = db.ExecContext(ctx, `
                INSERT INTO customer_rewards (customer_id, loyalty_program_id, reward_type, reward_order_type, reward_value, creation_date)
                VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP)
            `, req.CustomerID, req.LoyaltyProgramID, rewardType, rewardOrderType, rewardValue)
			if err != nil {
				return 0, err
			}
		}
	}

	/*
		if err := tx.Commit(); err != nil {
			return 0, err
		}
	*/

	return rewardToCreate, nil
}

func (r *CustomersRepository) UpdateLoyaltyReward(ctx context.Context, req *LoyaltyRewardUpdateRequest, merchantID string) error {
	// TODO : Vérifier que la reward appartient bien au client et au marchand avant de l'updater (sécurité)

	_, err := r.database.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = ?, usage_date = UTC_TIMESTAMP
        WHERE reward_id = ? AND customer_id IN (SELECT customer_id FROM customers WHERE merchant_id = ? AND enabled = 1)
    `, req.IsUsed, req.RewardID, merchantID)

	return err
}

func (r *CustomersRepository) SearchCustomers(ctx context.Context, merchantID, term, sortField, sortDir string, page, pageSize int) ([]CustomerSearchResult, int, error) {
	var results []CustomerSearchResult

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	fromClause := `
    FROM customer c
    WHERE c.merchant_id = ?
      AND c.enabled = true
      AND c.customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')
`
	args := []interface{}{merchantID}

	trimmedTerm := strings.TrimSpace(term)
	if trimmedTerm != "" {
		likeTerm := "%" + trimmedTerm + "%"
		fromClause += `
      AND (
            c.customer_code = ?
         OR UPPER(COALESCE(c.customer_tel, '')) LIKE UPPER(?)
         OR UPPER(COALESCE(c.customer_name, '')) LIKE UPPER(?)
		 OR UPPER(COALESCE(c.customer_first_name, '')) LIKE UPPER(?)
		 OR UPPER(COALESCE(c.customer_last_name, '')) LIKE UPPER(?)
      )
`
		args = append(args, trimmedTerm, likeTerm, likeTerm, likeTerm, likeTerm)
	}

	countQuery := `SELECT COUNT(*) ` + fromClause
	var totalItems int
	if err := r.database.QueryRowContext(ctx, countQuery, args...).Scan(&totalItems); err != nil {
		return results, 0, err
	}

	orderBy := buildCustomerOrderByClause(sortField, sortDir)
	query := `
    SELECT
        c.customer_id,
        c.customer_name,
		c.customer_last_name,
		c.customer_first_name,
        c.customer_tel,
        c.customer_address,
        c.customer_email,
        c.customer_nb_orders,
        c.customer_total_spent,
        c.creation_date,
        c.last_order_date,
        c.customer_code,
		c.advertising_consent,
		c.customer_brand
` + fromClause + orderBy + `
	LIMIT ? OFFSET ?
`

	queryArgs := append(args, pageSize, offset)
	rows, err := r.database.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return results, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var c CustomerSearchResult
		var creationDate sql.NullTime
		var lastOrderDate sql.NullTime
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
			&lastOrderDate,
			&c.CustomerCode,
			&c.AdvertisingConsent,
			&c.CustomerBrand,
		)

		c.CreationDate = nullTimeToUTCISOZ(creationDate)
		c.LastOrderDate = nullTimeToUTCISOZ(lastOrderDate)
		if err != nil {
			logger.FromContext(ctx).Info("Error while scanning customers " + err.Error())
			continue
		}

		c.MatchScore = computeScore(term, &c)
		results = append(results, c)
	}

	return results, totalItems, nil
}

func (r *CustomersRepository) ListCustomers(ctx context.Context, merchantID, sortField, sortDir string, page, pageSize int) ([]CustomerSearchResult, int, error) {
	var results []CustomerSearchResult

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	fromClause := `
    FROM customer c
    WHERE c.merchant_id = ?
      AND c.enabled = true
      AND c.customer_brand NOT IN ('UBER_EATS', 'DELIVEROO')
`

	var totalItems int
	countQuery := `SELECT COUNT(*) ` + fromClause
	if err := r.database.QueryRowContext(ctx, countQuery, merchantID).Scan(&totalItems); err != nil {
		return results, 0, err
	}

	orderBy := buildCustomerOrderByClause(sortField, sortDir)
	query := `
    SELECT
        c.customer_id,
        c.customer_name,
		c.customer_last_name,
		c.customer_first_name,
        c.customer_tel,
        c.customer_address,
        c.customer_email,
        c.customer_nb_orders,
        c.customer_total_spent,
        c.creation_date,
        c.last_order_date,
        c.customer_code,
		c.advertising_consent,
		c.customer_brand
` + fromClause + orderBy + `
	LIMIT ? OFFSET ?
`

	rows, err := r.database.QueryContext(ctx, query, merchantID, pageSize, offset)
	if err != nil {
		return results, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var c CustomerSearchResult
		var creationDate sql.NullTime
		var lastOrderDate sql.NullTime
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
			&lastOrderDate,
			&c.CustomerCode,
			&c.AdvertisingConsent,
			&c.CustomerBrand,
		)

		c.CreationDate = nullTimeToUTCISOZ(creationDate)
		c.LastOrderDate = nullTimeToUTCISOZ(lastOrderDate)
		if err != nil {
			logger.FromContext(ctx).Info("Error while scanning customers " + err.Error())
			continue
		}

		results = append(results, c)
	}

	return results, totalItems, nil
}

func buildCustomerOrderByClause(sortField, sortDir string) string {
	field := strings.ToLower(strings.TrimSpace(sortField))
	dir := strings.ToUpper(strings.TrimSpace(sortDir))

	if dir != "ASC" && dir != "DESC" {
		dir = "DESC"
	}

	fieldExpr := "c.creation_date"
	isText := false

	switch field {
	case "customer_first_name":
		fieldExpr = "LOWER(c.customer_first_name)"
		isText = true
	case "customer_email":
		fieldExpr = "LOWER(c.customer_email)"
		isText = true
	case "customer_tel":
		fieldExpr = "LOWER(c.customer_tel)"
		isText = true
	case "customer_nb_orders":
		fieldExpr = "c.customer_nb_orders"
	case "customer_total_spent":
		fieldExpr = "c.customer_total_spent"
	case "creation_date":
		fieldExpr = "c.creation_date"
	case "last_order_date":
		fieldExpr = "c.last_order_date"
	}

	if isText {
		return fmt.Sprintf(" ORDER BY CASE WHEN %s IS NULL OR %s = '' THEN 1 ELSE 0 END ASC, %s %s, c.customer_id ASC ", fieldExpr, fieldExpr, fieldExpr, dir)
	}

	return fmt.Sprintf(" ORDER BY CASE WHEN %s IS NULL THEN 1 ELSE 0 END ASC, %s %s, c.customer_id ASC ", fieldExpr, fieldExpr, dir)
}

func nullTimeToUTCISOZ(nt sql.NullTime) *string {
	if !nt.Valid {
		return nil
	}
	formatted := nt.Time.UTC().Format(time.RFC3339)
	return &formatted
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
	lastName := normalizeStr(c.CustomerLastName)
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
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx, `
        UPDATE customer_rewards
        SET is_used = false,
            usage_date = NULL,
            used_on_order_id = NULL
        WHERE used_on_order_id = ?`,
		orderID,
	)
	return err
}

// Internal struct pour récupérer les données du programme de fidélité
func (r *CustomersRepository) UpdateLoyaltyFromOrder(ctx context.Context, orderID string) error {
	log := logger.FromContext(ctx)
	db := dbutils.GetDB(ctx, r.database)

	/*
		tx, err := r.database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback() // Se déclenche auto si la fonction crash ou s'arrête sans Commit
	*/

	// 1. Mise à jour des stats globales du client
	const qUpdateStats = `
		UPDATE customer c
		INNER JOIN orders o ON o.customer_id = c.customer_id
		SET c.customer_nb_orders = c.customer_nb_orders + 1,
			c.customer_total_spent = c.customer_total_spent + o.price,
			c.last_order_date = UTC_TIMESTAMP(),
			c.loyalty_reminder_count = 0
		WHERE o.order_id = ?
	`
	if _, err := db.ExecContext(ctx, qUpdateStats, orderID); err != nil {
		return err
	}

	// 2. Récupérer les infos de la commande
	const qGetOrder = `
		SELECT o.customer_id, o.merchant_id, o.price, o.order_type
		FROM orders o
		WHERE o.order_id = ? AND o.brand = 'WELLO_RESTO'
	`
	var customerID, merchantID, orderType string
	var price int

	err := db.QueryRowContext(ctx, qGetOrder, orderID).Scan(&customerID, &merchantID, &price, &orderType)
	if err == sql.ErrNoRows || customerID == "" {
		// Pas de commande trouvée, pas WELLO_RESTO, ou pas de client rattaché -> On s'arrête avec succès
		return nil
	} else if err != nil {
		return err
	}

	// 3. Récupérer les programmes actifs du marchand
	const qGetPrograms = `
		SELECT id, type, target_value, reward_type, reward_value, rewards_order_type
		FROM customer_loyalty_programs
		WHERE merchant_id = ? AND enabled = 1
	`
	rows, err := db.QueryContext(ctx, qGetPrograms, merchantID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var programs []loyaltyProgram
	for rows.Next() {
		var p loyaltyProgram
		if err := rows.Scan(&p.ID, &p.Type, &p.TargetValue, &p.RewardType, &p.RewardValue, &p.RewardsOrderType); err != nil {
			return err
		}
		programs = append(programs, p)
	}
	rows.Close()

	// 4. Parcourir et appliquer la logique pour chaque programme
	for _, p := range programs {
		// Vérifier si la commande a déjà été comptée
		var exists int
		err := db.QueryRowContext(ctx, "SELECT 1 FROM customer_loyalty_progress_order WHERE order_id = ? AND loyalty_program_id = ? LIMIT 1", orderID, p.ID).Scan(&exists)
		if err == nil {
			continue // Déjà traitée
		} else if err != sql.ErrNoRows {
			return err
		}

		// Récupérer la progression actuelle
		var progressID string
		var currentValue int
		err = db.QueryRowContext(ctx, "SELECT id, current_value FROM customer_loyalty_progress WHERE customer_id = ? AND loyalty_program_id = ? LIMIT 1", customerID, p.ID).Scan(&progressID, &currentValue)

		if err == sql.ErrNoRows {
			// Créer la progression
			res, err := db.ExecContext(ctx, "INSERT INTO customer_loyalty_progress (customer_id, loyalty_program_id, current_value, last_update) VALUES (?, ?, 0, UTC_TIMESTAMP())", customerID, p.ID)
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			// Attention : LastInsertId retourne un int64. Si ton ID est un UUID/string dans ta DB, adapte cette partie.
			// (Si l'ID n'est pas auto-increment, il faut générer un UUID ici et l'insérer)
			// Dans le doute, je simule une base standard :
			err = db.QueryRowContext(ctx, "SELECT id FROM customer_loyalty_progress WHERE id = ?", id).Scan(&progressID)
			currentValue = 0
		} else if err != nil {
			return err
		}

		// 5. Calculer l'incrémentation
		increment := 0
		switch p.Type {
		case "orders_count":
			increment = 1
		case "total_spent":
			increment = price
		case "product_count":
			// OPTIMISATION GO : On fait le sum() et la vérification des produits cibles directement en SQL !
			const qSumProducts = `
				SELECT COALESCE(SUM(oi.quantity), 0)
				FROM orderitems oi
				INNER JOIN customer_loyalty_program_target_products tp ON tp.product_id = oi.product_id
				WHERE oi.order_id = ? AND tp.loyalty_program_id = ?
			`
			_ = db.QueryRowContext(ctx, qSumProducts, orderID, p.ID).Scan(&increment)
		}

		if increment == 0 {
			continue // Rien à ajouter pour cette commande
		}

		newValue := currentValue + increment

		// 6. Mettre à jour la progression et loguer
		_, err = db.ExecContext(ctx, "UPDATE customer_loyalty_progress SET current_value = ?, last_update = UTC_TIMESTAMP() WHERE id = ?", newValue, progressID)
		if err != nil {
			return err
		}

		_, err = db.ExecContext(ctx, "INSERT INTO customer_loyalty_progress_order (loyalty_program_id, progress_id, order_id, increment_value) VALUES (?, ?, ?, ?)", p.ID, progressID, orderID, increment)
		if err != nil {
			return err
		}

		// 7. Vérifier les paliers (Rewards)
		if p.TargetValue > 0 {
			rewardsAlready := currentValue / p.TargetValue // Division entière en Go
			rewardsExpected := newValue / p.TargetValue
			rewardsToAdd := rewardsExpected - rewardsAlready

			for i := 0; i < rewardsToAdd; i++ {
				_, err = db.ExecContext(ctx, `
					INSERT INTO customer_rewards(loyalty_program_id, customer_id, reward_type, reward_order_type, reward_value, creation_date)
					VALUES (?, ?, ?, ?, ?, UTC_TIMESTAMP())
				`, p.ID, customerID, p.RewardType, p.RewardsOrderType, p.RewardValue)

				if err != nil {
					return err
				}
				log.Info("🎉 Nouvelle récompense ajoutée pour le client " + customerID)
			}
		}
	}

	// return tx.Commit()
	return nil
}
