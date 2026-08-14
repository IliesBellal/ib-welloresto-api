package importutil

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
)

// utf8BOM est le marqueur d'ordre des octets UTF-8 que certains tableurs
// (Excel en tête) préfixent aux fichiers CSV qu'ils exportent.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ReadCSVRows lit un flux CSV et rend ses lignes, avec le même contrat que
// ReadSheetRows ([][]string) pour que la logique d'en-tête (FoldHeader,
// alias) soit commune xlsx/CSV en aval.
//
// Le séparateur est la virgule. FieldsPerRecord est laissé à -1 pour tolérer
// les lignes de longueur variable (sections, lignes de séparation), et
// LazyQuotes est activé : les exports CSV rencontrés ne respectent pas
// toujours strictement la RFC 4180 sur l'échappement des guillemets.
func ReadCSVRows(r io.Reader) ([][]string, error) {
	br := bufio.NewReader(r)
	if bom, err := br.Peek(len(utf8BOM)); err == nil && bytes.Equal(bom, utf8BOM) {
		_, _ = br.Discard(len(utf8BOM))
	}

	reader := csv.NewReader(br)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.Comma = ','

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("lecture du fichier CSV: %w", err)
	}
	return rows, nil
}
