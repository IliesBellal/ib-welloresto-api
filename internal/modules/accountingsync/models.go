package accountingsync

import (
	"encoding/json"
	"errors"
	"time"
)

// Statuts d'une connexion.
const (
	StatusSetup  = "setup"  // connectée, correspondance des comptes pas encore validée
	StatusActive = "active" // synchronisation en marche
	StatusError  = "error"  // identifiants refusés : reconnexion nécessaire
)

// Statuts d'un envoi (voir migration 181).
const (
	ItemSent      = "sent"
	ItemFailed    = "failed"
	ItemUncertain = "uncertain"
	ItemSkipped   = "skipped"
	ItemAbandoned = "abandoned"
)

var (
	ErrUnknownProvider  = errors.New("accountingsync: outil inconnu")
	ErrAlreadyConnected = errors.New("accountingsync: un outil est déjà connecté, le déconnecter d'abord")
	ErrNotConnected     = errors.New("accountingsync: aucun outil connecté")
	ErrAuthMethod       = errors.New("accountingsync: mode de connexion indisponible pour cet outil")
	ErrInvalidState     = errors.New("accountingsync: état OAuth invalide ou expiré")
	ErrNotConfigured    = errors.New("accountingsync: chiffrement des identifiants non configuré")
	ErrItemNotFound     = errors.New("accountingsync: envoi introuvable")
	ErrItemNotRetryable = errors.New("accountingsync: cet envoi n'est pas en échec")
	ErrNotActive        = errors.New("accountingsync: la synchronisation n'est pas active")
)

// Connection est la connexion d'un établissement à un outil, telle que stockée.
type Connection struct {
	MerchantID   string
	Provider     string
	Status       string
	Credentials  []byte // chiffrées
	Config       Mapping
	State        json.RawMessage
	AccountLabel string
	SyncFrom     *time.Time
	ConnectedAt  time.Time
	ActivatedAt  *time.Time
	LastSyncAt   *time.Time
	LastError    string
}

// ConnectionView est la connexion renvoyée au back-office : sans identifiants.
type ConnectionView struct {
	Provider     string     `json:"provider"`
	ProviderName string     `json:"provider_name"`
	Status       string     `json:"status"`
	AccountLabel string     `json:"account_label,omitempty"`
	Mapping      Mapping    `json:"mapping"`
	SyncFrom     *string    `json:"sync_from,omitempty"`
	ConnectedAt  time.Time  `json:"connected_at"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// Closure est une clôture fiscale journalière à envoyer.
type Closure struct {
	ID          int64
	Date        string
	Hash        string
	VATRaw      []byte
	PaymentsRaw []byte
	// Attempts : tentatives déjà faites (0 si jamais tentée).
	Attempts int
}

// Item est une ligne du journal des envois.
type Item struct {
	Date       string          `json:"date"`
	Status     string          `json:"status"`
	Attempts   int             `json:"attempts"`
	ExternalID string          `json:"external_id,omitempty"`
	Difference int64           `json:"difference"`
	LastError  string          `json:"last_error,omitempty"`
	SentAt     *time.Time      `json:"sent_at,omitempty"`
	Entry      json.RawMessage `json:"entry,omitempty"`
}

// ItemUpdate est ce qui s'enregistre après un essai d'envoi.
type ItemUpdate struct {
	MerchantID  string
	Provider    string
	Date        string
	ClosureID   int64
	ClosureHash string
	Status      string
	ExternalID  string
	Entry       []byte
	Difference  int64
	LastError   string
}
