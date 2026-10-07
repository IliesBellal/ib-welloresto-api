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
	GetOrderTaxLines(ctx context.Context, orderID string) ([]fiscal.TaxLine, int64, error)
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

// GetOrderTaxLines renvoie les parts TTC de la commande par taux et le total
// de ses remises de caisse actives, pour ventiler la TVA de son ticket (C9) :
// mêmes règles que l'export comptable (pos/accounting GetOrderVATLines) —
// chaque ligne (prix + suppléments) × quantité au taux figé sur la ligne
// (migration 164, à défaut celui de la catégorie du produit), les frais de
// livraison non nuls à leur propre taux.
func (r *receiptRepository) GetOrderTaxLines(ctx context.Context, orderID string) ([]fiscal.TaxLine, int64, error) {
	db := dbx.GetDB(ctx, r.database)

	// Une seule requête (chemin de chaque clôture de vente) : parts « L » par
	// taux, puis une ligne « D » portant le total des remises de caisse.
	rows, err := db.QueryContext(ctx, `
		SELECT 'L' AS kind, `+models.OrderItemTVARateSQL("oi", "tva")+` AS rate,
		       ((oi.price + COALESCE((SELECT SUM(ex.price) FROM extra ex WHERE ex.order_item_id = oi.order_item_id), 0)) * oi.quantity) AS ttc
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
		INNER JOIN tva_categories tva ON tva.tva_id = `+models.OrderItemTVAIDSQL("oi", "o", "p")+`
		WHERE oi.order_id = ?
		UNION ALL
		SELECT 'L', `+models.DeliveryFeesTVARateSQL("o_fees", "tva_fees")+`, o_fees.delivery_fees
		FROM orders o_fees
		INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
		WHERE o_fees.order_id = ? AND o_fees.delivery_fees <> 0
		UNION ALL
		SELECT 'D', 0, COALESCE(SUM(amount), 0) FROM payments
		WHERE order_id = ? AND enabled = TRUE AND upper(mop) IN `+models.DiscountMOPsSQL, orderID, orderID, orderID)
	if err != nil {
		return nil, 0, fmt.Errorf("load order tax lines: %w", err)
	}
	defer rows.Close()
	var lines []fiscal.TaxLine
	var discount int64
	for rows.Next() {
		var kind string
		var l fiscal.TaxLine
		if err := rows.Scan(&kind, &l.Rate, &l.TTC); err != nil {
			return nil, 0, fmt.Errorf("scan order tax line: %w", err)
		}
		if kind == "D" {
			discount = l.TTC
			continue
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("load order tax lines: %w", err)
	}
	return lines, discount, nil
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
