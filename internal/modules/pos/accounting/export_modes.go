package accounting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	cashregisters "welloresto-api/internal/modules/cash_registers"

	"go.uber.org/zap"
)

// closingModeStraddleError signale une période d'export qui chevauche un
// changement de mode de clôture : refusée, chaque mode a son propre rapport.
type closingModeStraddleError struct {
	effectiveFrom time.Time
}

func (e *closingModeStraddleError) Error() string {
	return "closing mode changes on " + e.effectiveFrom.Format("2006-01-02")
}

// periodClosingMode renvoie le mode de clôture de l'établissement sur
// [fromLocal, lastDayLocal] (dates du calendrier de l'établissement) : celui en
// vigueur au premier jour, à condition qu'aucun changement vers un autre mode
// ne prenne effet à l'intérieur de la période (*closingModeStraddleError sinon).
func (s *AccountingService) periodClosingMode(ctx context.Context, merchantID string, fromLocal, lastDayLocal time.Time) (string, error) {
	mode, err := s.cashRegistersRepo.ResolveClosingMode(ctx, merchantID, fromLocal)
	if err != nil {
		return "", err
	}
	history, err := s.cashRegistersRepo.ListClosingModes(ctx, merchantID)
	if err != nil {
		return "", err
	}
	first := fromLocal.Format("2006-01-02")
	last := lastDayLocal.Format("2006-01-02")
	for _, change := range history {
		// Comparaison des dates YYYY-MM-DD en texte : ordre lexicographique =
		// ordre chronologique.
		if change.EffectiveFrom > first && change.EffectiveFrom <= last && change.Mode != mode {
			effectiveFrom, _ := time.ParseInLocation("2006-01-02", change.EffectiveFrom, fromLocal.Location())
			return "", &closingModeStraddleError{effectiveFrom: effectiveFrom}
		}
	}
	return mode, nil
}

// ExportChannel est un canal de commande filtrable dans l'export comptable.
type ExportChannel struct {
	Value string `json:"value"` // valeur de orders.order_source, à renvoyer dans channels
	Label string `json:"label"`
}

// ExportOptionsResponse répond à GET /pos/accounting/export-options : le mode
// de clôture de l'établissement au premier jour de la période, et les canaux
// filtrables (vide en clôture manuelle, où l'export n'est pas configurable).
type ExportOptionsResponse struct {
	Status      string          `json:"status"`
	ClosingMode string          `json:"closing_mode"`
	Channels    []ExportChannel `json:"channels"`
}

// accountingOrderSourceLabels : libellés affichés des canaux (accountingOrderSources).
var accountingOrderSourceLabels = map[string]string{
	"WELLO_RESTO_POS": "Caisse",
	"KIOSK":           "Borne de commande",
	"SCANNORDER":      "ScanNOrder",
	"UBER_EATS":       "Uber Eats",
	"DELIVEROO":       "Deliveroo",
}

// ExportOptions renvoie le mode de clôture de l'établissement à la date
// dateFrom (YYYY-MM-DD, calendrier de l'établissement ; aujourd'hui si vide) et
// les canaux qu'il peut filtrer. L'export revalide de toute façon la période
// entière (refus d'une période à cheval sur un changement de mode).
func (s *AccountingService) ExportOptions(ctx context.Context, dateFrom string) (*ExportOptionsResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, models.ErrUnauthorized
	}
	loc, err := s.cashRegistersRepo.MerchantLocation(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	day := time.Now().In(loc)
	if strings.TrimSpace(dateFrom) != "" {
		parsed, err := parseLocalDate(dateFrom, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: date_from", models.ErrInvalidInput)
		}
		day = parsed
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)

	mode, err := s.cashRegistersRepo.ResolveClosingMode(ctx, user.MerchantID, day)
	if err != nil {
		return nil, err
	}
	channels := []ExportChannel{}
	if mode == cashregisters.ClosingModeAuto {
		for _, source := range accountingOrderSources {
			channels = append(channels, ExportChannel{Value: source, Label: accountingOrderSourceLabels[source]})
		}
	}
	return &ExportOptionsResponse{Status: "1", ClosingMode: mode, Channels: channels}, nil
}

// normalizeOrderSources valide les canaux demandés (valeurs de
// orders.order_source, cf. accountingOrderSources). Vide, ou tous les canaux
// cochés, renvoie nil : aucun filtre, y compris pour les rares commandes sans
// canal connu.
func normalizeOrderSources(channels []string) ([]string, error) {
	if len(channels) == 0 {
		return nil, nil
	}
	known := map[string]bool{}
	for _, s := range accountingOrderSources {
		known[s] = true
	}
	seen := map[string]bool{}
	for _, c := range channels {
		c = strings.ToUpper(strings.TrimSpace(c))
		if !known[c] {
			return nil, fmt.Errorf("Canal de commande inconnu : %q (attendus : %s).", c, strings.Join(accountingOrderSources, ", "))
		}
		seen[c] = true
	}
	if len(seen) == len(accountingOrderSources) {
		return nil, nil
	}
	out := make([]string, 0, len(seen))
	for _, s := range accountingOrderSources {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out, nil
}

// buildManualSections calcule le rapport du mode de clôture manuel : TVA sur
// les lignes (périmètre historique), remises de caisse déduites ; encaissements
// = réel des registres validés et de confiance (cf. GetTrustedEnclosedRegisterIDs),
// remises et canaux hors périmètre exclus.
func (s *AccountingService) buildManualSections(ctx context.Context, merchantID string, fromLocal, toExclusive time.Time) ([]TVARow, []PaymentRow, DiscountSummary, error) {
	lines, err := s.repo.GetOrderVATLines(ctx, merchantID, fromLocal, toExclusive, manualOrderScope())
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	orderPayments, err := s.repo.GetOrderPayments(ctx, merchantID, fromLocal, toExclusive, manualOrderScope())
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	tvaRows, discounts := buildManualVAT(lines, orderPayments)

	// Section Encaissements : réel des registres de caisse enclosed
	// uniquement (cf. docs/decisions.md) — pas de repli sur le théorique
	// (payments) dans ce mode. Un merchant sans registre correctement
	// clôturé sur la période affiche une table vide, voir buildPDFReport.
	trustedRegisterIDs, err := s.repo.GetTrustedEnclosedRegisterIDs(ctx, merchantID, fromLocal, toExclusive)
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	payments, err := s.repo.GetRealPaymentsData(ctx, trustedRegisterIDs)
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	return tvaRows, filterExcludedPaymentLabels(payments), discounts, nil
}

// buildAutoSections calcule le rapport du mode de clôture automatique : les
// paiements de chaque commande du périmètre (canaux choisis) sont ventilés
// entre ses taux ; total TTC = total des encaissements par construction.
func (s *AccountingService) buildAutoSections(ctx context.Context, merchantID string, fromLocal, toExclusive time.Time, scope orderScope) ([]TVARow, []PaymentRow, DiscountSummary, error) {
	lines, err := s.repo.GetOrderVATLines(ctx, merchantID, fromLocal, toExclusive, scope)
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	orderPayments, err := s.repo.GetOrderPayments(ctx, merchantID, fromLocal, toExclusive, scope)
	if err != nil {
		return nil, nil, DiscountSummary{}, err
	}
	report := buildAutoReport(lines, orderPayments)
	if len(report.UnallocatedOrders) > 0 {
		// Commandes payées sans aucune ligne sur laquelle répartir : exclues
		// des deux tableaux pour garder l'égalité TVA = encaissements. Anomalie
		// de données à examiner, jamais montrée au comptable.
		logger.FromContext(ctx).Warn("accounting export: paid orders without VAT lines excluded",
			zap.String("merchant_id", merchantID),
			zap.Int64s("order_ids", report.UnallocatedOrders))
	}
	return report.TVARows, report.Payments, report.Summary, nil
}
