// Package ics produit les fichiers iCalendar (RFC 5545) joints aux mails :
// rendez-vous de démo, réservations (docs/emails-styles.md). Un seul VEVENT,
// sans récurrence ni fuseau (horodatages UTC, suffixe « Z »).
package ics

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Event est un rendez-vous. UID identifie l'événement d'un envoi à l'autre :
// un nouveau fichier de même UID et de Sequence plus grande met à jour
// l'événement dans l'agenda au lieu d'en créer un second ; Cancelled le
// retire (METHOD:CANCEL, STATUS:CANCELLED).
type Event struct {
	UID         string
	Sequence    int
	Start       time.Time
	End         time.Time
	Summary     string
	Description string
	Location    string
	Cancelled   bool
}

// Build renvoie le fichier .ics de l'événement. prodID identifie l'émetteur,
// ex. "-//WelloResto//Reservation//FR".
func Build(e Event, prodID string) []byte {
	method, status := "PUBLISH", "CONFIRMED"
	if e.Cancelled {
		method, status = "CANCEL", "CANCELLED"
	}
	uid := e.UID
	if uid == "" {
		uid = RandomUID()
	}
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:" + prodID,
		"CALSCALE:GREGORIAN",
		"METHOD:" + method,
		"BEGIN:VEVENT",
		"UID:" + uid,
		fmt.Sprintf("SEQUENCE:%d", e.Sequence),
		"DTSTAMP:" + FormatTime(time.Now()),
		"DTSTART:" + FormatTime(e.Start),
		"DTEND:" + FormatTime(e.End),
		"STATUS:" + status,
		"SUMMARY:" + EscapeText(e.Summary),
	}
	if e.Description != "" {
		lines = append(lines, "DESCRIPTION:"+EscapeText(e.Description))
	}
	if e.Location != "" {
		lines = append(lines, "LOCATION:"+EscapeText(e.Location))
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")

	var b strings.Builder
	for _, l := range lines {
		// RFC 5545 : lignes de 75 octets au plus, fins de ligne CRLF, y
		// compris sur la dernière.
		b.WriteString(fold(l))
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// GoogleCalendarLink construit un lien « ajout rapide » Google Agenda : une
// simple URL, sans intégration à l'API Google Calendar.
func GoogleCalendarLink(e Event) string {
	params := url.Values{}
	params.Set("action", "TEMPLATE")
	params.Set("text", e.Summary)
	params.Set("dates", FormatTime(e.Start)+"/"+FormatTime(e.End))
	if e.Description != "" {
		params.Set("details", e.Description)
	}
	if e.Location != "" {
		params.Set("location", e.Location)
	}
	return "https://calendar.google.com/calendar/render?" + params.Encode()
}

func FormatTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// EscapeText échappe les caractères spéciaux du format (RFC 5545 §3.3.11) :
// les textes viennent de champs saisis (nom d'établissement, adresse).
func EscapeText(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`).Replace(s)
}

// RandomUID renvoie un UID unique, pour un événement qui ne sera jamais mis à jour.
func RandomUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) + "@welloresto.fr"
}

// fold coupe une ligne en morceaux de 75 octets au plus, sans couper un
// caractère UTF-8 ; chaque suite commence par une espace (RFC 5545 §3.1).
func fold(line string) string {
	const max = 75
	if len(line) <= max {
		return line
	}
	var b strings.Builder
	limit := max
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = max - 1 // l'espace de continuation compte dans les 75 octets
	}
	b.WriteString(line)
	return b.String()
}
