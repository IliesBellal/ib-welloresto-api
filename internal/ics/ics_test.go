package ics

import (
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.FixedZone("CET", 3600))
	e := Event{
		UID:         "b-42@reservation.welloresto.fr",
		Sequence:    7,
		Start:       start,
		End:         start.Add(90 * time.Minute),
		Summary:     "Réservation chez Trattoria Nonna Lucia",
		Description: "Réservation pour 4 personne(s), référence R-48213.\nGérer la réservation : https://rsv.welloresto.fr/restaurant/nonna-lucia/booking/R-48213",
		Location:    "Trattoria Nonna Lucia, 12 rue des Jardins, 57000 Metz",
	}
	out := string(Build(e, "-//WelloResto//Reservation//FR"))

	for _, want := range []string{
		"METHOD:PUBLISH", "STATUS:CONFIRMED", "UID:b-42@reservation.welloresto.fr", "SEQUENCE:7",
		"DTSTART:20261107T190000Z", "DTEND:20261107T203000Z", `LOCATION:Trattoria Nonna Lucia\, 12 rue des Jardins\, 57000 Metz`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fichier sans %q :\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "END:VCALENDAR\r\n") {
		t.Error("fin de fichier sans CRLF")
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("ligne de %d octets (max 75) : %q", len(line), line)
		}
	}
	// Une ligne repliée se recolle en retirant CRLF + espace.
	if !strings.Contains(strings.ReplaceAll(out, "\r\n ", ""), "booking/R-48213") {
		t.Error("description mal repliée")
	}

	e.Cancelled = true
	cancelled := string(Build(e, "-//WelloResto//Reservation//FR"))
	if !strings.Contains(cancelled, "METHOD:CANCEL") || !strings.Contains(cancelled, "STATUS:CANCELLED") {
		t.Errorf("annulation sans METHOD:CANCEL / STATUS:CANCELLED :\n%s", cancelled)
	}
}

func TestFoldKeepsUTF8(t *testing.T) {
	line := "SUMMARY:" + strings.Repeat("é", 60)
	for _, part := range strings.Split(fold(line), "\r\n ") {
		if !strings.HasPrefix(part, "SUMMARY") && !strings.HasPrefix(part, "é") {
			t.Fatalf("caractère UTF-8 coupé : %q", part)
		}
	}
}
