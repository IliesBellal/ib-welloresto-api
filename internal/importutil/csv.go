package importutil

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
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
//
// Certains exports (Zelty en tête) sortent en Windows-1252 plutôt qu'en
// UTF-8 : encoding/csv ne valide pas l'encodage et laisse passer les octets
// invalides tels quels, qui atterrissent ensuite dans une colonne texte
// Postgres — lequel les rejette (SQLSTATE 22021 invalid byte sequence for
// encoding "UTF8") au moment du commit, bien après la preview. On détecte ce
// cas ici, en amont de tout parsing, et on retranscode en UTF-8.
func ReadCSVRows(r io.Reader) ([][]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("lecture du fichier CSV: %w", err)
	}
	data = bytes.TrimPrefix(data, utf8BOM)

	if !utf8.Valid(data) {
		if decoded, decErr := charmap.Windows1252.NewDecoder().Bytes(data); decErr == nil {
			data = decoded
		}
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.Comma = ','

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("lecture du fichier CSV: %w", err)
	}
	return rows, nil
}
