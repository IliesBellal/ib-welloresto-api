// Package version porte le nom et le numéro de version du logiciel de caisse
// attesté (BOI-TVA-DECLA-30-10-30 §340 et §375 : version majeure / mineure).
//
// Numérotation (décision d'Ilies, 2026-10-08, docs/versions-logiciel.md) :
// « WelloResto » MAJEUR.MINEUR.CORRECTIF, racine majeure 2. 2.1.6 (numéro
// choisi par Ilies le 2026-10-08) est la première version attestée : mise en
// production unique des lots A à F. Un changement des conditions
// d'inaltérabilité, de sécurisation, de conservation ou d'archivage donne une
// nouvelle version majeure (3.0.0) et une nouvelle attestation ; tout autre
// changement reste dans 2.x.y.
//
// La valeur peut être injectée au build :
//
//	go build -ldflags "-X welloresto-api/internal/version.Version=2.1.7" ./cmd/api
package version

import "strings"

// Product est le nom commercial du logiciel attesté.
const Product = "WelloResto"

// Version est le numéro de version du logiciel.
var Version = "2.1.6"

// MajorRoot est la racine de la version majeure (« 2 » pour 2.1.6), portée
// par l'attestation : les versions mineures en sont les subdivisions.
func MajorRoot() string {
	root, _, _ := strings.Cut(Version, ".")
	return root
}

// MinorPattern décrit les subdivisions réservées aux versions mineures
// (« 2.x.y »).
func MinorPattern() string {
	return MajorRoot() + ".x.y"
}
