//go:build postgres_integration

package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/modules/menu"
	"welloresto-api/internal/utils/dbutils"
	"welloresto-api/internal/webhook/marketplacefallback"
	"welloresto-api/internal/webhook/ubereats/repository"
)

// repoCatalog crée les produits comme le fait MenuService, sans ses autres dépendances.
type repoCatalog struct{ repo *menu.MenuRepository }

func (c repoCatalog) CreateProductFromExternal(ctx context.Context, merchantID, title, description string, amount int, alcohol bool) (*string, error) {
	id, err := c.repo.CreateExternalProductTx(ctx, merchantID, title, description, amount, alcohol)
	if err != nil {
		return nil, err
	}
	s := strconv.FormatInt(id, 10)
	return &s, nil
}

type unexpected struct{ msg string }

func (e *unexpected) Error() string { return e.msg }

func newMappingFixture(t *testing.T, merchantID string) (*Service, func(q string, args ...any) int, func(q string, args ...any), context.Context) {
	t.Helper()
	db := pgtest.Open(t)
	ctx := context.Background()

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM integration_uber_eats_options_mapping WHERE merchant_id = $1`,
			`DELETE FROM integration_uber_eats_attributes_mapping WHERE merchant_id = $1`,
			`DELETE FROM integration_uber_eats_products_mapping WHERE merchant_id = $1`,
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, merchantID)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	s := &Service{
		db:                   db,
		productMappingRepo:   repository.NewProductMappingRepository(db),
		attributeMappingRepo: repository.NewAttributeMappingRepository(db),
		menuService:          repoCatalog{repo: menu.NewMenuRepository(db, nil)},
		inLockedTx: func(ctx context.Context, key string, fn func(context.Context) error) error {
			return dbutils.RunInTxLocked(ctx, db, key, fn)
		},
		fallbackProduct: func(ctx context.Context, merchantID string) (string, error) {
			return marketplacefallback.Product(ctx, db, merchantID, "UBER_EATS", marketplacefallback.UberEatsName)
		},
	}
	count := func(q string, args ...any) int {
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s : %v", q, err)
		}
		return n
	}
	exec := func(q string, args ...any) {
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s : %v", q, err)
		}
	}
	return s, count, exec, ctx
}

// Des commandes simultanées qui contiennent le même article inconnu créent un seul
// produit et un seul mapping ; un titre de groupe trop long pour la colonne ne fait
// plus perdre la commande.
func TestUberMapping_Concurrent_Postgres(t *testing.T) {
	const merchantID = "itest-ue-lot3-a"
	s, count, _, ctx := newMappingFixture(t, merchantID)

	// La table de mapping des groupes porte configurable_attribute_id en integer alors que
	// les groupes créés depuis portent un identifiant texte (migration 197) : tant qu'elle
	// n'est pas appliquée, un groupe inconnu ne peut pas être mappé et passe en texte.
	structured := count(`SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'integration_uber_eats_attributes_mapping' AND column_name = 'configurable_attribute_id' AND data_type <> 'integer'`) == 1

	// Titre de groupe de 150 caractères : sans troncature, configurable_attributes.name (50)
	// et title (80) refusaient l'insertion.
	longGroup := strings.Repeat("Choisissez votre base ", 7)
	order := parseOrder(t, `{"cart": {"items": [{"id": "new-pizza", "title": "Pizza Nouvelle", "quantity": 1,
	  "price": {"unit_price": {"amount": 1200}},
	  "selected_modifier_groups": [{"id": "grp-new", "title": "`+longGroup+`", "selected_items": [
	    {"id": "opt-new", "title": "Tomate", "quantity": 1, "price": {"unit_price": {"amount": 0}}}]}]}]}}`)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	results := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.mapUberItemsToOrderProducts(ctx, merchantID, order.Cart.Items)
			if err != nil {
				errs <- err
				return
			}
			signature := got[0].ProductID
			if got[0].Config != nil {
				signature += "/" + got[0].Config.Attributes[0].ID + "/" + got[0].Config.Attributes[0].Options[0].ID
			}
			if got[0].Comment != nil {
				signature += "/" + got[0].Comment.Content
			}
			results <- signature
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("une commande a échoué : %v", err)
	}
	seen := map[string]bool{}
	for r := range results {
		seen[r] = true
	}
	if len(seen) != 1 {
		t.Fatalf("les commandes simultanées n'ont pas toutes la même ligne : %v", seen)
	}

	if n := count(`SELECT count(*) FROM products WHERE merchant_id = $1 AND category = 'UBER_EATS'`, merchantID); n != 1 {
		t.Errorf("%d produits créés, want 1", n)
	}
	if n := count(`SELECT count(*) FROM integration_uber_eats_products_mapping WHERE merchant_id = $1`, merchantID); n != 1 {
		t.Errorf("%d mappings de produit, want 1", n)
	}

	attrs := count(`SELECT count(*) FROM configurable_attributes WHERE merchant_id = $1`, merchantID)
	if structured {
		if attrs != 1 || count(`SELECT count(*) FROM integration_uber_eats_attributes_mapping WHERE merchant_id = $1`, merchantID) != 1 ||
			count(`SELECT count(*) FROM integration_uber_eats_options_mapping WHERE merchant_id = $1`, merchantID) != 1 {
			t.Errorf("groupe ou option créés en double : %d groupes", attrs)
		}
		var name, title string
		if err := s.db.QueryRowContext(ctx, `SELECT name, title FROM configurable_attributes WHERE merchant_id = $1`, merchantID).Scan(&name, &title); err != nil {
			t.Fatal(err)
		}
		if len([]rune(name)) > 50 || len([]rune(title)) > 80 || !strings.HasPrefix(name, "ue_choisissez") {
			t.Errorf("groupe créé = %q / %q, want un nom ≤ 50 et un titre ≤ 80 caractères", name, title)
		}
		return
	}

	// Schéma non migré : le groupe passe en texte, et la tentative de création, annulée
	// en entier, n'a laissé aucun groupe ni option orphelins.
	if attrs != 0 {
		t.Errorf("%d groupes orphelins après l'échec du mapping, want 0", attrs)
	}
	for r := range seen {
		if !strings.HasSuffix(r, "/Options "+longGroup[:len(strings.TrimSpace(longGroup))]+" : Tomate") &&
			!strings.Contains(r, "/Options Choisissez votre base") {
			t.Errorf("ligne = %q, want le groupe en texte dans le commentaire", r)
		}
	}
}

// L'échec de création d'une option annule tout (pas d'option sans mapping) et la
// commande est créée, l'option en texte.
func TestUberMapping_OptionFailure_Postgres(t *testing.T) {
	const merchantID = "itest-ue-lot3-b"
	s, count, exec, ctx := newMappingFixture(t, merchantID)

	// Un groupe déjà mappé (identifiant numérique, valable avant et après la migration 197)
	// et une option dont l'identifiant Uber est déjà pris par un autre groupe : le mapping
	// d'option (merchant_id, item_id) est unique, la création échoue après l'insertion de
	// l'option, qui doit être annulée avec le reste.
	exec(`INSERT INTO products (merchant_id, name, price, category) VALUES ($1, 'itest-ue-seed', 0, 'UBER_EATS')`, merchantID)
	var seedProduct int
	if err := s.db.QueryRowContext(ctx, `SELECT product_id FROM products WHERE merchant_id = $1`, merchantID).Scan(&seedProduct); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO configurable_attributes (id, product_id, merchant_id, name, title, max_options) VALUES ('9100001', $1, $2, 'ue_seed', 'Seed', 99)`, seedProduct, merchantID)
	exec(`INSERT INTO integration_uber_eats_attributes_mapping (merchant_id, configurable_attribute_id, modifier_group_id) VALUES ($1, '9100001', 'grp-mapped')`, merchantID)
	exec(`INSERT INTO configurable_attribute_options (configurable_attribute_id, title, extra_price) VALUES ('9100002', 'Autre groupe', 0)`)
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM configurable_attribute_options WHERE configurable_attribute_id = '9100002'`)
	})
	var otherOpt int
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM configurable_attribute_options WHERE configurable_attribute_id = '9100002'`).Scan(&otherOpt); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO integration_uber_eats_options_mapping (merchant_id, configurable_attribute_option_id, item_id) VALUES ($1, $2, 'opt-clash')`, merchantID, otherOpt)
	before := count(`SELECT count(*) FROM configurable_attribute_options WHERE configurable_attribute_id = '9100001'`)

	order := parseOrder(t, `{"cart": {"items": [{"id": "pizza", "title": "Pizza", "quantity": 1,
	  "price": {"unit_price": {"amount": 1200}},
	  "selected_modifier_groups": [{"id": "grp-mapped", "title": "Base", "selected_items": [
	    {"id": "opt-clash", "title": "Mozzarella", "quantity": 2, "price": {"unit_price": {"amount": 0}}}]}]}]}}`)

	got, err := s.mapUberItemsToOrderProducts(ctx, merchantID, order.Cart.Items)
	if err != nil || len(got) != 1 {
		t.Fatalf("la commande ne doit pas échouer : (%+v, %v)", got, err)
	}
	if got[0].Comment == nil || got[0].Comment.Content != "Options Base : Mozzarella ×2" {
		t.Fatalf("commentaire = %+v, want l'option en texte", got[0].Comment)
	}
	if got[0].Config != nil {
		t.Fatalf("aucune option structurée attendue : %+v", got[0].Config)
	}
	if after := count(`SELECT count(*) FROM configurable_attribute_options WHERE configurable_attribute_id = '9100001'`); after != before {
		t.Fatalf("une option orpheline a été laissée (%d avant, %d après)", before, after)
	}
}
