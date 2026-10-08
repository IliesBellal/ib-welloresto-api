package config

import (
	"os"
	"strings"
)

// AttestationConfig règle la génération des attestations individuelles de
// l'éditeur (conformité caisse lot F, modèle BOI-LETTRE-000242,
// docs/attestation-conformite-07-lot-F-brief.md). L'attestation engage
// pénalement le représentant légal de l'éditeur (C. pén. art. 441-1) : elle
// reste fermée tant que ATTESTATION_ENABLED n'est pas « true » (après la mise
// en production des lots A à F et un contrôle d'intégrité sans erreur sur les
// données réelles).
//
// Identité de l'éditeur (décision d'Ilies, 2026-10-08) : BINYA, représentée
// par Ilies BELLAL, à Metz ; version 2.1.6 mise sur le marché le 15/07/2026.
// Chaque valeur peut être remplacée par sa variable d'environnement.
type AttestationConfig struct {
	Enabled bool
	// Volet 1 : « Je soussigné, NOM Prénom, représentant légal de la société
	// RAISON SOCIALE, éditeur… », « Fait à (VILLE) ».
	EditorRepresentative string
	EditorCompany        string
	EditorCity           string
	// EditorSignatureKey : clé, dans le bucket R2 privé, d'une image PNG de
	// signature qui remplace celle intégrée au binaire. Facultative.
	EditorSignatureKey string
	// ReleaseDate : date de mise sur le marché de la version attestée
	// (AAAA-MM-JJ).
	ReleaseDate string
}

// Complete : identité de l'éditeur et date de mise sur le marché renseignées.
func (c AttestationConfig) Complete() bool {
	return c.EditorRepresentative != "" && c.EditorCompany != "" && c.EditorCity != "" && c.ReleaseDate != ""
}

func loadAttestationConfig() AttestationConfig {
	env := func(k, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
		return fallback
	}
	return AttestationConfig{
		Enabled:              strings.EqualFold(env("ATTESTATION_ENABLED", ""), "true"),
		EditorRepresentative: env("ATTESTATION_EDITOR_REPRESENTATIVE", "BELLAL Ilies"),
		EditorCompany:        env("ATTESTATION_EDITOR_COMPANY", "BINYA"),
		EditorCity:           env("ATTESTATION_EDITOR_CITY", "Metz"),
		EditorSignatureKey:   env("ATTESTATION_EDITOR_SIGNATURE_KEY", ""),
		ReleaseDate:          env("ATTESTATION_RELEASE_DATE", "2026-07-15"),
	}
}
