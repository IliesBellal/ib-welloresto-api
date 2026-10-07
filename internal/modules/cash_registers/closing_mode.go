package cash_registers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
)

// Modes de clôture d'un registre (migration 165,
// docs/EXPORT_COMPTABLE_MODES_CLOTURE.md).
//   - MANUAL : fermeture, relevé de caisse et de TPE (custom items), validation.
//     L'export comptable lit le réel des registres validés.
//   - AUTO : fermeture sans relevé, validation automatique. L'export comptable
//     ventile la TVA à partir des paiements.
const (
	ClosingModeManual = "MANUAL"
	ClosingModeAuto   = "AUTO"

	// defaultClosingMode s'applique à un établissement sans historique : les
	// nouveaux établissements sont en clôture automatique (les existants ont
	// reçu une ligne MANUAL par la migration 165).
	defaultClosingMode = ClosingModeAuto

	closingModeDateLayout = "2006-01-02"
)

// Erreurs de planification d'un mode de clôture, renvoyées telles quelles aux
// endpoints internes /admin (l'équipe WelloResto doit savoir pourquoi c'est
// refusé).
var (
	ErrClosingModeInvalidMode     = errors.New("closing_mode_invalid_mode")
	ErrClosingModeInvalidDate     = errors.New("closing_mode_invalid_date")
	ErrClosingModeNotMonthStart   = errors.New("closing_mode_not_month_start")
	ErrClosingModeRetroactive     = errors.New("closing_mode_retroactive")
	ErrClosingModeAlreadyInEffect = errors.New("closing_mode_already_in_effect")
	ErrClosingModeAlreadyPlanned  = errors.New("closing_mode_already_planned")
	ErrClosingModeNotFound        = errors.New("closing_mode_not_found")
	ErrClosingModeMerchantUnknown = errors.New("closing_mode_merchant_unknown")
)

// ClosingModeChange est une ligne de l'historique merchant_closing_modes.
type ClosingModeChange struct {
	Mode          string    `json:"mode"`
	EffectiveFrom string    `json:"effective_from"` // YYYY-MM-DD, calendrier de l'établissement
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

// ClosingModesOverview répond aux endpoints internes : le mode en vigueur
// aujourd'hui (calendrier de l'établissement) et tout l'historique, changements
// planifiés compris.
type ClosingModesOverview struct {
	MerchantID  string              `json:"merchant_id"`
	Timezone    string              `json:"timezone"`
	Today       string              `json:"today"`
	CurrentMode string              `json:"current_mode"`
	History     []ClosingModeChange `json:"history"`
}

// ScheduleClosingModeRequest est le corps de POST
// /admin/merchants/{id}/cash-register-closing-modes.
type ScheduleClosingModeRequest struct {
	Mode          string `json:"mode"`
	EffectiveFrom string `json:"effective_from"` // YYYY-MM-DD, 1er du mois
}

// IsValidClosingMode indique si mode est une valeur connue.
func IsValidClosingMode(mode string) bool {
	return mode == ClosingModeManual || mode == ClosingModeAuto
}

// MerchantLocation renvoie le fuseau de l'établissement (merchant.timezone),
// Europe/Paris s'il est vide ou invalide — même repli que l'export comptable.
// ErrClosingModeMerchantUnknown si l'établissement n'existe pas.
func (r *CashRegisterRepository) MerchantLocation(ctx context.Context, merchantID string) (*time.Location, error) {
	db := dbx.GetDB(ctx, r.database)

	var tz sql.NullString
	err := db.QueryRowContext(ctx, `SELECT timezone FROM merchant WHERE id = ?`, merchantID).Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrClosingModeMerchantUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("load merchant timezone: %w", err)
	}
	name := strings.TrimSpace(tz.String)
	if name == "" {
		name = "Europe/Paris"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc, _ = time.LoadLocation("Europe/Paris")
		if loc == nil {
			loc = time.UTC
		}
	}
	return loc, nil
}

// ResolveClosingMode renvoie le mode de clôture en vigueur pour l'établissement
// à la date day (date du calendrier de l'établissement, seule la partie
// année-mois-jour compte) : la ligne d'historique de date d'effet la plus
// récente <= day, à défaut defaultClosingMode.
func (r *CashRegisterRepository) ResolveClosingMode(ctx context.Context, merchantID string, day time.Time) (string, error) {
	db := dbx.GetDB(ctx, r.database)

	var mode string
	err := db.QueryRowContext(ctx, `
		SELECT mode
		FROM merchant_closing_modes
		WHERE merchant_id = ?
		  AND effective_from <= CAST(? AS date)
		ORDER BY effective_from DESC
		LIMIT 1
	`, merchantID, day.Format(closingModeDateLayout)).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultClosingMode, nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve closing mode: %w", err)
	}
	return mode, nil
}

// ListClosingModes renvoie l'historique complet de l'établissement, du plus
// ancien au plus récent.
func (r *CashRegisterRepository) ListClosingModes(ctx context.Context, merchantID string) ([]ClosingModeChange, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		SELECT mode, effective_from, created_by, created_at
		FROM merchant_closing_modes
		WHERE merchant_id = ?
		ORDER BY effective_from
	`, merchantID)
	if err != nil {
		return nil, fmt.Errorf("list closing modes: %w", err)
	}
	defer rows.Close()

	out := []ClosingModeChange{}
	for rows.Next() {
		var c ClosingModeChange
		var effectiveFrom time.Time
		if err := rows.Scan(&c.Mode, &effectiveFrom, &c.CreatedBy, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan closing mode: %w", err)
		}
		c.EffectiveFrom = effectiveFrom.Format(closingModeDateLayout)
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertClosingMode enregistre un changement planifié. ErrClosingModeAlreadyPlanned
// si un changement existe déjà à cette date.
func (r *CashRegisterRepository) InsertClosingMode(ctx context.Context, merchantID, mode string, effectiveFrom time.Time, createdBy string) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		INSERT INTO merchant_closing_modes (merchant_id, mode, effective_from, created_by)
		VALUES (?, ?, CAST(? AS date), ?)
		ON CONFLICT (merchant_id, effective_from) DO NOTHING
	`, merchantID, mode, effectiveFrom.Format(closingModeDateLayout), createdBy)
	if err != nil {
		return fmt.Errorf("insert closing mode: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrClosingModeAlreadyPlanned
	}
	return nil
}

// DeleteClosingMode supprime un changement à une date donnée. L'appelant
// vérifie qu'il n'est pas encore entré en vigueur. ErrClosingModeNotFound s'il
// n'existe pas.
func (r *CashRegisterRepository) DeleteClosingMode(ctx context.Context, merchantID string, effectiveFrom time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	res, err := db.ExecContext(ctx, `
		DELETE FROM merchant_closing_modes
		WHERE merchant_id = ? AND effective_from = CAST(? AS date)
	`, merchantID, effectiveFrom.Format(closingModeDateLayout))
	if err != nil {
		return fmt.Errorf("delete closing mode: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrClosingModeNotFound
	}
	return nil
}

// CashRegisterBelongsToMerchant indique si le registre appartient à
// l'établissement : le registre lui-même (cash_registers.merchant_id, inscrit à
// l'ouverture depuis la session) ou sa caisse (cash_desks.merchant_id, critère
// de l'historique des registres) lui est rattaché. Un identifiant non numérique
// n'appartient à personne.
func (r *CashRegisterRepository) CashRegisterBelongsToMerchant(ctx context.Context, cashRegisterID, merchantID string) (bool, error) {
	if cashRegisterID == "" || merchantID == "" {
		return false, nil
	}
	for _, c := range cashRegisterID {
		if c < '0' || c > '9' {
			return false, nil
		}
	}

	db := dbx.GetDB(ctx, r.database)
	var one int
	err := db.QueryRowContext(ctx, `
		SELECT 1
		FROM cash_registers cr
		LEFT JOIN cash_desks cd ON cd.cash_desk_id = cr.cash_desk_id
		WHERE cr.cash_register_id = ?
		  AND (cr.merchant_id = ? OR cd.merchant_id = ?)
		LIMIT 1
	`, cashRegisterID, merchantID, merchantID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check cash register ownership: %w", err)
	}
	return true, nil
}

// GetRegisterClosingState renvoie le mode inscrit sur le registre (MANUAL pour
// un registre antérieur à la migration 165, colonne NULL) et s'il est validé.
func (r *CashRegisterRepository) GetRegisterClosingState(ctx context.Context, cashRegisterID string) (mode string, enclosed bool, err error) {
	db := dbx.GetDB(ctx, r.database)

	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(closing_mode, ?), enclosed
		FROM cash_registers
		WHERE cash_register_id = ?
	`, ClosingModeManual, cashRegisterID).Scan(&mode, &enclosed)
	if err != nil {
		return "", false, fmt.Errorf("get register closing state: %w", err)
	}
	return mode, enclosed, nil
}

// localDay ramène un instant à minuit du même jour dans loc : seule la date du
// calendrier de l'établissement compte pour les modes de clôture.
func localDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// validateClosingModeSchedule applique les règles de planification : mode
// connu, date au format YYYY-MM-DD, premier jour d'un mois, et au plus tôt le
// 1er du mois suivant « aujourd'hui » dans le calendrier de l'établissement
// (pas de changement rétroactif ni pour le mois en cours : des registres ont pu
// être ouverts). Renvoie la date d'effet parsée.
func validateClosingModeSchedule(req ScheduleClosingModeRequest, now time.Time, loc *time.Location) (time.Time, error) {
	if !IsValidClosingMode(req.Mode) {
		return time.Time{}, ErrClosingModeInvalidMode
	}
	effectiveFrom, err := time.ParseInLocation(closingModeDateLayout, strings.TrimSpace(req.EffectiveFrom), loc)
	if err != nil {
		return time.Time{}, ErrClosingModeInvalidDate
	}
	if effectiveFrom.Day() != 1 {
		return time.Time{}, ErrClosingModeNotMonthStart
	}
	today := localDay(now, loc)
	firstOfNextMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
	if effectiveFrom.Before(firstOfNextMonth) {
		return time.Time{}, ErrClosingModeRetroactive
	}
	return effectiveFrom, nil
}
