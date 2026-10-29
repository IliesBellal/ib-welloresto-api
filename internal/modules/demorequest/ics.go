package demorequest

import (
	"time"

	"welloresto-api/internal/ics"
)

// buildICS génère le fichier iCalendar de l'appel de démo réservé (package
// ics, partagé avec les réservations).
func buildICS(start, end time.Time, summary, description string) []byte {
	return ics.Build(ics.Event{Start: start, End: end, Summary: summary, Description: description}, "-//WelloResto//Demo Booking//FR")
}

// buildGoogleCalendarLink construit un lien "ajout rapide" Google Agenda —
// une simple URL, aucune intégration Google Calendar API/OAuth nécessaire
// (voir wello-resto-vitrine/docs/decisions-log.md pour pourquoi l'intégration
// complète a été écartée au profit de ce système de créneaux maison).
func buildGoogleCalendarLink(start, end time.Time, summary, description string) string {
	return ics.GoogleCalendarLink(ics.Event{Start: start, End: end, Summary: summary, Description: description})
}
