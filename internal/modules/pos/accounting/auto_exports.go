package accounting

import (
	"context"
	"fmt"
	"time"

	"welloresto-api/internal/database/dbx"
	cashregisters "welloresto-api/internal/modules/cash_registers"
)

// Génération automatique de l'export comptable à la clôture fiscale du mois
// (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md, « Génération automatique »). Dès
// qu'un établissement a sa clôture fiscale MENSUELLE (fiscal_closures,
// period_type = 'MONTH'), le rapport du mois entier — du premier au dernier jour,
// tous canaux — est produit comme un export demandé au back-office, avec
// generated_by = SYSTEM. Il apparaît dans la liste du popup « Export comptable ».

// AutoExportGeneratedBy est l'auteur d'un export produit automatiquement.
const AutoExportGeneratedBy = "SYSTEM"

// autoExportRegisterGraceDays : en clôture manuelle, le rapport lit le réel
// des registres VALIDÉS. On attend la validation des registres du mois
// (l'export est figé une fois écrit) pendant ce nombre de jours après la fin du
// mois ; passé ce délai, il est produit malgré un registre resté ouvert.
const autoExportRegisterGraceDays = 3

// ExportStore est le stockage privé où l'export est archivé (client R2 privé).
type ExportStore interface {
	accountingExportUploader
}

// MonthRef désigne un mois clos d'un établissement, sans export tous canaux.
type MonthRef struct {
	MerchantID string
	From, To   string // YYYY-MM-DD, jours locaux, bornes incluses
}

// MonthlyExportResult résume un passage de GenerateDueMonthlyExports.
type MonthlyExportResult struct {
	Generated int // exports écrits
	Failed    int // mois refusés ou en erreur, retentés au passage suivant
	Waiting   int // mois en attente de la validation des registres (clôture manuelle)
	Remaining int // mois non tentés, échéance atteinte
}

// ListMonthsWithoutExport renvoie les mois dont la clôture fiscale mensuelle est
// écrite et qui n'ont pas d'export couvrant le mois entier sans filtre de canal
// (un export manuel du mois entier, tous canaux, compte : rien à refaire), les
// plus anciens d'abord, au plus limit.
func (r *AccountingRepository) ListMonthsWithoutExport(ctx context.Context, limit int) ([]MonthRef, error) {
	rows, err := dbx.GetDB(ctx, r.database).QueryContext(ctx, `
		SELECT c.merchant_id, to_char(c.period_start, 'YYYY-MM-DD'), to_char(c.period_end, 'YYYY-MM-DD')
		FROM fiscal_closures c
		WHERE c.period_type = 'MONTH'
		  AND NOT EXISTS (SELECT 1 FROM accounting_exports e
		                  WHERE e.merchant_id = c.merchant_id
		                    AND e.period_from = c.period_start AND e.period_to = c.period_end
		                    AND e.channels = '')
		ORDER BY c.period_start, c.merchant_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list months without accounting export: %w", err)
	}
	defer rows.Close()
	var out []MonthRef
	for rows.Next() {
		var m MonthRef
		if err := rows.Scan(&m.MerchantID, &m.From, &m.To); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// HasUnenclosedRegister indique si un registre ouvert dans [from, toExclusive[
// n'est pas encore validé (enclosed).
func (r *AccountingRepository) HasUnenclosedRegister(ctx context.Context, merchantID string, from, toExclusive time.Time) (bool, error) {
	var exists bool
	err := dbx.GetDB(ctx, r.database).QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM cash_registers
		               WHERE merchant_id = ? AND enclosed IS NOT TRUE
		                 AND start_date >= ? AND start_date < ?)`,
		merchantID, acctUTCParam(from), acctUTCParam(toExclusive)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check unenclosed registers: %w", err)
	}
	return exists, nil
}

// waitingForRegisters : en clôture manuelle, vrai tant qu'un registre du mois
// n'est pas validé et que le délai de grâce n'est pas écoulé.
func (s *AccountingService) waitingForRegisters(ctx context.Context, m MonthRef) (bool, error) {
	loc, err := s.cashRegistersRepo.MerchantLocation(ctx, m.MerchantID)
	if err != nil {
		return false, err
	}
	from, err := parseLocalDate(m.From, loc)
	if err != nil {
		return false, err
	}
	last, err := parseLocalDate(m.To, loc)
	if err != nil {
		return false, err
	}
	toExclusive := last.AddDate(0, 0, 1)
	if time.Now().After(toExclusive.AddDate(0, 0, autoExportRegisterGraceDays)) {
		return false, nil
	}
	mode, err := s.cashRegistersRepo.ResolveClosingMode(ctx, m.MerchantID, from)
	if err != nil {
		return false, err
	}
	if mode != cashregisters.ClosingModeManual {
		return false, nil
	}
	return s.repo.HasUnenclosedRegister(ctx, m.MerchantID, from, toExclusive)
}

// GenerateDueMonthlyExports produit, un à un, les exports comptables des mois
// clos qui n'en ont pas (au plus limit), tant que l'échéance n'est pas atteinte.
// Un mois refusé (commande restée ouverte, changement de mode de clôture dans le
// mois…) ou en erreur est signalé à onFailure et retenté au passage suivant ;
// il ne bloque pas les autres. L'export reste immuable : un mois déjà couvert
// n'est jamais régénéré.
func (s *AccountingService) GenerateDueMonthlyExports(ctx context.Context, store ExportStore, limit int, deadline time.Time,
	onFailure func(m MonthRef, reason string)) (MonthlyExportResult, error) {
	var res MonthlyExportResult
	months, err := s.repo.ListMonthsWithoutExport(ctx, limit)
	if err != nil {
		return res, err
	}
	fail := func(m MonthRef, reason string) {
		res.Failed++
		if onFailure != nil {
			onFailure(m, reason)
		}
	}
	for i, m := range months {
		if !time.Now().Before(deadline) {
			res.Remaining = len(months) - i
			break
		}
		waiting, err := s.waitingForRegisters(ctx, m)
		if err != nil {
			fail(m, err.Error())
			continue
		}
		if waiting {
			res.Waiting++
			continue
		}
		resp, _, err := s.generateAccountingExport(ctx, m.MerchantID, AutoExportGeneratedBy, m.From, m.To, nil, store)
		if err != nil {
			fail(m, err.Error())
			continue
		}
		if resp.Status != "1" {
			fail(m, resp.Error)
			continue
		}
		res.Generated++
	}
	return res, nil
}
