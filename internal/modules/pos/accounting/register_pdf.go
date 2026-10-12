package accounting

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	cashregisters "welloresto-api/internal/modules/cash_registers"

	"github.com/jung-kurt/gofpdf"
	"go.uber.org/zap"
)

// PDF des registres de caisse (docs/EXPORT_REGISTRES_PDF.md). Le PDF d'un
// registre est le rapport comptable restreint à ce registre : même TVA, même
// section « Encaissements », selon le mode de clôture du registre —
//   - MANUAL : TVA sur les lignes, remises déduites (buildManualVAT) ;
//     encaissements = réel du relevé de caisse (cash_registers_custom_items) ;
//   - AUTO : TVA ventilée à partir des paiements (buildAutoReport) ;
//     encaissements = paiements enregistrés (théorique, pas de relevé).

// RegisterExportMax est le plafond de registres d'un export par lot : la
// génération est synchrone, le plafond borne la durée de la requête.
const RegisterExportMax = 100

// RegisterExportRequest désigne les registres à exporter : soit une liste
// d'identifiants, soit une période — jamais les deux. La période est celle de
// l'historique des registres (POST /cash_register/history) : mêmes bornes sur la
// date d'ouverture (« YYYY-MM-DD » ou « YYYY-MM-DD HH:MM:SS », une date seule
// couvrant la journée entière) et même visibilité (registres de l'utilisateur,
// tous pour un administrateur) — « toute la période » est exactement ce que la
// liste affiche.
type RegisterExportRequest struct {
	RegisterIDs []string `json:"register_ids"`
	DateFrom    string   `json:"date_from"`
	DateTo      string   `json:"date_to"`
}

// RegistersExport est le ZIP d'un export par lot.
type RegistersExport struct {
	Data     []byte
	Filename string
	Exported int
	Ignored  int
}

// merchantPDFContext regroupe ce qui est commun à tous les PDF d'un même
// export : en-tête de l'établissement et fuseau d'affichage.
type merchantPDFContext struct {
	header *MerchantHeader
	tzName string
	loc    *time.Location
}

func (s *AccountingService) loadMerchantPDFContext(ctx context.Context, merchantID string) (*merchantPDFContext, error) {
	header, err := s.repo.GetMerchantHeader(ctx, merchantID)
	if err != nil {
		return nil, err
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
	return &merchantPDFContext{header: header, tzName: tzName, loc: loc}, nil
}

// registerPDF est le PDF généré d'un registre.
type registerPDF struct {
	PDF        []byte
	Filename   string
	StartLocal time.Time
}

// registerPDFData est le contenu d'un PDF de registre.
type registerPDFData struct {
	Header               *MerchantHeader
	RegisterNumber       string
	OpenedBy, ClosedBy   string
	From, To             time.Time
	CashFundInitial      int64
	CashFundFinal        int64
	ClosingMode          string
	TVARows              []TVARow
	Discounts            DiscountSummary
	Payments             []PaymentRow
	EmptyPaymentsMessage string
	// Drifted : les paiements du registre ont changé depuis sa clôture
	// (cf. AccountingRepository.IsRegisterTrusted) — mention sur le PDF.
	Drifted bool
	TZName  string
}

// registerDriftNotice est la mention d'un registre en dérive : le rapport
// mensuel l'écarte, son PDF est produit quand même, avec cet avertissement.
const registerDriftNotice = "Attention : les paiements de ce registre ont été modifiés après sa clôture. " +
	"Les encaissements (relevé figé à la clôture) et la TVA (recalculée sur les commandes) peuvent donc différer. " +
	"Ce registre est écarté du rapport comptable mensuel."

func closingModeLabel(mode string) string {
	if mode == cashregisters.ClosingModeAuto {
		return "Automatique (encaissements = paiements enregistrés)"
	}
	return "Manuel (encaissements = relevé de caisse)"
}

// buildRegisterPDFReport dessine le PDF d'un registre : entête, TVA, remises,
// encaissements — mêmes briques que le rapport mensuel (buildPDFReport).
func buildRegisterPDFReport(d registerPDFData) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("cp1252")
	pdf.AddPage()

	infoText := fmt.Sprintf(
		"Période : %s -> %s (%s)\nOuvert par : %s\nClôturé par : %s\nFond de caisse initial : %.2f %s\nFond de caisse final : %.2f %s\nMode de clôture : %s\nAdresse : %s\nSIRET : %s",
		d.From.Format("02/01/2006 15:04:05"),
		d.To.Format("02/01/2006 15:04:05"),
		d.TZName,
		d.OpenedBy,
		d.ClosedBy,
		float64(d.CashFundInitial)/100, d.Header.Currency,
		float64(d.CashFundFinal)/100, d.Header.Currency,
		closingModeLabel(d.ClosingMode),
		d.Header.Address,
		d.Header.SIRET,
	)
	drawPDFHeader(pdf, translate, "Registre de caisse", d.Header.MerchantName, fmt.Sprintf("#%s", d.RegisterNumber), infoText)
	drawTVATable(pdf, translate, d.TVARows, d.Header.Currency)
	drawDiscountSummary(pdf, translate, d.Discounts, d.Header.Currency)
	drawPaymentsSection(pdf, translate, d.Payments, d.Header.Currency, d.EmptyPaymentsMessage)
	if d.Drifted {
		pdf.Ln(4)
		pdf.SetFont("Arial", "I", 10)
		pdf.MultiCell(190, 5, translate(registerDriftNotice), "", "L", false)
	}
	drawPDFFooter(pdf, translate)

	buf := new(bytes.Buffer)
	if err := pdf.Output(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildRegisterPDF produit le PDF d'un registre de l'établissement. Renvoie
// models.ErrNotFound si le registre n'existe pas ou appartient à un autre
// établissement, models.ErrRegisterNotEnclosed s'il n'est pas validé : le PDF
// s'appuie sur des données figées, comme le rapport mensuel.
func (s *AccountingService) buildRegisterPDF(ctx context.Context, mc *merchantPDFContext, merchantID, registerID string) (*registerPDF, error) {
	regID, err := strconv.ParseInt(registerID, 10, 64)
	if err != nil {
		return nil, models.ErrNotFound
	}

	// GetCashRegisterSummary ne filtre pas par établissement : cette garde passe
	// avant tout accès au registre.
	owned, err := s.cashRegistersRepo.CashRegisterBelongsToMerchant(ctx, registerID, merchantID)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, models.ErrNotFound
	}

	summaryResp, err := s.cashRegistersRepo.GetCashRegisterSummary(ctx, registerID, merchantID)
	if err != nil {
		return nil, err
	}
	if summaryResp == nil || summaryResp.CashRegister == nil {
		return nil, models.ErrNotFound
	}
	summary := summaryResp.CashRegister
	if !summary.Enclosed {
		return nil, models.ErrRegisterNotEnclosed
	}

	mode := summary.ClosingMode
	if mode != cashregisters.ClosingModeAuto {
		mode = cashregisters.ClosingModeManual
	}

	// Commandes du registre (orders.cash_register_id), mêmes lectures que le
	// rapport mensuel de leur mode ; la période est ouverte (registerScopeWindow).
	scope := orderScope{registerID: registerID, manual: mode == cashregisters.ClosingModeManual}
	from, toExclusive := registerScopeWindow()
	lines, err := s.repo.GetOrderVATLines(ctx, merchantID, from, toExclusive, scope)
	if err != nil {
		return nil, err
	}
	orderPayments, err := s.repo.GetOrderPayments(ctx, merchantID, from, toExclusive, scope)
	if err != nil {
		return nil, err
	}

	data := registerPDFData{
		Header:          mc.header,
		RegisterNumber:  summary.CashRegisterID,
		OpenedBy:        formatUserName(summary.OpenedBy),
		ClosedBy:        formatUserName(summary.ClosedBy),
		CashFundInitial: int64(summary.CashFund),
		CashFundFinal:   int64(summary.FinalCashFund),
		ClosingMode:     mode,
		TZName:          mc.tzName,
	}

	startLocal := time.Unix(summary.StartDate, 0).In(mc.loc)
	data.From = startLocal
	data.To = time.Now().In(mc.loc)
	if summary.EndDate != nil {
		data.To = time.Unix(int64(*summary.EndDate), 0).In(mc.loc)
	}

	if mode == cashregisters.ClosingModeAuto {
		report := buildAutoReport(lines, orderPayments)
		if len(report.UnallocatedOrders) > 0 {
			// Même traitement que le rapport mensuel : exclues des deux tableaux
			// pour garder l'égalité TVA = encaissements, jamais montrées au comptable.
			logger.FromContext(ctx).Warn("register pdf: paid orders without VAT lines excluded",
				zap.String("merchant_id", merchantID),
				zap.String("cash_register_id", registerID),
				zap.Int64s("order_ids", report.UnallocatedOrders))
		}
		data.TVARows, data.Payments, data.Discounts = report.TVARows, report.Payments, report.Summary
		data.EmptyPaymentsMessage = "Aucun encaissement sur ce registre."
	} else {
		data.TVARows, data.Discounts = buildManualVAT(lines, orderPayments)
		real, err := s.repo.GetRealPaymentsData(ctx, []int64{regID})
		if err != nil {
			return nil, err
		}
		data.Payments = filterExcludedPaymentLabels(real)
		data.EmptyPaymentsMessage = "Aucun encaissement saisi sur ce registre."

		trusted, err := s.repo.IsRegisterTrusted(ctx, merchantID, regID)
		if err != nil {
			return nil, err
		}
		data.Drifted = !trusted
	}

	pdfBytes, err := buildRegisterPDFReport(data)
	if err != nil {
		return nil, err
	}
	return &registerPDF{
		PDF:        pdfBytes,
		Filename:   fmt.Sprintf("WR_registre_%s.pdf", summary.CashRegisterID),
		StartLocal: startLocal,
	}, nil
}

// ExportRegisterPDF génère le PDF d'un seul registre de caisse validé.
func (s *AccountingService) ExportRegisterPDF(ctx context.Context, registerID string) ([]byte, string, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, "", models.ErrUnauthorized
	}
	mc, err := s.loadMerchantPDFContext(ctx, user.MerchantID)
	if err != nil {
		return nil, "", err
	}
	res, err := s.buildRegisterPDF(ctx, mc, user.MerchantID, strings.TrimSpace(registerID))
	if err != nil {
		return nil, "", err
	}
	return res.PDF, res.Filename, nil
}

// registerExportItem est un registre candidat à un export par lot.
type registerExportItem struct {
	id     string
	reason string // non vide : écarté d'office (non validé), sans génération
}

// ignoredRegister est un registre absent du ZIP, avec la raison.
type ignoredRegister struct {
	id     string
	reason string
}

// ExportRegistersPDF génère un ZIP contenant un PDF par registre validé. Les
// registres demandés mais non exportables (inconnus, non validés, en erreur)
// ne font pas échouer le lot : ils sont listés dans ignores.txt du ZIP. Le lot
// est plafonné à RegisterExportMax registres exportables.
func (s *AccountingService) ExportRegistersPDF(ctx context.Context, req RegisterExportRequest) (*RegistersExport, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	mc, err := s.loadMerchantPDFContext(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	items, zipName, err := s.resolveRegisterExport(ctx, mc, user.MerchantID, user.UserID, req)
	if err != nil {
		return nil, err
	}

	type pdfEntry struct {
		name string
		data []byte
	}
	var (
		entries   []pdfEntry
		ignored   []ignoredRegister
		firstFail error
	)
	for _, item := range items {
		if item.reason != "" {
			ignored = append(ignored, ignoredRegister{item.id, item.reason})
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := s.buildRegisterPDF(ctx, mc, user.MerchantID, item.id)
		switch {
		case err == nil:
			entries = append(entries, pdfEntry{
				name: fmt.Sprintf("WR_registre_%s_%s.pdf", item.id, res.StartLocal.Format("2006-01-02")),
				data: res.PDF,
			})
		case errors.Is(err, models.ErrNotFound):
			ignored = append(ignored, ignoredRegister{item.id, "registre introuvable"})
		case errors.Is(err, models.ErrRegisterNotEnclosed):
			ignored = append(ignored, ignoredRegister{item.id, "registre non clôturé ou non validé"})
		default:
			logger.FromContext(ctx).Error("register pdf export failed",
				zap.String("merchant_id", user.MerchantID),
				zap.String("cash_register_id", item.id),
				zap.Error(err))
			ignored = append(ignored, ignoredRegister{item.id, "erreur lors de la génération"})
			if firstFail == nil {
				firstFail = err
			}
		}
	}

	if len(entries) == 0 {
		// Rien de généré à cause d'une erreur technique : c'en est une, pas un
		// refus métier.
		if firstFail != nil {
			return nil, firstFail
		}
		return nil, models.ErrRegisterExportNoneEligible
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	add := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	for _, e := range entries {
		if err := add(e.name, e.data); err != nil {
			return nil, err
		}
	}
	if len(ignored) > 0 {
		var b strings.Builder
		b.WriteString("Registres non exportés :\r\n")
		for _, ig := range ignored {
			fmt.Fprintf(&b, "- registre %s : %s\r\n", ig.id, ig.reason)
		}
		if err := add("ignores.txt", []byte(b.String())); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}

	return &RegistersExport{Data: buf.Bytes(), Filename: zipName, Exported: len(entries), Ignored: len(ignored)}, nil
}

// resolveRegisterExport transforme la demande en liste de registres candidats
// (ordre de la demande, ou chronologique pour une période) et nom du ZIP, en
// appliquant le plafond.
func (s *AccountingService) resolveRegisterExport(ctx context.Context, mc *merchantPDFContext, merchantID, userID string, req RegisterExportRequest) ([]registerExportItem, string, error) {
	hasIDs := len(req.RegisterIDs) > 0
	hasPeriod := strings.TrimSpace(req.DateFrom) != "" || strings.TrimSpace(req.DateTo) != ""
	switch {
	case hasIDs && hasPeriod:
		return nil, "", models.ErrInvalidInput
	case !hasIDs && !hasPeriod:
		return nil, "", models.ErrRegisterExportEmpty
	}

	if hasIDs {
		seen := make(map[string]bool, len(req.RegisterIDs))
		var items []registerExportItem
		for _, raw := range req.RegisterIDs {
			id := strings.TrimSpace(raw)
			if _, err := strconv.ParseInt(id, 10, 64); err != nil {
				return nil, "", models.ErrInvalidInput
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, registerExportItem{id: id})
		}
		if len(items) > RegisterExportMax {
			return nil, "", models.ErrRegisterExportTooMany
		}
		return items, fmt.Sprintf("WR_registres_%d_%s.zip", len(items), time.Now().In(mc.loc).Format("2006-01-02")), nil
	}

	// Période : la requête de l'historique des registres, pour que « toute la
	// période » soit exactement la liste affichée (bornes et visibilité).
	dateFrom, okFrom := normalizeHistoryBound(req.DateFrom, "00:00:00")
	dateTo, okTo := normalizeHistoryBound(req.DateTo, "23:59:59")
	if !okFrom || !okTo || dateFrom > dateTo {
		return nil, "", models.ErrInvalidInput
	}
	page, limit := 1, RegisterExportMax
	history, err := s.cashRegistersRepo.GetCashRegisterHistory(ctx, merchantID, userID, cashregisters.CashRegisterHistoryRequest{
		Page: &page, Limit: &limit, DateFrom: &dateFrom, DateTo: &dateTo,
	})
	if err != nil {
		return nil, "", err
	}
	if history.Metadata.TotalItems > RegisterExportMax {
		return nil, "", models.ErrRegisterExportTooMany
	}
	// L'historique va du plus récent au plus ancien ; le ZIP est chronologique.
	items := make([]registerExportItem, 0, len(history.CashRegisters))
	for i := len(history.CashRegisters) - 1; i >= 0; i-- {
		register := history.CashRegisters[i]
		item := registerExportItem{id: register.CashRegisterID}
		if !register.Enclosed {
			item.reason = "registre non clôturé ou non validé"
		}
		items = append(items, item)
	}
	return items, fmt.Sprintf("WR_registres_%s_%s.zip", dateFrom[:10], dateTo[:10]), nil
}

var historyBoundRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}( \d{2}:\d{2}:\d{2})?$`)

// normalizeHistoryBound valide une borne de période et complète une date seule
// avec l'heure par défaut (début ou fin de journée), comme le fait l'écran de
// l'historique avant d'appeler l'API.
func normalizeHistoryBound(raw, defaultTime string) (string, bool) {
	v := strings.TrimSpace(raw)
	if !historyBoundRe.MatchString(v) {
		return "", false
	}
	if _, err := time.Parse("2006-01-02", v[:10]); err != nil {
		return "", false
	}
	if len(v) == 10 {
		v += " " + defaultTime
	}
	return v, true
}
