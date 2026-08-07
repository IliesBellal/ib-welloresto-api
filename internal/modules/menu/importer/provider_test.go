package importer

import (
	"errors"
	"io"
	"testing"
)

func TestDefaultRegistry(t *testing.T) {
	registry := DefaultRegistry()

	wantSlugs := []string{WelloGenericSlug, ZeltySlug}
	got := registry.Slugs()
	if len(got) != len(wantSlugs) {
		t.Fatalf("Slugs() = %v, want %v", got, wantSlugs)
	}
	for i, want := range wantSlugs {
		if got[i] != want {
			t.Fatalf("Slugs()[%d] = %q, want %q (tri alphabetique)", i, got[i], want)
		}
	}

	for _, slug := range wantSlugs {
		provider, err := registry.Get(slug)
		if err != nil {
			t.Fatalf("Get(%q): %v", slug, err)
		}
		if provider.Slug() != slug {
			t.Fatalf("Get(%q).Slug() = %q", slug, provider.Slug())
		}
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

// Le registre est une valeur injectable, pas une map globale : un appelant doit
// pouvoir substituer un provider en test sans toucher a l'etat du paquet.
func TestNewRegistryOverridesBySlug(t *testing.T) {
	stub := &stubProvider{slug: ZeltySlug}

	registry := NewRegistry(NewZeltyProvider(), stub)

	provider, err := registry.Get(ZeltySlug)
	if err != nil {
		t.Fatalf("Get(%q): %v", ZeltySlug, err)
	}
	if provider != ImportProvider(stub) {
		t.Fatal("le dernier provider enregistre ne l'emporte pas")
	}
	if len(registry.Slugs()) != 1 {
		t.Fatalf("Slugs() = %v, want une seule entree", registry.Slugs())
	}
}

type stubProvider struct{ slug string }

func (p *stubProvider) Slug() string { return p.slug }

func (p *stubProvider) Parse(io.Reader) (*IntermediateImport, error) {
	return &IntermediateImport{Provider: p.slug}, nil
}

func TestRowErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"avec colonne", rowErrorf(42, "Prix", "montant %q illisible", "offert"), `ligne 42, colonne "Prix": montant "offert" illisible`},
		{"sans colonne", rowErrorf(7, "", "ligne incomprehensible"), "ligne 7: ligne incomprehensible"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}
