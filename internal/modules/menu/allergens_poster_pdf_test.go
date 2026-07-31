package menu

import (
	"bytes"
	"fmt"
	"testing"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/pos/accounting"
)

func TestBuildAllergensPosterPDF_SinglePageForSmallCatalog(t *testing.T) {
	header := &accounting.MerchantHeader{MerchantName: "Brasserie Du Midi"}
	catalog := []models.AllergenEntry{
		{ID: "alg-gluten", Name: "Gluten", Code: "GLU"},
		{ID: "alg-lait", Name: "Lait", Code: "LAC"},
	}
	categories := []models.ProductCategory{
		{
			CategoryName: "Plats",
			Products: []models.ProductEntry{
				{
					ProductID: "p1",
					Name:      "Burger",
					Allergens: []models.AllergenEntry{{ID: "alg-gluten", Name: "Gluten", Code: "GLU"}},
				},
				{
					ProductID: "p2",
					Name:      "Salade",
					SubProducts: []models.ProductEntry{
						{ProductID: "p2-sub", Name: "Salade sans sauce", Allergens: []models.AllergenEntry{{ID: "alg-lait", Name: "Lait", Code: "LAC"}}},
					},
				},
			},
		},
	}

	pdfBytes, err := buildAllergensPosterPDF(header, catalog, categories)
	if err != nil {
		t.Fatalf("buildAllergensPosterPDF() error = %v", err)
	}

	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("buildAllergensPosterPDF() did not produce a valid PDF header")
	}

	if got := pageCount(pdfBytes); got != 1 {
		t.Fatalf("pageCount() = %d, want 1 for a small catalog/product list", got)
	}
}

func TestBuildAllergensPosterPDF_PaginatesAcrossMultiplePages(t *testing.T) {
	header := &accounting.MerchantHeader{MerchantName: "Brasserie Du Midi"}
	catalog := []models.AllergenEntry{
		{ID: "alg-gluten", Name: "Gluten", Code: "GLU"},
	}

	products := make([]models.ProductEntry, 0, 100)
	for i := 0; i < 100; i++ {
		products = append(products, models.ProductEntry{
			ProductID: fmt.Sprintf("p%d", i),
			Name:      fmt.Sprintf("Produit %d", i),
		})
	}
	categories := []models.ProductCategory{{CategoryName: "Plats", Products: products}}

	pdfBytes, err := buildAllergensPosterPDF(header, catalog, categories)
	if err != nil {
		t.Fatalf("buildAllergensPosterPDF() error = %v", err)
	}

	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("buildAllergensPosterPDF() did not produce a valid PDF header")
	}

	if got := pageCount(pdfBytes); got < 2 {
		t.Fatalf("pageCount() = %d, want >= 2 — 100 products should force at least one page break", got)
	}
}

func TestBuildAllergensPosterPDF_EmptyCatalogAndProducts(t *testing.T) {
	header := &accounting.MerchantHeader{MerchantName: "Brasserie Du Midi"}

	pdfBytes, err := buildAllergensPosterPDF(header, nil, nil)
	if err != nil {
		t.Fatalf("buildAllergensPosterPDF() error = %v", err)
	}

	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatalf("buildAllergensPosterPDF() did not produce a valid PDF header")
	}

	if got := pageCount(pdfBytes); got != 1 {
		t.Fatalf("pageCount() = %d, want 1 (still emits a page even with no data)", got)
	}
}

// pageCount compte les objets "/Type /Page" (page individuelle) dans le PDF généré, sans compter
// le "/Type /Pages" (nœud racine, pluriel). Le flux de contenu de chaque page est compressé
// (compression gofpdf par défaut, adaptée à un catalogue potentiellement long), donc on ne peut
// pas chercher directement du texte comme "Généré le" dans les octets — on vérifie la pagination
// réelle via la structure d'objets PDF, qui elle reste en clair.
func pageCount(pdfBytes []byte) int {
	return bytes.Count(pdfBytes, []byte("/Type /Page\n"))
}
