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
