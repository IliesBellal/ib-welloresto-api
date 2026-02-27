package repository

import (
	"context"
	"database/sql"
)

type ProductMappingRepository struct {
	db *sql.DB
}

func NewProductMappingRepository(db *sql.DB) *ProductMappingRepository {
	return &ProductMappingRepository{db: db}
}

func (r *ProductMappingRepository) FindProductIDByUberItemID(ctx context.Context, tx *sql.Tx, merchantID, uberItemID string) (*string, error) {
	var productID string
	err := tx.QueryRowContext(ctx,
		`SELECT product_id 
        FROM integration_uber_eats_products_mapping 
        WHERE merchant_id = ? AND item_id = ?`,
		merchantID, uberItemID).Scan(&productID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &productID, nil
}

func (r *ProductMappingRepository) CreateProductMapping(ctx context.Context, tx *sql.Tx, merchantID, productID, uberItemID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_products_mapping(merchant_id, product_id, item_id)
        VALUES(?, ?, ?)`,
		merchantID, productID, uberItemID)
	return err
}
