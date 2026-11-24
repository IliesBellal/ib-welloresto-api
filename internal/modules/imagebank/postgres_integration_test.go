//go:build postgres_integration

package imagebank

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/infrastructure/r2"
)

// Migration 224 requise. Écriture par le script d'import, relecture par le
// back-office, et requête « Mes images » sur le schéma réel.
func TestImageBankRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	repo := NewRepository(db)
	const slug = "itest-burger-classique"
	key := r2.GenerateImageBankKey("burgers", slug, ".jpg")
	_, _ = db.ExecContext(ctx, `DELETE FROM image_bank WHERE slug = $1`, slug)
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM image_bank WHERE slug = $1`, slug) })

	if e, err := repo.FindBySlug(ctx, slug); err != nil || e != nil {
		t.Fatalf("FindBySlug avant import : %+v %v", e, err)
	}
	row := ImportRow{
		Slug: slug, Category: "burgers", Name: "Burger classique", Tags: []string{"Burger", "boeuf", "burger"},
		Format: FormatSquare, ImageURL: "https://storage.welloresto.fr/" + key, R2Key: key,
		ContentType: "image/jpeg", Width: 1200, Height: 1200, SizeBytes: 300000,
		ContentHash: "0000000000000000000000000000000000000000000000000000000000000001",
		Source:      "ia", License: "test", Enabled: true,
	}
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatal(err)
	}
	row.Name = "Burger maison"
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatalf("second upsert : %v", err)
	}
	e, err := repo.FindBySlug(ctx, slug)
	if err != nil || e == nil || e.R2Key != key {
		t.Fatalf("FindBySlug : %+v %v", e, err)
	}
	images, err := repo.ListEnabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *Image
	for i := range images {
		if images[i].Slug == slug {
			found = &images[i]
		}
	}
	if found == nil || found.Name != "Burger maison" || len(found.Tags) != 2 {
		t.Fatalf("ListEnabled : %+v", found)
	}
	if ok, err := repo.IsEnabledKey(ctx, key); err != nil || !ok {
		t.Fatalf("IsEnabledKey : %v %v", ok, err)
	}

	row.Enabled = false
	if err := repo.Upsert(ctx, row); err != nil {
		t.Fatal(err)
	}
	if ok, _ := repo.IsEnabledKey(ctx, key); ok {
		t.Fatal("une image désactivée ne doit plus être proposée")
	}

	// « Mes images » : une catégorie et une bannière ScanNOrder d'un
	// établissement fictif.
	const mid = "itest-imgbank"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM productcateg WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM scannorder_settings WHERE merchant_id = $1`, mid)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := db.ExecContext(ctx, `INSERT INTO productcateg (merchant_id, merchant_categ_id, categ_name, categ_order, image_url, enabled)
		VALUES ($1, 'itest-cat', 'Desserts', 1, 'https://storage.welloresto.fr/x/cat.jpg', TRUE)`, mid); err != nil {
		t.Fatalf("seed productcateg : %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO scannorder_settings (merchant_id, seo_title, seo_description, seo_keywords, seo_cuisine_type, banner_url)
		VALUES ($1, '', '', '', '', 'https://storage.welloresto.fr/x/banner.jpg')`, mid); err != nil {
		t.Fatalf("seed scannorder_settings : %v", err)
	}
	mine, err := repo.ListMerchantImageURLs(ctx, mid)
	if err != nil {
		t.Fatalf("ListMerchantImageURLs : %v", err)
	}
	usages := map[string]string{}
	for _, m := range mine {
		usages[m.Usage] = m.Label
	}
	if len(mine) != 2 || usages["category"] != "Desserts" {
		t.Fatalf("ListMerchantImageURLs : %+v", mine)
	}
	if _, ok := usages["scannorder_banner"]; !ok {
		t.Fatalf("bannière ScanNOrder absente : %+v", mine)
	}
}
