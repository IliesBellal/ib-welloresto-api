package importer

import (
	"fmt"
	"strings"
)

// Statuts possibles d'une ligne de preview. Un seul par ligne, calculé selon
// l'ordre de priorité documenté sur BuildPreview.
const (
	StatusCreate          = "create"
	StatusAlreadyImported = "already_imported"
	StatusMappingStale    = "mapping_stale"
	StatusDuplicate       = "duplicate"
	StatusConflict        = "conflict"
)

// MatchedBy identifie le champ qui a produit un rapprochement "duplicate".
const (
	MatchedByEmail = "email"
	MatchedByPhone = "phone"
	MatchedByBoth  = "both"
)

// Résolutions proposées par défaut, et options que le wizard peut choisir à
// la place avant de rejouer le commit (phase 4). Seuls les défauts sont émis
// ici ; les options sont documentées pour que le front-end les propose sans
// deviner les chaînes attendues.
const (
	ResolutionCreate        = "create"          // défaut de "create"
	ResolutionSkip          = "skip"            // défaut de already_imported/duplicate/conflict
	ResolutionRecreate      = "recreate"        // défaut de mapping_stale
	ResolutionUpdate        = "update"          // option "duplicate"
	ResolutionImportAnyway  = "import_anyway"   // option "duplicate"/"already_imported"
	ResolutionUpdateToEmail = "update_to_email" // option "conflict"
	ResolutionUpdateToPhone = "update_to_phone" // option "conflict"
)

// Codes de warning posés par la preview, en plus de ceux remontés par le
// parser (WarnMissingContact, WarnInvalidEmail, etc., voir values.go).
const (
	// WarnDuplicateConflict signale une ligne "conflict" : email et téléphone
	// désignent deux clients différents, aucun arbitrage automatique n'est
	// possible.
	WarnDuplicateConflict = "duplicate_conflict"

	// WarnIntraFileSharedPhone est informatif, non bloquant : plusieurs lignes
	// destinées à être créées partagent le même téléphone normalisé dans le
	// fichier lui-même (652 cas dans l'export Zelty réel analysé). Rien
	// n'empêche l'import ; c'est un signal pour le restaurateur qui relit la
	// preview.
	WarnIntraFileSharedPhone = "intra_file_shared_phone"
)

// MappingEntry est ce que charge LoadImportMappings pour un external_id :
// l'identifiant du client Wello créé par un import précédent, et si ce
// client existe encore (enabled) — un mapping peut survivre à la suppression
// du client qu'il désigne.
type MappingEntry struct {
	CustomerID   int
	TargetExists bool
}

// PreviewLookups est tout ce que BuildPreview lit de la base, déjà chargé et
// scopé au marchand par l'appelant. Les clés sont déjà normalisées de la même
// façon que le canonique (email en minuscule, téléphone au format
// normalizePhoneFR) : BuildPreview ne renormalise rien.
type PreviewLookups struct {
	ByEmail   map[string]int
	ByPhone   map[string]int
	ByMapping map[string]MappingEntry
}

// PreviewSummary compte les lignes par statut. Total est la somme des cinq
// autres compteurs.
type PreviewSummary struct {
	Total           int `json:"total"`
	ToCreate        int `json:"to_create"`
	Duplicates      int `json:"duplicates"`
	Conflicts       int `json:"conflicts"`
	AlreadyImported int `json:"already_imported"`
	MappingStale    int `json:"mapping_stale"`
}

// PreviewRow est le résultat d'une ligne du fichier, dans l'ordre du fichier.
type PreviewRow struct {
	ExternalID  string `json:"external_id"`
	SourceLine  int    `json:"source_line"`
	DisplayName string `json:"display_name"`

	Status string `json:"status"`

	// MatchedBy et MatchedCustomerID ne sont renseignés que pour "duplicate".
	MatchedBy         string `json:"matched_by,omitempty"`
	MatchedCustomerID int    `json:"matched_customer_id,omitempty"`

	// EmailCustomerID et PhoneCustomerID ne sont renseignés que pour
	// "conflict" : l'email et le téléphone de la ligne désignent deux clients
	// différents.
	EmailCustomerID int `json:"email_customer_id,omitempty"`
	PhoneCustomerID int `json:"phone_customer_id,omitempty"`

	// Resolution est la décision PROPOSÉE par défaut, pas encore arbitrée par
	// l'utilisateur. Le wizard peut la remplacer par une des options listées
	// en constantes avant de rejouer le commit.
	Resolution string `json:"resolution"`
}

// PreviewResult est ce que la preview renvoie au client HTTP. Token est
// laissé vide ici : c'est le service, pas cette fonction pure, qui le
// renseigne une fois le snapshot déposé.
type PreviewResult struct {
	Token    string         `json:"token"`
	Summary  PreviewSummary `json:"summary"`
	Rows     []PreviewRow   `json:"rows"`
	Warnings []Warning      `json:"warnings"`
}

// BuildPreview calcule le dry-run d'un import : un statut par client, calculé
// EN MÉMOIRE à partir du canonique et des lookups déjà chargés. Aucun accès
// I/O — c'est ce qui la rend testable sans base ni Redis.
//
// Précondition : imp n'est jamais nil (le service ne l'appelle qu'après un
// parse ou un BuildManualCustomerImport réussis, qui ne rendent jamais un
// canonique nil sans erreur associée).
//
// Ordre de priorité par ligne :
//  1. mapping existant (ByMapping) : already_imported si la cible existe
//     encore, mapping_stale sinon. Un client déjà mappé n'est PAS soumis à la
//     dédup email/téléphone — le mapping fait autorité.
//  2. dédup métier email/téléphone : duplicate (même client via l'un ou les
//     deux champs) ou conflict (email et téléphone désignent deux clients
//     différents).
//  3. sinon, create.
func BuildPreview(imp *IntermediateCustomerImport, lk PreviewLookups) *PreviewResult {
	result := &PreviewResult{
		Rows:     make([]PreviewRow, 0, len(imp.Customers)),
		Warnings: append([]Warning{}, imp.Warnings...),
	}

	// Compte les téléphones des lignes "create", pour le warning informatif
	// intra_file_shared_phone posé dans une seconde passe ci-dessous.
	createPhoneCount := make(map[string]int)

	for _, c := range imp.Customers {
		row := PreviewRow{
			ExternalID:  c.ExternalID,
			SourceLine:  c.SourceLine,
			DisplayName: c.Name,
		}

		if entry, mapped := lk.ByMapping[c.ExternalID]; mapped {
			if entry.TargetExists {
				row.Status = StatusAlreadyImported
				row.MatchedCustomerID = entry.CustomerID
				row.Resolution = ResolutionSkip
				result.Summary.AlreadyImported++
			} else {
				row.Status = StatusMappingStale
				row.MatchedCustomerID = entry.CustomerID
				row.Resolution = ResolutionRecreate
				result.Summary.MappingStale++
			}
			result.Rows = append(result.Rows, row)
			continue
		}

		var emailMatch, phoneMatch int
		if c.Email != nil {
			emailMatch = lk.ByEmail[strings.ToLower(*c.Email)]
		}
		if c.Phone != nil {
			phoneMatch = lk.ByPhone[*c.Phone]
		}

		switch {
		case emailMatch > 0 && phoneMatch > 0 && emailMatch == phoneMatch:
			row.Status = StatusDuplicate
			row.MatchedBy = MatchedByBoth
			row.MatchedCustomerID = emailMatch
			row.Resolution = ResolutionSkip
			result.Summary.Duplicates++

		case emailMatch > 0 && phoneMatch > 0:
			row.Status = StatusConflict
			row.EmailCustomerID = emailMatch
			row.PhoneCustomerID = phoneMatch
			row.Resolution = ResolutionSkip
			result.Summary.Conflicts++
			result.Warnings = append(result.Warnings, Warning{
				Code:    WarnDuplicateConflict,
				Ref:     c.ExternalID,
				Message: fmt.Sprintf("l'email et le telephone de cette ligne designent deux clients differents (%d et %d)", emailMatch, phoneMatch),
			})

		case emailMatch > 0:
			row.Status = StatusDuplicate
			row.MatchedBy = MatchedByEmail
			row.MatchedCustomerID = emailMatch
			row.Resolution = ResolutionSkip
			result.Summary.Duplicates++

		case phoneMatch > 0:
			row.Status = StatusDuplicate
			row.MatchedBy = MatchedByPhone
			row.MatchedCustomerID = phoneMatch
			row.Resolution = ResolutionSkip
			result.Summary.Duplicates++

		default:
			row.Status = StatusCreate
			row.Resolution = ResolutionCreate
			result.Summary.ToCreate++
			if c.Phone != nil && *c.Phone != "" {
				createPhoneCount[*c.Phone]++
			}
		}

		result.Rows = append(result.Rows, row)
	}

	result.Summary.Total = len(imp.Customers)

	// Deuxième passe, informative : signale les lignes "create" dont le
	// téléphone est partagé par au moins une autre ligne "create" du même
	// fichier. imp.Customers et result.Rows sont en correspondance 1:1, dans
	// le même ordre — aucune ligne n'est sautée dans la boucle ci-dessus.
	for i, c := range imp.Customers {
		if result.Rows[i].Status != StatusCreate || c.Phone == nil || *c.Phone == "" {
			continue
		}
		if shared := createPhoneCount[*c.Phone]; shared > 1 {
			result.Warnings = append(result.Warnings, Warning{
				Code:    WarnIntraFileSharedPhone,
				Ref:     c.ExternalID,
				Message: fmt.Sprintf("telephone partage avec %d autre(s) ligne(s) a creer dans ce fichier", shared-1),
			})
		}
	}

	return result
}
