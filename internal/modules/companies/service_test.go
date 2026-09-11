package companies

import (
	"context"
	"errors"
	"testing"
)

// fakeSireneClient lets tests control exactly what the external API
// "returns" without a live network call.
type fakeSireneClient struct {
	results []sireneResult
	err     error
}

func (f *fakeSireneClient) Search(ctx context.Context, name, postalCode string, nafCodes []string) ([]sireneResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func mkResult(siret, siren, name, naf, postal, etat string) sireneResult {
	r := sireneResult{SIREN: siren, SIRET: siret, NomComplet: name, ActivitePrincipale: naf, EtatAdministratif: etat}
	r.Siege.CodePostal = postal
	return r
}

func TestResolve_HighConfidence_SingleCandidate(t *testing.T) {
	client := &fakeSireneClient{results: []sireneResult{
		mkResult("81490975000014", "814909750", "Le Maghreb", "56.10A", "75001", "A"),
	}}
	svc := NewService(client, nil)

	resp, err := svc.Resolve(context.Background(), "203.0.113.1", ResolveRequest{Name: "Le Maghreb", PostalCode: "75001", City: "Paris"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(resp.Candidates))
	}
	if !resp.Candidates[0].HighConfidence {
		t.Fatalf("candidate = %+v, want HighConfidence=true (exact name + postal match)", resp.Candidates[0])
	}
	if resp.Candidates[0].SIRET != "81490975000014" {
		t.Fatalf("siret = %q, want %q", resp.Candidates[0].SIRET, "81490975000014")
	}
}

func TestResolve_LowScore_EmptyList(t *testing.T) {
	client := &fakeSireneClient{results: []sireneResult{
		mkResult("11111111100014", "111111111", "Complètement Différent SARL", "56.10A", "13001", "A"),
	}}
	svc := NewService(client, nil)

	resp, err := svc.Resolve(context.Background(), "203.0.113.2", ResolveRequest{Name: "Le Maghreb", PostalCode: "75001", City: "Paris"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %v, want none (score below 0.5)", resp.Candidates)
	}
}

func TestResolve_MidScore_MultipleCandidates(t *testing.T) {
	// Same name, wrong postal code (postal mismatch drags every candidate
	// below the high-confidence bar but name similarity alone still clears
	// the 0.5 floor for both).
	client := &fakeSireneClient{results: []sireneResult{
		mkResult("11111111100014", "111111111", "Ok Pizza", "56.10C", "13001", "A"),
		mkResult("22222222200014", "222222222", "Ok Pizza Express", "56.10C", "13002", "A"),
	}}
	svc := NewService(client, nil)

	resp, err := svc.Resolve(context.Background(), "203.0.113.3", ResolveRequest{Name: "Ok Pizza", PostalCode: "75001", City: "Paris"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resp.Candidates) < 1 || len(resp.Candidates) > 3 {
		t.Fatalf("candidates = %d, want 1-3", len(resp.Candidates))
	}
	if resp.Candidates[0].HighConfidence {
		t.Fatalf("candidate = %+v, want HighConfidence=false (postal code did not match)", resp.Candidates[0])
	}
	// Best match (exact name) must rank first.
	if resp.Candidates[0].CompanyName != "Ok Pizza" {
		t.Fatalf("top candidate = %q, want %q first", resp.Candidates[0].CompanyName, "Ok Pizza")
	}
}

func TestResolve_ThirdPartyFailure_ReturnsEmptyNotError(t *testing.T) {
	client := &fakeSireneClient{err: errors.New("boom: connection refused")}
	svc := NewService(client, nil)

	resp, err := svc.Resolve(context.Background(), "203.0.113.4", ResolveRequest{Name: "Le Maghreb", PostalCode: "75001", City: "Paris"})
	if err != nil {
		t.Fatalf("Resolve: expected nil error even on third-party failure, got %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %v, want none", resp.Candidates)
	}
}

func TestResolve_InvalidInput(t *testing.T) {
	svc := NewService(&fakeSireneClient{}, nil)
	if _, err := svc.Resolve(context.Background(), "203.0.113.5", ResolveRequest{Name: "", PostalCode: "75001", City: "Paris"}); err == nil {
		t.Fatal("Resolve with empty name: expected an error, got nil")
	}
}

func TestResolve_NoUsableSIRET_SkipsCandidate(t *testing.T) {
	// A result with no siret anywhere (siege empty, no matching
	// établissements) must never surface — it's useless for pre-fill.
	client := &fakeSireneClient{results: []sireneResult{
		{SIREN: "999999999", NomComplet: "Le Maghreb", ActivitePrincipale: "56.10A", EtatAdministratif: "A"},
	}}
	svc := NewService(client, nil)

	resp, err := svc.Resolve(context.Background(), "203.0.113.6", ResolveRequest{Name: "Le Maghreb", PostalCode: "75001", City: "Paris"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %v, want none (no SIRET available)", resp.Candidates)
	}
}

func TestNameSimilarity(t *testing.T) {
	if got := nameSimilarity("Le Maghreb", "LE MAGHREB"); got != 1 {
		t.Fatalf("nameSimilarity(case-insensitive exact) = %v, want 1", got)
	}
	if got := nameSimilarity("Café Le Maghreb", "Cafe Le Maghreb"); got != 1 {
		t.Fatalf("nameSimilarity(diacritic-insensitive) = %v, want 1", got)
	}
	if got := nameSimilarity("Le Maghreb", "Ok Pizza"); got > 0.3 {
		t.Fatalf("nameSimilarity(unrelated names) = %v, want low", got)
	}
}
