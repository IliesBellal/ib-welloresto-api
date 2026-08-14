// Package importer porte le modèle canonique de l'import de clients en masse
// et les adaptateurs qui traduisent un fichier source vers ce modèle.
//
// Plusieurs portes d'entrée convergent vers un seul pipeline : un provider
// tiers (Zelty), un formulaire de saisie manuelle, et un template Wello. Toutes
// produisent un *IntermediateCustomerImport, consommé ensuite par une preview
// (dry-run) puis par un commit atomique — ce pipeline en miroir de
// internal/modules/menu/importer, sans en dépendre.
//
// Invariant de ce package : le parser ne décide rien. Il ne dédoublonne pas
// contre l'existant en base, il ne décide pas du consentement publicitaire par
// défaut, il ne normalise aucune décision métier et il ne touche pas la base
// du tout. Ces arbitrages sont pris dans la preview et le commit, pas ici.
package importer

import "time"

// IntermediateCustomerImport est le résultat d'un parse : la représentation
// neutre du fichier source, avant toute décision.
type IntermediateCustomerImport struct {
	Customers []CanonicalCustomer
	Warnings  []Warning
}

// CanonicalCustomer est un client du fichier source.
type CanonicalCustomer struct {
	// ExternalID est l'identifiant source (Zelty) ou généré (wello-generic,
	// manual). Rempli par les providers en phase 2 — vide dans ce squelette.
	ExternalID string

	Name      string
	FirstName string
	LastName  string

	Email *string
	Phone *string

	Address Address

	BusinessName *string
	Birthdate    *time.Time

	AdditionalInfo *string
	DeliveryNotes  *string

	// AdvertisingConsent est laissé à nil par le parser : nil => false est
	// décidé au commit (phase 4), pas ici.
	AdvertisingConsent *bool

	// CreationDate préserve l'ancienneté du client côté provider (Zelty) quand
	// la source la fournit. nil => now() est décidé au commit.
	CreationDate *time.Time

	// SourceLine est la ligne du fichier source, pour les messages d'erreur et
	// de warning.
	SourceLine int
}

// Address est l'adresse d'un client, telle que lue dans la source.
type Address struct {
	Address           string
	FloorNumber       string
	DoorNumber        string
	AdditionalAddress string
}

// Warning signale une anomalie non bloquante rencontrée pendant le parsing.
//
// Tags JSON explicites (snake_case) : contrairement à CanonicalCustomer et
// Address, qui ne transitent que dans le snapshot Redis interne (jamais lus
// que par ce même backend), Warning fait partie de PreviewResult et est donc
// consommé par le front-end — la convention snake_case du reste du contrat
// d'import doit s'y appliquer aussi.
type Warning struct {
	Code    string `json:"code"`
	Ref     string `json:"ref,omitempty"`
	Message string `json:"message"`
}
