// Command image_bank_import alimente la banque d'images Wello Resto (table
// image_bank + dossier R2 wello_resto_images_storage/image_bank/) à partir d'un
// dossier d'images et d'un manifeste CSV. Voir docs/banque-images.md.
//
// Chaque image est allégée avant envoi (internal/infrastructure/imageopt :
// plus grand côté borné, JPEG qualité 82 ou PNG si transparence). L'import est
// idempotent : clé d'upsert = slug, et un fichier dont l'empreinte (sha256 du
// fichier source) n'a pas changé n'est pas renvoyé.
//
// Sans --apply, la commande vérifie le manifeste, mesure le gain de
// compression et affiche ce qui serait fait, sans rien écrire. POSTGRES_URL
// est alors facultatif (sans lui, toutes les lignes sont « nouvelles »).
//
// Les fichiers R2 ne sont jamais supprimés : retirer une image = enabled
// « false » dans le manifeste (des établissements l'utilisent peut-être).
//
// Usage :
//
//	POSTGRES_URL=... R2_ACCESS_KEY_ID=... R2_SECRET_ACCESS_KEY=... \
//	R2_ENDPOINT=... R2_BUCKET=... R2_PUBLIC_BASE_URL=https://storage.welloresto.fr \
//	[REDIS_URL=...] go run ./cmd/image_bank_import --manifest images/manifest.csv [--dir images] [--apply]
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/infrastructure/imageopt"
	"welloresto-api/internal/infrastructure/r2"
	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/modules/imagebank"
)

// Colonnes du manifeste. tags : séparés par « | ». enabled et sort_order
// facultatifs (défauts : true, 0).
var requiredColumns = []string{"slug", "file", "category", "name", "tags", "format", "source", "license", "author", "source_url"}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Entry : ligne validée du manifeste.
type Entry struct {
	Line      int
	Slug      string
	File      string
	Category  string
	Name      string
	Tags      []string
	Format    string
	Source    string
	License   string
	Author    string
	SourceURL string
	SortOrder int
	Enabled   bool
}

// ParseManifest lit et valide le manifeste. Toutes les erreurs sont
// renvoyées d'un coup (une par ligne fautive).
func ParseManifest(r io.Reader) ([]Entry, []error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, []error{fmt.Errorf("CSV illisible : %w", err)}
	}
	if len(records) == 0 {
		return nil, []error{errors.New("manifeste vide")}
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	var errs []error
	for _, c := range requiredColumns {
		if _, ok := col[c]; !ok {
			errs = append(errs, fmt.Errorf("colonne manquante : %s", c))
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	get := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var entries []Entry
	seen := map[string]int{}
	for n, rec := range records[1:] {
		line := n + 2
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		e := Entry{
			Line: line, Slug: get(rec, "slug"), File: get(rec, "file"), Category: get(rec, "category"),
			Name: get(rec, "name"), Format: get(rec, "format"), Source: get(rec, "source"),
			License: get(rec, "license"), Author: get(rec, "author"), SourceURL: get(rec, "source_url"),
			Enabled: true,
		}
		for _, t := range strings.Split(get(rec, "tags"), "|") {
			if t = strings.TrimSpace(t); t != "" {
				e.Tags = append(e.Tags, t)
			}
		}
		if v := get(rec, "sort_order"); v != "" {
			o, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("ligne %d : sort_order invalide %q", line, v))
			}
			e.SortOrder = o
		}
		if v := strings.ToLower(get(rec, "enabled")); v != "" {
			e.Enabled = v == "true" || v == "1" || v == "oui" || v == "yes"
		}

		switch {
		case !slugRe.MatchString(e.Slug) || len(e.Slug) > 120:
			errs = append(errs, fmt.Errorf("ligne %d : slug invalide %q (minuscules, chiffres, tirets)", line, e.Slug))
		case seen[e.Slug] != 0:
			errs = append(errs, fmt.Errorf("ligne %d : slug %q déjà utilisé ligne %d", line, e.Slug, seen[e.Slug]))
		}
		seen[e.Slug] = line
		if e.File == "" {
			errs = append(errs, fmt.Errorf("ligne %d : file vide", line))
		}
		if !imagebank.IsCategory(e.Category) {
			errs = append(errs, fmt.Errorf("ligne %d : catégorie inconnue %q", line, e.Category))
		}
		if e.Name == "" || len([]rune(e.Name)) > 160 {
			errs = append(errs, fmt.Errorf("ligne %d : name vide ou trop long", line))
		}
		if !imagebank.IsFormat(e.Format) {
			errs = append(errs, fmt.Errorf("ligne %d : format %q (square ou banner)", line, e.Format))
		}
		// Provenance obligatoire : chaque image doit pouvoir être justifiée.
		if e.Source == "" || e.License == "" {
			errs = append(errs, fmt.Errorf("ligne %d : source et license obligatoires", line))
		}
		entries = append(entries, e)
	}
	return entries, errs
}

type store interface {
	FindBySlug(ctx context.Context, slug string) (*imagebank.ExistingImport, error)
	Upsert(ctx context.Context, row imagebank.ImportRow) error
}

type uploader interface {
	UploadFile(ctx context.Context, key string, file io.Reader, contentType string) (string, error)
	PublicURL(key string) string
}

type stats struct {
	created, updated, unchanged, failed int
	before, after                       int64
}

// importAll traite les entrées. db et up peuvent être nil en simulation.
func importAll(ctx context.Context, entries []Entry, dir string, db store, up uploader, opt imageopt.Options, apply bool, out io.Writer) stats {
	var st stats
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.File))
		if err != nil {
			fmt.Fprintf(out, "ÉCHEC  %-40s lecture : %v\n", e.Slug, err)
			st.failed++
			continue
		}
		ct := r2.GetContentTypeFromExtension(e.File)
		if !r2.ValidateImageType(ct) {
			fmt.Fprintf(out, "ÉCHEC  %-40s format de fichier non accepté (jpg, png, webp)\n", e.Slug)
			st.failed++
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		res := imageopt.Optimize(data, ct, opt)
		key := r2.GenerateImageBankKey(e.Category, e.Slug, r2.GetExtensionFromContentType(res.ContentType))
		st.before += int64(len(data))
		st.after += int64(len(res.Data))

		var existing *imagebank.ExistingImport
		if db != nil {
			if existing, err = db.FindBySlug(ctx, e.Slug); err != nil {
				fmt.Fprintf(out, "ÉCHEC  %-40s %v\n", e.Slug, err)
				st.failed++
				continue
			}
		}
		fileChanged := existing == nil || existing.ContentHash != hash || existing.R2Key != key
		status := "NOUVEAU"
		switch {
		case existing == nil:
			st.created++
		case fileChanged:
			status = "FICHIER"
			st.updated++
		default:
			status = "MÉTA"
			st.unchanged++
		}
		fmt.Fprintf(out, "%-7s %-40s %4dx%-4d %7s → %7s %s\n", status, e.Slug, res.Width, res.Height,
			human(int64(len(data))), human(int64(len(res.Data))), key)
		if !apply {
			continue
		}

		publicURL := up.PublicURL(key)
		if fileChanged {
			if publicURL, err = up.UploadFile(ctx, key, bytes.NewReader(res.Data), res.ContentType); err != nil {
				fmt.Fprintf(out, "ÉCHEC  %-40s envoi R2 : %v\n", e.Slug, err)
				st.failed++
				continue
			}
		}
		err = db.Upsert(ctx, imagebank.ImportRow{
			Slug: e.Slug, Category: e.Category, Name: e.Name, Tags: e.Tags, Format: e.Format,
			ImageURL: publicURL, R2Key: key, ContentType: res.ContentType,
			Width: res.Width, Height: res.Height, SizeBytes: len(res.Data), ContentHash: hash,
			Source: e.Source, License: e.License, Author: e.Author, SourceURL: e.SourceURL,
			SortOrder: e.SortOrder, Enabled: e.Enabled,
		})
		if err != nil {
			fmt.Fprintf(out, "ÉCHEC  %-40s base : %v\n", e.Slug, err)
			st.failed++
		}
	}
	return st
}

func human(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f Mo", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d Ko", n/(1<<10))
	}
	return fmt.Sprintf("%d o", n)
}

func main() {
	manifest := flag.String("manifest", "", "manifeste CSV (obligatoire)")
	dir := flag.String("dir", "", "dossier des images (défaut : dossier du manifeste)")
	apply := flag.Bool("apply", false, "envoyer et enregistrer (défaut : simulation)")
	maxSide := flag.Int("max-side", imageopt.DefaultMaxSide, "plus grand côté conservé (px)")
	quality := flag.Int("quality", imageopt.DefaultJPEGQuality, "qualité JPEG (1-100)")
	flag.Parse()

	if *manifest == "" {
		log.Fatal("--manifest est obligatoire")
	}
	if *dir == "" {
		*dir = filepath.Dir(*manifest)
	}
	f, err := os.Open(*manifest)
	if err != nil {
		log.Fatalf("manifeste : %v", err)
	}
	entries, errs := ParseManifest(f)
	f.Close()
	if len(errs) > 0 {
		for _, e := range errs {
			log.Println(e)
		}
		log.Fatalf("%d erreur(s) dans le manifeste, rien n'a été fait", len(errs))
	}

	_ = os.Setenv(dbx.EnvDialect, "postgres")
	ctx := context.Background()

	var db store
	if pg := os.Getenv("POSTGRES_URL"); pg != "" {
		conn, err := sql.Open("pgx", pg)
		if err != nil {
			log.Fatalf("connexion à la base : %v", err)
		}
		defer conn.Close()
		db = imagebank.NewRepository(conn)
	} else if *apply {
		log.Fatal("POSTGRES_URL doit être défini avec --apply")
	}

	var up uploader
	if *apply {
		base := strings.TrimRight(os.Getenv("R2_PUBLIC_BASE_URL"), "/")
		if !strings.HasPrefix(base, "https://") {
			log.Fatal("R2_PUBLIC_BASE_URL doit commencer par https://")
		}
		client, err := r2.NewClient(r2.UploadConfig{
			AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
			Endpoint:        os.Getenv("R2_ENDPOINT"),
			Bucket:          os.Getenv("R2_BUCKET"),
			PublicBaseURL:   base,
		})
		if err != nil {
			log.Fatalf("client R2 : %v", err)
		}
		up = client
	} else {
		up = previewUploader{}
	}

	st := importAll(ctx, entries, *dir, db, up, imageopt.Options{MaxSide: *maxSide, JPEGQuality: *quality}, *apply, os.Stdout)

	mode := "SIMULATION (aucune écriture ; relancer avec --apply)"
	if *apply {
		mode = "APPLIQUÉ"
		if os.Getenv("REDIS_URL") != "" {
			if rc, err := redisclient.New(); err == nil {
				rc.Delete(ctx, imagebank.CacheKey)
			} else {
				log.Printf("cache Redis non vidé (%v) : la banque se mettra à jour sous 10 minutes", err)
			}
		}
	}
	fmt.Printf("\n%s — %d nouvelle(s), %d fichier(s) remplacé(s), %d métadonnées seules, %d échec(s)\n",
		mode, st.created, st.updated, st.unchanged, st.failed)
	if st.before > 0 {
		fmt.Printf("Poids : %s → %s (-%d %%)\n", human(st.before), human(st.after), 100-st.after*100/st.before)
	}
	if st.failed > 0 {
		os.Exit(1)
	}
}

type previewUploader struct{}

func (previewUploader) UploadFile(context.Context, string, io.Reader, string) (string, error) {
	return "", errors.New("simulation")
}
func (previewUploader) PublicURL(key string) string { return key }
