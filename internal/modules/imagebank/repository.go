package imagebank

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"welloresto-api/internal/database/dbx"
)

type Repository struct {
	database *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{database: db}
}

// ListEnabled renvoie toutes les images actives, dans l'ordre d'affichage.
// La banque compte quelques centaines d'images : le filtrage se fait ensuite
// en mémoire sur la liste mise en cache (Service.List).
func (r *Repository) ListEnabled(ctx context.Context) ([]Image, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
	SELECT id, slug, category, name, tags, format, image_url, width, height
	FROM image_bank
	WHERE enabled = TRUE
	ORDER BY category, sort_order, name, id`)
	if err != nil {
		return nil, fmt.Errorf("list image bank: %w", err)
	}
	defer rows.Close()

	images := []Image{}
	for rows.Next() {
		var img Image
		var tags string
		if err := rows.Scan(&img.ID, &img.Slug, &img.Category, &img.Name, &tags, &img.Format, &img.ImageURL, &img.Width, &img.Height); err != nil {
			return nil, fmt.Errorf("scan image bank: %w", err)
		}
		img.Tags = SplitTags(tags)
		images = append(images, img)
	}
	return images, rows.Err()
}

// IsEnabledKey indique si key est le fichier d'une image active de la banque.
func (r *Repository) IsEnabledKey(ctx context.Context, key string) (bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var exists bool
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM image_bank WHERE r2_key = ? AND enabled = TRUE)`, key).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check image bank key: %w", err)
	}
	return exists, nil
}

// ListMerchantImageURLs renvoie les images actuellement utilisées par un
// établissement, sans dédoublonnage ni filtrage (fait par le service). Seuls
// les éléments actifs sont lus : un produit supprimé n'a plus rien à proposer.
func (r *Repository) ListMerchantImageURLs(ctx context.Context, merchantID string) ([]MyImage, error) {
	db := dbx.GetDB(ctx, r.database)
	rows, err := db.QueryContext(ctx, `
	SELECT p.image_url, 'product', COALESCE(p.name, '')
	  FROM products p
	 WHERE p.merchant_id = ? AND p.enabled = TRUE AND p.image_url IS NOT NULL AND p.image_url <> ''
	UNION ALL
	SELECT cao.image_url, 'option', COALESCE(cao.title, '')
	  FROM configurable_attribute_options cao
	  JOIN configurable_attributes ca ON ca.id = cao.configurable_attribute_id
	 WHERE ca.merchant_id = ? AND ca.enabled = TRUE AND cao.enabled = 1
	   AND cao.image_url IS NOT NULL AND cao.image_url <> ''
	UNION ALL
	SELECT pc.image_url, 'category', COALESCE(pc.categ_name, '')
	  FROM productcateg pc
	 WHERE pc.merchant_id = ? AND pc.enabled = TRUE AND pc.image_url IS NOT NULL AND pc.image_url <> ''
	UNION ALL
	SELECT mc.image_url, 'marketing_category', COALESCE(mc.name, '')
	  FROM marketing_categories mc
	 WHERE mc.merchant_id = ? AND mc.enabled = TRUE AND mc.image_url IS NOT NULL AND mc.image_url <> ''
	UNION ALL
	SELECT sns.banner_url, 'scannorder_banner', ''
	  FROM scannorder_settings sns
	 WHERE sns.merchant_id = ? AND sns.banner_url IS NOT NULL AND sns.banner_url <> ''
	UNION ALL
	SELECT ks.idle_image_url, 'kiosk_idle', ''
	  FROM kiosk_settings ks
	 WHERE ks.merchant_id = ? AND ks.idle_image_url IS NOT NULL AND ks.idle_image_url <> ''
	UNION ALL
	SELECT km.url, 'kiosk_marketing', ''
	  FROM kiosk_marketing_media km
	 WHERE km.merchant_id = ? AND km.kind = 'image' AND km.url IS NOT NULL
	UNION ALL
	SELECT cm.url, 'cds_media', ''
	  FROM cds_media_items cm
	  JOIN cds_displays cd ON cd.id = cm.display_id
	 WHERE cd.merchant_id = ? AND cd.status <> 'revoked' AND cm.kind = 'image' AND cm.url IS NOT NULL`,
		merchantID, merchantID, merchantID, merchantID, merchantID, merchantID, merchantID, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list merchant images: %w", err)
	}
	defer rows.Close()

	out := []MyImage{}
	for rows.Next() {
		var img MyImage
		if err := rows.Scan(&img.ImageURL, &img.Usage, &img.Label); err != nil {
			return nil, fmt.Errorf("scan merchant image: %w", err)
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

// ---- Écriture (cmd/image_bank_import uniquement) ----

// ImportRow : une image à enregistrer par le script d'import.
type ImportRow struct {
	Slug        string
	Category    string
	Name        string
	Tags        []string
	Format      string
	ImageURL    string
	R2Key       string
	ContentType string
	Width       int
	Height      int
	SizeBytes   int
	ContentHash string
	Source      string
	License     string
	Author      string
	SourceURL   string
	SortOrder   int
	Enabled     bool
}

// ExistingImport : état connu d'une image avant import.
type ExistingImport struct {
	R2Key       string
	ContentHash string
}

// FindBySlug renvoie la clé R2 et l'empreinte d'une image déjà importée, ou
// nil si le slug est inconnu.
func (r *Repository) FindBySlug(ctx context.Context, slug string) (*ExistingImport, error) {
	db := dbx.GetDB(ctx, r.database)
	var e ExistingImport
	err := db.QueryRowContext(ctx, `SELECT r2_key, content_hash FROM image_bank WHERE slug = ?`, slug).Scan(&e.R2Key, &e.ContentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find image bank slug: %w", err)
	}
	return &e, nil
}

// Upsert crée ou met à jour une image (clé : slug).
func (r *Repository) Upsert(ctx context.Context, row ImportRow) error {
	db := dbx.GetDB(ctx, r.database)
	_, err := db.ExecContext(ctx, `
	INSERT INTO image_bank (slug, category, name, tags, format, image_url, r2_key, content_type,
	                        width, height, size_bytes, content_hash, source, license, author, source_url,
	                        sort_order, enabled)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (slug) DO UPDATE SET
		category = EXCLUDED.category,
		name = EXCLUDED.name,
		tags = EXCLUDED.tags,
		format = EXCLUDED.format,
		image_url = EXCLUDED.image_url,
		r2_key = EXCLUDED.r2_key,
		content_type = EXCLUDED.content_type,
		width = EXCLUDED.width,
		height = EXCLUDED.height,
		size_bytes = EXCLUDED.size_bytes,
		content_hash = EXCLUDED.content_hash,
		source = EXCLUDED.source,
		license = EXCLUDED.license,
		author = EXCLUDED.author,
		source_url = EXCLUDED.source_url,
		sort_order = EXCLUDED.sort_order,
		enabled = EXCLUDED.enabled,
		updated_at = now()`,
		row.Slug, row.Category, row.Name, JoinTags(row.Tags), row.Format, row.ImageURL, row.R2Key, row.ContentType,
		row.Width, row.Height, row.SizeBytes, row.ContentHash, row.Source, row.License, row.Author, row.SourceURL,
		row.SortOrder, row.Enabled)
	if err != nil {
		return fmt.Errorf("upsert image bank %s: %w", row.Slug, err)
	}
	return nil
}

// SplitTags lit la colonne tags (« a,b,c »).
func SplitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// JoinTags normalise des tags (minuscules, sans doublon) pour la colonne tags.
func JoinTags(tags []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}
