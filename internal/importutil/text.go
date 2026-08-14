package importutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// maxSlugLen borne la partie lisible d'un identifiant généré. Les colonnes
// external_id des tables import_*_mapping sont en varchar(64) : avec un
// préfixe court et une empreinte de 8, on reste sous la limite.
const maxSlugLen = 40

// generatedIDHashLen est la longueur de l'empreinte hexadécimale accolée au
// slug. Elle désambiguë deux libellés dont le slug serait identique
// ("Pizza 4 fromages" et "Pizza 4 Fromages !") sans rendre l'identifiant
// illisible en preview.
const generatedIDHashLen = 8

// frenchDiacritics ramène les lettres accentuées françaises à leur base. Sert
// aux en-têtes de colonnes et aux slugs, jamais à la comparaison de libellés
// métier : "VEGE" et "VÉGÉ" sont deux tags distincts et doivent le rester.
var frenchDiacritics = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", // a grave, circonflexe, tréma
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i",
	"ô", "o", "ö", "o",
	"ù", "u", "û", "u", "ü", "u",
	"ÿ", "y", "ç", "c",
	"œ", "oe", "æ", "ae",
)

// SplitLabels découpe une liste de libellés séparés par des virgules.
func SplitLabels(raw string) []string {
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

// NormalizeLabel produit la clé de rapprochement d'un libellé : casse et
// espacement neutralisés, accents conservés.
func NormalizeLabel(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// FoldHeader normalise un en-tête de colonne. Contrairement à NormalizeLabel,
// les accents sont repliés : un restaurateur qui retape "Categorie" sans
// accent dans le template doit être compris.
func FoldHeader(s string) string {
	return frenchDiacritics.Replace(NormalizeLabel(s))
}

// Slugify rend la partie lisible d'un identifiant généré.
func Slugify(s string) string {
	folded := FoldHeader(s)

	var b strings.Builder
	lastDash := true // évite le tiret de tête
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

// GeneratedExternalID fabrique un identifiant externe pour une source qui
// n'en fournit pas. Il doit être stable dans le temps : c'est lui qui porte
// l'idempotence via la clé (merchant_id, provider, external_id), donc deux
// imports successifs du même libellé doivent produire la même valeur.
func GeneratedExternalID(prefix, name string) string {
	sum := sha256.Sum256([]byte(NormalizeLabel(name)))
	fingerprint := hex.EncodeToString(sum[:])[:generatedIDHashLen]

	slug := Slugify(name)
	if slug == "" {
		// Un libellé entièrement composé de caractères non latins (emoji,
		// idéogrammes) n'a pas de slug exploitable : l'empreinte suffit.
		return prefix + "-" + fingerprint
	}
	return prefix + "-" + slug + "-" + fingerprint
}
