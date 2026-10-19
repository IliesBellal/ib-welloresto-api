package scannorder

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Horaires de livraison ScanNOrder (table delivery_hours, docs/DELIVERY_HOURS.md).
//
// Ils restreignent uniquement les commandes ScanNOrder DELIVERY, en plus des
// horaires d'ouverture. Le POS, la borne, Uber Eats et Deliveroo les ignorent
// volontairement : ne jamais déplacer ce contrôle dans un code partagé
// (orders, order_life_cycle, openinghours, hours_of_operation).

// StatusDeliveryClosed : commande DELIVERY hors des horaires de livraison.
const StatusDeliveryClosed = "delivery_closed"

// DeliveryHour : un créneau de livraison.
type DeliveryHour struct {
	// DayOfWeek : 1 = lundi ... 7 = dimanche.
	DayOfWeek int `json:"day_of_week"`
	// HourFrom : début inclus, "HH:MM".
	HourFrom string `json:"hour_from"`
	// HourTo : fin exclue, "HH:MM". HourTo <= HourFrom = le créneau passe
	// minuit et se termine le lendemain.
	HourTo string `json:"hour_to"`
}

// isDeliveryOpenAt indique si une livraison est acceptée à l'instant at
// (exprimé dans le fuseau du marchand). Aucun créneau = aucune restriction.
func isDeliveryOpenAt(hours []DeliveryHour, at time.Time) bool {
	if len(hours) == 0 {
		return true
	}
	day := helpers.ISOWeekday(at)
	previousDay := day - 1
	if previousDay == 0 {
		previousDay = 7
	}
	clock := at.Format("15:04")

	for _, h := range hours {
		if h.HourTo > h.HourFrom {
			if h.DayOfWeek == day && clock >= h.HourFrom && clock < h.HourTo {
				return true
			}
			continue
		}
		// Créneau qui passe minuit : sa partie avant minuit le jour même, sa
		// partie après minuit le lendemain.
		if h.DayOfWeek == day && clock >= h.HourFrom {
			return true
		}
		if h.DayOfWeek == previousDay && clock < h.HourTo {
			return true
		}
	}
	return false
}

// deliveryRequestedAt : instant de livraison à contrôler — le créneau choisi
// pour une commande programmée, maintenant sinon. Le front envoie toujours un
// estimated_ready (préparation estimée pour une commande immédiate) : seul
// is_scheduled dit qu'il s'agit de l'heure demandée par le client.
func deliveryRequestedAt(order *models.OrderRequest, now time.Time) time.Time {
	if order != nil && order.IsScheduled {
		if t, ok := parseEstimatedReady(order.EstimatedReady); ok {
			return t.In(now.Location())
		}
	}
	return now
}

// parseEstimatedReady accepte les mêmes formats que la création de commande
// (order_life_cycle.normalizeEstimatedReady) : ISO 8601 ou timestamp Unix.
func parseEstimatedReady(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 1_000_000_000 {
		return time.Unix(unix, 0), true
	}
	return time.Time{}, false
}

// normalizeDeliveryHours valide les créneaux saisis et les remet au format
// "HH:MM", triés par jour puis heure de début.
func normalizeDeliveryHours(hours []DeliveryHour) ([]DeliveryHour, error) {
	out := make([]DeliveryHour, 0, len(hours))
	for _, h := range hours {
		if h.DayOfWeek < 1 || h.DayOfWeek > 7 {
			return nil, fmt.Errorf("%w: day_of_week %d", models.ErrInvalidInput, h.DayOfWeek)
		}
		from, errFrom := time.Parse("15:04", formatHour(h.HourFrom))
		to, errTo := time.Parse("15:04", formatHour(h.HourTo))
		if errFrom != nil || errTo != nil {
			return nil, fmt.Errorf("%w: hour %q-%q", models.ErrInvalidInput, h.HourFrom, h.HourTo)
		}
		if from.Equal(to) {
			return nil, fmt.Errorf("%w: empty slot %q", models.ErrInvalidInput, h.HourFrom)
		}
		out = append(out, DeliveryHour{
			DayOfWeek: h.DayOfWeek,
			HourFrom:  from.Format("15:04"),
			HourTo:    to.Format("15:04"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DayOfWeek != out[j].DayOfWeek {
			return out[i].DayOfWeek < out[j].DayOfWeek
		}
		return out[i].HourFrom < out[j].HourFrom
	})
	return out, nil
}

// weeklyDeliveryHours présente les créneaux par jour, au format de
// MerchantStatus.OpenHours. nil quand aucun créneau n'est configuré.
func weeklyDeliveryHours(hours []DeliveryHour) []OpenHoursDay {
	if len(hours) == 0 {
		return nil
	}
	dayNames := []string{"Lundi", "Mardi", "Mercredi", "Jeudi", "Vendredi", "Samedi", "Dimanche"}
	weekly := make([]OpenHoursDay, 7)
	for i := range weekly {
		weekly[i] = OpenHoursDay{DayOfWeek: i + 1, DayName: dayNames[i], Hours: []OpeningPeriod{}}
	}
	for _, h := range hours {
		if h.DayOfWeek >= 1 && h.DayOfWeek <= 7 {
			weekly[h.DayOfWeek-1].Hours = append(weekly[h.DayOfWeek-1].Hours, OpeningPeriod{From: h.HourFrom, To: h.HourTo})
		}
	}
	return weekly
}

// GetDeliveryHoursSettings : créneaux de livraison du marchand connecté (back-office).
func (s *Service) GetDeliveryHoursSettings(ctx context.Context) ([]DeliveryHour, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	return s.repo.GetDeliveryHours(ctx, user.MerchantID)
}

// ReplaceDeliveryHoursSettings remplace toute la semaine de créneaux du
// marchand connecté. Une liste vide supprime la restriction.
func (s *Service) ReplaceDeliveryHoursSettings(ctx context.Context, hours []DeliveryHour) ([]DeliveryHour, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	normalized, err := normalizeDeliveryHours(hours)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceDeliveryHours(ctx, user.MerchantID, normalized); err != nil {
		return nil, err
	}
	// La réponse GetMerchant (en cache) expose les créneaux.
	s.redis.InvalidateMerchantStatusCache(ctx, user.MerchantID)
	return normalized, nil
}
