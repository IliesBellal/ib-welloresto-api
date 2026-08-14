package importutil

import (
	"strings"
	"testing"
)

func TestReadCSVRows(t *testing.T) {
	input := "Nom,Email,Adresse\nJean Dupont,jean@example.com,\"12 rue de la Paix, 75002 Paris\"\n"

	rows, err := ReadCSVRows(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadCSVRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ReadCSVRows = %d lignes, want 2: %v", len(rows), rows)
	}
	if got, want := rows[0], []string{"Nom", "Email", "Adresse"}; !equalRows(got, want) {
		t.Fatalf("en-tete = %v, want %v", got, want)
	}
	if got, want := rows[1][2], "12 rue de la Paix, 75002 Paris"; got != want {
		t.Fatalf("champ entre guillemets = %q, want %q", got, want)
	}
}

func TestReadCSVRowsStripsUTF8BOM(t *testing.T) {
	withBOM := "\xEF\xBB\xBFNom,Email\nJean,jean@example.com\n"

	rows, err := ReadCSVRows(strings.NewReader(withBOM))
	if err != nil {
		t.Fatalf("ReadCSVRows: %v", err)
	}
	if got, want := rows[0][0], "Nom"; got != want {
		t.Fatalf("premiere cellule = %q, want %q (BOM non retire)", got, want)
	}
}

func TestReadCSVRowsWithoutBOM(t *testing.T) {
	rows, err := ReadCSVRows(strings.NewReader("Nom,Email\nJean,jean@example.com\n"))
	if err != nil {
		t.Fatalf("ReadCSVRows: %v", err)
	}
	if got, want := rows[0][0], "Nom"; got != want {
		t.Fatalf("premiere cellule = %q, want %q", got, want)
	}
}

// FieldsPerRecord = -1 : les lignes de longueur variable (sections, lignes de
// separation) ne doivent pas faire echouer la lecture.
func TestReadCSVRowsToleratesVariableLengthRows(t *testing.T) {
	input := "Nom,Email,Telephone\nJean,jean@example.com,0600000000\nMarie\n,,\n"

	rows, err := ReadCSVRows(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadCSVRows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("ReadCSVRows = %d lignes, want 4: %v", len(rows), rows)
	}
	if len(rows[2]) != 1 {
		t.Fatalf("ligne courte = %v, want 1 champ", rows[2])
	}
}

func TestReadCSVRowsEmptyInput(t *testing.T) {
	rows, err := ReadCSVRows(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ReadCSVRows(vide): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ReadCSVRows(vide) = %v, want aucune ligne", rows)
	}
}

// LazyQuotes est active : un guillemet mal echappe ne doit pas faire
// echouer la lecture, les exports rencontres ne respectant pas toujours
// strictement la RFC 4180.
func TestReadCSVRowsLazyQuotes(t *testing.T) {
	input := "Nom,Notes\nJean,Livrer avant 12h \"pile\"\n"

	rows, err := ReadCSVRows(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ReadCSVRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ReadCSVRows = %d lignes, want 2: %v", len(rows), rows)
	}
}

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
