package importer

import (
	"regexp"
	"strings"
	"time"

	"welloresto-api/internal/helpers"
)

// Codes de warning stables, réutilisés par la preview (phase 3) pour
// regrouper/afficher les anomalies. Les valeurs sont des chaînes plutôt que
// des iota : elles transitent potentiellement par JSON/Redis en aval, comme
// les codes de warning du produit.
const (
	WarnMissingContact              = "missing_contact"
	WarnMissingName                 = "missing_name"
	WarnInvalidEmail                = "invalid_email"
	WarnInvalidPhone                = "invalid_phone"
	WarnUnparseableBirthdate        = "unparseable_birthdate"
	WarnUnparseableRegistrationDate = "unparseable_registration_date"
)

// frenchDateLayout est le format des colonnes de date des exports connus
// (Zelty, template Wello) : JJ/MM/AAAA.
const frenchDateLayout = "02/01/2006"

// displayNameMaxLen borne buildDisplayName : customer.customer_name est
// varchar(50).
const displayNameMaxLen = 50

// emailPattern est une validation SIMPLE et locale, volontairement pas la
// regex du module facturation (order_life_cycle) : ce paquet ne dépend
// d'aucun autre module métier, et une validation d'import n'a pas besoin
// d'être RFC-complète, seulement d'écarter les saisies manifestement
// invalides.
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// normalizePhoneFR normalise un numéro de téléphone en supposant la France à
// défaut d'indicatif. La valeur normalisée best-effort est toujours rendue,
// même quand elle n'est pas plausible : une saisie imparfaite reste
// consultable et corrigible, elle ne doit pas disparaître silencieusement.
//
// ok=false signale l'implausibilité (WarnInvalidPhone côté appelant), sans
// empêcher l'import pour un provider tolérant (Zelty).
func normalizePhoneFR(raw string) (normalized string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	normalized = helpers.NormalizePhoneNumber(trimmed, "FR")
	_, err := helpers.FormatToE164(trimmed, "FR")
	return normalized, err == nil
}

// emailState distingue une cellule vide (absence légitime, pas d'anomalie)
// d'une cellule renseignée mais malformée (anomalie, à signaler).
type emailState int

const (
	emailAbsent emailState = iota
	emailValid
	emailInvalid
)

// validateEmail vérifie la plausibilité d'une adresse email, sans en changer
// la casse : la comparaison insensible à la casse est un sujet de
// rapprochement (phase 3), pas de parsing.
func validateEmail(raw string) (cleaned string, state emailState) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", emailAbsent
	}
	if !emailPattern.MatchString(trimmed) {
		return "", emailInvalid
	}
	return trimmed, emailValid
}

// parseFrenchDate lit une date au format JJ/MM/AAAA. Une cellule vide est une
// absence légitime (ok=true, *time.Time nil) à distinguer d'une valeur
// illisible (ok=false), qui déclenche un warning côté appelant.
func parseFrenchDate(raw string) (*time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, true
	}

	t, err := time.Parse(frenchDateLayout, trimmed)
	if err != nil {
		return nil, false
	}
	return &t, true
}

// parseConsent traduit les deux colonnes d'opt-in Zelty en un consentement
// publicitaire unique. Décision figée : l'un OU l'autre à "Oui" vaut
// consentement, toute autre valeur (vide, "Non") vaut refus — jamais nil,
// le commit (phase 4) a besoin d'une valeur explicite.
func parseConsent(optinMail, optinSMS string) *bool {
	consent := strings.EqualFold(strings.TrimSpace(optinMail), "Oui") ||
		strings.EqualFold(strings.TrimSpace(optinSMS), "Oui")
	return &consent
}

// buildDisplayName assemble le nom d'affichage à partir du prénom et du nom.
// Tronqué à 50 caractères : customer.customer_name est varchar(50), un nom
// composé trop long ne doit pas faire échouer l'insertion en base.
func buildDisplayName(firstName, lastName string) string {
	name := strings.Join(strings.Fields(strings.TrimSpace(firstName)+" "+strings.TrimSpace(lastName)), " ")

	runes := []rune(name)
	if len(runes) > displayNameMaxLen {
		runes = runes[:displayNameMaxLen]
	}
	return string(runes)
}
