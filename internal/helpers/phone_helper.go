package helpers

import (
	"errors"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// Map des pays limitrophes (et proches)
var countryPrefixes = map[string]string{
	"FR": "+33",  // France
	"BE": "+32",  // Belgique
	"CH": "+41",  // Suisse
	"LU": "+352", // Luxembourg
	"MC": "+377", // Monaco
	"ES": "+34",  // Espagne
	"IT": "+39",  // Italie
	"DE": "+49",  // Allemagne
	"GB": "+44",  // Royaume-Uni
}

func NormalizePhoneNumber(phone string, countryCode string) string {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))

	// 1. Nettoyage : On ne garde que les chiffres et le '+'
	clean := ""
	for _, char := range phone {
		if (char >= '0' && char <= '9') || char == '+' {
			clean += string(char)
		}
	}

	// 2. Si c'est déjà international (+ ou 00)
	if strings.HasPrefix(clean, "+") {
		return clean
	}
	if strings.HasPrefix(clean, "00") {
		return "+" + clean[2:]
	}

	// 3. Récupération de l'indicatif
	prefix, exists := countryPrefixes[countryCode]
	if !exists || countryCode == "" {
		return phone // Pas de pays ou pays inconnu -> on ne touche à rien
	}

	// 4. Gestion du '0' de début (Règles spécifiques)
	if strings.HasPrefix(clean, "0") {
		// EXCEPTION : En Italie (+39), on ne retire JAMAIS le 0 initial
		// pour les numéros fixes (et certains mobiles).
		// Dans le doute pour l'Italie, on garde la structure telle quelle.
		if countryCode != "IT" {
			clean = clean[1:]
		}
	}

	return prefix + clean
}

func normalizeRegionCode(region string) string {
	r := strings.ToUpper(strings.TrimSpace(region))
	if len(r) == 2 {
		return r
	}

	fullToISO := map[string]string{
		"FRANCE":         "FR",
		"BELGIUM":        "BE",
		"BELGIQUE":       "BE",
		"SWITZERLAND":    "CH",
		"SUISSE":         "CH",
		"LUXEMBOURG":     "LU",
		"MONACO":         "MC",
		"SPAIN":          "ES",
		"ESPAGNE":        "ES",
		"ITALY":          "IT",
		"ITALIE":         "IT",
		"GERMANY":        "DE",
		"ALLEMAGNE":      "DE",
		"UNITED KINGDOM": "GB",
		"ROYAUME-UNI":    "GB",
		"GREAT BRITAIN":  "GB",
	}

	if iso, ok := fullToISO[r]; ok {
		return iso
	}

	return ""
}

// FormatToE164 parses and validates a phone number, then formats it to E.164.
// If the number is local (no international prefix), defaultRegion is used.
func FormatToE164(phone string, defaultRegion string) (string, error) {
	clean := strings.TrimSpace(phone)
	if clean == "" {
		return "", errors.New("invalid_phone_format")
	}

	if strings.HasPrefix(clean, "00") {
		clean = "+" + strings.TrimPrefix(clean, "00")
	}

	region := normalizeRegionCode(defaultRegion)
	if region == "" {
		region = "ZZ"
	}

	number, err := phonenumbers.Parse(clean, region)
	if err != nil {
		return "", errors.New("invalid_phone_format")
	}

	if !phonenumbers.IsPossibleNumber(number) || !phonenumbers.IsValidNumber(number) {
		return "", errors.New("invalid_phone_format")
	}

	return phonenumbers.Format(number, phonenumbers.E164), nil
}

func Ucfirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
