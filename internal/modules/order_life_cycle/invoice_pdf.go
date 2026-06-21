package order_life_cycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"

	"github.com/jung-kurt/gofpdf"
)

// buildInvoicePDF génère le PDF de facture à partir du Receipt déjà figé (NF525) — aucun recalcul de montant.
func buildInvoicePDF(rcpt *models.Receipt, header *accounting.MerchantHeader, orderNum string) ([]byte, error) {
	if header.VATNumber == nil {
		header.VATNumber = new(string)
		*header.VATNumber = ""
	}
	var items []models.SnapshotItem
	_ = json.Unmarshal(rcpt.ItemsSnapshot, &items)

	var payments []models.SnapshotPayment
	_ = json.Unmarshal(rcpt.PaymentsSnapshot, &payments)

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false) // garde le flux PDF en clair (facture peu volumineuse, pas de gain réel à compresser)
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(190, 8, translate(header.MerchantName), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	pdf.CellFormat(190, 6, translate("Facture "+rcpt.ReceiptNumber), "", 1, "C", false, 0, "")
	pdf.Ln(5)

	pdf.SetFont("Arial", "", 10)
	headerText := fmt.Sprintf(
		"Commande : %s\nDate : %s\nAdresse : %s\nSIRET : %s\nTVA : %s\nTéléphone : %s",
		orderNum,
		rcpt.CreatedAt.Format("02/01/2006 15:04:05"),
		header.Address,
		header.SIRET,
		*header.VATNumber,
		header.Phone,
	)
	pdf.MultiCell(190, 6, translate(headerText), "", "L", false)

	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Détail des articles"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(90, 8, translate("Article"), "1", 0, "L", false, 0, "")
	pdf.CellFormat(30, 8, translate("Quantité"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(70, 8, translate("Prix TTC"), "1", 1, "R", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	for _, item := range items {
		pdf.CellFormat(90, 8, translate(item.Name), "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, 8, fmt.Sprintf("%d", item.Quantity), "1", 0, "R", false, 0, "")
		pdf.CellFormat(70, 8, fmt.Sprintf("%.2f %s", float64(item.PriceTTC)/100, header.Currency), "1", 1, "R", false, 0, "")
	}

	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Totaux"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	pdf.CellFormat(120, 8, "Total HT", "1", 0, "L", false, 0, "")
	pdf.CellFormat(70, 8, fmt.Sprintf("%.2f %s", float64(rcpt.TotalHT)/100, header.Currency), "1", 1, "R", false, 0, "")
	pdf.CellFormat(120, 8, "Total TTC", "1", 0, "L", false, 0, "")
	pdf.CellFormat(70, 8, fmt.Sprintf("%.2f %s", float64(rcpt.TotalTTC)/100, header.Currency), "1", 1, "R", false, 0, "")

	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Encaissements"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	for _, payment := range payments {
		pdf.CellFormat(120, 8, translate(payment.MOP), "1", 0, "L", false, 0, "")
		pdf.CellFormat(70, 8, fmt.Sprintf("%.2f %s", float64(payment.Amount)/100, header.Currency), "1", 1, "R", false, 0, "")
	}

	pdf.Ln(10)
	pdf.SetFont("Arial", "I", 8)
	pdf.CellFormat(190, 6, translate(fmt.Sprintf("Document généré le %s — Référence fiscale : %s", time.Now().Format("02/01/2006 15:04"), rcpt.ReceiptNumber)), "", 1, "R", false, 0, "")

	buf := new(bytes.Buffer)
	if err := pdf.Output(buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
