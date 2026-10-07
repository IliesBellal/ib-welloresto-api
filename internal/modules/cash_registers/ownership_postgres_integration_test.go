//go:build postgres_integration

package cash_registers

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

// Appartenance des registres (2026-10-04) : un établissement ne peut ni
// fermer, ni valider, ni consulter, ni modifier le relevé du registre d'un
// autre établissement — et la fermeture d'un registre étranger ou inexistant
// est un no-op répondu « déjà fermé, validé », pour que l'app poursuive
// normalement. Le parcours de clôture de l'établissement propriétaire est
// inchangé.
func TestCashRegisterOwnership_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantA = "999931"
		merchantB = "999932"
		userA     = "itest-cr-own-user-a"
		userB     = "itest-cr-own-user-b"
		deviceA   = "itest-cr-own-device-a"
	)
	cleanup := func() {
		for _, mid := range []string{merchantA, merchantB} {
			_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_custom_items WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers_items WHERE cash_register_id IN (SELECT cash_register_id FROM cash_registers WHERE merchant_id = $1)`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM cash_desks WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_closing_modes WHERE merchant_id = $1`, mid)
			_, _ = db.ExecContext(ctx, `DELETE FROM merchant_parameters WHERE merchant_id = $1`, mid)
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id IN ($1, $2)`, userA, userB)
	}
	cleanup()
	t.Cleanup(cleanup)

	mustExec := func(label, q string, args ...interface{}) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	for _, u := range []struct{ id, mid string }{{userA, merchantA}, {userB, merchantB}} {
		mustExec("seed user", `
			INSERT INTO users (user_id, name, first_name, last_name, password, email, token)
			VALUES ($1, $1, 'Caisse', 'Own', 'x', $2, $1)`, u.id, u.id+"@example.com")
		mustExec("seed merchant_parameters", `
			INSERT INTO merchant_parameters (merchant_id, last_menu_update, currency, is_open)
			VALUES ($1, now(), 'EUR', true)`, u.mid)
	}
	// A est en clôture manuelle (comme tous les établissements existants).
	mustExec("seed closing mode A", `
		INSERT INTO merchant_closing_modes (merchant_id, mode, effective_from, created_by)
		VALUES ($1, 'MANUAL', DATE '1970-01-01', 'itest')`, merchantA)
	var cashDeskA int64
	if err := db.QueryRowContext(ctx, `INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Own Desk A') RETURNING cash_desk_id`, merchantA).Scan(&cashDeskA); err != nil {
		t.Fatalf("seed cash_desks: %v", err)
	}

	repo := NewCashRegisterRepository(db)
	svc := NewCashRegisterService(repo)
	ctxA := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userA, MerchantID: merchantA})
	ctxB := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: userB, MerchantID: merchantB})

	openReq := &models.OpenCashRegisterRequest{DeviceID: deviceA}
	openReq.CashRegister.CashDeskID = strconv.FormatInt(cashDeskA, 10)
	openReq.CashRegister.UserID = userA
	openReq.CashRegister.CashFund = 1000
	openResp, err := repo.OpenCashRegister(ctx, openReq, merchantA)
	if err != nil || openResp.CashRegister == nil {
		t.Fatalf("OpenCashRegister A = (%+v, %v)", openResp, err)
	}
	regA := openResp.CashRegister.CashRegisterId

	// Paiement ScanNOrder de B en attente de rattachement : une fermeture du
	// registre de A demandée par B ne doit pas le rattacher au registre de A.
	var orderB int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO orders (merchant_id, order_num, brand_status, state, price, TVA, HT, created_by)
		VALUES ($1, 1, 'ACCEPTED', 'CLOSED', 500, 0, 0, $2) RETURNING order_id`, merchantB, userB).Scan(&orderB); err != nil {
		t.Fatalf("seed order B: %v", err)
	}
	mustExec("seed payment B", `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, cash_register_id, enabled)
		VALUES ($1, $2, $3, 500, 'STRIPE', 'SCANNORDER', true)`, merchantB, userB, orderB)

	// --- B sur le registre de A ---
	result, err := svc.CloseCashRegister(ctxB, "", regA, &models.CloseCashRegisterRequest{})
	if err != nil || !result.AlreadyClosed || !result.Enclosed {
		t.Fatalf("fermeture par B du registre de A = (%+v, %v), want no-op « déjà fermé, validé »", result, err)
	}
	var closedA bool
	if err := db.QueryRowContext(ctx, `SELECT closed FROM cash_registers WHERE cash_register_id = $1`, regA).Scan(&closedA); err != nil || closedA {
		t.Fatalf("registre de A après la tentative de B : closed=%v (%v), want toujours ouvert", closedA, err)
	}
	var paymentRegister string
	if err := db.QueryRowContext(ctx, `SELECT cash_register_id FROM payments WHERE order_id = $1`, orderB).Scan(&paymentRegister); err != nil || paymentRegister != "SCANNORDER" {
		t.Fatalf("paiement de B après la tentative : cash_register_id=%q (%v), want toujours SCANNORDER", paymentRegister, err)
	}

	// --- A ferme son registre : parcours manuel inchangé ---
	result, err = svc.CloseCashRegister(ctxA, "", regA, &models.CloseCashRegisterRequest{})
	if err != nil || result.AlreadyClosed || result.ClosingMode != ClosingModeManual || result.Enclosed {
		t.Fatalf("fermeture par A = (%+v, %v), want fermé, MANUAL, non validé", result, err)
	}
	if _, err := svc.GetCashRegisterSummary(ctxA, "", regA); err != nil {
		t.Fatalf("résumé par A : %v", err)
	}
	added, err := svc.AddCustomItem(ctxA, "", regA, &models.AddCustomItemRequest{Label: "ES", Value: 1000})
	if err != nil {
		t.Fatalf("relevé par A : %v", err)
	}
	itemID := added.(models.HandlerDefaultResponseModelSet).Data1

	// --- B ne peut ni consulter, ni modifier, ni valider ---
	if _, err := svc.GetCashRegisterSummary(ctxB, "", regA); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("résumé par B : err = %v, want ErrNotFound", err)
	}
	if _, err := svc.AddCustomItem(ctxB, "", regA, &models.AddCustomItemRequest{Label: "ES", Value: 99999}); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("relevé par B : err = %v, want ErrNotFound", err)
	}
	if _, err := svc.DeleteCustomItem(ctxB, "", regA, itemID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("suppression de relevé par B : err = %v, want ErrNotFound", err)
	}
	if _, err := svc.EncloseCashRegister(ctxB, regA, "", ""); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("validation par B : err = %v, want ErrNotFound", err)
	}
	var customCount int
	var enclosedA bool
	if err := db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM cash_registers_custom_items WHERE cash_register_id = $1 AND enabled), enclosed
		FROM cash_registers WHERE cash_register_id = $1`, regA).Scan(&customCount, &enclosedA); err != nil {
		t.Fatalf("relecture du registre de A : %v", err)
	}
	if customCount != 1 || enclosedA {
		t.Fatalf("registre de A après les tentatives de B : %d ligne(s) de relevé, enclosed=%v — want 1 ligne, non validé", customCount, enclosedA)
	}

	// --- A valide : inchangé ---
	if _, err := svc.EncloseCashRegister(ctxA, regA, "", "RAS"); err != nil {
		t.Fatalf("validation par A : %v", err)
	}

	// --- Registre inexistant ou identifiant invalide ---
	if result, err := svc.CloseCashRegister(ctxA, "", "999999999", &models.CloseCashRegisterRequest{}); err != nil || !result.AlreadyClosed {
		t.Fatalf("fermeture d'un registre inexistant = (%+v, %v), want no-op", result, err)
	}
	if _, err := svc.GetCashRegisterSummary(ctxA, "", "abc"); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("résumé d'un identifiant invalide : err = %v, want ErrNotFound", err)
	}
}
