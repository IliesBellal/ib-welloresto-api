package scannorder

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newLookupTestService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Service{repo: NewRepository(db)}, mock
}

// Un QR inconnu reste une réponse 200 « no_merchant_found » (contrat inchangé).
func TestComputeGetMerchant_UnknownQR(t *testing.T) {
	s, mock := newLookupTestService(t)
	mock.ExpectQuery("FROM\\s+qrcodes").WillReturnError(sql.ErrNoRows)

	resp, err := s.computeGetMerchant(context.Background(), "inconnu")
	if err != nil {
		t.Fatalf("QR inconnu ne doit pas être une erreur: %v", err)
	}
	if resp == nil || resp.Status != "no_merchant_found" || resp.Merchant != nil {
		t.Fatalf("réponse inattendue: %+v", resp)
	}
}

// Une erreur de base (annulation, connexion…) n'est plus déguisée en QR inconnu :
// GetMerchant aurait mis cette fausse réponse en cache.
func TestComputeGetMerchant_DBErrorIsReturned(t *testing.T) {
	s, mock := newLookupTestService(t)
	mock.ExpectQuery("FROM\\s+qrcodes").WillReturnError(context.Canceled)

	resp, err := s.computeGetMerchant(context.Background(), "tastyfoodyutz")
	if err == nil {
		t.Fatalf("une erreur de base doit être remontée, reçu %+v", resp)
	}
	if resp != nil {
		t.Fatalf("aucune réponse attendue avec l'erreur, reçu %+v", resp)
	}
}

// GetMenu ne doit jamais dérefencer un Merchant nil (panic observé en prod).
func TestGetMenu_UnknownQR_NoPanic(t *testing.T) {
	s, mock := newLookupTestService(t)
	mock.ExpectQuery("FROM\\s+qrcodes").WillReturnError(sql.ErrNoRows)

	menu, err := s.GetMenu(context.Background(), "inconnu", "TAKE_AWAY")
	if !errors.Is(err, ErrMerchantUnavailable) {
		t.Fatalf("ErrMerchantUnavailable attendue, reçu (%+v, %v)", menu, err)
	}
}

func TestGetMenu_DBErrorIsNotMerchantUnavailable(t *testing.T) {
	s, mock := newLookupTestService(t)
	mock.ExpectQuery("FROM\\s+qrcodes").WillReturnError(errors.New("connection refused"))

	_, err := s.GetMenu(context.Background(), "tastyfoodyutz", "TAKE_AWAY")
	if err == nil || errors.Is(err, ErrMerchantUnavailable) {
		t.Fatalf("une panne de base doit rester une erreur serveur, reçu %v", err)
	}
}

func TestIsClientGone(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"pas d'erreur", context.Background(), nil, false},
		{"contexte annulé", canceled, errors.New("tls error: context canceled"), true},
		{"erreur enveloppant context.Canceled", context.Background(), errors.Join(errors.New("dial"), context.Canceled), true},
		{"panne base, contexte vivant", context.Background(), errors.New("connection refused"), false},
		{"aucune ligne, contexte vivant", context.Background(), sql.ErrNoRows, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClientGone(tt.ctx, tt.err); got != tt.want {
				t.Fatalf("isClientGone = %v, attendu %v", got, tt.want)
			}
		})
	}
}
