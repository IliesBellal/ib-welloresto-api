package payouts

import "time"

// Channel est le canal par lequel une vente a été encaissée.
type Channel string

const (
	ChannelScanNOrder Channel = "scannorder"
	ChannelKiosk      Channel = "kiosk"
	ChannelOther      Channel = "other" // caisse, paiement non rattaché à une commande, ...
)

// Label est le libellé du canal sur le relevé.
func (c Channel) Label() string {
	switch c {
	case ChannelScanNOrder:
		return "Commandes en ligne (ScanNOrder)"
	case ChannelKiosk:
		return "Borne de commande"
	default:
		return "Autres encaissements"
	}
}

// Ref identifie un payout à justifier.
type Ref struct {
	PayoutID    string
	AccountID   string
	Amount      int64 // centimes
	Currency    string
	ArrivalDate time.Time
}

// Pending est une ligne de payout_documents à traiter.
type Pending struct {
	Ref
	Attempts int
}

// Merchant est le destinataire des justificatifs (table merchant).
type Merchant struct {
	ID        string
	Name      string
	Address   string
	SIRET     string
	VATNumber string
	Email     string
}

// ChannelTotal est le total des ventes d'un canal.
type ChannelTotal struct {
	Channel Channel
	Count   int
	Amount  int64
}

// Summary est le contenu chiffré d'un relevé de versement. Tous les montants
// sont en centimes ; la relation fondamentale est
//
//	Sales + Refunds + Adjustments - Commission - StripeFees + Discrepancy == Net
//
// avec Net égal au montant versé par Stripe. Discrepancy est nul quand le
// payout est entièrement rapproché ; sinon c'est la part que les lignes lues
// n'expliquent pas.
type Summary struct {
	Channels    []ChannelTotal
	Sales       int64
	Refunds     int64 // négatif ou nul
	RefundCount int
	Adjustments int64 // litiges et ajustements, signé
	Commission  int64 // commission Wello Resto, TTC
	StripeFees  int64
	Net         int64
	Discrepancy int64
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// Types de document téléchargeable.
const (
	KindStatement = "statement"
	KindInvoice   = "invoice"
)

// Listed est un payout dont les justificatifs ont été envoyés, vu du back-office.
type Listed struct {
	PayoutID      string    `json:"payout_id"`
	Amount        int64     `json:"amount"`
	Currency      string    `json:"currency"`
	ArrivalDate   time.Time `json:"arrival_date"`
	SentAt        time.Time `json:"sent_at"`
	InvoiceNumber string    `json:"invoice_number,omitempty"`
	CommissionTTC int64     `json:"commission_ttc,omitempty"`
	HasStatement  bool      `json:"has_statement"`
	HasInvoice    bool      `json:"has_invoice"`

	StatementKey string `json:"-"`
	InvoiceKey   string `json:"-"`
}

// DownloadLink est le lien signé d'un document.
type DownloadLink struct {
	PayoutID    string `json:"payout_id"`
	Kind        string `json:"kind"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
}

// Reconciled dit si les lignes du payout expliquent entièrement son montant.
func (s *Summary) Reconciled() bool { return s.Discrepancy == 0 }

// Invoice est une facture de commission émise (table commission_invoices).
type Invoice struct {
	ID          int64
	Number      string
	Series      string
	MerchantID  string
	PayoutID    string
	AmountTTC   int64
	AmountHT    int64
	AmountVAT   int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	IssuedAt    time.Time
}

// splitVAT sépare un montant TTC en HT et TVA au taux vatRatePercent.
func splitVAT(ttc int64) (ht, vat int64) {
	if ttc < 0 {
		ht, vat = splitVAT(-ttc)
		return -ht, -vat
	}
	ht = (ttc*100 + (100+vatRatePercent)/2) / (100 + vatRatePercent)
	return ht, ttc - ht
}
