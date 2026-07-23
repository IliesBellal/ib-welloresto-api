package accounting

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"welloresto-api/internal/infrastructure/r2"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"github.com/jung-kurt/gofpdf"
)

type AccountingService struct {
	repo *AccountingRepository
}

func NewAccountingService(repo *AccountingRepository) *AccountingService {
	return &AccountingService{repo: repo}
}

// ExportAccountingReport génère un rapport comptable en PDF et l'upload vers R2
func (s *AccountingService) ExportAccountingReport(ctx context.Context, token, dateFrom, dateTo string, r2Client *r2.Client) (*ExportAccountingResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	// Récupérer les infos merchant (dont timezone) pour traiter la période en heure locale établissement.
	header, err := s.repo.GetMerchantHeader(ctx, user.MerchantID)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des infos du merchant",
		}, nil
	}

	tzName := strings.TrimSpace(header.Timezone)
	if tzName == "" {
		tzName = "Europe/Paris"
	}

	merchantLoc, err := time.LoadLocation(tzName)
	if err != nil {
		merchantLoc = time.FixedZone("UTC", 0)
		tzName = "UTC"
	}

	// Parse les dates UTC reçues par les applications puis conversion en TZ merchant.
	fromUTC, err := parseUTCDateTime(dateFrom)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Format date_from invalide. Attendu: YYYY-MM-DD HH:MM:SS",
		}, nil
	}
	toUTC, err := parseUTCDateTime(dateTo)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Format date_to invalide. Attendu: YYYY-MM-DD HH:MM:SS",
		}, nil
	}

	fromLocal := fromUTC.In(merchantLoc)
	toLocal := toUTC.In(merchantLoc)

	year := fromLocal.Year()
	month := int(fromLocal.Month())

	// Vérifier que le mois est clôturé
	monthClosed, err := s.repo.IsMonthClosed(ctx, user.MerchantID, strconv.Itoa(year), fmt.Sprintf("%02d", month))
	if err != nil || !monthClosed {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Le mois n'est pas encore clôturé. Impossible de générer un rapport officiel.",
		}, nil
	}

	tvaRows, err := s.repo.GetTVAData(ctx, user.MerchantID, dateFrom, dateTo)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des données TVA",
		}, nil
	}

	payments, err := s.repo.GetPaymentsData(ctx, user.MerchantID, dateFrom, dateTo)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des paiements",
		}, nil
	}

	// Construire le PDF
	pdfBytes, err := s.buildPDFReport(year, month, header, tvaRows, payments, fromLocal, toLocal, tzName)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la génération du PDF",
		}, nil
	}

	filename := fmt.Sprintf("WR_rapport_comptable_%d_%02d.pdf", year, month)

	// Uploader vers R2
	key := fmt.Sprintf("wello_resto_accounting/merchants/%s/reports/%s", user.MerchantID, filename)
	downloadURL, err := r2Client.UploadFile(ctx, key, bytes.NewReader(pdfBytes), "application/pdf")
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de l'upload du PDF vers R2",
		}, nil
	}

	return &ExportAccountingResponse{
		Status:      "1",
		Filename:    filename,
		DownloadURL: downloadURL,
	}, nil
}

// buildPDFReport génère le PDF avec les données comptables
func (s *AccountingService) buildPDFReport(year, month int, header *MerchantHeader, tvaRows []TVARow, payments []PaymentRow, fromLocal, toLocal time.Time, tzName string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()
	pdf.SetFont("Arial", "", 12)

	// --- HEADER ---
	pdf.CellFormat(190, 6, translate("Rapport comptable"), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(190, 10, translate(header.MerchantName), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 12)
	pdf.CellFormat(190, 6, fmt.Sprintf("%02d/%d", month, year), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	pdf.Ln(5)
	vatNumber := ""
	if header.VATNumber != nil {
		vatNumber = *header.VATNumber
	}
	headerText := fmt.Sprintf(
		"Période : %s -> %s (%s)\nAdresse : %s\nSIRET : %s\nTVA : %s\nTéléphone : %s",
		fromLocal.Format("02/01/2006 15:04:05"),
		toLocal.Format("02/01/2006 15:04:05"),
		tzName,
		header.Address,
		header.SIRET,
		vatNumber,
		header.Phone,
	)
	pdf.MultiCell(190, 6, translate(headerText), "", "L", false)

	// --- TVA ---
	pdf.Ln(5)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("TVA"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(60, 8, translate("Taux"), "1", 0, "L", false, 0, "")
	pdf.CellFormat(40, 8, "HT", "1", 0, "R", false, 0, "")
	pdf.CellFormat(40, 8, "TVA", "1", 0, "R", false, 0, "")
	pdf.CellFormat(50, 8, "TTC", "1", 1, "R", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	for _, row := range tvaRows {
		pdf.CellFormat(60, 8, translate(fmt.Sprintf("%s (%.1f%%)", row.TVATitle, row.Rate)), "1", 0, "L", false, 0, "")
		pdf.CellFormat(40, 8, translate(fmt.Sprintf("%.2f %s", row.HT/100, header.Currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(40, 8, translate(fmt.Sprintf("%.2f %s", row.TVA/100, header.Currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(50, 8, translate(fmt.Sprintf("%.2f %s", row.TTC/100, header.Currency)), "1", 1, "R", false, 0, "")
	}

	// --- PAYMENTS ---
	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Encaissements"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	for _, payment := range payments {
		if payment.Amount == 0 {
			continue
		}
		pdf.CellFormat(50, 8, translate(payment.Label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(140, 8, translate(fmt.Sprintf("%.2f %s", float64(payment.Amount)/100, header.Currency)), "1", 1, "R", false, 0, "")
	}

	// --- FOOTER ---
	pdf.Ln(10)
	pdf.SetFont("Arial", "I", 9)
	footerText := "Document comptable généré automatiquement par le système WR.\n" +
		"Conforme aux exigences fiscales françaises concernant la conservation des données,\n" +
		"notamment la loi de 2018 sur les systèmes de caisse certifiés.\n\n" +
		"Ce rapport doit être conservé pendant au moins 6 années (Code général des impôts).\n" +
		"Les montants affichés sont arrondis conformément aux règles comptables."
	pdf.MultiCell(190, 6, translate(footerText), "", "L", false)
	pdf.Ln(2)

	pdf.SetFont("Arial", "I", 9)
	pdf.CellFormat(190, 6, translate(fmt.Sprintf("Généré le : %s", time.Now().Format("2006-01-02 15:04"))), "", 1, "R", false, 0, "")

	// Convertir en bytes
	buf := new(bytes.Buffer)
	err := pdf.Output(buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func parseUTCDateTime(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}

	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), nil
	}

	if t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC); err == nil {
		return t.UTC(), nil
	}

	if t, err := time.ParseInLocation("2006-01-02", value, time.UTC); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("invalid datetime format: %s", value)
}

func (s *AccountingService) CalculateVAT(ctx context.Context, req VATCalculateRequest) (*VATCalculateResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	fromUTC, err := parseUTCDateTime(req.StartDate)
	if err != nil {
		return nil, models.ErrInvalidInput
	}
	toUTC, err := parseUTCDateTime(req.EndDate)
	if err != nil {
		return nil, models.ErrInvalidInput
	}

	if toUTC.Before(fromUTC) {
		return nil, models.ErrInvalidInput
	}

	channels, err := normalizeChannels(req.Channels)
	if err != nil {
		return nil, models.ErrInvalidInput
	}

	orderTypes, err := normalizeOrderTypes(req.OrderTypes)
	if err != nil {
		return nil, models.ErrInvalidInput
	}

	rows, err := s.repo.GetVATAggregationRows(ctx, user.MerchantID, fromUTC, toUTC, channels, orderTypes)
	if err != nil {
		return nil, err
	}

	resp := &VATCalculateResponse{
		TotalVAT:         0,
		VATByRate:        map[string]VATRateBreakdown{},
		MonthlyBreakdown: []VATMonthlyBreakdown{},
		ByChannel:        map[string]VATShare{},
		ByOrderType:      map[string]VATShare{},
	}

	monthlyMap := map[string]*VATMonthlyBreakdown{}
	channelVAT := map[string]int64{}
	orderTypeVAT := map[string]int64{}

	for _, row := range rows {
		rateKey := formatVATRateKey(row.Rate)
		rateAgg := resp.VATByRate[rateKey]
		rateAgg.Amount += row.VATCents
		rateAgg.BaseHT += row.HTCents
		resp.VATByRate[rateKey] = rateAgg

		monthAgg := monthlyMap[row.Month]
		if monthAgg == nil {
			monthAgg = &VATMonthlyBreakdown{
				Month:     row.Month,
				VATByRate: map[string]int64{},
			}
			monthlyMap[row.Month] = monthAgg
		}

		monthAgg.RevenueHT += row.HTCents
		monthAgg.RevenueTTC += row.TTCCents
		monthAgg.VATTotal += row.VATCents
		monthAgg.VATByRate[rateKey] += row.VATCents

		channelVAT[row.Channel] += row.VATCents
		orderTypeVAT[row.OrderType] += row.VATCents
		resp.TotalVAT += row.VATCents
	}

	monthKeys := make([]string, 0, len(monthlyMap))
	for month := range monthlyMap {
		monthKeys = append(monthKeys, month)
	}
	sort.Strings(monthKeys)
	for _, month := range monthKeys {
		resp.MonthlyBreakdown = append(resp.MonthlyBreakdown, *monthlyMap[month])
	}

	for _, channel := range channels {
		vat := channelVAT[channel]
		resp.ByChannel[channel] = VATShare{
			VAT:        vat,
			Percentage: computePercentage(vat, resp.TotalVAT),
		}
	}

	for _, orderType := range orderTypes {
		vat := orderTypeVAT[orderType]
		resp.ByOrderType[orderType] = VATShare{
			VAT:        vat,
			Percentage: computePercentage(vat, resp.TotalVAT),
		}
	}

	return resp, nil
}

func (s *AccountingService) ExportVATCSV(ctx context.Context, req VATCalculateRequest) ([]byte, string, error) {
	resp, err := s.CalculateVAT(ctx, req)
	if err != nil {
		return nil, "", err
	}

	// Collect all unique rates from all months and sort them
	rateSet := make(map[string]struct{})
	for _, month := range resp.MonthlyBreakdown {
		for rate := range month.VATByRate {
			rateSet[rate] = struct{}{}
		}
	}
	rates := make([]string, 0, len(rateSet))
	for rate := range rateSet {
		rates = append(rates, rate)
	}
	sort.Strings(rates)

	// Build header dynamically
	header := []string{"Période", "CA HT"}
	for _, rate := range rates {
		header = append(header, fmt.Sprintf("TVA %s%%", rate))
	}
	header = append(header, "TVA Totale", "CA TTC")

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)

	if err := writer.Write(header); err != nil {
		return nil, "", err
	}

	for _, row := range resp.MonthlyBreakdown {
		record := []string{
			formatPeriodFR(row.Month),
			formatCSVAmount(row.RevenueHT),
		}
		for _, rate := range rates {
			record = append(record, formatCSVAmount(row.VATByRate[rate]))
		}
		record = append(record,
			formatCSVAmount(row.VATTotal),
			formatCSVAmount(row.RevenueTTC),
		)
		if err := writer.Write(record); err != nil {
			return nil, "", err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, "", err
	}

	filename := fmt.Sprintf("vat_export_%s_%s.csv", normalizeDateForFilename(req.StartDate), normalizeDateForFilename(req.EndDate))
	return buffer.Bytes(), filename, nil
}

func normalizeChannels(values []string) ([]string, error) {
	allowed := map[string]struct{}{
		"restaurant": {},
		"scannorder": {},
		"ubereats":   {},
		"deliveroo":  {},
	}

	if len(values) == 0 {
		return []string{"restaurant", "scannorder", "ubereats", "deliveroo"}, nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowed[v]; !ok {
			return nil, fmt.Errorf("invalid channel: %s", raw)
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	return out, nil
}

func normalizeOrderTypes(values []string) ([]string, error) {
	allowed := map[string]struct{}{
		"in":        {},
		"take_away": {},
		"delivery":  {},
	}

	if len(values) == 0 {
		return []string{"in", "take_away", "delivery"}, nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowed[v]; !ok {
			return nil, fmt.Errorf("invalid order_type: %s", raw)
		}
		if _, exists := seen[v]; exists {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	return out, nil
}

func formatVATRateKey(rate float64) string {
	rounded := math.Round(rate*10) / 10
	if rounded == 10.0 {
		return "10.0"
	}
	if rounded == math.Trunc(rounded) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}

func normalizeRateForMonthly(rate float64) string {
	// We keep the same key logic but force 10 -> 10.0 for the monthly field mapping.
	key := formatVATRateKey(rate)
	if key == "10" {
		return "10.0"
	}
	return key
}

func computePercentage(part, total int64) int64 {
	if total <= 0 {
		return 0
	}
	return int64(math.Round((float64(part) * 100.0) / float64(total)))
}

func formatCSVAmount(v int64) string {
	return fmt.Sprintf("%.2f", float64(v))
}

func formatPeriodFR(monthKey string) string {
	parts := strings.Split(monthKey, "-")
	if len(parts) != 2 {
		return monthKey
	}

	monthNum, err := strconv.Atoi(parts[1])
	if err != nil || monthNum < 1 || monthNum > 12 {
		return monthKey
	}

	months := []string{
		"janvier",
		"février",
		"mars",
		"avril",
		"mai",
		"juin",
		"juillet",
		"août",
		"septembre",
		"octobre",
		"novembre",
		"décembre",
	}

	return fmt.Sprintf("%s %s", months[monthNum-1], parts[0])
}

func normalizeDateForFilename(value string) string {
	v := strings.TrimSpace(value)
	if t, err := parseUTCDateTime(v); err == nil {
		return t.Format("2006-01-02")
	}
	v = strings.ReplaceAll(v, ":", "-")
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "T", "_")
	v = strings.ReplaceAll(v, "/", "-")
	if v == "" {
		return "unknown"
	}
	return v
}
