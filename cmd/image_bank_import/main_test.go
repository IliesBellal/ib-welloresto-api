package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"welloresto-api/internal/infrastructure/imageopt"
	"welloresto-api/internal/modules/imagebank"
)

const header = "slug,file,category,name,tags,format,source,license,author,source_url\n"

func TestParseManifest_Valid(t *testing.T) {
	csv := header + `canette-cola,cola.png,boissons-soft,"Canette de cola 33 cl",soda|canette|cola,square,fournisseur,Accord écrit 2026-11,Marque X,
`
	entries, errs := ParseManifest(strings.NewReader(csv))
	if len(errs) > 0 {
		t.Fatalf("erreurs inattendues : %v", errs)
	}
	if len(entries) != 1 || len(entries[0].Tags) != 3 || !entries[0].Enabled {
		t.Fatalf("entrée inattendue : %+v", entries)
	}
}

func TestParseManifest_Errors(t *testing.T) {
	csv := header +
		"Mauvais Slug,a.jpg,burgers,Burger,,square,openverse,CC0,,\n" +
		"ok,a.jpg,inconnue,Burger,,square,openverse,CC0,,\n" +
		"ok,a.jpg,burgers,Burger,,portrait,openverse,CC0,,\n" +
		"autre,a.jpg,burgers,Burger,,square,,,,\n"
	_, errs := ParseManifest(strings.NewReader(csv))
	// slug, catégorie, doublon "ok", format, provenance
	if len(errs) != 5 {
		t.Fatalf("attendu 5 erreurs, obtenu %d : %v", len(errs), errs)
	}
}

func TestParseManifest_MissingColumn(t *testing.T) {
	_, errs := ParseManifest(strings.NewReader("slug,file\nx,y.jpg\n"))
	if len(errs) == 0 {
		t.Fatal("colonnes manquantes non signalées")
	}
}

type memDB struct {
	rows map[string]imagebank.ImportRow
}

func (m *memDB) FindBySlug(_ context.Context, slug string) (*imagebank.ExistingImport, error) {
	r, ok := m.rows[slug]
	if !ok {
		return nil, nil
	}
	return &imagebank.ExistingImport{R2Key: r.R2Key, ContentHash: r.ContentHash}, nil
}
func (m *memDB) Upsert(_ context.Context, row imagebank.ImportRow) error {
	m.rows[row.Slug] = row
	return nil
}

type memR2 struct{ uploads int }

func (m *memR2) UploadFile(_ context.Context, key string, f io.Reader, _ string) (string, error) {
	_, _ = io.ReadAll(f)
	m.uploads++
	return "https://storage.welloresto.fr/" + key, nil
}
func (m *memR2) PublicURL(key string) string { return "https://storage.welloresto.fr/" + key }

func TestImportAll_IdempotentAndCompressed(t *testing.T) {
	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	rng := rand.New(rand.NewSource(3))
	for i := range img.Pix {
		if i%4 == 3 {
			img.Pix[i] = 255
		} else {
			img.Pix[i] = uint8(rng.Intn(256))
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	if err := os.WriteFile(filepath.Join(dir, "a.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := []Entry{{Slug: "burger-classique", File: "a.png", Category: "burgers", Name: "Burger", Format: "square", Source: "ia", License: "x", Enabled: true}}
	db := &memDB{rows: map[string]imagebank.ImportRow{}}
	up := &memR2{}

	st := importAll(context.Background(), entries, dir, db, up, imageopt.Options{MaxSide: 600}, true, io.Discard)
	if st.created != 1 || up.uploads != 1 || st.failed != 0 {
		t.Fatalf("premier import : %+v, uploads=%d", st, up.uploads)
	}
	row := db.rows["burger-classique"]
	if row.ContentType != "image/jpeg" || row.Width != 600 || !strings.HasSuffix(row.R2Key, "image_bank/burgers/burger-classique.jpg") {
		t.Fatalf("ligne inattendue : %+v", row)
	}
	if st.after >= st.before {
		t.Fatalf("pas de gain : %d → %d", st.before, st.after)
	}

	entries[0].Name = "Burger classique"
	st = importAll(context.Background(), entries, dir, db, up, imageopt.Options{MaxSide: 600}, true, io.Discard)
	if st.unchanged != 1 || up.uploads != 1 || db.rows["burger-classique"].Name != "Burger classique" {
		t.Fatalf("second import : %+v, uploads=%d", st, up.uploads)
	}
}
