//go:build postgres_integration

package cash_registers

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Modes de clôture (migration 165, docs/EXPORT_COMPTABLE_MODES_CLOTURE.md) :
// résolution par l'historique, planification / annulation par l'équipe
// WelloResto, mode inscrit sur le registre à l'ouverture, validation
// automatique à la fermeture en AUTO, relevé de caisse toujours possible en
// MANUAL.
func TestCashRegisterClosingModes_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const userID = "itest-cr-mode-user"
	var merchantID string
	cleanupFor := func(mid string) {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM merchant_closing_modes WHERE merchant_id = $1`,
			`DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`,
			`DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`,
			`DELETE FROM cash_registers WHERE merchant_id = $1`,
			`DELETE FROM cash_desks WHERE merchant_id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE CAST(id AS TEXT) = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-cr-mode' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	} else {
		cleanupFor("")
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest CR Mode', 'a', '1', 's', '75001', 'Paris', 'siret-cr-mode', 'https://x', '06', 'mtok-cr-mode', 'Europe/Paris')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	mustExec := func(label, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	mustExec("seed users", `
		INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
		VALUES ($1, 'ITest CR Mode', 'Caisse', 'Mode', 'x', 'itest-cr-mode@example.com', 'cr-mode-tok')`, userID)
	mustExec("seed merchant_parameters", `
		INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
		VALUES ($1, now(), 'EUR', true)`, merchantID)
	var cashDeskID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Mode Desk')
		RETURNING cash_desk_id`, merchantID).Scan(&cashDeskID); err != nil {
		t.Fatalf("seed cash_desks: %v", err)
	}

	repo := NewCashRegisterRepository(db)
	svc := NewCashRegisterService(repo)
	authedCtx := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userID, MerchantID: merchantID})

	paris, _ := time.LoadLocation("Europe/Paris")
	today := localDay(time.Now(), paris)
	firstOfThisMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, paris)
	firstOfNextMonth := firstOfThisMonth.AddDate(0, 1, 0)
	day := func(t time.Time) string { return t.Format(closingModeDateLayout) }

	// --- 1. Sans historique : AUTO (défaut des nouveaux établissements). ---
	overview, err := svc.GetClosingModes(authedCtx, merchantID)
	if err != nil || overview.CurrentMode != ClosingModeAuto || len(overview.History) != 0 {
		t.Fatalf("GetClosingModes (sans historique) = (%+v, %v), want AUTO sans historique", overview, err)
	}

	// --- 2. Planification par l'équipe WelloResto. ---
	if _, err := svc.ScheduleClosingMode(authedCtx, merchantID, ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: day(firstOfNextMonth)}); !errors.Is(err, ErrClosingModeAlreadyInEffect) {
		t.Fatalf("planifier AUTO alors qu'AUTO est en vigueur : err = %v, want ErrClosingModeAlreadyInEffect", err)
	}
	if _, err := svc.ScheduleClosingMode(authedCtx, merchantID, ScheduleClosingModeRequest{Mode: ClosingModeManual, EffectiveFrom: day(firstOfThisMonth)}); !errors.Is(err, ErrClosingModeRetroactive) {
		t.Fatalf("planifier pour le mois en cours : err = %v, want ErrClosingModeRetroactive", err)
	}
	overview, err = svc.ScheduleClosingMode(authedCtx, merchantID, ScheduleClosingModeRequest{Mode: ClosingModeManual, EffectiveFrom: day(firstOfNextMonth)})
	if err != nil || len(overview.History) != 1 || overview.History[0].Mode != ClosingModeManual || overview.History[0].CreatedBy != userID {
		t.Fatalf("planifier MANUAL au 1er du mois prochain = (%+v, %v)", overview, err)
	}
	if overview.CurrentMode != ClosingModeAuto {
		t.Fatalf("un changement planifié ne s'applique pas avant sa date : current = %s, want AUTO", overview.CurrentMode)
	}
	if _, err := svc.ScheduleClosingMode(authedCtx, merchantID, ScheduleClosingModeRequest{Mode: ClosingModeAuto, EffectiveFrom: day(firstOfNextMonth)}); !errors.Is(err, ErrClosingModeAlreadyPlanned) {
		t.Fatalf("deuxième changement à la même date : err = %v, want ErrClosingModeAlreadyPlanned", err)
	}
	if mode, err := repo.ResolveClosingMode(ctx, merchantID, firstOfNextMonth); err != nil || mode != ClosingModeManual {
		t.Fatalf("ResolveClosingMode(1er du mois prochain) = (%s, %v), want MANUAL", mode, err)
	}
	if mode, err := repo.ResolveClosingMode(ctx, merchantID, firstOfNextMonth.AddDate(0, 0, -1)); err != nil || mode != ClosingModeAuto {
		t.Fatalf("ResolveClosingMode(veille) = (%s, %v), want AUTO", mode, err)
	}

	// --- 3. Annulation : seulement un changement pas encore en vigueur. ---
	if _, err := svc.CancelClosingMode(authedCtx, merchantID, day(firstOfThisMonth)); !errors.Is(err, ErrClosingModeRetroactive) {
		t.Fatalf("annuler un 1er déjà passé : err = %v, want ErrClosingModeRetroactive", err)
	}
	overview, err = svc.CancelClosingMode(authedCtx, merchantID, day(firstOfNextMonth))
	if err != nil || len(overview.History) != 0 {
		t.Fatalf("annuler le changement planifié = (%+v, %v), want historique vide", overview, err)
	}
	if _, err := svc.CancelClosingMode(authedCtx, merchantID, day(firstOfNextMonth)); !errors.Is(err, ErrClosingModeNotFound) {
		t.Fatalf("annuler deux fois : err = %v, want ErrClosingModeNotFound", err)
	}

	openRegister := func(deviceID string) string {
		t.Helper()
		req := &models.OpenCashRegisterRequest{DeviceID: deviceID}
		req.CashRegister.CashDeskID = strconv.FormatInt(cashDeskID, 10)
		req.CashRegister.UserID = userID
		req.CashRegister.CashFund = 1000
		resp, err := repo.OpenCashRegister(ctx, req, merchantID)
		if err != nil || resp.CashRegister == nil {
			t.Fatalf("OpenCashRegister(%s) = (%+v, %v)", deviceID, resp, err)
		}
		return resp.CashRegister.CashRegisterId
	}

	// --- 4. Registre ouvert sans historique : AUTO, validé à la fermeture. ---
	autoReg := openRegister("itest-cr-mode-device-auto")
	if mode, enclosed, err := repo.GetRegisterClosingState(ctx, autoReg); err != nil || mode != ClosingModeAuto || enclosed {
		t.Fatalf("registre AUTO ouvert = (%s, %v, %v), want AUTO non validé", mode, enclosed, err)
	}
	result, err := svc.CloseCashRegister(authedCtx, "", autoReg, &models.CloseCashRegisterRequest{})
	if err != nil || result.AlreadyClosed || result.ClosingMode != ClosingModeAuto || !result.Enclosed {
		t.Fatalf("fermeture AUTO = (%+v, %v), want validé en une fois", result, err)
	}
	var closedBy string
	if err := db.QueryRowContext(ctx, `SELECT closed_by FROM cash_registers WHERE cash_register_id = $1`, autoReg).Scan(&closedBy); err != nil || closedBy != userID {
		t.Fatalf("closed_by du registre AUTO = (%q, %v), want %q", closedBy, err, userID)
	}
	if _, err := repo.AddCustomItem(ctx, autoReg, &models.AddCustomItemRequest{Label: "ES", Value: 100}, &auth.UserLoginRow{UserID: userID, MerchantID: merchantID}); !errors.Is(err, models.ErrCashRegisterStillOpen) {
		t.Fatalf("relevé sur un registre AUTO validé : err = %v, want refus", err)
	}
	summary, err := repo.GetCashRegisterSummary(ctx, autoReg, merchantID)
	if err != nil || summary.CashRegister.ClosingMode != ClosingModeAuto || !summary.CashRegister.Enclosed {
		t.Fatalf("résumé du registre AUTO = (%+v, %v)", summary, err)
	}

	// --- 5. Établissement en MANUAL (comme les existants après la migration
	// 165) : registre MANUAL, fermé sans validation, relevé possible. ---
	mustExec("history MANUAL", `
		INSERT INTO merchant_closing_modes (merchant_id, mode, effective_from, created_by)
		VALUES ($1, 'MANUAL', DATE '1970-01-01', 'itest')`, merchantID)
	manualReg := openRegister("itest-cr-mode-device-manual")
	result, err = svc.CloseCashRegister(authedCtx, "", manualReg, &models.CloseCashRegisterRequest{})
	if err != nil || result.ClosingMode != ClosingModeManual || result.Enclosed {
		t.Fatalf("fermeture MANUAL = (%+v, %v), want fermé non validé", result, err)
	}
	if _, err := repo.AddCustomItem(ctx, manualReg, &models.AddCustomItemRequest{Label: "ES", Value: 100}, &auth.UserLoginRow{UserID: userID, MerchantID: merchantID}); err != nil {
		t.Fatalf("relevé sur un registre MANUAL fermé : %v", err)
	}

	// Le mode reste celui inscrit à l'ouverture, même si l'historique change
	// ensuite (ici : retour à AUTO depuis toujours, simulé en base).
	mustExec("history rewrite", `DELETE FROM merchant_closing_modes WHERE merchant_id = $1`, merchantID)
	if mode, _, err := repo.GetRegisterClosingState(ctx, manualReg); err != nil || mode != ClosingModeManual {
		t.Fatalf("mode du registre après changement d'historique = (%s, %v), want MANUAL (inscrit à l'ouverture)", mode, err)
	}
}
