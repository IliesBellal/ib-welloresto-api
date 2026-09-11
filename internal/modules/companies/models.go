package companies

// RestaurationNAFCodes are the NAF/APE codes chantier 12 (§5.4.2) restricts
// candidates to — a company outside these codes is not a restaurant and
// should never surface as a plausible signup match.
var RestaurationNAFCodes = []string{
	"56.10A", "56.10C", "56.30Z", "10.71C", "10.71D", "47.81Z", "47.24Z",
}

// ResolveRequest is POST /v1/public/companies/resolve's JSON payload.
type ResolveRequest struct {
	Name       string `json:"name"`
	PostalCode string `json:"postal_code"`
	City       string `json:"city"`
}

// Candidate is one entry in ResolveResponse.Candidates.
type Candidate struct {
	SIRET          string  `json:"siret"`
	SIREN          string  `json:"siren"`
	CompanyName    string  `json:"company_name"`
	LegalForm      string  `json:"legal_form"`
	NAF            string  `json:"naf"`
	Address        string  `json:"address"`
	Score          float64 `json:"score"`
	IsActive       bool    `json:"is_active"`
	HighConfidence bool    `json:"high_confidence,omitempty"`
}

// ResolveResponse is always HTTP 200 — see Service.Resolve's doc comment on
// why a third-party failure must never surface as an error to the caller.
type ResolveResponse struct {
	Candidates []Candidate `json:"candidates"`
}
