package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// Openverse (https://api.openverse.org) : moteur de recherche d'images sous
// licence libre (WordPress). Seules les licences CC0 et Public Domain Mark
// sont demandées : aucune attribution n'est exigée, l'usage commercial est
// libre.
//
// Sans identifiants, l'API limite à 20 requêtes/minute et 200/jour. Avec
// OPENVERSE_CLIENT_ID / OPENVERSE_CLIENT_SECRET (inscription gratuite :
// https://api.openverse.org/v1/#tag/auth), les limites sont bien plus hautes.
const openverseBase = "https://api.openverse.org/v1"

// Sources professionnelles (banques de photos CC0), essayées en premier.
var openverseProSources = []string{"stocksnap", "rawpixel", "wordpress"}

var sourceWeight = map[string]float64{
	"stocksnap": 3,
	"rawpixel":  2.5,
	"wordpress": 2.5,
	"flickr":    1,
}

// Mots qui désignent un dessin, un montage ou un objet sans rapport.
var openverseBadWords = []string{
	"illustration", "vector", "drawing", "sticker", "clipart", "clip art", "icon", "pattern",
	"mockup", "mock up", "cartoon", "logo", "psd", "template", "poster", "vintage", "botanical",
	"engraving", "painting", "collage", "design element", "watercolor", "sketch", "lithograph",
	"chromolithograph", "seamless", "background design", "flyer", "banner template",
	"menu template", "social media", "instagram",
}

type openverseResult struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	URL               string `json:"url"`
	Creator           string `json:"creator"`
	License           string `json:"license"`
	LicenseVersion    string `json:"license_version"`
	Source            string `json:"source"`
	ForeignLandingURL string `json:"foreign_landing_url"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Mature            bool   `json:"mature"`
	Tags              []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

type openverse struct {
	f       *fetcher
	token   string
	expires time.Time
	id      string
	secret  string
}

func newOpenverse(f *fetcher) *openverse {
	o := &openverse{f: f, id: os.Getenv("OPENVERSE_CLIENT_ID"), secret: os.Getenv("OPENVERSE_CLIENT_SECRET")}
	// 20 requêtes/minute en anonyme : une toutes les 3,2 s.
	f.minGap["openverse"] = 3200 * time.Millisecond
	if o.id != "" {
		f.minGap["openverse"] = 700 * time.Millisecond
	}
	return o
}

func (o *openverse) headers(ctx context.Context) (map[string]string, error) {
	if o.id == "" || o.secret == "" {
		return nil, nil
	}
	if o.token == "" || time.Now().After(o.expires) {
		form := url.Values{"grant_type": {"client_credentials"}, "client_id": {o.id}, "client_secret": {o.secret}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, openverseBase+"/auth_tokens/token/", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", userAgent)
		resp, err := o.f.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jeton Openverse : %w", err)
		}
		defer resp.Body.Close()
		var tok struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		}
		if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&tok) != nil || tok.AccessToken == "" {
			return nil, fmt.Errorf("jeton Openverse refusé (HTTP %d) : vérifier OPENVERSE_CLIENT_ID/SECRET", resp.StatusCode)
		}
		o.token = tok.AccessToken
		o.expires = time.Now().Add(time.Duration(tok.ExpiresIn-60) * time.Second)
	}
	return map[string]string{"Authorization": "Bearer " + o.token}, nil
}

// search renvoie les résultats bruts d'une recherche.
func (o *openverse) search(ctx context.Context, query string, extra url.Values) ([]openverseResult, error) {
	params := url.Values{
		"q":         {query},
		"license":   {"cc0,pdm"},
		"page_size": {"20"},
		"mature":    {"false"},
	}
	for k, v := range extra {
		params[k] = v
	}
	h, err := o.headers(ctx)
	if err != nil {
		return nil, err
	}
	body, err := o.f.get(ctx, "openverse", openverseBase+"/images/?"+params.Encode(), h, 4<<20)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []openverseResult `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("réponse Openverse illisible : %w", err)
	}
	return resp.Results, nil
}

// candidates : d'abord les banques professionnelles, puis toutes les photos
// si elles ne donnent rien d'utilisable.
func (o *openverse) candidates(ctx context.Context, it Item) ([]openverseResult, error) {
	strict := url.Values{"source": {strings.Join(openverseProSources, ",")}}
	loose := url.Values{"category": {"photograph"}, "size": {"large,medium"}}
	if it.Format == "banner" {
		loose.Set("aspect_ratio", "wide")
	}
	var all []openverseResult
	for _, extra := range []url.Values{strict, loose} {
		res, err := o.search(ctx, it.Query, extra)
		if err != nil {
			return nil, err
		}
		ranked := rankOpenverse(res, it)
		all = append(all, ranked...)
		if len(ranked) >= 3 {
			break
		}
	}
	return all, nil
}

// rankOpenverse filtre et classe les résultats : licence, taille, titre sans
// mot interdit, orientation adaptée au format, source professionnelle, mots
// de la recherche présents dans le titre ou les tags.
func rankOpenverse(results []openverseResult, it Item) []openverseResult {
	type scored struct {
		r openverseResult
		s float64
	}
	exclude := append(append([]string{}, openverseBadWords...), it.Exclude...)
	words := queryWords(it.Query)
	var out []scored
	for _, r := range results {
		if r.Mature || r.URL == "" || (r.License != "cc0" && r.License != "pdm") {
			continue
		}
		minSide := min(r.Width, r.Height)
		if r.Width > 0 && minSide < 600 {
			continue
		}
		title := " " + fold(r.Title) + " "
		if containsAny(title, exclude) {
			continue
		}
		ratio := 1.0
		if r.Height > 0 {
			ratio = float64(r.Width) / float64(r.Height)
		}
		if it.Format == "banner" && ratio < 1.3 {
			continue
		}
		s := sourceWeight[r.Source]
		if s == 0 {
			s = 0.5
		}
		hay := title
		for _, t := range r.Tags {
			hay += " " + fold(t.Name)
		}
		matched := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				matched++
			}
		}
		if len(words) > 0 {
			ratio := float64(matched) / float64(len(words))
			// Moins de la moitié des mots dans le titre ou les tags : la photo
			// parle d'autre chose (une pizza pour « basilic »).
			if ratio < 0.5 {
				continue
			}
			s += 3 * ratio
		}
		if it.Format == "square" && ratio >= 0.75 && ratio <= 1.6 {
			s += 0.5 // recadrage carré sans trop perdre
		}
		if minSide >= 1200 {
			s += 0.5
		}
		out = append(out, scored{r, s})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].s > out[j].s })
	ranked := make([]openverseResult, len(out))
	for i, o := range out {
		ranked[i] = o.r
	}
	return ranked
}

func openverseLicense(r openverseResult) string {
	switch r.License {
	case "cc0":
		return "CC0 " + r.LicenseVersion
	case "pdm":
		return "Public Domain Mark " + r.LicenseVersion
	}
	return r.License
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if w = fold(w); w != "" && strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// Mots de liaison ignorés dans le calcul de correspondance.
var queryStopWords = map[string]bool{"and": true, "of": true, "with": true, "the": true, "a": true, "on": true, "in": true}

func queryWords(q string) []string {
	var out []string
	for _, w := range strings.Fields(fold(q)) {
		if !queryStopWords[w] {
			out = append(out, w)
		}
	}
	return out
}
