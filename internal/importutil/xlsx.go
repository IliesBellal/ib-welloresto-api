package importutil

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

var (
	// ErrEmptyWorkbook signale un classeur sans feuille exploitable.
	ErrEmptyWorkbook = errors.New("classeur sans feuille de calcul")

	// ErrInvalidWorkbook signale un flux qui n'est pas un classeur .xlsx —
	// typiquement un CSV ou un .xls renommé. C'est une erreur de l'appelant,
	// pas du service : la couche HTTP doit la rendre en 400, d'où le sentinel.
	ErrInvalidWorkbook = errors.New("fichier illisible : un classeur .xlsx est attendu")
)

// ReadSheetRows lit la première feuille d'un classeur et rend ses lignes.
//
// La première feuille plutôt qu'un nom en dur : rien ne garantit qu'un
// export tiers nomme toujours sa feuille de la même façon d'une version à
// l'autre, et aucun des formats visés n'est multi-feuilles.
//
// excelize tronque les cellules vides de fin de ligne : une ligne peut donc
// être plus courte que l'en-tête, et un index de colonne n'est jamais sûr
// d'exister. Toute lecture passe par CellAt.
func ReadSheetRows(r io.Reader) ([][]string, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", ErrInvalidWorkbook, err)
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

// CellAt lit une cellule par index de colonne, en tolérant les lignes
// courtes. La valeur est rognée : les exports comportent des espaces de
// bordure sur les libellés comme sur les nombres.
func CellAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// RowIsEmpty repère les lignes de séparation entre sections.
func RowIsEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
