package importer

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"welloresto-api/internal/importutil"
)

// RowError est un alias vers importutil.RowError (même type, pas une
// redéfinition) : ce paquet ne porte pas son propre type d'erreur de
// ligne/colonne, il réutilise celui, générique, déjà partagé avec l'import de
// produits. L'alias évite de qualifier importutil.RowError à chaque site
// d'appel et dans les tests (errors.As(err, *RowError)).
type RowError = importutil.RowError

// Slugs des providers d'import de clients.
const (
	WelloGenericSlug = "wello-generic"
	ManualSlug       = "manual"
	ZeltySlug        = "zelty"
)

var (
	// ErrUnknownProvider est renvoyé par Registry.Get pour un slug inconnu.
	ErrUnknownProvider = errors.New("provider d'import inconnu")

	// ErrMissingColumn signale une colonne obligatoire absente de l'en-tête.
	ErrMissingColumn = errors.New("colonne obligatoire absente")

	// ErrNoCustomers signale un fichier syntaxiquement lisible mais sans
	// aucun client. Garde contre le mauvais fichier ou le mauvais provider,
	// calquée sur ErrNoProducts côté import de produits.
	ErrNoCustomers = errors.New("aucun client trouve dans le fichier")

	// ErrInvalidCSV signale un flux CSV illisible (guillemets non refermés,
	// etc.) — le pendant de ErrInvalidWorkbook pour la porte Zelty, qui est du
	// CSV et non du xlsx.
	ErrInvalidCSV = errors.New("fichier illisible : un CSV est attendu")

	// ErrEmptyWorkbook et ErrInvalidWorkbook sont réexportées telles quelles
	// depuis internal/importutil, comme RowError ci-dessus : le handler HTTP
	// n'a besoin de dépendre que de ce paquet, jamais d'importutil directement.
	ErrEmptyWorkbook   = importutil.ErrEmptyWorkbook
	ErrInvalidWorkbook = importutil.ErrInvalidWorkbook
)

// rowErrorf construit une importutil.RowError. Fonction locale plutôt
// qu'appel direct à importutil.RowErrorf sur chaque site : les providers de
// ce paquet en sont tous consommateurs.
func rowErrorf(line int, column, format string, args ...any) error {
	return importutil.RowErrorf(line, column, format, args...)
}

// CustomerImportProvider adapte un format source vers le modèle canonique.
// Une implémentation est pure : elle lit un flux et rend une structure, sans
// toucher à la base ni au réseau.
type CustomerImportProvider interface {
	Slug() string
	Parse(r io.Reader) (*IntermediateCustomerImport, error)
}

// Registry résout un slug vers son provider. Volontairement une valeur
// injectable plutôt qu'une map globale mutable, pour rester alignée sur la DI
// par constructeur du dépôt et rester substituable en test.
type Registry struct {
	providers map[string]CustomerImportProvider
}

// NewRegistry construit un registre à partir des providers fournis. Un slug en
// double écrase le précédent, l'ordre d'enregistrement fait foi.
func NewRegistry(providers ...CustomerImportProvider) *Registry {
	r := &Registry{providers: make(map[string]CustomerImportProvider, len(providers))}
	for _, p := range providers {
		r.providers[p.Slug()] = p
	}
	return r
}

// DefaultRegistry rend le registre des providers livrés avec l'API.
//
// La saisie manuelle (BuildManualCustomerImport) n'y figure pas : comme côté
// produit, elle n'a pas de flux à lire et est appelée directement, hors
// registre.
func DefaultRegistry() *Registry {
	return NewRegistry(
		NewZeltyCustomerProvider(),
		NewWelloGenericCustomerProvider(),
	)
}

// Get résout un slug. L'erreur enveloppe ErrUnknownProvider.
func (r *Registry) Get(slug string) (CustomerImportProvider, error) {
	p, ok := r.providers[slug]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, slug)
	}
	return p, nil
}

// Slugs rend les slugs disponibles, triés, pour alimenter une liste de choix.
func (r *Registry) Slugs() []string {
	slugs := make([]string, 0, len(r.providers))
	for slug := range r.providers {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}
