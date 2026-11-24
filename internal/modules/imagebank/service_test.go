package imagebank

import (
	"context"
	"strings"
	"testing"

	"welloresto-api/internal/infrastructure/r2"
)

type fakeStore struct {
	images []Image
	mine   []MyImage
	keys   map[string]bool
}

func (f *fakeStore) ListEnabled(context.Context) ([]Image, error) { return f.images, nil }
func (f *fakeStore) IsEnabledKey(_ context.Context, k string) (bool, error) {
	return f.keys[k], nil
}
func (f *fakeStore) ListMerchantImageURLs(context.Context, string) ([]MyImage, error) {
	return f.mine, nil
}

type keys struct{}

func (keys) GetKeyFromURL(u string) string {
	u = strings.SplitN(u, "?", 2)[0]
	return strings.TrimPrefix(u, "https://storage.welloresto.fr/")
}

func svc(f *fakeStore) *Service { return &Service{repo: f, keys: keys{}} }

func TestList_FiltersAndAccentInsensitiveSearch(t *testing.T) {
	s := svc(&fakeStore{images: []Image{
		{ID: 1, Category: "desserts", Name: "Crème brûlée", Format: FormatSquare},
		{ID: 2, Category: "desserts", Name: "Tiramisu", Tags: []string{"café"}, Format: FormatSquare},
		{ID: 3, Category: "pizzas", Name: "Pizza four à bois", Format: FormatBanner},
	}})
	ctx := context.Background()

	res, _ := s.List(ctx, ListFilter{Query: "creme BRULEE"})
	if res.Total != 1 || res.Images[0].ID != 1 {
		t.Fatalf("recherche sans accents : %+v", res.Images)
	}
	res, _ = s.List(ctx, ListFilter{Query: "cafe"})
	if res.Total != 1 || res.Images[0].ID != 2 {
		t.Fatalf("recherche dans les tags : %+v", res.Images)
	}
	res, _ = s.List(ctx, ListFilter{Format: FormatBanner})
	if res.Total != 1 || res.Images[0].ID != 3 {
		t.Fatalf("filtre format : %+v", res.Images)
	}
	res, _ = s.List(ctx, ListFilter{Query: "pâtisseries"}) // libellé de catégorie
	if res.Total != 2 {
		t.Fatalf("recherche dans le libellé de catégorie : %+v", res.Images)
	}
}

func TestCategories_Counts(t *testing.T) {
	s := svc(&fakeStore{images: []Image{
		{Category: "pizzas", Format: FormatSquare}, {Category: "pizzas", Format: FormatBanner},
	}})
	cats, _ := s.Categories(context.Background())
	if len(cats) != len(Categories) {
		t.Fatalf("toutes les catégories attendues, obtenu %d", len(cats))
	}
	for _, c := range cats {
		if c.Code == "pizzas" && (c.Count != 2 || c.Formats[FormatBanner] != 1) {
			t.Fatalf("comptes pizzas : %+v", c)
		}
	}
}

func TestMine_KeepsOnlyOwnImagesDeduplicated(t *testing.T) {
	b := "https://storage.welloresto.fr/"
	own := r2.GenerateProductKey("7", "1", ".jpg")
	s := svc(&fakeStore{mine: []MyImage{
		{ImageURL: b + own + "?v=1", Usage: "product"},
		{ImageURL: b + own + "?v=2", Usage: "option"},                             // doublon
		{ImageURL: b + r2.ImageBankPrefix + "pizzas/x.jpg", Usage: "product"},     // banque
		{ImageURL: "https://exemple.fr/x.jpg", Usage: "product"},                  // externe
		{ImageURL: b + r2.GenerateProductKey("8", "1", ".jpg"), Usage: "product"}, // autre merchant
		{ImageURL: b + "wello_resto_images_storage/merchants/7/kiosk/idle_video.mp4", Usage: "kiosk_idle"},
	}})
	res, _ := s.Mine(context.Background(), "7")
	if len(res.Images) != 1 || res.Images[0].Usage != "product" {
		t.Fatalf("attendu une seule image, obtenu %+v", res.Images)
	}
}

func TestIsBankImage(t *testing.T) {
	key := r2.ImageBankPrefix + "pizzas/x.jpg"
	s := svc(&fakeStore{keys: map[string]bool{key: true}})
	if ok, _ := s.IsBankImage(context.Background(), key); !ok {
		t.Fatal("image active non reconnue")
	}
	if ok, _ := s.IsBankImage(context.Background(), "wello_resto_images_storage/merchants/7/x.jpg"); ok {
		t.Fatal("une image d'établissement n'est pas de la banque")
	}
}
