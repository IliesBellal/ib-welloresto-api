package orders

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"welloresto-api/internal/helpers"
	"welloresto-api/internal/models"
	"welloresto-api/internal/utils/dbutils"
)

type OrdersFetcher struct {
	database *sql.DB
}

func NewOrdersFetcher(db *sql.DB) *OrdersFetcher {
	return &OrdersFetcher{
		database: db}
}

func (r *OrdersFetcher) FetchAndBuildOrders(ctx context.Context, merchantID string, whereFilters, orderByFilter, limitsFilters string) ([]models.Order, error) {

	// Begin transaction (read-only)
	// Note: On utilise le ctx parent. Si la requête HTTP est annulée, la transaction s'arrêtera proprement.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	/*
		tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, fmt.Errorf("BeginTx failed: %w", err)
		}

		// Ensure rollback if anything goes wrong
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		// --- HELPER FUNCTIONS CORRIGÉES ---
		// Helper to run a query with logging
		runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {

			rows, err := tx.QueryContext(ctx, query, args...)

			if err != nil {
				return nil, fmt.Errorf("%s query error: %w", step, err)
			}

			return rows, nil
		}
	*/

	// 1️⃣ Récupération dynamique de la DB ou de la Transaction depuis le contexte
	db := dbutils.GetDB(ctx, r.database)

	// --- HELPER FUNCTIONS ---
	// Le helper utilise maintenant 'db' (qui peut être un *sql.DB ou un *sql.Tx)
	runQuery := func(step string, query string, args ...interface{}) (*sql.Rows, error) {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("%s query error: %w", step, err)
		}
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
		case when oic.id is null then false else true end as selected,
		COALESCE(oic.quantity, 0) as quantity, cao.max_quantity
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
		LEFT JOIN order_item_configuration oic on oic.order_item_id = oi.order_item_id and cao.id = oic.configuration_attribute_option_id
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var attrID, orderItemID, id, title sql.NullString
			var extraPrice int
			var quantity, maxQuantity sql.NullInt64
			var selected sql.NullBool
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
				Selected:          selected.Bool,
			})
		}
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id
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
				return nil, err
			}
			commentsByOrderID[orderID.String] = append(commentsByOrderID[orderID.String], models.OrderComment{
				OrderID: orderID.String, UserName: helpers.NullStringToPtr(userName), Content: content.String, CreationDate: helpers.NullTimePtr(creationDate),
			})
		}
	}

	// --- 6. PAYMENTS ---
	paymentsByOrderID := map[string][]models.Payment{}
	{
		step := "payments"
		q := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.user_id, p.enabled
		from payments p
		INNER JOIN orders o on o.order_id = p.order_id
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id
		WHERE o.merchant_id = ? ` + whereFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var paymentID, amount sql.NullInt64
			var mop, orderID, UserID sql.NullString
			var paymentDate sql.NullTime
			var enabled sql.NullBool

			if err := rows.Scan(&orderID, &paymentID, &mop, &amount, &paymentDate, &UserID, &enabled); err != nil {
				return nil, err
			}
			paymentsByOrderID[orderID.String] = append(paymentsByOrderID[orderID.String], models.Payment{
				OrderID: orderID.String, PaymentID: paymentID.Int64, MOP: mop.String, Amount: amount.Int64, PaymentDate: helpers.NullTimePtr(paymentDate).UTC().Unix(), UserID: UserID.String, Enabled: enabled.Bool,
			})
		}
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
		-- LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		-- LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
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
				PriceTakeAway:                &priceTakeAway.Int64,
				PriceDelivery:                &priceDelivery.Int64,
				DiscountID:                   helpers.NullInt64ToPtr(discountID),
				DiscountName:                 helpers.NullStringToPtr(discountName),
				DiscountedPrice:              helpers.NilIfNullInt64Discount(discountID, price.Int64),
				TVAIn:                        &tvaIn.Float64,
				TVADelivery:                  &tvaDelivery.Float64,
				TVATakeAway:                  &tvaTakeAway.Float64,
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
	}

	// =====================================================
	// TEMP DELIVERY SESSIONS (AVANT HEADER)
	// =====================================================
	type deliverySessionTmp struct {
		SessionID string
		Priority  *int
	}

	deliverySessionsByOrderID := make(map[string]deliverySessionTmp)

	{
		step := "delivery_sessions_tmp"

		q := `
	SELECT
		dso.order_id,
		ds.id AS delivery_session_id,
		dso.priority
	FROM delivery_session_order dso
	INNER JOIN delivery_session ds
		ON ds.id = dso.delivery_session_id
		AND ds.status IN ('1','PENDING')
	WHERE ds.merchant_id = ?
	`

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var orderID string
			var sessionID sql.NullString
			var priority sql.NullInt64

			if err := rows.Scan(&orderID, &sessionID, &priority); err != nil {
				return nil, err
			}

			if sessionID.Valid {
				var p *int
				if priority.Valid {
					v := int(priority.Int64)
					p = &v
				}

				deliverySessionsByOrderID[orderID] = deliverySessionTmp{
					SessionID: sessionID.String,
					Priority:  p,
				}
			}
		}
	}

	// =====================================================
	// HEADER ORDERS
	// =====================================================
	var orders []models.Order
	{
		step := "header"
		q := `
	SELECT
		o.order_id, o.order_num, o.order_type, o.state, o.scheduled,
		o.brand, o.brand_status, o.brand_order_id, o.brand_order_num,
		o.estimated_ready, o.means_of_payement, o.price, o.TVA, o.HT,
		o.monnaie, o.cutlery_notes,
		o.isPaid, o.isDistributed, o.dateCall, o.isDelivery,
		o.merchant_approval, o.delivery_fees, o.last_update,
		o.fulfillment_type, o.use_customer_temporary_address,
		o.creation_date, o.places_settings, o.pager_number,

		c.customer_id, c.customer_name, c.customer_last_name, c.customer_first_name, c.customer_tel,
		c.customer_lat, c.customer_lng,
		c.customer_temporary_phone, c.customer_temporary_phone_code,
		c.customer_nb_orders, c.customer_zone_code,
		c.customer_address, c.customer_floor_number,
		c.customer_door_number, c.customer_additional_address,
		c.customer_business_name, c.customer_birthdate,
		c.customer_additional_info,
		c.customer_temporary_address, c.customer_temporary_lat,
		c.customer_temporary_lng, c.customer_temporary_floor_number,
		c.customer_temporary_door_number,
		c.advertising_consent, c.customer_brand,
		c.customer_temporary_additional_address,

		u.user_id, u.lat, u.lng, u.tel AS deliveryTel, u.userName,
	
		cr.cash_register_id, case when cr.end_date is null AND cr.cash_register_id is not null then false else true end as closed
	
	FROM orders o
	LEFT JOIN customer c ON o.customer_id = c.customer_id
	LEFT JOIN users u ON o.responsible = u.user_id AND o.merchant_id = u.merchant_id
    LEFT JOIN cash_registers cr on cr.cash_register_id = o.cash_register_id
	WHERE o.merchant_id = ? ` + whereFilters + " " + orderByFilter + " " + limitsFilters

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var ord models.Order

			var customerNbOrders, isDelivery, useCustomerTemporaryAddress,
				price, TVA, HT, deliveryFees, placesSettings sql.NullInt64

			var customerID, orderID, orderNum, orderType, state,
				brand, brandStatus, brandOrderID, brandOrderNum,
				meansOfPayment, monnaie, cutleryNotes, dateCall,
				fulfillmentType, pagerNumber, merchantApproval, userID, customerBrand sql.NullString

			var customerLat, customerLng, customerTemporaryLat,
				customerTemporaryLng, userLat, userLng sql.NullFloat64

			var lastUpdate, creationDate, estimatedReady sql.NullTime
			var scheduled, isPaid, isDistributed, cashRegisterClosed, advertisingConsent sql.NullBool

			var cName, cLastName, cFirstName, cTel, cTempPhone, cTempPhoneCode, cZoneCode,
				cAddr, cFloor, cDoor, cAddAddr, cBusName, cBirth,
				cInfo, cTempAddr, cTempFloor, cTempDoor, cTempAddAddr sql.NullString

			var delTel, delUserName, cashRegisterID sql.NullString

			if err := rows.Scan(
				&orderID, &orderNum, &orderType, &state, &scheduled,
				&brand, &brandStatus, &brandOrderID, &brandOrderNum,
				&estimatedReady, &meansOfPayment, &price, &TVA, &HT,
				&monnaie, &cutleryNotes,
				&isPaid, &isDistributed, &dateCall, &isDelivery,
				&merchantApproval, &deliveryFees, &lastUpdate,
				&fulfillmentType, &useCustomerTemporaryAddress,
				&creationDate, &placesSettings, &pagerNumber,

				&customerID, &cName, &cLastName, &cFirstName, &cTel, &customerLat, &customerLng,
				&cTempPhone, &cTempPhoneCode, &customerNbOrders, &cZoneCode,
				&cAddr, &cFloor, &cDoor, &cAddAddr, &cBusName, &cBirth,
				&cInfo,
				&cTempAddr, &customerTemporaryLat, &customerTemporaryLng,
				&cTempFloor, &cTempDoor, &advertisingConsent, &customerBrand, &cTempAddAddr,

				&userID, &userLat, &userLng, &delTel, &delUserName,

				&cashRegisterID, &cashRegisterClosed,
			); err != nil {
				return nil, err
			}

			// --- Mapping Order ---
			ord.OrderID = orderID.String
			ord.OrderNum = helpers.NullStringToPtr(orderNum)
			ord.Brand = helpers.NullStringToPtr(brand)
			ord.BrandOrderID = helpers.NullStringToPtr(brandOrderID)
			ord.BrandOrderNum = helpers.NullStringToPtr(brandOrderNum)
			ord.BrandStatus = helpers.NullStringToPtr(brandStatus)
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

			// --- Delivery Session (ASSOCIATION TEMPORAIRE) ---
			if ds, ok := deliverySessionsByOrderID[orderID.String]; ok {
				ord.DeliverySessionID = &ds.SessionID
				if ds.Priority != nil {
					ord.DeliveryPriority = ds.Priority
				}
			}

			// --- Customer ---
			if customerID.Valid {
				var cust models.Customer
				cust.CustomerID = &customerID.String
				cust.CustomerName = helpers.NullStringToPtr(cName)
				cust.CustomerLastName = helpers.NullStringToPtr(cLastName)
				cust.CustomerFirstName = helpers.NullStringToPtr(cFirstName)
				cust.CustomerTel = helpers.NullStringToPtr(cTel)
				cust.CustomerTemporaryPhone = helpers.NullStringToPtr(cTempPhone)
				cust.CustomerTemporaryPhoneCode = helpers.NullStringToPtr(cTempPhoneCode)
				nb := int(customerNbOrders.Int64)
				cust.CustomerNbOrders = &nb
				cust.CustomerAdditionalInfo = helpers.NullStringToPtr(cInfo)
				cust.CustomerZoneCode = helpers.NullStringToPtr(cZoneCode)
				cust.AdvertisingConsent = &advertisingConsent.Bool
				cust.CustomerBrand = helpers.NullStringToPtr(customerBrand)
				cust.CustomerBusinessName = helpers.NullStringToPtr(cBusName)
				cust.CustomerBirthdate = helpers.NullStringToPtr(cBirth)

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

			if cashRegisterID.Valid {
				ord.CashRegister = &models.CashRegister{
					CashRegisterID: cashRegisterID.String,
					Closed:         cashRegisterClosed.Bool,
				}
			}

			// --- Attach Children ---
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
	}

	return orders, nil
}
