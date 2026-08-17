package importer

import (
	"fmt"
	"strings"

	"welloresto-api/internal/models"
)

// Codes de blocage. Le commit est refusé tant qu'il en reste un : rien n'est
// écrit, l'utilisateur doit revenir au wizard.
const (
	// BlockerUnknownDecision : une décision du client référence un
	// ExternalID absent du snapshot.
	BlockerUnknownDecision = "unknown_decision"

	// BlockerNewDuplicateDetected : la ligne était "create" en preview, mais
	// un client email/téléphone identique a été créé entre-temps. Le client
	// doit revalider (relancer une preview) ou choisir explicitement
	// import_anyway.
	BlockerNewDuplicateDetected = "new_duplicate_detected"

	// BlockerInvalidUpdateTarget : la résolution demande une mise à jour vers
	// un client qui n'existe plus (ou n'a jamais existé) dans l'état frais de
	// la base.
	BlockerInvalidUpdateTarget = "invalid_update_target"

	// BlockerInvalidDecision : la résolution ne correspond à aucune action
	// possible pour l'état frais de cette ligne (ex. "recreate" sur une ligne
	// qui n'a jamais été mappée, "update" sur une ligne déjà mappée,
	// résolution inconnue).
	BlockerInvalidDecision = "invalid_decision"
)

// CommitRowDecision est l'arbitrage du wizard pour une ligne du snapshot.
type CommitRowDecision struct {
	ExternalID string `json:"external_id"`
	Resolution string `json:"resolution"`
}

// CommitDecisions porte les arbitrages reçus du client, fusionnés en phase 1
// de BuildCommitPlan sur les résolutions par défaut du snapshot.
type CommitDecisions struct {
	Decisions []CommitRowDecision `json:"decisions"`
}

// CommitBlocker est une raison de refuser le lot, rattachée à la ligne
// fautive (Ref = ExternalID, vide si le blocage est global).
type CommitBlocker struct {
	Code    string `json:"code"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}

// CommitAction est une ligne entièrement résolue, prête à être écrite. La
// transaction n'a plus de décision à prendre : Customer est déjà construit
// (voir buildCommitCustomer) et TargetCustomerID, quand il est renseigné,
// désigne la fiche à mettre à jour.
type CommitAction struct {
	ExternalID       string
	Customer         models.Customer
	TargetCustomerID *int
}

// CommitPlan est le lot entièrement résolu. Creates et Recreates suivent la
// même écriture (INSERT) côté transaction ; ils restent séparés ici pour que
// CommitSummary distingue les deux dans le compte-rendu au wizard.
type CommitPlan struct {
	Creates      []CommitAction
	Updates      []CommitAction
	Recreates    []CommitAction
	SkippedCount int
}

// CommitSummary est le résumé renvoyé au wizard après matérialisation.
type CommitSummary struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Recreated int `json:"recreated"`
	Skipped   int `json:"skipped"`
}

// BuildCommitPlan calcule le plan d'écriture, 100% en mémoire, contre les
// lookups FRAIS passés en paramètre — jamais ceux de la preview. « Le commit
// ne fait jamais confiance à la preview » (audit produit §5.3) : entre la
// preview et le commit, un autre import ou une action manuelle a pu créer,
// supprimer ou modifier des clients ; chaque ligne est revalidée contre
// l'état actuel avant d'être planifiée.
//
// Étape 1 — fusion : la résolution par défaut de chaque PreviewRow du
// snapshot est remplacée par celle du wizard quand elle est fournie. Une
// décision qui référence un ExternalID absent du snapshot est un
// BlockerUnknownDecision.
//
// Étape 2 — par client, la résolution (fusionnée) est validée contre l'état
// frais :
//   - un mapping existant (fresh.ByMapping) fait toujours autorité en
//     premier, comme dans BuildPreview : "update"/"update_to_email"/
//     "update_to_phone"/"create"/"import_anyway" sont incohérents pour une
//     ligne mappée (BlockerInvalidDecision — elle doit être "skip" ou
//     "recreate") ;
//   - "recreate" n'a de sens que pour une ligne mappée (already_imported ou
//     mapping_stale, TargetExists importe peu : dans les deux cas on crée une
//     nouvelle fiche et on réaffecte le mapping) ;
//   - "create" échoue en BlockerNewDuplicateDetected si un doublon
//     email/téléphone est apparu depuis la preview — sauf "import_anyway",
//     qui crée malgré tout ;
//   - "update" résout sa cible depuis emailMatch/phoneMatch frais (pas depuis
//     MatchedCustomerID de la preview, potentiellement périmé) : cible
//     absente ou ambiguë (email et téléphone frais pointent vers deux clients
//     différents) → BlockerInvalidUpdateTarget ;
//   - "update_to_email"/"update_to_phone" résolvent respectivement vers
//     emailMatch/phoneMatch frais, cible absente → même blocker ;
//   - "skip" est toujours valide, quel que soit l'état frais — c'est le seul
//     choix qui ne peut jamais être bloqué ;
//   - toute résolution inconnue → BlockerInvalidDecision.
//
// S'il reste au moins un blocker, la fonction rend (nil, blockers) : aucune
// action n'est retenue, même pour les lignes valides — le commit est
// tout-ou-rien dès la planification.
func BuildCommitPlan(snap *PreviewSnapshot, dec CommitDecisions, fresh PreviewLookups) (*CommitPlan, []CommitBlocker) {
	resolutionByExternalID := make(map[string]string, len(snap.Rows))
	for _, row := range snap.Rows {
		resolutionByExternalID[row.ExternalID] = row.Resolution
	}

	var blockers []CommitBlocker
	for _, d := range dec.Decisions {
		if _, known := resolutionByExternalID[d.ExternalID]; !known {
			blockers = append(blockers, CommitBlocker{
				Code:    BlockerUnknownDecision,
				Ref:     d.ExternalID,
				Message: fmt.Sprintf("aucune ligne de preview pour %q", d.ExternalID),
			})
			continue
		}
		resolutionByExternalID[d.ExternalID] = d.Resolution
	}

	plan := &CommitPlan{}

	for _, c := range snap.Customers {
		resolution := resolutionByExternalID[c.ExternalID]

		var emailMatch, phoneMatch int
		if c.Email != nil {
			emailMatch = fresh.ByEmail[strings.ToLower(*c.Email)]
		}
		if c.Phone != nil {
			phoneMatch = fresh.ByPhone[*c.Phone]
		}
		_, hasMapping := fresh.ByMapping[c.ExternalID]

		switch resolution {
		case ResolutionCreate:
			if hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			if emailMatch > 0 || phoneMatch > 0 {
				blockers = append(blockers, CommitBlocker{
					Code:    BlockerNewDuplicateDetected,
					Ref:     c.ExternalID,
					Message: "un client avec le meme email ou telephone a ete cree depuis la preview ; relancez une preview ou choisissez import_anyway",
				})
				continue
			}
			plan.Creates = append(plan.Creates, newCreateAction(snap.MerchantID, c))

		case ResolutionImportAnyway:
			if hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			plan.Creates = append(plan.Creates, newCreateAction(snap.MerchantID, c))

		case ResolutionSkip:
			plan.SkippedCount++

		case ResolutionUpdate:
			if hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			target := resolveUpdateTarget(emailMatch, phoneMatch)
			if target == 0 {
				blockers = append(blockers, invalidUpdateTargetBlocker(c.ExternalID))
				continue
			}
			plan.Updates = append(plan.Updates, newUpdateAction(snap.MerchantID, c, target))

		case ResolutionUpdateToEmail:
			if hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			if emailMatch == 0 {
				blockers = append(blockers, invalidUpdateTargetBlocker(c.ExternalID))
				continue
			}
			plan.Updates = append(plan.Updates, newUpdateAction(snap.MerchantID, c, emailMatch))

		case ResolutionUpdateToPhone:
			if hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			if phoneMatch == 0 {
				blockers = append(blockers, invalidUpdateTargetBlocker(c.ExternalID))
				continue
			}
			plan.Updates = append(plan.Updates, newUpdateAction(snap.MerchantID, c, phoneMatch))

		case ResolutionRecreate:
			if !hasMapping {
				blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
				continue
			}
			plan.Recreates = append(plan.Recreates, newCreateAction(snap.MerchantID, c))

		default:
			blockers = append(blockers, invalidDecisionBlocker(c.ExternalID, resolution))
		}
	}

	if len(blockers) > 0 {
		return nil, blockers
	}
	return plan, nil
}

// resolveUpdateTarget résout la cible d'un "update" non qualifié depuis
// l'état frais. Ambiguë (email et téléphone pointent vers deux clients
// différents — la ligne est devenue un conflit depuis la preview) ou absente
// (les deux à zéro) : rend 0, l'appelant bloque avec invalid_update_target.
func resolveUpdateTarget(emailMatch, phoneMatch int) int {
	switch {
	case emailMatch > 0 && phoneMatch > 0 && emailMatch == phoneMatch:
		return emailMatch
	case emailMatch > 0 && phoneMatch == 0:
		return emailMatch
	case phoneMatch > 0 && emailMatch == 0:
		return phoneMatch
	default:
		return 0
	}
}

func newCreateAction(merchantID string, c CanonicalCustomer) CommitAction {
	return CommitAction{ExternalID: c.ExternalID, Customer: BuildCommitCustomer(merchantID, c)}
}

func newUpdateAction(merchantID string, c CanonicalCustomer, targetCustomerID int) CommitAction {
	target := targetCustomerID
	return CommitAction{
		ExternalID:       c.ExternalID,
		Customer:         BuildCommitCustomer(merchantID, c),
		TargetCustomerID: &target,
	}
}

func invalidDecisionBlocker(externalID, resolution string) CommitBlocker {
	return CommitBlocker{
		Code:    BlockerInvalidDecision,
		Ref:     externalID,
		Message: fmt.Sprintf("resolution %q incoherente avec l'etat actuel de cette ligne", resolution),
	}
}

func invalidUpdateTargetBlocker(externalID string) CommitBlocker {
	return CommitBlocker{
		Code:    BlockerInvalidUpdateTarget,
		Ref:     externalID,
		Message: "le client cible de la mise a jour n'existe plus",
	}
}

// BuildCommitCustomer traduit un CanonicalCustomer en models.Customer, prêt à
// être passé à CustomersRepository.UpdateOrCreateCustomer. Quatre points de
// correction (audit clients), tous traités ici plutôt que dans le
// repository :
//
// C1 — advertising_consent toujours explicite. UpdateOrCreateCustomer écrit
// toujours une valeur (jamais NULL) sur INSERT : son extractFieldValue rend
// littéralement `false` quand c.AdvertisingConsent est nil (import_service /
// audit clients §3.2), ce qui court-circuite le défaut SQL `true` de la
// colonne — un customer.customer_id inséré sans passer par ce chemin
// hériterait `true`, ce qui n'est jamais franc. On ne change rien côté
// repository (le comportement peut être utile ailleurs) ; on s'assure
// seulement que la valeur qu'on lui passe est TOUJOURS la valeur résolue par
// le parser (jamais nil) — ce que le canonique garantit déjà depuis la phase
// 2 (parseConsent/BuildManualCustomerImport ne rendent jamais nil).
//
// C2 — creation_date (Zelty « Date d'inscription »). La map `allowed` de
// UpdateOrCreateCustomer N'INCLUT PAS "creation_date" : la colonne a un
// défaut SQL `now()` qui s'applique tant qu'elle n'apparaît pas dans la
// requête, et le repository ne l'y met jamais. Modifier la fonction partagée
// pour l'y ajouter est risqué (elle est utilisée par les flux
// booking/order) ; le choix retenu est de NE PAS toucher au repository, et de
// porter la date résolue dans models.Customer.CreationDate — un champ que
// UpdateOrCreateCustomer ignore totalement (aucun cas ne le référence dans
// extractFieldValue/getStringField), utilisé ici uniquement comme
// transporteur de valeur jusqu'à materializeImportTx, qui exécute un second
// UPDATE ciblé dans LA MÊME TRANSACTION une fois l'INSERT fait — et
// seulement pour un Create/Recreate, jamais pour un Update (une mise à jour
// ne doit pas réécrire la date de création d'une fiche existante).
//
// Constat additionnel, même symptôme que C2 : "delivery_notes" (Zelty « Info
// interne ») n'est pas non plus dans `allowed` — la colonne ne porte pas le
// préfixe customer_ (contrairement à toutes les autres), ce qui explique
// probablement l'oubli. Même traitement que C2 : porté via
// models.Customer.CustomerDeliveryNotes (champ existant, également ignoré
// par UpdateOrCreateCustomer), appliqué par un second UPDATE dans
// materializeImportTx — cette fois aussi bien pour Create/Recreate QUE pour
// Update, uniquement quand le fichier fournit une valeur (sémantique
// partielle, voir C3).
//
// C3 — mise à jour partielle. Lu dans la branche UPDATE de
// UpdateOrCreateCustomer : `for col := range allowed { v :=
// extractFieldValue(c, col); if v != nil { setParts = append(...) } }` — une
// colonne dont la valeur extraite est nil est purement OMISE du SET, jamais
// mise à NULL. extractFieldValue lui-même traite déjà une chaîne vide comme
// "absente" (`if ptr != nil && *ptr != "" { return *ptr }; return nil`). La
// fonction fait donc DÉJÀ ce qu'il faut : passer un models.Customer où seuls
// les champs réellement fournis par le fichier sont non-nil/non-vides suffit
// à obtenir une mise à jour partielle sûre — buildCommitCustomer ne
// renseigne un pointeur que lorsque la valeur canonique correspondante est
// non vide, aucune variante d'update dédiée n'est nécessaire. Vérifié par
// TestBuildCommitCustomerOmitsAbsentFields et par le test d'intégration
// Postgres (un update sans email dans le fichier ne vide pas l'email
// existant).
//
// C4 — CustomersService.UpdateOrCreateCustomer(ctx, map) est un stub mort
// (TODO, n'écrit rien) : jamais appelé ici. C'est
// CustomersRepository.UpdateOrCreateCustomer(ctx, *models.Customer), la
// version transaction-agnostique, qui est utilisée (voir
// customer_import_commit_service.go). Le type est internal/models.Customer,
// pas customers.Customer (customers/models.go), qui est un type orphelin non
// branché sur ce repository.
//
// Exportée pour être réutilisée par CustomersService.CreateCustomer (endpoint
// POST /customers, création unitaire) sans dupliquer ce mapping.
func BuildCommitCustomer(merchantID string, c CanonicalCustomer) models.Customer {
	consent := false
	if c.AdvertisingConsent != nil {
		consent = *c.AdvertisingConsent
	}
	brand := models.BrandWelloResto

	customer := models.Customer{
		MerchantID:             merchantID,
		CustomerBrand:          &brand,
		CustomerTel:            c.Phone,
		CustomerEmail:          c.Email,
		CustomerBusinessName:   c.BusinessName,
		CustomerAdditionalInfo: c.AdditionalInfo,
		AdvertisingConsent:     &consent,
		// Transportés jusqu'à materializeImportTx, ignorés par
		// UpdateOrCreateCustomer (voir C2 ci-dessus).
		CustomerDeliveryNotes: c.DeliveryNotes,
	}

	if c.Name != "" {
		name := c.Name
		customer.CustomerName = &name
	}
	if c.FirstName != "" {
		firstName := c.FirstName
		customer.CustomerFirstName = &firstName
	}
	if c.LastName != "" {
		lastName := c.LastName
		customer.CustomerLastName = &lastName
	}
	if c.Address.Address != "" {
		address := c.Address.Address
		customer.CustomerAddress = &address
	}
	if c.Address.FloorNumber != "" {
		floor := c.Address.FloorNumber
		customer.CustomerFloorNumber = &floor
	}
	if c.Address.DoorNumber != "" {
		door := c.Address.DoorNumber
		customer.CustomerDoorNumber = &door
	}
	if c.Address.AdditionalAddress != "" {
		additional := c.Address.AdditionalAddress
		customer.CustomerAdditionalAddress = &additional
	}
	if c.Birthdate != nil {
		birthdate := c.Birthdate.Format("2006-01-02")
		customer.CustomerBirthdate = &birthdate
	}
	if c.CreationDate != nil {
		creationDate := c.CreationDate.Format("2006-01-02 15:04:05")
		customer.CreationDate = &creationDate
	}

	return customer
}
