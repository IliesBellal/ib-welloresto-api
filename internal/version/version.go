// Package version porte le nom et le numéro de version du logiciel de caisse
// attesté (BOI-TVA-DECLA-30-10-30 §340 et §375 : version majeure / mineure).
//
// Numérotation (décision d'Ilies, 2026-10-08, docs/attestation-conformite-04-plan-D-E-F.md) :
// « WelloResto » 2.MINEUR.CORRECTIF. 2.0.0 est la première version attestée
// (mise en production unique des lots A à F) ; un changement des conditions
// d'inaltérabilité, de sécurisation, de conservation ou d'archivage donne une
// nouvelle version majeure (3.0.0) et une nouvelle attestation.
//
// La valeur définitive est fixée au lot F ; elle peut être injectée au build :
//
//	go build -ldflags "-X welloresto-api/internal/version.Version=2.0.0" ./cmd/api
package version

// Product est le nom commercial du logiciel attesté.
const Product = "WelloResto"

// Version est le numéro de version du logiciel (racine majeure 2).
var Version = "2.0.0-dev"
