// Package reviewrequests envoie aux clients des commandes Wello Resto (tous
// canaux : ScanNOrder, caisse, borne ; ni Uber Eats ni Deliveroo) une demande
// d'avis Google par e-mail, au nom de l'établissement, et gère le lien « ne
// plus recevoir » (docs/scannorder-seo.md, « Fiche Google et demandes d'avis »).
//
// Règles :
//   - commande Wello Resto close depuis au moins ReviewDelay et au plus
//     ReviewWindow, établissement ayant activé la demande et renseigné son lien
//     d'avis Google, client avec e-mail ;
//   - envoi entre QuietHoursEnd et QuietHoursStart, heure de l'établissement ;
//   - au plus une demande par client et par établissement tous les
//     FrequencyCap ; jamais à un client désinscrit de cet établissement ;
//   - règles Google : aucune contrepartie, aucun tri des clients selon leur
//     satisfaction — tous les clients éligibles reçoivent la même demande.
package reviewrequests

import (
	"errors"
	"time"
)

const (
	// ReviewDelay : délai après la clôture de la commande (le client a mangé).
	ReviewDelay = 90 * time.Minute
	// ReviewWindow : au-delà, la commande n'est plus sollicitée (pas d'envoi
	// de l'historique au déploiement).
	ReviewWindow = 48 * time.Hour
	// FrequencyCap : pas de nouvelle demande au même client du même
	// établissement avant ce délai.
	FrequencyCap = 90 * 24 * time.Hour
	// QuietHoursEnd / QuietHoursStart : envoi de 9 h à 21 h, heure locale de
	// l'établissement ; en dehors, la commande attend le passage suivant.
	QuietHoursEnd   = 9
	QuietHoursStart = 21
	// MaxOrdersPerCustomerPerDay : au-delà, la fiche client est jugée générique
	// (« Comptoir », « Passage ») et n'est pas sollicitée.
	MaxOrdersPerCustomerPerDay = 5
	batchSize                  = 100
)

// Statuts de review_requests (migration 223).
const (
	StatusPending        = "pending"
	StatusSent           = "sent"
	StatusFailed         = "failed"
	StatusSkippedOptOut  = "skipped_opt_out"
	StatusSkippedRecent  = "skipped_recent"
	StatusSkippedInvalid = "skipped_invalid"
)

var (
	// ErrNotConfigured : mailer ou adresse publique ScanNOrder absents.
	ErrNotConfigured = errors.New("review_requests_not_configured")
	// ErrMissingReviewURL : l'établissement n'a pas de lien d'avis Google.
	ErrMissingReviewURL = errors.New("missing_review_url")
	// ErrMissingRecipient : l'utilisateur connecté n'a pas d'e-mail (envoi de test).
	ErrMissingRecipient = errors.New("missing_user_email")
)

// candidate : commande close à solliciter.
type candidate struct {
	OrderID      string
	MerchantID   string
	MerchantName string
	Timezone     string
	FirstName    string
	Email        string
	ReviewURL    string
}

// requestRow : ligne de review_requests.
type requestRow struct {
	MerchantID string
	OrderID    string
	EmailHash  string
	Token      string
	Status     string
}

// tokenInfo : demande retrouvée par le jeton du lien de désinscription.
type tokenInfo struct {
	MerchantID   string
	MerchantName string
	EmailHash    string
	OptedOut     bool
}

// RunResult résume un passage de la tâche.
type RunResult struct {
	Sent    int
	Skipped int
	Failed  int
}

// PublicInfo : ce que la page de désinscription affiche.
type PublicInfo struct {
	MerchantName string `json:"merchant_name"`
	LogoURL      string `json:"logo_url,omitempty"`
	Color        string `json:"color"`
	TextColor    string `json:"text_color"`
	OptedOut     bool   `json:"opted_out"`
}
