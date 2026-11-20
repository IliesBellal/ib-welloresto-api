package customer_accounts

import (
	"context"
	"errors"
	"testing"

	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

func TestBuildOperation(t *testing.T) {
	req := func(amount int64, mop, comment string) models.CustomerAccountOperationRequest {
		return models.CustomerAccountOperationRequest{Amount: amount, MOP: mop, Comment: comment}
	}

	dep, err := buildOperation("m1", "u1", 7, req(20000, " cb ", ""), models.AccountOperationDeposit)
	if err != nil || dep.Amount != 20000 || dep.MOP == nil || *dep.MOP != "CB" || dep.Comment != nil {
		t.Fatalf("deposit = %+v, %v", dep, err)
	}

	ref, err := buildOperation("m1", "u1", 7, req(5000, "ES", "Solde rendu"), models.AccountOperationRefund)
	if err != nil || ref.Amount != -5000 {
		t.Fatalf("refund = %+v, %v ; want amount -5000", ref, err)
	}

	wo, err := buildOperation("m1", "u1", 7, req(3000, "ES", "Client parti"), models.AccountOperationWriteOff)
	if err != nil || wo.MOP != nil || wo.Amount != 3000 {
		t.Fatalf("write-off = %+v, %v ; want no mop", wo, err)
	}

	if _, err := buildOperation("m1", "u1", 7, req(-1, "ES", ""), models.AccountOperationDeposit); !errors.Is(err, models.ErrAccountInvalidAmount) {
		t.Fatalf("negative amount = %v", err)
	}
	if _, err := buildOperation("m1", "u1", 7, req(maxOperationAmount+1, "CB", ""), models.AccountOperationDeposit); !errors.Is(err, models.ErrAccountInvalidAmount) {
		t.Fatalf("huge amount = %v", err)
	}
	if _, err := buildOperation("m1", "u1", 7, req(5000, "ES", ""), models.AccountOperationRefund); !errors.Is(err, models.ErrAccountReasonRequired) {
		t.Fatalf("refund without reason = %v", err)
	}
}

func TestCheckBalance(t *testing.T) {
	cases := []struct {
		name    string
		opType  string
		amount  int64
		balance int64
		want    error
	}{
		{"versement sur ardoise", models.AccountOperationDeposit, 10000, -40000, nil},
		{"remboursement dans le solde", models.AccountOperationRefund, -3000, 3000, nil},
		{"remboursement au-delà du solde", models.AccountOperationRefund, -3001, 3000, models.ErrAccountRefundExceedsBalance},
		{"remboursement d'une ardoise", models.AccountOperationRefund, -100, -500, models.ErrAccountRefundExceedsBalance},
		{"abandon de toute la dette", models.AccountOperationWriteOff, 500, -500, nil},
		{"abandon au-delà de la dette", models.AccountOperationWriteOff, 501, -500, models.ErrAccountWriteOffExceedsDebt},
		{"abandon sans dette", models.AccountOperationWriteOff, 100, 0, models.ErrAccountWriteOffExceedsDebt},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := checkBalance(c.opType, c.amount, c.balance); !errors.Is(err, c.want) {
				t.Fatalf("checkBalance() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestWithBalances(t *testing.T) {
	entries := withBalances([]models.CustomerAccountEntry{
		{Kind: "payment", Date: 300, Delta: -2350},
		{Kind: "operation", Date: 100, Delta: 100000},
		{Kind: "payment", Date: 200, Delta: -1500},
	})
	// Du plus récent au plus ancien, solde après chaque ligne.
	wantDates := []int64{300, 200, 100}
	wantBalances := []int64{96150, 98500, 100000}
	for i, e := range entries {
		if e.Date != wantDates[i] || e.BalanceAfter != wantBalances[i] {
			t.Fatalf("entry %d = date %d balance %d ; want %d / %d", i, e.Date, e.BalanceAfter, wantDates[i], wantBalances[i])
		}
	}
}

// L'abandon de créance est réservé aux administrateurs : refusé avant tout
// accès à la base pour un utilisateur sans rôle administrateur, même s'il a
// le droit de rembourser.
func TestWriteOff_AdminOnly(t *testing.T) {
	nonAdmin := &auth.UserLoginRow{UserID: "u1", MerchantID: "m1"}
	ctx := middleware.WithUser(context.Background(), nonAdmin)
	req := models.CustomerAccountOperationRequest{Amount: 1000, Comment: "Client parti"}
	if _, err := (&Service{}).WriteOff(ctx, 7, req); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("non-admin write-off = %v, want ErrForbidden", err)
	}
	if _, err := (&Service{}).WriteOff(context.Background(), 7, req); !errors.Is(err, models.ErrUnauthorized) {
		t.Fatalf("anonymous write-off = %v, want ErrUnauthorized", err)
	}
}
