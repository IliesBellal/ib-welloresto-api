package demorequest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// buildICS génère un fichier iCalendar (RFC 5545) minimal pour l'appel de
// démo réservé — aucune librairie iCalendar dans ce repo (vérifié), le
// format est simple à produire à la main pour un unique VEVENT sans
// récurrence ni fuseau horaire (horodatage UTC, suffixe "Z").
func buildICS(start, end time.Time, summary, description string) []byte {
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//WelloResto//Demo Booking//FR",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:" + generateICSUID(),
		"DTSTAMP:" + formatICSTime(time.Now()),
		"DTSTART:" + formatICSTime(start),
		"DTEND:" + formatICSTime(end),
		"SUMMARY:" + escapeICSText(summary),
		"DESCRIPTION:" + escapeICSText(description),
		"END:VEVENT",
		"END:VCALENDAR",
	}
	// RFC 5545 impose des fins de ligne CRLF, y compris sur la dernière.
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// buildGoogleCalendarLink construit un lien "ajout rapide" Google Agenda —
// une simple URL, aucune intégration Google Calendar API/OAuth nécessaire
// (voir wello-resto-vitrine/docs/decisions-log.md pour pourquoi l'intégration
// complète a été écartée au profit de ce système de créneaux maison).
func buildGoogleCalendarLink(start, end time.Time, summary, description string) string {
	params := url.Values{}
	params.Set("action", "TEMPLATE")
	params.Set("text", summary)
	params.Set("dates", formatICSTime(start)+"/"+formatICSTime(end))
	params.Set("details", description)
	return "https://calendar.google.com/calendar/render?" + params.Encode()
}

func formatICSTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// escapeICSText échappe les caractères spéciaux du format (RFC 5545 §3.3.11)
// — texte libre construit ici depuis des champs saisis par le visiteur (nom
// d'établissement), jamais garanti exempt de virgule/point-virgule.
func escapeICSText(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		";", `\;`,
		",", `\,`,
		"\n", `\n`,
	)
	return replacer.Replace(s)
}

func generateICSUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s@welloresto.fr", hex.EncodeToString(b))
}
