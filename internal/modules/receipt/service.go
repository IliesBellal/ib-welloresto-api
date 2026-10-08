package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
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
	CancelSaleReceipt(ctx context.Context, merchantID, orderID string) error
	GetReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
	GetSaleReceiptByOrderID(ctx context.Context, orderID string) (*models.Receipt, error)
}

type receiptService struct {
	repo ReceiptRepository
}

func NewReceiptService(repo ReceiptRepository) ReceiptService {
	return &receiptService{repo: repo}
}

// GenerateFiscalReceipt émet le ticket de vente d'une commande à sa clôture.
// Reclôture d'une commande rouverte (lot C conformité caisse, R2 et R3) : le
// contenu de la vente (lignes, quantités, prix, taux, TVA ventilée, totaux
// TTC et HT — pas les paiements) est comparé au dernier ticket de vente
// encore en vigueur. Identique : aucun ticket. Différent : avoir de ce
// ticket (pour ce qu'il en reste après avoirs), puis nouveau ticket. Une
// vente déjà entièrement annulée par avoirs ne compte plus : nouveau ticket.
func (s *receiptService) GenerateFiscalReceipt(ctx context.Context, order *models.Order, items []models.SnapshotItem, payments []models.SnapshotPayment) error {
	// 1. Verrou de la chaîne, dernier ticket de l'établissement et vente en
	// vigueur de la commande (une requête).
	head, err := s.repo.GetReceiptChainHead(ctx, *order.MerchantID, order.OrderID)
	if err != nil {
		return fmt.Errorf("failed to get last receipt data: %w", err)
	}

	// 2. Contenu du ticket, dont la ventilation de TVA par taux, nette des
	// remises de caisse (C9, lot B conformité caisse)
	itemsJSON, _ := json.Marshal(items)
	paymentsJSON, _ := json.Marshal(payments)
	taxLines, discount, err := s.repo.GetOrderTaxLines(ctx, order.OrderID)
	if err != nil {
		return fmt.Errorf("failed to compute receipt tax details: %w", err)
	}
	taxDetailsJSON, err := json.Marshal(fiscal.BuildTaxDetails(taxLines, discount))
	if err != nil {
		return fmt.Errorf("failed to encode receipt tax details: %w", err)
	}
	receipt := &models.Receipt{
		ReceiptID:        helpers.GeneratePrefixedID(helpers.ReceiptIDPrefix),
		MerchantID:       *order.MerchantID,
		OrderID:          order.OrderID,
		TotalTTC:         int(order.TTC),
		TotalHT:          int(*order.HT),
		TaxDetails:       taxDetailsJSON,
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: paymentsJSON,
	}

	// 3. Reclôture (R2)
	if head.Sale != nil && head.Remaining > 0 {
		same, err := sameSale(head.Sale, receipt)
		if err != nil {
			return err
		}
		if same {
			return nil
		}
		refund, err := s.buildRefundReceipt(ctx, head.Sale, -int(head.Remaining), "")
		if err != nil {
			return err
		}
		if err := s.insertChained(ctx, head, refund); err != nil {
			return err
		}
	}

	// 4. Ticket de vente
	return s.insertChained(ctx, head, receipt)
}

// CancelSaleReceipt émet l'avoir de la vente encore en vigueur d'une
// commande qu'on annule ou qu'on refuse après l'avoir rouverte (lot C
// conformité caisse, R2) : pour ce qu'il reste du dernier ticket de vente
// après avoirs. Rien s'il n'y a pas de vente en vigueur.
func (s *receiptService) CancelSaleReceipt(ctx context.Context, merchantID, orderID string) error {
	head, err := s.repo.GetReceiptChainHead(ctx, merchantID, orderID)
	if err != nil {
		return fmt.Errorf("failed to get last receipt data: %w", err)
	}
	if head.Sale == nil || head.Remaining <= 0 {
		return nil
	}
	refund, err := s.buildRefundReceipt(ctx, head.Sale, -int(head.Remaining), "")
	if err != nil {
		return err
	}
	return s.insertChained(ctx, head, refund)
}

// insertChained numérote, date, scelle et insère un ticket à la suite de la
// tête de chaîne, puis avance la tête (plusieurs tickets dans une même
// transaction : avoir puis nouvelle vente).
func (s *receiptService) insertChained(ctx context.Context, head *ChainHead, receipt *models.Receipt) error {
	receipt.ReceiptNumber = s.generateNextReceiptNumber(head.LastNumber)
	receipt.CreatedAt = fiscal.Now()
	receipt.PrevHash = head.LastHash
	if err := sealReceipt(receipt); err != nil {
		return err
	}
	if err := s.repo.InsertReceipt(ctx, receipt); err != nil {
		return err
	}
	head.LastNumber, head.LastHash = receipt.ReceiptNumber, receipt.Hash
	return nil
}

// sameSale compare le contenu de vente d'un ticket à celui du dernier ticket
// de vente : totaux TTC et HT, ventilation de TVA et lignes (nom, quantité,
// prix, taux, TVA), sous forme canonique et sans tenir compte de l'ordre des
// lignes. Les paiements n'en font pas partie (R3).
func sameSale(prev, next *models.Receipt) (bool, error) {
	if prev.TotalTTC != next.TotalTTC || prev.TotalHT != next.TotalHT {
		return false, nil
	}
	prevTax, err := fiscal.Canonical(prev.TaxDetails)
	if err != nil {
		return false, err
	}
	nextTax, err := fiscal.Canonical(next.TaxDetails)
	if err != nil {
		return false, err
	}
	if string(prevTax) != string(nextTax) {
		return false, nil
	}
	prevItems, err := canonicalLines(prev.ItemsSnapshot)
	if err != nil {
		return false, err
	}
	nextItems, err := canonicalLines(next.ItemsSnapshot)
	if err != nil {
		return false, err
	}
	if len(prevItems) != len(nextItems) {
		return false, nil
	}
	for i := range prevItems {
		if prevItems[i] != nextItems[i] {
			return false, nil
		}
	}
	return true, nil
}

// canonicalLines renvoie les lignes d'un items_snapshot sous forme canonique,
// triées. Vide ou null : aucune ligne.
func canonicalLines(raw []byte) ([]string, error) {
	var lines []json.RawMessage
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil, fmt.Errorf("receipt items snapshot: %w", err)
		}
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		c, err := fiscal.Canonical(l)
		if err != nil {
			return nil, err
		}
		out = append(out, string(c))
	}
	sort.Strings(out)
	return out, nil
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
	originalReceipt.MerchantID, originalReceipt.OrderID = merchantID, orderID
	receipt, err := s.buildRefundReceipt(ctx, originalReceipt, refundAmountNegative, mop)
	if err != nil {
		return err
	}
	// 2. Scellement (le chaînage est respecté, même avec un montant négatif) et insertion
	return s.insertChained(ctx, &ChainHead{LastNumber: lastNumber, LastHash: lastHash}, receipt)
}

// buildRefundReceipt prépare un avoir du ticket originalReceipt (même
// établissement, même commande), sans numéro ni scellement. mop vide : avoir
// de correction (réouverture, lot C), sans mouvement d'argent, donc sans
// paiement dans son détail.
func (s *receiptService) buildRefundReceipt(ctx context.Context, originalReceipt *models.Receipt, refundAmountNegative int, mop string) (*models.Receipt, error) {
	// Ventilation de TVA de l'avoir (C9, lot B conformité caisse) : au
	// prorata des taux du ticket d'origine ; pour un ticket antérieur au lot B
	// (sans ventilation), des taux de la commande. Une ligne d'avoir par taux.
	origTax, ok := fiscal.ParseTaxDetails(originalReceipt.TaxDetails)
	if !ok {
		taxLines, discount, err := s.repo.GetOrderTaxLines(ctx, originalReceipt.OrderID)
		if err != nil {
			return nil, fmt.Errorf("failed to compute refund tax details: %w", err)
		}
		origTax = fiscal.BuildTaxDetails(taxLines, discount)
	}
	refundTax := fiscal.ProrateTaxDetails(origTax, int64(refundAmountNegative))
	totalHT := refundAmountNegative
	itemsSnap := []models.SnapshotItem{}
	for _, l := range refundTax.Lines {
		itemsSnap = append(itemsSnap, models.SnapshotItem{
			Name:      fmt.Sprintf("Avoir sur facture %s", originalReceipt.ReceiptNumber),
			Quantity:  1,
			PriceTTC:  l.TTC, // Négatif
			TaxRate:   int64(math.Round(l.Rate * 100)),
			TaxAmount: l.TVA,
		})
	}
	if len(refundTax.Lines) > 0 {
		totalHT = int(refundTax.TotalHT())
	} else {
		// Aucun taux connu (commande sans ligne) : ligne unique, comme avant.
		itemsSnap = append(itemsSnap, models.SnapshotItem{
			Name:     fmt.Sprintf("Avoir sur facture %s", originalReceipt.ReceiptNumber),
			Quantity: 1,
			PriceTTC: int64(refundAmountNegative),
		})
	}
	itemsJSON, _ := json.Marshal(itemsSnap)
	taxDetailsJSON, err := json.Marshal(refundTax)
	if err != nil {
		return nil, fmt.Errorf("failed to encode refund tax details: %w", err)
	}

	paySnap := []models.SnapshotPayment{}
	if mop != "" {
		paySnap = append(paySnap, models.SnapshotPayment{Amount: refundAmountNegative, MOP: mop})
	}
	payJSON, _ := json.Marshal(paySnap)

	return &models.Receipt{
		ReceiptID:        helpers.GeneratePrefixedID(helpers.ReceiptIDPrefix),
		MerchantID:       originalReceipt.MerchantID,
		OrderID:          originalReceipt.OrderID, // On le lie à la même commande !
		TotalTTC:         refundAmountNegative,
		TotalHT:          totalHT,
		TaxDetails:       taxDetailsJSON,
		ItemsSnapshot:    itemsJSON,
		PaymentsSnapshot: payJSON,
	}, nil
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
