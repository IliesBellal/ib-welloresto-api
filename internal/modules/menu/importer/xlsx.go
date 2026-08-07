package importer

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ErrEmptyWorkbook signale un classeur sans feuille exploitable.
var ErrEmptyWorkbook = errors.New("classeur sans feuille de calcul")

// readSheetRows lit la premiere feuille d'un classeur et rend ses lignes.
//
// La premiere feuille plutot qu'un nom en dur : les deux exports Zelty connus
// nomment la leur "Sheet1", mais rien ne le garantit d'un export a l'autre et
// aucun des formats vises n'est multi-feuilles.
//
// excelize tronque les cellules vides de fin de ligne : une ligne peut donc
// etre plus courte que l'en-tete, et un index de colonne n'est jamais sur
// d'exister. Toute lecture passe par cellAt.
func readSheetRows(r io.Reader) ([][]string, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("ouverture du classeur: %w", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, ErrEmptyWorkbook
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("lecture de la feuille %q: %w", sheets[0], err)
	}
	return rows, nil
}

// cellAt lit une cellule par index de colonne, en tolerant les lignes courtes.
// La valeur est rognee : les exports comportent des espaces de bordure sur les
// libelles comme sur les nombres.
func cellAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// rowIsEmpty repere les lignes de separation entre sections.
func rowIsEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
