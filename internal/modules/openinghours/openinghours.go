// Package openinghours porte en Go la logique de l'ancienne procédure stockée
// MySQL GET_POS_STATUS : à partir des créneaux actifs de hours_of_operation et
// d'un instant donné (dans le fuseau du marchand), il calcule le dernier
// créneau passé, le créneau courant, le prochain créneau et le statut
// ouvert/fermé. Le calcul est pur (aucun accès base) ; la lecture des créneaux
// se fait via FetchActiveSlots (repository.go).
package openinghours

import (
	"fmt"
	"strings"
	"time"
)

// DateTimeLayout est le format "YYYY-MM-DD HH:MM:SS" produit par l'ancienne
// procédure (CONCAT date + heure) et attendu par les clients.
const DateTimeLayout = "2006-01-02 15:04:05"

// Slot est un créneau d'ouverture de hours_of_operation.
// Convention de la table : day_of_week 1 = lundi ... 7 = dimanche.
// Les heures sont au format "HH:MM:SS" (ou "HH:MM").
type Slot struct {
	DayOfWeekFrom int
	DayOfWeekTo   int
	HourFrom      string
	HourTo        string
}

// POSStatus reprend les paramètres OUT de GET_POS_STATUS. Une borne nil
// correspond à un OUT NULL (aucun créneau trouvé pour cette catégorie).
type POSStatus struct {
	IsOpen       bool
	LastStart    *time.Time
	LastEnd      *time.Time
	CurrentStart *time.Time
	CurrentEnd   *time.Time
	NextStart    *time.Time
	NextEnd      *time.Time
}

// FormatDateTime rend la borne au format DateTimeLayout, ou "" si nil —
// équivalent du sql.NullString.String utilisé par les appelants historiques.
func FormatDateTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(DateTimeLayout)
}

// ComputePOSStatus reproduit GET_POS_STATUS. currentDatetime doit être dans le
// fuseau du marchand (ex-paramètre p_current_datetime). Les slots doivent déjà
// être filtrés sur enabled et valid_from/valid_to (fait par FetchActiveSlots).
//
// La sémantique MySQL est conservée telle quelle, y compris ses limites :
// les comparaisons sont bornées à la semaine courante (un créneau du dimanche
// n'est jamais "dernier créneau" un lundi), et un créneau à cheval sur minuit
// (hour_from > hour_to) ne matche jamais comme créneau courant.
func ComputePOSStatus(currentDatetime time.Time, slots []Slot) POSStatus {
	// WEEKDAY(p_current_datetime) + 1 : 1 = lundi ... 7 = dimanche.
	day := isoWeekday(currentDatetime)
	nowSec := currentDatetime.Hour()*3600 + currentDatetime.Minute()*60 + currentDatetime.Second()

	type candidate struct {
		slot    Slot
		fromSec int
		toSec   int
	}
	var last, current, next *candidate

	for _, s := range slots {
		fromSec, okFrom := parseClock(s.HourFrom)
		toSec, okTo := parseClock(s.HourTo)
		if !okFrom || !okTo {
			continue
		}
		c := candidate{slot: s, fromSec: fromSec, toSec: toSec}

		// Dernier créneau : entièrement passé dans la semaine courante.
		// ORDER BY day_of_week_from DESC, hour_from DESC LIMIT 1.
		if day > s.DayOfWeekTo || (day == s.DayOfWeekTo && nowSec > toSec) {
			if last == nil || s.DayOfWeekFrom > last.slot.DayOfWeekFrom ||
				(s.DayOfWeekFrom == last.slot.DayOfWeekFrom && fromSec > last.fromSec) {
				cc := c
				last = &cc
			}
		}

		// Créneau courant : jour et heure dans les bornes (BETWEEN inclusif).
		// La procédure faisait LIMIT 1 sans ORDER BY ; ici le choix est rendu
		// déterministe (premier créneau par day_of_week_from, hour_from).
		if s.DayOfWeekFrom <= day && day <= s.DayOfWeekTo && fromSec <= nowSec && nowSec <= toSec {
			if current == nil || s.DayOfWeekFrom < current.slot.DayOfWeekFrom ||
				(s.DayOfWeekFrom == current.slot.DayOfWeekFrom && fromSec < current.fromSec) {
				cc := c
				current = &cc
			}
		}

		// Prochain créneau : commence plus tard dans la semaine courante.
		// ORDER BY day_of_week_from, hour_from LIMIT 1.
		if day < s.DayOfWeekFrom || (day == s.DayOfWeekFrom && nowSec < fromSec) {
			if next == nil || s.DayOfWeekFrom < next.slot.DayOfWeekFrom ||
				(s.DayOfWeekFrom == next.slot.DayOfWeekFrom && fromSec < next.fromSec) {
				cc := c
				next = &cc
			}
		}
	}

	var status POSStatus
	if last != nil {
		// DATE_SUB(DATE(now), INTERVAL (day - day_of_week_from) DAY) + hour_from
		status.LastStart = atClock(currentDatetime, last.slot.DayOfWeekFrom-day, last.fromSec)
		status.LastEnd = atClock(currentDatetime, last.slot.DayOfWeekTo-day, last.toSec)
	}
	if current != nil {
		status.IsOpen = true
		// La procédure datait les deux bornes du jour courant, même pour un
		// créneau multi-jours — comportement conservé.
		status.CurrentStart = atClock(currentDatetime, 0, current.fromSec)
		status.CurrentEnd = atClock(currentDatetime, 0, current.toSec)
	}
	if next != nil {
		// DATE_ADD(DATE(now), INTERVAL (day_of_week_from - day) DAY) + hour_from
		status.NextStart = atClock(currentDatetime, next.slot.DayOfWeekFrom-day, next.fromSec)
		status.NextEnd = atClock(currentDatetime, next.slot.DayOfWeekTo-day, next.toSec)
	}
	return status
}

// isoWeekday convertit time.Weekday (dimanche = 0) vers 1 = lundi ... 7 = dimanche.
func isoWeekday(t time.Time) int {
	d := int(t.Weekday())
	if d == 0 {
		return 7
	}
	return d
}

// parseClock convertit "HH:MM:SS" ou "HH:MM" en secondes depuis minuit.
func parseClock(s string) (int, bool) {
	s = strings.TrimSpace(s)
	var h, m, sec int
	switch strings.Count(s, ":") {
	case 2:
		if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
			return 0, false
		}
	case 1:
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 || sec < 0 || sec > 59 {
		return 0, false
	}
	return h*3600 + m*60 + sec, true
}

// atClock construit la date du jour de ref décalée de deltaDays, à l'heure
// clockSec, dans le fuseau de ref. Arithmétique calendaire pure, comme les
// DATE_ADD/DATE_SUB de la procédure (time.Date normalise les débordements).
func atClock(ref time.Time, deltaDays, clockSec int) *time.Time {
	t := time.Date(ref.Year(), ref.Month(), ref.Day()+deltaDays,
		clockSec/3600, (clockSec%3600)/60, clockSec%60, 0, ref.Location())
	return &t
}
