package tags

import (
	"context"
	"database/sql"
	"strings"

	"github.com/go-sql-driver/mysql"

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

// ListTags returns all tags belonging to a merchant.
func (r *Repository) ListTags(ctx context.Context, merchantID string) ([]models.TagEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	rows, err := db.QueryContext(ctx,
		`SELECT tag_id, merchant_id, name, COALESCE(display_order, 0) as display_order, color
		 FROM tags
		 WHERE merchant_id = ?
		 ORDER BY display_order ASC, name ASC`,
		merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	defer rows.Close()

	var result []models.TagEntry
	for rows.Next() {
		var t models.TagEntry
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.Name, &t.DisplayOrder, &t.Color); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// TagBelongsToMerchant verifies a tag is owned by the given merchant.
func (r *Repository) TagBelongsToMerchant(ctx context.Context, tagID string, merchantID string) (bool, error) {
	db := dbutils.GetDB(ctx, r.database)

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&count)
	return count > 0, err
}

// CreateTag inserts a new tag for a merchant.
// Returns the created tag entry.
func (r *Repository) CreateTag(ctx context.Context, merchantID string, req *CreateTagRequest) (*models.TagEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Get the next display order (count of existing tags)
	var displayOrder int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tags WHERE merchant_id = ?`,
		merchantID,
	).Scan(&displayOrder)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	// Insert the tag
	_, err = db.ExecContext(ctx,
		`INSERT INTO tags (tag_id, merchant_id, name, display_order, color)
		 VALUES (?, ?, ?, ?, ?)`,
		*req.ID, merchantID, req.Name, displayOrder, req.Color,
	)
	if err != nil {
		log.Error(err.Error())
		// Check for duplicate constraint violation
		if isUniqueConstraintError(err) {
			return nil, models.ErrInvalidInput // "Tag with this name already exists for this merchant"
		}
		return nil, err
	}

	// Return the created tag
	return &models.TagEntry{
		ID:           *req.ID,
		MerchantID:   merchantID,
		Name:         req.Name,
		DisplayOrder: displayOrder,
		Color:        *req.Color,
	}, nil
}

// DeleteTag removes a tag by ID (if it belongs to the merchant).
// Also cascades to product_tags due to FK constraint.
func (r *Repository) DeleteTag(ctx context.Context, merchantID string, tagID string) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Verify ownership first
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&count); err != nil {
		log.Error(err.Error())
		return err
	}
	if count == 0 {
		return models.ErrForbidden
	}

	// Delete the tag (FK cascade will remove product_tags entries)
	result, err := db.ExecContext(ctx,
		`DELETE FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	// Verify something was actually deleted
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Error(err.Error())
		return err
	}
	if rowsAffected == 0 {
		return models.ErrNotFound
	}

	return nil
}

// UpdateTagsDisplayOrder updates the display order of tags for a merchant.
func (r *Repository) UpdateTagsDisplayOrder(ctx context.Context, merchantID string, tags []TagDisplayOrderItem) error {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// Update display_order for each tag
	for displayOrder, tag := range tags {
		// Verify tag belongs to merchant
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
			tag.ID, merchantID,
		).Scan(&count); err != nil {
			log.Error(err.Error())
			return err
		}
		if count == 0 {
			return models.ErrForbidden
		}

		// Update display_order
		_, err := db.ExecContext(ctx,
			`UPDATE tags SET display_order = ? WHERE tag_id = ? AND merchant_id = ?`,
			displayOrder, tag.ID, merchantID,
		)
		if err != nil {
			log.Error(err.Error())
			return err
		}
	}

	return nil
}

// UpdateTag updates a tag's properties (name, color, display_order).
// Only updates the fields that are provided (non-nil).
func (r *Repository) UpdateTag(ctx context.Context, merchantID string, tagID string, req *UpdateTagRequest) (*models.TagEntry, error) {
	db := dbutils.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	// 1. Verify tag belongs to merchant and exists
	var tagExists int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&tagExists); err != nil {
		log.Error(err.Error())
		return nil, err
	}
	if tagExists == 0 {
		return nil, models.ErrForbidden
	}

	// 2. Build dynamic UPDATE query
	var updates []string
	var args []interface{}

	if req.Name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Color != nil {
		updates = append(updates, "color = ?")
		args = append(args, *req.Color)
	}
	if req.DisplayOrder != nil {
		updates = append(updates, "display_order = ?")
		args = append(args, *req.DisplayOrder)
	}

	// If nothing to update, return current tag
	if len(updates) == 0 {
		var t models.TagEntry
		err := db.QueryRowContext(ctx,
			`SELECT tag_id, merchant_id, name, COALESCE(display_order, 0) as display_order, COALESCE(color, '') as color
			 FROM tags
			 WHERE tag_id = ? AND merchant_id = ?`,
			tagID, merchantID,
		).Scan(&t.ID, &t.MerchantID, &t.Name, &t.DisplayOrder, &t.Color)
		if err != nil {
			log.Error(err.Error())
			return nil, err
		}
		return &t, nil
	}

	// 3. Execute update
	args = append(args, tagID, merchantID)
	updateSQL := `UPDATE tags SET ` + strings.Join(updates, ", ") + ` WHERE tag_id = ? AND merchant_id = ?`

	if _, err := db.ExecContext(ctx, updateSQL, args...); err != nil {
		log.Error(err.Error())
		// Check for duplicate constraint violation
		if isUniqueConstraintError(err) {
			return nil, models.ErrInvalidInput
		}
		return nil, err
	}

	// 4. Fetch and return updated tag
	var t models.TagEntry
	err := db.QueryRowContext(ctx,
		`SELECT tag_id, merchant_id, name, COALESCE(display_order, 0) as display_order, COALESCE(color, '') as color
		 FROM tags
		 WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&t.ID, &t.MerchantID, &t.Name, &t.DisplayOrder, &t.Color)
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}

	return &t, nil
}

// Helper to detect unique constraint violations (MySQL-specific).
func isUniqueConstraintError(err error) bool {
	// MySQL specific error code for unique constraint violation is 1062
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		return mysqlErr.Number == 1062
	}
	return false
}
