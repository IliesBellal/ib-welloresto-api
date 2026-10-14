package customers

import (
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"welloresto-api/internal/helpers"
)

// Blocage de clients (docs/CUSTOMER_BLOCKING.md) : un client bloqué ne peut
// plus passer de commande en ligne ScanNOrder chez l'établissement qui l'a
// bloqué. Le blocage est porté par customer.blocked_at, par établissement ;
// l'identité d'un client est son téléphone (la commande ScanNOrder ne transporte
// ni email ni compte), si bien qu'un blocage vaut pour toutes les fiches
// (doublons compris) qui partagent le téléphone ou l'email de la fiche visée.

// objectBlockReason est deletion_reasons.deletion_reason_object des motifs de
// blocage (seed : migration 183), servis par GET /pos/deletion_reasons/customer.
const objectBlockReason = "customer"

// Erreurs de validation de BlockCustomer, converties en 400 par le handler.
var (
	ErrBlockIdentityRequired = errors.New("customer block: a customer_id, a phone or an email is required")
	ErrBlockInvalidPhone     = errors.New("customer block: invalid phone number")
	ErrBlockInvalidEmail     = errors.New("customer block: invalid email address")
	ErrBlockReasonInvalid    = errors.New("customer block: unknown block reason")
	ErrBlockCommentRequired  = errors.New("customer block: this reason requires a comment")
	ErrCustomerNotFound      = errors.New("customer not found")
)

// maxBlockCommentLen est la taille de customer.blocked_comment (varchar(255)).
const maxBlockCommentLen = 255

// BlockCustomerRequest est le corps de POST /customers/block. Soit customer_id
// (fiche existante), soit phone et/ou email (client éventuellement inconnu : sa
// fiche est alors créée). first_name / last_name ne servent qu'à une création.
type BlockCustomerRequest struct {
	CustomerID string `json:"customer_id"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	ReasonID   string `json:"reason_id"`
	Comment    string `json:"comment"`
}

// BlockCustomerResult est la réponse de POST /customers/block.
type BlockCustomerResult struct {
	CustomerID   string `json:"customer_id"`
	Created      bool   `json:"created"`
	BlockedCount int    `json:"blocked_count"`
}

// blockIdentity désigne les fiches d'un établissement visées par un blocage ou
// un déblocage : la fiche CustomerID et/ou toutes celles qui partagent Tel ou
// Email. Un champ vide est ignoré.
type blockIdentity struct {
	CustomerID string
	Tel        string
	Email      string
}

func (i blockIdentity) isEmpty() bool {
	return i.CustomerID == "" && i.Tel == "" && i.Email == ""
}

// blockInfo est l'état de blocage à écrire sur les fiches visées.
type blockInfo struct {
	By       string
	ReasonID string
	Comment  string
}

// normalizeBlockPhone normalise un téléphone saisi comme le font les flux
// publics (FindCustomerByPhone : indicatif FR par défaut) et refuse ce qui ne
// peut pas être un numéro. Une saisie vide n'est pas une erreur : "".
func normalizeBlockPhone(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	digits := 0
	for _, c := range raw {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	if digits < 6 {
		return "", ErrBlockInvalidPhone
	}
	return helpers.NormalizePhoneNumber(raw, "FR"), nil
}

// normalizeBlockEmail valide et met en minuscules un email saisi. Une saisie
// vide n'est pas une erreur : "".
func normalizeBlockEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return "", ErrBlockInvalidEmail
	}
	return strings.ToLower(raw), nil
}

// blockedCustomerName est le nom de la fiche créée pour bloquer un client
// inconnu : customer_name ne doit pas être NULL (les listes le scannent dans un
// string). Prénom + nom s'ils sont connus, sinon le téléphone, sinon l'email.
func blockedCustomerName(firstName, lastName, phone, email string) string {
	name := strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	if name == "" {
		name = phone
	}
	if name == "" {
		name = email
	}
	return truncateRunes(name, 50)
}

// truncateRunes coupe s à n caractères (colonnes varchar(50)).
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
