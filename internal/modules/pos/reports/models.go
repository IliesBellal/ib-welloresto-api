package reports

// ReportsRequest structure pour les requêtes de rapports
type ReportsRequest struct {
	DateFrom string `json:"date_from"` // Format: YYYY-MM-DD
	DateTo   string `json:"date_to"`   // Format: YYYY-MM-DD
}

// ============ TVA Report ============

type TVAReportResponse struct {
	Status   string         `json:"status"`
	Calendar []TVADayReport `json:"calendar"`
}

type TVADayReport struct {
	Date    string        `json:"date"`
	TTCSum  int64         `json:"TTC_sum"`
	HTSum   int64         `json:"HT_sum"`
	TVASum  int64         `json:"TVA_sum"`
	VATData []VATDataItem `json:"VAT_data"`
}

type VATDataItem struct {
	TVATitle             string `json:"tva_title"`
	TVADeliveryTypeLabel string `json:"tva_delivery_type_label"`
	TTC                  int64  `json:"ttc"`
	HT                   int64  `json:"ht"`
	TVA                  int64  `json:"tva"`
}

// ============ Payments Report ============

type PaymentsReportResponse struct {
	Status   string              `json:"status"`
	Calendar []PaymentsDayReport `json:"calendar"`
}

type PaymentsDayReport struct {
	Date     string        `json:"date"`
	Payments []PaymentItem `json:"payments"`
}

type PaymentItem struct {
	MOP    string `json:"mop"`    // Mode of Payment code
	Label  string `json:"label"`  // Mode of Payment label
	Amount int64  `json:"amount"` // Amount in centimes
}

// ============ Export (CSV) ============

// ExportReportResponse structure de réponse pour les exports CSV TVA/Paiements.
// Mêmes tags JSON que accounting.ExportAccountingResponse (le frontend
// consomme les deux via le même type ExportResponse).
type ExportReportResponse struct {
	Status      string `json:"status"`
	Filename    string `json:"filename"`
	DownloadURL string `json:"download_url"` // URL R2 pour télécharger le CSV
	Error       string `json:"error,omitempty"`
}
