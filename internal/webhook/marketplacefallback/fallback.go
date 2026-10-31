// Package marketplacefallback fournit l'article de repli d'une commande
// Uber Eats / Deliveroo dont un article ne peut pas être rattaché à un produit.
//
// Sans repli, un article dont la création de produit échoue fait échouer la
// commande entière alors que le client a déjà payé. Avec lui, la commande est
// créée ; la ligne porte l'article de repli et le vrai titre dans son commentaire.
package marketplacefallback

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

// Noms des articles de repli, un par plateforme.
const (
	UberEatsName  = "Article Uber Eats non reconnu"
	DeliverooName = "Article Deliveroo non reconnu"
)

// Product renvoie l'identifiant de l'article de repli de l'établissement pour la
// catégorie marketplace donnée (models.MarketplaceCategoryUberEats ou
// MarketplaceCategoryDeliveroo), en le créant à la première utilisation. La
// création est sérialisée : des commandes simultanées n'en créent qu'un.
func Product(ctx context.Context, db *sql.DB, merchantID, category, name string) (string, error) {
	if id, found, err := find(ctx, db, merchantID, category, name); err != nil || found {
		return id, err
	}

	var id string
	err := dbutils.RunInTxLocked(ctx, db, "marketplace-fallback:"+merchantID+":"+category, func(txCtx context.Context) error {
		existing, found, err := find(txCtx, db, merchantID, category, name)
		if err != nil {
			return err
		}
		if found {
			id = existing
			return nil
		}

		tva := models.MarketplaceTVAFor(false)
		newID, err := dbx.GetDB(txCtx, db).InsertReturningID(txCtx, `
			INSERT INTO products (merchant_id, name, product_desc, category, price, is_available_on_sno, tva_in_id, tva_delivery_id, tva_take_away_id)
			VALUES (?, ?, ?, ?, 0, FALSE, ?, ?, ?)`,
			"product_id", merchantID, name, "Article de repli", category, tva.In, tva.Delivery, tva.TakeAway)
		if err != nil {
			return fmt.Errorf("create marketplace fallback product: %w", err)
		}
		id = strconv.FormatInt(newID, 10)
		return nil
	})
	return id, err
}

func find(ctx context.Context, db *sql.DB, merchantID, category, name string) (id string, found bool, err error) {
	var productID int64
	err = dbx.GetDB(ctx, db).QueryRowContext(ctx, `
		SELECT product_id FROM products
		WHERE merchant_id = ? AND category = ? AND name = ?
		ORDER BY product_id LIMIT 1`, merchantID, category, name).Scan(&productID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return strconv.FormatInt(productID, 10), true, nil
}
