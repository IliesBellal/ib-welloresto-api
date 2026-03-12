package helpers

import (
	"strings"
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

func Ucfirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
