package ubereats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/fiscal"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/distributiontime"
	"welloresto-api/internal/modules/receipt"
	"welloresto-api/internal/utils/dbutils"
)

type UberRepository struct {
	database *sql.DB
}

func NewUberEatsRepository(db *sql.DB) *UberRepository {
	return &UberRepository{database: db}
}

// ueMerchantJoinCast retourne le fragment de jointure merchant.id (integer)
// vs iue.merchant_id (varchar) selon le dialecte — MySQL coerçait, Postgres
// exige le cast (CHAR nu = char(1) en PG, d'où TEXT).
func ueMerchantJoinCast() string {
	if dbx.ActiveDialect() == dbx.Postgres {
		return "CAST(m.id AS TEXT)"
	}
	return "CAST(m.id AS CHAR)"
}

// GetMerchantIDFromStoreID récupère le merchant_id du store
func (r *UberRepository) GetMerchantIDFromStoreID(ctx context.Context, storeID string) (*string, error) {
	db := dbx.GetDB(ctx, r.database)
	log := logger.FromContext(ctx)

	query := fmt.Sprintf(`
		SELECT iue.merchant_id
		FROM integration_uber_eats iue
		INNER JOIN merchant m on %s = iue.merchant_id
		WHERE iue.store_id = ?
		ORDER BY iue.enabled DESC, iue.merchant_id
		LIMIT 1`, ueMerchantJoinCast())

	var MerchantID string
	// Gestion des NULLs potentiels avec sql.NullInt64 si nécessaire, ici simplifié
	row := db.QueryRowContext(ctx, query, storeID)
	err := row.Scan(&MerchantID)
	if err != nil {
		log.Error("store id : " + storeID + " not found :" + err.Error())
		return nil, err
	}
	return &MerchantID, nil
}

// GetStoreData récupère les infos du magasin "principal" du marchand.
//
// Un marchand peut avoir plusieurs comptes Uber Eats (migration 111, PK
// composite (merchant_id, store_id)) : le compte actif passe en premier (un
// restaurant déconnecté puis remplacé par un autre ne doit pas rester
// "principal"), puis ORDER BY store_id rend le choix déterministe. Pour un
// marchand mono-compte (le cas de tous les marchands aujourd'hui), c'est
// rigoureusement le même comportement qu'avant. Un appelant qui connaît le
// compte exact doit utiliser GetStoreDataByStoreID.
func (r *UberRepository) GetStoreData(ctx context.Context, merchantID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
       SELECT iue.merchant_id, iue.store_id, m.timezone,
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON %s = iue.merchant_id
       WHERE iue.merchant_id = ?
       ORDER BY iue.enabled DESC, iue.store_id
       LIMIT 1`, ueMerchantJoinCast())

	var store Store

	row := db.QueryRowContext(ctx, query, merchantID)

	err := row.Scan(
		&store.MerchantID,
		&store.StoreID,
		&store.Timezone,
		&store.EstimatedPreparationTime,
		&store.LastEstimatedPreparationTime,
		&store.AutoAcceptOrders,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &store, nil
}

// GetStoreDataByStoreID récupère un compte Uber Eats précis d'un marchand.
// storeID est validé comme appartenant à merchantID (défense en profondeur :
// un utilisateur authentifié pour un marchand ne doit jamais pouvoir lire/agir
// sur le compte Uber Eats d'un autre marchand via un store_id arbitraire).
func (r *UberRepository) GetStoreDataByStoreID(ctx context.Context, merchantID, storeID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
       SELECT iue.merchant_id, iue.store_id, m.timezone,
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON %s = iue.merchant_id
       WHERE iue.merchant_id = ? AND iue.store_id = ?`, ueMerchantJoinCast())

	var store Store
	err := db.QueryRowContext(ctx, query, merchantID, storeID).Scan(
		&store.MerchantID,
		&store.StoreID,
		&store.Timezone,
		&store.EstimatedPreparationTime,
		&store.LastEstimatedPreparationTime,
		&store.AutoAcceptOrders,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &store, nil
}

// GetAccountsByMerchant liste tous les comptes Uber Eats d'un marchand
// (1 aujourd'hui pour tous les marchands, potentiellement plusieurs depuis la
// migration 111).
func (r *UberRepository) GetAccountsByMerchant(ctx context.Context, merchantID string) ([]Store, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
       SELECT iue.merchant_id, iue.store_id, m.timezone,
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON %s = iue.merchant_id
       WHERE iue.merchant_id = ?
       ORDER BY iue.enabled DESC, iue.store_id`, ueMerchantJoinCast())

	rows, err := db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stores []Store
	for rows.Next() {
		var store Store
		if err := rows.Scan(
			&store.MerchantID,
			&store.StoreID,
			&store.Timezone,
			&store.EstimatedPreparationTime,
			&store.LastEstimatedPreparationTime,
			&store.AutoAcceptOrders,
		); err != nil {
			return nil, err
		}
		stores = append(stores, store)
	}
	return stores, rows.Err()
}

// GetStoreByStoreIDOnly récupère un compte Uber Eats par store_id seul, sans
// connaître le merchant_id à l'avance. Utilisé par le chemin webhook : Uber
// Eats notifie une commande avec le store_id, jamais le merchant_id WelloResto.
// L'index idx_integration_uber_eats_store_id (migration 111) n'est PAS
// unique : un restaurant déconnecté d'un établissement puis relié à un autre
// laisse une ligne inactive derrière lui. La ligne active est donc préférée
// (ORDER BY enabled DESC) ; une seule ligne active par store_id est garantie à
// la connexion (ActiveStoreOwner).
func (r *UberRepository) GetStoreByStoreIDOnly(ctx context.Context, storeID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`
       SELECT iue.merchant_id, iue.store_id, m.timezone,
              iue.estimated_preparation_time, iue.last_estimated_preparation_time,
			  iue.auto_accept_orders
       FROM integration_uber_eats iue
       INNER JOIN merchant m ON %s = iue.merchant_id
       WHERE iue.store_id = ?
       ORDER BY iue.enabled DESC, iue.merchant_id
       LIMIT 1`, ueMerchantJoinCast())

	var store Store
	err := db.QueryRowContext(ctx, query, storeID).Scan(
		&store.MerchantID,
		&store.StoreID,
		&store.Timezone,
		&store.EstimatedPreparationTime,
		&store.LastEstimatedPreparationTime,
		&store.AutoAcceptOrders,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &store, nil
}

// GetCurrentToken récupère le token actuel
func (r *UberRepository) GetCurrentToken(ctx context.Context, tokenType string) (*UberToken, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `SELECT access_token, expires_at FROM external_tokens WHERE token_type = ?`
	var token UberToken

	err := db.QueryRowContext(ctx, query, tokenType).Scan(&token.AccessToken, &token.ExpiresAt)

	return &token, err
}

// SaveNewToken met à jour le token
func (r *UberRepository) SaveNewToken(ctx context.Context, tokenType, accessToken string, expiresIn int) error {
	db := dbx.GetDB(ctx, r.database)

	// Upsert par dialecte (PK external_tokens.token_type) ; l'intervalle est
	// parametre : DATE_ADD(..., INTERVAL ? SECOND) n'a pas d'equivalent PG
	// direct -> `now() + (? * interval '1 second')` (pattern Tier 1 upsell).
	query := `
       INSERT INTO external_tokens (token_type, access_token, expires_at)
       VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND))
       ON DUPLICATE KEY UPDATE expires_at = DATE_ADD(UTC_TIMESTAMP, INTERVAL ? SECOND), access_token = ?`
	if dbx.ActiveDialect() == dbx.Postgres {
		query = `
       INSERT INTO external_tokens (token_type, access_token, expires_at)
       VALUES (?, ?, now() + (? * interval '1 second'))
       ON CONFLICT (token_type) DO UPDATE SET expires_at = now() + (? * interval '1 second'), access_token = ?`
	}

	_, err := db.ExecContext(ctx, query, tokenType, accessToken, expiresIn, expiresIn, accessToken)
	return err
}

// ActivateStore crée (ou réactive) le compte Uber Eats d'un restaurant que le
// restaurateur vient de connecter depuis le back-office
// (docs/marketplace-onboarding.md). Postgres uniquement.
//
//   - bearer_token = '' : la colonne ne porte plus de jeton (toutes les
//     opérations passent par le token client_credentials), mais « bearer_token
//     IS NOT NULL » est le marqueur « Uber Eats relié » que lit la réponse de
//     connexion du POS (auth/users repository). '' le pose sans rien stocker de
//     secret ; une réactivation conserve une valeur historique existante.
//   - delay_duration / auto_accept_orders / pos_provisionning_* sont NOT NULL
//     sans défaut : valeurs neutres à la création (acceptation automatique
//     désactivée, le restaurateur l'active dans ses réglages), jamais touchées
//     ensuite.
//   - ON CONFLICT sur la PK (merchant_id, store_id) : reconnecter le même
//     restaurant réactive la ligne et conserve ses réglages.
func (r *UberRepository) ActivateStore(ctx context.Context, merchantID, storeID string) error {
	db := dbx.GetDB(ctx, r.database)
	const q = `
		INSERT INTO integration_uber_eats (
			merchant_id, store_id, bearer_token, enabled, auto_accept_orders, delay_duration,
			pos_provisionning_refresh_token, pos_provisionning_token_expiration_date, unlink_date
		)
		VALUES (?, ?, '', true, false, 0, '', now(), NULL)
		ON CONFLICT (merchant_id, store_id) DO UPDATE SET
			enabled = true,
			bearer_token = COALESCE(integration_uber_eats.bearer_token, ''),
			unlink_date = NULL`
	_, err := db.ExecContext(ctx, q, merchantID, storeID)
	return err
}

// DeactivateStore désactive un compte Uber Eats précis du marchand (retrait de
// l'intégration) et lève le marqueur bearer_token lu par le POS.
func (r *UberRepository) DeactivateStore(ctx context.Context, merchantID, storeID string) error {
	db := dbx.GetDB(ctx, r.database)
	const q = `
		UPDATE integration_uber_eats
		SET enabled = false, bearer_token = NULL, refresh_token = NULL, unlink_date = now()
		WHERE merchant_id = ? AND store_id = ?`
	_, err := db.ExecContext(ctx, q, merchantID, storeID)
	return err
}

// DeactivateStoreEverywhere désactive toutes les lignes actives d'un store_id
// (webhook store.deprovisioned : Uber ne dit pas pour quel marchand) et
// renvoie les marchands concernés.
func (r *UberRepository) DeactivateStoreEverywhere(ctx context.Context, storeID string) ([]string, error) {
	db := dbx.GetDB(ctx, r.database)
	const q = `
		UPDATE integration_uber_eats
		SET enabled = false, bearer_token = NULL, refresh_token = NULL, unlink_date = now()
		WHERE store_id = ? AND enabled = true
		RETURNING merchant_id`
	rows, err := db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var merchants []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		merchants = append(merchants, m)
	}
	return merchants, rows.Err()
}

// ActiveStoreOwner renvoie le marchand dont un compte actif porte ce store_id
// ("" s'il n'y en a pas). Sert à refuser de relier un même restaurant Uber Eats
// à deux établissements Wello Resto.
func (r *UberRepository) ActiveStoreOwner(ctx context.Context, storeID string) (string, error) {
	db := dbx.GetDB(ctx, r.database)
	const q = `SELECT merchant_id FROM integration_uber_eats WHERE store_id = ? AND enabled = true ORDER BY merchant_id LIMIT 1`
	var m string
	err := db.QueryRowContext(ctx, q, storeID).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return m, err
}

// GetOrderMetadata récupère les IDs pour la requête. StoreID identifie le
// compte Uber Eats exact d'où vient la commande (colonne orders.brand_store_id,
// migration 111) : indispensable pour AcceptOrder dès qu'un marchand a
// plusieurs comptes, GetStoreData(merchantID) seul ne suffit plus à choisir
// le bon.
func (r *UberRepository) GetOrderMetadata(ctx context.Context, localOrderID string) (*UberOrderMetadata, error) {
	db := dbx.GetDB(ctx, r.database)

	query := `
       SELECT o.brand_order_id, o.creation_date, o.brand_store_id
       FROM orders o
       INNER JOIN integration_uber_eats iue
           on iue.merchant_id = o.merchant_id
           AND (o.brand_store_id IS NULL OR iue.store_id = o.brand_store_id)
       WHERE o.order_id = ?`

	var meta UberOrderMetadata
	var storeID sql.NullString

	err := db.QueryRowContext(ctx, query, localOrderID).Scan(&meta.BrandOrderID, &meta.CreationDate, &storeID)

	if err != nil {
		return nil, fmt.Errorf("cannot retrieve uber order id: %v", err)
	}
	if storeID.Valid {
		meta.StoreID = storeID.String
	}
	return &meta, nil
}

// UpdateOrderAccepted met à jour le statut local
func (r *UberRepository) UpdateOrderAccepted(ctx context.Context, orderID string, prepTime int) error {
	db := dbx.GetDB(ctx, r.database)

	estimatedReady := "DATE_ADD(UTC_TIMESTAMP, INTERVAL ? MINUTE)"
	if dbx.ActiveDialect() == dbx.Postgres {
		estimatedReady = "now() + (? * interval '1 minute')"
	}
	query := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = 'ACCEPTED', merchant_approval = 'ACCEPTED',
		    last_update = %s,
		    estimated_ready = %s
		WHERE order_id = ?`, dbx.UTCNow(), estimatedReady)
	_, err := db.ExecContext(ctx, query, prepTime, orderID)
	return err
}

// CalculateAutoPrepTime calcule le temps de préparation automatique
func (r *UberRepository) CalculateAutoPrepTime(ctx context.Context, merchantID, orderID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)

	// 1. Get product count
	var qty int
	err := db.QueryRowContext(ctx, "SELECT sum(quantity) FROM orderitems WHERE order_id = ?", orderID).Scan(&qty)
	if err != nil {
		return 0, err
	}

	// 2. Temps de distribution estimé (ex-procédure GET_AVERAGE_DISTRIBUTION_TIME)
	estTime, _, err := distributiontime.EstimatedSeconds(ctx, r.database, merchantID, qty)
	if err != nil {
		return 0, err
	}

	// Logique PHP: intval($data['estimated_distribution_time'])/60*0.7
	return int((float64(estTime) / 60) * 0.7), nil
}

// SetOrderStatusDenied met à jour le statut en DENIED
func (r *UberRepository) SetOrderStatusDenied(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE orders SET brand_status = 'DENIED', last_update = %s WHERE order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusCanceled met à jour le statut en CANCELED
func (r *UberRepository) SetOrderStatusCanceled(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	query := fmt.Sprintf(`UPDATE orders SET brand_status = 'CANCELED', status = '-1', last_update = %s WHERE order_id = ?`, dbx.UTCNow())
	_, err := db.ExecContext(ctx, query, orderID)
	return err
}

// SetOrderStatusReady met à jour orders et orderitems pour le statut READY
func (r *UberRepository) SetOrderStatusReady(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)

	queryOrder := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = 'READY_FOR_HANDOFF', status = '2', isDistributed = '1',
		    last_update = %[1]s, delivered_on = %[1]s
		WHERE order_id = ?`, dbx.UTCNow())

	queryItems := fmt.Sprintf(`
		UPDATE orderitems
		SET distributed_quantity = quantity,
		    ready_for_distribution_quantity = quantity,
		    isDistributed = '1',
		    distributed_on = %s
		WHERE order_id = ?`, dbx.UTCNow())

	if _, err := db.ExecContext(ctx, queryOrder, orderID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, queryItems, orderID); err != nil {
		return err
	}
	return nil
}

// SaleReceiptFunc émet le ticket fiscal d'une vente plateforme qui vient
// d'être clôturée, dans la transaction de la clôture
// (OrdersLifeCycleService.HandlerFiscalReceiptGeneration, injecté : ce
// module ne peut pas importer order_life_cycle, qui l'importe).
type SaleReceiptFunc func(ctx context.Context, merchantID, orderID string) error

// SyncOrderState met à jour la commande lors du finishOrderIfDoesNotExist (succès API)
//
// cancelled_by_type (PROMPT 11, §2) : ce chemin de réconciliation est
// appelé après l'échec d'un appel API Uber (Deny/Cancel/SetReady/Accept —
// voir service.go RecoverOrderState). Dans le cas Deny/Cancel, l'ordre
// d'exécution place toujours DenyOrderLocal/DeleteOrderLocal AVANT cet
// appel API (order_life_cycle/service.go SetOrderDenied/DeleteOrder) :
// cancelled_by_type porte alors déjà l'acteur réel (STAFF), qu'il ne faut
// pas écraser. `cancelled_by_type IS NULL` protège exactement ce cas —
// PLATFORM n'est posé que quand rien n'a encore classé l'annulation
// (reconciliation déclenchée par SetOrderReady/AcceptOrder, où Uber a
// fermé la commande de son propre chef, découvert incidemment). DENIED/
// CANCELED seuls comptent comme annulation ici ; DELIVERY_FAILED (déjà
// hors périmètre C2, voir lot 1) et les statuts non-terminaux (ACCEPTED,
// COMPLETED, EN_ROUTE_TO_DROPOFF) sont laissés intacts.
//
// Clôture (state CLOSED, conformité caisse, C10) : les commandes encore
// ouvertes sont clôturées (date de clôture, qui les rattache à leur clôture
// journalière scellée) et, pour une vente (COMPLETED, EN_ROUTE_TO_DROPOFF),
// reçoivent leur ticket ; une vente pas entièrement payée reste ouverte ; une
// annulation (DENIED, CANCELED, DELIVERY_FAILED) voit ses paiements annulés
// (closePlatformOrder, lot 6). Les commandes déjà closes ne reçoivent que la
// mise à jour de statut, sans nouvelle date de clôture.
func (r *UberRepository) SyncOrderState(ctx context.Context, uberOrderID, status, state, approval string, reasonID sql.NullInt64, issueSaleReceipt SaleReceiptFunc) error {
	// deletion_reason_id is varchar, not integer: pgx's stdlib driver can't
	// encode a sql.NullInt64 directly into a text-typed column ("cannot find
	// encode plan") — a pre-existing bug on Postgres found while adding the
	// cancelled_by_type guard above (this call was previously only ever
	// exercised with reasonID.Valid=false in tests, never a real value).
	// MySQL coerces the int implicitly, which is presumably why this went
	// unnoticed. Converted explicitly here rather than changing this
	// function's public signature.
	var reasonIDArg interface{}
	if reasonID.Valid {
		reasonIDArg = strconv.FormatInt(reasonID.Int64, 10)
	}

	if state != StateClosed {
		// Plus aucune réouverture par une plateforme (lot C conformité caisse,
		// R1 bis) : une commande close ne reçoit que le statut.
		db := dbx.GetDB(ctx, r.database)
		query := fmt.Sprintf(`
		UPDATE orders
		SET brand_status = ?, state = CASE WHEN state IN ('CLOSED', 'DONE') THEN state ELSE ? END, merchant_approval = ?, deletion_reason_id = ?,
		    cancelled_by_type = CASE WHEN cancelled_by_type IS NULL AND ? IN ('DENIED', 'CANCELED') THEN 'PLATFORM' ELSE cancelled_by_type END,
		    delivered_on = CASE WHEN ? = 'COMPLETED' THEN %[1]s ELSE delivered_on END,
		    last_update = %[1]s
		WHERE brand_order_id = ?`, dbx.UTCNow())
		_, err := db.ExecContext(ctx, query, status, state, approval, reasonIDArg, status, status, uberOrderID)
		return err
	}

	businessSet := `brand_status = ?, merchant_approval = ?, deletion_reason_id = ?,
		    cancelled_by_type = CASE WHEN cancelled_by_type IS NULL AND ? IN ('DENIED', 'CANCELED') THEN 'PLATFORM' ELSE cancelled_by_type END,
		    last_update = ` + dbx.UTCNow()
	businessArgs := []any{status, approval, reasonIDArg, status}
	sale := status == StatusCompleted || status == StatusEnRoute

	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		if _, err := db.ExecContext(txCtx, `UPDATE orders SET `+businessSet+` WHERE brand_order_id = ? AND state <> 'OPEN'`,
			append(append([]any{}, businessArgs...), uberOrderID)...); err != nil {
			return err
		}
		open, err := fiscal.LockOpenOrdersByBrandOrderID(txCtx, db, uberOrderID)
		if err != nil {
			return err
		}
		for _, o := range open {
			if err := r.closePlatformOrder(txCtx, db, o, sale, businessSet, businessArgs, issueSaleReceipt, voidPaymentReason(status)); err != nil {
				return err
			}
		}
		_, err = db.ExecContext(txCtx, fmt.Sprintf(`
			UPDATE orderitems
			SET distributed_on = %s,
			    isDistributed = '1'
			FROM orders o
			WHERE o.order_id = orderitems.order_id AND o.brand_order_id = ?`, dbx.UTCNow()), uberOrderID)
		return err
	})
}

// HandleOrderNotFound gère le cas 404 de l'API Uber (finishOrderIfDoesNotExist)
//
// cancelled_by_type : même garde `IS NULL` que SyncOrderState (voir son
// commentaire) — n'attribue PLATFORM que si rien n'a déjà classé cette
// annulation (pas d'écrasement d'un DenyOrderLocal/DeleteOrderLocal
// STAFF déjà exécuté avant l'appel API qui a mené ici). Seule la branche
// CANCELED (READY_FOR_HANDOFF -> CLOSED n'est pas une annulation) compte.
//
// Conformité caisse (C10) : une commande ouverte est clôturée (date de
// clôture) ; READY_FOR_HANDOFF est une vente (ticket, reste ouverte si pas
// entièrement payée), le reste une annulation. Les commandes déjà closes ne
// reçoivent que la mise à jour de statut.
func (r *UberRepository) HandleOrderNotFound(ctx context.Context, uberOrderID string, issueSaleReceipt SaleReceiptFunc) error {
	businessSet := `brand_status = CASE WHEN brand_status = 'READY_FOR_HANDOFF' THEN 'CLOSED' ELSE 'CANCELED' END,
		    cancelled_by_type = CASE WHEN cancelled_by_type IS NULL AND brand_status <> 'READY_FOR_HANDOFF' THEN 'PLATFORM' ELSE cancelled_by_type END,
		    last_update = ` + dbx.UTCNow()

	return dbutils.RunInTx(ctx, r.database, func(txCtx context.Context) error {
		db := dbx.GetDB(txCtx, r.database)
		if _, err := db.ExecContext(txCtx, `UPDATE orders SET `+businessSet+` WHERE brand_order_id = ? AND state <> 'OPEN'`, uberOrderID); err != nil {
			return err
		}
		open, err := fiscal.LockOpenOrdersByBrandOrderID(txCtx, db, uberOrderID)
		if err != nil {
			return err
		}
		for _, o := range open {
			sale := o.BrandStatus == "READY_FOR_HANDOFF"
			if err := r.closePlatformOrder(txCtx, db, o, sale, businessSet, nil, issueSaleReceipt, "Commande introuvable chez Uber Eats"); err != nil {
				return err
			}
		}
		return nil
	})
}

// closePlatformOrder clôture une commande plateforme encore ouverte (verrouillée
// par fiscal.LockOpenOrdersByBrandOrderID) : mêmes colonnes métier que le
// chemin appelant (businessSet/businessArgs), plus état et date de clôture.
// Une vente doit être entièrement payée (sinon la commande reste ouverte,
// l'erreur est journalisée) et reçoit son ticket.
//
// Une commande qui n'est pas une vente (annulée, refusée, livraison échouée)
// n'est pas payée par Uber : ses paiements actifs sont annulés dans la même
// transaction (fiscal.CloseVoidPlatformOrder, source UBER_EATS, motif
// voidReason) et une commande rouverte après une vente reçoit l'avoir de
// cette vente, comme l'annulation par webhook (webhook/ubereats CancelOrder).
// Avant le lot 6 (docs/marketplace-fiabilite-import.md), ses paiements
// restaient actifs : encaissement sans vente dans la clôture journalière.
func (r *UberRepository) closePlatformOrder(ctx context.Context, db *dbx.DB, o fiscal.OpenPlatformOrder, sale bool, businessSet string, businessArgs []any, issueSaleReceipt SaleReceiptFunc, voidReason string) error {
	if !sale {
		receipts := receipt.NewReceiptService(receipt.NewReceiptRepository(r.database))
		return fiscal.CloseVoidPlatformOrder(ctx, r.database, fiscal.PaymentCancellation{
			MerchantID: o.MerchantID, OrderID: o.OrderID,
			Source: fiscal.CancelSourceUberEats, UserID: models.UberEatsWebhookUserID,
			Reason: voidReason,
		}, func(ctx context.Context, hasSaleReceipt bool) error {
			if hasSaleReceipt {
				if err := receipts.CancelSaleReceipt(ctx, o.MerchantID, o.OrderID); err != nil {
					return err
				}
			}
			args := append(append(append([]any{}, businessArgs...), fiscal.ClosureArgs()...), o.OrderID)
			_, err := dbx.GetDB(ctx, r.database).ExecContext(ctx, `UPDATE orders SET `+businessSet+`, `+fiscal.ClosureColumns+` WHERE order_id = ?`, args...)
			return err
		})
	}
	paid, err := fiscal.OrderFullyPaid(ctx, db, o.OrderID)
	if err != nil {
		return err
	}
	if !paid {
		logger.FromContext(ctx).Error(fmt.Sprintf("ubereats: platform sale %s not fully paid, left open", o.OrderID))
		return nil
	}
	if issueSaleReceipt == nil {
		return errors.New("ubereats: sale receipt issuer not configured")
	}
	args := append(append(append([]any{}, businessArgs...), fiscal.ClosureArgs()...), o.OrderID)
	if _, err := db.ExecContext(ctx, `UPDATE orders SET `+businessSet+`, `+fiscal.ClosureColumns+` WHERE order_id = ?`, args...); err != nil {
		return err
	}
	return issueSaleReceipt(ctx, o.MerchantID, o.OrderID)
}

// voidPaymentReason est le motif, inscrit au journal d'audit, de l'annulation
// des paiements d'une commande qu'Uber ne paie pas.
func voidPaymentReason(status string) string {
	switch status {
	case StatusDeliveryFailed:
		return "Livraison Uber Eats échouée"
	case StatusDenied:
		return "Refus Uber Eats"
	default:
		return "Annulation Uber Eats"
	}
}

// UpdateBusyModeData met à jour les données de "Busy Mode"
func (r *UberRepository) UpdateBusyModeData(ctx context.Context, storeID string, delayUntil time.Time, duration int) error {
	db := dbx.GetDB(ctx, r.database)

	query := `
		UPDATE integration_uber_eats
		SET delay_until = ?, delay_duration = ?
		WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, delayUntil, duration, storeID)
	return err
}

// UpdatePreparationTime met à jour le temps de préparation estimé. storeID
// vide s'applique à tous les comptes du marchand (comportement historique,
// correct pour un marchand mono-compte) ; storeID renseigné ne cible que ce
// compte précis.
func (r *UberRepository) UpdatePreparationTime(ctx context.Context, merchantID, storeID string, timeVal int, isAuto bool) error {
	db := dbx.GetDB(ctx, r.database)
	// Logique PHP :
	// estimated_preparation_time = case when :auto = 'TRUE' then estimated_preparation_time else :time end
	// last_estimated_preparation_time = :time

	// estimated_preparation_time / last_estimated_preparation_time sont des
	// colonnes varchar : pgx refuse d'y lier un int Go (MySQL coercait) —
	// meme correctif que le module integrations (Tier 2).
	timeStr := strconv.Itoa(timeVal)
	query := `
		UPDATE integration_uber_eats
		SET estimated_preparation_time = CASE WHEN ? = true THEN estimated_preparation_time ELSE ? END,
		    last_estimated_preparation_time = ?
		WHERE merchant_id = ?`
	args := []interface{}{isAuto, timeStr, timeStr, merchantID}
	if storeID != "" {
		query += ` AND store_id = ?`
		args = append(args, storeID)
	}

	_, err := db.ExecContext(ctx, query, args...)
	return err
}

// UpdateStoreClosure met à jour la date de fermeture temporaire
func (r *UberRepository) UpdateStoreClosure(ctx context.Context, storeID string, closedUntil time.Time) error {
	db := dbx.GetDB(ctx, r.database)

	query := `UPDATE integration_uber_eats SET closed_until = ? WHERE store_id = ?`
	_, err := db.ExecContext(ctx, query, closedUntil, storeID)
	return err
}

// GetStoreInfoForMenu récupère ID, BasicAuth et Timezone (utilisé pour toggle item).
// storeID vide retombe sur le compte "principal" du marchand (ORDER BY
// store_id, comportement historique identique pour un marchand mono-compte) ;
// storeID renseigné cible ce compte précis.
func (r *UberRepository) GetStoreInfoForMenu(ctx context.Context, merchantID, storeID string) (*Store, error) {
	db := dbx.GetDB(ctx, r.database)

	// Similaire à GetStoreData mais s'assure que le token n'est pas null
	query := fmt.Sprintf(`
		SELECT iue.merchant_id, iue.store_id, m.timezone, iue.bearer_token
		FROM integration_uber_eats iue
		INNER JOIN merchant m on %s = iue.merchant_id
		WHERE iue.merchant_id = ? AND iue.bearer_token IS NOT NULL`, ueMerchantJoinCast())
	args := []interface{}{merchantID}
	if storeID != "" {
		query += ` AND iue.store_id = ?`
		args = append(args, storeID)
	} else {
		query += ` ORDER BY iue.store_id LIMIT 1`
	}

	var store Store
	// On ignore les champs non demandés dans le Scan
	err := db.QueryRowContext(ctx, query, args...).Scan(&store.MerchantID, &store.StoreID, &store.Timezone, &store.BearerToken)
	return &store, err
}
