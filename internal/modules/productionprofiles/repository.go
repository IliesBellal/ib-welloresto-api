package productionprofiles

import (
	"context"
	"database/sql"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// profileScanner abstracts *sql.Row and *sql.Rows so a single scan helper covers both.
type profileScanner interface {
	Scan(dest ...interface{}) error
}

const selectProfileCols = `
	SELECT production_profile_id, merchant_id, name, split_by_source, display_only_paid_orders, created_at, updated_at
	FROM production_profiles`

func scanProfile(s profileScanner) (ProductionProfileEntry, error) {
	var p ProductionProfileEntry
	err := s.Scan(
		&p.ID, &p.MerchantID, &p.Name,
		&p.SplitBySource, &p.DisplayOnlyPaidOrders,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

// ListProfiles returns every production profile for a merchant, without the
// product association matrix (sans détail produits).
func (r *Repository) ListProfiles(ctx context.Context, merchantID string) ([]ProductionProfileEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx,
		selectProfileCols+` WHERE merchant_id = ? ORDER BY name ASC`,
		merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	result := make([]ProductionProfileEntry, 0)
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// getProfileEntry fetches a single profile owned by the merchant, without products.
func (r *Repository) getProfileEntry(ctx context.Context, merchantID, profileID string) (*ProductionProfileEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	row := db.QueryRowContext(ctx,
		selectProfileCols+` WHERE production_profile_id = ? AND merchant_id = ?`,
		profileID, merchantID,
	)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		return nil, models.ErrNotFound
	}
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return &p, nil
}

// GetProfile returns a single profile with its sparse product association
// matrix — only products carrying at least one true flag are included, same
// convention as the allergens/tags associations in internal/modules/menu.
func (r *Repository) GetProfile(ctx context.Context, merchantID, profileID string) (*ProductionProfileDetail, error) {
	entry, err := r.getProfileEntry(ctx, merchantID, profileID)
	if err != nil {
		return nil, err
	}

	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx,
		`SELECT product_id, should_produce, should_monitor
		 FROM product_production_profiles
		 WHERE production_profile_id = ? AND (should_produce = TRUE OR should_monitor = TRUE)
		 ORDER BY product_id ASC`,
		profileID,
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	products := make([]ProductProductionProfile, 0)
	for rows.Next() {
		var pp ProductProductionProfile
		if err := rows.Scan(&pp.ProductID, &pp.ShouldProduce, &pp.ShouldMonitor); err != nil {
			log.Error(err.Error())
			return nil, err
		}
		products = append(products, pp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ProductionProfileDetail{
		ID:                    entry.ID,
		MerchantID:            entry.MerchantID,
		Name:                  entry.Name,
		SplitBySource:         entry.SplitBySource,
		DisplayOnlyPaidOrders: entry.DisplayOnlyPaidOrders,
		CreatedAt:             entry.CreatedAt,
		UpdatedAt:             entry.UpdatedAt,
		Products:              products,
	}, nil
}

// CreateProfile inserts a new production profile and returns the created
// entry. SplitBySource/DisplayOnlyPaidOrders fall back to their column
// defaults (true/false) when omitted from the request.
func (r *Repository) CreateProfile(ctx context.Context, merchantID, profileID string, req *CreateProductionProfileRequest) (*ProductionProfileEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	splitBySource := true
	if req.SplitBySource != nil {
		splitBySource = *req.SplitBySource
	}
	displayOnlyPaidOrders := false
	if req.DisplayOnlyPaidOrders != nil {
		displayOnlyPaidOrders = *req.DisplayOnlyPaidOrders
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO production_profiles (production_profile_id, merchant_id, name, split_by_source, display_only_paid_orders)
		 VALUES (?, ?, ?, ?, ?)`,
		profileID, merchantID, req.Name, splitBySource, displayOnlyPaidOrders,
	)
	if err != nil {
		log.Error(err.Error())
		if isUniqueConstraintError(err) {
			return nil, models.ErrInvalidInput
		}
		return nil, err
	}

	return r.getProfileEntry(ctx, merchantID, profileID)
}

// UpdateProfile applies a partial update (name and/or display settings) to a
// profile owned by the merchant.
func (r *Repository) UpdateProfile(ctx context.Context, merchantID, profileID string, req *UpdateProductionProfileRequest) (*ProductionProfileEntry, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Verify ownership before mutating.
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM production_profiles WHERE production_profile_id = ? AND merchant_id = ?`,
		profileID, merchantID,
	).Scan(&count); err != nil {
		log.Error(err.Error())
		return nil, err
	}
	if count == 0 {
		return nil, models.ErrForbidden
	}

	var updates []string
	var args []interface{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
	}
	if req.SplitBySource != nil {
		updates = append(updates, "split_by_source = ?")
		args = append(args, *req.SplitBySource)
	}
	if req.DisplayOnlyPaidOrders != nil {
		updates = append(updates, "display_only_paid_orders = ?")
		args = append(args, *req.DisplayOnlyPaidOrders)
	}

	if len(updates) > 0 {
		args = append(args, profileID, merchantID)
		query := `UPDATE production_profiles SET ` + strings.Join(updates, ", ") + ` WHERE production_profile_id = ? AND merchant_id = ?`
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			log.Error(err.Error())
			return nil, err
		}
	}

	return r.getProfileEntry(ctx, merchantID, profileID)
}

// DeleteProfile removes a profile and its product associations. No FK
// cascade exists (convention du projet), so both deletes run explicitly
// inside a transaction.
func (r *Repository) DeleteProfile(ctx context.Context, merchantID, profileID string) error {
	log := logger.FromContext(ctx)

	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		// Verify ownership before mutating.
		var count int
		if err := db.QueryRowContext(txCtx,
			`SELECT COUNT(1) FROM production_profiles WHERE production_profile_id = ? AND merchant_id = ?`,
			profileID, merchantID,
		).Scan(&count); err != nil {
			log.Error(err.Error())
			return err
		}
		if count == 0 {
			return models.ErrNotFound
		}

		if _, err := db.ExecContext(txCtx,
			`DELETE FROM product_production_profiles WHERE production_profile_id = ?`,
			profileID,
		); err != nil {
			log.Error(err.Error())
			return err
		}

		if _, err := db.ExecContext(txCtx,
			`DELETE FROM production_profiles WHERE production_profile_id = ? AND merchant_id = ?`,
			profileID, merchantID,
		); err != nil {
			log.Error(err.Error())
			return err
		}

		return nil
	})
}

// ReplaceProducts fully replaces a profile's product association matrix:
// delete everything, then insert only rows carrying at least one true flag
// (a product with both flags false is simply absent). Same full-replace
// semantics as MenuRepository.SyncProductAllergens.
func (r *Repository) ReplaceProducts(ctx context.Context, merchantID, profileID string, items ReplaceProductsRequest) error {
	log := logger.FromContext(ctx)

	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)

		// Ownership check: the profile itself.
		var profileCount int
		if err := db.QueryRowContext(txCtx,
			`SELECT COUNT(1) FROM production_profiles WHERE production_profile_id = ? AND merchant_id = ?`,
			profileID, merchantID,
		).Scan(&profileCount); err != nil {
			log.Error(err.Error())
			return err
		}
		if profileCount == 0 {
			return models.ErrForbidden
		}

		// Ownership check: every referenced product must belong to the merchant
		// (items has no duplicate product_id — enforced by ReplaceProductsRequest.Validate).
		if len(items) > 0 {
			placeholders := make([]string, len(items))
			args := make([]interface{}, 0, len(items)+1)
			args = append(args, merchantID)
			for i, item := range items {
				placeholders[i] = "?"
				args = append(args, item.ProductID)
			}
			var owned int
			if err := db.QueryRowContext(txCtx,
				`SELECT COUNT(1) FROM products WHERE merchant_id = ? AND product_id IN (`+strings.Join(placeholders, ",")+`)`,
				args...,
			).Scan(&owned); err != nil {
				log.Error(err.Error())
				return err
			}
			if owned != len(items) {
				return models.ErrForbidden
			}
		}

		if _, err := db.ExecContext(txCtx,
			`DELETE FROM product_production_profiles WHERE production_profile_id = ?`,
			profileID,
		); err != nil {
			log.Error(err.Error())
			return err
		}

		for _, item := range items {
			if !item.ShouldProduce && !item.ShouldMonitor {
				continue
			}
			if _, err := db.ExecContext(txCtx,
				`INSERT INTO product_production_profiles (production_profile_id, product_id, should_produce, should_monitor) VALUES (?, ?, ?, ?)`,
				profileID, item.ProductID, item.ShouldProduce, item.ShouldMonitor,
			); err != nil {
				log.Error(err.Error())
				return err
			}
		}

		return nil
	})
}

func isUniqueConstraintError(err error) bool {
	return dbx.IsDuplicateEntry(err)
}
