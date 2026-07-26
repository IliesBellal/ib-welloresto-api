package bookingcore

import "fmt"

const (
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusSeated    = "seated"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusNoShow    = "no_show"
	StatusDenied    = "denied"
)

// validStatuses énumère les seules valeurs acceptées en insertion. Utilisé
// par bookingcore.CreateBooking pour rejeter au plus tôt un statut vide ou
// non normalisé, avant qu'il n'atteigne le SQL (cf. status="" historique :
// un appelant qui oublie de fournir/normaliser le statut ne doit plus
// pouvoir écrire une valeur silencieusement invalide).
var validStatuses = map[string]bool{
	StatusPending:   true,
	StatusConfirmed: true,
	StatusSeated:    true,
	StatusCompleted: true,
	StatusCancelled: true,
	StatusNoShow:    true,
	StatusDenied:    true,
}

// IsValidStatus indique si status correspond à l'une des valeurs cible de
// l'enum ci-dessus (post-normalisation : les alias legacy ne sont pas
// valides tels quels, l'appelant doit passer par NormalizeLegacyStatus
// d'abord).
func IsValidStatus(status string) bool {
	return validStatuses[status]
}

// NormalizeLegacyStatus aligne les valeurs historiques vers le vocabulaire
// cible. Les statuts deja normalises sont conserves tels quels.
func NormalizeLegacyStatus(status string) string {
	switch status {
	case "PENDING_APPROVAL":
		return StatusPending
	case "ACCEPTED":
		return StatusConfirmed
	case "ORDER_OPEN":
		return StatusSeated
	case "0":
		return StatusCompleted
	case "CANCELED":
		return StatusCancelled
	case "DENIED":
		return StatusDenied
	default:
		return status
	}
}

func IsActiveForConflict(status string) bool {
	n := NormalizeLegacyStatus(status)
	return n == StatusPending || n == StatusConfirmed || n == StatusSeated
}

// CanTransition valide la machine a etats cible.
func CanTransition(from, to string) error {
	f := NormalizeLegacyStatus(from)
	t := NormalizeLegacyStatus(to)

	if f == t {
		return nil
	}

	allowed := map[string]map[string]bool{
		StatusPending: {
			StatusConfirmed: true,
			StatusDenied:    true,
		},
		StatusConfirmed: {
			StatusSeated:    true,
			StatusCancelled: true,
			StatusPending:   true,
			StatusNoShow:    true,
		},
		StatusSeated: {
			StatusCompleted: true,
			StatusCancelled: true,
		},
	}

	if next, ok := allowed[f]; ok && next[t] {
		return nil
	}

	return fmt.Errorf("invalid_transition: %s -> %s", f, t)
}

// ResolveCancellationActor centralise la valeur canceled_by en fonction
// du contexte d'appel.
func ResolveCancellationActor(actor string, actorID string) string {
	switch actor {
	case "customer":
		return "CUSTOMER"
	case "system":
		return "SYSTEM"
	default:
		if actorID != "" {
			return actorID
		}
		return "SYSTEM"
	}
}
