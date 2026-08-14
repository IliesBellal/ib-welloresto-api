package importutil

import (
	"bytes"
	"io"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildXLSX fabrique un classeur en mémoire à partir d'une grille de chaînes.
func buildXLSX(t *testing.T, rows [][]string) io.Reader {
	t.Helper()

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)

	for r, row := range rows {
		for c, value := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("CoordinatesToCellName(%d, %d): %v", c+1, r+1, err)
			}
			if err := f.SetCellStr(sheet, cell, value); err != nil {
				t.Fatalf("SetCellStr(%s): %v", cell, err)
			}
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return &buf
}

func TestReadSheetRowsMinimal(t *testing.T) {
	rows, err := ReadSheetRows(buildXLSX(t, [][]string{
		{"Nom", "Prix"},
		{"Margherita", "9,90"},
	}))
	if err != nil {
		t.Fatalf("ReadSheetRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ReadSheetRows = %d lignes, want 2", len(rows))
	}
	if rows[0][0] != "Nom" || rows[1][0] != "Margherita" {
		t.Fatalf("ReadSheetRows = %v", rows)
	}
}

// cellAt doit tolérer les lignes courtes : excelize tronque les cellules
// vides de fin de ligne, et toutes les sections d'un fichier n'utilisent pas
// forcément le même nombre de colonnes.
func TestCellAtToleratesShortRows(t *testing.T) {
	row := []string{"ZD1", "Produit", "  Margherita  "}

	cases := []struct {
		name string
		idx  int
		want string
	}{
		{"cellule presente rognee", 2, "Margherita"},
		{"au-dela de la ligne", 7, ""},
		{"index negatif (colonne absente)", -1, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CellAt(row, tc.idx); got != tc.want {
				t.Fatalf("CellAt(row, %d) = %q, want %q", tc.idx, got, tc.want)
			}
		})
	}
}

func TestRowIsEmpty(t *testing.T) {
	cases := []struct {
		name string
		row  []string
		want bool
	}{
		{"ligne vide", []string{}, true},
		{"cellules blanches", []string{"", "  ", "\t"}, true},
		{"une cellule renseignee", []string{"", "x", ""}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RowIsEmpty(tc.row); got != tc.want {
				t.Fatalf("RowIsEmpty(%v) = %v, want %v", tc.row, got, tc.want)
			}
		})
	}
}

func TestReadSheetRowsRejectsNonWorkbook(t *testing.T) {
	if _, err := ReadSheetRows(bytes.NewReader([]byte("Nom;Prix\nMargherita;9,90\n"))); err == nil {
		t.Fatal("ReadSheetRows(csv) = nil, want une erreur")
	}
}

func TestReadSheetRowsRejectsEmptyWorkbook(t *testing.T) {
	if _, err := ReadSheetRows(bytes.NewReader(nil)); err == nil {
		t.Fatal("ReadSheetRows(vide) = nil, want une erreur")
	}
}
