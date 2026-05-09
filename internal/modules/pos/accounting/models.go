package accounting

// ExportAccountingRequest structure pour la requête d'export
type ExportAccountingRequest struct {
	DateFrom string `json:"date_from"` // Format: YYYY-MM-DD
	DateTo   string `json:"date_to"`   // Format: YYYY-MM-DD
}

// ExportAccountingResponse structure de réponse
type ExportAccountingResponse struct {
	Status      string `json:"status"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"` // URL R2 pour télécharger le PDF
	Error       string `json:"error,omitempty"`
}

// MerchantHeader contient les infos d'entête pour le PDF
type MerchantHeader struct {
	MerchantName string
	SIRET        string
	VATNumber    string
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
	Month      string           `json:"month"`
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

type VATAggregationRow struct {
	Month     string
	Channel   string
	OrderType string
	Rate      float64
	TTCCents  int64
	HTCents   int64
	VATCents  int64
}
