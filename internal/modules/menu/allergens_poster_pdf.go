package menu

import (
	"bytes"
	"fmt"
	"strings"
	"time"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"

	"github.com/jung-kurt/gofpdf"
)

const (
	posterProductColWidth = 55.0 // mm — largeur de la colonne "Produit"
	posterRowHeight       = 6.0  // mm — hauteur d'une ligne produit
	posterHeaderRowHeight = 7.0  // mm — hauteur de la ligne d'en-têtes de colonnes
	posterBottomMargin    = 18.0 // mm — laisse la place au pied de page sur 2 lignes
)

// buildAllergensPosterPDF génère l'affiche des allergènes : un tableau produits x allergènes
// (une colonne par allergène du référentiel, une croix si le produit le contient).
//
// Contrairement à buildInvoicePDF et buildPDFReport (pos/accounting/service.go) qui tiennent sur
// une seule page, cette affiche peut lister un catalogue entier : la pagination est réelle,
// pilotée par SetHeaderFunc/SetAutoPageBreak, avec l'en-tête établissement et les en-têtes de
// colonnes réimprimés sur chaque page.
//
// Orientation paysage ("L") plutôt que portrait comme les deux autres générateurs : avec une
// colonne par allergène du référentiel, le nombre de colonnes ne tient pas dans les 190mm de
// largeur utile d'un A4 portrait.
func buildAllergensPosterPDF(header *accounting.MerchantHeader, catalog []models.AllergenEntry, categories []models.ProductCategory) ([]byte, error) {
	pdf := gofpdf.New("L", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AliasNbPages("")
	pdf.SetAutoPageBreak(true, posterBottomMargin)

	leftMargin, _, rightMargin, _ := pdf.GetMargins()
	pageWidth, pageHeight := pdf.GetPageSize()
	contentWidth := pageWidth - leftMargin - rightMargin
	pageBreakTrigger := pageHeight - posterBottomMargin

	allergenColWidth := 0.0
	if n := len(catalog); n > 0 {
		allergenColWidth = (contentWidth - posterProductColWidth) / float64(n)
	}

	// legendLine reconstruit "CODE = Nom" pour chaque allergène : les colonnes du tableau
	// n'affichent que le code (largeur de colonne trop étroite pour le nom complet).
	legendParts := make([]string, 0, len(catalog))
	for _, a := range catalog {
		legendParts = append(legendParts, fmt.Sprintf("%s = %s", a.Code, a.Name))
	}
	legendLine := strings.Join(legendParts, "   |   ")

	renderHeader := func() {
		pdf.SetFont("Arial", "B", 14)
		pdf.CellFormat(contentWidth, 8, translate(header.MerchantName), "", 1, "C", false, 0, "")

		pdf.SetFont("Arial", "", 11)
		pdf.CellFormat(contentWidth, 6, translate("Tableau des allergènes"), "", 1, "C", false, 0, "")
		pdf.Ln(2)

		pdf.SetFont("Arial", "", 7)
		pdf.MultiCell(contentWidth, 3.5, translate(legendLine), "", "L", false)
		pdf.Ln(2)

		pdf.SetFont("Arial", "B", 8)
		pdf.SetFillColor(230, 230, 230)
		pdf.CellFormat(posterProductColWidth, posterHeaderRowHeight, translate("Produit"), "1", 0, "L", true, 0, "")
		for _, a := range catalog {
			pdf.CellFormat(allergenColWidth, posterHeaderRowHeight, translate(a.Code), "1", 0, "C", true, 0, "")
		}
		pdf.Ln(posterHeaderRowHeight)
	}
	pdf.SetHeaderFunc(renderHeader)

	renderFooter := func() {
		pdf.SetY(-posterBottomMargin + 3)
		pdf.SetFont("Arial", "I", 7)
		pdf.MultiCell(contentWidth, 3.5, translate(
			"Informations allergènes fournies à titre indicatif (règlement UE n°1169/2011) — se référer au personnel pour toute question.",
		), "", "C", false)

		pdf.SetFont("Arial", "I", 7)
		pdf.CellFormat(contentWidth, 4, translate(fmt.Sprintf(
			"Généré le %s — Page %d/{nb}", time.Now().Format("02/01/2006 15:04"), pdf.PageNo(),
		)), "", 0, "C", false, 0, "")
	}
	pdf.SetFooterFunc(renderFooter)

	pdf.AddPage()

	drawRow := func(p models.ProductEntry) {
		if pdf.GetY()+posterRowHeight > pageBreakTrigger {
			pdf.AddPage()
		}
		// Repris à chaque ligne : renderHeader() laisse la police en gras après avoir dessiné
		// les en-têtes de colonnes, aussi bien à l'ouverture du document qu'à chaque saut de page.
		pdf.SetFont("Arial", "", 8)

		allergenSet := make(map[string]bool, len(p.Allergens))
		for _, a := range p.Allergens {
			allergenSet[a.ID] = true
		}

		pdf.CellFormat(posterProductColWidth, posterRowHeight, translate(p.Name), "1", 0, "L", false, 0, "")
		for _, a := range catalog {
			mark := ""
			if allergenSet[a.ID] {
				mark = "X"
			}
			pdf.CellFormat(allergenColWidth, posterRowHeight, mark, "1", 0, "C", false, 0, "")
		}
		pdf.Ln(posterRowHeight)
	}

	for _, cat := range categories {
		for _, p := range cat.Products {
			drawRow(p)
			for _, sp := range p.SubProducts {
				drawRow(sp)
			}
		}
	}

	buf := new(bytes.Buffer)
	if err := pdf.Output(buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
