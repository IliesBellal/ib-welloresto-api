package order_life_cycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/helpers"
	stripeclient "welloresto-api/internal/infrastructure/stripe"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
)

// Paiement par QR code depuis la caisse. La caisse demande une session Stripe
// Checkout pour une part de la commande et affiche son adresse en QR code.
// Aucun paiement n'est écrit ici : c'est le webhook checkout.session.completed
// (channel=pos_qr) qui l'enregistre une fois le client passé à la caisse
// Stripe, puis prévient la caisse.

// QRPaymentRequest : POST /orders/{order_id}/payments/qr.
type QRPaymentRequest struct {
	// Amount en centimes : de 0,50 € au reste à payer.
	Amount int `json:"amount"`
	// DeviceID : appareil de la caisse, qui donne le registre ouvert (comme pour
	// un paiement saisi).
	DeviceID string `json:"device_id"`
}

// QRPaymentSession : la session créée.
type QRPaymentSession struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Amount    int    `json:"amount"`
	ExpiresAt int64  `json:"expires_at"`
}

// SetScanNOrderBaseURL fixe la base de la page de retour du client (vide :
// adresse publique par défaut de ScanNOrder).
func (s *OrdersLifeCycleService) SetScanNOrderBaseURL(baseURL string) {
	s.scannorderBaseURL = baseURL
}

// CreateQRPaymentSession ouvre la session Stripe Checkout d'une part de la
// commande.
func (s *OrdersLifeCycleService) CreateQRPaymentSession(ctx context.Context, orderID string, req *QRPaymentRequest) (*QRPaymentSession, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if s.stripeManager == nil {
		return nil, models.ErrQRPaymentUnavailable
	}

	order, err := s.ordersLifeCycleRepo.qrPaymentOrder(ctx, user.MerchantID, orderID)
	if err != nil {
		return nil, err
	}
	if !order.open {
		return nil, models.ErrOrderClosed
	}
	if req.Amount < stripeclient.POSQRMinAmount || req.Amount > order.due() {
		return nil, models.ErrQRPaymentInvalidAmount
	}

	account, ok, err := s.ordersLifeCycleRepo.qrPaymentStripeAccount(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, models.ErrQRPaymentUnavailable
	}

	// Registre ouvert de l'appareil, relevé maintenant : le webhook y rattache le
	// paiement même si la caisse est fermée entre-temps.
	registerID, err := s.ordersLifeCycleRepo.GetActiveCashRegisterID(ctx, user.MerchantID, req.DeviceID)
	if err != nil {
		return nil, err
	}

	returnURL := helpers.ScanNOrderBaseURL(s.scannorderBaseURL)
	if slug := s.ordersLifeCycleRepo.qrPaymentMerchantSlug(ctx, user.MerchantID); slug != "" {
		returnURL += "/restaurant/" + slug
	}
	description := "Commande"
	if order.num != "" {
		description += " n°" + order.num
	}

	amount := int64(req.Amount)
	session, err := s.stripeManager.CreatePOSQRCheckout(stripeclient.POSQRCheckout{
		StripeAccountID: account.accountID,
		Amount:          amount,
		ApplicationFee:  stripeclient.ApplicationFee(amount, account.variableFees, account.fixedFees),
		Description:     description,
		SuccessURL:      returnURL,
		CancelURL:       returnURL,
		Metadata: map[string]string{
			"channel":          stripeclient.POSQRPaymentChannel,
			"merchant_id":      user.MerchantID,
			"order_id":         orderID,
			"cash_register_id": registerID,
			"user_id":          user.UserID,
		},
	})
	if err != nil {
		logger.FromContext(ctx).Error("[pos qr] create checkout session: " + err.Error())
		return nil, fmt.Errorf("create qr checkout session: %w", err)
	}

	return &QRPaymentSession{
		SessionID: session.ID,
		URL:       session.URL,
		Amount:    req.Amount,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// CreateQRPayment POST /orders/{order_id}/payments/qr — session Stripe Checkout
// d'une part de la commande, dont la caisse affiche l'adresse en QR code.
func (h *OrdersLifeCycleHandler) CreateQRPayment(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		models.SendJSON(w, http.StatusBadRequest, "order_life_cycle", "create_qr_payment", map[string]string{"error": "missing_parameter"})
		return
	}
	var req QRPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "create_qr_payment", models.ErrInvalidRequestBody)
		return
	}
	session, err := h.ordersLifeCycleService.CreateQRPaymentSession(r.Context(), orderID, &req)
	if err != nil {
		models.SendErrorJSON(w, "order_life_cycle", "create_qr_payment", err)
		return
	}
	models.SendJSON(w, http.StatusOK, "order_life_cycle", "create_qr_payment", session)
}

type qrPaymentOrder struct {
	price int
	paid  int
	open  bool
	num   string
}

// due : reste à payer (jamais négatif).
func (o qrPaymentOrder) due() int {
	if o.price <= o.paid {
		return 0
	}
	return o.price - o.paid
}

// qrPaymentOrder lit le prix, ce qui est déjà payé et l'état d'une commande de
// l'établissement (models.ErrNotFound si elle n'existe pas).
func (r *OrdersLifeCycleRepository) qrPaymentOrder(ctx context.Context, merchantID, orderID string) (qrPaymentOrder, error) {
	db := dbx.GetDB(ctx, r.database)
	var o qrPaymentOrder
	var state string
	var num sql.NullString
	var paid float64
	err := db.QueryRowContext(ctx, `
		SELECT o.price, o.state, o.order_num,
		       COALESCE((SELECT SUM(p.amount) FROM payments p WHERE p.order_id = o.order_id AND p.enabled = TRUE), 0)
		FROM orders o
		WHERE o.order_id = ? AND o.merchant_id = ?`, orderID, merchantID).Scan(&o.price, &state, &num, &paid)
	if errors.Is(err, sql.ErrNoRows) {
		return o, models.ErrNotFound
	}
	if err != nil {
		return o, fmt.Errorf("qr payment: read order: %w", err)
	}
	o.open = state == "OPEN"
	o.num = num.String
	o.paid = int(math.Round(paid))
	return o, nil
}

type qrPaymentAccount struct {
	accountID    string
	variableFees float64
	fixedFees    int64
}

// qrPaymentStripeAccount : compte Stripe Connect actif de l'établissement et
// frais ScanNOrder qui s'appliquent à ses paiements en ligne.
func (r *OrdersLifeCycleRepository) qrPaymentStripeAccount(ctx context.Context, merchantID string) (qrPaymentAccount, bool, error) {
	db := dbx.GetDB(ctx, r.database)
	var a qrPaymentAccount
	var variable sql.NullFloat64
	var fixed sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(sa.account_id, ''), snos.variable_fees, snos.fixed_fees
		FROM stripe_accounts sa
		LEFT JOIN scannorder_settings snos ON snos.merchant_id = sa.merchant_id
		WHERE sa.merchant_id = ? AND COALESCE(sa.account_id, '') <> ''
		LIMIT 1`, merchantID).Scan(&a.accountID, &variable, &fixed)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, fmt.Errorf("qr payment: stripe account: %w", err)
	}
	a.variableFees, a.fixedFees = variable.Float64, fixed.Int64
	return a, true, nil
}

// qrPaymentMerchantSlug : identifiant public de l'établissement dans l'adresse
// de ScanNOrder ("" si introuvable : le client retombe sur l'accueil).
func (r *OrdersLifeCycleRepository) qrPaymentMerchantSlug(ctx context.Context, merchantID string) string {
	db := dbx.GetDB(ctx, r.database)
	var code sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT code FROM qrcodes WHERE merchant_id = ? LIMIT 1`, merchantID).Scan(&code); err != nil {
		return ""
	}
	return strings.TrimSpace(code.String)
}
