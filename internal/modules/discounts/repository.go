package discounts

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetActiveDiscounts retrieves all active discounts for a merchant (valid now)
func (r *Repository) GetActiveDiscounts(ctx context.Context, merchantID string) ([]Discount, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	now := time.Now().UTC()
	rows, err := db.QueryContext(ctx, `
		SELECT d.discount_id_new, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.merchant_id = ? AND d.enabled = true AND d.available = true
		  AND d.valid_from <= ? AND (d.valid_to IS NULL OR d.valid_to > ?)
		ORDER BY d.prefered_order ASC
	`, merchantID, now, now)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	discounts := make([]Discount, 0)
	discountIDsNew := make([]int, 0)
	for rows.Next() {
		var d Discount
		var discountIDNew int
		var orderType sql.NullString
		var validTo sql.NullTime

		if err := rows.Scan(
			&discountIDNew, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
			&d.PreferredOrder, &d.DiscountCode, &orderType,
			&d.DiscountValue, &d.DiscountUnit, &d.ValidFrom, &validTo,
			&d.MinOrderValue, &d.MinOrderUnit, &d.MaxDiscountValue,
			&d.MaxDiscountUnit, &d.DiscountedQuantity, &d.IsCumulative,
			&d.IsTimeLimited, &d.Available, &d.Enabled, &d.CreationDate,
		); err != nil {
			log.Error(err.Error())
			return nil, err
		}

		d.DiscountID = strconv.Itoa(discountIDNew)
		if orderType.Valid {
			ot := OrderType(orderType.String)
			d.OrderType = &ot
		}
		if validTo.Valid {
			d.ValidTo = &validTo.Time
		}

		discounts = append(discounts, d)
		discountIDsNew = append(discountIDsNew, discountIDNew)
	}

	if err := rows.Err(); err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// Load products and schedules AFTER closing rows to avoid deadlock
	for i := range discounts {
		if products, err := r.getDiscountProducts(ctx, discountIDsNew[i]); err == nil {
			discounts[i].Products = products
		}
		if schedules, err := r.getDiscountSchedules(ctx, discountIDsNew[i]); err == nil {
			discounts[i].Schedules = schedules
		}
	}

	return discounts, nil
}

// GetAllDiscounts retrieves all discounts for a merchant (regardless of validity)
func (r *Repository) GetAllDiscounts(ctx context.Context, merchantID string) ([]Discount, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT d.discount_id_new, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.merchant_id = ? AND d.enabled = true
		ORDER BY d.prefered_order ASC
	`, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	discounts := make([]Discount, 0)
	discountIDsNew := make([]int, 0)
	for rows.Next() {
		var d Discount
		var discountIDNew int
		var orderType sql.NullString
		var validTo sql.NullTime

		if err := rows.Scan(
			&discountIDNew, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
			&d.PreferredOrder, &d.DiscountCode, &orderType,
			&d.DiscountValue, &d.DiscountUnit, &d.ValidFrom, &validTo,
			&d.MinOrderValue, &d.MinOrderUnit, &d.MaxDiscountValue,
			&d.MaxDiscountUnit, &d.DiscountedQuantity, &d.IsCumulative,
			&d.IsTimeLimited, &d.Available, &d.Enabled, &d.CreationDate,
		); err != nil {
			log.Error(err.Error())
			return nil, err
		}

		d.DiscountID = strconv.Itoa(discountIDNew)
		if orderType.Valid {
			ot := OrderType(orderType.String)
			d.OrderType = &ot
		}
		if validTo.Valid {
			d.ValidTo = &validTo.Time
		}

		discounts = append(discounts, d)
		discountIDsNew = append(discountIDsNew, discountIDNew)
	}

	if err := rows.Err(); err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// Load products and schedules AFTER closing rows to avoid deadlock
	for i := range discounts {
		if products, err := r.getDiscountProducts(ctx, discountIDsNew[i]); err == nil {
			discounts[i].Products = products
		}
		if schedules, err := r.getDiscountSchedules(ctx, discountIDsNew[i]); err == nil {
			discounts[i].Schedules = schedules
		}
	}

	return discounts, nil
}

// GetDiscountByID retrieves a single discount by its integer ID (discount_id_new)
func (r *Repository) GetDiscountByID(ctx context.Context, merchantID string, discountIDNew int) (*Discount, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var d Discount
	var scannedID int
	var orderType sql.NullString
	var validTo sql.NullTime

	err := db.QueryRowContext(ctx, `
		SELECT d.discount_id_new, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.discount_id_new = ? AND d.merchant_id = ? AND d.enabled = true
	`, discountIDNew, merchantID).Scan(
		&scannedID, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
		&d.PreferredOrder, &d.DiscountCode, &orderType,
		&d.DiscountValue, &d.DiscountUnit, &d.ValidFrom, &validTo,
		&d.MinOrderValue, &d.MinOrderUnit, &d.MaxDiscountValue,
		&d.MaxDiscountUnit, &d.DiscountedQuantity, &d.IsCumulative,
		&d.IsTimeLimited, &d.Available, &d.Enabled, &d.CreationDate,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrDiscountNotFound
		}
		log.Error(err.Error())
		return nil, err
	}

	d.DiscountID = strconv.Itoa(scannedID)
	if orderType.Valid {
		ot := OrderType(orderType.String)
		d.OrderType = &ot
	}
	if validTo.Valid {
		d.ValidTo = &validTo.Time
	}

	// Load products and schedules
	if products, err := r.getDiscountProducts(ctx, scannedID); err == nil {
		d.Products = products
	}
	if schedules, err := r.getDiscountSchedules(ctx, scannedID); err == nil {
		d.Schedules = schedules
	}

	return &d, nil
}

// CreateDiscount creates a new discount with products and schedules
func (r *Repository) CreateDiscount(ctx context.Context, merchantID string, req *CreateDiscountRequest) (*Discount, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// min_order_value is NOT NULL DEFAULT 0, but since the column is always
	// listed explicitly below, an explicit NULL parameter overrides that
	// default instead of falling back to it. Default nil here so any caller
	// that omits/nulls this optional field still gets the column's own
	// "no minimum" semantics instead of a constraint violation.
	minOrderValue := 0.0
	if req.MinOrderValue != nil {
		minOrderValue = *req.MinOrderValue
	}

	// Insert discount. discount_id_new is not listed: it gets its value from
	// the column DEFAULT (nextval on discounts_discount_id_new_seq, set up by
	// migration 118) — req.DiscountID (varchar) remains the legacy/transition
	// identifier, still the physical PRIMARY KEY until a future contraction lot.
	var discountIDNew int
	err := db.QueryRowContext(ctx, `
		INSERT INTO discounts (
			discount_id, merchant_id, discount_name, discount_desc, prefered_order,
			discount_code, discount_order_type, discount_value, discount_unit,
			valid_from, valid_to, min_order_value, min_order_unit,
			max_discount_value, max_discount_unit, discounted_quantity,
			is_cumulative, is_time_limited, available, enabled, creation_date
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING discount_id_new
	`,
		req.DiscountID, merchantID, req.DiscountName, req.DiscountDesc, req.PreferredOrder,
		req.DiscountCode, req.OrderType, req.DiscountValue, req.DiscountUnit,
		req.ValidFrom, req.ValidTo, minOrderValue, req.MinOrderUnit,
		req.MaxDiscountValue, req.MaxDiscountUnit, req.DiscountedQuantity,
		req.IsCumulative, req.IsTimeLimited, req.Available, true, time.Now().UTC(),
	).Scan(&discountIDNew)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// Insert products
	for _, p := range req.Products {
		_, err := db.ExecContext(ctx, `
			INSERT INTO discounts_products (discount_id, discount_id_new, product_id, new_price, enabled)
			VALUES (?, ?, ?, ?, true)
		`, req.DiscountID, discountIDNew, p.ProductID, p.NewPrice)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Insert schedules
	for _, s := range req.Schedules {
		// Format TIME columns as HH:MM:SS strings for MySQL TIME type
		availableFromStr := s.AvailableFrom.Format("15:04:05")
		availableToStr := s.AvailableTo.Format("15:04:05")

		_, err := db.ExecContext(ctx, `
			INSERT INTO discounts_schedules (discount_id, discount_id_new, day_of_week, available_from, available_to, enabled)
			VALUES (?, ?, ?, ?, ?, true)
		`, req.DiscountID, discountIDNew, s.DayOfWeek, availableFromStr, availableToStr)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Fetch and return the created discount
	return r.GetDiscountByID(ctx, merchantID, discountIDNew)
}

// UpdateDiscount updates an existing discount, identified by its integer ID (discount_id_new)
func (r *Repository) UpdateDiscount(ctx context.Context, merchantID string, discountIDNew int, req *UpdateDiscountRequest) (*Discount, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Build dynamic UPDATE query
	updates := []string{}
	args := []interface{}{}

	if req.DiscountName != nil {
		updates = append(updates, "discount_name = ?")
		args = append(args, *req.DiscountName)
	}
	if req.DiscountDesc != nil {
		updates = append(updates, "discount_desc = ?")
		args = append(args, *req.DiscountDesc)
	}
	if req.PreferredOrder != nil {
		updates = append(updates, "prefered_order = ?")
		args = append(args, *req.PreferredOrder)
	}
	if req.DiscountCode != nil {
		updates = append(updates, "discount_code = ?")
		args = append(args, *req.DiscountCode)
	}
	if req.OrderType != nil {
		updates = append(updates, "discount_order_type = ?")
		args = append(args, *req.OrderType)
	}
	if req.DiscountValue != nil {
		updates = append(updates, "discount_value = ?")
		args = append(args, *req.DiscountValue)
	}
	if req.DiscountUnit != nil {
		updates = append(updates, "discount_unit = ?")
		args = append(args, *req.DiscountUnit)
	}
	if req.ValidFrom != nil {
		updates = append(updates, "valid_from = ?")
		args = append(args, *req.ValidFrom)
	}
	if req.ValidTo != nil {
		updates = append(updates, "valid_to = ?")
		args = append(args, *req.ValidTo)
	}
	if req.MinOrderValue != nil {
		updates = append(updates, "min_order_value = ?")
		args = append(args, *req.MinOrderValue)
	}
	if req.MinOrderUnit != nil {
		updates = append(updates, "min_order_unit = ?")
		args = append(args, *req.MinOrderUnit)
	}
	if req.MaxDiscountValue != nil {
		updates = append(updates, "max_discount_value = ?")
		args = append(args, *req.MaxDiscountValue)
	}
	if req.MaxDiscountUnit != nil {
		updates = append(updates, "max_discount_unit = ?")
		args = append(args, *req.MaxDiscountUnit)
	}
	if req.DiscountedQuantity != nil {
		updates = append(updates, "discounted_quantity = ?")
		args = append(args, *req.DiscountedQuantity)
	}
	if req.IsCumulative != nil {
		updates = append(updates, "is_cumulative = ?")
		args = append(args, *req.IsCumulative)
	}
	if req.IsTimeLimited != nil {
		updates = append(updates, "is_time_limited = ?")
		args = append(args, *req.IsTimeLimited)
	}
	if req.Available != nil {
		updates = append(updates, "available = ?")
		args = append(args, *req.Available)
	}

	// Execute update if there are updates
	if len(updates) > 0 {
		args = append(args, discountIDNew, merchantID)
		updateSQL := "UPDATE discounts SET " + strings.Join(updates, ", ") + " WHERE discount_id_new = ? AND merchant_id = ? AND enabled = true"
		if _, err := db.ExecContext(ctx, updateSQL, args...); err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Update products if provided
	if len(req.Products) > 0 {
		// Delete existing products
		if _, err := db.ExecContext(ctx, "DELETE FROM discounts_products WHERE discount_id_new = ?", discountIDNew); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		// Insert new products. discount_id (varchar) is still NOT NULL on
		// discounts_products — pulled from discounts via subquery rather than
		// requiring a round trip here.
		for _, p := range req.Products {
			_, err := db.ExecContext(ctx, `
				INSERT INTO discounts_products (discount_id, discount_id_new, product_id, new_price, enabled)
				SELECT d.discount_id, ?, ?, ?, true FROM discounts d WHERE d.discount_id_new = ?
			`, discountIDNew, p.ProductID, p.NewPrice, discountIDNew)
			if err != nil {
				log.Error(err.Error())
				return nil, err
			}
		}
	}

	// Update schedules if provided
	if len(req.Schedules) > 0 {
		// Delete existing schedules
		if _, err := db.ExecContext(ctx, "DELETE FROM discounts_schedules WHERE discount_id_new = ?", discountIDNew); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		// Insert new schedules
		for _, s := range req.Schedules {
			// Format TIME columns as HH:MM:SS strings (matches CreateDiscount;
			// avoids pgx inferring a timestamptz parameter type for a time.Time
			// bound to a `time` column).
			availableFromStr := s.AvailableFrom.Format("15:04:05")
			availableToStr := s.AvailableTo.Format("15:04:05")

			_, err := db.ExecContext(ctx, `
				INSERT INTO discounts_schedules (discount_id, discount_id_new, day_of_week, available_from, available_to, enabled)
				SELECT d.discount_id, ?, ?, ?, ?, true FROM discounts d WHERE d.discount_id_new = ?
			`, discountIDNew, s.DayOfWeek, availableFromStr, availableToStr, discountIDNew)
			if err != nil {
				log.Error(err.Error())
				return nil, err
			}
		}
	}

	return r.GetDiscountByID(ctx, merchantID, discountIDNew)
}

// DeleteDiscount performs a soft delete on a discount, identified by its integer ID (discount_id_new)
func (r *Repository) DeleteDiscount(ctx context.Context, merchantID string, discountIDNew int) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// "AND enabled = true" makes RowsAffected consistent across dialects: MySQL's
	// default affected-rows counts only changed rows (a second delete would already
	// report 0), Postgres counts matched rows regardless of value change (would
	// report 1 without this guard) — scoping the match to still-enabled rows keeps
	// the not-found semantics identical on both.
	result, err := db.ExecContext(ctx, `
		UPDATE discounts SET enabled = false WHERE discount_id_new = ? AND merchant_id = ? AND enabled = true
	`, discountIDNew, merchantID)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Error(err.Error())
		return err
	}
	if rowsAffected == 0 {
		return ErrDiscountNotFound
	}

	return nil
}

// Helper functions

func (r *Repository) getDiscountProducts(ctx context.Context, discountIDNew int) ([]DiscountProduct, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT id, discount_id_new, product_id, new_price, enabled
		FROM discounts_products
		WHERE discount_id_new = ? AND enabled = true
	`, discountIDNew)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []DiscountProduct
	for rows.Next() {
		var p DiscountProduct
		var discID int
		if err := rows.Scan(&p.ID, &discID, &p.ProductID, &p.NewPrice, &p.Enabled); err != nil {
			return nil, err
		}
		p.DiscountID = strconv.Itoa(discID)
		products = append(products, p)
	}

	return products, rows.Err()
}

func (r *Repository) getDiscountSchedules(ctx context.Context, discountIDNew int) ([]DiscountSchedule, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT schedule_id, discount_id_new, day_of_week, available_from, available_to, enabled
		FROM discounts_schedules
		WHERE discount_id_new = ? AND enabled = true
	`, discountIDNew)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []DiscountSchedule
	for rows.Next() {
		var s DiscountSchedule
		var discID int
		var availableFrom, availableTo string // Read TIME columns as strings

		if err := rows.Scan(&s.ScheduleID, &discID, &s.DayOfWeek, &availableFrom, &availableTo, &s.Enabled); err != nil {
			return nil, err
		}
		s.DiscountID = strconv.Itoa(discID)

		// Parse TIME strings to TimeOfDay (format: "HH:MM:SS")
		if t, err := time.Parse("15:04:05", availableFrom); err == nil {
			s.AvailableFrom = TimeOfDay(t)
		}
		if t, err := time.Parse("15:04:05", availableTo); err == nil {
			s.AvailableTo = TimeOfDay(t)
		}

		schedules = append(schedules, s)
	}

	return schedules, rows.Err()
}
