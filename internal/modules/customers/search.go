package customers

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Recherche de clients (POS et back-office) — voir docs/recherche-clients.md.
//
// La saisie est découpée en mots ; CHAQUE mot doit correspondre quelque part
// (prénom/nom, email, téléphone, code), dans n'importe quel ordre. La pertinence
// est calculée en SQL (somme, par mot, de la meilleure correspondance) pour que
// le LIMIT ne garde que les meilleurs candidats : calculée après coup en Go,
// elle ne voyait que la page déjà tronquée.
//
// Les fonctions customer_search_norm / customer_search_name / customer_search_phone
// et les trois index trigramme viennent de la migration 198 : les expressions
// ci-dessous doivent rester IDENTIQUES à celles des index pour qu'ils servent.

const (
	searchMaxTermRunes   = 100
	searchMaxTokens      = 5
	searchMaxTokenRunes  = 40
	searchMinTextRunes   = 2
	searchMinPhoneDigits = 3
	// Au-delà de 4 caractères un mot ne peut plus être un code client (varchar(4)).
	searchCodeMaxRunes = 4
	// Un email n'est cherché qu'à partir de 3 caractères : en dessous, « ma »
	// remonterait tous les « gmail ».
	searchEmailMinRunes = 3

	// France : indicatif des numéros stockés au format international (+33…).
	searchFRCountryDigits = "33"

	// Barème de pertinence (somme sur les mots). Un score >= 200 signifie
	// « correspondance forte » (utilisé tel quel par le badge du back-office).
	scoreCodeExact      = 1000
	scorePhoneExact     = 600
	scorePhoneEdge      = 450 // début d'un numéro international, ou fin saisie en chiffres seuls
	scorePhoneNationalS = 400 // fin de numéro commençant par 0 (« 0512 » en dernier bloc)
	scorePhonePrefix    = 300
	scorePhoneContains  = 150
	scoreWordExact      = 500 // le mot est exactement un prénom/nom
	scoreWordPrefix     = 300 // un prénom/nom commence par le mot
	scoreNameContains   = 100
	scoreEmailPrefix    = 80
	scoreEmailContains  = 40
)

// Les trois expressions suivantes sont indexées par la migration 198.
const (
	sqlSearchName  = `customer_search_name(c.customer_first_name, c.customer_last_name, c.customer_name)`
	sqlSearchPhone = `customer_search_phone(c.customer_tel)`
	sqlSearchEmail = `lower(c.customer_email)`
)

type searchTokenKind int

const (
	searchTokenText searchTokenKind = iota
	searchTokenPhone
)

type searchToken struct {
	kind searchTokenKind
	// raw : mot tel que saisi (fusionné pour un numéro tapé avec des espaces).
	raw string
	// digits : chiffres seuls (téléphone). "00" initial retiré pour l'international.
	digits string
	// international : saisi avec « + » ou « 00 » (le début du numéro est connu).
	international bool
	// national : commence par un 0 (« 06… ») → équivalent stocké « 336… ».
	national bool
}

// searchSQL est la partie dynamique de la requête de recherche, dans l'ordre
// d'apparition des `?` : scoreArgs (SELECT) puis whereArgs (WHERE).
type searchSQL struct {
	score     string
	scoreArgs []interface{}
	where     string
	whereArgs []interface{}
}

// parseSearchTerm découpe la saisie en mots exploitables. Les mots trop courts
// (1 caractère, moins de 3 chiffres) sont ignorés : ils correspondraient à
// presque tout le fichier client. Un résultat vide signifie « ne pas interroger ».
func parseSearchTerm(term string) []searchToken {
	term = truncateRunes(strings.TrimSpace(term), searchMaxTermRunes)
	fields := strings.Fields(term)

	// « 06 12 34 56 78 » arrive en cinq mots : les blocs de chiffres adjacents
	// sont refusionnés en un seul numéro.
	var merged []string
	for _, f := range fields {
		if n := len(merged); n > 0 && looksLikePhoneSearch(f) && looksLikePhoneSearch(merged[n-1]) {
			merged[n-1] += f
			continue
		}
		merged = append(merged, f)
	}

	var tokens []searchToken
	for _, f := range merged {
		if len(tokens) == searchMaxTokens {
			break
		}
		f = truncateRunes(f, searchMaxTokenRunes)
		if looksLikePhoneSearch(f) {
			if t, ok := newPhoneToken(f); ok {
				tokens = append(tokens, t)
			}
			continue
		}
		if utf8.RuneCountInString(f) >= searchMinTextRunes {
			tokens = append(tokens, searchToken{kind: searchTokenText, raw: f})
		}
	}
	return tokens
}

func newPhoneToken(raw string) (searchToken, bool) {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()

	t := searchToken{kind: searchTokenPhone, raw: raw}
	switch {
	case strings.HasPrefix(raw, "+"):
		t.international = true
	case strings.HasPrefix(d, "00"):
		t.international = true
		d = d[2:]
	case strings.HasPrefix(d, "0"):
		t.national = true
	}
	t.digits = d
	return t, len(d) >= searchMinPhoneDigits
}

// buildSearchSQL assemble le score et le filtre pour les mots donnés.
// tokens doit être non vide (voir parseSearchTerm).
func buildSearchSQL(tokens []searchToken) searchSQL {
	var out searchSQL
	var scores, conds []string

	for _, t := range tokens {
		var score, cond string
		var sArgs, cArgs []interface{}
		if t.kind == searchTokenPhone {
			score, sArgs, cond, cArgs = phoneTokenSQL(t)
		} else {
			score, sArgs, cond, cArgs = textTokenSQL(t)
		}
		scores = append(scores, score)
		out.scoreArgs = append(out.scoreArgs, sArgs...)
		conds = append(conds, cond)
		out.whereArgs = append(out.whereArgs, cArgs...)
	}

	out.score = "(" + strings.Join(scores, " + ") + ")"
	out.where = " AND (" + strings.Join(conds, ") AND (") + ")"
	return out
}

func textTokenSQL(t searchToken) (score string, scoreArgs []interface{}, cond string, condArgs []interface{}) {
	esc := escapeLike(t.raw)
	emailEsc := escapeLike(strings.ToLower(t.raw))
	runes := utf8.RuneCountInString(t.raw)
	norm := `customer_search_norm(?)`
	padded := `(' ' || ` + sqlSearchName + ` || ' ')`

	// Filtre : le mot est contenu dans le nom, ou dans l'email, ou est le code.
	conds := []string{sqlSearchName + ` LIKE '%' || ` + norm + ` || '%'`}
	condArgs = append(condArgs, esc)

	// Score : meilleure des correspondances possibles pour ce mot.
	var scores []string
	if runes <= searchCodeMaxRunes {
		conds = append(conds, `c.customer_code = ?`)
		condArgs = append(condArgs, t.raw)
		scores = append(scores, `CASE WHEN c.customer_code = ? THEN `+itoa(scoreCodeExact)+` ELSE 0 END`)
		scoreArgs = append(scoreArgs, t.raw)
	}
	scores = append(scores, `CASE`+
		` WHEN `+padded+` LIKE '% ' || `+norm+` || ' %' THEN `+itoa(scoreWordExact)+
		` WHEN `+padded+` LIKE '% ' || `+norm+` || '%' THEN `+itoa(scoreWordPrefix)+
		` WHEN `+sqlSearchName+` LIKE '%' || `+norm+` || '%' THEN `+itoa(scoreNameContains)+
		` ELSE 0 END`)
	scoreArgs = append(scoreArgs, esc, esc, esc)

	if runes >= searchEmailMinRunes {
		conds = append(conds, sqlSearchEmail+` LIKE ?`)
		condArgs = append(condArgs, "%"+emailEsc+"%")
		scores = append(scores, `CASE`+
			` WHEN `+sqlSearchEmail+` LIKE ? THEN `+itoa(scoreEmailPrefix)+
			` WHEN `+sqlSearchEmail+` LIKE ? THEN `+itoa(scoreEmailContains)+
			` ELSE 0 END`)
		scoreArgs = append(scoreArgs, emailEsc+"%", "%"+emailEsc+"%")
	}

	return `GREATEST(` + strings.Join(scores, ", ") + `)`, scoreArgs, strings.Join(conds, " OR "), condArgs
}

func phoneTokenSQL(t searchToken) (score string, scoreArgs []interface{}, cond string, condArgs []interface{}) {
	d := t.digits
	ph := sqlSearchPhone

	var conds []string
	var whens []string
	when := func(sqlCond string, points int, arg interface{}) {
		whens = append(whens, `WHEN `+sqlCond+` THEN `+itoa(points))
		scoreArgs = append(scoreArgs, arg)
	}

	// Un numéro de 4 chiffres peut aussi être un code client.
	if len(d) == searchCodeMaxRunes && !t.international {
		conds = append(conds, `c.customer_code = ?`)
		condArgs = append(condArgs, d)
		when(`c.customer_code = ?`, scoreCodeExact, d)
	}

	switch {
	case t.national:
		// « 0612… » : stocké « 33612… » (prefixe FR) ou, pour les anciens
		// numéros, tel quel. Une saisie partielle reste un début de numéro.
		fr := searchFRCountryDigits + d[1:]
		conds = append(conds, ph+` LIKE ?`, ph+` LIKE ?`)
		condArgs = append(condArgs, fr+"%", "%"+d+"%")
		when(ph+` = ?`, scorePhoneExact, fr)
		when(ph+` = ?`, scorePhoneExact, d)
		when(ph+` LIKE ?`, scorePhoneEdge, fr+"%")
		when(ph+` LIKE ?`, scorePhoneEdge, d+"%")
		when(ph+` LIKE ?`, scorePhoneNationalS, "%"+d)
		when(ph+` LIKE ?`, scorePhoneContains, "%"+d+"%")
	case t.international:
		conds = append(conds, ph+` LIKE ?`)
		condArgs = append(condArgs, "%"+d+"%")
		when(ph+` = ?`, scorePhoneExact, d)
		when(ph+` LIKE ?`, scorePhoneEdge, d+"%")
		when(ph+` LIKE ?`, scorePhoneContains, "%"+d+"%")
	default:
		// Chiffres seuls sans 0 initial : le plus souvent la fin du numéro.
		conds = append(conds, ph+` LIKE ?`)
		condArgs = append(condArgs, "%"+d+"%")
		when(ph+` = ?`, scorePhoneExact, d)
		when(ph+` LIKE ?`, scorePhoneEdge, "%"+d)
		when(ph+` LIKE ?`, scorePhonePrefix, d+"%")
		when(ph+` LIKE ?`, scorePhoneContains, "%"+d+"%")
	}

	return `CASE ` + strings.Join(whens, " ") + ` ELSE 0 END`, scoreArgs, strings.Join(conds, " OR "), condArgs
}

// looksLikePhoneSearch : la saisie ne contient que des chiffres et la
// ponctuation d'un numéro (+ espace - . ( )), avec au moins un chiffre.
func looksLikePhoneSearch(term string) bool {
	hasDigit := false
	for _, r := range term {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '+' || r == ' ' || r == '-' || r == '.' || r == '(' || r == ')':
			continue
		default:
			return false
		}
	}
	return hasDigit
}

// escapeLike neutralise les jokers LIKE (\ % _) d'une saisie utilisateur.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func itoa(n int) string { return strconv.Itoa(n) }
