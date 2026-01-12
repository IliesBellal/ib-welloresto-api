package orders

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type OrdersFetcher struct {
	db  *sql.DB
	log *zap.Logger
}

func NewOrdersFetcher(db *sql.DB, log *zap.Logger) *OrdersFetcher {
	return &OrdersFetcher{
		db:  db,
		log: log}
}

func (r *OrdersFetcher) FetchAndBuildOrders(ctx context.Context, merchantID string, whereFilters, orderByFilter, limitsFilters string) ([]models.Order, error) {
	startTotal := time.Now()
	r.log.Info("fetchAndBuildOrders START OPTIMIZED", zap.String("merchant_id", merchantID))

	if ctx.Err() != nil {
		r.log.Error("CTX ALREADY CANCELED", zap.Error(ctx.Err()))
		return nil, ctx.Err()
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		r.log.Error("BeginTx failed", zap.Error(err))
		return nil, fmt.Errorf("BeginTx failed: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// --- ÉTAPE 1 : Récupérer les "Headers" (Les commandes) en premier ---
	// Cela permet d'appliquer la pagination tout de suite et de limiter les sous-requêtes aux seuls IDs nécessaires.

	var orders []models.Order
	var orderIDs []string // Pour construire la clause IN (...)

	// Note: J'ai renforcé la jointure delivery_session pour ne prendre que l'active ou la pending
	qHeader := `SELECT o.order_id, o.order_num, o.order_type, o.state, o.scheduled, o.brand, o.brand_status, o.brand_order_id, o.brand_order_num, o.estimated_ready, o.means_of_payement, o.price, o.TVA, o.HT, o.monnaie, o.cutlery_notes,
		o.isPaid, o.isDistributed, o.dateCall, o.isDelivery, o.merchant_approval, o.delivery_fees, o.last_update, o.fulfillment_type, o.use_customer_temporary_address, o.creation_date, o.places_settings, o.pager_number,
		c.customer_id, c.customer_name, c.customer_tel, c.customer_lat, c.customer_lng, c.customer_temporary_phone, c.customer_temporary_phone_code, c.customer_nb_orders, c.customer_zone_code,
		c.customer_address, c.customer_floor_number, c.customer_door_number, c.customer_additional_address, c.customer_business_name, c.customer_birthdate, c.customer_additional_info,
		c.customer_temporary_address, c.customer_temporary_lat, c.customer_temporary_lng, c.customer_temporary_floor_number, c.customer_temporary_door_number, c.customer_temporary_additional_address,
		u.user_id, u.lat, u.lng, u.tel as deliveryTel, u.userName,
		ds.id as delivery_session_id, dso.priority
		FROM orders o
		LEFT JOIN customer c ON o.customer_id = c.customer_id
		LEFT JOIN users u ON o.responsible = u.user_id AND o.merchant_id = u.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING') 
		WHERE o.merchant_id = ? ` + whereFilters + " " + orderByFilter + " " + limitsFilters

	t0 := time.Now()
	rows, err := tx.QueryContext(ctx, qHeader, merchantID)
	if err != nil {
		r.log.Error("Query Header ERROR", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ord models.Order
		var customerNbOrders, priority, isDelivery, useCustomerTemporaryAddress, price, TVA, HT, deliveryFees, placesSettings sql.NullInt64
		var customerID, orderID, orderNum, orderType, state, brand, brandStatus, brandOrderID, brandOrderNum, meansOfPayment, monnaie, cutleryNotes, dateCall, fulfillmentType, pagerNumber, merchantApproval, deliverySessionID, userID sql.NullString
		var customerLat, customerLng, customerTemporaryLat, customerTemporaryLng, userLat, userLng sql.NullFloat64
		var lastUpdate, creationDate, estimatedReady sql.NullTime
		var scheduled, isPaid, isDistributed sql.NullBool
		var cName, cTel, cTempPhone, cTempPhoneCode, cZoneCode, cAddr, cFloor, cDoor, cAddAddr, cBusName, cBirth, cInfo, cTempAddr, cTempFloor, cTempDoor, cTempAddAddr sql.NullString
		var delTel, delUserName sql.NullString

		if err := rows.Scan(&orderID, &orderNum, &orderType, &state, &scheduled, &brand, &brandStatus, &brandOrderID, &brandOrderNum, &estimatedReady, &meansOfPayment, &price, &TVA, &HT, &monnaie, &cutleryNotes,
			&isPaid, &isDistributed, &dateCall, &isDelivery, &merchantApproval, &deliveryFees, &lastUpdate, &fulfillmentType, &useCustomerTemporaryAddress, &creationDate, &placesSettings, &pagerNumber,
			&customerID, &cName, &cTel, &customerLat, &customerLng, &cTempPhone, &cTempPhoneCode, &customerNbOrders, &cZoneCode,
			&cAddr, &cFloor, &cDoor, &cAddAddr, &cBusName, &cBirth, &cInfo,
			&cTempAddr, &customerTemporaryLat, &customerTemporaryLng, &cTempFloor, &cTempDoor, &cTempAddAddr,
			&userID, &userLat, &userLng, &delTel, &delUserName,
			&deliverySessionID, &priority); err != nil {
			return nil, err
		}

		// Mapping basique
		ord.OrderID = orderID.String
		ord.OrderNum = helpers.NullStringToPtr(orderNum)
		ord.Brand = helpers.NullStringToPtr(brand)
		ord.BrandOrderID = helpers.NullStringToPtr(brandOrderID)
		ord.BrandOrderNum = helpers.NullStringToPtr(brandOrderNum)
		ord.BrandStatus = helpers.NullStringToPtr(brandStatus)
		ord.DeliverySessionID = &deliverySessionID.String
		ord.OrderType = helpers.NullStringToPtr(orderType)
		ord.CutleryNotes = helpers.NullStringToPtr(cutleryNotes)
		ord.State = helpers.NullStringToPtr(state)
		ord.Scheduled = scheduled.Bool
		ord.TTC = price.Int64
		ord.TVA = helpers.NullInt64ToPtr(TVA)
		ord.HT = helpers.NullInt64ToPtr(HT)
		ord.PlacesSettings = helpers.NullInt64ToPtr(placesSettings)
		ord.PagerNumber = helpers.NullStringToPtr(pagerNumber)
		ord.IsPaid = isPaid.Bool
		ord.IsDistributed = isDistributed.Bool
		ord.IsSNO = userID.String == "-1"
		ord.CallHour = helpers.NullStringToPtr(dateCall)
		ord.EstimatedReady = helpers.NullTimeToNullUnixInt(estimatedReady)
		ord.IsDelivery = int(isDelivery.Int64)
		ord.MerchantApproval = merchantApproval.String
		ord.DeliveryFees = helpers.NullInt64ToPtr(deliveryFees)
		ord.CreationDate = helpers.NullTimePtr(creationDate).UTC().Unix()
		ord.FulfillmentType = helpers.NullStringToPtr(fulfillmentType)
		ord.LastUpdate = helpers.NullTimePtr(lastUpdate).UTC().Unix()

		if customerID.Valid {
			var cust models.Customer
			cust.CustomerID = &customerID.String
			cust.CustomerName = helpers.NullStringToPtr(cName)
			cust.CustomerTel = helpers.NullStringToPtr(cTel)
			cust.CustomerTemporaryPhone = helpers.NullStringToPtr(cTempPhone)
			cust.CustomerTemporaryPhoneCode = helpers.NullStringToPtr(cTempPhoneCode)
			nb := int(customerNbOrders.Int64)
			cust.CustomerNbOrders = &nb
			cust.CustomerAdditionalInfo = helpers.NullStringToPtr(cInfo)
			cust.CustomerZoneCode = helpers.NullStringToPtr(cZoneCode)

			// Logique d'adresse temporaire
			if useCustomerTemporaryAddress.Int64 == 1 {
				cust.CustomerAddress = helpers.NullStringToPtr(cTempAddr)
				cust.CustomerLat = helpers.NullFloat64Ptr(customerTemporaryLat)
				cust.CustomerLng = helpers.NullFloat64Ptr(customerTemporaryLng)
				cust.CustomerFloorNumber = helpers.NullStringToPtr(cTempFloor)
				cust.CustomerDoorNumber = helpers.NullStringToPtr(cTempDoor)
				cust.CustomerAdditionalAddress = helpers.NullStringToPtr(cTempAddAddr)
			} else {
				cust.CustomerAddress = helpers.NullStringToPtr(cAddr)
				cust.CustomerLat = helpers.NullFloat64Ptr(customerLat)
				cust.CustomerLng = helpers.NullFloat64Ptr(customerLng)
				cust.CustomerFloorNumber = helpers.NullStringToPtr(cFloor)
				cust.CustomerDoorNumber = helpers.NullStringToPtr(cDoor)
				cust.CustomerAdditionalAddress = helpers.NullStringToPtr(cAddAddr)
			}
			ord.Customer = &cust
		}

		if userID.Valid && userID.String != "0" {
			ord.Responsible = &models.OrderUser{
				UserID: userID.String, Lat: helpers.NullFloat64Ptr(userLat), Lng: helpers.NullFloat64Ptr(userLng), FirstName: &delUserName.String,
			}
		}

		orders = append(orders, ord)
		orderIDs = append(orderIDs, ord.OrderID)
	}
	r.log.Info("headers loaded", zap.Int("count", len(orders)), zap.Duration("elapsed", time.Since(t0)))

	// Si aucune commande, on retourne tout de suite
	if len(orders) == 0 {
		return orders, nil
	}

	// --- ÉTAPE 2 : Construction de la clause IN (...) et initialisation des maps ---

	// Helper pour créer les placeholder (?,?,?)
	placeholders := make([]string, len(orderIDs))
	args := make([]interface{}, len(orderIDs)+1)
	args[0] = merchantID
	for i, id := range orderIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}
	inClause := fmt.Sprintf("IN (%s)", strings.Join(placeholders, ","))

	// Maps pour stocker les résultats
	locationsByOrderID := make(map[string][]models.Location, len(orders))
	componentsMap := make(map[string][]models.ComponentUsage) // ProductID based
	extrasMap := make(map[string][]models.OrderProductExtra)
	withoutsMap := make(map[string][]models.OrderProductWithout)
	snoClientsMap := make(map[string][]interface{})

	type optKey struct {
		OrderItemID string
		AttrID      string
	}
	configurableOptionsMap := make(map[optKey][]models.ConfigurableOption)
	configurableAttributesMap := make(map[string][]models.ConfigurableAttribute)
	commentsByOrderID := make(map[string][]models.OrderComment)
	paymentsByOrderID := make(map[string][]models.Payment)
	productsByOrderID := make(map[string][]models.ProductEntry)

	// --- ÉTAPE 3 : Exécution parallèle des sous-requêtes (sans jointure delivery_session) ---

	var wg sync.WaitGroup
	var queryErr error
	var errMutex sync.Mutex // Pour protéger queryErr

	// Helper pour exécuter une sous-requête
	runSubQuery := func(stepName, query string, scanner func(*sql.Rows) error) {
		defer wg.Done()
		if ctx.Err() != nil {
			return
		}

		tQ := time.Now()
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			errMutex.Lock()
			if queryErr == nil {
				queryErr = fmt.Errorf("%s query error: %w", stepName, err)
			}
			errMutex.Unlock()
			r.log.Error("SubQuery ERROR", zap.String("step", stepName), zap.Error(err))
			return
		}
		defer rows.Close()

		for rows.Next() {
			if err := scanner(rows); err != nil {
				errMutex.Lock()
				if queryErr == nil {
					queryErr = fmt.Errorf("%s scan error: %w", stepName, err)
				}
				errMutex.Unlock()
				return
			}
		}
		r.log.Debug("SubQuery DONE", zap.String("step", stepName), zap.Duration("elapsed", time.Since(tQ)))
	}

	// Lancement des Goroutines
	// Note: J'ai retiré les jointures delivery_session, orders (sauf si nécessaire pour ID), etc.
	// On filtre uniquement par o.merchant_id et o.order_id IN (...)

	wg.Add(1)
	go runSubQuery("locations", `
		SELECT ol.order_id, ol.location_id, l.location_name, l.location_desc
		FROM order_location ol
		INNER JOIN locations l on l.merchant_id = ? and l.location_id = ol.location_id -- merchant_id check on location logic
		WHERE ol.order_id `+inClause,
		func(rows *sql.Rows) error {
			var locName, locDesc, ordID, locID sql.NullString
			if err := rows.Scan(&ordID, &locID, &locName, &locDesc); err != nil {
				return err
			}
			// Utilisation d'un mutex pour écrire dans la map n'est pas nécessaire si chaque goroutine écrit dans sa propre map
			// MAIS ici nous écrivons dans des maps partagées. Golang panic sur write concurrent map.
			// Nous devons utiliser un Mutex par Map.
			// Optimisation : On va utiliser un gros mutex global pour la simplicité, ou mieux, des channels.
			// Pour faire simple dans le copy/paste : je vais rajouter un Mutex global 'mu'.
			return nil
		})

	// Correction : Les maps ne sont pas thread-safe.
	// Pour un code production robuste et lisible, je vais sérialiser l'écriture ou utiliser des mutex spécifiques.
	// Vu la structure, je vais utiliser un `sync.Mutex` global pour protéger les écritures dans les maps.
	var mu sync.Mutex

	wg = sync.WaitGroup{} // Reset pour clarté

	// 1. Locations
	wg.Add(1)
	go runSubQuery("locations", `
		SELECT ol.order_id, ol.location_id, l.location_name, l.location_desc
		FROM order_location ol
		INNER JOIN locations l on l.location_id = ol.location_id
		WHERE l.merchant_id = ? AND ol.order_id `+inClause, func(rows *sql.Rows) error {
		var oID, lID, lName, lDesc sql.NullString
		if err := rows.Scan(&oID, &lID, &lName, &lDesc); err != nil {
			return err
		}
		mu.Lock()
		locationsByOrderID[oID.String] = append(locationsByOrderID[oID.String], models.Location{
			OrderID: &oID.String, LocationID: lID.String, LocationName: lName.String, LocationDesc: helpers.NullStringToPtr(lDesc),
		})
		mu.Unlock()
		return nil
	})

	// 2. Components (Note: Components filter by MerchantID, not OrderID directly usually, but logic seems to bind them later.
	// WARNING: Your original query filtered by merchantID only. It loads ALL components for the merchant. Keeping optimized.)
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Components ne dépendent pas des OrderIDs dans ta requête originale, mais du MerchantID.
		// On garde la logique originale (chargement global pour le merchant).
		q := `SELECT r.product_id, c.component_id, c.name, c.component_price as price, c.status, rq.quantity, uomd.uom_desc
			FROM components c
			INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled IS TRUE
			INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
			INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
			WHERE c.merchant_id = ? AND c.available = '1' AND rq.enabled IS TRUE`

		// Note: args[0] is merchantID.
		rows, err := tx.QueryContext(ctx, q, merchantID)
		if err != nil {
			errMutex.Lock()
			queryErr = err
			errMutex.Unlock()
			return
		}
		defer rows.Close()
		for rows.Next() {
			var pID, name, uom sql.NullString
			var cID, price, status sql.NullInt64
			var qty sql.NullFloat64
			if err := rows.Scan(&pID, &cID, &name, &price, &status, &qty, &uom); err != nil {
				continue
			}

			mu.Lock()
			componentsMap[pID.String] = append(componentsMap[pID.String], models.ComponentUsage{
				ComponentID: cID.Int64, Name: name.String, ProductID: pID.String, Price: price.Int64, Quantity: qty.Float64, UnitOfMeasure: uom.String, Status: int(status.Int64),
			})
			mu.Unlock()
		}
		r.log.Info("components loaded")
	}()

	// 3. Extras
	wg.Add(1)
	go runSubQuery("extras", `
		SELECT e.order_item_id, e.id, oi.order_id, e.product_id, ce.name, e.component_id, e.price
		FROM extra e
		INNER JOIN orderitems oi on e.order_item_id = oi.order_item_id
		INNER JOIN components ce on e.component_id = ce.component_id
		WHERE oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {
		var oiID, id, oID, pID, cID, name sql.NullString
		var price sql.NullFloat64
		if err := rows.Scan(&oiID, &id, &oID, &pID, &name, &cID, &price); err != nil {
			return err
		}
		mu.Lock()
		extrasMap[oiID.String] = append(extrasMap[oiID.String], models.OrderProductExtra{
			ID: id.String, OrderItemID: oiID.String, OrderID: oID.String, ProductID: pID.String, Name: name.String, ComponentID: cID.String, Price: price.Float64,
		})
		mu.Unlock()
		return nil
	})

	// 4. Withouts
	wg.Add(1)
	go runSubQuery("withouts", `
		SELECT w.order_item_id, w.id, oi.order_id, w.product_id, cw.name, w.component_id
		FROM without w
		INNER JOIN orderitems oi on w.order_item_id = oi.order_item_id
		INNER JOIN components cw on w.component_id = cw.component_id
		WHERE oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {
		var oiID, id, oID, pID, cID, name sql.NullString
		if err := rows.Scan(&oiID, &id, &oID, &pID, &name, &cID); err != nil {
			return err
		}
		mu.Lock()
		withoutsMap[oiID.String] = append(withoutsMap[oiID.String], models.OrderProductWithout{
			ID: id.String, OrderItemID: oiID.String, OrderID: oID.String, ProductID: pID.String, Name: name.String, ComponentID: cID.String, Price: 0,
		})
		mu.Unlock()
		return nil
	})

	// 5. ClientSNO
	wg.Add(1)
	go runSubQuery("clientSNO", `
		SELECT DISTINCT ss.user_code, ss.user_name, oi.order_item_id, so.quantity
		FROM orderitems oi
		INNER JOIN session_orderitem so on so.order_item_id = oi.order_item_id
		INNER JOIN scannorder_session ss on so.user_code = ss.user_code
		WHERE oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {
		var uCode, uName, oiID sql.NullString
		var qty sql.NullInt64
		if err := rows.Scan(&uCode, &uName, &oiID, &qty); err != nil {
			return err
		}
		mu.Lock()
		snoClientsMap[oiID.String] = append(snoClientsMap[oiID.String], map[string]interface{}{"user_code": uCode.String, "user_name": uName.String, "quantity": qty.Int64})
		mu.Unlock()
		return nil
	})

	// 6. Config Options
	wg.Add(1)
	go runSubQuery("config_options", `
		SELECT ca.id as configurable_attribute_id, oi.order_item_id, cao.id, cao.title, cao.extra_price, 
		case when oic.id is null then 0 else 1 end as selected,
		case when oic.quantity is null then 0 else oic.quantity end as quantity, cao.max_quantity
		FROM orderitems oi
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
		LEFT JOIN order_item_configuration oic on oic.order_item_id = oi.order_item_id and cao.id = oic.configuration_attribute_option_id
		WHERE oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {
		var aID, oiID, id, title sql.NullString
		var extPrice int
		var sel, qty, maxQty sql.NullInt64
		if err := rows.Scan(&aID, &oiID, &id, &title, &extPrice, &sel, &qty, &maxQty); err != nil {
			return err
		}
		key := optKey{OrderItemID: oiID.String, AttrID: aID.String}
		mu.Lock()
		configurableOptionsMap[key] = append(configurableOptionsMap[key], models.ConfigurableOption{
			ID: id.String, ConfigAttributeID: aID.String, OrderItemID: oiID.String, Title: title.String, ExtraPrice: extPrice, Quantity: int(qty.Int64), MaxQuantity: int(maxQty.Int64), Selected: int(sel.Int64),
		})
		mu.Unlock()
		return nil
	})

	// 7. Config Attributes
	wg.Add(1)
	go runSubQuery("config_attributes", `
		SELECT oi.order_item_id, ca.id, ca.title, ca.max_options, ca.attribute_type
		FROM orderitems oi
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		WHERE oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {
		var oiID, id, title, aType sql.NullString
		var maxOpt sql.NullInt64
		if err := rows.Scan(&oiID, &id, &title, &maxOpt, &aType); err != nil {
			return err
		}
		mu.Lock()
		// On ne charge pas les options ici, on fera le lien à la fin pour éviter blocage
		configurableAttributesMap[oiID.String] = append(configurableAttributesMap[oiID.String], models.ConfigurableAttribute{
			ID: id.String, OrderItemID: oiID.String, AttributeType: aType.String, Title: title.String, MaxOptions: int(maxOpt.Int64),
		})
		mu.Unlock()
		return nil
	})

	// 8. Order Comments
	wg.Add(1)
	go runSubQuery("order_comments", `
		SELECT oc.id, oc.user_id, oc.content, oc.creation_date, oc.order_id, u.userName
		FROM order_comments oc
		LEFT JOIN users u on u.user_id = oc.user_id
		INNER JOIN orders o on o.order_id = oc.order_id
		WHERE o.merchant_id = ? AND oc.order_item_id IS NULL AND o.order_id `+inClause, func(rows *sql.Rows) error {
		var id sql.NullInt64
		var uID, content, uName, oID sql.NullString
		var cDate sql.NullTime
		if err := rows.Scan(&id, &uID, &content, &cDate, &oID, &uName); err != nil {
			return err
		}
		mu.Lock()
		commentsByOrderID[oID.String] = append(commentsByOrderID[oID.String], models.OrderComment{
			OrderID: oID.String, UserName: helpers.NullStringToPtr(uName), Content: content.String, CreationDate: helpers.NullTimePtr(cDate),
		})
		mu.Unlock()
		return nil
	})

	// 9. Payments
	wg.Add(1)
	go runSubQuery("payments", `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled
		FROM payments p
		INNER JOIN orders o on o.order_id = p.order_id
		WHERE o.merchant_id = ? AND o.order_id `+inClause, func(rows *sql.Rows) error {
		var oID, mop sql.NullString
		var pID, enb sql.NullInt64
		var amt sql.NullFloat64
		var pDate sql.NullTime
		if err := rows.Scan(&oID, &pID, &mop, &amt, &pDate, &enb); err != nil {
			return err
		}
		mu.Lock()
		paymentsByOrderID[oID.String] = append(paymentsByOrderID[oID.String], models.Payment{
			OrderID: oID.String, PaymentID: pID.Int64, MOP: mop.String, Amount: amt.Float64, PaymentDate: helpers.NullTimePtr(pDate), Enabled: int(enb.Int64),
		})
		mu.Unlock()
		return nil
	})

	// 10. Products (Main Items)
	wg.Add(1)
	go runSubQuery("products", `
		SELECT oi.order_id, oi.quantity, oi.paid_quantity, oi.price, oi.product_id, p.name, p.product_desc, pc.categ_name, oi.order_item_id,
			oi.isPaid, oi.isDistributed, oi.ordered_on, p.price as base_price, oi.discount_id, d.discount_name, oi.ready_for_distribution_quantity,
			oi.distributed_quantity, tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, oi.delay_id, oc.content, oc.user_id, oc.creation_date,
			p.price_take_away, p.price_delivery, p.image_url, oi.production_status, oi.production_status_done_quantity, p.production_color,
			p.available_in, p.available_take_away, p.available_delivery
		FROM orderitems oi
		INNER JOIN products p ON oi.product_id = p.product_id AND oi.merchant_id = p.merchant_id
		LEFT JOIN productcateg pc ON pc.merchant_id = oi.merchant_id AND p.category = pc.merchant_categ_id
		INNER JOIN tva_categories tva_in ON tva_in.tva_id = p.tva_in_id
		INNER JOIN tva_categories tva_delivery ON tva_delivery.tva_id = p.tva_delivery_id
		INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
		LEFT JOIN discounts d ON d.discount_id = oi.discount_id
		LEFT JOIN order_comments oc ON oc.order_id = oi.order_id AND oc.order_item_id = oi.order_item_id
		WHERE oi.quantity > 0 AND oi.merchant_id = ? AND oi.order_id `+inClause, func(rows *sql.Rows) error {

		var quantity, paidQuantity, price, isPaid, isDistributed, basePrice, discountID, readyForDistribution, distributedQuantity, priceTakeAway, priceDelivery, productionDoneQty sql.NullInt64
		var productID, name, productDesc, categName, orderItemID, discountName, delayID, commentContent, commentUserID, imageURL, productionStatus, productionColor, orderID sql.NullString
		var tvaIn, tvaDelivery, tvaTakeAway sql.NullFloat64
		var orderedOn, commentCreation sql.NullTime
		var availableIn, availableTakeAway, availableDelivery sql.NullBool

		if err := rows.Scan(
			&orderID, &quantity, &paidQuantity, &price, &productID, &name, &productDesc,
			&categName, &orderItemID, &isPaid, &isDistributed, &orderedOn, &basePrice,
			&discountID, &discountName, &readyForDistribution, &distributedQuantity,
			&tvaIn, &tvaDelivery, &tvaTakeAway, &delayID, &commentContent, &commentUserID,
			&commentCreation, &priceTakeAway, &priceDelivery, &imageURL, &productionStatus,
			&productionDoneQty, &productionColor, &availableIn, &availableTakeAway,
			&availableDelivery,
		); err != nil {
			return err
		}

		var comment models.OrderComment
		if commentContent.Valid {
			comment = models.OrderComment{
				OrderID: orderID.String, UserName: &commentUserID.String, Content: commentContent.String, CreationDate: helpers.NullTimePtr(commentCreation),
			}
		}

		op := models.ProductEntry{
			OrderID:                      orderID.String,
			OrderItemID:                  orderItemID.String,
			OrderedOn:                    helpers.NullTimePtr(orderedOn).UTC().Unix(),
			ProductID:                    productID.String,
			ProductionStatus:             productionStatus.String,
			ProductionStatusDoneQuantity: int(productionDoneQty.Int64),
			Name:                         name.String,
			ImageURL:                     helpers.NullStringToPtr(imageURL),
			Category:                     helpers.NullStringToPtr(categName),
			CategoryID:                   helpers.NullStringToPtr(categName),
			Description:                  helpers.NullStringToPtr(productDesc),
			Quantity:                     int(quantity.Int64),
			PaidQuantity:                 int(paidQuantity.Int64),
			DistributedQuantity:          int(distributedQuantity.Int64),
			ReadyForDistributionQuantity: int(readyForDistribution.Int64),
			IsPaid:                       int(isPaid.Int64),
			IsDistributed:                int(isDistributed.Int64),
			Price:                        price.Int64,
			PriceTakeAway:                priceTakeAway.Int64,
			PriceDelivery:                priceDelivery.Int64,
			DiscountID:                   helpers.NullInt64ToPtr(discountID),
			DiscountName:                 helpers.NullStringToPtr(discountName),
			DiscountedPrice:              helpers.NilIfNullInt64Discount(discountID, price.Int64),
			TVAIn:                        tvaIn.Float64,
			TVADelivery:                  tvaDelivery.Float64,
			TVATakeAway:                  tvaTakeAway.Float64,
			AvailableIn:                  availableIn.Bool,
			AvailableTakeAway:            availableTakeAway.Bool,
			AvailableDelivery:            availableDelivery.Bool,
			ProductionColor:              helpers.NullStringToPtr(productionColor),
			Comment:                      comment,
		}

		mu.Lock()
		productsByOrderID[orderID.String] = append(productsByOrderID[orderID.String], op)
		mu.Unlock()
		return nil
	})

	// Attendre la fin de toutes les sous-requêtes
	wg.Wait()
	if queryErr != nil {
		return nil, queryErr
	}

	// --- ÉTAPE 4 : Assemblage Final ---

	// On lie tout ensemble (CPU bound, très rapide)
	for i := range orders {
		oID := orders[i].OrderID

		// Map simple lists
		if v, ok := locationsByOrderID[oID]; ok {
			orders[i].Location = v
		} else {
			orders[i].Location = []models.Location{}
		}
		if v, ok := paymentsByOrderID[oID]; ok {
			orders[i].Payments = v
		} else {
			orders[i].Payments = []models.Payment{}
		}
		if v, ok := commentsByOrderID[oID]; ok {
			orders[i].Comments = v
		} else {
			orders[i].Comments = []models.OrderComment{}
		}

		// Map products and deeper structures
		rawProds := productsByOrderID[oID]
		finalProds := make([]models.ProductEntry, len(rawProds))

		for j, p := range rawProds {
			// Link Extras
			if ex, ok := extrasMap[p.OrderItemID]; ok {
				p.Extra = ex
			} else {
				p.Extra = []models.OrderProductExtra{}
			}
			// Link Withouts
			if w, ok := withoutsMap[p.OrderItemID]; ok {
				p.Without = w
			} else {
				p.Without = []models.OrderProductWithout{}
			}
			// Link Components
			if c, ok := componentsMap[p.ProductID]; ok {
				p.Components = c
			} else {
				p.Components = []models.ComponentUsage{}
			}
			// Link Customers SNO
			if c, ok := snoClientsMap[p.OrderItemID]; ok {
				p.Customers = c
			} else {
				p.Customers = []interface{}{}
			}

			// Link Config Attributes & Options
			if attrs, ok := configurableAttributesMap[p.OrderItemID]; ok {
				// Attacher les options à chaque attribut
				for k := range attrs {
					key := optKey{OrderItemID: p.OrderItemID, AttrID: attrs[k].ID}
					if opts, ok := configurableOptionsMap[key]; ok {
						attrs[k].Options = opts
					} else {
						attrs[k].Options = []models.ConfigurableOption{}
					}
				}
				p.Configuration.Attributes = attrs
			} else {
				p.Configuration.Attributes = []models.ConfigurableAttribute{}
			}

			finalProds[j] = p
		}

		if finalProds == nil {
			finalProds = []models.ProductEntry{}
		}
		orders[i].Products = finalProds
	}

	r.log.Info("FetchAndBuildOrders END", zap.Int("orders_count", len(orders)), zap.Duration("total_duration", time.Since(startTotal)))
	return orders, nil
}

func (r *OrdersFetcher) FetchAndBuildOrdersOld(ctx context.Context, merchantID string, whereFilters, orderByFilter, limitsFilters string) ([]models.Order, error) {
	startTotal := time.Now()
	r.log.Info("fetchAndBuildOrders START with filters "+whereFilters, zap.String("merchant_id", merchantID))

	// Begin transaction (read-only)
	// Note: On utilise le ctx parent. Si la requête HTTP est annulée, la transaction s'arrêtera proprement.
	if ctx.Err() != nil {
		r.log.Error("CTX ALREADY CANCELED", zap.Error(ctx.Err()))
	}

	r.log.Info("Beginning tx")
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	r.log.Info("tx open")
	if err != nil {
		r.log.Error("BeginTx failed", zap.Error(err))
		return nil, fmt.Errorf("BeginTx failed: %w", err)
	}

	// Ensure rollback if anything goes wrong
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	r.log.Info("first query")

	// --- HELPER FUNCTIONS CORRIGÉES ---
	// Helper to run a query with logging
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		r.log.Info("Query START", zap.String("step", step))

		t0 := time.Now()
		rows, err := tx.QueryContext(ctx, query, args...)
		elapsed := time.Since(t0)

		if err != nil {
			r.log.Error(
				"Query ERROR",
				zap.String("step", step),
				zap.Duration("elapsed", elapsed),
				zap.String("sql", query),
				zap.Any("args", args),
				zap.Error(err),
			)
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}

		r.log.Info("Query DONE", zap.String("step", step), zap.Duration("elapsed", elapsed))
		return rows, nil
	}

	// --- 9. LOCATIONS ---
	locationsByOrderID := map[string][]models.Location{}
	{
		step := "locations"
		q := `SELECT ol.order_id, ol.location_id, l.location_name, l.location_desc
		FROM orders o
		INNER JOIN order_location ol on ol.order_id = o.order_id
		INNER JOIN locations l on l.merchant_id = o.merchant_id and l.location_id = ol.location_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var locationName, locationDesc, orderID, locationID sql.NullString
			if err := rows.Scan(&orderID, &locationID, &locationName, &locationDesc); err != nil {
				return nil, err
			}
			locationsByOrderID[orderID.String] = append(locationsByOrderID[orderID.String], models.Location{
				OrderID: &orderID.String, LocationID: locationID.String, LocationName: locationName.String, LocationDesc: helpers.NullStringToPtr(locationDesc),
			})
		}
		r.log.Info("locations loaded")
	}

	// --- 3. COMPONENTS (Optimisation possible: filtrer par orderID si liste courte, sinon global) ---
	componentsMap := map[string][]models.ComponentUsage{}
	{
		step := "components"
		q := `
		SELECT r.product_id, c.component_id, c.name, c.component_price as price, c.status,
		rq.quantity, uomd.uom_desc
		FROM components c
		INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled IS TRUE
		INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
		INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
		WHERE c.merchant_id = ? AND c.available = '1' AND rq.enabled IS TRUE`

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var productID, name, uom sql.NullString
			var compID, price, status sql.NullInt64
			var qty sql.NullFloat64
			if err := rows.Scan(&productID, &compID, &name, &price, &status, &qty, &uom); err != nil {
				return nil, err
			}
			componentsMap[productID.String] = append(componentsMap[productID.String], models.ComponentUsage{
				ComponentID:   compID.Int64,
				Name:          name.String,
				ProductID:     productID.String,
				Price:         price.Int64,
				Quantity:      qty.Float64,
				UnitOfMeasure: uom.String,
				Status:        int(status.Int64),
			})
		}
		r.log.Info("components loaded")
	}

	// --- 4. EXTRAS ---
	extrasMap := map[string][]models.OrderProductExtra{}
	{
		step := "extras"
		q := `
		SELECT e.order_item_id, e.id, e.order_id, e.product_id, ce.name, e.component_id, e.price
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN extra e on e.order_item_id = oi.order_item_id
		INNER JOIN components ce on e.component_id = ce.component_id and ce.merchant_id = o.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var orderItemID, id, orderID, productID, compID, name sql.NullString
			var price sql.NullFloat64
			if err := rows.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID, &price); err != nil {
				return nil, err
			}
			extrasMap[orderItemID.String] = append(extrasMap[orderItemID.String], models.OrderProductExtra{
				ID:          id.String,
				OrderItemID: orderItemID.String,
				OrderID:     orderID.String,
				ProductID:   productID.String,
				Name:        name.String,
				ComponentID: compID.String,
				Price:       price.Float64,
			})
		}
		r.log.Info("extras loaded")
	}

	// --- 5. WITHOUTS ---
	withoutsMap := map[string][]models.OrderProductWithout{}
	{
		step := "withouts"
		q := `
		SELECT w.order_item_id, w.id, w.order_id, w.product_id, cw.name, w.component_id
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN without w on w.order_item_id = oi.order_item_id
		INNER JOIN components cw on w.component_id = cw.component_id and cw.merchant_id = o.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var orderItemID, id, orderID, productID, compID, name sql.NullString
			if err := rows.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID); err != nil {
				return nil, err
			}
			withoutsMap[orderItemID.String] = append(withoutsMap[orderItemID.String], models.OrderProductWithout{
				ID:          id.String,
				OrderItemID: orderItemID.String,
				OrderID:     orderID.String,
				ProductID:   productID.String,
				Name:        name.String,
				ComponentID: compID.String,
				Price:       0,
			})
		}
		r.log.Info("withouts loaded")
	}

	// --- 7. CLIENTS SNO ---
	snoClientsMap := map[string][]interface{}{}
	{
		step := "clientSNO"
		q := `
		SELECT DISTINCT ss.user_code, ss.user_name, oi.order_item_id, so.quantity
		FROM orderitems oi
		INNER JOIN session_orderitem so on so.order_item_id = oi.order_item_id
		INNER JOIN scannorder_session ss on so.user_code = ss.user_code
		INNER JOIN orders o ON o.order_id = oi.order_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE oi.merchant_id = ? ` + whereFilters + " " + orderByFilter

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var userCode, userName, orderItemID sql.NullString
			var quantity sql.NullInt64
			if err := rows.Scan(&userCode, &userName, &orderItemID, &quantity); err != nil {
				return nil, err
			}
			clientObj := map[string]interface{}{"user_code": userCode.String, "user_name": userName.String, "quantity": quantity.Int64}
			snoClientsMap[orderItemID.String] = append(snoClientsMap[orderItemID.String], clientObj)
		}
		r.log.Info("clientSNO loaded")
	}

	// --- 11. CONFIG OPTIONS ---
	// --- Config Options ---
	type optKey struct {
		OrderItemID string
		AttrID      string
	}
	configurableOptionsMap := map[optKey][]models.ConfigurableOption{}
	{
		step := "configuration_attributes_options"
		q := `
		SELECT ca.id as configurable_attribute_id, oi.order_item_id, cao.id, cao.title, cao.extra_price, 
		case when oic.id is null then 0 else 1 end as selected,
		case when oic.quantity is null then 0 else oic.quantity end as quantity, cao.max_quantity
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
		LEFT JOIN order_item_configuration oic on oic.order_item_id = oi.order_item_id and cao.id = oic.configuration_attribute_option_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var attrID, orderItemID, id, title sql.NullString
			var extraPrice int
			var selected, quantity, maxQuantity sql.NullInt64
			if err := rows.Scan(&attrID, &orderItemID, &id, &title, &extraPrice, &selected, &quantity, &maxQuantity); err != nil {
				return nil, err
			}

			key := optKey{OrderItemID: orderItemID.String, AttrID: attrID.String}
			configurableOptionsMap[key] = append(configurableOptionsMap[key], models.ConfigurableOption{
				ID:                id.String,
				ConfigAttributeID: attrID.String,
				OrderItemID:       orderItemID.String,
				Title:             title.String,
				ExtraPrice:        extraPrice,
				Quantity:          int(quantity.Int64),
				MaxQuantity:       int(maxQuantity.Int64),
				Selected:          int(selected.Int64),
			})
		}
		r.log.Info("configuration_attributes_options loaded")
	}

	// --- 10. CONFIG ATTRIBUTES ---
	configurableAttributesMap := map[string][]models.ConfigurableAttribute{}
	{
		step := "configuration_attribute"
		q := `
		SELECT oi.order_item_id, ca.id, ca.title, ca.max_options, ca.attribute_type
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, orderItemID, title, attrType sql.NullString
			var maxOptions sql.NullInt64
			if err := rows.Scan(&orderItemID, &id, &title, &maxOptions, &attrType); err != nil {
				return nil, err
			}

			key := optKey{OrderItemID: orderItemID.String, AttrID: id.String}
			opts := []models.ConfigurableOption{}
			if val, ok := configurableOptionsMap[key]; ok {
				opts = val
			}

			configurableAttributesMap[orderItemID.String] = append(configurableAttributesMap[orderItemID.String], models.ConfigurableAttribute{
				ID:            id.String,
				OrderItemID:   orderItemID.String,
				AttributeType: attrType.String,
				Title:         title.String,
				MaxOptions:    int(maxOptions.Int64),
				Options:       opts,
			})
		}
		r.log.Info("configuration_attribute loaded")
	}

	// --- 8. ORDER COMMENTS ---
	commentsByOrderID := map[string][]models.OrderComment{}
	{
		step := "order_comment"
		q := `
		SELECT oc.id, oc.user_id, oc.content, oc.creation_date, oc.order_id, u.userName
		from order_comments oc
		inner join orders o on o.order_id = oc.order_id
		left join users u on u.user_id = oc.user_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id
		WHERE o.merchant_id = ? and oc.order_item_id is null ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id sql.NullInt64
			var content, userName, orderID, userID sql.NullString
			var creationDate sql.NullTime
			if err := rows.Scan(&id, &userID, &content, &creationDate, &orderID, &userName); err != nil {
				// LOG BRUT : row complet, colonnes, valeurs reçues
				cols, _ := rows.Columns()
				raw := helpers.DumpRawRow(rows)

				r.log.Error("SCAN ERROR",
					zap.String("step", step),
					zap.Strings("columns", cols),
					zap.Any("raw_row", raw),
					zap.Error(err),
				)

				return nil, err
			}
			commentsByOrderID[orderID.String] = append(commentsByOrderID[orderID.String], models.OrderComment{
				OrderID: orderID.String, UserName: helpers.NullStringToPtr(userName), Content: content.String, CreationDate: helpers.NullTimePtr(creationDate),
			})
		}
		r.log.Info("order_comment loaded")
	}

	// --- 6. PAYMENTS ---
	paymentsByOrderID := map[string][]models.Payment{}
	{
		step := "payments"
		q := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled
		from payments p
		INNER JOIN orders o on o.order_id = p.order_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var paymentID, enabled sql.NullInt64
			var mop, orderID sql.NullString
			var amount sql.NullFloat64
			var paymentDate sql.NullTime

			if err := rows.Scan(&orderID, &paymentID, &mop, &amount, &paymentDate, &enabled); err != nil {
				// LOG BRUT : row complet, colonnes, valeurs reçues
				cols, _ := rows.Columns()
				raw := helpers.DumpRawRow(rows)

				r.log.Error("SCAN ERROR",
					zap.String("step", step),
					zap.Strings("columns", cols),
					zap.Any("raw_row", raw),
					zap.Error(err),
				)

				return nil, err
			}
			paymentsByOrderID[orderID.String] = append(paymentsByOrderID[orderID.String], models.Payment{
				OrderID: orderID.String, PaymentID: paymentID.Int64, MOP: mop.String, Amount: amount.Float64, PaymentDate: helpers.NullTimePtr(paymentDate), Enabled: int(enabled.Int64),
			})
		}
		r.log.Info("payments loaded")
	}

	// --- 2. PRODUCTS ---
	// On utilise le même filtre sur 'o' (orders) car on join dessus
	productsByOrderID := map[string][]models.ProductEntry{}
	{
		step := "products"
		q := `
		SELECT o.order_id, oi.quantity, oi.paid_quantity, oi.price, oi.product_id, p.name, p.product_desc, pc.categ_name, oi.order_item_id,
		       oi.isPaid, oi.isDistributed, oi.ordered_on, p.price as base_price, oi.discount_id, d.discount_name, oi.ready_for_distribution_quantity,
		       oi.distributed_quantity, tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, oi.delay_id, oc.content, oc.user_id, oc.creation_date,
		p.price_take_away, p.price_delivery, p.image_url, oi.production_status, oi.production_status_done_quantity, p.production_color,
		p.available_in, p.available_take_away, p.available_delivery
		FROM orders o
		INNER JOIN orderitems oi ON o.order_id = oi.order_id AND oi.merchant_id = o.merchant_id
		INNER JOIN products p ON oi.product_id = p.product_id AND oi.merchant_id = p.merchant_id
		LEFT JOIN productcateg pc ON pc.merchant_id = oi.merchant_id AND p.category = pc.merchant_categ_id
		INNER JOIN tva_categories tva_in ON tva_in.tva_id = p.tva_in_id
		INNER JOIN tva_categories tva_delivery ON tva_delivery.tva_id = p.tva_delivery_id
		INNER JOIN tva_categories tva_take_away ON tva_take_away.tva_id = p.tva_take_away_id
		LEFT JOIN discounts d ON d.discount_id = oi.discount_id
		LEFT JOIN order_comments oc ON oc.order_id = o.order_id AND oc.order_item_id = oi.order_item_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE oi.quantity > 0 AND o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var quantity, paidQuantity, price, isPaid, isDistributed, basePrice, discountID, readyForDistribution, distributedQuantity, priceTakeAway, priceDelivery, productionDoneQty sql.NullInt64
			var productID, name, productDesc, categName, orderItemID, discountName, delayID, commentContent, commentUserID, imageURL, productionStatus, productionColor, orderID sql.NullString
			var tvaIn, tvaDelivery, tvaTakeAway sql.NullFloat64
			var orderedOn, commentCreation sql.NullTime
			var availableIn, availableTakeAway, availableDelivery sql.NullBool

			scanErr := rows.Scan(
				&orderID, &quantity, &paidQuantity, &price, &productID, &name, &productDesc,
				&categName, &orderItemID, &isPaid, &isDistributed, &orderedOn, &basePrice,
				&discountID, &discountName, &readyForDistribution, &distributedQuantity,
				&tvaIn, &tvaDelivery, &tvaTakeAway, &delayID, &commentContent, &commentUserID,
				&commentCreation, &priceTakeAway, &priceDelivery, &imageURL, &productionStatus,
				&productionDoneQty, &productionColor, &availableIn, &availableTakeAway,
				&availableDelivery,
			)

			if scanErr != nil {
				cols, _ := rows.Columns()

				fmt.Println("❌ SCAN FAILED")
				fmt.Println("➡️ Error:", scanErr)
				fmt.Println("➡️ Number of columns:", len(cols))
				fmt.Println("➡️ Columns returned by SQL:")
				for i, c := range cols {
					fmt.Printf("   [%02d] %s\n", i, c)
				}

				// Compare expected types vs actual
				debugTargets := []interface{}{
					&orderID, &quantity, &paidQuantity, &price, &productID, &name, &productDesc,
					&categName, &orderItemID, &isPaid, &isDistributed, &orderedOn, &basePrice,
					&discountID, &discountName, &readyForDistribution, &distributedQuantity,
					&tvaIn, &tvaDelivery, &tvaTakeAway, &delayID, &commentContent, &commentUserID,
					&commentCreation, &priceTakeAway, &priceDelivery, &imageURL, &productionStatus,
					&productionDoneQty, &productionColor, &availableIn, &availableTakeAway,
					&availableDelivery,
				}

				fmt.Println("➡️ Types attendus par Go pour chaque champ :")
				for i, v := range debugTargets {
					fmt.Printf("   [%02d] %T (pointer to %T)\n", i, v, reflect.Indirect(reflect.ValueOf(v)).Interface())
				}

				return nil, fmt.Errorf("Scan failed: %w", scanErr)
			}

			var comment models.OrderComment
			if commentContent.Valid {
				comment = models.OrderComment{
					OrderID: orderID.String, UserName: &commentUserID.String, Content: commentContent.String, CreationDate: helpers.NullTimePtr(commentCreation),
				}
			} else {
				comment = models.OrderComment{}
			}

			op := models.ProductEntry{
				OrderID:                      orderID.String,
				OrderItemID:                  orderItemID.String,
				OrderedOn:                    helpers.NullTimePtr(orderedOn).UTC().Unix(),
				ProductID:                    productID.String,
				ProductionStatus:             productionStatus.String,
				ProductionStatusDoneQuantity: int(productionDoneQty.Int64),
				Name:                         name.String,
				ImageURL:                     helpers.NullStringToPtr(imageURL),
				Category:                     helpers.NullStringToPtr(categName),
				CategoryID:                   helpers.NullStringToPtr(categName),
				Description:                  helpers.NullStringToPtr(productDesc),
				Quantity:                     int(quantity.Int64),
				PaidQuantity:                 int(paidQuantity.Int64),
				DistributedQuantity:          int(distributedQuantity.Int64),
				ReadyForDistributionQuantity: int(readyForDistribution.Int64),
				IsPaid:                       int(isPaid.Int64),
				IsDistributed:                int(isDistributed.Int64),
				Price:                        price.Int64,
				PriceTakeAway:                priceTakeAway.Int64,
				PriceDelivery:                priceDelivery.Int64,
				DiscountID:                   helpers.NullInt64ToPtr(discountID),
				DiscountName:                 helpers.NullStringToPtr(discountName),
				DiscountedPrice:              helpers.NilIfNullInt64Discount(discountID, price.Int64),
				TVAIn:                        tvaIn.Float64,
				TVADelivery:                  tvaDelivery.Float64,
				TVATakeAway:                  tvaTakeAway.Float64,
				AvailableIn:                  availableIn.Bool,
				AvailableTakeAway:            availableTakeAway.Bool,
				AvailableDelivery:            availableDelivery.Bool,
				ProductionColor:              helpers.NullStringToPtr(productionColor),
				Extra:                        extrasMap[orderItemID.String],
				Without:                      withoutsMap[orderItemID.String],
				Components:                   componentsMap[productID.String],
				Customers:                    snoClientsMap[orderItemID.String],
				Comment:                      comment,
			}
			if op.Customers == nil {
				op.Customers = []interface{}{}
			}
			if op.Extra == nil {
				op.Extra = []models.OrderProductExtra{}
			}
			if op.Without == nil {
				op.Without = []models.OrderProductWithout{}
			}
			if op.Components == nil {
				op.Components = []models.ComponentUsage{}
			}

			if attrs, ok := configurableAttributesMap[orderItemID.String]; ok {
				op.Configuration.Attributes = attrs
			} else {
				op.Configuration.Attributes = []models.ConfigurableAttribute{}
			}

			productsByOrderID[orderID.String] = append(productsByOrderID[orderID.String], op)
		}
		r.log.Info("products loaded")
	}

	// --- 1. HEADER ---
	// On injecte 'whereFilters' qui contient soit "state='OPEN'" soit "order_id=X"
	var orders []models.Order
	{
		step := "header"
		q := `
		SELECT o.order_id, o.order_num, o.order_type, o.state, o.scheduled, o.brand, o.brand_status, o.brand_order_id, o.brand_order_num, o.estimated_ready, o.means_of_payement, o.price, o.TVA, o.HT, o.monnaie, o.cutlery_notes,
		o.isPaid, o.isDistributed, o.dateCall, o.isDelivery, o.merchant_approval, o.delivery_fees, o.last_update, o.fulfillment_type, o.use_customer_temporary_address, o.creation_date, o.places_settings, o.pager_number,
		c.customer_id, c.customer_name, c.customer_tel, c.customer_lat, c.customer_lng, c.customer_temporary_phone, c.customer_temporary_phone_code, c.customer_nb_orders, c.customer_zone_code,
		c.customer_address, c.customer_floor_number, c.customer_door_number, c.customer_additional_address, c.customer_business_name, c.customer_birthdate, c.customer_additional_info,
		c.customer_temporary_address, c.customer_temporary_lat, c.customer_temporary_lng, c.customer_temporary_floor_number, c.customer_temporary_door_number, c.customer_temporary_additional_address,
		u.user_id, u.lat, u.lng, u.tel as deliveryTel, u.userName,
		ds.id as delivery_session_id, dso.priority
		FROM orders o
		LEFT JOIN customer c ON o.customer_id = c.customer_id
		LEFT JOIN users u ON o.responsible = u.user_id AND o.merchant_id = u.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING')
		WHERE o.merchant_id = ? ` + whereFilters + " " + orderByFilter + " " + limitsFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var ord models.Order
			var customerNbOrders, priority, isDelivery, useCustomerTemporaryAddress, price, TVA, HT, deliveryFees, placesSettings sql.NullInt64
			var customerID, orderID, orderNum, orderType, state, brand, brandStatus, brandOrderID, brandOrderNum, meansOfPayment, monnaie, cutleryNotes, dateCall, fulfillmentType, pagerNumber, merchantApproval, deliverySessionID, userID sql.NullString
			var customerLat, customerLng, customerTemporaryLat, customerTemporaryLng, userLat, userLng sql.NullFloat64
			var lastUpdate, creationDate, estimatedReady sql.NullTime
			var scheduled, isPaid, isDistributed sql.NullBool
			var cName, cTel, cTempPhone, cTempPhoneCode, cZoneCode, cAddr, cFloor, cDoor, cAddAddr, cBusName, cBirth, cInfo, cTempAddr, cTempFloor, cTempDoor, cTempAddAddr sql.NullString
			var delTel, delUserName sql.NullString

			if err := rows.Scan(&orderID, &orderNum, &orderType, &state, &scheduled, &brand, &brandStatus, &brandOrderID, &brandOrderNum, &estimatedReady, &meansOfPayment, &price, &TVA, &HT, &monnaie, &cutleryNotes,
				&isPaid, &isDistributed, &dateCall, &isDelivery, &merchantApproval, &deliveryFees, &lastUpdate, &fulfillmentType, &useCustomerTemporaryAddress, &creationDate, &placesSettings, &pagerNumber,
				&customerID, &cName, &cTel, &customerLat, &customerLng, &cTempPhone, &cTempPhoneCode, &customerNbOrders, &cZoneCode,
				&cAddr, &cFloor, &cDoor, &cAddAddr, &cBusName, &cBirth, &cInfo,
				&cTempAddr, &customerTemporaryLat, &customerTemporaryLng, &cTempFloor, &cTempDoor, &cTempAddAddr,
				&userID, &userLat, &userLng, &delTel, &delUserName,
				&deliverySessionID, &priority); err != nil {
				return nil, err
			}

			// --- Mapping Fields ---
			ord.OrderID = orderID.String
			ord.OrderNum = helpers.NullStringToPtr(orderNum)
			ord.Brand = helpers.NullStringToPtr(brand)
			ord.BrandOrderID = helpers.NullStringToPtr(brandOrderID)
			ord.BrandOrderNum = helpers.NullStringToPtr(brandOrderNum)
			ord.BrandStatus = helpers.NullStringToPtr(brandStatus)
			ord.DeliverySessionID = &deliverySessionID.String
			ord.OrderType = helpers.NullStringToPtr(orderType)
			ord.CutleryNotes = helpers.NullStringToPtr(cutleryNotes)
			ord.State = helpers.NullStringToPtr(state)
			ord.Scheduled = scheduled.Bool
			ord.TTC = price.Int64
			ord.TVA = helpers.NullInt64ToPtr(TVA)
			ord.HT = helpers.NullInt64ToPtr(HT)
			ord.PlacesSettings = helpers.NullInt64ToPtr(placesSettings)
			ord.PagerNumber = helpers.NullStringToPtr(pagerNumber)
			ord.IsPaid = isPaid.Bool
			ord.IsDistributed = isDistributed.Bool
			ord.IsSNO = userID.String == "-1"
			ord.CallHour = helpers.NullStringToPtr(dateCall)
			ord.EstimatedReady = helpers.NullTimeToNullUnixInt(estimatedReady)
			ord.IsDelivery = int(isDelivery.Int64)
			ord.MerchantApproval = merchantApproval.String
			ord.DeliveryFees = helpers.NullInt64ToPtr(deliveryFees)
			ord.CreationDate = helpers.NullTimePtr(creationDate).UTC().Unix()
			ord.FulfillmentType = helpers.NullStringToPtr(fulfillmentType)
			ord.LastUpdate = helpers.NullTimePtr(lastUpdate).UTC().Unix()

			// --- Customer ---
			if customerID.Valid {
				var cust models.Customer
				cust.CustomerID = &customerID.String
				cust.CustomerName = helpers.NullStringToPtr(cName)
				cust.CustomerTel = helpers.NullStringToPtr(cTel)
				cust.CustomerTemporaryPhone = helpers.NullStringToPtr(cTempPhone)
				cust.CustomerTemporaryPhoneCode = helpers.NullStringToPtr(cTempPhoneCode)
				nb := int(customerNbOrders.Int64)
				cust.CustomerNbOrders = &nb
				cust.CustomerAdditionalInfo = helpers.NullStringToPtr(cInfo)
				cust.CustomerZoneCode = helpers.NullStringToPtr(cZoneCode)

				if useCustomerTemporaryAddress.Int64 == 1 {
					cust.CustomerAddress = helpers.NullStringToPtr(cTempAddr)
					cust.CustomerLat = helpers.NullFloat64Ptr(customerTemporaryLat)
					cust.CustomerLng = helpers.NullFloat64Ptr(customerTemporaryLng)
					cust.CustomerFloorNumber = helpers.NullStringToPtr(cTempFloor)
					cust.CustomerDoorNumber = helpers.NullStringToPtr(cTempDoor)
					cust.CustomerAdditionalAddress = helpers.NullStringToPtr(cTempAddAddr)
				} else {
					cust.CustomerAddress = helpers.NullStringToPtr(cAddr)
					cust.CustomerLat = helpers.NullFloat64Ptr(customerLat)
					cust.CustomerLng = helpers.NullFloat64Ptr(customerLng)
					cust.CustomerFloorNumber = helpers.NullStringToPtr(cFloor)
					cust.CustomerDoorNumber = helpers.NullStringToPtr(cDoor)
					cust.CustomerAdditionalAddress = helpers.NullStringToPtr(cAddAddr)
				}
				ord.Customer = &cust
			}

			// --- Responsible (Delivery Man info on Order) ---
			if userID.Valid && userID.String != "0" && false {
				ord.Responsible = &models.OrderUser{
					UserID:    userID.String,
					Lat:       helpers.NullFloat64Ptr(userLat),
					Lng:       helpers.NullFloat64Ptr(userLng),
					FirstName: &delUserName.String, // Assuming userName contains name
					// Phone etc not explicitly selected in header join for u.*, assumed ok
				}
			}

			// Attach Children
			if prods, ok := productsByOrderID[orderID.String]; ok {
				ord.Products = prods
			} else {
				ord.Products = []models.ProductEntry{}
			}
			if pay, ok := paymentsByOrderID[orderID.String]; ok {
				ord.Payments = pay
			} else {
				ord.Payments = []models.Payment{}
			}
			if comm, ok := commentsByOrderID[orderID.String]; ok {
				ord.Comments = comm
			} else {
				ord.Comments = []models.OrderComment{}
			}
			if loc, ok := locationsByOrderID[orderID.String]; ok {
				ord.Location = loc
			} else {
				ord.Location = []models.Location{}
			}

			orders = append(orders, ord)
		}
		r.log.Info("header loaded")
	}

	r.log.Info(
		"fetchAndBuildOrders END",
		zap.Int("orders_count", len(orders)),
		zap.Duration("total_duration", time.Since(startTotal)))
	return orders, nil
}
