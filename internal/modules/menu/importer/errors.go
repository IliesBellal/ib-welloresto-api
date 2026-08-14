package importer

import "welloresto-api/internal/importutil"

// RowError, ErrEmptyWorkbook et ErrInvalidWorkbook sont réexportés depuis
// internal/importutil, qui en est désormais la source de vérité. Le type
// alias préserve l'identité de type attendue par import_handler.go
// (errors.As(err, *importer.RowError)), et les erreurs sentinelles
// réexportées préservent errors.Is(err, importer.ErrEmptyWorkbook) côté
// appelant.
type RowError = importutil.RowError

var (
	ErrEmptyWorkbook   = importutil.ErrEmptyWorkbook
	ErrInvalidWorkbook = importutil.ErrInvalidWorkbook
)

// rowErrorf construit une RowError. Conservé comme fonction locale pour ne
// pas réécrire chaque site d'appel du package en importutil.RowErrorf.
func rowErrorf(line int, column, format string, args ...any) error {
	return importutil.RowErrorf(line, column, format, args...)
}
