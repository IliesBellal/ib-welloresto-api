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
	cashregisters "welloresto-api/internal/modules/cash_registers"

	"github.com/jung-kurt/gofpdf"
)

type AccountingService struct {
	repo              *AccountingRepository
	cashRegistersRepo *cashregisters.CashRegisterRepository
}

func NewAccountingService(repo *AccountingRepository, cashRegistersRepo *cashregisters.CashRegisterRepository) *AccountingService {
	return &AccountingService{repo: repo, cashRegistersRepo: cashRegistersRepo}
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

	// Les dates reçues sont des dates de calendrier nues (YYYY-MM-DD) interprétées
	// dans le fuseau de l'établissement : la période comptable court du premier
	// jour 00:00:00 local au dernier jour 23:59:59 local. Une commande créée le
	// 31/08 à 23h30 heure locale appartient donc au rapport d'août, quelle que
	// soit la date de son encaissement.
	fromLocal, err := parseLocalDate(dateFrom, merchantLoc)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Format date_from invalide. Attendu: YYYY-MM-DD",
		}, nil
	}
	lastDayLocal, err := parseLocalDate(dateTo, merchantLoc)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Format date_to invalide. Attendu: YYYY-MM-DD",
		}, nil
	}

	if lastDayLocal.Before(fromLocal) {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "La date de fin doit être postérieure ou égale à la date de début.",
		}, nil
	}

	// Borne haute exclusive (lendemain 00:00:00 local) : couvre 23:59:59 et ses
	// fractions de seconde, et reste juste lors d'un changement d'heure — un
	// jour peut durer 23h ou 25h, AddDate raisonne en heure murale.
	toExclusive := lastDayLocal.AddDate(0, 0, 1)
	// Borne affichée sur le PDF : dernière seconde incluse dans la période.
	toLocal := toExclusive.Add(-time.Second)

	year := fromLocal.Year()
	month := int(fromLocal.Month())

	// Vérifier que le mois est clôturé
	monthClosed, err := s.repo.IsMonthClosed(ctx, user.MerchantID, year, month, merchantLoc)
	if err != nil || !monthClosed {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Le mois n'est pas encore clôturé. Impossible de générer un rapport officiel.",
		}, nil
	}

	tvaRows, err := s.repo.GetTVAData(ctx, user.MerchantID, fromLocal, toExclusive)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des données TVA",
		}, nil
	}

	// Section Encaissements : réel des registres de caisse enclosed
	// uniquement (cf. docs/decisions.md) — pas de repli sur le théorique
	// (payments) dans ce rapport. Un merchant sans registre correctement
	// clôturé sur la période affiche une table vide, voir buildPDFReport.
	trustedRegisterIDs, err := s.repo.GetTrustedEnclosedRegisterIDs(ctx, user.MerchantID, fromLocal, toExclusive)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des registres de caisse",
		}, nil
	}

	payments, err := s.repo.GetRealPaymentsData(ctx, trustedRegisterIDs)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des paiements",
		}, nil
	}
	payments = filterExcludedPaymentLabels(payments)

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

// accountingExcludedPaymentLabels liste les libellés d'encaissement à exclure
// du rapport comptable, comparés en majuscule pour rester robustes à la casse
// et aux libellés saisis librement (ex. cash_registers_custom_items dont le
// texte ne matche pas les codes MOP bruts déjà filtrés par
// accountingExcludedChannelMOPs côté repository). Ces canaux externes ont leur
// propre gestion de TVA à venir, cf. docs/decisions.md.
var accountingExcludedPaymentLabels = []string{"UBER EATS", "DELIVEROO", "SCANNORDER"}

// filterExcludedPaymentLabels retire du "réel" les encaissements dont le
// libellé correspond (en majuscule) à un canal externe hors périmètre TVA de
// ce rapport.
func filterExcludedPaymentLabels(payments []PaymentRow) []PaymentRow {
	filtered := make([]PaymentRow, 0, len(payments))
	for _, payment := range payments {
		upperLabel := strings.ToUpper(strings.TrimSpace(payment.Label))
		excluded := false
		for _, excludedLabel := range accountingExcludedPaymentLabels {
			if upperLabel == excludedLabel {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		filtered = append(filtered, payment)
	}
	return filtered
}

// buildPDFReport génère le PDF avec les données comptables
func (s *AccountingService) buildPDFReport(year, month int, header *MerchantHeader, tvaRows []TVARow, payments []PaymentRow, fromLocal, toLocal time.Time, tzName string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()

	vatNumber := ""
	if header.VATNumber != nil {
		vatNumber = *header.VATNumber
	}
	infoText := fmt.Sprintf(
		"Période : %s -> %s (%s)\nAdresse : %s\nSIRET : %s\nTVA : %s\nTéléphone : %s",
		fromLocal.Format("02/01/2006 15:04:05"),
		toLocal.Format("02/01/2006 15:04:05"),
		tzName,
		header.Address,
		header.SIRET,
		vatNumber,
		header.Phone,
	)
	drawPDFHeader(pdf, translate, "Rapport comptable", header.MerchantName, fmt.Sprintf("%02d/%d", month, year), infoText)
	drawTVATable(pdf, translate, tvaRows, header.Currency)

	// --- PAYMENTS ---
	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Encaissements"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	paymentRowsDrawn := 0
	for _, payment := range payments {
		if payment.Amount == 0 {
			continue
		}
		pdf.CellFormat(50, 8, translate(payment.Label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(140, 8, translate(fmt.Sprintf("%.2f %s", float64(payment.Amount)/100, header.Currency)), "1", 1, "R", false, 0, "")
		paymentRowsDrawn++
	}
	if paymentRowsDrawn == 0 {
		pdf.SetFont("Arial", "I", 10)
		pdf.MultiCell(190, 6, translate("Aucune clôture de caisse validée sur cette période."), "", "L", false)
	}

	drawPDFFooter(pdf, translate)

	// Convertir en bytes
	buf := new(bytes.Buffer)
	err := pdf.Output(buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// RegisterComparisonRow représente une ligne théorique/réel/écart par moyen
// de paiement pour le PDF d'un registre unique.
type RegisterComparisonRow struct {
	Label       string
	Theoretical int64
	Real        int64
}

// drawPDFHeader dessine l'entête commune aux PDF comptables (rapport mensuel
// et registre unique) : titre, nom du merchant, sous-titre (période
// mois/année ou numéro de registre), puis un bloc d'infos libre.
func drawPDFHeader(pdf *gofpdf.Fpdf, translate func(string) string, title, merchantName, subtitle, infoText string) {
	pdf.SetFont("Arial", "", 12)
	pdf.CellFormat(190, 6, translate(title), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "B", 14)
	pdf.CellFormat(190, 10, translate(merchantName), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 12)
	pdf.CellFormat(190, 6, translate(subtitle), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 11)
	pdf.Ln(5)
	pdf.MultiCell(190, 6, translate(infoText), "", "L", false)
}

// drawTVATable dessine le tableau TVA (Taux/HT/TVA/TTC), commun au rapport
// mensuel et au PDF d'un registre unique.
func drawTVATable(pdf *gofpdf.Fpdf, translate func(string) string, tvaRows []TVARow, currency string) {
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
		pdf.CellFormat(40, 8, translate(fmt.Sprintf("%.2f %s", row.HT/100, currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(40, 8, translate(fmt.Sprintf("%.2f %s", row.TVA/100, currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(50, 8, translate(fmt.Sprintf("%.2f %s", row.TTC/100, currency)), "1", 1, "R", false, 0, "")
	}
}

// drawComparisonTable dessine le tableau théorique/réel/écart par moyen de
// paiement (spécifique au PDF d'un registre unique — pas de section
// équivalente dans le rapport mensuel).
func drawComparisonTable(pdf *gofpdf.Fpdf, translate func(string) string, rows []RegisterComparisonRow, currency string) {
	pdf.Ln(8)
	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(190, 8, translate("Théorique / Réel"), "", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(60, 8, translate("Moyen"), "1", 0, "L", false, 0, "")
	pdf.CellFormat(43, 8, translate("Théorique"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(43, 8, translate("Réel"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(44, 8, translate("Écart"), "1", 1, "R", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	var totalTheoretical, totalReal int64
	for _, row := range rows {
		variance := row.Real - row.Theoretical
		pdf.CellFormat(60, 8, translate(row.Label), "1", 0, "L", false, 0, "")
		pdf.CellFormat(43, 8, translate(fmt.Sprintf("%.2f %s", float64(row.Theoretical)/100, currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(43, 8, translate(fmt.Sprintf("%.2f %s", float64(row.Real)/100, currency)), "1", 0, "R", false, 0, "")
		pdf.CellFormat(44, 8, translate(fmt.Sprintf("%.2f %s", float64(variance)/100, currency)), "1", 1, "R", false, 0, "")
		totalTheoretical += row.Theoretical
		totalReal += row.Real
	}

	pdf.SetFont("Arial", "B", 10)
	totalVariance := totalReal - totalTheoretical
	pdf.CellFormat(60, 8, translate("Total"), "1", 0, "L", false, 0, "")
	pdf.CellFormat(43, 8, translate(fmt.Sprintf("%.2f %s", float64(totalTheoretical)/100, currency)), "1", 0, "R", false, 0, "")
	pdf.CellFormat(43, 8, translate(fmt.Sprintf("%.2f %s", float64(totalReal)/100, currency)), "1", 0, "R", false, 0, "")
	pdf.CellFormat(44, 8, translate(fmt.Sprintf("%.2f %s", float64(totalVariance)/100, currency)), "1", 1, "R", false, 0, "")
}

// drawPDFFooter dessine le pied de page légal, commun aux PDF comptables.
func drawPDFFooter(pdf *gofpdf.Fpdf, translate func(string) string) {
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
}

// buildRegisterPDFReport génère le PDF pour un seul registre de caisse —
// même chrome visuel que buildPDFReport (header/tableau TVA/footer partagés
// via drawPDFHeader/drawTVATable/drawPDFFooter), avec en plus un tableau
// théorique/réel/écart par moyen de paiement à la place de la section
// Encaissements (qui n'a de sens qu'agrégée sur plusieurs registres).
func (s *AccountingService) buildRegisterPDFReport(
	header *MerchantHeader,
	registerNumber string,
	openedBy string,
	closedBy string,
	periodFrom, periodTo time.Time,
	cashFundInitial, cashFundFinal int64,
	tvaRows []TVARow,
	comparisonRows []RegisterComparisonRow,
	tzName string,
) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()

	infoText := fmt.Sprintf(
		"Période : %s -> %s (%s)\nOuvert par : %s\nClôturé par : %s\nFond de caisse initial : %.2f %s\nFond de caisse final : %.2f %s\nAdresse : %s\nSIRET : %s",
		periodFrom.Format("02/01/2006 15:04:05"),
		periodTo.Format("02/01/2006 15:04:05"),
		tzName,
		openedBy,
		closedBy,
		float64(cashFundInitial)/100, header.Currency,
		float64(cashFundFinal)/100, header.Currency,
		header.Address,
		header.SIRET,
	)
	drawPDFHeader(pdf, translate, "Registre de caisse", header.MerchantName, fmt.Sprintf("#%s", registerNumber), infoText)
	drawTVATable(pdf, translate, tvaRows, header.Currency)
	drawComparisonTable(pdf, translate, comparisonRows, header.Currency)
	drawPDFFooter(pdf, translate)

	buf := new(bytes.Buffer)
	if err := pdf.Output(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ExportRegisterPDF génère le PDF d'un seul registre de caisse (théorique/
// réel + TVA), en réutilisant le générateur du rapport comptable mensuel.
// Le registre doit être enclosed : ce PDF s'appuie sur les mêmes données
// figées que le rapport mensuel fait confiance une fois clôturées.
func (s *AccountingService) ExportRegisterPDF(ctx context.Context, registerID string) ([]byte, string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, "", models.ErrUnauthorized
	}

	// GetCashRegisterTVADetails filtre par merchant_id (WHERE cash_register_id = ?
	// AND merchant_id = ?) — sert de garde d'autorisation avant tout accès à
	// GetCashRegisterSummary, qui lui ne filtre pas par merchant.
	details, err := s.cashRegistersRepo.GetCashRegisterTVADetails(ctx, user.MerchantID, registerID)
	if err != nil {
		return nil, "", err
	}
	if details == nil {
		return nil, "", fmt.Errorf("registre introuvable")
	}

	summaryResp, err := s.cashRegistersRepo.GetCashRegisterSummary(ctx, registerID, user.MerchantID)
	if err != nil {
		return nil, "", err
	}
	if summaryResp == nil || summaryResp.CashRegister == nil {
		return nil, "", fmt.Errorf("registre introuvable")
	}
	summary := summaryResp.CashRegister

	if !summary.Enclosed {
		return nil, "", fmt.Errorf("le registre doit être clôturé pour être exporté")
	}

	header, err := s.repo.GetMerchantHeader(ctx, user.MerchantID)
	if err != nil {
		return nil, "", err
	}

	tzName := strings.TrimSpace(header.Timezone)
	if tzName == "" {
		tzName = "Europe/Paris"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.FixedZone("UTC", 0)
		tzName = "UTC"
	}

	periodFrom := time.Unix(summary.StartDate, 0).In(loc)
	periodTo := time.Now().In(loc)
	if summary.EndDate != nil {
		periodTo = time.Unix(int64(*summary.EndDate), 0).In(loc)
	}

	pdfBytes, err := s.buildRegisterPDFReport(
		header,
		summary.CashRegisterID,
		formatUserName(summary.OpenedBy),
		formatUserName(summary.ClosedBy),
		periodFrom,
		periodTo,
		int64(summary.CashFund),
		int64(summary.FinalCashFund),
		buildRegisterTVARows(details),
		buildComparisonRows(summary.Items, summary.CustomItems),
		tzName,
	)
	if err != nil {
		return nil, "", err
	}

	filename := fmt.Sprintf("WR_registre_%s.pdf", summary.CashRegisterID)
	return pdfBytes, filename, nil
}

// formatUserName construit un nom affichable à partir d'un UserBaseInfo dont
// les champs sont nullable en base.
func formatUserName(u models.UserBaseInfo) string {
	first := ""
	if u.FirstName != nil {
		first = *u.FirstName
	}
	last := ""
	if u.LastName != nil {
		last = *u.LastName
	}
	name := strings.TrimSpace(first + " " + last)
	if name == "" {
		return "-"
	}
	return name
}

// buildRegisterTVARows aplatit le détail TVA par type de service
// (CashReportDeliveryGroup) d'un registre en lignes TVARow fusionnées par
// (titre, taux) — même shape que le tableau TVA du rapport mensuel.
func buildRegisterTVARows(details *models.CashRegisterDetails) []TVARow {
	type acc struct {
		title string
		rate  float64
		ht    float64
		ttc   float64
		tva   float64
	}
	rows := make(map[string]*acc)
	var order []string

	for _, group := range details.CashReport {
		for _, cat := range group.TVACategories {
			if cat.HT == 0 && cat.TTC == 0 && cat.TVA == 0 {
				continue
			}
			key := fmt.Sprintf("%s|%.2f", cat.TVATitle, cat.Rate)
			row, ok := rows[key]
			if !ok {
				row = &acc{title: cat.TVATitle, rate: cat.Rate}
				rows[key] = row
				order = append(order, key)
			}
			row.ht += float64(cat.HT)
			row.ttc += float64(cat.TTC)
			row.tva += float64(cat.TVA)
		}
	}

	result := make([]TVARow, 0, len(order))
	for _, key := range order {
		r := rows[key]
		result = append(result, TVARow{TVATitle: r.title, Rate: r.rate, HT: r.ht, TTC: r.ttc, TVA: r.tva})
	}
	return result
}

// buildComparisonRows regroupe les items théorique (CRItem, par MOP) et réel
// (CRCustomItem, par MOP) d'un registre en lignes théorique/réel par moyen
// de paiement — même logique de regroupement que ClosureModal.tsx
// (theoreticalByMop/realByMop) côté back-office, reproduite ici pour le PDF.
func buildComparisonRows(items []models.CRItem, customItems []models.CRCustomItem) []RegisterComparisonRow {
	type acc struct {
		label       string
		theoretical int64
		real        int64
	}
	rows := make(map[string]*acc)
	var order []string

	for _, item := range items {
		label := item.MOP
		if item.Label != nil && strings.TrimSpace(*item.Label) != "" {
			label = *item.Label
		}
		row, ok := rows[item.MOP]
		if !ok {
			row = &acc{label: label}
			rows[item.MOP] = row
			order = append(order, item.MOP)
		}
		row.theoretical += int64(math.Round(item.Amount))
	}

	for _, item := range customItems {
		key := item.MOP
		if key == "" {
			key = "OTHER"
		}
		row, ok := rows[key]
		if !ok {
			row = &acc{label: item.Label}
			rows[key] = row
			order = append(order, key)
		}
		row.real += int64(math.Round(item.Amount))
	}

	result := make([]RegisterComparisonRow, 0, len(order))
	for _, key := range order {
		r := rows[key]
		result = append(result, RegisterComparisonRow{Label: r.label, Theoretical: r.theoretical, Real: r.real})
	}
	return result
}

// parseLocalDate interprète une date de calendrier nue ('YYYY-MM-DD') comme le
// début de journée dans le fuseau de l'établissement. Les valeurs horodatées
// héritées ('YYYY-MM-DD HH:MM:SS', RFC3339) sont tolérées : seule la partie
// date est retenue, l'heure reçue n'ayant aucun sens comme borne comptable.
func parseLocalDate(raw string, loc *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}

	if len(value) > 10 {
		value = value[:10]
	}

	t, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date format: %s", raw)
	}

	return t, nil
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
