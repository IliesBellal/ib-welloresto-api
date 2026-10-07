package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
)

type ReceiptService interface {
	GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error
	GenerateRefundReceipt(ctx context.Context, merchantID string, orderID string, originalReceipt *models.Receipt, refundAmountNegative int, mop string) error
	GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
	GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
}

type receiptService struct {
	repo ReceiptRepository
}

func NewReceiptService(repo ReceiptRepository) ReceiptService {
	return &receiptService{repo: repo}
}

func (s *receiptService) GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error {
	// 1. Récupérer le dernier état (avec Lock via le txCtx)
	lastNumber, lastHash, err := s.repo.GetLastReceiptData(ctx, *order.MerchantID)
	if err != nil {
		return fmt.Errorf("failed to get last receipt data: %w", err)
	}

	// 2. Générer le nouveau numéro séquentiel
	newNumber := s.generateNextReceiptNumber(lastNumber)

	// 3. Préparer les JSON
	itemsJSON, _ := json.Marshal(items)
	paymentsJSON, _ := json.Marshal(payments)
	taxDetailsJSON := []byte("{}") // À remplacer par ta logique de ventilation TVA si nécessaire

	// 4. Création, scellement et sauvegarde
	receipt := &models.Receipt{
		ReceiptID:        helpers.GeneratePrefixedID(helpers.ReceiptIDPrefix),
		MerchantID:       *order.MerchantID,
		OrderID:          order.OrderID,
		ReceiptNumber:    newNumber,
		TotalTTC:         int(order.TTC),
		TotalHT:          int(*order.HT),
		TaxDetails:       taxDetailsJSON,
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: paymentsJSON,
		CreatedAt:        fiscal.Now(),
		PrevHash:         lastHash,
	}
	if err := sealReceipt(receipt); err != nil {
		return err
	}

	return s.repo.InsertReceipt(ctx, receipt)
}

// sealReceipt scelle un ticket ou un avoir (lot A conformité caisse) :
// empreinte v2 de l'en-tête et du détail (articles, paiements, TVA), chaînée
// sur le ticket précédent de l'établissement. L'ancienne formule ne couvrait
// que le numéro, le TTC et la date.
func sealReceipt(receipt *models.Receipt) error {
	receipt.PrevHash = fiscal.PrevOrGenesis(receipt.PrevHash)
	payload, err := fiscal.NewReceiptPayload(receipt.MerchantID, receipt.ReceiptNumber, receipt.OrderID, receipt.CreatedAt,
		receipt.TotalTTC, receipt.TotalHT, receipt.TaxDetails, receipt.ItemsSnapshot, receipt.PaymentsSnapshot)
	if err != nil {
		return err
	}
	receipt.Hash, receipt.Signature, err = fiscal.Seal(fiscal.ChainReceipts, receipt.PrevHash, payload)
	if err != nil {
		return err
	}
	receipt.HashVersion = fiscal.HashVersion
	return nil
}

// generateNextReceiptNumber transforme "F-2026-000045" en "F-2026-000046"
func (s *receiptService) generateNextReceiptNumber(lastNumber string) string {
	currentYear := time.Now().UTC().Format("2006")
	prefix := "F-" + currentYear + "-"

	// Si c'est le 1er reçu ou qu'on a changé d'année
	if lastNumber == "" || !strings.HasPrefix(lastNumber, prefix) {
		return prefix + "000001" // On passe à 6 digits ici
	}

	parts := strings.Split(lastNumber, "-")
	if len(parts) == 3 {
		seq, err := strconv.Atoi(parts[2])
		if err == nil {
			return fmt.Sprintf("%s%06d", prefix, seq+1) // %06d force les 6 chiffres
		}
	}

	return prefix + "ERROR"
}

func (s *receiptService) GenerateRefundReceipt(ctx context.Context, merchantID string, orderID string, originalReceipt *models.Receipt, refundAmountNegative int, mop string) error {

	// 1. On lock et on récupère le dernier chaînage
	lastNumber, lastHash, err := s.repo.GetLastReceiptData(ctx, merchantID)
	if err != nil {
		return err
	}

	newNumber := s.generateNextReceiptNumber(lastNumber)
	newTechID := helpers.GeneratePrefixedID(helpers.ReceiptIDPrefix)

	// 2. Snapshot : on met juste une ligne explicite pour l'avoir (puisqu'on omet les items précis pour l'instant)
	itemsSnap := []models.SnapshotItem{
		{
			Name:      fmt.Sprintf("Avoir sur facture %s", originalReceipt.ReceiptNumber),
			Quantity:  1,
			PriceTTC:  int64(refundAmountNegative), // Négatif
			TaxRate:   0,                           // Pour un remboursement générique sans gestion d'items, on lisse souvent à 0, ou on doit recalculer le prorata exact.
			TaxAmount: 0,
		},
	}
	itemsJSON, _ := json.Marshal(itemsSnap)

	paySnap := []models.SnapshotPayment{
		{Amount: refundAmountNegative, MOP: mop},
	}
	payJSON, _ := json.Marshal(paySnap)

	// 3. Scellement (le chaînage est respecté, même avec un montant négatif)
	receipt := &models.Receipt{
		ReceiptID:        newTechID,
		MerchantID:       merchantID,
		OrderID:          orderID, // On le lie à la même commande !
		ReceiptNumber:    newNumber,
		TotalTTC:         refundAmountNegative,
		TotalHT:          refundAmountNegative, // Simplifié pour cet exemple
		TaxDetails:       []byte("{}"),
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: payJSON,
		CreatedAt:        fiscal.Now(),
		PrevHash:         lastHash,
	}
	if err := sealReceipt(receipt); err != nil {
		return err
	}

	// 4. Insertion
	return s.repo.InsertReceipt(ctx, receipt)
}

func (s *receiptService) GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error) {
	// Logique métier optionnelle : vérifier si le merchantID correspondrait ici
	// si on le passait en paramètre.

	receipt, err := s.repo.GetReceiptByOrderID(ctx, orderID)
	if err != nil {
		// On wrap l'erreur du repo avec un contexte "Service"
		return nil, fmt.Errorf("receiptService.GetReceiptByOrderID: %w", err)
	}

	return receipt, nil
}

func (s *receiptService) GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error) {
	receipt, err := s.repo.GetSaleReceiptByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("receiptService.GetSaleReceiptByOrderID: %w", err)
	}

	return receipt, nil
}
