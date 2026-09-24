package demorequest

import (
	"hash/fnv"
	"time"
)

const (
	// slotDurationMinutes : créneaux d'une heure pile (8h, 9h, 10h…) depuis
	// le 2026-09-25, demande du fondateur pour un tableau plus lisible que
	// des créneaux de 30 minutes.
	slotDurationMinutes = 60

	// slotLeadTime est le délai de prévenance minimal avant qu'un créneau ne
	// devienne réservable — évite qu'un visiteur réserve un appel dans les
	// 10 prochaines minutes, que personne côté Wello Resto ne peut honorer.
	slotLeadTime = 3 * time.Hour

	// slotLookaheadDays borne l'horizon de réservation (jours calendaires) —
	// 10 jours, pour correspondre exactement au tableau scrollable
	// horizontalement du site vitrine (une colonne par jour).
	slotLookaheadDays = 10
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

// generateGridSlots énumère TOUS les créneaux de la grille (un par heure,
// chaque jour des slotLookaheadDays prochains jours), sans aucun filtrage —
// y compris ceux trop proches (slotLeadTime) ou déjà pris. C'est la grille
// affichée telle quelle par le tableau du site vitrine (colonnes = jours,
// lignes = heures) : les créneaux indisponibles doivent y rester visibles,
// grisés, pas disparaître (2026-09-25, demande du fondateur). Le statut de
// chaque créneau se calcule séparément — voir isSlotBookable et
// Service.SlotGrid.
func generateGridSlots(now time.Time) []time.Time {
	loc := parisLocation()
	now = now.In(loc)

	var slots []time.Time
	for dayOffset := 0; dayOffset < slotLookaheadDays; dayOffset++ {
		day := now.AddDate(0, 0, dayOffset)
		for _, w := range slotWindows {
			start := time.Date(day.Year(), day.Month(), day.Day(), w.StartHour, w.StartMinute, 0, 0, loc)
			end := time.Date(day.Year(), day.Month(), day.Day(), w.EndHour, w.EndMinute, 0, 0, loc)
			step := time.Duration(slotDurationMinutes) * time.Minute
			for t := start; !t.Add(step).After(end); t = t.Add(step) {
				slots = append(slots, t)
			}
		}
	}
	return slots
}

// isSlotBookable dit si un créneau de la grille peut structurellement être
// réservé à l'instant `now` — délai de prévenance respecté et non marqué
// isSimulatedBusy. Ne sait toujours rien des créneaux RÉELLEMENT réservés
// (Repository.BookedSlotsFrom, vérifié séparément par Service.SlotGrid et
// par l'index unique Postgres au moment de l'insertion, voir
// Repository.CreateBooking) : cette fonction ne couvre que ce qui est décidé
// sans toucher la base.
func isSlotBookable(slot, now time.Time) bool {
	if slot.Before(now.Add(slotLeadTime)) {
		return false
	}
	return !isSimulatedBusy(slot)
}

// generateCandidateSlots énumère les créneaux structurellement réservables
// (grille filtrée par isSlotBookable) — utilisé uniquement par
// isCandidateSlot pour revalider côté serveur un slot_start soumis par le
// client (Service.Create). Ne retranche pas les créneaux réellement déjà
// réservés : c'est l'index unique Postgres (migration 154) qui tranche ce
// cas au moment de l'insertion, pas cette fonction.
func generateCandidateSlots(now time.Time) []time.Time {
	now = now.In(parisLocation())

	var candidates []time.Time
	for _, slot := range generateGridSlots(now) {
		if isSlotBookable(slot, now) {
			candidates = append(candidates, slot)
		}
	}
	return candidates
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
