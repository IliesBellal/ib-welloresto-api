package companies

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sireneBaseURL is recherche-entreprises.api.gouv.fr's search endpoint —
// free, no authentication, confirmed live 2026-09-11 (fetched its OpenAPI
// spec directly). Field names below are taken from that spec, not guessed:
// activite_principale (dotted NAF, e.g. "56.10A", comma-separated for a
// list), code_postal, etat_administratif ("A"/"C"), and a per-result
// top-level "siret" that the API promotes from the matching établissement
// once code_postal narrows the search — siege.siret is the fallback for
// when no établissement-level match won that promotion.
const sireneBaseURL = "https://recherche-entreprises.api.gouv.fr/search"

const sireneRequestTimeout = 6 * time.Second

// sireneResult mirrors the subset of one /search result this chantier reads.
type sireneResult struct {
	SIREN              string `json:"siren"`
	SIRET              string `json:"siret"`
	NomComplet         string `json:"nom_complet"`
	NatureJuridique    string `json:"nature_juridique"`
	ActivitePrincipale string `json:"activite_principale"`
	EtatAdministratif  string `json:"etat_administratif"`
	Siege              struct {
		SIRET      string `json:"siret"`
		CodePostal string `json:"code_postal"`
		Adresse    string `json:"adresse"`
	} `json:"siege"`
	MatchingEtablissements []struct {
		SIRET      string `json:"siret"`
		CodePostal string `json:"code_postal"`
		Adresse    string `json:"adresse"`
	} `json:"matching_etablissements"`
}

// resolvedEstablishment picks which physical location this candidate
// represents. matching_etablissements is the API's own answer to "which of
// this company's establishments actually matched code_postal" — confirmed
// live (2026-09-11) it can differ from siege (the registered head office),
// e.g. a chain whose siège is elsewhere but has a branch at the requested
// address. Preferring an exact postal match there over siege makes the
// address/SIRET returned to the caller the one actually near
// req.PostalCode, not just whichever the company's headquarters happens to be.
func (r sireneResult) resolvedEstablishment(reqPostalCode string) (siret, postalCode, address string) {
	for _, e := range r.MatchingEtablissements {
		if e.SIRET != "" && e.CodePostal == reqPostalCode {
			return e.SIRET, e.CodePostal, e.Adresse
		}
	}
	if r.SIRET != "" {
		return r.SIRET, r.Siege.CodePostal, r.Siege.Adresse
	}
	if r.Siege.SIRET != "" {
		return r.Siege.SIRET, r.Siege.CodePostal, r.Siege.Adresse
	}
	for _, e := range r.MatchingEtablissements {
		if e.SIRET != "" {
			return e.SIRET, e.CodePostal, e.Adresse
		}
	}
	return "", "", ""
}

type sireneSearchResponse struct {
	Results []sireneResult `json:"results"`
}

// SireneClient is the outbound dependency Service.Resolve calls — an
// interface so tests can substitute a fake instead of hitting the real
// third party.
type SireneClient interface {
	Search(ctx context.Context, name, postalCode string, nafCodes []string) ([]sireneResult, error)
}

type httpSireneClient struct {
	httpClient *http.Client
}

func NewSireneClient() SireneClient {
	return &httpSireneClient{httpClient: &http.Client{Timeout: sireneRequestTimeout}}
}

func (c *httpSireneClient) Search(ctx context.Context, name, postalCode string, nafCodes []string) ([]sireneResult, error) {
	q := url.Values{}
	q.Set("q", name)
	if postalCode != "" {
		q.Set("code_postal", postalCode)
	}
	q.Set("activite_principale", strings.Join(nafCodes, ","))
	q.Set("etat_administratif", "A")
	q.Set("per_page", "10")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sireneBaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("recherche-entreprises: unexpected status %d", resp.StatusCode)
	}

	var body sireneSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Results, nil
}
