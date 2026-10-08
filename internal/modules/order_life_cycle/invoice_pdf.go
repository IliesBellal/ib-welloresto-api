package order_life_cycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"

	"github.com/jung-kurt/gofpdf"
)

// invoiceCustomer est l'identité du client imprimée sur la facture. Chaque
// champ est optionnel : seules les lignes renseignées sont imprimées.
type invoiceCustomer struct {
	Name         string
	BusinessName string
	Address      string
}

func (c invoiceCustomer) lines() []string {
	var lines []string
	for _, value := range []string{c.BusinessName, c.Name, c.Address} {
		if value = strings.TrimSpace(value); value != "" {
			lines = append(lines, value)
		}
	}
	return lines
}

// invoiceVATLine est la ventilation de la TVA pour un taux (montants en centimes).
type invoiceVATLine struct {
	Rate float64 // en %, ex : 10, 5.5
	HT   int64
	TVA  int64
	TTC  int64
}

// buildInvoicePDF génère le PDF de facture à partir du Receipt déjà figé (NF525) : les totaux
// HT/TTC sont repris tels quels, et vatLines (cf. computeInvoiceVATBreakdown) se recale sur eux.
// Les mentions absentes (SIRET, n° de TVA, adresse, téléphone, client) sont omises.
func buildInvoicePDF(rcpt *models.Receipt, header *accounting.MerchantHeader, orderNum string, customer invoiceCustomer, vatLines []invoiceVATLine) ([]byte, error) {
	var items []models.SnapshotItem
	_ = json.Unmarshal(rcpt.ItemsSnapshot, &items)

	var payments []models.SnapshotPayment
	_ = json.Unmarshal(rcpt.PaymentsSnapshot, &payments)

	formatAmount := func(cents int64) string {
		return fmt.Sprintf("%.2f %s", float64(cents)/100, header.Currency)
	}

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
	pdf.MultiCell(190, 6, translate(strings.Join(invoiceHeaderLines(rcpt, header, orderNum), "\n")), "", "L", false)

	if customerLines := customer.lines(); len(customerLines) > 0 {
		pdf.Ln(5)
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 8, translate("Client"), "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 10)
		pdf.MultiCell(190, 6, translate(strings.Join(customerLines, "\n")), "", "L", false)
	}

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
		pdf.CellFormat(70, 8, formatAmount(item.PriceTTC), "1", 1, "R", false, 0, "")
	}

	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Totaux"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	totalHT := int64(rcpt.TotalHT)
	totalTTC := int64(rcpt.TotalTTC)
	pdf.CellFormat(120, 8, "Total HT", "1", 0, "L", false, 0, "")
	pdf.CellFormat(70, 8, formatAmount(totalHT), "1", 1, "R", false, 0, "")
	pdf.CellFormat(120, 8, "TVA", "1", 0, "L", false, 0, "")
	pdf.CellFormat(70, 8, formatAmount(totalTTC-totalHT), "1", 1, "R", false, 0, "")
	pdf.CellFormat(120, 8, "Total TTC", "1", 0, "L", false, 0, "")
	pdf.CellFormat(70, 8, formatAmount(totalTTC), "1", 1, "R", false, 0, "")

	if len(vatLines) > 0 {
		pdf.Ln(5)
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(190, 8, translate("Détail de la TVA"), "", 1, "L", false, 0, "")

		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(40, 8, "Taux", "1", 0, "L", false, 0, "")
		pdf.CellFormat(50, 8, "Base HT", "1", 0, "R", false, 0, "")
		pdf.CellFormat(50, 8, "TVA", "1", 0, "R", false, 0, "")
		pdf.CellFormat(50, 8, "TTC", "1", 1, "R", false, 0, "")

		pdf.SetFont("Arial", "", 10)
		for _, line := range vatLines {
			pdf.CellFormat(40, 8, formatVATRate(line.Rate), "1", 0, "L", false, 0, "")
			pdf.CellFormat(50, 8, formatAmount(line.HT), "1", 0, "R", false, 0, "")
			pdf.CellFormat(50, 8, formatAmount(line.TVA), "1", 0, "R", false, 0, "")
			pdf.CellFormat(50, 8, formatAmount(line.TTC), "1", 1, "R", false, 0, "")
		}
	}

	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Encaissements"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	for _, payment := range payments {
		pdf.CellFormat(120, 8, translate(payment.MOP), "1", 0, "L", false, 0, "")
		pdf.CellFormat(70, 8, formatAmount(int64(payment.Amount)), "1", 1, "R", false, 0, "")
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

// invoiceHeaderLines liste les mentions de l'établissement, en omettant celles
// qui ne sont pas renseignées plutôt que d'imprimer une étiquette vide.
func invoiceHeaderLines(rcpt *models.Receipt, header *accounting.MerchantHeader, orderNum string) []string {
	vatNumber := ""
	if header.VATNumber != nil {
		vatNumber = *header.VATNumber
	}

	lines := make([]string, 0, 6)
	appendIfSet := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			lines = append(lines, label+" : "+value)
		}
	}
	appendIfSet("Commande", orderNum)
	appendIfSet("Date", rcpt.CreatedAt.Format("02/01/2006 15:04:05"))
	appendIfSet("Adresse", header.Address)
	appendIfSet("Téléphone", header.Phone)
	appendIfSet("SIRET", header.SIRET)
	appendIfSet("N° TVA intracommunautaire", vatNumber)
	return lines
}

// formatVATRate affiche un taux à la française : 10 -> "10 %", 5.5 -> "5,5 %".
func formatVATRate(rate float64) string {
	return strings.Replace(strconv.FormatFloat(rate, 'f', -1, 64), ".", ",", 1) + " %"
}

// computeInvoiceVATBreakdown ventile les totaux figés du ticket (totalTTC/totalHT) par taux de
// TVA, au prorata du TTC des lignes de la commande (prix remisé + suppléments) et des frais de
// livraison. Le taux le plus représenté absorbe les écarts d'arrondi, si bien que la somme des
// lignes égale exactement les totaux du ticket.
//
// Renvoie nil — pas de détail imprimé, le total de TVA reste affiché — dès qu'un taux est
// inconnu ou que la ventilation ne recoupe pas le HT figé (commande de plateforme dont le HT
// n'a pas été calculé avec notre mapping de TVA, par exemple) : mieux vaut aucun détail
// qu'un détail faux.
func computeInvoiceVATBreakdown(order *models.Order, deliveryFeesRate *float64, totalTTC, totalHT int64) []invoiceVATLine {
	if order == nil || totalTTC <= 0 || totalHT < 0 || totalHT > totalTTC {
		return nil
	}

	orderType := ""
	if order.OrderType != nil {
		orderType = *order.OrderType
	}

	weights := make(map[float64]int64)
	for _, product := range order.Products {
		rate := invoiceProductVATRate(product, orderType)
		if rate == nil {
			return nil
		}
		unitPrice := product.Price
		if product.DiscountedPrice != nil {
			unitPrice = *product.DiscountedPrice
		}
		if product.Extra != nil {
			for _, extra := range *product.Extra {
				unitPrice += int64(math.Round(extra.Price))
			}
		}
		quantity := 0
		if product.Quantity != nil {
			quantity = *product.Quantity
		}
		if lineTTC := unitPrice * int64(quantity); lineTTC > 0 {
			weights[*rate] += lineTTC
		}
	}
	if order.DeliveryFees != nil && *order.DeliveryFees > 0 {
		if deliveryFeesRate == nil {
			return nil
		}
		weights[*deliveryFeesRate] += *order.DeliveryFees
	}

	var weightSum int64
	rates := make([]float64, 0, len(weights))
	for rate, weight := range weights {
		rates = append(rates, rate)
		weightSum += weight
	}
	if weightSum <= 0 {
		return nil
	}
	sort.Float64s(rates)

	mainRate := rates[0]
	for _, rate := range rates {
		if weights[rate] > weights[mainRate] {
			mainRate = rate
		}
	}

	lines := make([]invoiceVATLine, 0, len(rates))
	var allocatedTTC, allocatedHT int64
	for _, rate := range rates {
		if rate == mainRate {
			continue
		}
		ttc := int64(math.Round(float64(totalTTC) * float64(weights[rate]) / float64(weightSum)))
		ht := invoiceHTFromTTC(ttc, rate)
		allocatedTTC += ttc
		allocatedHT += ht
		lines = append(lines, invoiceVATLine{Rate: rate, HT: ht, TVA: ttc - ht, TTC: ttc})
	}

	mainTTC := totalTTC - allocatedTTC
	mainHT := totalHT - allocatedHT
	if mainTTC < 0 || mainHT < 0 || mainHT > mainTTC {
		return nil
	}
	// Tolérance : un arrondi au centime par ligne de commande dans le HT d'origine.
	tolerance := int64(len(order.Products)) + 1
	if diff := mainHT - invoiceHTFromTTC(mainTTC, mainRate); diff > tolerance || diff < -tolerance {
		return nil
	}
	lines = append(lines, invoiceVATLine{Rate: mainRate, HT: mainHT, TVA: mainTTC - mainHT, TTC: mainTTC})

	sort.Slice(lines, func(i, j int) bool { return lines[i].Rate < lines[j].Rate })
	return lines
}

// invoiceProductVATRate reprend le choix de taux des tickets antérieurs au
// ticket complet (ancien receipt.BuildItemsSnapshot : taux sur place par
// défaut) ; nil si le taux applicable n'est pas renseigné.
func invoiceProductVATRate(product models.ProductEntry, orderType string) *float64 {
	switch orderType {
	case models.OrderTypeDelivery:
		return product.TVADelivery
	case models.OrderTypeTakeAway:
		return product.TVATakeAway
	default:
		return product.TVAIn
	}
}

func invoiceHTFromTTC(ttc int64, rate float64) int64 {
	if rate == 0 {
		return ttc
	}
	return int64(math.Round(float64(ttc) * 100.0 / (100.0 + rate)))
}
