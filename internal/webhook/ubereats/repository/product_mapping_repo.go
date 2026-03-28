package repository

import (
	"context"
	"database/sql"
	"welloresto-api/internal/utils/dbutils"
)

type ProductMappingRepository struct {
	database *sql.DB
}

func NewProductMappingRepository(db *sql.DB) *ProductMappingRepository {
	return &ProductMappingRepository{database: db}
}

func (r *ProductMappingRepository) FindProductIDByUberItemID(ctx context.Context, merchantID, uberItemID string) (*string, error) {
	db := dbutils.GetDB(ctx, r.database)

	var productID string
	err := db.QueryRowContext(ctx,
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

func (r *ProductMappingRepository) CreateProductMapping(ctx context.Context, merchantID, productID, uberItemID string) error {
	db := dbutils.GetDB(ctx, r.database)

	_, err := db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_products_mapping(merchant_id, product_id, item_id)
        VALUES(?, ?, ?)`,
		merchantID, productID, uberItemID)
	return err
}
