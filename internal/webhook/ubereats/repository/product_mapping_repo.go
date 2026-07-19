package repository

import (
	"context"
	"database/sql"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/database/dbx"
)

type ProductMappingRepository struct {
	database *sql.DB
}

func NewProductMappingRepository(db *sql.DB) *ProductMappingRepository {
	return &ProductMappingRepository{database: db}
}

func (r *ProductMappingRepository) FindProductIDByUberItemID(ctx context.Context, merchantID, uberItemID string) (*string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

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
		log.Error("Error fetching product ID by Uber item ID: " + err.Error())
		return nil, err
	}
	return &productID, nil
}

func (r *ProductMappingRepository) CreateProductMapping(ctx context.Context, merchantID, productID, uberItemID string) error {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	_, err := db.ExecContext(ctx,
		`INSERT INTO integration_uber_eats_products_mapping(merchant_id, product_id, item_id)
        VALUES(?, ?, ?)`,
		merchantID, productID, uberItemID)

	if err != nil {
		log.Error("Error creating product mapping: " + err.Error())
	}

	return err
}
