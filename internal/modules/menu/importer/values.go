package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// maxSlugLen borne la partie lisible d'un identifiant genere. Les colonnes
// external_id des tables import_*_mapping sont en varchar(64) : avec un prefixe
// de 5 caracteres et une empreinte de 8, on reste sous la limite.
const maxSlugLen = 40

// generatedIDHashLen est la longueur de l'empreinte hexadecimale accolee au
// slug. Elle desambigue deux libelles dont le slug serait identique
// ("Pizza 4 fromages" et "Pizza 4 Fromages !") sans rendre l'identifiant
// illisible en preview.
const generatedIDHashLen = 8

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

// frenchDiacritics ramene les lettres accentuees francaises a leur base. Sert
// aux en-tetes de colonnes et aux slugs, jamais a la comparaison de libelles
// metier : "VEGE" et "VEGE" accentue sont deux tags distincts et doivent le
// rester.
var frenchDiacritics = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", // a grave, circonflexe, trema
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i",
	"ô", "o", "ö", "o",
	"ù", "u", "û", "u", "ü", "u",
	"ÿ", "y", "ç", "c",
	"œ", "oe", "æ", "ae",
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

// splitLabels decoupe une liste de libelles separes par des virgules.
func splitLabels(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		if label := strings.TrimSpace(part); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

// normalizeLabel produit la cle de rapprochement d'un libelle : casse et
// espacement neutralises, accents conserves.
func normalizeLabel(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// foldHeader normalise un en-tete de colonne. Contrairement a normalizeLabel,
// les accents sont replies : un restaurateur qui retape "Categorie" sans
// accent dans le template doit etre compris.
func foldHeader(s string) string {
	return frenchDiacritics.Replace(normalizeLabel(s))
}

// slugify rend la partie lisible d'un identifiant genere.
func slugify(s string) string {
	folded := foldHeader(s)

	var b strings.Builder
	lastDash := true // evite le tiret de tete
	for _, r := range folded {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= maxSlugLen {
			break
		}
	}

	return strings.Trim(b.String(), "-")
}

// generatedExternalID fabrique un identifiant externe pour une source qui n'en
// fournit pas. Il doit etre stable dans le temps : c'est lui qui porte
// l'idempotence via la cle (merchant_id, provider, external_id), donc deux
// imports successifs du meme libelle doivent produire la meme valeur.
func generatedExternalID(prefix, name string) string {
	sum := sha256.Sum256([]byte(normalizeLabel(name)))
	fingerprint := hex.EncodeToString(sum[:])[:generatedIDHashLen]

	slug := slugify(name)
	if slug == "" {
		// Un libelle entierement compose de caracteres non latins (emoji,
		// ideogrammes) n'a pas de slug exploitable : l'empreinte suffit.
		return prefix + "-" + fingerprint
	}
	return prefix + "-" + slug + "-" + fingerprint
}
