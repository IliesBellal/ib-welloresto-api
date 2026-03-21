package receipt

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/security"
)

type ReceiptService interface {
	GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error
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

	// 4. Calcul du Hash et Signature
	now := time.Now().UTC()

	// Formule du chaînage (respecte bien l'ordre et les types)
	// $$ H_n = \text{SHA256}(H_{n-1} | ReceiptNumber | TotalTTC | Date) $$
	payload := fmt.Sprintf("%s|%s|%d|%s", lastHash, newNumber, order.TTC, now.Format(time.RFC3339))
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	signature := security.SignHash(newHash) // La fonction HMAC qu'on a vu précédemment

	receiptID, err := helpers.GeneratePrefixedID("receipt")
	if err != nil {
		return err
	}

	// 5. Création et Sauvegarde
	receipt := &models.Receipt{
		ReceiptID:        receiptID,
		MerchantID:       *order.MerchantID,
		OrderID:          order.OrderID,
		ReceiptNumber:    newNumber,
		TotalTTC:         order.TTC,
		TotalHT:          *order.HT,
		TaxDetails:       taxDetailsJSON,
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: paymentsJSON,
		CreatedAt:        now,
		PrevHash:         lastHash,
		Hash:             newHash,
		Signature:        signature,
	}

	return s.repo.InsertReceipt(ctx, receipt)
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
