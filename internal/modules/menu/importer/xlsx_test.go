package importer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

// buildXLSX fabrique un classeur en memoire a partir d'une grille de chaines.
// Il sert aux cas limites qu'aucun export reel ne contient (ligne malformee,
// colonne manquante) : les fixtures .xlsx restent reservees a la verification
// du format tel qu'il arrive vraiment.
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

func openFixture(t *testing.T, name string) io.ReadCloser {
	t.Helper()

	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ouverture de la fixture %q: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// Les numeros de ligne des messages d'erreur sont ceux du tableur. Ce test
// verrouille l'hypothese dont ils dependent : excelize conserve les lignes
// vides, donc l'index de la ligne dans le resultat vaut son numero moins un.
// Les exports Zelty commencent par une ligne vide, l'en-tete est en ligne 2.
func TestReadSheetRowsPreservesRowNumbering(t *testing.T) {
	rows, err := readSheetRows(openFixture(t, fixtureZelty2026))
	if err != nil {
		t.Fatalf("readSheetRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("aucune ligne lue")
	}

	if !rowIsEmpty(rows[0]) {
		t.Fatalf("ligne 1 = %v, want vide", rows[0])
	}
	if got := cellAt(rows[1], zeltyColID); got != "ID" {
		t.Fatalf("ligne 2, colonne A = %q, want %q", got, "ID")
	}
	if got := cellAt(rows[1], zeltyColType); got != "Type" {
		t.Fatalf("ligne 2, colonne B = %q, want %q", got, "Type")
	}
}

// cellAt doit tolerer les lignes courtes : excelize tronque les cellules vides
// de fin de ligne, et les sections de l'export Zelty n'utilisent pas toutes le
// meme nombre de colonnes.
func TestCellAtToleratesShortRows(t *testing.T) {
	row := []string{"ZD1", "Produit", "  Margherita  "}

	cases := []struct {
		name string
		idx  int
		want string
	}{
		{"cellule presente rognee", 2, "Margherita"},
		{"au-dela de la ligne", zeltyColTags, ""},
		{"index negatif (colonne absente)", -1, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cellAt(row, tc.idx); got != tc.want {
				t.Fatalf("cellAt(row, %d) = %q, want %q", tc.idx, got, tc.want)
			}
		})
	}
}

func TestReadSheetRowsRejectsNonWorkbook(t *testing.T) {
	if _, err := readSheetRows(bytes.NewReader([]byte("Nom;Prix\nMargherita;9,90\n"))); err == nil {
		t.Fatal("readSheetRows(csv) = nil, want une erreur")
	}
}
