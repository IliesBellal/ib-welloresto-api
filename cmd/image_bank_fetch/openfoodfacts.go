package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Open Food Facts (https://world.openfoodfacts.org) : base collaborative des
// produits alimentaires. Photos des emballages sous licence CC BY-SA 3.0
// (attribution « Contributeurs Open Food Facts », conservée en base). Les
// marques et logos restent la propriété de leurs fabricants.
//
// Limites : 100 lectures de produit/minute, 10 recherches/minute.
const offBase = "https://world.openfoodfacts.org"

type offProduct struct {
	Code          string `json:"code"`
	ProductName   string `json:"product_name"`
	ProductNameFR string `json:"product_name_fr"`
	Brands        string `json:"brands"`
	Quantity      string `json:"quantity"`
	FrontURL      string `json:"image_front_url"`
	Selected      struct {
		Front struct {
			Display map[string]string `json:"display"`
		} `json:"front"`
	} `json:"selected_images"`
}

// imageURLs : photos de face en pleine résolution, la française d'abord.
func (p offProduct) imageURLs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(u string) {
		if u == "" {
			return
		}
		u = offFullImageURL(u)
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	add(p.Selected.Front.Display["fr"])
	add(p.FrontURL)
	return out
}

type openFoodFacts struct{ f *fetcher }

func newOpenFoodFacts(f *fetcher) *openFoodFacts {
	f.minGap["off-search"] = 6500 * time.Millisecond
	f.minGap["off-product"] = 700 * time.Millisecond
	return &openFoodFacts{f: f}
}

const offFields = "code,product_name,product_name_fr,brands,quantity,image_front_url,selected_images"

// candidates : le produit du code-barres imposé, sinon les produits de la
// marque vendus en France, les plus scannés d'abord, dont le nom contient
// tous les mots de query et aucun mot exclu.
func (o *openFoodFacts) candidates(ctx context.Context, it Item) ([]offProduct, error) {
	if it.Barcode != "" {
		body, err := o.f.get(ctx, "off-product", offBase+"/api/v2/product/"+url.PathEscape(it.Barcode)+".json?fields="+offFields, nil, 2<<20)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Status  int        `json:"status"`
			Product offProduct `json:"product"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || resp.Status != 1 {
			return nil, fmt.Errorf("code-barres %s inconnu d'Open Food Facts", it.Barcode)
		}
		if resp.Product.FrontURL == "" {
			return nil, nil
		}
		return []offProduct{resp.Product}, nil
	}

	params := url.Values{
		"brands_tags":       {it.Brand},
		"countries_tags_en": {"france"},
		"sort_by":           {"unique_scans_n"},
		"page_size":         {"50"},
		"fields":            {offFields},
	}
	body, err := o.f.get(ctx, "off-search", offBase+"/api/v2/search?"+params.Encode(), nil, 8<<20)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Products []offProduct `json:"products"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("réponse Open Food Facts illisible : %w", err)
	}
	return filterOFF(resp.Products, it), nil
}

// filterOFF garde les produits avec photo de face dont le nom (et la
// quantité) contient tous les mots de query (« a/b » : a ou b) et aucun mot
// exclu.
func filterOFF(products []offProduct, it Item) []offProduct {
	words := strings.Fields(fold(it.Query))
	var out []offProduct
	for _, p := range products {
		if p.FrontURL == "" {
			continue
		}
		name := p.ProductNameFR
		if name == "" {
			name = p.ProductName
		}
		hay := " " + fold(name+" "+p.Quantity) + " "
		ok := true
		for _, w := range words {
			// « zero/sucres » : l'un ou l'autre.
			any := false
			for _, alt := range strings.Split(w, "/") {
				if alt != "" && strings.Contains(hay, alt) {
					any = true
					break
				}
			}
			if !any {
				ok = false
				break
			}
		}
		if ok && !containsAny(hay, it.Exclude) && !containsAny(hay, offMultipackWords) {
			out = append(out, p)
		}
	}
	return out
}

// offMultipackWords : les lots photographiés en carton ne font pas une
// vignette de carte.
var offMultipackWords = []string{" pack", "lot de", " x ", " * ", "multipack", "carton"}

var offSizeRe = regexp.MustCompile(`\.(100|200|400)\.jpg$`)

// offFullImageURL : la photo de face en pleine résolution (l'API renvoie la
// version 400 px).
func offFullImageURL(front string) string {
	return offSizeRe.ReplaceAllString(front, ".full.jpg")
}

func offProductURL(code string) string { return offBase + "/product/" + code }
