package importer

import (
	"errors"
	"io"
	"testing"
)

type stubProvider struct{ slug string }

func (p *stubProvider) Slug() string { return p.slug }

func (p *stubProvider) Parse(io.Reader) (*IntermediateCustomerImport, error) {
	return &IntermediateCustomerImport{}, nil
}

func TestRegistryRegistersAndResolvesProvider(t *testing.T) {
	stub := &stubProvider{slug: ZeltySlug}
	registry := NewRegistry(stub)

	provider, err := registry.Get(ZeltySlug)
	if err != nil {
		t.Fatalf("Get(%q): %v", ZeltySlug, err)
	}
	if provider != CustomerImportProvider(stub) {
		t.Fatal("Get() ne rend pas le provider enregistré")
	}
	if got, want := registry.Slugs(), []string{ZeltySlug}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Slugs() = %v, want %v", got, want)
	}
}

// Un slug en double écrase le précédent, l'ordre d'enregistrement fait foi :
// même contrat que le registre de l'import de produits.
func TestNewRegistryOverridesBySlug(t *testing.T) {
	first := &stubProvider{slug: ManualSlug}
	second := &stubProvider{slug: ManualSlug}

	registry := NewRegistry(first, second)

	provider, err := registry.Get(ManualSlug)
	if err != nil {
		t.Fatalf("Get(%q): %v", ManualSlug, err)
	}
	if provider != CustomerImportProvider(second) {
		t.Fatal("le dernier provider enregistré ne l'emporte pas")
	}
	if len(registry.Slugs()) != 1 {
		t.Fatalf("Slugs() = %v, want une seule entrée", registry.Slugs())
	}
}

func TestRegistryGetUnknownProvider(t *testing.T) {
	_, err := DefaultRegistry().Get("zelty-v2")
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("Get(inconnu) = %v, want ErrUnknownProvider", err)
	}
	if _, err := DefaultRegistry().Get(""); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("Get(\"\") = %v, want ErrUnknownProvider", err)
	}
}

// DefaultRegistry porte les deux providers à flux (zelty, wello-generic).
// La saisie manuelle n'y figure pas : elle est appelée directement, hors
// registre (voir BuildManualCustomerImport).
func TestDefaultRegistryHasFileProviders(t *testing.T) {
	want := []string{WelloGenericSlug, ZeltySlug}
	got := DefaultRegistry().Slugs()
	if len(got) != len(want) {
		t.Fatalf("Slugs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Slugs()[%d] = %q, want %q (tri alphabetique)", i, got[i], want[i])
		}
	}
}
