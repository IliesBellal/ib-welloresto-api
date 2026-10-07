package accounting

// ExportAccountingRequest structure pour la requête d'export.
// Les dates sont des dates de calendrier nues, interprétées dans le fuseau de
// l'établissement (merchant.timezone) : premier jour inclus à 00:00:00 local,
// dernier jour inclus jusqu'à 23:59:59 local. Ne pas envoyer de dates converties
// en UTC, cela décalerait la période d'un jour.
//
// Channels filtre les commandes par canal (valeurs de orders.order_source :
// WELLO_RESTO_POS, KIOSK, SCANNORDER, UBER_EATS, DELIVEROO) — uniquement pour
// un établissement en clôture automatique ; vide = tous les canaux. Refusé en
// clôture manuelle (rapport historique non configurable).
type ExportAccountingRequest struct {
	DateFrom string   `json:"date_from"`          // Format: YYYY-MM-DD (heure locale établissement)
	DateTo   string   `json:"date_to"`            // Format: YYYY-MM-DD (heure locale établissement)
	Channels []string `json:"channels,omitempty"` // canaux (clôture automatique uniquement)
}

// ExportAccountingResponse structure de réponse
type ExportAccountingResponse struct {
	Status      string `json:"status"`
	ExportID    int64  `json:"export_id,omitempty"` // id dans accounting_exports, pour retélécharger plus tard
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"` // lien signé (bucket R2 privé), valable une heure
	Error       string `json:"error,omitempty"`
}

// MerchantHeader contient les infos d'entête pour le PDF
type MerchantHeader struct {
	MerchantName string
	SIRET        string
	VATNumber    *string
	Address      string
	Phone        string
	Currency     string
	Timezone     string
}

// TVARow représente une ligne de TVA
type TVARow struct {
	TVATitle string
	Rate     float64
	TTC      float64
	HT       float64
	TVA      float64
}

// PaymentRow représente un moyen de paiement
type PaymentRow struct {
	Label  string
	Amount int64
}

// PDFReportData contient toutes les données pour le PDF
type PDFReportData struct {
	Header   MerchantHeader
	TVARows  []TVARow
	Payments []PaymentRow
	Footer   string
	Month    string
	Year     string
}

type VATCalculateRequest struct {
	StartDate  string   `json:"start_date"`
	EndDate    string   `json:"end_date"`
	Channels   []string `json:"channels"`
	OrderTypes []string `json:"order_types"`
}

type VATRateBreakdown struct {
	Amount int64 `json:"amount"`
	BaseHT int64 `json:"base_ht"`
}

type VATMonthlyBreakdown struct {
	Month string `json:"month"`
	// ClosingMode : mode de clôture du mois (MANUAL : TVA sur les lignes,
	// remises déduites ; AUTO : TVA ventilée à partir des encaissements).
	ClosingMode string           `json:"closing_mode"`
	RevenueHT  int64            `json:"revenue_ht"`
	VATByRate  map[string]int64 `json:"vat_by_rate"`
	VATTotal   int64            `json:"vat_total"`
	RevenueTTC int64            `json:"revenue_ttc"`
}

type VATShare struct {
	VAT        int64 `json:"vat"`
	Percentage int64 `json:"percentage"`
}

type VATCalculateResponse struct {
	TotalVAT         int64                       `json:"total_vat"`
	VATByRate        map[string]VATRateBreakdown `json:"vat_by_rate"`
	MonthlyBreakdown []VATMonthlyBreakdown       `json:"monthly_breakdown"`
	ByChannel        map[string]VATShare         `json:"by_channel"`
	ByOrderType      map[string]VATShare         `json:"by_order_type"`
}
