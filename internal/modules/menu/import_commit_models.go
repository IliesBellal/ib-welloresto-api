package menu

import "welloresto-api/internal/modules/menu/importer"

// ImportCommitRequest est le corps de POST /menu/import/commit.
//
// Decisions est exactement le champ `decisions` renvoyé par la preview, amendé
// par le wizard. Le type du domaine est décodé directement : TvaRateKey sait
// se lire depuis une clé JSON "<taux>:<canal>". Omis ou null, ce sont les
// défauts calculés par la preview et stockés dans le snapshot qui s'appliquent
// — c'est la porte « tout accepter ».
type ImportCommitRequest struct {
	Token     string                    `json:"token"`
	Decisions *importer.ImportDecisions `json:"decisions"`
}

// ImportCommitCounts ventile un type d'entité entre créé, réutilisé et ignoré.
type ImportCommitCounts struct {
	Created int `json:"created"`
	Reused  int `json:"reused"`
	Skipped int `json:"skipped"`
}

// ImportCommitSummary est le résumé affiché au retour du wizard.
type ImportCommitSummary struct {
	Categories     ImportCommitCounts `json:"categories"`
	Tags           ImportCommitCounts `json:"tags"`
	Attributes     ImportCommitCounts `json:"attributes"`
	Products       ImportCommitCounts `json:"products"`
	OptionsCreated int                `json:"options_created"`
}

// ImportCommitResponse est la réponse d'un commit réussi.
type ImportCommitResponse struct {
	Provider string              `json:"provider"`
	Summary  ImportCommitSummary `json:"summary"`

	Categories []ImportCommitEntity `json:"categories"`
	Tags       []ImportCommitEntity `json:"tags"`
	Attributes []ImportCommitEntity `json:"attributes"`
	Products   []ImportCommitEntity `json:"products"`
}

// newImportCommitResponse ventile le résultat de la transaction.
func newImportCommitResponse(provider string, outcome *ImportCommitOutcome) *ImportCommitResponse {
	response := &ImportCommitResponse{
		Provider:   provider,
		Categories: outcome.Categories,
		Tags:       outcome.Tags,
		Attributes: outcome.Attributes,
		Products:   outcome.Products,
	}
	response.Summary.OptionsCreated = outcome.OptionsCreated
	response.Summary.Categories = countImportActions(outcome.Categories)
	response.Summary.Tags = countImportActions(outcome.Tags)
	response.Summary.Attributes = countImportActions(outcome.Attributes)
	response.Summary.Products = countImportActions(outcome.Products)
	return response
}

func countImportActions(entities []ImportCommitEntity) ImportCommitCounts {
	var counts ImportCommitCounts
	for _, entity := range entities {
		switch entity.Action {
		case CommitActionCreated:
			counts.Created++
		case CommitActionReused:
			counts.Reused++
		case CommitActionSkipped:
			counts.Skipped++
		}
	}
	return counts
}
