//go:build postgres_integration

package user_services

import (
	"context"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

func TestGetCurrentService_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	const (
		merchantID = "999903"
		userID     = "itest-usvc-user"
		deviceID   = "itest-usvc-device"
	)

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM services_performed WHERE user_id = $1`, userID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_registers WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM cash_desks WHERE merchant_id = $1`, merchantID)
		_, _ = db.ExecContext(ctx, `DELETE FROM device_link WHERE device_id = $1`, deviceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	}
	cleanup()
	t.Cleanup(cleanup)

	_, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, merchant_id, name, first_name, last_name, password, email, token)
		VALUES ($1, $2, 'ITest User', 'ITest', 'User', 'x', 'itest-usvc@example.com', 'itest-tok')`,
		userID, merchantID)
	if err != nil {
		t.Fatalf("seed users: %v", err)
	}

	var cashDeskID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_desks (merchant_id, name) VALUES ($1, 'ITest Desk') RETURNING cash_desk_id`,
		merchantID).Scan(&cashDeskID)
	if err != nil {
		t.Fatalf("seed cash_desks: %v", err)
	}

	var cashRegisterID int64
	err = db.QueryRowContext(ctx, `
		INSERT INTO cash_registers (merchant_id, cash_desk_id, device_id, user_id, cash_fund, start_date, closure_comment)
		VALUES ($1, $2, $3, $4, 10000, now(), '') RETURNING cash_register_id`,
		merchantID, cashDeskID, deviceID, userID).Scan(&cashRegisterID)
	if err != nil {
		t.Fatalf("seed cash_registers: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO services_performed (user_id, merchant_id, cash_desk_id, start_date)
		VALUES ($1, $2, $3, now())`, userID, merchantID, cashDeskID)
	if err != nil {
		t.Fatalf("seed services_performed: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO device_link (device_id, user_id, on_behalf_of)
		VALUES ($1, $2, 'itest-obo')`, deviceID, userID)
	if err != nil {
		t.Fatalf("seed device_link: %v", err)
	}

	repo := NewServicesRepository(db)
	resp, err := repo.GetCurrentService(ctx, userID, merchantID, deviceID)
	if err != nil {
		t.Fatalf("GetCurrentService failed against postgres: %v", err)
	}

	if resp.Service == nil {
		t.Fatal("expected an open service")
	}
	if resp.Service.StartDate == nil || *resp.Service.StartDate == "" {
		t.Fatal("expected a non-empty start_date")
	}
	if resp.CashRegister == nil {
		t.Fatal("expected a cash register for the device")
	}
	if resp.CashRegister.CashDesk.CashDeskName != "ITest Desk" {
		t.Fatalf("unexpected cash desk: %+v", resp.CashRegister)
	}
	if len(resp.CashDesks) != 1 {
		t.Fatalf("expected 1 cash desk, got %d", len(resp.CashDesks))
	}
	if resp.CashDesks[0].Active != 1 {
		t.Fatalf("expected desk active=1, got %d", resp.CashDesks[0].Active)
	}
	if resp.OnBehalfOf == nil || *resp.OnBehalfOf != "itest-obo" {
		t.Fatalf("unexpected on_behalf_of: %v", resp.OnBehalfOf)
	}
}
