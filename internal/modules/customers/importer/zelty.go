package importer

import (
	"fmt"
	"io"

	"welloresto-api/internal/importutil"
)

// Colonnes de l'export Zelty (CSV) reconnues, sous leur forme repliée par
// importutil.FoldHeader. Les colonnes non listées ici (VIP, carte fidélité,
// id externe, points de fidélité, CA, solde, nombre de commandes, date de la
// dernière commande, dernier restaurant, statut, téléphone 2) sont
// dénormalisées/calculées côté Zelty ou volontairement non retenues : elles
// ne sont jamais recherchées, donc silencieusement ignorées.
const (
	zeltyCustomerColID          = "id"
	zeltyCustomerColLastName    = "nom"
	zeltyCustomerColFirstName   = "prenom"
	zeltyCustomerColBusiness    = "entreprise"
	zeltyCustomerColEmail       = "mail"
	zeltyCustomerColPhone       = "telephone"
	zeltyCustomerColInfoClient  = "info client"
	zeltyCustomerColInfoInterne = "info interne"
	zeltyCustomerColBirthdate   = "date de naissance"
	zeltyCustomerColRegistered  = "date d'inscription"
	zeltyCustomerColOptinSMS    = "optin sms"
	zeltyCustomerColOptinMail   = "optin mail"
)

// ZeltyCustomerProvider lit un export clients Zelty (.csv).
//
// Provider à identifiant SOURCE (external_id = colonne ID Zelty, stable et
// unique côté Zelty) : les anomalies de données produisent des warnings non
// bloquants plutôt que des erreurs, la ligne est importée quand même. Un
// export Zelty réel comporte des centaines de lignes sans email/téléphone ou
// sans nom/prénom mais avec un ID valide ; les bloquer casserait un export
// légitime.
type ZeltyCustomerProvider struct{}

func NewZeltyCustomerProvider() *ZeltyCustomerProvider { return &ZeltyCustomerProvider{} }

func (p *ZeltyCustomerProvider) Slug() string { return ZeltySlug }

func (p *ZeltyCustomerProvider) Parse(r io.Reader) (*IntermediateCustomerImport, error) {
	rows, err := importutil.ReadCSVRows(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidCSV, err)
	}
	if len(rows) == 0 {
		return nil, ErrNoCustomers
	}

	columns, err := parseZeltyCustomerHeader(rows[0])
	if err != nil {
		return nil, err
	}
	cell := func(row []string, key string) string {
		idx, known := columns[key]
		if !known {
			return ""
		}
		return importutil.CellAt(row, idx)
	}

	out := &IntermediateCustomerImport{}

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		line := i + 1 // numerotation du fichier, en-tete compris
		if importutil.RowIsEmpty(row) {
			continue
		}

		externalID := cell(row, zeltyCustomerColID)
		if externalID == "" {
			return nil, rowErrorf(line, "ID", "ligne sans identifiant Zelty")
		}

		firstName := cell(row, zeltyCustomerColFirstName)
		lastName := cell(row, zeltyCustomerColLastName)

		customer := CanonicalCustomer{
			ExternalID: externalID,
			Name:       buildDisplayName(firstName, lastName),
			FirstName:  firstName,
			LastName:   lastName,
			SourceLine: line,
		}

		if business := cell(row, zeltyCustomerColBusiness); business != "" {
			customer.BusinessName = &business
		}

		emailRaw := cell(row, zeltyCustomerColEmail)
		if email, state := validateEmail(emailRaw); state == emailValid {
			customer.Email = &email
		} else if state == emailInvalid {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnInvalidEmail,
				Ref:     externalID,
				Message: fmt.Sprintf("adresse email illisible : %q", emailRaw),
			})
		}

		if phoneRaw := cell(row, zeltyCustomerColPhone); phoneRaw != "" {
			normalized, plausible := normalizePhoneFR(phoneRaw)
			customer.Phone = &normalized
			if !plausible {
				out.Warnings = append(out.Warnings, Warning{
					Code:    WarnInvalidPhone,
					Ref:     externalID,
					Message: fmt.Sprintf("telephone implausible : %q", phoneRaw),
				})
			}
		}

		if info := cell(row, zeltyCustomerColInfoClient); info != "" {
			customer.AdditionalInfo = &info
		}
		if notes := cell(row, zeltyCustomerColInfoInterne); notes != "" {
			customer.DeliveryNotes = &notes
		}

		customer.AdvertisingConsent = parseConsent(cell(row, zeltyCustomerColOptinMail), cell(row, zeltyCustomerColOptinSMS))

		birthdateRaw := cell(row, zeltyCustomerColBirthdate)
		if birthdate, ok := parseFrenchDate(birthdateRaw); ok {
			customer.Birthdate = birthdate
		} else {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnUnparseableBirthdate,
				Ref:     externalID,
				Message: fmt.Sprintf("date de naissance illisible : %q", birthdateRaw),
			})
		}

		registeredRaw := cell(row, zeltyCustomerColRegistered)
		if registered, ok := parseFrenchDate(registeredRaw); ok {
			customer.CreationDate = registered
		} else {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnUnparseableRegistrationDate,
				Ref:     externalID,
				Message: fmt.Sprintf("date d'inscription illisible : %q", registeredRaw),
			})
		}

		if customer.Email == nil && customer.Phone == nil {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnMissingContact,
				Ref:     externalID,
				Message: "ni email ni telephone : le client ne pourra pas etre rapproche automatiquement",
			})
		}
		if firstName == "" && lastName == "" {
			out.Warnings = append(out.Warnings, Warning{
				Code:    WarnMissingName,
				Ref:     externalID,
				Message: "ni nom ni prenom",
			})
		}

		out.Customers = append(out.Customers, customer)
	}

	if len(out.Customers) == 0 {
		return nil, ErrNoCustomers
	}
	return out, nil
}

// parseZeltyCustomerHeader associe chaque en-tete replie a son index de
// colonne. Resolution par en-tete et non par position : l'export Zelty a une
// ligne d'en-tete stable, mais rien ne garantit l'ordre des colonnes d'un
// export a l'autre.
func parseZeltyCustomerHeader(header []string) (map[string]int, error) {
	columns := make(map[string]int, len(header))
	for idx, cell := range header {
		key := importutil.FoldHeader(cell)
		if key == "" {
			continue
		}
		// Premiere occurrence retenue en cas d'en-tete duplique.
		if _, exists := columns[key]; !exists {
			columns[key] = idx
		}
	}

	if _, ok := columns[zeltyCustomerColID]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrMissingColumn, "ID")
	}

	return columns, nil
}
