// Package attestations génère les attestations individuelles de l'éditeur
// (conformité caisse lot F ; BOI-TVA-DECLA-30-10-30 §270 à §375 ; modèle
// BOI-LETTRE-000242) : le restaurateur obtient lui-même la sienne, depuis le
// back-office ou la caisse. Volet 1 pré-rempli et pré-signé par l'éditeur
// (§370), volet 2 complété par l'établissement et signé électroniquement par
// son représentant légal. Voir docs/attestation-conformite-07-lot-F-brief.md.
package attestations

// Document porte toutes les valeurs reportées dans l'attestation. Il est
// conservé tel quel (attestations.content) pour reproduire le document.
type Document struct {
	Reference string `json:"reference"`

	// Volet 1 : éditeur et logiciel.
	EditorRepresentative string   `json:"editor_representative"`
	EditorCompany        string   `json:"editor_company"`
	EditorCity           string   `json:"editor_city"`
	EditorSignedOn       string   `json:"editor_signed_on"` // JJ/MM/AAAA
	Software             string   `json:"software"`
	SoftwareDescription  string   `json:"software_description"`
	Version              string   `json:"version"`
	MajorRoot            string   `json:"major_root"`
	MinorPattern         string   `json:"minor_pattern"`
	ReleaseDate          string   `json:"release_date"` // JJ/MM/AAAA
	Licence              string   `json:"licence"`
	Covered              []string `json:"covered"`
	NotCovered           []string `json:"not_covered"`

	// Volet 2 : établissement et son représentant légal.
	MerchantID      string `json:"merchant_id"`
	CompanyName     string `json:"company_name"`
	Siret           string `json:"siret"`
	Address         string `json:"address"`
	City            string `json:"city"`
	SignerName      string `json:"signer_name"`
	AcquisitionDate string `json:"acquisition_date"` // JJ/MM/AAAA
	UsageStartDate  string `json:"usage_start_date"` // JJ/MM/AAAA
	SignedOn        string `json:"signed_on"`        // JJ/MM/AAAA
	SignedAt        string `json:"signed_at"`        // JJ/MM/AAAA à HH:MM (heure locale)
	SignerAccount   string `json:"signer_account"`   // compte connecté qui a signé
	IntegrityCheck  string `json:"integrity_check"`  // résultat du contrôle préalable
}

// SoftwareDescription : « nom et références caractérisant le logiciel ou
// système » du modèle.
const SoftwareDescription = "système de caisse en ligne : API WelloResto, application de caisse, borne de commande, commande en ligne et à table ScanNOrder"

// Covered : périmètre couvert (modèle : « Le périmètre couvert par cette
// attestation concerne les fonctionnalités suivantes »). Décision d'Ilies
// (F4, 2026-10-08) : le strict nécessaire fiscal, les fonctionnalités de
// caisse, sur tous les canaux qui enregistrent un règlement client.
var Covered = []string{
	"Les fonctionnalités de caisse : enregistrement des règlements des clients et émission des tickets et avoirs (application de caisse, borne de commande, commande en ligne ScanNOrder, commandes des plateformes de livraison intégrées), avec leurs clôtures, leur journal et leur archivage.",
}

// NotCovered : fonctionnalités non couvertes (modèle : « Les fonctionnalités
// suivantes ne sont pas couvertes par cette attestation »).
var NotCovered = []string{
	"Toutes les autres fonctionnalités du logiciel, qui n'enregistrent pas de règlement client.",
}
