package importer

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

var (
	// ErrUnknownProvider est renvoye par Registry.Get pour un slug inconnu.
	ErrUnknownProvider = errors.New("provider d'import inconnu")

	// ErrNoProducts signale un fichier syntaxiquement lisible mais sans
	// aucun produit. C'est la garde contre le mauvais fichier ou le mauvais
	// provider : sans elle l'utilisateur enchainerait sur une preview vide
	// sans comprendre pourquoi.
	ErrNoProducts = errors.New("aucun produit trouve dans le fichier")
)

// ImportProvider adapte un format source vers le modele canonique. Une
// implementation est pure : elle lit un flux et rend une structure, sans
// toucher a la base ni au reseau.
type ImportProvider interface {
	Slug() string
	Parse(r io.Reader) (*IntermediateImport, error)
}

// Registry resout un slug vers son provider. Volontairement une valeur
// injectable plutot qu'une map globale mutable, pour rester alignee sur la DI
// par constructeur du depot et rester substituable en test.
type Registry struct {
	providers map[string]ImportProvider
}

// NewRegistry construit un registre a partir des providers fournis. Un slug en
// double ecrase le precedent, l'ordre d'enregistrement fait foi.
func NewRegistry(providers ...ImportProvider) *Registry {
	r := &Registry{providers: make(map[string]ImportProvider, len(providers))}
	for _, p := range providers {
		r.providers[p.Slug()] = p
	}
	return r
}

// DefaultRegistry rend le registre des providers livres avec l'API.
func DefaultRegistry() *Registry {
	return NewRegistry(
		NewZeltyProvider(),
		NewWelloGenericProvider(),
	)
}

// Get resout un slug. L'erreur enveloppe ErrUnknownProvider.
func (r *Registry) Get(slug string) (ImportProvider, error) {
	p, ok := r.providers[slug]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, slug)
	}
	return p, nil
}

// Slugs rend les slugs disponibles, tries, pour alimenter une liste de choix.
func (r *Registry) Slugs() []string {
	slugs := make([]string, 0, len(r.providers))
	for slug := range r.providers {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

// RowError situe une erreur de parsing dans le fichier. Le numero de ligne est
// celui affiche par le tableur (1-indexe), pour que le message soit
// directement actionnable par le restaurateur.
//
// Exporte pour que la couche HTTP puisse la distinguer d'une panne serveur :
// une ligne mal remplie est une erreur de l'appelant, pas du service.
type RowError struct {
	Line   int
	Column string
	Reason string
}

func (e *RowError) Error() string {
	if e.Column == "" {
		return fmt.Sprintf("ligne %d: %s", e.Line, e.Reason)
	}
	return fmt.Sprintf("ligne %d, colonne %q: %s", e.Line, e.Column, e.Reason)
}

func rowErrorf(line int, column, format string, args ...any) error {
	return &RowError{Line: line, Column: column, Reason: fmt.Sprintf(format, args...)}
}
