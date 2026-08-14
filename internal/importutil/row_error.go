package importutil

import "fmt"

// RowError situe une erreur de parsing dans le fichier. Le numéro de ligne
// est celui affiché par le tableur (1-indexé), pour que le message soit
// directement actionnable par l'utilisateur.
//
// Exporté pour que la couche HTTP puisse la distinguer d'une panne serveur :
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

// RowErrorf construit un *RowError avec un message formaté.
func RowErrorf(line int, column, format string, args ...any) error {
	return &RowError{Line: line, Column: column, Reason: fmt.Sprintf(format, args...)}
}
