package companies

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	redisclient "welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

const (
	// resolveIPThrottlePrefix/Max/Window: this is the tunnel's only public
	// route that calls a third party (per the chantier's own note) — a
	// tighter throttle than signup-context's 30/hour, since every call here
	// costs a request against someone else's free API.
	resolveIPThrottlePrefix = "companiesresolve:ipthrottle:"
	resolveIPThrottleMax    = 20
	resolveIPThrottleWindow = time.Hour

	resolveCacheTTL = 7 * 24 * time.Hour

	// Thresholds are the chantier's own (§5.4.2), not an assumption.
	highConfidenceThreshold = 0.85
	candidateThreshold      = 0.5
	maxCandidates           = 3
)

type Service struct {
	sirene SireneClient
	redis  *redisclient.Client
}

func NewService(sirene SireneClient, redis *redisclient.Client) *Service {
	return &Service{sirene: sirene, redis: redis}
}

// Resolve handles POST /v1/public/companies/resolve. It NEVER returns an
// error for a third-party failure or timeout — per the chantier's explicit
// instruction, an unavailable recherche-entreprises.api.gouv.fr must fall
// back to an empty candidate list (the tunnel then switches to manual
// entry), not a 5xx that would look like this API is broken. The only
// errors this returns are genuine caller mistakes (missing input) or the
// IP throttle.
func (s *Service) Resolve(ctx context.Context, clientIP string, req ResolveRequest) (ResolveResponse, error) {
	name := strings.TrimSpace(req.Name)
	postalCode := strings.TrimSpace(req.PostalCode)
	city := strings.TrimSpace(req.City)
	if name == "" || postalCode == "" || city == "" {
		return ResolveResponse{}, models.ErrCompanyResolveInvalidInput
	}

	if s.redis.TooManyRequestsFromIP(ctx, resolveIPThrottlePrefix, clientIP, resolveIPThrottleMax, resolveIPThrottleWindow) {
		return ResolveResponse{}, models.ErrRateLimited
	}

	cacheKey := "companies:resolve:" + normalizeName(name) + ":" + postalCode
	if raw, found := s.redis.Get(ctx, cacheKey); found {
		var cached ResolveResponse
		if err := json.Unmarshal([]byte(raw), &cached); err == nil {
			return cached, nil
		}
	}

	results, err := s.sirene.Search(ctx, name, postalCode, RestaurationNAFCodes)
	if err != nil {
		logger.FromContext(ctx).Warn("companies: recherche-entreprises call failed — falling back to manual entry",
			zap.Error(err))
		return ResolveResponse{Candidates: []Candidate{}}, nil
	}

	resp := ResolveResponse{Candidates: scoreAndRank(name, postalCode, results)}

	if encoded, err := json.Marshal(resp); err == nil {
		s.redis.Set(ctx, cacheKey, string(encoded), resolveCacheTTL)
	}

	return resp, nil
}

// scoreAndRank is §5.4.2's algorithm — deterministic, no AI. score =
// 0.75×name_similarity + 0.20×postal_code_exactness + 0.05×uniqueness_bonus
// (uniqueness_bonus applies only when exactly one raw result came back).
// Weights are this chantier's own choice — the brief specifies the
// thresholds exactly but leaves the weighting to implementation; not a
// figure taken from §5.4.2 itself.
func scoreAndRank(reqName, reqPostalCode string, results []sireneResult) []Candidate {
	if len(results) == 0 {
		return []Candidate{}
	}

	uniquenessBonus := 0.0
	if len(results) == 1 {
		uniquenessBonus = 0.05
	}

	candidates := make([]Candidate, 0, len(results))
	for _, r := range results {
		siret, postalCode, address := r.resolvedEstablishment(reqPostalCode)
		if siret == "" {
			continue // no usable establishment identifier — not a candidate
		}
		nameSim := nameSimilarity(reqName, r.NomComplet)
		postalMatch := 0.0
		if postalCode == reqPostalCode {
			postalMatch = 1.0
		}
		score := 0.75*nameSim + 0.20*postalMatch + uniquenessBonus
		if score > 1 {
			score = 1
		}
		candidates = append(candidates, Candidate{
			SIRET:       siret,
			SIREN:       r.SIREN,
			CompanyName: r.NomComplet,
			LegalForm:   r.NatureJuridique,
			NAF:         r.ActivitePrincipale,
			Address:     address,
			Score:       score,
			IsActive:    r.EtatAdministratif == "A",
		})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })

	if len(candidates) == 0 || candidates[0].Score < candidateThreshold {
		return []Candidate{}
	}
	if candidates[0].Score >= highConfidenceThreshold {
		candidates[0].HighConfidence = true
		return candidates[:1]
	}

	// 0.5 <= top score < 0.85: two or three candidates, whichever of those
	// clear the 0.5 floor.
	n := 0
	for n < len(candidates) && n < maxCandidates && candidates[n].Score >= candidateThreshold {
		n++
	}
	return candidates[:n]
}
