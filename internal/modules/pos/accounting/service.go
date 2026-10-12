package accounting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
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

// ExportAccountingReport génère un rapport comptable en PDF et l'upload vers R2.
// Le contenu dépend du mode de clôture de l'établissement sur la période
// (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md) : en MANUAL, le rapport historique
// (TVA sur les lignes, remises déduites ; encaissements = réel des registres
// validés), non filtrable ; en AUTO, TVA ventilée à partir des paiements,
// filtrable par canal (channels : valeurs de orders.order_source, vide = tous).
func (s *AccountingService) ExportAccountingReport(ctx context.Context, token, dateFrom, dateTo string, channels []string, r2Client *r2.Client) (*ExportAccountingResponse, error) {
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

	// Mode de clôture de la période : celui en vigueur au premier jour, sans
	// changement de mode à l'intérieur de la période.
	closingMode, err := s.periodClosingMode(ctx, user.MerchantID, fromLocal, lastDayLocal)
	if err != nil {
		var straddle *closingModeStraddleError
		if errors.As(err, &straddle) {
			return &ExportAccountingResponse{
				Status: "0",
				Error: fmt.Sprintf("La période chevauche un changement de mode de clôture le %s : générez un export avant et un export à partir de cette date.",
					straddle.effectiveFrom.Format("02/01/2006")),
			}, nil
		}
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération du mode de clôture",
		}, nil
	}

	sources, err := normalizeOrderSources(channels)
	if err != nil {
		return &ExportAccountingResponse{Status: "0", Error: err.Error()}, nil
	}
	if closingMode == cashregisters.ClosingModeManual && len(channels) > 0 {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Le filtre par canal de commande n'est disponible qu'en clôture automatique.",
		}, nil
	}

	var (
		tvaRows      []TVARow
		payments     []PaymentRow
		discounts    DiscountSummary
		emptyMessage string
	)
	if closingMode == cashregisters.ClosingModeAuto {
		tvaRows, payments, discounts, err = s.buildAutoSections(ctx, user.MerchantID, fromLocal, toExclusive, orderScope{sources: sources})
		emptyMessage = "Aucun encaissement sur cette période."
	} else {
		tvaRows, payments, discounts, err = s.buildManualSections(ctx, user.MerchantID, fromLocal, toExclusive)
		emptyMessage = "Aucune clôture de caisse validée sur cette période."
	}
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la récupération des données comptables",
		}, nil
	}

	// Construire le PDF
	pdfBytes, err := s.buildPDFReport(year, month, header, tvaRows, payments, discounts, emptyMessage, fromLocal, toLocal, tzName)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la génération du PDF",
		}, nil
	}

	// Archivage (migration 166) : bucket R2 privé, nom unique horodaté — un
	// export régénéré n'écrase jamais celui déjà envoyé —, référence en base
	// avec l'empreinte du fichier, téléchargement par lien signé.
	generatedAt := time.Now().UTC()
	filename := accountingExportFilename(fromLocal, lastDayLocal, sources, generatedAt)
	key := fmt.Sprintf("wello_resto_accounting/merchants/%s/reports/%s", user.MerchantID, filename)
	if _, err := r2Client.UploadPrivateFile(ctx, key, bytes.NewReader(pdfBytes), "application/pdf"); err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de l'upload du PDF vers R2",
		}, nil
	}

	digest := sha256.Sum256(pdfBytes)
	exportID, err := s.repo.InsertAccountingExport(ctx, user.MerchantID, AccountingExport{
		PeriodFrom:  fromLocal.Format("2006-01-02"),
		PeriodTo:    lastDayLocal.Format("2006-01-02"),
		ClosingMode: closingMode,
		Channels:    sources,
		Filename:    filename,
		SHA256:      hex.EncodeToString(digest[:]),
		SizeBytes:   len(pdfBytes),
		GeneratedBy: user.UserID,
		GeneratedAt: generatedAt,
		r2Key:       key,
	})
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de l'enregistrement de l'export",
		}, nil
	}

	downloadURL, err := r2Client.GenerateSignedURL(ctx, key, accountingExportLinkTTL)
	if err != nil {
		return &ExportAccountingResponse{
			Status: "0",
			Error:  "Erreur lors de la création du lien de téléchargement",
		}, nil
	}

	return &ExportAccountingResponse{
		Status:      "1",
		ExportID:    exportID,
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
// S'y ajoutent les remises accordées en caisse (models.IsDiscountPaymentLabel) :
// ce n'est pas de l'argent encaissé, et elles sont déjà déduites de la base
// TVA (buildManualVAT).
var accountingExcludedPaymentLabels = []string{"UBER EATS", "DELIVEROO", "SCANNORDER"}

// filterExcludedPaymentLabels retire du "réel" les encaissements dont le
// libellé correspond (en majuscule) à un canal externe hors périmètre TVA de
// ce rapport.
func filterExcludedPaymentLabels(payments []PaymentRow) []PaymentRow {
	filtered := make([]PaymentRow, 0, len(payments))
	for _, payment := range payments {
		upperLabel := strings.ToUpper(strings.TrimSpace(payment.Label))
		excluded := models.IsDiscountPaymentLabel(payment.Label)
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

// buildPDFReport génère le PDF avec les données comptables. discounts alimente
// la ligne d'information placée sous le tableau TVA, hors de tout total (rien
// si aucune remise) ; emptyPaymentsMessage s'affiche quand aucun encaissement
// n'est à lister (message propre à chaque mode de clôture).
func (s *AccountingService) buildPDFReport(year, month int, header *MerchantHeader, tvaRows []TVARow, payments []PaymentRow, discounts DiscountSummary, emptyPaymentsMessage string, fromLocal, toLocal time.Time, tzName string) ([]byte, error) {
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
	drawDiscountSummary(pdf, translate, discounts, header.Currency)
	drawPaymentsSection(pdf, translate, payments, header.Currency, emptyPaymentsMessage)

	drawPDFFooter(pdf, translate)

	// Convertir en bytes
	buf := new(bytes.Buffer)
	err := pdf.Output(buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// drawPaymentsSection dessine la section « Encaissements » (un libellé et un
// montant par moyen de paiement), commune au rapport mensuel et au PDF d'un
// registre. emptyMessage s'affiche quand aucun encaissement n'est à lister.
func drawPaymentsSection(pdf *gofpdf.Fpdf, translate func(string) string, payments []PaymentRow, currency, emptyMessage string) {
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
		pdf.CellFormat(140, 8, translate(fmt.Sprintf("%.2f %s", float64(payment.Amount)/100, currency)), "1", 1, "R", false, 0, "")
		paymentRowsDrawn++
	}
	if paymentRowsDrawn == 0 {
		pdf.SetFont("Arial", "I", 10)
		pdf.MultiCell(190, 6, translate(emptyMessage), "", "L", false)
	}
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
// drawDiscountSummary affiche sous le tableau TVA, en information et hors de
// tout total, le CA avant remises, les remises accordées et le CA TTC. Rien si
// aucune remise sur la période.
func drawDiscountSummary(pdf *gofpdf.Fpdf, translate func(string) string, discounts DiscountSummary, currency string) {
	if discounts.Discounts == 0 {
		return
	}
	pdf.Ln(2)
	pdf.SetFont("Arial", "I", 9)
	pdf.MultiCell(190, 5, translate(fmt.Sprintf(
		"CA TTC avant remises : %.2f %s  ·  Remises accordées : %.2f %s  ·  CA TTC : %.2f %s",
		float64(discounts.TTCBeforeDiscounts)/100, currency,
		float64(discounts.Discounts)/100, currency,
		float64(discounts.TTC)/100, currency,
	)), "", "L", false)
}

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

// CalculateVAT calcule la déclaration de TVA de la période (dates de
// calendrier dans le fuseau de l'établissement, bornes incluses), alignée sur
// l'export comptable — cf. vat_declaration.go. Channels : valeurs de
// orders.order_source (ou anciennes valeurs restaurant / scannorder / ubereats
// / deliveroo) ; OrderTypes : in / take_away / delivery ; vides = tout.
func (s *AccountingService) CalculateVAT(ctx context.Context, req VATCalculateRequest) (*VATCalculateResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}

	loc, err := s.cashRegistersRepo.MerchantLocation(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	fromLocal, err := parseCalendarDate(req.StartDate, loc)
	if err != nil {
		return nil, models.ErrInvalidInput
	}
	lastDayLocal, err := parseCalendarDate(req.EndDate, loc)
	if err != nil {
		return nil, models.ErrInvalidInput
	}
	if lastDayLocal.Before(fromLocal) {
		return nil, models.ErrInvalidInput
	}
	toExclusive := lastDayLocal.AddDate(0, 0, 1)

	sources, channelKeys, err := normalizeDeclarationChannels(req.Channels)
	if err != nil {
		return nil, err
	}
	orderTypes, err := normalizeOrderTypes(req.OrderTypes)
	if err != nil {
		return nil, models.ErrInvalidInput
	}
	var orderTypeFilter []string
	if len(orderTypes) < 3 {
		for _, t := range orderTypes {
			orderTypeFilter = append(orderTypeFilter, strings.ToUpper(t))
		}
	}

	rows, err := s.vatDeclarationRows(ctx, user.MerchantID, fromLocal, toExclusive, orderScope{sources: sources, orderTypes: orderTypeFilter})
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
				Month:       row.Month,
				ClosingMode: row.ClosingMode,
				VATByRate:   map[string]int64{},
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

	for _, channel := range channelKeys {
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

// formatCSVAmount écrit un montant en centimes en euros (jusqu'au 2026-10-05,
// le CSV affichait les centimes comme des euros : 12,34 € sortait « 1234.00 »).
func formatCSVAmount(v int64) string {
	return fmt.Sprintf("%.2f", float64(v)/100)
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
