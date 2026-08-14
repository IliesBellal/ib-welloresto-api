package importer

import (
	"fmt"
	"io"
	"strings"

	"welloresto-api/internal/importutil"
)

// welloGenericCustomerIDPrefix prefixe les identifiants generes par ce
// provider (voir GeneratedExternalID) : le template n'a pas d'ID source,
// l'identite d'une ligne derive de son email ou de son telephone.
const welloGenericCustomerIDPrefix = "cl-wg"

// Champs canoniques reconnus dans l'en-tete du template Wello. Cles internes
// utilisees comme index dans la map colonne, distinctes des libelles
// affiches pour ne pas coupler la resolution aux libelles exacts.
const (
	wgcName              = "name"
	wgcFirstName         = "first_name"
	wgcLastName          = "last_name"
	wgcEmail             = "email"
	wgcPhone             = "phone"
	wgcAddress           = "address"
	wgcFloorNumber       = "floor_number"
	wgcDoorNumber        = "door_number"
	wgcAdditionalAddress = "additional_address"
	wgcBusinessName      = "business_name"
	wgcBirthdate         = "birthdate"
	wgcAdditionalInfo    = "additional_info"
	wgcDeliveryNotes     = "delivery_notes"
	wgcConsent           = "consent"
)

// welloGenericCustomerAliases reconnait les en-tetes du template, repliees
// par importutil.FoldHeader : casse, espaces multiples et accents ne
// comptent pas.
var welloGenericCustomerAliases = map[string]string{
	"nom":                    wgcName,
	"prenom":                 wgcFirstName,
	"nom de famille":         wgcLastName,
	"email":                  wgcEmail,
	"mail":                   wgcEmail,
	"telephone":              wgcPhone,
	"tel":                    wgcPhone,
	"adresse":                wgcAddress,
	"etage":                  wgcFloorNumber,
	"porte":                  wgcDoorNumber,
	"complement d'adresse":   wgcAdditionalAddress,
	"complement adresse":     wgcAdditionalAddress,
	"raison sociale":         wgcBusinessName,
	"entreprise":             wgcBusinessName,
	"date de naissance":      wgcBirthdate,
	"infos complementaires":  wgcAdditionalInfo,
	"info complementaire":    wgcAdditionalInfo,
	"notes de livraison":     wgcDeliveryNotes,
	"note de livraison":      wgcDeliveryNotes,
	"consentement marketing": wgcConsent,
	"consentement":           wgcConsent,
	"optin":                  wgcConsent,
}

// WelloGenericCustomerProvider lit le template .xlsx defini par Wello.
//
// Provider a identifiant GENERE (email ou telephone normalise, a defaut
// aucun ID source) : contrairement a Zelty, les anomalies qui empechent une
// identite fiable sont des erreurs bloquantes. Le restaurateur remplit
// activement ce modele, il doit corriger avant de pouvoir importer.
type WelloGenericCustomerProvider struct{}

func NewWelloGenericCustomerProvider() *WelloGenericCustomerProvider {
	return &WelloGenericCustomerProvider{}
}

func (p *WelloGenericCustomerProvider) Slug() string { return WelloGenericSlug }

func (p *WelloGenericCustomerProvider) Parse(r io.Reader) (*IntermediateCustomerImport, error) {
	rows, err := importutil.ReadSheetRows(r)
	if err != nil {
		return nil, err
	}

	headerIdx, columns, err := parseWelloGenericCustomerHeader(rows)
	if err != nil {
		return nil, err
	}

	out := &IntermediateCustomerImport{}
	lineByExternalID := make(map[string]int)

	for i := headerIdx + 1; i < len(rows); i++ {
		row := rows[i]
		line := i + 1
		if importutil.RowIsEmpty(row) {
			continue
		}

		cell := func(key string) string {
			idx, known := columns[key]
			if !known {
				return ""
			}
			return importutil.CellAt(row, idx)
		}

		nameRaw := cell(wgcName)
		if nameRaw == "" {
			return nil, rowErrorf(line, "Nom", "ligne renseignee sans nom")
		}

		emailRaw := cell(wgcEmail)
		email, emailState := validateEmail(emailRaw)
		if emailState == emailInvalid {
			return nil, rowErrorf(line, "Email", "adresse email illisible : %q", emailRaw)
		}

		phoneRaw := cell(wgcPhone)
		var phone *string
		phonePlausible := true
		if phoneRaw != "" {
			normalized, plausible := normalizePhoneFR(phoneRaw)
			phone = &normalized
			phonePlausible = plausible
		}

		if emailState != emailValid && phone == nil {
			return nil, rowErrorf(line, "Email/Telephone", "ni email ni telephone : au moins l'un des deux est obligatoire")
		}

		// Cle de dedoublonnage : email en minuscule si present (l'identite la
		// plus fiable), sinon telephone normalise.
		dedupKey := strings.ToLower(email)
		if emailState != emailValid {
			dedupKey = *phone
		}
		externalID := importutil.GeneratedExternalID(welloGenericCustomerIDPrefix, dedupKey)

		if previousLine, dup := lineByExternalID[externalID]; dup {
			return nil, rowErrorf(line, "Email/Telephone",
				"identite deja utilisee ligne %d (meme email ou meme telephone) ; l'import doit rester rejouable", previousLine)
		}
		lineByExternalID[externalID] = line

		customer := CanonicalCustomer{
			ExternalID: externalID,
			Name:       buildDisplayName(nameRaw, ""),
			FirstName:  cell(wgcFirstName),
			LastName:   cell(wgcLastName),
			Address: Address{
				Address:           cell(wgcAddress),
				FloorNumber:       cell(wgcFloorNumber),
				DoorNumber:        cell(wgcDoorNumber),
				AdditionalAddress: cell(wgcAdditionalAddress),
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

		if business := cell(wgcBusinessName); business != "" {
			customer.BusinessName = &business
		}
		if info := cell(wgcAdditionalInfo); info != "" {
			customer.AdditionalInfo = &info
		}
		if notes := cell(wgcDeliveryNotes); notes != "" {
			customer.DeliveryNotes = &notes
		}

		consent := parseWelloGenericConsent(cell(wgcConsent))
		customer.AdvertisingConsent = &consent

		birthdateRaw := cell(wgcBirthdate)
		if birthdate, ok := parseFrenchDate(birthdateRaw); ok {
			customer.Birthdate = birthdate
		} else {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnUnparseableBirthdate,
				Ref:     externalID,
				Message: fmt.Sprintf("date de naissance illisible : %q", birthdateRaw),
			})
		}

		out.Customers = append(out.Customers, customer)
	}

	if len(out.Customers) == 0 {
		return nil, ErrNoCustomers
	}
	return out, nil
}

// parseWelloGenericCustomerHeader localise la ligne d'en-tete et associe
// chaque champ reconnu a son index de colonne. Seule "Nom" est obligatoire :
// les autres colonnes sont facultatives, une colonne absente laisse le champ
// a sa valeur neutre.
func parseWelloGenericCustomerHeader(rows [][]string) (int, map[string]int, error) {
	headerIdx := -1
	for i, row := range rows {
		if !importutil.RowIsEmpty(row) {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return 0, nil, fmt.Errorf("%w: %s", ErrMissingColumn, "fichier vide")
	}

	columns := make(map[string]int, len(rows[headerIdx]))
	for idx, cell := range rows[headerIdx] {
		field, known := welloGenericCustomerAliases[importutil.FoldHeader(cell)]
		if !known {
			continue
		}
		// Premiere occurrence retenue : une colonne dupliquee ne doit pas
		// masquer celle que le restaurateur a effectivement remplie en
		// premier.
		if _, exists := columns[field]; !exists {
			columns[field] = idx
		}
	}

	if _, ok := columns[wgcName]; !ok {
		return 0, nil, fmt.Errorf("%w: %q (ligne %d)", ErrMissingColumn, "Nom", headerIdx+1)
	}

	return headerIdx, columns, nil
}

// parseWelloGenericConsent traduit la colonne unique "Consentement
// marketing" du template. Toujours une valeur explicite (jamais nil) :
// vide/Non/tout le reste vaut refus.
func parseWelloGenericConsent(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "oui", "vrai", "1", "true", "yes":
		return true
	default:
		return false
	}
}
