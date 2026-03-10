package tags

import (
	"context"
	"database/sql"

	"github.com/go-sql-driver/mysql"

	"welloresto-api/internal/models"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ListTags returns all tags belonging to a merchant.
func (r *Repository) ListTags(ctx context.Context, merchantID string) ([]models.TagEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT tag_id, merchant_id, name
		 FROM tags
		 WHERE merchant_id = ?
		 ORDER BY name ASC`,
		merchantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []models.TagEntry
	for rows.Next() {
		var t models.TagEntry
		if err := rows.Scan(&t.ID, &t.MerchantID, &t.Name); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// TagBelongsToMerchant verifies a tag is owned by the given merchant.
func (r *Repository) TagBelongsToMerchant(ctx context.Context, tagID string, merchantID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&count)
	return count > 0, err
}

// CreateTag inserts a new tag for a merchant.
// Returns the created tag entry.
func (r *Repository) CreateTag(ctx context.Context, merchantID string, tagID string, name string) (*models.TagEntry, error) {
	// Insert the tag
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tags (tag_id, merchant_id, name)
		 VALUES (?, ?, ?)`,
		tagID, merchantID, name,
	)
	if err != nil {
		// Check for duplicate constraint violation
		if isUniqueConstraintError(err) {
			return nil, models.ErrInvalidInput // "Tag with this name already exists for this merchant"
		}
		return nil, err
	}

	// Return the created tag
	return &models.TagEntry{
		ID:         tagID,
		MerchantID: merchantID,
		Name:       name,
	}, nil
}

// DeleteTag removes a tag by ID (if it belongs to the merchant).
// Also cascades to product_tags due to FK constraint.
func (r *Repository) DeleteTag(ctx context.Context, merchantID string, tagID string) error {
	// Verify ownership first
	var count int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return models.ErrForbidden
	}

	// Delete the tag (FK cascade will remove product_tags entries)
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM tags WHERE tag_id = ? AND merchant_id = ?`,
		tagID, merchantID,
	)
	if err != nil {
		return err
	}

	// Verify something was actually deleted
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return models.ErrNotFound
	}

	return nil
}

// Helper to detect unique constraint violations (MySQL-specific).
func isUniqueConstraintError(err error) bool {
	// MySQL specific error code for unique constraint violation is 1062
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		return mysqlErr.Number == 1062
	}
	return false
}
