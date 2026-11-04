package deliveroo

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"reflect"
	"strings"
	"testing"

	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/marketplacecatalog"
)

func ptr[T any](v T) *T { return &v }

func testCatalog(t *testing.T) *marketplacecatalog.Catalog {
	t.Helper()
	p := func(id, name string, price int64) models.ProductEntry {
		return models.ProductEntry{ProductID: id, Name: name, Price: price, Status: "available",
			SyncDeliveroo: ptr(true), TVADelivery: ptr(10.0)}
	}
	pizza := p("20", "Pizza 4 fromages", 1200)
	pizza.ImageURL = ptr("https://img/pizza.jpg")
	pizza.Description = ptr("Mozzarella")
	pizza.Configuration = models.ConfigurableResponse{Attributes: []models.ConfigurableAttribute{
		{ID: "attr-1", Title: "Suppléments", MinOptions: 0, MaxOptions: 2, Options: []models.ConfigurableOption{
			{ID: "501", Title: "Olives", ExtraPrice: 100},
		}},
	}}
	calzone := p("21", "Calzone", 1100)
	calzone.Configuration = pizza.Configuration // groupe partagé
	vin := p("30", "Vin rouge", 900)
	vin.TVADelivery = ptr(20.0)
	court := p("31", "X", 100) // nom trop court : ignoré

	menu := &models.MenuResponse{ProductsTypes: []models.ProductCategory{
		{CategoryName: "Pizzas", CategoryID: ptr("c1"), Products: []models.ProductEntry{pizza, calzone}},
		{CategoryName: "Vins", CategoryID: ptr("c2"), Products: []models.ProductEntry{vin, court}},
	}}
	cat, err := marketplacecatalog.Build(menu, marketplacecatalog.Deliveroo, map[string]marketplacecatalog.ProductExtras{
		"30": {ContainsAlcohol: true},
	})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return cat
}

func TestBuildMenuDocument_RequiredFieldsAndIDs(t *testing.T) {
	built, err := BuildMenuDocument(testCatalog(t), DocumentOptions{SiteID: "102330", MenuName: "Brasserie", CoverImageURL: "https://cdn/cover.jpg"})
	if err != nil {
		t.Fatalf("BuildMenuDocument: %v", err)
	}
	doc := built.Document

	if !reflect.DeepEqual(doc.SiteIDs, []string{"102330"}) {
		t.Errorf("site_ids = %v", doc.SiteIDs)
	}
	mt := doc.Menu.Mealtimes
	if len(mt) != 1 || mt[0].Image.URL != "https://cdn/cover.jpg" || len(mt[0].Schedule) != 7 {
		t.Fatalf("mealtime = %+v", mt)
	}
	if !reflect.DeepEqual(mt[0].CategoryIDs, []string{"cat-c1", "cat-c2"}) {
		t.Errorf("category_ids = %v", mt[0].CategoryIDs)
	}
	if mt[0].Name["en"] != "Brasserie" || mt[0].Description["en"] == "" {
		t.Errorf("nom / description du mealtime manquants : %+v", mt[0])
	}

	items := map[string]MenuItem{}
	for _, it := range doc.Menu.Items {
		items[it.ID] = it
	}
	pizza := items["20"]
	if pizza.PLU != "20" || pizza.OperationalName != "Pizza 4 fromages" || pizza.Type != "ITEM" ||
		pizza.TaxRate != "10" || pizza.PriceInfo.Price != 1200 || pizza.Image == nil || pizza.Description["en"] != "Mozzarella" {
		t.Errorf("article pizza = %+v", pizza)
	}
	if !reflect.DeepEqual(pizza.ModifierIDs, []string{"mod-attr-1"}) {
		t.Errorf("modifier_ids = %v", pizza.ModifierIDs)
	}
	vin := items["30"]
	if vin.TaxRate != "20" || !vin.ContainsAlcohol {
		t.Errorf("vin = %+v, want TVA 20 et contains_alcohol", vin)
	}
	if _, ok := items["31"]; ok {
		t.Error("un nom de moins de 2 caractères doit être ignoré")
	}
	if len(built.Warnings) != 1 || !strings.Contains(built.Warnings[0], "31") {
		t.Errorf("warnings = %v", built.Warnings)
	}

	opt, ok := items["opt-501"]
	if !ok || opt.Type != "CHOICE" || opt.PLU != "opt-501" || opt.PriceInfo.Price != 100 {
		t.Errorf("option = %+v", opt)
	}
	if len(doc.Menu.Modifiers) != 1 {
		t.Fatalf("modifiers = %d, want 1 (groupe partagé publié une fois)", len(doc.Menu.Modifiers))
	}
	if m := doc.Menu.Modifiers[0]; m.MaxSelection != 2 || !reflect.DeepEqual(m.ItemIDs, []string{"opt-501"}) {
		t.Errorf("modifier = %+v", m)
	}

	if !reflect.DeepEqual(built.ProductItemIDs, []string{"20", "21", "30"}) {
		t.Errorf("ProductItemIDs = %v", built.ProductItemIDs)
	}
	if built.OptionItems["opt-501"] != "501" || built.ProductNames["30"] != "Vin rouge" {
		t.Errorf("correspondances = %v / %v", built.OptionItems, built.ProductNames)
	}

	// Le JSON contient bien les champs obligatoires de Deliveroo.
	raw, _ := json.Marshal(doc)
	for _, key := range []string{`"site_ids"`, `"mealtimes"`, `"category_ids"`, `"schedule"`, `"plu"`, `"tax_rate"`, `"contains_alcohol"`, `"operational_name"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("champ %s absent du document", key)
		}
	}
}

func TestBuildMenuDocument_FingerprintAndRevision(t *testing.T) {
	opts := DocumentOptions{SiteID: "1", MenuName: "B", CoverImageURL: "u"}
	a, err := BuildMenuDocument(testCatalog(t), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := BuildMenuDocument(testCatalog(t), opts)
	if a.Fingerprint != b.Fingerprint {
		t.Error("même catalogue, même empreinte attendue")
	}
	opts.Revision = "rév. 1"
	c, _ := BuildMenuDocument(testCatalog(t), opts)
	if c.Fingerprint == a.Fingerprint {
		t.Error("la révision doit changer l'empreinte (scénarios : webhook garanti)")
	}
}

func TestBuildMenuDocument_RequiresSiteAndCover(t *testing.T) {
	if _, err := BuildMenuDocument(testCatalog(t), DocumentOptions{CoverImageURL: "u"}); err == nil {
		t.Error("site_id manquant : erreur attendue")
	}
	if _, err := BuildMenuDocument(testCatalog(t), DocumentOptions{SiteID: "1"}); err == nil {
		t.Error("couverture manquante : erreur attendue")
	}
}

func TestFormatTaxRate(t *testing.T) {
	for in, want := range map[float64]string{10: "10", 5.5: "5.5", 20: "20", 0: "0", 2.1: "2.1"} {
		if got := formatTaxRate(in); got != want {
			t.Errorf("formatTaxRate(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAvailabilityPayload(t *testing.T) {
	got := availabilityPayload(map[string]marketplacecatalog.Availability{
		"30": marketplacecatalog.Hidden, "20": marketplacecatalog.Available, "21": marketplacecatalog.SoldOut,
	})
	want := []ItemUnavailability{{"20", "available"}, {"21", "unavailable"}, {"30", "hidden"}}
	if !reflect.DeepEqual(got.ItemUnavailabilities, want) {
		t.Errorf("payload = %v, want %v", got.ItemUnavailabilities, want)
	}
}

func TestCenterCrop16x9(t *testing.T) {
	cases := []struct {
		in, want image.Rectangle
	}{
		{image.Rect(0, 0, 1920, 1280), image.Rect(0, 100, 1920, 1180)}, // bannière 3:2
		{image.Rect(0, 0, 1024, 1024), image.Rect(0, 224, 1024, 800)},  // carré
		{image.Rect(0, 0, 3000, 1000), image.Rect(611, 0, 2388, 1000)}, // très large
		{image.Rect(0, 0, 1920, 1080), image.Rect(0, 0, 1920, 1080)},   // déjà 16:9
	}
	for _, c := range cases {
		if got := centerCrop16x9(c.in); got != c.want {
			t.Errorf("centerCrop16x9(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func decodeJPEGSize(t *testing.T, data []byte) image.Point {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("JPEG illisible: %v", err)
	}
	return img.Bounds().Size()
}

func TestRenderCovers_Are1920x1080JPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	for x := 0; x < 1024; x++ {
		src.Set(x, 512, color.Black)
	}
	data, err := RenderCoverFromImage(src)
	if err != nil {
		t.Fatalf("RenderCoverFromImage: %v", err)
	}
	if got := decodeJPEGSize(t, data); got != image.Pt(1920, 1080) {
		t.Errorf("couverture = %v, want 1920x1080", got)
	}

	def, err := RenderDefaultCover()
	if err != nil {
		t.Fatalf("RenderDefaultCover: %v", err)
	}
	if got := decodeJPEGSize(t, def); got != image.Pt(1920, 1080) {
		t.Errorf("couverture par défaut = %v, want 1920x1080", got)
	}
}
