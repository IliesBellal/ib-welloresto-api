package importer

import (
	"bytes"
	"encoding/csv"
	"io"
	"testing"

	"github.com/xuri/excelize/v2"
)

// csvReaderFromRows encode une grille de chaines en CSV, pour les cas limites
// qu'aucune fixture reelle ne contient (colonne manquante, fichier vide).
func csvReaderFromRows(rows [][]string) io.Reader {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.WriteAll(rows)
	w.Flush()
	return &buf
}

// buildXLSX fabrique un classeur en memoire a partir d'une grille de chaines,
// pour les tests du provider wello-generic (.xlsx).
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
