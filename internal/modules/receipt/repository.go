package receipt

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type ReceiptRepository interface {
	GetLastReceiptData(ctx context.Context, merchantID string) (lastNumber string, lastHash string, err error)
	InsertReceipt(ctx context.Context, receipt *models.Receipt) error
}

type receiptRepository struct {
	db *sql.DB
}

func NewReceiptRepository(db *sql.DB) ReceiptRepository {
	return &receiptRepository{db: db}
}

// GetLastReceiptData verrouille la lecture pour éviter les doublons de numérotation
func (r *receiptRepository) GetLastReceiptData(ctx context.Context, merchantID string) (string, string, error) {
	db := dbutils.GetDB(ctx, r.db)

	var lastNumber sql.NullString
	var lastHash sql.NullString

	// Le FOR UPDATE est capital ici : si 2 commandes sont payées à la même milliseconde,
	// la base de données mettra la 2ème en attente pour garantir la séquence.
	err := db.QueryRowContext(ctx, `
		SELECT receipt_number, hash 
		FROM receipts 
		WHERE merchant_id = ? 
		ORDER BY created_at DESC, receipt_number DESC 
		LIMIT 1 
		FOR UPDATE
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
	db := dbutils.GetDB(ctx, r.db)

	query := `
		INSERT INTO receipts 
		(receipt_id, merchant_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, payments_snapshot, created_at, prev_hash, hash, signature)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		receipt.ReceiptID, receipt.MerchantID, receipt.OrderID, receipt.ReceiptNumber,
		receipt.TotalTTC, receipt.TotalHT, receipt.TaxDetails,
		receipt.ItemsSnapshot, receipt.PaymentsSnapshot,
		receipt.CreatedAt, receipt.PrevHash, receipt.Hash, receipt.Signature,
	)
	return err
}
