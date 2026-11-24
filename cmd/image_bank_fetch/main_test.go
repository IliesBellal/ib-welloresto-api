package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const catHeader = "slug,category,name,tags,format,provider,query,brand,exclude,barcode,priority\n"

func TestParseCatalogue(t *testing.T) {
	items, errs := ParseCatalogue(strings.NewReader(catHeader +
		"coca-cola-33,boissons-soft,Coca-Cola 33 cl,coca|soda,square,openfoodfacts,33,coca-cola,zero|light,,P1\n" +
		"champignons,ingredients,Champignons,supplement,square,openverse,sliced mushrooms,,,,P1\n"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(items) != 2 || len(items[0].Exclude) != 2 || items[1].Query != "sliced mushrooms" {
		t.Fatalf("catalogue mal lu : %+v", items)
	}
	_, errs = ParseCatalogue(strings.NewReader(catHeader +
		"x,boissons-soft,X,,square,openfoodfacts,,,,,P1\n" + // ni marque ni code-barres
		"y,ingredients,Y,,square,openverse,,,,,P1\n" + // pas de recherche
		"z,ingredients,Z,,square,google,q,,,,P1\n")) // fournisseur inconnu
	if len(errs) != 3 {
		t.Fatalf("attendu 3 erreurs, obtenu %v", errs)
	}
}

func TestCatalogueFileIsValid(t *testing.T) {
	f, err := os.Open("../../docs/banque-images/catalogue.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	items, errs := ParseCatalogue(f)
	if len(errs) > 0 {
		t.Fatalf("catalogue invalide : %v", errs)
	}
	if len(items) < 300 {
		t.Fatalf("catalogue trop court : %d", len(items))
	}
}

func TestRankOpenverse(t *testing.T) {
	it := Item{Query: "sliced mushrooms", Format: "square"}
	res := rankOpenverse([]openverseResult{
		{ID: "flickr", Title: "Mushrooms", License: "cc0", Source: "flickr", Width: 1024, Height: 768, URL: "u"},
		{ID: "dessin", Title: "Mushroom illustration", License: "cc0", Source: "rawpixel", Width: 3000, Height: 3000, URL: "u"},
		{ID: "petite", Title: "Sliced mushrooms", License: "cc0", Source: "stocksnap", Width: 400, Height: 300, URL: "u"},
		{ID: "byca", Title: "Sliced mushrooms", License: "by", Source: "stocksnap", Width: 4000, Height: 3000, URL: "u"},
		{ID: "pro", Title: "Sliced mushrooms on board", License: "cc0", Source: "stocksnap", Width: 4000, Height: 3000, URL: "u"},
	}, it)
	if len(res) != 2 || res[0].ID != "pro" || res[1].ID != "flickr" {
		ids := []string{}
		for _, r := range res {
			ids = append(ids, r.ID)
		}
		t.Fatalf("classement inattendu : %v", ids)
	}
	banner := rankOpenverse([]openverseResult{
		{ID: "portrait", Title: "Burger", License: "cc0", Source: "stocksnap", Width: 2000, Height: 3000, URL: "u"},
		{ID: "large", Title: "Burger", License: "cc0", Source: "stocksnap", Width: 3000, Height: 1500, URL: "u"},
	}, Item{Query: "burger", Format: "banner"})
	if len(banner) != 1 || banner[0].ID != "large" {
		t.Fatalf("une bannière doit être large : %+v", banner)
	}
}

func TestFilterOFF(t *testing.T) {
	it := Item{Query: "orange 33", Exclude: []string{"zero"}}
	got := filterOFF([]offProduct{
		{Code: "1", ProductName: "Fanta Orange", Quantity: "33 cl", FrontURL: "u"},
		{Code: "2", ProductName: "Fanta Orange Zero", Quantity: "33 cl", FrontURL: "u"},
		{Code: "3", ProductName: "Fanta Orange", Quantity: "6 x 33 cl", FrontURL: "u"},
		{Code: "4", ProductName: "Fanta Citron", Quantity: "33 cl", FrontURL: "u"},
		{Code: "5", ProductName: "Fanta Orange", Quantity: "33 cl"},
	}, it)
	if len(got) != 1 || got[0].Code != "1" {
		t.Fatalf("filtre inattendu : %+v", got)
	}
	if u := offFullImageURL("https://images.openfoodfacts.org/images/products/544/900/000/0996/front_fr.12.400.jpg"); !strings.HasSuffix(u, "front_fr.12.full.jpg") {
		t.Fatalf("URL pleine résolution : %s", u)
	}
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 120
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFrame(t *testing.T) {
	_, ext, w, h, err := frame(jpegOf(t, 3000, 2000), "square", false)
	if err != nil || ext != ".jpg" || w != 2000 || h != 2000 {
		t.Fatalf("carré recadré : %dx%d %s %v", w, h, ext, err)
	}
	data, _, w, h, err := frame(jpegOf(t, 600, 1500), "square", true)
	if err != nil || w != h {
		t.Fatalf("produit sur fond carré : %dx%d %v", w, h, err)
	}
	img, _, _ := image.Decode(bytes.NewReader(data))
	if r, g, b, _ := img.At(2, 2).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
		t.Fatalf("le fond doit être blanc : %v", color.RGBAModel.Convert(img.At(2, 2)))
	}
	_, _, w, h, err = frame(jpegOf(t, 3000, 3000), "banner", false)
	if d := w*9 - h*16; err != nil || d > 20 || d < -20 {
		t.Fatalf("bannière en 16:9 : %dx%d %v", w, h, err)
	}
	if _, _, _, _, err := frame(jpegOf(t, 300, 300), "square", false); !errors.Is(err, errTooSmall) {
		t.Fatalf("une image trop petite doit être refusée : %v", err)
	}
}

type fakeFinder struct {
	calls []string
	t     *testing.T
}

func (f *fakeFinder) find(_ context.Context, it Item, used map[string]bool) (*Picked, error) {
	f.calls = append(f.calls, it.Slug)
	if it.Slug == "introuvable" {
		return nil, errNotFound
	}
	src := "https://exemple/" + it.Slug
	if used[src] {
		src += "-bis" // la photo rejetée n'est plus proposée : une autre est choisie
	}
	return &Picked{Data: jpegOf(f.t, 10, 10), Ext: ".jpg", Width: 10, Height: 10, ID: src,
		Source: "openverse:stocksnap", License: "CC0 1.0", Author: "a", SourceURL: src}, nil
}

func TestRun_ResumesAndWritesManifest(t *testing.T) {
	dir := t.TempDir()
	items := []Item{
		{Slug: "frites", Category: "accompagnements", Name: "Frites", Format: "square", Provider: providerOpenverse, Priority: "P1"},
		{Slug: "introuvable", Category: "plats", Name: "X", Format: "square", Provider: providerOpenverse, Priority: "P1"},
		{Slug: "tiramisu", Category: "desserts", Name: "Tiramisu", Format: "square", Provider: providerOpenverse, Priority: "P2"},
	}
	ff := &fakeFinder{t: t}
	added, missing, err := run(context.Background(), items, ff, options{out: dir, priority: map[string]bool{"P1": true}}, io.Discard)
	if err != nil || added != 1 || len(missing) != 1 {
		t.Fatalf("premier passage : added=%d missing=%v err=%v", added, missing, err)
	}

	// Second passage sans filtre : frites déjà faite, tiramisu ajouté.
	ff.calls = nil
	added, _, err = run(context.Background(), items, ff, options{out: dir}, io.Discard)
	if err != nil || added != 1 || strings.Join(ff.calls, ",") != "introuvable,tiramisu" {
		t.Fatalf("reprise : added=%d calls=%v err=%v", added, ff.calls, err)
	}

	// Image supprimée à la relecture : elle est recherchée de nouveau.
	if err := os.Remove(filepath.Join(dir, "desserts", "tiramisu.jpg")); err != nil {
		t.Fatal(err)
	}
	ff.calls = nil
	if _, _, err := run(context.Background(), items, ff, options{out: dir, only: map[string]bool{"tiramisu": true}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(filepath.Join(dir, "manifest.csv"))
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rejected, _ := os.ReadFile(filepath.Join(dir, rejectedFile))
	if strings.TrimSpace(string(rejected)) != "https://exemple/tiramisu" {
		t.Fatalf("image supprimée non mémorisée : %q", rejected)
	}
	if len(rows) != 3 || rows[0][0] != "slug" || rows[1][0] != "frites" || rows[2][0] != "tiramisu" {
		t.Fatalf("manifeste inattendu : %v", rows)
	}
	if rows[2][9] != "https://exemple/tiramisu-bis" {
		t.Fatalf("la photo rejetée a été reprise : %v", rows[2])
	}
	if rows[1][6] != "openverse:stocksnap" || rows[1][7] != "CC0 1.0" || rows[1][1] != "accompagnements/frites.jpg" {
		t.Fatalf("provenance absente : %v", rows[1])
	}
}

func TestBackgroundScore(t *testing.T) {
	packshot := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for i := range packshot.Pix {
		packshot.Pix[i] = 255
	}
	for y := 100; y < 300; y++ {
		for x := 150; x < 250; x++ {
			packshot.Set(x, y, color.RGBA{200, 0, 0, 255})
		}
	}
	scene := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for i := range scene.Pix {
		scene.Pix[i] = 110
	}
	if s := backgroundScore(packshot); s < 0.95 {
		t.Fatalf("packshot sur fond blanc : %.2f", s)
	}
	if s := backgroundScore(scene); s > 0.05 {
		t.Fatalf("photo sur fond sombre : %.2f", s)
	}
}

func TestFilterOFF_Alternatives(t *testing.T) {
	got := filterOFF([]offProduct{
		{Code: "1", ProductName: "Coca-Cola Zéro", FrontURL: "u"},
		{Code: "2", ProductName: "Coca-Cola sans sucres", FrontURL: "u"},
		{Code: "3", ProductName: "Coca-Cola", FrontURL: "u"},
	}, Item{Query: "zero/sucres"})
	if len(got) != 2 || got[0].Code != "1" || got[1].Code != "2" {
		t.Fatalf("alternatives : %+v", got)
	}
}
