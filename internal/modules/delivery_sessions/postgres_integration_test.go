//go:build postgres_integration

package delivery_sessions

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/orders"
)

// Couvre le cycle de vie complet d'une tournée de livraison contre Postgres :
// démarrage (InsertReturningID + UPDATE...FROM), sélection/arrivée/livraison/
// échec des stops (UTCNow), clôtures (conducteur + manager + annulation), et
// l'assemblage complet (jointures castées vers user_status_view/users_rights).
func TestDeliverySessionsRepository_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	var userID string // dérivé de users_rights.id (jointure héritée ur.id = user_id)

	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		_, _ = db.ExecContext(ctx, `DELETE FROM payments WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session_order WHERE delivery_session_id IN (SELECT id FROM delivery_session WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM delivery_session WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE user_id IN (SELECT user_id FROM users_rights WHERE merchant_id = $1)`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM users_rights WHERE merchant_id = $1`, mid)
		_, _ = db.ExecContext(ctx, `DELETE FROM merchant WHERE id = $1`, mid)
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-ds' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone, lat, lng)
		VALUES ('ITest DS Merchant', 'a', '1', 's', '75001', 'Paris', 'siret-ds', 'https://x', '06', 'mtok-ds', 'Europe/Paris', 1, 2)
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)

	// La jointure héritée `ur.id = usv.user_id` (assembleDeliverySessionDetails)
	// ne matche que les user_id numériques égaux à un id de users_rights — on
	// crée d'abord la ligne de droits, puis l'utilisateur avec cet id.
	var rightsID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users_rights (user_id, merchant_id, token, enabled)
		VALUES ('placeholder-ds', $1, 'ds-rights-tok', true) RETURNING id`, merchantID).Scan(&rightsID); err != nil {
		t.Fatalf("seed users_rights: %v", err)
	}
	userID = strconv.FormatInt(rightsID, 10)
	if _, err := db.ExecContext(ctx, `UPDATE users_rights SET user_id = $1 WHERE id = $2`, userID, rightsID); err != nil {
		t.Fatalf("bind rights user_id: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (user_id, merchant_id, name, first_name, last_name, password, email, token, lat, lng)
		VALUES ($1, $2, 'ITest Livreur', 'Livreur', 'Test', 'x', 'itest-ds@example.com', 'ds-tok', '48.85', '2.35')`, userID, merchantID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	var customerID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO customer (customer_name, merchant_id, customer_tel, delivery_notes)
		VALUES ('ITest DS Client', $1, '+33612345678', 'code 1234B') RETURNING customer_id`, merchantID).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	newOrder := func() string {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, `
			INSERT INTO orders (merchant_id, customer_id, order_num, brand_status, order_type, state, price, TVA, HT, created_by, fulfillment_type)
			VALUES ($1, $2, 1, 'READY_FOR_HANDOFF', 'DELIVERY', 'OPEN', 1500, 150, 1350, $3, 'DELIVERY_BY_RESTAURANT')
			RETURNING order_id`, merchantID, customerID, userID).Scan(&id); err != nil {
			t.Fatalf("seed order: %v", err)
		}
		return strconv.FormatInt(id, 10)
	}
	order1 := newOrder()
	order2 := newOrder()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (merchant_id, user_id, order_id, amount, mop, enabled)
		VALUES ($1, 'itest-ds-cashier', $2, 1500, 'ES', true)`, merchantID, order1); err != nil {
		t.Fatalf("seed payment: %v", err)
	}

	fetcher := orders.NewOrdersFetcher(db)
	repo := NewDeliverySessionsRepository(db, fetcher)

	// --- StartDeliverySession ---
	req := &models.DeliverySessionRequest{
		MerchantID: merchantID,
		Distance:   "1200",
		Duration:   "900",
		Orders:     []models.DeliveryOrderItem{{OrderID: order1}, {OrderID: order2}},
	}
	req.DeliveryMan.UserID = userID
	session, err := repo.StartDeliverySession(ctx, req)
	if err != nil {
		t.Fatalf("StartDeliverySession failed against postgres: %v", err)
	}
	sessionID := session.DeliverySessionID

	var brandStatus string
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, order1).Scan(&brandStatus); err != nil {
		t.Fatalf("read back order: %v", err)
	}
	if brandStatus != "EN_ROUTE_TO_DROPOFF" {
		t.Fatalf("expected EN_ROUTE_TO_DROPOFF after start, got %q", brandStatus)
	}

	// Double démarrage → refusé
	if _, err := repo.StartDeliverySession(ctx, req); err != models.ErrDeliverySessionAlreadyActive {
		t.Fatalf("expected ErrDeliverySessionAlreadyActive, got %v", err)
	}

	// --- GetPendingDeliverySessions (IN quoté + assemblage fetcher) ---
	pending, err := repo.GetPendingDeliverySessions(ctx, merchantID)
	if err != nil {
		t.Fatalf("GetPendingDeliverySessions failed against postgres: %v", err)
	}
	if len(pending) != 1 || len(pending[0].Orders) != 2 {
		t.Fatalf("expected 1 session with 2 orders, got %+v", pending)
	}
	if pending[0].Orders[0].DeliveryStop == nil {
		t.Fatal("expected per-stop state attached to orders")
	}

	// --- GetDeliverySession / GetActiveDeliverySessionForUser (jointures castées) ---
	got, err := repo.GetDeliverySession(ctx, merchantID, sessionID)
	if err != nil {
		t.Fatalf("GetDeliverySession failed against postgres: %v", err)
	}
	if got.DeliveryMan.UserID != userID || len(got.Orders) != 2 {
		t.Fatalf("unexpected assembled session: man=%+v orders=%d", got.DeliveryMan, len(got.Orders))
	}
	if got.Orders[0].Customer == nil || got.Orders[0].Customer.CustomerDeliveryNotes == nil || *got.Orders[0].Customer.CustomerDeliveryNotes != "code 1234B" {
		t.Fatalf("expected delivery notes merged into customer, got %+v", got.Orders[0].Customer)
	}

	active, err := repo.GetActiveDeliverySessionForUser(ctx, merchantID, userID)
	if err != nil {
		t.Fatalf("GetActiveDeliverySessionForUser failed against postgres: %v", err)
	}
	if active.DeliverySessionID != sessionID {
		t.Fatalf("expected active session %s, got %s", sessionID, active.DeliverySessionID)
	}

	// --- GetOrderCustomerPhones ---
	phones, err := repo.GetOrderCustomerPhones(ctx, []string{order1, order2})
	if err != nil {
		t.Fatalf("GetOrderCustomerPhones failed against postgres: %v", err)
	}
	if phones[order1] != "+33612345678" {
		t.Fatalf("unexpected phones: %+v", phones)
	}

	// --- FSM des stops ---
	sid, err := repo.SelectDeliveryStop(ctx, merchantID, userID, order1)
	if err != nil || sid != sessionID {
		t.Fatalf("SelectDeliveryStop = (%s, %v)", sid, err)
	}
	if _, err := repo.MarkDeliveryStopArrived(ctx, merchantID, userID, order1); err != nil {
		t.Fatalf("MarkDeliveryStopArrived failed against postgres: %v", err)
	}
	if _, err := repo.ResolveDeliverableStop(ctx, merchantID, userID, order1); err != nil {
		t.Fatalf("ResolveDeliverableStop failed against postgres: %v", err)
	}
	if err := repo.FinalizeDeliveredStop(ctx, sessionID, order1); err != nil {
		t.Fatalf("FinalizeDeliveredStop failed against postgres: %v", err)
	}

	var stopStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM delivery_session_order WHERE delivery_session_id = $1 AND order_id = $2`, sessionID, order1).Scan(&stopStatus); err != nil {
		t.Fatalf("read back stop: %v", err)
	}
	if stopStatus != "delivered" {
		t.Fatalf("expected delivered stop, got %q", stopStatus)
	}
	var payUser string
	if err := db.QueryRowContext(ctx, `SELECT user_id FROM payments WHERE order_id = $1`, order1).Scan(&payUser); err != nil {
		t.Fatalf("read back payment: %v", err)
	}
	if payUser != userID {
		t.Fatalf("expected payment reassigned to delivery man %s, got %q", userID, payUser)
	}
	// current avancé sur order2 (en_route)
	var currentOrder *string
	if err := db.QueryRowContext(ctx, `SELECT current_order_id FROM delivery_session WHERE id = $1`, sessionID).Scan(&currentOrder); err != nil {
		t.Fatalf("read back session: %v", err)
	}
	if currentOrder == nil || *currentOrder != order2 {
		t.Fatalf("expected current stop advanced to %s, got %v", order2, currentOrder)
	}

	// --- échec du stop courant (terminalize + advance → NULL) ---
	if _, err := repo.MarkDeliveryStopFailed(ctx, merchantID, userID, order2, "client absent", nil, nil); err != nil {
		t.Fatalf("MarkDeliveryStopFailed failed against postgres: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, order2).Scan(&brandStatus); err != nil {
		t.Fatalf("read back failed order: %v", err)
	}
	if brandStatus != "DELIVERY_FAILED" {
		t.Fatalf("expected DELIVERY_FAILED, got %q", brandStatus)
	}

	// --- clôture par le livreur (stops tous terminaux) ---
	if _, err := repo.CloseMyDeliverySession(ctx, merchantID, userID); err != nil {
		t.Fatalf("CloseMyDeliverySession failed against postgres: %v", err)
	}
	var sessStatus string
	var heading int
	if err := db.QueryRowContext(ctx, `SELECT status FROM delivery_session WHERE id = $1`, sessionID).Scan(&sessStatus); err != nil {
		t.Fatalf("read back session status: %v", err)
	}
	if sessStatus != "done" {
		t.Fatalf("expected done session, got %q", sessStatus)
	}
	if err := db.QueryRowContext(ctx, `SELECT heading FROM users WHERE user_id = $1`, userID).Scan(&heading); err != nil {
		t.Fatalf("read back user heading: %v", err)
	}
	if heading != 0 {
		t.Fatalf("expected heading reset to 0, got %d", heading)
	}

	// --- CancelDeliverySession (manager) sur une nouvelle tournée ---
	order3 := newOrder()
	req2 := &models.DeliverySessionRequest{MerchantID: merchantID, Distance: "500", Duration: "300", Orders: []models.DeliveryOrderItem{{OrderID: order3}}}
	req2.DeliveryMan.UserID = userID
	s2, err := repo.StartDeliverySession(ctx, req2)
	if err != nil {
		t.Fatalf("StartDeliverySession (2) failed: %v", err)
	}
	canceled, err := repo.CancelDeliverySession(ctx, s2.DeliverySessionID)
	if err != nil {
		t.Fatalf("CancelDeliverySession failed against postgres: %v", err)
	}
	if canceled.MerchantID != merchantID || canceled.Status != "canceled" {
		t.Fatalf("unexpected canceled session: %+v", canceled)
	}
	if err := db.QueryRowContext(ctx, `SELECT brand_status FROM orders WHERE order_id = $1`, order3).Scan(&brandStatus); err != nil {
		t.Fatalf("read back canceled order: %v", err)
	}
	if brandStatus != "READY_FOR_HANDOFF" {
		t.Fatalf("expected order reverted to READY_FOR_HANDOFF, got %q", brandStatus)
	}

	// --- Clôture manager sur une troisième tournée ---
	//
	// La clôture manager est désormais orchestrée par le service
	// (DeliverySessionsService.CloseDeliverySession) : il livre chaque arrêt
	// ouvert via order_life_cycle.SetDelivered — hash NF525, signature, audit —
	// puis appelle MarkDeliverySessionDone. Le repo ne ferme donc plus les
	// commandes lui-même, et ce test couvre les deux primitives qu'il expose.
	s3, err := repo.StartDeliverySession(ctx, req2)
	if err != nil {
		t.Fatalf("StartDeliverySession (3) failed: %v", err)
	}

	openStops, err := repo.GetOpenStopOrderIDs(ctx, merchantID, s3.DeliverySessionID)
	if err != nil {
		t.Fatalf("GetOpenStopOrderIDs failed against postgres: %v", err)
	}
	if len(openStops) != 1 || openStops[0] != order3 {
		t.Fatalf("expected the session's only open stop to be %q, got %v", order3, openStops)
	}

	closed, err := repo.MarkDeliverySessionDone(ctx, merchantID, s3.DeliverySessionID)
	if err != nil {
		t.Fatalf("MarkDeliverySessionDone failed against postgres: %v", err)
	}
	if closed.Status != "done" {
		t.Fatalf("unexpected closed session: %+v", closed)
	}

	// La commande reste intacte : sa clôture fiscale appartient à SetDelivered,
	// pas au repository. Cette assertion garde la régression d'origine — un
	// UPDATE en masse qui fermait les commandes sans hash ni signature.
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state, brand_status FROM orders WHERE order_id = $1`, order3).Scan(&state, &brandStatus); err != nil {
		t.Fatalf("read back order after session close: %v", err)
	}
	if state != "OPEN" || brandStatus != "EN_ROUTE_TO_DROPOFF" {
		t.Fatalf("expected the repository to leave the order untouched (OPEN/EN_ROUTE_TO_DROPOFF), got %s/%s", state, brandStatus)
	}

	// Une session close n'expose plus d'arrêt ouvert : rejouer la clôture est
	// un no-op, ce sur quoi repose la reprise sur incident du service.
	openStops, err = repo.GetOpenStopOrderIDs(ctx, merchantID, s3.DeliverySessionID)
	if err != nil {
		t.Fatalf("GetOpenStopOrderIDs (after close) failed against postgres: %v", err)
	}
	if len(openStops) != 0 {
		t.Fatalf("expected no open stop on a done session, got %v", openStops)
	}

	// --- GetDeliverySessionByIDForUser (sans filtre de statut) ---
	byID, err := repo.GetDeliverySessionByIDForUser(ctx, merchantID, userID, s3.DeliverySessionID)
	if err != nil {
		t.Fatalf("GetDeliverySessionByIDForUser failed against postgres: %v", err)
	}
	if byID.Status != "done" {
		t.Fatalf("expected done session by id, got %+v", byID)
	}
}
