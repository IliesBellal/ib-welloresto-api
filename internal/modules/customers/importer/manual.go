package importer

import (
	"fmt"
	"strings"

	"welloresto-api/internal/importutil"
)

// manualCustomerIDPrefix prefixe les identifiants generes par la saisie
// manuelle : comme le template, elle n'a pas d'identifiant source, l'identite
// d'une ligne derive de son email ou de son telephone.
const manualCustomerIDPrefix = "cl-man"

// ManualCustomerInput est une ligne du formulaire de saisie manuelle de
// clients. Les valeurs arrivent deja typees (JSON) : ce n'est pas un parsing
// de fichier, mais les memes regles STRICTES que wello-generic s'appliquent
// (P1/P2/P3) — c'est un utilisateur qui saisit activement, il doit corriger.
type ManualCustomerInput struct {
	Name              string
	FirstName         string
	LastName          string
	Email             string
	Phone             string
	Address           string
	FloorNumber       string
	DoorNumber        string
	AdditionalAddress string
	BusinessName      string
	Birthdate         string
	AdditionalInfo    string
	DeliveryNotes     string

	AdvertisingConsent *bool
}

// BuildManualCustomerImport assemble le canonique a partir d'une saisie
// manuelle. Fonction standalone plutot qu'un CustomerImportProvider
// enregistre dans le Registry : la saisie manuelle n'a pas de flux a lire,
// elle est appelee directement — meme parti pris que
// menu/importer.BuildManualImport.
func BuildManualCustomerImport(inputs []ManualCustomerInput) (*IntermediateCustomerImport, error) {
	out := &IntermediateCustomerImport{}
	lineByExternalID := make(map[string]int, len(inputs))

	for i, in := range inputs {
		line := i + 1

		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, rowErrorf(line, "name", "client sans nom")
		}

		email, emailState := validateEmail(in.Email)
		if emailState == emailInvalid {
			return nil, rowErrorf(line, "email", "adresse email illisible : %q", in.Email)
		}

		phoneRaw := strings.TrimSpace(in.Phone)
		var phone *string
		phonePlausible := true
		if phoneRaw != "" {
			normalized, plausible := normalizePhoneFR(phoneRaw)
			phone = &normalized
			phonePlausible = plausible
		}

		if emailState != emailValid && phone == nil {
			return nil, rowErrorf(line, "email/phone", "ni email ni telephone : au moins l'un des deux est obligatoire")
		}

		dedupKey := strings.ToLower(email)
		if emailState != emailValid {
			dedupKey = *phone
		}
		externalID := importutil.GeneratedExternalID(manualCustomerIDPrefix, dedupKey)

		if previousLine, dup := lineByExternalID[externalID]; dup {
			return nil, rowErrorf(line, "email/phone",
				"identite deja utilisee ligne %d (meme email ou meme telephone) ; l'import doit rester rejouable", previousLine)
		}
		lineByExternalID[externalID] = line

		customer := CanonicalCustomer{
			ExternalID: externalID,
			Name:       buildDisplayName(name, ""),
			FirstName:  strings.TrimSpace(in.FirstName),
			LastName:   strings.TrimSpace(in.LastName),
			Address: Address{
				Address:           strings.TrimSpace(in.Address),
				FloorNumber:       strings.TrimSpace(in.FloorNumber),
				DoorNumber:        strings.TrimSpace(in.DoorNumber),
				AdditionalAddress: strings.TrimSpace(in.AdditionalAddress),
			},
			SourceLine: line,
		}

		if emailState == emailValid {
			customer.Email = &email
		}
		if phone != nil {
			customer.Phone = phone
			if !phonePlausible {
				out.Warnings = append(out.Warnings, Warning{
					Code:    WarnInvalidPhone,
					Ref:     externalID,
					Message: fmt.Sprintf("telephone implausible : %q", phoneRaw),
				})
			}
		}

		if business := strings.TrimSpace(in.BusinessName); business != "" {
			customer.BusinessName = &business
		}
		if info := strings.TrimSpace(in.AdditionalInfo); info != "" {
			customer.AdditionalInfo = &info
		}
		if notes := strings.TrimSpace(in.DeliveryNotes); notes != "" {
			customer.DeliveryNotes = &notes
		}

		if in.AdvertisingConsent != nil {
			consent := *in.AdvertisingConsent
			customer.AdvertisingConsent = &consent
		} else {
			consent := false
			customer.AdvertisingConsent = &consent
		}

		if birthdate, ok := parseFrenchDate(in.Birthdate); ok {
			customer.Birthdate = birthdate
		} else {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnUnparseableBirthdate,
				Ref:     externalID,
				Message: fmt.Sprintf("date de naissance illisible : %q", in.Birthdate),
			})
		}

		out.Customers = append(out.Customers, customer)
	}

	if len(out.Customers) == 0 {
		return nil, ErrNoCustomers
	}
	return out, nil
}
