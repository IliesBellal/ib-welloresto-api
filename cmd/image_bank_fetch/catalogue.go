package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"welloresto-api/internal/modules/imagebank"
)

// Fournisseurs d'images.
const (
	providerOpenverse = "openverse"     // photos CC0 / domaine public (Openverse)
	providerOFF       = "openfoodfacts" // produits de marque (Open Food Facts)
)

// Item : ligne du catalogue (docs/banque-images/catalogue.csv).
//
//	query    : recherche (anglais pour Openverse ; pour Open Food Facts, mots
//	           requis dans le nom du produit, séparés par des espaces, « a/b »
//	           pour l'un ou l'autre)
//	brand    : marque Open Food Facts (étiquette, ex. coca-cola)
//	exclude  : mots interdits dans le titre / nom du produit (séparés par |)
//	barcode  : code-barres imposé (Open Food Facts), prioritaire sur la recherche
type Item struct {
	Line     int
	Slug     string
	Category string
	Name     string
	Tags     []string
	Format   string
	Provider string
	Query    string
	Brand    string
	Exclude  []string
	Barcode  string
	Priority string
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var catalogueColumns = []string{"slug", "category", "name", "tags", "format", "provider", "query"}

// ParseCatalogue lit et valide le catalogue.
func ParseCatalogue(r io.Reader) ([]Item, []error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, []error{fmt.Errorf("CSV illisible : %w", err)}
	}
	if len(records) == 0 {
		return nil, []error{errors.New("catalogue vide")}
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	var errs []error
	for _, c := range catalogueColumns {
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
	split := func(s string) []string {
		var out []string
		for _, t := range strings.Split(s, "|") {
			if t = strings.TrimSpace(t); t != "" {
				out = append(out, t)
			}
		}
		return out
	}

	var items []Item
	seen := map[string]int{}
	for n, rec := range records[1:] {
		line := n + 2
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		it := Item{
			Line: line, Slug: get(rec, "slug"), Category: get(rec, "category"), Name: get(rec, "name"),
			Tags: split(get(rec, "tags")), Format: get(rec, "format"), Provider: get(rec, "provider"),
			Query: get(rec, "query"), Brand: get(rec, "brand"), Exclude: split(get(rec, "exclude")),
			Barcode: get(rec, "barcode"), Priority: get(rec, "priority"),
		}
		switch {
		case !slugRe.MatchString(it.Slug):
			errs = append(errs, fmt.Errorf("ligne %d : slug invalide %q", line, it.Slug))
		case seen[it.Slug] != 0:
			errs = append(errs, fmt.Errorf("ligne %d : slug %q déjà utilisé ligne %d", line, it.Slug, seen[it.Slug]))
		}
		seen[it.Slug] = line
		if !imagebank.IsCategory(it.Category) {
			errs = append(errs, fmt.Errorf("ligne %d : catégorie inconnue %q", line, it.Category))
		}
		if it.Name == "" {
			errs = append(errs, fmt.Errorf("ligne %d : name vide", line))
		}
		if !imagebank.IsFormat(it.Format) {
			errs = append(errs, fmt.Errorf("ligne %d : format %q (square ou banner)", line, it.Format))
		}
		switch it.Provider {
		case providerOpenverse:
			if it.Query == "" {
				errs = append(errs, fmt.Errorf("ligne %d : query obligatoire pour openverse", line))
			}
		case providerOFF:
			if it.Barcode == "" && it.Brand == "" {
				errs = append(errs, fmt.Errorf("ligne %d : brand ou barcode obligatoire pour openfoodfacts", line))
			}
		default:
			errs = append(errs, fmt.Errorf("ligne %d : provider %q (openverse ou openfoodfacts)", line, it.Provider))
		}
		items = append(items, it)
	}
	return items, errs
}
