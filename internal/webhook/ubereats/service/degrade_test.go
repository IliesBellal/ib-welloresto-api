package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- faux dépôts : un mapping en mémoire, des échecs à la demande -----------------

type fakeProductRepo struct {
	byItem    map[string]string
	findErr   error
	createErr error
	created   []string
}

func (f *fakeProductRepo) FindProductIDByUberItemID(_ context.Context, _, item string) (*string, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if id, ok := f.byItem[item]; ok {
		return &id, nil
	}
	return nil, nil
}

func (f *fakeProductRepo) CreateProductMapping(_ context.Context, _, productID, item string) error {
	if f.createErr != nil {
		return f.createErr
	}
	if f.byItem == nil {
		f.byItem = map[string]string{}
	}
	f.byItem[item] = productID
	f.created = append(f.created, item)
	return nil
}

type fakeCatalog struct {
	err   error
	calls int
	names []string
}

func (f *fakeCatalog) CreateProductFromExternal(_ context.Context, _, title, _ string, _ int, _ bool) (*string, error) {
	f.calls++
	f.names = append(f.names, title)
	if f.err != nil {
		return nil, f.err
	}
	id := "900"
	return &id, nil
}

type fakeAttrRepo struct {
	attrs          map[string]string // groupe -> attribut
	options        map[string]string // option Uber -> option
	getAttrErr     error
	createAttrErr  error
	createOptErr   error
	createdAttrs   int
	createdOptions int
}

func (f *fakeAttrRepo) GetAttributeIDByModifierGroupID(_ context.Context, _, group string) (*string, error) {
	if f.getAttrErr != nil {
		return nil, f.getAttrErr
	}
	if id, ok := f.attrs[group]; ok {
		return &id, nil
	}
	return nil, nil
}

func (f *fakeAttrRepo) CreateAttributeFromUberGroup(_ context.Context, _, _, _ string) (string, error) {
	if f.createAttrErr != nil {
		return "", f.createAttrErr
	}
	f.createdAttrs++
	return "attr-new", nil
}

func (f *fakeAttrRepo) CreateAttributeMapping(_ context.Context, _, attrID, group string) error {
	if f.attrs == nil {
		f.attrs = map[string]string{}
	}
	f.attrs[group] = attrID
	return nil
}

func (f *fakeAttrRepo) GetOptionIDByUberItemID(_ context.Context, _, item string) (*string, error) {
	if id, ok := f.options[item]; ok {
		return &id, nil
	}
	return nil, nil
}

func (f *fakeAttrRepo) CreateOptionFromUber(_ context.Context, _, _ string, _ int) (string, error) {
	if f.createOptErr != nil {
		return "", f.createOptErr
	}
	f.createdOptions++
	return "opt-new", nil
}

func (f *fakeAttrRepo) CreateOptionMapping(_ context.Context, _, optID, item string) error {
	if f.options == nil {
		f.options = map[string]string{}
	}
	f.options[item] = optID
	return nil
}

func newTestService(products *fakeProductRepo, catalog *fakeCatalog, attrs *fakeAttrRepo) *Service {
	return &Service{
		productMappingRepo:   products,
		attributeMappingRepo: attrs,
		menuService:          catalog,
		inLockedTx: func(ctx context.Context, _ string, fn func(context.Context) error) error {
			return fn(ctx) // pas de base en test unitaire : la transaction est testée en intégration
		},
		fallbackProduct: func(context.Context, string) (string, error) { return "777", nil },
	}
}

const itemWithOptions = `{"id": "pizza-1", "title": "Pizza Reine", "quantity": 1,
  "price": {"unit_price": {"amount": 1450}},
  "selected_modifier_groups": [
    {"id": "grp-base", "title": "Base", "selected_items": [
      {"id": "opt-tomate", "title": "Tomate", "quantity": 1, "price": {"unit_price": {"amount": 0}}},
      {"id": "opt-creme", "title": "Crème", "quantity": 2, "price": {"unit_price": {"amount": 100}}}]}]}`

// --- chemin courant : inchangé -----------------------------------------------------

func TestMapItems_mappingExistant_aucuneCreation(t *testing.T) {
	products := &fakeProductRepo{byItem: map[string]string{"pizza-1": "42"}}
	catalog := &fakeCatalog{}
	attrs := &fakeAttrRepo{attrs: map[string]string{"grp-base": "A1"}, options: map[string]string{"opt-tomate": "O1", "opt-creme": "O2"}}
	s := newTestService(products, catalog, attrs)
	order := parseOrder(t, `{"cart": {"items": [`+itemWithOptions+`]}}`)

	got, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items)

	if err != nil || len(got) != 1 {
		t.Fatalf("mapUberItemsToOrderProducts = (%+v, %v)", got, err)
	}
	p := got[0]
	if p.ProductID != "42" || p.Comment != nil {
		t.Fatalf("produit = %+v, want le produit mappé et aucun commentaire", p)
	}
	if catalog.calls != 0 || attrs.createdAttrs != 0 || attrs.createdOptions != 0 {
		t.Fatal("rien ne doit être créé quand tout est déjà mappé")
	}
	if p.Config == nil || len(p.Config.Attributes) != 1 || len(p.Config.Attributes[0].Options) != 2 {
		t.Fatalf("configuration = %+v, want 1 groupe et 2 options", p.Config)
	}
	if opt := p.Config.Attributes[0].Options[1]; opt.ID != "O2" || opt.Quantity != 2 || opt.ExtraPrice != 100 {
		t.Fatalf("option = %+v", opt)
	}
}

func TestMapItems_articleInconnu_creeProduitEtMapping(t *testing.T) {
	products := &fakeProductRepo{}
	catalog := &fakeCatalog{}
	s := newTestService(products, catalog, &fakeAttrRepo{})
	order := parseOrder(t, `{"cart": {"items": [{"id": "new-1", "title": "Pizza Neuve", "quantity": 1, "price": {"unit_price": {"amount": 900}}}]}}`)

	got, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items)

	if err != nil || len(got) != 1 || got[0].ProductID != "900" || got[0].Comment != nil {
		t.Fatalf("got = (%+v, %v)", got, err)
	}
	if len(products.created) != 1 || products.byItem["new-1"] != "900" {
		t.Fatalf("le mapping doit être créé avec le produit : %+v", products.byItem)
	}
}

// --- repli ------------------------------------------------------------------------

func TestMapItems_optionNonCreable_passeEnTexteEtLaCommandeContinue(t *testing.T) {
	products := &fakeProductRepo{byItem: map[string]string{"pizza-1": "42"}}
	attrs := &fakeAttrRepo{attrs: map[string]string{"grp-base": "A1"}, options: map[string]string{"opt-tomate": "O1"}, createOptErr: errors.New("value too long")}
	s := newTestService(products, &fakeCatalog{}, attrs)
	order := parseOrder(t, `{"cart": {"items": [`+itemWithOptions+`]}}`)

	got, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items)

	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	p := got[0]
	if p.Config == nil || len(p.Config.Attributes[0].Options) != 1 || p.Config.Attributes[0].Options[0].ID != "O1" {
		t.Fatalf("l'option mappée doit rester structurée : %+v", p.Config)
	}
	if p.Comment == nil || p.Comment.Content != "Options Base : Crème ×2" {
		t.Fatalf("l'option perdue doit être dans le commentaire : %+v", p.Comment)
	}
}

func TestMapItems_groupeNonCreable_toutLeGroupePasseEnTexte(t *testing.T) {
	products := &fakeProductRepo{byItem: map[string]string{"pizza-1": "42"}}
	attrs := &fakeAttrRepo{createAttrErr: errors.New("duplicate key")}
	s := newTestService(products, &fakeCatalog{}, attrs)
	order := parseOrder(t, `{"cart": {"items": [`+itemWithOptions+`]}}`)

	got, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items)

	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	if got[0].Config != nil {
		t.Fatalf("aucune option structurée attendue : %+v", got[0].Config)
	}
	if want := "Options Base : Tomate, Crème ×2"; got[0].Comment == nil || got[0].Comment.Content != want {
		t.Fatalf("commentaire = %+v, want %q", got[0].Comment, want)
	}
}

func TestMapItems_produitNonCreable_articleDeRepliAvecLeVraiTitre(t *testing.T) {
	products := &fakeProductRepo{}
	catalog := &fakeCatalog{err: errors.New("insert failed")}
	s := newTestService(products, catalog, &fakeAttrRepo{})
	order := parseOrder(t, `{"cart": {"items": [{"id": "pizza-1", "title": "Pizza Reine", "quantity": 2,
	  "price": {"unit_price": {"amount": 1450}}, "special_instructions": "Sans olives",
	  "selected_modifier_groups": [{"id": "g", "title": "Base", "selected_items": [
	    {"id": "o", "title": "Tomate", "quantity": 1, "price": {"unit_price": {"amount": 0}}}]}]}]}}`)

	got, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items)

	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	p := got[0]
	if p.ProductID != "777" || p.Quantity != 2 || p.Price != 1450 || p.Config != nil {
		t.Fatalf("ligne = %+v, want l'article de repli au prix d'Uber et sans option structurée", p)
	}
	want := "Article Uber Eats : Pizza Reine | Sans olives | Options Base : Tomate"
	if p.Comment == nil || p.Comment.Content != want {
		t.Fatalf("commentaire = %+v, want %q", p.Comment, want)
	}
}

func TestMapItems_produitEtRepliEnEchec_laCommandeEchoue(t *testing.T) {
	s := newTestService(&fakeProductRepo{}, &fakeCatalog{err: errors.New("down")}, &fakeAttrRepo{})
	s.fallbackProduct = func(context.Context, string) (string, error) { return "", errors.New("down too") }
	order := parseOrder(t, `{"cart": {"items": [{"id": "x", "title": "X", "quantity": 1, "price": {"unit_price": {"amount": 100}}}]}}`)

	if _, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items); err == nil {
		t.Fatal("sans article de repli la commande doit échouer, pour que la nouvelle tentative d'Uber la traite")
	}
}

// Une lecture en échec (base en difficulté) n'est pas une dégradation : on laisse la
// nouvelle tentative d'Uber traiter la commande entière avec ses options structurées.
func TestMapItems_lectureEnEchec_remonteLErreur(t *testing.T) {
	s := newTestService(&fakeProductRepo{findErr: errors.New("connection reset")}, &fakeCatalog{}, &fakeAttrRepo{})
	order := parseOrder(t, `{"cart": {"items": [`+itemWithOptions+`]}}`)
	if _, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items); err == nil {
		t.Fatal("une erreur de lecture doit remonter")
	}

	s = newTestService(&fakeProductRepo{byItem: map[string]string{"pizza-1": "42"}}, &fakeCatalog{}, &fakeAttrRepo{getAttrErr: errors.New("connection reset")})
	if _, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items); err == nil {
		t.Fatal("une erreur de lecture du groupe doit remonter")
	}
}

func TestMapItems_titreTropLong_estTronquePourLaColonne(t *testing.T) {
	catalog := &fakeCatalog{}
	s := newTestService(&fakeProductRepo{}, catalog, &fakeAttrRepo{})
	long := strings.Repeat("🍕", 300)
	order := parseOrder(t, `{"cart": {"items": [{"id": "x", "title": "`+long+`", "quantity": 1, "price": {"unit_price": {"amount": 100}}}]}}`)

	if _, err := s.mapUberItemsToOrderProducts(context.Background(), "212", order.Cart.Items); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(catalog.names[0])); n != productNameMax {
		t.Fatalf("nom du produit créé = %d caractères, want %d (products.name)", n, productNameMax)
	}
}
