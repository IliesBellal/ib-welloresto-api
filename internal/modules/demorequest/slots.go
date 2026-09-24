package demorequest

import "time"

const (
	// slotDurationMinutes est la durée d'un appel de démo — reprend le
	// "30 minutes chrono" déjà annoncé ailleurs sur le site vitrine
	// (/contact, mockups).
	slotDurationMinutes = 30

	// slotLeadTime est le délai de prévenance minimal avant qu'un créneau ne
	// devienne réservable — évite qu'un visiteur réserve un appel dans les
	// 10 prochaines minutes, que personne côté Wello Resto ne peut honorer.
	slotLeadTime = 3 * time.Hour

	// slotLookaheadDays borne l'horizon de réservation (jours calendaires,
	// dimanche exclu ensuite) — au-delà, rien n'est proposé.
	slotLookaheadDays = 14
)

type slotWindow struct {
	StartHour, StartMinute int
	EndHour, EndMinute     int
}

// slotWindows reprend les créneaux hors service déjà annoncés sur le site
// vitrine (brief §4.3) : 9h-11h et 14h30-17h, du lundi au samedi (dimanche
// exclu dans generateCandidateSlots). Modifier cette liste suffit à changer
// les horaires ouverts à la réservation — aucune autre partie du code n'a de
// connaissance indépendante des horaires.
var slotWindows = []slotWindow{
	{StartHour: 9, StartMinute: 0, EndHour: 11, EndMinute: 0},
	{StartHour: 14, StartMinute: 30, EndHour: 17, EndMinute: 0},
}

func parisLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		// Dégradation minimale : mieux vaut UTC (horaires décalés de 1-2h)
		// que planter si la base de données de fuseaux horaires du système
		// est absente — cas improbable mais jamais fatal pour un formulaire.
		return time.UTC
	}
	return loc
}

// generateCandidateSlots énumère tous les créneaux ouverts à la réservation
// entre now et les slotLookaheadDays prochains jours calendaires, hors
// dimanche et hors créneaux commençant avant now+slotLeadTime. Ne sait rien
// des créneaux déjà réservés — Service.AvailableSlots retranche ensuite ceux
// remontés par Repository.BookedSlotsFrom, et Service.Create rappelle cette
// même fonction pour revalider un slot_start soumis par le client plutôt que
// de lui faire confiance.
func generateCandidateSlots(now time.Time) []time.Time {
	loc := parisLocation()
	now = now.In(loc)
	earliest := now.Add(slotLeadTime)

	var slots []time.Time
	for dayOffset := 0; dayOffset < slotLookaheadDays; dayOffset++ {
		day := now.AddDate(0, 0, dayOffset)
		if day.Weekday() == time.Sunday {
			continue
		}
		for _, w := range slotWindows {
			start := time.Date(day.Year(), day.Month(), day.Day(), w.StartHour, w.StartMinute, 0, 0, loc)
			end := time.Date(day.Year(), day.Month(), day.Day(), w.EndHour, w.EndMinute, 0, 0, loc)
			step := time.Duration(slotDurationMinutes) * time.Minute
			for t := start; !t.Add(step).After(end); t = t.Add(step) {
				if t.Before(earliest) {
					continue
				}
				slots = append(slots, t)
			}
		}
	}
	return slots
}

// isCandidateSlot vérifie qu'un horaire précis fait bien partie des créneaux
// actuellement valides — recalculé côté serveur à l'identique, jamais confié
// au client (voir Service.Create).
func isCandidateSlot(slotStart, now time.Time) bool {
	for _, candidate := range generateCandidateSlots(now) {
		if candidate.Equal(slotStart) {
			return true
		}
	}
	return false
}
