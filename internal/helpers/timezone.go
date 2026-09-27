package helpers

import (
	"context"
	"strings"
	"time"

	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

// MerchantLocation charge le fuseau IANA d'un merchant (merchant.timezone).
// Ne renvoie jamais nil : un nom vide vaut UTC (comportement de
// time.LoadLocation), un nom invalide — ou des données de fuseaux absentes —
// retombe sur UTC avec un avertissement. Le motif `loc, _ :=
// time.LoadLocation(...)` renvoyait nil dans ce cas, et time.Now().In(nil)
// panique : toutes les commandes du merchant échouaient.
func MerchantLocation(ctx context.Context, name string) *time.Location {
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		logger.FromContext(ctx).Warn("invalid merchant timezone, falling back to UTC",
			zap.String("timezone", name), zap.Error(err))
		return time.UTC
	}
	return loc
}

// ISOWeekday retourne le jour de la semaine ISO de t (1 = lundi, ...,
// 7 = dimanche), convention de discounts_schedules, availabilities_schedules
// et hours_of_operation.
func ISOWeekday(t time.Time) int {
	if t.Weekday() == time.Sunday {
		return 7
	}
	return int(t.Weekday())
}
