package bookingcore

import (
	"fmt"
	"time"
)

var frWeekdays = map[time.Weekday]string{
	time.Monday:    "lundi",
	time.Tuesday:   "mardi",
	time.Wednesday: "mercredi",
	time.Thursday:  "jeudi",
	time.Friday:    "vendredi",
	time.Saturday:  "samedi",
	time.Sunday:    "dimanche",
}

var frMonths = map[time.Month]string{
	time.January:   "janvier",
	time.February:  "février",
	time.March:     "mars",
	time.April:     "avril",
	time.May:       "mai",
	time.June:      "juin",
	time.July:      "juillet",
	time.August:    "août",
	time.September: "septembre",
	time.October:   "octobre",
	time.November:  "novembre",
	time.December:  "décembre",
}

// FormatDateLabelFR formate une date en français long (ex: "vendredi 12
// juillet 2026"), utilisé par les messages de communication (email/SMS)
// envoyés au client à propos d'une réservation.
func FormatDateLabelFR(t time.Time) string {
	return fmt.Sprintf("%s %d %s %d", frWeekdays[t.Weekday()], t.Day(), frMonths[t.Month()], t.Year())
}
