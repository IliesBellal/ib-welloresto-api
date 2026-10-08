package receipt

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/models"
)

type ReceiptRepository interface {
	GetLastReceiptData(ctx context.Context, merchantID string) (lastNumber string, lastHash string, err error)
	InsertReceipt(ctx context.Context, receipt *models.Receipt) error
	GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
	GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
	GetOrderSaleLines(ctx context.Context, orderID string) ([]fiscal.SaleLine, int64, error)
	GetReceiptChainHead(ctx context.Context, merchantID, orderID string) (*ChainHead, error)
	ListOrderReceipts(ctx context.Context, merchantID, orderID string) (state string, list []storedReceipt, found bool, err error)
}

// ChainHead est la tête de la chaîne des tickets d'un établissement, lue sous
// son verrou, et l'état de vente d'une commande.
type ChainHead struct {
	LastNumber string
	LastHash   string
	// Sale : dernier ticket de vente de la commande (TTC >= 0), nil s'il n'y
	// en a pas. Remaining : son TTC moins les avoirs émis depuis (> 0 : vente
	// toujours en vigueur).
	Sale      *models.Receipt
	Remaining int64
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

// GetReceiptChainHead prend le verrou de la chaîne receipts de
// l'établissement puis lit, en une requête, le dernier ticket de
// l'établissement et le dernier ticket de vente de la commande avec ce qu'il
// en reste après avoirs (reclôture d'une commande rouverte, lot C conformité
// caisse, R2). Même contrat de transaction que GetLastReceiptData.
func (r *receiptRepository) GetReceiptChainHead(ctx context.Context, merchantID, orderID string) (*ChainHead, error) {
	db := dbx.GetDB(ctx, r.database)
	if err := fiscal.LockChain(ctx, fiscal.ChainReceipts, merchantID); err != nil {
		return nil, err
	}
	var lastNumber, lastHash, saleID, saleNumber sql.NullString
	var saleTTC, saleHT, remaining sql.NullInt64
	var saleTax, saleItems []byte
	var saleAt sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT h.receipt_number, h.hash,
		       s.receipt_id, s.receipt_number, s.total_ttc, s.total_ht, s.tax_details, s.items_snapshot, s.created_at,
		       s.total_ttc + COALESCE((
		           SELECT SUM(a.total_ttc) FROM receipts a
		           WHERE a.order_id = s.order_id AND a.total_ttc < 0 AND a.created_at > s.created_at), 0)
		FROM (SELECT 1) one
		LEFT JOIN LATERAL (
			SELECT receipt_number, hash FROM receipts
			WHERE merchant_id = ?
			ORDER BY created_at DESC, receipt_number DESC LIMIT 1) h ON TRUE
		LEFT JOIN LATERAL (
			SELECT receipt_id, order_id, receipt_number, total_ttc, total_ht, tax_details, items_snapshot, created_at
			FROM receipts
			WHERE order_id = ? AND total_ttc >= 0
			ORDER BY created_at DESC LIMIT 1) s ON TRUE`, merchantID, orderID).
		Scan(&lastNumber, &lastHash, &saleID, &saleNumber, &saleTTC, &saleHT, &saleTax, &saleItems, &saleAt, &remaining)
	if err != nil {
		return nil, err
	}
	head := &ChainHead{LastNumber: lastNumber.String, LastHash: lastHash.String}
	if saleID.Valid {
		head.Sale = &models.Receipt{
			ReceiptID: saleID.String, MerchantID: merchantID, OrderID: orderID, ReceiptNumber: saleNumber.String,
			TotalTTC: int(saleTTC.Int64), TotalHT: int(saleHT.Int64), TaxDetails: saleTax, ItemsSnapshot: saleItems,
			CreatedAt: saleAt.Time,
		}
		head.Remaining = remaining.Int64
	}
	return head, nil
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

// GetOrderSaleLines renvoie le détail de la vente d'une commande et le total
// de ses remises de caisse actives, d'où se déduisent les lignes figées de son
// ticket et sa TVA ventilée (lot B C9, lot D ticket complet). Mêmes règles que
// l'export comptable (pos/accounting GetOrderVATLines), options payantes en
// plus :
//   - chaque article : prix × quantité, au taux figé sur la ligne (migration
//     164, à défaut celui de la catégorie du produit) ;
//   - ses options payantes : surcoût figé (migration 173, à défaut celui du
//     catalogue) × quantité de l'option × quantité de l'article, au taux de
//     l'article. Pas pour Uber Eats et Deliveroo : on ne sait pas encore si
//     le prix unitaire de la plateforme inclut ses modificateurs (le compter
//     deux fois serait pire que l'omettre), voir le brief du lot D ;
//   - ses suppléments : prix × quantité de l'article, au taux de l'article ;
//   - les frais de livraison non nuls, à leur propre taux.
//
// Une seule requête (chemin de chaque clôture de vente) ; les parts sont
// triées par article (article, options, suppléments), puis la livraison.
func (r *receiptRepository) GetOrderSaleLines(ctx context.Context, orderID string) ([]fiscal.SaleLine, int64, error) {
	db := dbx.GetDB(ctx, r.database)

	itemFrom := `
		FROM orderitems oi
		INNER JOIN orders o ON o.order_id = oi.order_id
		INNER JOIN products p ON p.product_id = oi.product_id
		INNER JOIN tva_categories tva ON tva.tva_id = ` + models.OrderItemTVAIDSQL("oi", "o", "p")
	itemRate := models.OrderItemTVARateSQL("oi", "tva")
	rows, err := db.QueryContext(ctx, `
		SELECT 'A' AS kind, oi.order_item_id AS sort_item, 1 AS sort_kind, 0::bigint AS sort_sub,
		       COALESCE(p.name, '') AS label, oi.quantity::bigint AS qty, oi.price::bigint AS unit, `+itemRate+` AS rate
		`+itemFrom+`
		WHERE oi.order_id = ?
		UNION ALL
		SELECT 'O', oi.order_item_id, 2, oic.id::bigint,
		       COALESCE(cao.title, ''), (oic.quantity * oi.quantity)::bigint,
		       COALESCE(oic.extra_price, cao.extra_price, 0)::bigint, `+itemRate+`
		`+itemFrom+`
		INNER JOIN order_item_configuration oic ON oic.order_item_id = oi.order_item_id
		LEFT JOIN configurable_attribute_options cao ON cao.id = oic.configuration_attribute_option_id
		WHERE oi.order_id = ? AND oic.quantity > 0 AND COALESCE(oic.extra_price, cao.extra_price, 0) <> 0
		  AND COALESCE(o.brand, '') NOT IN ('`+models.BrandUberEats+`', '`+models.BrandDeliveroo+`')
		UNION ALL
		SELECT 'E', oi.order_item_id, 3, ex.id::bigint,
		       COALESCE(ce.name, ''), oi.quantity::bigint, ex.price::bigint, `+itemRate+`
		`+itemFrom+`
		INNER JOIN extra ex ON ex.order_item_id = oi.order_item_id
		LEFT JOIN components ce ON ce.component_id = ex.component_id AND ce.merchant_id = o.merchant_id
		WHERE oi.order_id = ?
		UNION ALL
		SELECT 'F', NULL, 4, 0, 'Frais de livraison', 1, o_fees.delivery_fees::bigint, `+models.DeliveryFeesTVARateSQL("o_fees", "tva_fees")+`
		FROM orders o_fees
		INNER JOIN tva_categories tva_fees ON tva_fees.tva_id = -1
		WHERE o_fees.order_id = ? AND o_fees.delivery_fees <> 0
		UNION ALL
		SELECT 'D', NULL, 5, 0, '', 1, COALESCE(SUM(amount), 0)::bigint, 0 FROM payments
		WHERE order_id = ? AND enabled = TRUE AND upper(mop) IN `+models.DiscountMOPsSQL+`
		ORDER BY sort_item NULLS LAST, sort_kind, sort_sub`, orderID, orderID, orderID, orderID, orderID)
	if err != nil {
		return nil, 0, fmt.Errorf("load order sale lines: %w", err)
	}
	defer rows.Close()
	var lines []fiscal.SaleLine
	var discount int64
	for rows.Next() {
		var kind, label string
		var item sql.NullInt64
		var sortKind, sortSub, qty, unit int64
		var rate float64
		if err := rows.Scan(&kind, &item, &sortKind, &sortSub, &label, &qty, &unit, &rate); err != nil {
			return nil, 0, fmt.Errorf("scan order sale line: %w", err)
		}
		l := fiscal.SaleLine{Label: label, Quantity: qty, UnitTTC: unit, Rate: rate}
		if item.Valid {
			l.Item = strconv.FormatInt(item.Int64, 10)
		}
		switch kind {
		case "A":
			l.Kind = models.SnapshotKindArticle
		case "O":
			l.Kind = models.SnapshotKindOption
		case "E":
			l.Kind = models.SnapshotKindSupplement
		case "F":
			l.Kind = models.SnapshotKindDelivery
		case "D":
			discount = unit
			continue
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("load order sale lines: %w", err)
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
