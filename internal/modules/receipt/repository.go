package receipt

import (
	"context"
	"database/sql"
	"fmt"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
)

type ReceiptRepository interface {
	GetLastReceiptData(ctx context.Context, merchantID string) (lastNumber string, lastHash string, err error)
	InsertReceipt(ctx context.Context, receipt *models.Receipt) error
	GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
	GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
}

type receiptRepository struct {
	database *sql.DB
}

func NewReceiptRepository(db *sql.DB) ReceiptRepository {
	return &receiptRepository{database: db}
}

// GetLastReceiptData prend le verrou de la chaîne receipts de l'établissement puis lit le
// dernier ticket : numérotation et chaînage restent sérialisés jusqu'à la fin
// de la transaction de l'appelant, qui doit en avoir une (l'insertion du
// ticket suivant doit s'y faire). L'ancien FOR UPDATE laissait deux
// transactions concurrentes relire le même dernier ticket : même numéro, même
// parent (lot A conformité caisse, constat C5).
func (r *receiptRepository) GetLastReceiptData(ctx context.Context, merchantID string) (string, string, error) {
	db := dbx.GetDB(ctx, r.database)

	if err := fiscal.LockChain(ctx, fiscal.ChainReceipts, merchantID); err != nil {
		return "", "", err
	}

	var lastNumber sql.NullString
	var lastHash sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT receipt_number, hash
		FROM receipts
		WHERE merchant_id = ?
		ORDER BY created_at DESC, receipt_number DESC
		LIMIT 1
	`, merchantID).Scan(&lastNumber, &lastHash)

	if err == sql.ErrNoRows {
		return "", "", nil // Premier reçu du marchand
	}
	if err != nil {
		return "", "", err
	}

	return lastNumber.String, lastHash.String, nil
}

func (r *receiptRepository) InsertReceipt(ctx context.Context, receipt *models.Receipt) error {
	db := dbx.GetDB(ctx, r.database)

	hashVersion := receipt.HashVersion
	if hashVersion == 0 {
		hashVersion = 1
	}
	query := `
		INSERT INTO receipts
		(receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature, hash_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		receipt.ReceiptID, receipt.MerchantID, receipt.OrderID, receipt.ReceiptNumber,
		receipt.TotalTTC, receipt.TotalHT, receipt.TaxDetails,
		receipt.ItemsSnapshot, receipt.PaymentsSnapshot,
		receipt.CreatedAt, receipt.PrevHash, receipt.Hash, receipt.Signature, hashVersion,
	)
	return err
}

func (r *receiptRepository) GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error) {
	return r.getLatestReceiptByOrderID(ctx, orderID, "")
}

// GetSaleReceiptByOrderID renvoie le dernier ticket de vente de la commande, en
// ignorant les avoirs (GenerateRefundReceipt les rattache à la même commande
// avec un total négatif) : c'est lui, et non l'avoir le plus récent, que la
// facture client doit reprendre.
func (r *receiptRepository) GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error) {
	return r.getLatestReceiptByOrderID(ctx, orderID, " AND total_ttc >= 0")
}

func (r *receiptRepository) getLatestReceiptByOrderID(ctx context.Context, orderID, extraWhere string) (*models.Receipt, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
		SELECT
			receipt_id, merchant_id, order_id, receipt_number,
			total_ttc, total_ht, tax_details, items_snapshot,
			payments_snapshot, created_at, prev_hash, hash, signature
		FROM receipts
		WHERE order_id = ?` + extraWhere + `
		ORDER BY created_at DESC
		LIMIT 1
	`

	var receipt models.Receipt
	err := db.QueryRowContext(ctx, query, orderID).Scan(
		&receipt.ReceiptID,
		&receipt.MerchantID,
		&receipt.OrderID,
		&receipt.ReceiptNumber,
		&receipt.TotalTTC,
		&receipt.TotalHT,
		&receipt.TaxDetails,
		&receipt.ItemsSnapshot,
		&receipt.PaymentsSnapshot,
		&receipt.CreatedAt,
		&receipt.PrevHash,
		&receipt.Hash,
		&receipt.Signature,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("aucun reçu trouvé pour la commande %s", orderID)
	} else if err != nil {
		return nil, fmt.Errorf("erreur lors de la récupération du reçu: %w", err)
	}

	return &receipt, nil
}
