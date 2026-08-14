package importer

import (
	"fmt"
	"strconv"
	"strings"
)

// numericCleaner retire de la saisie tout ce qui n'appartient pas au nombre :
// separateurs de milliers (espaces ordinaire, insecable, insecable etroit,
// fine), symbole monetaire, signe pourcent. La virgule decimale francaise est
// ramenee au point.
var numericCleaner = strings.NewReplacer(
	" ", "", // espace ordinaire
	" ", "", // espace insecable
	" ", "", // espace insecable etroit
	" ", "", // espace fine
	"€", "", // symbole euro
	"%", "",
	",", ".",
)

// parsePriceCents convertit un montant en centimes entiers.
//
// La conversion est faite sur la chaine, pas via un float : 9,9 * 100 vaut
// 989,999... en binaire, et un arrondi sur ce resultat est une roulette russe
// pour des montants qui finissent en base. On decoupe donc partie entiere et
// decimales, avec un arrondi au demi superieur sur la troisieme decimale.
//
// Une cellule vide vaut 0 : c'est le cas legitime des lignes de frais et des
// options sans supplement, pas une anomalie.
func parsePriceCents(raw string) (int, error) {
	s := numericCleaner.Replace(strings.TrimSpace(raw))
	if s == "" {
		return 0, nil
	}

	negative := false
	switch {
	case strings.HasPrefix(s, "-"):
		negative, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}

	whole, frac, hasFrac := strings.Cut(s, ".")
	if strings.Contains(frac, ".") {
		return 0, fmt.Errorf("montant %q: deux separateurs decimaux", raw)
	}
	if whole == "" && frac == "" {
		return 0, fmt.Errorf("montant %q: aucun chiffre", raw)
	}
	if !isDigits(whole) || !isDigits(frac) {
		return 0, fmt.Errorf("montant %q: caracteres non numeriques", raw)
	}
	if hasFrac && frac == "" {
		// "12," est tolere : le separateur seul ne porte pas d'information.
		frac = "0"
	}

	cents := 0
	if whole != "" {
		units, err := strconv.Atoi(whole)
		if err != nil {
			return 0, fmt.Errorf("montant %q: %w", raw, err)
		}
		cents = units * 100
	}

	switch {
	case len(frac) == 1:
		cents += digit(frac[0]) * 10
	case len(frac) >= 2:
		cents += digit(frac[0])*10 + digit(frac[1])
		if len(frac) > 2 && frac[2] >= '5' {
			cents++
		}
	}

	if negative {
		cents = -cents
	}
	return cents, nil
}

// parseTvaRate lit un taux de TVA en pourcentage. Une cellule vide rend nil
// (colonne absente ou non renseignee), a distinguer d'un 0 explicite qui vaut
// desactivation du canal. Aucune resolution vers tva_categories ici.
func parseTvaRate(raw string) (*float64, error) {
	s := numericCleaner.Replace(strings.TrimSpace(raw))
	if s == "" {
		return nil, nil
	}

	rate, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("taux %q: valeur non numerique", raw)
	}
	if rate < 0 {
		return nil, fmt.Errorf("taux %q: valeur negative", raw)
	}
	return &rate, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func digit(b byte) int { return int(b - '0') }
