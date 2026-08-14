package reports

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"sort"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

type ReportsService struct {
	reportsRepo *ReportsRepository
}

func NewReportsService(repo *ReportsRepository) *ReportsService {
	return &ReportsService{reportsRepo: repo}
}

// GetTVAReport retourne le rapport TVA pour la période donnée
func (s *ReportsService) GetTVAReport(ctx context.Context, token, dateFrom, dateTo string) (*TVAReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	// Fetch TVA data from repository
	tvaData, err := s.reportsRepo.GetTVAReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	// Sort by date
	sort.Slice(tvaData, func(i, j int) bool {
		return tvaData[i].Date < tvaData[j].Date
	})

	report := &TVAReportResponse{
		Status:   "success",
		Calendar: tvaData,
	}

	return report, nil
}

// GetPaymentsReport retourne le rapport de paiements pour la période donnée
func (s *ReportsService) GetPaymentsReport(ctx context.Context, token, dateFrom, dateTo string) (*PaymentsReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	// Fetch payments data from repository
	paymentsData, err := s.reportsRepo.GetPaymentsReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	// Sort by date
	sort.Slice(paymentsData, func(i, j int) bool {
		return paymentsData[i].Date < paymentsData[j].Date
	})

	report := &PaymentsReportResponse{
		Status:   "success",
		Calendar: paymentsData,
	}

	return report, nil
}

// ExportTVAReport génère un export CSV du rapport TVA jour par jour et
// l'upload vers R2. Réutilise les mêmes données que GetTVAReport, en miroir
// du tableau "Détail TVA" affiché côté back-office.
func (s *ReportsService) ExportTVAReport(ctx context.Context, token, dateFrom, dateTo string, r2Client *r2.Client) (*ExportReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	tvaData, err := s.reportsRepo.GetTVAReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	sort.Slice(tvaData, func(i, j int) bool {
		return tvaData[i].Date < tvaData[j].Date
	})

	csvBytes, err := buildTVACSV(tvaData)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("WR_rapport_tva_%s_%s.csv", dateFrom, dateTo)
	key := fmt.Sprintf("wello_resto_accounting/merchants/%s/reports/%s", merchantID, filename)
	downloadURL, err := r2Client.UploadFile(ctx, key, bytes.NewReader(csvBytes), "text/csv; charset=utf-8")
	if err != nil {
		return nil, err
	}

	return &ExportReportResponse{
		Status:      "1",
		Filename:    filename,
		DownloadURL: downloadURL,
	}, nil
}

// ExportPaymentsReport génère un export CSV du rapport de paiements jour par
// jour et l'upload vers R2. Réutilise les mêmes données que
// GetPaymentsReport, en miroir du tableau "Détail Paiements".
func (s *ReportsService) ExportPaymentsReport(ctx context.Context, token, dateFrom, dateTo string, r2Client *r2.Client) (*ExportReportResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	merchantID := user.MerchantID
	if merchantID == "" {
		return nil, models.ErrUnauthorized
	}

	paymentsData, err := s.reportsRepo.GetPaymentsReportData(ctx, merchantID, dateFrom, dateTo)
	if err != nil {
		return nil, err
	}

	sort.Slice(paymentsData, func(i, j int) bool {
		return paymentsData[i].Date < paymentsData[j].Date
	})

	csvBytes, err := buildPaymentsCSV(paymentsData)
	if err != nil {
		return nil, err
	}

	filename := fmt.Sprintf("WR_rapport_paiements_%s_%s.csv", dateFrom, dateTo)
	key := fmt.Sprintf("wello_resto_accounting/merchants/%s/reports/%s", merchantID, filename)
	downloadURL, err := r2Client.UploadFile(ctx, key, bytes.NewReader(csvBytes), "text/csv; charset=utf-8")
	if err != nil {
		return nil, err
	}

	return &ExportReportResponse{
		Status:      "1",
		Filename:    filename,
		DownloadURL: downloadURL,
	}, nil
}

// formatEuros convertit un montant en centimes vers une chaîne en euros à
// deux décimales (ex: 12345 -> "123.45").
func formatEuros(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

// buildTVACSV construit le CSV "Détail TVA", en miroir de VATDetailTable.tsx :
// une ligne par item de TVA du jour (filtré comme à l'écran sur ttc/ht/tva
// non nuls), suivie d'une ligne de total journalier.
func buildTVACSV(data []TVADayReport) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)

	if err := w.Write([]string{"Date", "Type de TVA", "Service", "TTC", "HT", "TVA"}); err != nil {
		return nil, err
	}

	for _, day := range data {
		for _, item := range day.VATData {
			if item.TTC == 0 && item.HT == 0 && item.TVA == 0 {
				continue
			}

			title := item.TVATitle
			if title == "" {
				title = "Autre"
			}
			service := item.TVADeliveryTypeLabel
			if service == "" {
				service = "-"
			}

			row := []string{
				day.Date,
				title,
				service,
				formatEuros(item.TTC),
				formatEuros(item.HT),
				formatEuros(item.TVA),
			}
			if err := w.Write(row); err != nil {
				return nil, err
			}
		}

		totalRow := []string{
			fmt.Sprintf("Total %s", day.Date),
			"", "",
			formatEuros(day.TTCSum),
			formatEuros(day.HTSum),
			formatEuros(day.TVASum),
		}
		if err := w.Write(totalRow); err != nil {
			return nil, err
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// buildPaymentsCSV construit le CSV "Détail Paiements", en miroir de
// PaymentDetailTable.tsx : une ligne par moyen de paiement du jour, suivie
// d'une ligne de total journalier.
func buildPaymentsCSV(data []PaymentsDayReport) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)

	if err := w.Write([]string{"Date", "Moyen de Paiement", "Montant"}); err != nil {
		return nil, err
	}

	for _, day := range data {
		var total int64
		for _, payment := range day.Payments {
			row := []string{day.Date, payment.Label, formatEuros(payment.Amount)}
			if err := w.Write(row); err != nil {
				return nil, err
			}
			total += payment.Amount
		}

		totalRow := []string{fmt.Sprintf("Total %s", day.Date), "", formatEuros(total)}
		if err := w.Write(totalRow); err != nil {
			return nil, err
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
