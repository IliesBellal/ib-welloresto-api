package demorequest

import (
	"hash/fnv"
	"time"
)

const (
	// slotDurationMinutes est la durée d'un appel de démo — reprend le
	// "30 minutes chrono" déjà annoncé ailleurs sur le site vitrine
	// (/contact, mockups).
	slotDurationMinutes = 30

	// slotLeadTime est le délai de prévenance minimal avant qu'un créneau ne
	// devienne réservable — évite qu'un visiteur réserve un appel dans les
	// 10 prochaines minutes, que personne côté Wello Resto ne peut honorer.
	slotLeadTime = 3 * time.Hour

	// slotLookaheadDays borne l'horizon de réservation (jours calendaires) —
	// au-delà, rien n'est proposé.
	slotLookaheadDays = 14
)

type slotWindow struct {
	StartHour, StartMinute int
	EndHour, EndMinute     int
}

// slotWindows : tous les jours, 8h-18h (demande du fondateur du 2026-09-24 —
// remplace l'ancien modèle "9h-11h/14h30-17h, lundi-samedi"). Modifier cette
// liste suffit à changer les horaires ouverts à la réservation — aucune
// autre partie du code n'a de connaissance indépendante des horaires.
var slotWindows = []slotWindow{
	{StartHour: 8, StartMinute: 0, EndHour: 18, EndMinute: 0},
}

const (
	// simulatedBusyRatio est la part de créneaux affichés comme pris alors
	// qu'aucun rendez-vous réel n'existe — donne une impression d'affluence
	// (demande explicite du fondateur, 2026-09-24 : "simuler une
	// affluence"). Purement cosmétique côté affichage, mais appliqué au
	// niveau de generateCandidateSlots donc également opposable à une
	// réservation : un horaire simulé "pris" est rejeté par isCandidateSlot
	// exactement comme un horaire réellement réservé (voir Service.Create)
	// — jamais de décalage entre ce qui est montré indisponible et ce qui
	// est réellement réservable.
	simulatedBusyRatio = 0.4

	// simulatedBusySalt évite qu'un horaire "libre" ou "pris" ne se déduise
	// trivialement d'un calcul en clair sur l'horodatage brut — une chaîne
	// fixe suffit, ce n'est pas un secret cryptographique à protéger.
	simulatedBusySalt = "welloresto-demo-slots-v1"
)

// isSimulatedBusy décide, de façon déterministe et stable dans le temps
// (fonction pure de l'horaire, aucun état stocké — pas besoin de Redis ni de
// table dédiée), si un créneau doit apparaître pris sans réservation réelle
// derrière. Déterministe = le même horaire donne toujours le même résultat,
// à chaque appel, sur n'importe quelle instance de l'API : jamais un
// horaire "libre" à un instant puis "pris" l'instant suivant sans raison
// visible pour un visiteur qui recharge la page.
func isSimulatedBusy(slot time.Time) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(simulatedBusySalt))
	_, _ = h.Write([]byte(slot.UTC().Format(time.RFC3339)))
	return float64(h.Sum32()%1000)/1000 < simulatedBusyRatio
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
// entre now et les slotLookaheadDays prochains jours calendaires : hors
// créneaux commençant avant now+slotLeadTime, et hors créneaux marqués
// isSimulatedBusy (affluence simulée, voir plus haut). Ne sait rien des
// créneaux RÉELLEMENT réservés — Service.AvailableSlots retranche ensuite
// ceux remontés par Repository.BookedSlotsFrom, et Service.Create rappelle
// cette même fonction pour revalider un slot_start soumis par le client
// plutôt que de lui faire confiance.
func generateCandidateSlots(now time.Time) []time.Time {
	loc := parisLocation()
	now = now.In(loc)
	earliest := now.Add(slotLeadTime)

	var slots []time.Time
	for dayOffset := 0; dayOffset < slotLookaheadDays; dayOffset++ {
		day := now.AddDate(0, 0, dayOffset)
		for _, w := range slotWindows {
			start := time.Date(day.Year(), day.Month(), day.Day(), w.StartHour, w.StartMinute, 0, 0, loc)
			end := time.Date(day.Year(), day.Month(), day.Day(), w.EndHour, w.EndMinute, 0, 0, loc)
			step := time.Duration(slotDurationMinutes) * time.Minute
			for t := start; !t.Add(step).After(end); t = t.Add(step) {
				if t.Before(earliest) {
					continue
				}
				if isSimulatedBusy(t) {
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
