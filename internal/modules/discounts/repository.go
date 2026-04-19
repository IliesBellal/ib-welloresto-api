package discounts

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// GetActiveDiscounts retrieves all active discounts for a merchant (valid now)
func (r *Repository) GetActiveDiscounts(ctx context.Context, merchantID string) ([]Discount, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	now := time.Now().UTC()
	rows, err := db.QueryContext(ctx, `
		SELECT d.discount_id, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.merchant_id = ? AND d.enabled = 1 AND d.available = 1
		  AND d.valid_from <= ? AND (d.valid_to IS NULL OR d.valid_to > ?)
		ORDER BY d.prefered_order ASC
	`, merchantID, now, now)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var discounts []Discount
	for rows.Next() {
		var d Discount
		var orderType sql.NullString
		var validTo sql.NullTime

		if err := rows.Scan(
			&d.DiscountID, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
			&d.PreferredOrder, &d.DiscountCode, &orderType,
			&d.DiscountValue, &d.DiscountUnit, &d.ValidFrom, &validTo,
			&d.MinOrderValue, &d.MinOrderUnit, &d.MaxDiscountValue,
			&d.MaxDiscountUnit, &d.DiscountedQuantity, &d.IsCumulative,
			&d.IsTimeLimited, &d.Available, &d.Enabled, &d.CreationDate,
		); err != nil {
			log.Error(err.Error())
			return nil, err
		}

		if orderType.Valid {
			ot := OrderType(orderType.String)
			d.OrderType = &ot
		}
		if validTo.Valid {
			d.ValidTo = &validTo.Time
		}

		// Load products and schedules
		if products, err := r.getDiscountProducts(ctx, d.DiscountID); err == nil {
			d.Products = products
		}
		if schedules, err := r.getDiscountSchedules(ctx, d.DiscountID); err == nil {
			d.Schedules = schedules
		}

		discounts = append(discounts, d)
	}

	return discounts, rows.Err()
}

// GetAllDiscounts retrieves all discounts for a merchant (regardless of validity)
func (r *Repository) GetAllDiscounts(ctx context.Context, merchantID string) ([]Discount, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx, `
		SELECT d.discount_id, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.merchant_id = ? AND d.enabled = 1
		ORDER BY d.prefered_order ASC
	`, merchantID)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var discounts []Discount
	for rows.Next() {
		var d Discount
		var orderType sql.NullString
		var validTo sql.NullTime

		if err := rows.Scan(
			&d.DiscountID, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
			&d.PreferredOrder, &d.DiscountCode, &orderType,
			&d.DiscountValue, &d.DiscountUnit, &d.ValidFrom, &validTo,
			&d.MinOrderValue, &d.MinOrderUnit, &d.MaxDiscountValue,
			&d.MaxDiscountUnit, &d.DiscountedQuantity, &d.IsCumulative,
			&d.IsTimeLimited, &d.Available, &d.Enabled, &d.CreationDate,
		); err != nil {
			log.Error(err.Error())
			return nil, err
		}

		if orderType.Valid {
			ot := OrderType(orderType.String)
			d.OrderType = &ot
		}
		if validTo.Valid {
			d.ValidTo = &validTo.Time
		}

		// Load products and schedules
		if products, err := r.getDiscountProducts(ctx, d.DiscountID); err == nil {
			d.Products = products
		}
		if schedules, err := r.getDiscountSchedules(ctx, d.DiscountID); err == nil {
			d.Schedules = schedules
		}

		discounts = append(discounts, d)
	}

	return discounts, rows.Err()
}

// GetDiscountByID retrieves a single discount by ID
func (r *Repository) GetDiscountByID(ctx context.Context, merchantID string, discountID string) (*Discount, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	var d Discount
	var orderType sql.NullString
	var validTo sql.NullTime

	err := db.QueryRowContext(ctx, `
		SELECT d.discount_id, d.merchant_id, d.discount_name, d.discount_desc,
		       d.prefered_order, d.discount_code, d.discount_order_type,
		       d.discount_value, d.discount_unit, d.valid_from, d.valid_to,
		       d.min_order_value, d.min_order_unit, d.max_discount_value,
		       d.max_discount_unit, d.discounted_quantity, d.is_cumulative,
		       d.is_time_limited, d.available, d.enabled, d.creation_date
		FROM discounts d
		WHERE d.discount_id = ? AND d.merchant_id = ? AND d.enabled = 1
	`, discountID, merchantID).Scan(
		&d.DiscountID, &d.MerchantID, &d.DiscountName, &d.DiscountDesc,
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

	if orderType.Valid {
		ot := OrderType(orderType.String)
		d.OrderType = &ot
	}
	if validTo.Valid {
		d.ValidTo = &validTo.Time
	}

	// Load products and schedules
	if products, err := r.getDiscountProducts(ctx, d.DiscountID); err == nil {
		d.Products = products
	}
	if schedules, err := r.getDiscountSchedules(ctx, d.DiscountID); err == nil {
		d.Schedules = schedules
	}

	return &d, nil
}

// CreateDiscount creates a new discount with products and schedules
func (r *Repository) CreateDiscount(ctx context.Context, merchantID string, req *CreateDiscountRequest) (*Discount, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Insert discount
	result, err := db.ExecContext(ctx, `
		INSERT INTO discounts (
			merchant_id, discount_name, discount_desc, prefered_order,
			discount_code, discount_order_type, discount_value, discount_unit,
			valid_from, valid_to, min_order_value, min_order_unit,
			max_discount_value, max_discount_unit, discounted_quantity,
			is_cumulative, is_time_limited, available, enabled, creation_date
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		merchantID, req.DiscountName, req.DiscountDesc, req.PreferredOrder,
		req.DiscountCode, req.OrderType, req.DiscountValue, req.DiscountUnit,
		req.ValidFrom, req.ValidTo, req.MinOrderValue, req.MinOrderUnit,
		req.MaxDiscountValue, req.MaxDiscountUnit, req.DiscountedQuantity,
		req.IsCumulative, req.IsTimeLimited, req.Available, 1, time.Now().UTC(),
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	discountID, err := result.LastInsertId()
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	discountIDStr := strconv.FormatInt(discountID, 10)

	// Insert products
	for _, p := range req.Products {
		_, err := db.ExecContext(ctx, `
			INSERT INTO discounts_products (discount_id, product_id, new_price, enabled)
			VALUES (?, ?, ?, 1)
		`, discountIDStr, p.ProductID, p.NewPrice)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Insert schedules
	for _, s := range req.Schedules {
		_, err := db.ExecContext(ctx, `
			INSERT INTO discounts_schedules (discount_id, day_of_week, available_from, available_to, enabled)
			VALUES (?, ?, ?, ?, 1)
		`, discountIDStr, s.DayOfWeek, s.AvailableFrom, s.AvailableTo)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Fetch and return the created discount
	return r.GetDiscountByID(ctx, merchantID, discountIDStr)
}

// UpdateDiscount updates an existing discount
func (r *Repository) UpdateDiscount(ctx context.Context, merchantID string, discountID string, req *UpdateDiscountRequest) (*Discount, error) {
	db := dbutils.GetDB(ctx, r.database)
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
		args = append(args, discountID, merchantID)
		updateSQL := "UPDATE discounts SET " + strings.Join(updates, ", ") + " WHERE discount_id = ? AND merchant_id = ? AND enabled = 1"
		if _, err := db.ExecContext(ctx, updateSQL, args...); err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	// Update products if provided
	if len(req.Products) > 0 {
		// Delete existing products
		if _, err := db.ExecContext(ctx, "DELETE FROM discounts_products WHERE discount_id = ?", discountID); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		// Insert new products
		for _, p := range req.Products {
			_, err := db.ExecContext(ctx, `
				INSERT INTO discounts_products (discount_id, product_id, new_price, enabled)
				VALUES (?, ?, ?, 1)
			`, discountID, p.ProductID, p.NewPrice)
			if err != nil {
				log.Error(err.Error())
				return nil, err
			}
		}
	}

	// Update schedules if provided
	if len(req.Schedules) > 0 {
		// Delete existing schedules
		if _, err := db.ExecContext(ctx, "DELETE FROM discounts_schedules WHERE discount_id = ?", discountID); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		// Insert new schedules
		for _, s := range req.Schedules {
			_, err := db.ExecContext(ctx, `
				INSERT INTO discounts_schedules (discount_id, day_of_week, available_from, available_to, enabled)
				VALUES (?, ?, ?, ?, 1)
			`, discountID, s.DayOfWeek, s.AvailableFrom, s.AvailableTo)
			if err != nil {
				log.Error(err.Error())
				return nil, err
			}
		}
	}

	return r.GetDiscountByID(ctx, merchantID, discountID)
}

// DeleteDiscount performs a soft delete on a discount
func (r *Repository) DeleteDiscount(ctx context.Context, merchantID string, discountID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	result, err := db.ExecContext(ctx, `
		UPDATE discounts SET enabled = 0 WHERE discount_id = ? AND merchant_id = ?
	`, discountID, merchantID)
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

func (r *Repository) getDiscountProducts(ctx context.Context, discountID string) ([]DiscountProduct, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT id, discount_id, product_id, new_price, enabled
		FROM discounts_products
		WHERE discount_id = ? AND enabled = 1
	`, discountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []DiscountProduct
	for rows.Next() {
		var p DiscountProduct
		if err := rows.Scan(&p.ID, &p.DiscountID, &p.ProductID, &p.NewPrice, &p.Enabled); err != nil {
			return nil, err
		}
		products = append(products, p)
	}

	return products, rows.Err()
}

func (r *Repository) getDiscountSchedules(ctx context.Context, discountID string) ([]DiscountSchedule, error) {
	db := dbutils.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT schedule_id, discount_id, day_of_week, available_from, available_to, enabled
		FROM discounts_schedules
		WHERE discount_id = ? AND enabled = 1
	`, discountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []DiscountSchedule
	for rows.Next() {
		var s DiscountSchedule
		if err := rows.Scan(&s.ScheduleID, &s.DiscountID, &s.DayOfWeek, &s.AvailableFrom, &s.AvailableTo, &s.Enabled); err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}

	return schedules, rows.Err()
}
