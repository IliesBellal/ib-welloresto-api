//go:build postgres_integration

package menu

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Ordre d'affichage des options d'un attribut (migration 192) : la position dans
// le tableau reçu devient display_order, et toutes les lectures renvoient les
// options triées (display_order, id).
func TestMenuRepository_AttributeOptionsDisplayOrder_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanup := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM configurable_attribute_options WHERE configurable_attribute_id IN (SELECT id FROM configurable_attributes WHERE merchant_id = $1)`,
			`DELETE FROM configurable_attributes WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-menu-optorder' LIMIT 1`).Scan(&oldID); err == nil {
		cleanup(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanup(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest Option Order Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-menu-optorder', 'https://x', '06', 'mtok-menu-optorder', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed merchant_parameters: %v", err)
	}

	repo := NewMenuRepository(db, nil)

	// titles lit l'ordre renvoyé ET vérifie que display_order = rang 0, 1, 2...
	titles := func(label string, opts []AttributeOption) []string {
		t.Helper()
		out := make([]string, len(opts))
		for i, o := range opts {
			out[i] = o.Title
			if o.DisplayOrder != i {
				t.Fatalf("%s: option %q DisplayOrder = %d, want %d", label, o.Title, o.DisplayOrder, i)
			}
		}
		return out
	}
	assertOrder := func(label string, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s = %v, want %v", label, got, want)
			}
		}
	}

	// Création : l'ordre du tableau (volontairement non alphabétique) est conservé.
	attrID, err := repo.CreateAttribute(ctx, merchantID, &UpdateAttributePayload{
		Type: "CHECK", Name: "order-itest", Title: "Ordre ?", Min: 0, Max: 3,
		Options: []UpdateAttributeOptionPayload{{Title: "Mayo"}, {Title: "Ketchup"}, {Title: "Algérienne"}},
	})
	if err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}
	attr, err := repo.GetAttribute(ctx, merchantID, attrID)
	if err != nil {
		t.Fatalf("GetAttribute: %v", err)
	}
	assertOrder("GetAttribute après création", titles("GetAttribute", attr.Options), []string{"Mayo", "Ketchup", "Algérienne"})

	attrs, err := repo.GetAttributes(ctx, merchantID)
	if err != nil || len(attrs) != 1 {
		t.Fatalf("GetAttributes = (%d, %v)", len(attrs), err)
	}
	assertOrder("GetAttributes après création", titles("GetAttributes", attrs[0].Options), []string{"Mayo", "Ketchup", "Algérienne"})

	// Réordonnancement + ajout : les ids existants repartent dans un autre ordre,
	// la nouvelle option (sans id) se place où elle est dans le tableau.
	ids := map[string]string{}
	for _, o := range attr.Options {
		ids[o.Title] = o.ID
	}
	idOf := func(title string) *string { id := ids[title]; return &id }
	if err := repo.UpdateAttribute(ctx, merchantID, attrID, &UpdateAttributePayload{
		Type: "CHECK", Name: "order-itest", Title: "Ordre ?", Min: 0, Max: 3,
		Options: []UpdateAttributeOptionPayload{
			{ID: idOf("Algérienne"), Title: "Algérienne"},
			{Title: "Harissa"},
			{ID: idOf("Mayo"), Title: "Mayo"},
			{ID: idOf("Ketchup"), Title: "Ketchup"},
		},
	}); err != nil {
		t.Fatalf("UpdateAttribute: %v", err)
	}
	attr, err = repo.GetAttribute(ctx, merchantID, attrID)
	if err != nil {
		t.Fatalf("GetAttribute après update: %v", err)
	}
	assertOrder("GetAttribute après réordonnancement", titles("GetAttribute", attr.Options), []string{"Algérienne", "Harissa", "Mayo", "Ketchup"})

	// Suppression d'une option (absente du payload) : les restantes gardent leur ordre relatif.
	if err := repo.UpdateAttribute(ctx, merchantID, attrID, &UpdateAttributePayload{
		Type: "CHECK", Name: "order-itest", Title: "Ordre ?", Min: 0, Max: 3,
		Options: []UpdateAttributeOptionPayload{
			{ID: idOf("Ketchup"), Title: "Ketchup"},
			{ID: idOf("Algérienne"), Title: "Algérienne"},
		},
	}); err != nil {
		t.Fatalf("UpdateAttribute (suppression): %v", err)
	}
	attr, err = repo.GetAttribute(ctx, merchantID, attrID)
	if err != nil {
		t.Fatalf("GetAttribute après suppression: %v", err)
	}
	assertOrder("GetAttribute après suppression", titles("GetAttribute", attr.Options), []string{"Ketchup", "Algérienne"})
}
