package customeraccount

import (
	"errors"
	"testing"

	"welloresto-api/internal/models"
)

func ptr(s string) *string { return &s }

func TestValidateOperation(t *testing.T) {
	reason := ptr("Erreur de saisie")
	cases := []struct {
		name string
		op   Operation
		want error
	}{
		{"versement espèces", Operation{Type: models.AccountOperationDeposit, Amount: 5000, MOP: ptr("ES")}, nil},
		{"versement carte de 5 000 €", Operation{Type: models.AccountOperationDeposit, Amount: 500000, MOP: ptr("CB")}, nil},
		{"versement espèces de 1 000 € pile", Operation{Type: models.AccountOperationDeposit, Amount: 100000, MOP: ptr("ES")}, nil},
		{"versement sans moyen", Operation{Type: models.AccountOperationDeposit, Amount: 5000}, models.ErrAccountInvalidMOP},
		{"versement en titre-restaurant", Operation{Type: models.AccountOperationDeposit, Amount: 5000, MOP: ptr("TR")}, models.ErrAccountInvalidMOP},
		{"versement nul", Operation{Type: models.AccountOperationDeposit, Amount: 0, MOP: ptr("ES")}, models.ErrAccountInvalidAmount},
		{"remboursement avec motif", Operation{Type: models.AccountOperationRefund, Amount: -2000, MOP: ptr("ES"), Comment: reason}, nil},
		{"remboursement positif", Operation{Type: models.AccountOperationRefund, Amount: 2000, MOP: ptr("ES"), Comment: reason}, models.ErrAccountInvalidAmount},
		{"remboursement sans motif", Operation{Type: models.AccountOperationRefund, Amount: -2000, MOP: ptr("ES"), Comment: ptr("  ")}, models.ErrAccountReasonRequired},
		{"abandon avec motif", Operation{Type: models.AccountOperationWriteOff, Amount: 3000, Comment: reason}, nil},
		{"abandon avec un moyen", Operation{Type: models.AccountOperationWriteOff, Amount: 3000, MOP: ptr("ES"), Comment: reason}, models.ErrAccountInvalidMOP},
		{"abandon sans motif", Operation{Type: models.AccountOperationWriteOff, Amount: 3000}, models.ErrAccountReasonRequired},
		{"type inconnu", Operation{Type: "GIFT", Amount: 3000}, models.ErrInvalidInput},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateOperation(c.op)
			if c.want == nil && err != nil {
				t.Fatalf("ValidateOperation() = %v, want nil", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("ValidateOperation() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	cases := []struct{ balance, limit, want int64 }{
		{0, 0, 0},       // ni avance ni ardoise
		{5000, 0, 5000}, // avance seule
		{0, 20000, 20000},
		{-15000, 20000, 5000}, // ardoise entamée
		{-25000, 20000, 0},    // plafond baissé sous la dette : rien, jamais négatif
		{3000, 20000, 23000},  // avance + ardoise
	}
	for _, c := range cases {
		if got := Available(c.balance, c.limit); got != c.want {
			t.Errorf("Available(%d, %d) = %d, want %d", c.balance, c.limit, got, c.want)
		}
	}
}

func TestCashDelta(t *testing.T) {
	lines := []models.MOPLine{
		{MOP: "ES", Amount: 9999},              // vente : comptée ailleurs
		{MOP: "ACCOUNT", Amount: 4000},         // commande sur compte : pas d'argent
		{MOP: "ACCOUNT_IN_ES", Amount: 50000},  // espèces versées
		{MOP: "ACCOUNT_OUT_ES", Amount: -2000}, // espèces rendues
		{MOP: "ACCOUNT_IN_CB", Amount: 70000},  // carte : pas dans le tiroir
	}
	if got := CashDelta(lines); got != 48000 {
		t.Fatalf("CashDelta() = %d, want 48000", got)
	}
}
