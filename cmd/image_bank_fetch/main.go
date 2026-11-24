// Command image_bank_fetch constitue automatiquement les images de la banque
// Wello Resto à partir du catalogue docs/banque-images/catalogue.csv
// (docs/banque-images.md) :
//
//   - provider openverse : photo CC0 / domaine public trouvée sur Openverse
//     (plats, ingrédients, boissons génériques, bannières) ;
//   - provider openfoodfacts : photo de face d'un produit de marque
//     (Coca-Cola, Dada, Evian…) sur Open Food Facts, licence CC BY-SA 3.0.
//
// Les images sont cadrées (carré ou bannière), enregistrées dans --out, et
// un manifeste --out/manifest.csv est écrit au format de
// cmd/image_bank_import, provenance et licence remplies. Le dossier peut
// être relu avant l'import ; supprimer une image (et sa ligne) puis relancer
// en cherche une autre (la photo supprimée est notée dans rejetees.txt et
// n'est plus proposée). Une relance reprend là où la précédente s'est
// arrêtée ; les éléments introuvables sont listés dans --out/non-trouves.csv.
//
// Usage :
//
//	go run ./cmd/image_bank_fetch --out ./banque-images [--priority P1] [--only slug1,slug2] [--limit 20]
//	go run ./cmd/image_bank_import --manifest ./banque-images/manifest.csv [--apply]
//
// Variables facultatives : OPENVERSE_CLIENT_ID / OPENVERSE_CLIENT_SECRET
// (sans elles, Openverse limite à 200 recherches par jour).
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var manifestHeader = []string{"slug", "file", "category", "name", "tags", "format", "source", "license", "author", "source_url", "sort_order", "enabled"}

// maxCandidates : photos Openverse essayées par élément ; maxOFFProducts :
// produits Open Food Facts comparés.
const (
	maxCandidates  = 6
	maxOFFProducts = 15
)

// Picked : image retenue pour un élément.
type Picked struct {
	Data                               []byte
	Ext                                string
	Width, Height                      int
	Source, License, Author, SourceURL string
	ID                                 string
	// Score : part de fond neutre (photos de produits uniquement).
	Score float64
}

type finder interface {
	find(ctx context.Context, it Item, used map[string]bool) (*Picked, error)
}

type sources struct {
	f   *fetcher
	ov  *openverse
	off *openFoodFacts
	// offMinBackground : part minimale de fond neutre (0 à 1) d'une photo de
	// produit pour qu'elle soit retenue.
	offMinBackground float64
}

var errNotFound = errors.New("aucune image convenable")

func (s *sources) find(ctx context.Context, it Item, used map[string]bool) (*Picked, error) {
	switch it.Provider {
	case providerOpenverse:
		cands, err := s.ov.candidates(ctx, it)
		if err != nil {
			return nil, err
		}
		tried := 0
		var lastErr error
		for _, c := range cands {
			if used[c.ID] || used[c.ForeignLandingURL] || tried >= maxCandidates {
				continue
			}
			tried++
			data, err := s.f.get(ctx, "image", c.URL, nil, 40<<20)
			if err != nil {
				lastErr = fmt.Errorf("téléchargement de %s : %w", c.URL, err)
				continue
			}
			out, ext, w, h, err := frame(data, it.Format, false)
			if err != nil {
				lastErr = fmt.Errorf("image %s : %w", c.URL, err)
				continue
			}
			author := c.Creator
			if author == "" {
				author = "inconnu"
			}
			return &Picked{Data: out, Ext: ext, Width: w, Height: h, ID: c.ID,
				Source: "openverse:" + c.Source, License: openverseLicense(c), Author: author, SourceURL: c.ForeignLandingURL}, nil
		}
		if lastErr != nil {
			return nil, fmt.Errorf("%w (%d candidat(s) ; dernière erreur : %v)", errNotFound, tried, lastErr)
		}
		return nil, fmt.Errorf("%w (%d résultat(s) Openverse retenu(s))", errNotFound, len(cands))
	case providerOFF:
		cands, err := s.off.candidates(ctx, it)
		if err != nil {
			return nil, err
		}
		// On compare plusieurs produits et on garde la photo au fond le plus
		// neutre (packshot détouré plutôt que photo en rayon ou en main).
		var best *Picked
		bestScore := -1.0
		tried := 0
		for _, p := range cands {
			if used[offProductURL(p.Code)] || tried >= maxOFFProducts {
				continue
			}
			tried++
			for _, u := range p.imageURLs() {
				data, err := s.f.get(ctx, "image", u, nil, 40<<20)
				if err != nil {
					continue
				}
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					continue
				}
				score := backgroundScore(img)
				if score <= bestScore {
					continue
				}
				out, ext, w, h, err := frame(data, it.Format, it.Format == "square")
				if err != nil {
					continue
				}
				bestScore = score
				best = &Picked{Data: out, Ext: ext, Width: w, Height: h, ID: p.Code, Score: score,
					Source: "openfoodfacts", License: "CC BY-SA 3.0", Author: "Contributeurs Open Food Facts", SourceURL: offProductURL(p.Code)}
			}
			if bestScore >= 0.95 {
				break
			}
		}
		if best == nil {
			return nil, errNotFound
		}
		if bestScore < s.offMinBackground {
			return nil, fmt.Errorf("pas de photo détourée (meilleur fond neutre %.0f %%) : à demander au fournisseur", bestScore*100)
		}
		return best, nil
	}
	return nil, fmt.Errorf("provider inconnu %q", it.Provider)
}

// manifestState : lignes déjà présentes dans le manifeste dont le fichier
// existe encore (reprise), et sources déjà utilisées (pas deux fois la même
// photo).
func manifestState(path, dir string) (done map[string]bool, used map[string]bool, err error) {
	done, used = map[string]bool{}, map[string]bool{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return done, used, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("manifeste existant illisible : %w", err)
	}
	for i, rec := range records {
		if i == 0 || len(rec) < len(manifestHeader) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, rec[1])); err != nil {
			continue // fichier supprimé à la relecture : on en cherchera un autre
		}
		done[rec[0]] = true
		used[rec[9]] = true
	}
	return done, used, nil
}

// rejectedFile : sources des images supprimées à la relecture, jamais
// reproposées.
const rejectedFile = "rejetees.txt"

func loadRejected(dir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, rejectedFile))
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[l] = true
		}
	}
	return out
}

// rewriteManifest ne garde que les lignes dont le fichier existe encore ; la
// source des autres est ajoutée à rejetees.txt.
func rewriteManifest(path, dir string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	records, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil {
		return err
	}
	kept := [][]string{manifestHeader}
	var rejected []string
	for i, rec := range records {
		if i == 0 || len(rec) < len(manifestHeader) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, rec[1])); err == nil {
			kept = append(kept, rec)
		} else if rec[9] != "" {
			rejected = append(rejected, rec[9])
		}
	}
	if len(rejected) > 0 {
		f, err := os.OpenFile(filepath.Join(dir, rejectedFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, err = f.WriteString(strings.Join(rejected, "\n") + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return writeCSV(path, kept)
}

func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type options struct {
	out      string
	only     map[string]bool
	priority map[string]bool
	limit    int
}

func selected(it Item, o options) bool {
	if len(o.only) > 0 && !o.only[it.Slug] {
		return false
	}
	if len(o.priority) > 0 && !o.priority[strings.ToUpper(it.Priority)] {
		return false
	}
	return true
}

// run traite le catalogue ; renvoie le nombre d'images ajoutées et les
// éléments introuvables.
func run(ctx context.Context, items []Item, src finder, o options, log io.Writer) (int, [][]string, error) {
	manifestPath := filepath.Join(o.out, "manifest.csv")
	if err := rewriteManifest(manifestPath, o.out); err != nil {
		return 0, nil, err
	}
	done, used, err := manifestState(manifestPath, o.out)
	if err != nil {
		return 0, nil, err
	}
	for src := range loadRejected(o.out) {
		used[src] = true
	}
	_, statErr := os.Stat(manifestPath)
	mf, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, nil, err
	}
	defer mf.Close()
	mw := csv.NewWriter(mf)
	if errors.Is(statErr, os.ErrNotExist) {
		_ = mw.Write(manifestHeader)
		mw.Flush()
	}

	order := map[string]int{}
	added := 0
	var missing [][]string
	for _, it := range items {
		order[it.Category] += 10
		if !selected(it, o) || done[it.Slug] {
			continue
		}
		if o.limit > 0 && added >= o.limit {
			break
		}
		p, err := src.find(ctx, it, used)
		if err != nil {
			fmt.Fprintf(log, "MANQUE  %-40s %v\n", it.Slug, err)
			missing = append(missing, []string{it.Slug, it.Name, it.Provider, it.Query + " " + it.Brand + " " + it.Barcode, err.Error()})
			continue
		}
		rel := filepath.ToSlash(filepath.Join(it.Category, it.Slug+p.Ext))
		if err := os.MkdirAll(filepath.Join(o.out, it.Category), 0o755); err != nil {
			return added, missing, err
		}
		if err := os.WriteFile(filepath.Join(o.out, rel), p.Data, 0o644); err != nil {
			return added, missing, err
		}
		if err := mw.Write([]string{it.Slug, rel, it.Category, it.Name, strings.Join(it.Tags, "|"), it.Format,
			p.Source, p.License, p.Author, p.SourceURL, strconv.Itoa(order[it.Category]), "true"}); err != nil {
			return added, missing, err
		}
		mw.Flush()
		used[p.SourceURL] = true
		used[p.ID] = true
		added++
		note := ""
		if p.Source == "openfoodfacts" {
			note = fmt.Sprintf(" fond neutre %.0f %%", p.Score*100)
		}
		fmt.Fprintf(log, "OK      %-40s %4dx%-4d %s (%s)%s\n", it.Slug, p.Width, p.Height, p.Source, p.License, note)
	}
	return added, missing, mw.Error()
}

func splitSet(s string, upper bool) map[string]bool {
	out := map[string]bool{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			if upper {
				v = strings.ToUpper(v)
			}
			out[v] = true
		}
	}
	return out
}

func main() {
	catalogue := flag.String("catalogue", "docs/banque-images/catalogue.csv", "catalogue CSV")
	out := flag.String("out", "", "dossier de sortie (images + manifest.csv) — obligatoire")
	only := flag.String("only", "", "slugs à traiter, séparés par des virgules")
	priority := flag.String("priority", "", "priorités à traiter (ex. P1 ou P1,P2)")
	limit := flag.Int("limit", 0, "nombre maximal d'images à ajouter (0 = toutes)")
	offMin := flag.Float64("off-min-background", 0.6, "produits de marque : part minimale de fond neutre (0 = accepter toute photo)")
	flag.Parse()
	if *out == "" {
		log.Fatal("--out est obligatoire")
	}

	f, err := os.Open(*catalogue)
	if err != nil {
		log.Fatalf("catalogue : %v", err)
	}
	items, errs := ParseCatalogue(f)
	f.Close()
	if len(errs) > 0 {
		for _, e := range errs {
			log.Println(e)
		}
		log.Fatalf("%d erreur(s) dans le catalogue", len(errs))
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	fe := newFetcher()
	src := &sources{f: fe, ov: newOpenverse(fe), off: newOpenFoodFacts(fe), offMinBackground: *offMin}
	opts := options{out: *out, only: splitSet(*only, false), priority: splitSet(*priority, true), limit: *limit}

	start := time.Now()
	added, missing, err := run(context.Background(), items, src, opts, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	missingPath := filepath.Join(*out, "non-trouves.csv")
	_ = writeCSV(missingPath, append([][]string{{"slug", "name", "provider", "recherche", "raison"}}, missing...))

	fmt.Printf("\n%d image(s) ajoutée(s), %d introuvable(s) (%s) en %s.\n", added, len(missing), missingPath, time.Since(start).Round(time.Second))
	fmt.Printf("Relire les images dans %s, puis :\n  go run ./cmd/image_bank_import --manifest %s\n",
		*out, filepath.Join(*out, "manifest.csv"))
}
