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

// Reconciled dit si les lignes du payout expliquent entièrement son montant.
func (s *Summary) Reconciled() bool { return s.Discrepancy == 0 }

// channelAmount renvoie le total des ventes d'un canal.
func (s *Summary) channelAmount(c Channel) int64 {
	for _, t := range s.Channels {
		if t.Channel == c {
			return t.Amount
		}
	}
	return 0
}

// Line est un virement traité, tel que stocké dans payout_documents : de quoi
// construire le récapitulatif mensuel sans relire Stripe.
type Line struct {
	PayoutID    string
	ArrivalDate time.Time
	ScanNOrder  int64
	Kiosk       int64
	Other       int64
	Refunds     int64
	Adjustments int64
	Commission  int64 // TTC
	StripeFees  int64
	Discrepancy int64
	Net         int64 // montant versé
}

// Sales renvoie le total des encaissements du virement.
func (l Line) Sales() int64 { return l.ScanNOrder + l.Kiosk + l.Other }

// Types de document téléchargeable.
const (
	KindStatement = "statement" // relevé d'un virement
	KindInvoice   = "invoice"   // facture de commission mensuelle
	KindRecap     = "recap"     // récapitulatif mensuel des commissions
)

// Listed est un virement dont le relevé a été envoyé, vu du back-office.
type Listed struct {
	PayoutID      string    `json:"payout_id"`
	Amount        int64     `json:"amount"`
	Currency      string    `json:"currency"`
	ArrivalDate   time.Time `json:"arrival_date"`
	SentAt        time.Time `json:"sent_at"`
	CommissionTTC int64     `json:"commission_ttc"`
	HasStatement  bool      `json:"has_statement"`

	StatementKey string `json:"-"`
}

// Invoice est une facture de commission mensuelle (table commission_invoices).
type Invoice struct {
	ID          int64     `json:"-"`
	Number      string    `json:"number"`
	Series      string    `json:"series"`
	MerchantID  string    `json:"-"`
	PeriodMonth time.Time `json:"period_month"` // 1er jour du mois facturé
	AmountTTC   int64     `json:"amount_ttc"`
	AmountHT    int64     `json:"amount_ht"`
	AmountVAT   int64     `json:"amount_vat"`
	PayoutCount int       `json:"payout_count"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	IssuedAt    time.Time `json:"issued_at"`
	SentAt      time.Time `json:"sent_at"`
	Attempts    int       `json:"-"`
	HasInvoice  bool      `json:"has_invoice"`
	HasRecap    bool      `json:"has_recap"`

	InvoiceKey string `json:"-"`
	RecapKey   string `json:"-"`
}

// DownloadLink est le lien signé d'un document.
type DownloadLink struct {
	Ref         string `json:"ref"` // payout_id ou numéro de facture
	Kind        string `json:"kind"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"`
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
