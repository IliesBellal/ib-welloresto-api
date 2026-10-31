package deliveroo_orders

import (
	"context"
	"errors"
	"testing"
)

type fakeSync struct {
	productErr error
	optionErr  map[string]error // par pos_item_id de l'option
	products   int
	options    int
}

func (f *fakeSync) SyncProduct(_ context.Context, _ string, item DeliverooItem) (string, error) {
	f.products++
	if f.productErr != nil {
		return "", f.productErr
	}
	return "42", nil
}

func (f *fakeSync) SyncOption(_ context.Context, _ string, _ string, mod DeliverooModifier) (string, string, error) {
	f.options++
	if err := f.optionErr[mod.PosItemID]; err != nil {
		return "", "", err
	}
	return "A1", "O-" + mod.PosItemID, nil
}

func newTestDeliverooService(sync *fakeSync) *DeliverooService {
	return &DeliverooService{
		sync:            sync,
		fallbackProduct: func(context.Context, string) (string, error) { return "777", nil },
	}
}

var itemWithMods = DeliverooItem{
	PosItemID: "pizza", Name: "Pizza Reine", Quantity: 2, UnitPrice: money(1450),
	Modifiers: []DeliverooModifier{
		{PosItemID: "sans-oignons", Name: "Sans oignons", Quantity: 1},
		{PosItemID: "sauce", Name: "Sauce", Quantity: 2},
	},
}

func TestBuildProducts_toutMappe_cheminCourantInchange(t *testing.T) {
	s := newTestDeliverooService(&fakeSync{})

	got, err := s.buildProductsPayload(context.Background(), "212", []DeliverooItem{itemWithMods})

	if err != nil || len(got) != 1 {
		t.Fatalf("buildProductsPayload = (%+v, %v)", got, err)
	}
	p := got[0]
	if p.ProductID != "42" || p.Quantity != 2 || p.Price != 1450 || p.Comment != nil {
		t.Fatalf("ligne = %+v, want le produit mappé, sans commentaire", p)
	}
	if p.Config == nil || len(p.Config.Attributes) != 1 || len(p.Config.Attributes[0].Options) != 2 {
		t.Fatalf("configuration = %+v, want 1 groupe et 2 options", p.Config)
	}
}

func TestBuildProducts_sansOption_configurationVideCommeAvant(t *testing.T) {
	s := newTestDeliverooService(&fakeSync{})

	got, err := s.buildProductsPayload(context.Background(), "212", []DeliverooItem{{PosItemID: "x", Name: "X", Quantity: 1, UnitPrice: money(500)}})

	if err != nil || len(got) != 1 || got[0].Config == nil || len(got[0].Config.Attributes) != 0 {
		t.Fatalf("got = (%+v, %v), want une configuration vide non nulle", got, err)
	}
}

func TestBuildProducts_optionNonCreable_passeEnTexte(t *testing.T) {
	s := newTestDeliverooService(&fakeSync{optionErr: map[string]error{"sauce": errors.New("value too long")}})

	got, err := s.buildProductsPayload(context.Background(), "212", []DeliverooItem{itemWithMods})

	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	p := got[0]
	if len(p.Config.Attributes) != 1 || len(p.Config.Attributes[0].Options) != 1 || p.Config.Attributes[0].Options[0].ID != "O-sans-oignons" {
		t.Fatalf("l'option créée doit rester structurée : %+v", p.Config)
	}
	if p.Comment == nil || p.Comment.Content != "Options : Sauce ×2" {
		t.Fatalf("commentaire = %+v, want l'option perdue", p.Comment)
	}
}

func TestBuildProducts_produitNonCreable_articleDeRepliAvecLeVraiTitre(t *testing.T) {
	s := newTestDeliverooService(&fakeSync{productErr: errors.New("insert failed")})

	got, err := s.buildProductsPayload(context.Background(), "212", []DeliverooItem{itemWithMods})

	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	p := got[0]
	if p.ProductID != "777" || p.Quantity != 2 || p.Price != 1450 {
		t.Fatalf("ligne = %+v, want l'article de repli au prix de Deliveroo", p)
	}
	if len(p.Config.Attributes) != 0 {
		t.Fatalf("aucune option structurée sur l'article de repli : %+v", p.Config)
	}
	want := "Article Deliveroo : Pizza Reine | Options : Sans oignons, Sauce ×2"
	if p.Comment == nil || p.Comment.Content != want {
		t.Fatalf("commentaire = %+v, want %q", p.Comment, want)
	}
}

func TestBuildProducts_produitEtRepliEnEchec_laCommandeEchoue(t *testing.T) {
	s := newTestDeliverooService(&fakeSync{productErr: errors.New("down")})
	s.fallbackProduct = func(context.Context, string) (string, error) { return "", errors.New("down too") }

	if _, err := s.buildProductsPayload(context.Background(), "212", []DeliverooItem{itemWithMods}); err == nil {
		t.Fatal("sans article de repli la commande doit échouer, pour que la nouvelle tentative de Deliveroo la traite")
	}
}
