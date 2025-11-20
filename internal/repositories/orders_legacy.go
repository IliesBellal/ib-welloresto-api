package repositories

import (
	"context"
	"database/sql"
	"welloresto-api/internal/models"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type LegacyOrdersRepository struct {
	db *sql.DB
}

func NewLegacyOrdersRepository(db *sql.DB) *LegacyOrdersRepository {
	return &LegacyOrdersRepository{db: db}
}

// GetPendingOrders returns orders + delivery sessions
func (r *LegacyOrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {

	// 1. TRANSACTION : On ouvre la transaction avec le contexte parent
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	// Sécurité : Si ça plante, on rollback.
	defer func() { _ = tx.Rollback() }()

	// --- Helper Query (pour éviter de répéter le code d'erreur) ---
	runQuery := func(query string, args ...interface{}) (*sql.Rows, error) {
		// Utilisation stricte de 'ctx' et 'tx' pour éviter le bug "context canceled"
		return tx.QueryContext(ctx, query, args...)
	}

	// build query_filter per app param
	queryFilter := ""
	if app == "1" || app == "WR_DELIVERY" {
		queryFilter = " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		queryFilter = " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// =================================================================
	// EXÉCUTION DES REQUÊTES (Mode PHP : On charge tout d'abord)
	// =================================================================

	// 1) orders header
	qHeader := `
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
		WHERE ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING')) OR ds.id IS NOT NULL)
		AND o.merchant_id = ? ` + queryFilter

	rowsHeader, err := runQuery(qHeader, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsHeader.Close()

	// 2) order products
	qProducts := `
		SELECT o.order_id, oi.quantity, oi.paid_quantity, oi.price, oi.product_id, p.name, p.product_desc, pc.categ_name, oi.order_item_id, oi.isPaid, oi.isDistributed, oi.ordered_on, p.price as base_price, oi.discount_id, d.discount_name, oi.ready_for_distribution_quantity, oi.distributed_quantity, tva_in.tva_rate as tva_rate_in, tva_delivery.tva_rate as tva_rate_delivery, tva_take_away.tva_rate as tva_rate_take_away, oi.delay_id, oc.content, oc.user_id, oc.creation_date,
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
		WHERE (o.state = 'OPEN')
		AND oi.quantity > 0
		AND o.merchant_id = ?`

	rowsProducts, err := runQuery(qProducts, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsProducts.Close()

	// 3) components for products (global list)
	qProductComponents := `
		SELECT r.product_id, c.component_id, c.name, c.component_price as price, c.status,
		rq.quantity, uomd.uom_desc
		FROM components c
		INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled IS TRUE
		INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
		INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
		WHERE c.merchant_id = ?
		AND available = '1'
		AND rq.enabled IS TRUE`
	rowsProdComp, err := runQuery(qProductComponents, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsProdComp.Close()

	// 4) extras
	qExtras := `
		SELECT e.order_item_id, e.id, e.order_id, e.product_id, ce.name, e.component_id, e.price
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN extra e on e.order_item_id = oi.order_item_id
		INNER JOIN components ce on e.component_id = ce.component_id and ce.merchant_id = o.merchant_id
		WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsExtras, err := runQuery(qExtras, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsExtras.Close()

	// 5) withouts
	qWithouts := `
		SELECT w.order_item_id, w.id, w.order_id, w.product_id, cw.name, w.component_id
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN without w on w.order_item_id = oi.order_item_id
		INNER JOIN components cw on w.component_id = cw.component_id and cw.merchant_id = o.merchant_id
		WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsWithouts, err := runQuery(qWithouts, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsWithouts.Close()

	// 6) payments
	qPayments := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled
		from payments p
		INNER JOIN orders o on o.order_id = p.order_id
		WHERE o.state = 'OPEN'
		and o.merchant_id = ?`
	rowsPayments, err := runQuery(qPayments, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsPayments.Close()

	// 7) clients for SNO
	qClients := `
		SELECT DISTINCT ss.user_code, ss.user_name, oi.order_item_id, so.quantity
		FROM orderitems oi
		INNER JOIN session_orderitem so on so.order_item_id = oi.order_item_id
		INNER JOIN scannorder_session ss on so.user_code = ss.user_code
		WHERE oi.merchant_id = ?`
	rowsClients, err := runQuery(qClients, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsClients.Close()

	// 8) comments (order level)
	qOrderComments := `
		SELECT oc.id, oc.user_id, oc.content, oc.creation_date, oc.order_id, u.userName
		from order_comments oc
		inner join orders o on o.order_id = oc.order_id
		left join users u on u.user_id = oc.user_id
		WHERE o.state = 'OPEN'
		and o.merchant_id = ?
		and oc.order_item_id is null`
	rowsOrderComments, err := runQuery(qOrderComments, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsOrderComments.Close()

	// 9) locations
	qLocations := `
		SELECT ol.order_id, ol.location_id, l.location_name, l.location_desc
		FROM orders o
		INNER JOIN order_location ol on ol.order_id = o.order_id
		INNER JOIN locations l on l.merchant_id = o.merchant_id and l.location_id = ol.location_id
		WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsLocations, err := runQuery(qLocations, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsLocations.Close()

	// 10) configurable attributes + options
	qConfigAttr := `
		SELECT oi.order_item_id, ca.id, ca.title, ca.max_options, ca.attribute_type
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsConfigAttr, err := runQuery(qConfigAttr, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttr.Close()

	qConfigAttrOptions := `
		SELECT ca.id as configurable_attribute_id, oi.order_item_id, cao.id, cao.title, cao.extra_price, 
		case when oic.id is null then 0 else 1 end as selected,
		case when oic.quantity is null then 0 else oic.quantity end as quantity, cao.max_quantity
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
		LEFT JOIN order_item_configuration oic on oic.order_item_id = oi.order_item_id and cao.id = oic.configuration_attribute_option_id
		WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsConfigAttrOptions, err := runQuery(qConfigAttrOptions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttrOptions.Close()

	// 11) delivery sessions
	qDeliverySessions := `
		SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
		FROM delivery_session ds
		INNER JOIN users u on u.user_id = ds.user_id
		WHERE status IN ('1','PENDING')
		AND ds.merchant_id = ?`
	rowsDeliverySessions, err := runQuery(qDeliverySessions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsDeliverySessions.Close()

	// =================================================================
	// PARSING & MAPPING (Optimisation via Maps)
	// =================================================================

	// --- Config Options ---
	type optKey struct {
		OrderItemID string
		AttrID      string
	}
	configurableOptionsMap := map[optKey][]models.ConfigurableOption{}

	for rowsConfigAttrOptions.Next() {
		var attrID, orderItemID, id, title sql.NullString
		var extraPrice int
		var selected, quantity, maxQuantity sql.NullInt64

		if err := rowsConfigAttrOptions.Scan(&attrID, &orderItemID, &id, &title, &extraPrice, &selected, &quantity, &maxQuantity); err != nil {
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

	// --- Config Attributes (Map: OrderItemID -> Attributes) ---
	configurableAttributesMap := map[string][]models.ConfigurableAttribute{}
	for rowsConfigAttr.Next() {
		var id, orderItemID, title, attrType sql.NullString
		var maxOptions sql.NullInt64

		if err := rowsConfigAttr.Scan(&orderItemID, &id, &title, &maxOptions, &attrType); err != nil {
			return nil, err
		}

		// Build final object with options attached
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

	// --- Components (Map: ProductID -> Components) ---
	componentsMap := map[string][]models.ComponentUsage{}
	for rowsProdComp.Next() {
		var productID, name, uom sql.NullString
		var compID, price, status sql.NullInt64
		var qty sql.NullFloat64

		if err := rowsProdComp.Scan(&productID, &compID, &name, &price, &status, &qty, &uom); err != nil {
			return nil, err
		}
		c := models.ComponentUsage{
			ComponentID:   compID.Int64,
			Name:          name.String,
			ProductID:     productID.String,
			Price:         price.Int64,
			Quantity:      qty.Float64,
			UnitOfMeasure: uom.String,
			Status:        int(status.Int64),
		}
		componentsMap[productID.String] = append(componentsMap[productID.String], c)
	}

	// --- Extras (Map: OrderItemID -> Extras) ---
	extrasMap := map[string][]models.OrderProductExtra{}
	for rowsExtras.Next() {
		var orderItemID, id, orderID, productID, compID, name sql.NullString
		var price sql.NullFloat64
		if err := rowsExtras.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID, &price); err != nil {
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

	// --- Withouts (Map: OrderItemID -> Withouts) ---
	withoutsMap := map[string][]models.OrderProductWithout{}
	for rowsWithouts.Next() {
		var orderItemID, id, orderID, productID, compID, name sql.NullString
		if err := rowsWithouts.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID); err != nil {
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

	// --- Clients SNO (Map: OrderItemID -> Clients) ---
	snoClientsMap := map[string][]interface{}{} // Using interface{} to match model generic type
	for rowsClients.Next() {
		var userCode, userName, orderItemID sql.NullString
		var quantity sql.NullInt64
		if err := rowsClients.Scan(&userCode, &userName, &orderItemID, &quantity); err != nil {
			return nil, err
		}
		clientObj := map[string]interface{}{
			"user_code": userCode.String,
			"user_name": userName.String,
			"quantity":  quantity.Int64,
		}
		snoClientsMap[orderItemID.String] = append(snoClientsMap[orderItemID.String], clientObj)
	}

	// --- Products (Map: OrderID -> []ProductEntry) ---
	productsByOrderID := map[string][]models.ProductEntry{}

	for rowsProducts.Next() {
		var quantity, paidQuantity, price, isPaid, isDistributed, basePrice, discountID, readyForDistribution, distributedQuantity, priceTakeAway, priceDelivery, productionDoneQty sql.NullInt64
		var productID, name, productDesc, categName, orderItemID, discountName, delayID, commentContent, commentUserID, imageURL, productionStatus, productionColor, orderID sql.NullString
		var tvaIn, tvaDelivery, tvaTakeAway sql.NullFloat64
		var orderedOn, commentCreation sql.NullTime
		var availableIn, availableTakeAway, availableDelivery sql.NullBool

		if err := rowsProducts.Scan(&orderID, &quantity, &paidQuantity, &price, &productID, &name, &productDesc, &categName, &orderItemID, &isPaid, &isDistributed, &orderedOn, &basePrice, &discountID, &discountName, &readyForDistribution, &distributedQuantity, &tvaIn, &tvaDelivery, &tvaTakeAway, &delayID, &commentContent, &commentUserID, &commentCreation,
			&priceTakeAway, &priceDelivery, &imageURL, &productionStatus, &productionDoneQty, &productionColor, &availableIn, &availableTakeAway, &availableDelivery); err != nil {
			return nil, err
		}

		// Assemble Comment Map
		var comment interface{}
		if commentContent.Valid {
			comment = map[string]interface{}{
				"user_id":       commentUserID.String,
				"content":       commentContent.String,
				"creation_date": nilIfZeroTime(commentCreation),
			}
		} else {
			comment = map[string]interface{}{"user_id": nil, "content": nil, "creation_date": nil}
		}

		// Assemble Product
		op := models.ProductEntry{
			OrderID:                      orderID.String,
			OrderItemID:                  orderItemID.String,
			OrderedOn:                    nullTimePtr(orderedOn),
			ProductID:                    productID.String,
			ProductionStatus:             productionStatus.String,
			ProductionStatusDoneQuantity: int(productionDoneQty.Int64),
			Name:                         name.String,
			ImageURL:                     nullStringToPtr(imageURL),
			Category:                     nullStringToPtr(categName),
			Description:                  nullStringToPtr(productDesc),
			Quantity:                     int(quantity.Int64),
			PaidQuantity:                 int(paidQuantity.Int64),
			DistributedQuantity:          int(distributedQuantity.Int64),
			ReadyForDistributionQuantity: int(readyForDistribution.Int64),
			IsPaid:                       int(isPaid.Int64),
			IsDistributed:                int(isDistributed.Int64),
			Price:                        price.Int64,
			PriceTakeAway:                priceTakeAway.Int64,
			PriceDelivery:                priceDelivery.Int64,
			DiscountID:                   nullInt64ToPtr(discountID),
			DiscountName:                 nullStringToPtr(discountName),
			DiscountedPrice:              nilIfNullInt64Discount(discountID, price.Int64),
			TVAIn:                        tvaIn.Float64,
			TVADelivery:                  tvaDelivery.Float64,
			TVATakeAway:                  tvaTakeAway.Float64,
			AvailableIn:                  availableIn.Bool,
			AvailableTakeAway:            availableTakeAway.Bool,
			AvailableDelivery:            availableDelivery.Bool,
			ProductionColor:              nullStringToPtr(productionColor),

			// Attach children using Maps (Fast)
			Extra:      extrasMap[orderItemID.String],
			Without:    withoutsMap[orderItemID.String],
			Components: componentsMap[productID.String],
			Customers:  snoClientsMap[orderItemID.String],
			Comment:    comment,
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

		// Config
		if attrs, ok := configurableAttributesMap[orderItemID.String]; ok {
			op.Configuration.Attributes = attrs
		} else {
			op.Configuration.Attributes = []models.ConfigurableAttribute{}
		}

		productsByOrderID[orderID.String] = append(productsByOrderID[orderID.String], op)
	}

	// --- Payments (Map: OrderID -> Payments) ---
	paymentsByOrderID := map[string][]models.Payment{}
	for rowsPayments.Next() {
		var paymentID, enabled sql.NullInt64
		var mop, orderID sql.NullString
		var amount sql.NullFloat64
		var paymentDate sql.NullTime
		if err := rowsPayments.Scan(&orderID, &paymentID, &mop, &amount, &paymentDate, &enabled); err != nil {
			return nil, err
		}
		paymentsByOrderID[orderID.String] = append(paymentsByOrderID[orderID.String], models.Payment{
			OrderID:     orderID.String,
			PaymentID:   paymentID.Int64,
			MOP:         mop.String,
			Amount:      amount.Float64,
			PaymentDate: nullTimePtr(paymentDate),
			Enabled:     int(enabled.Int64),
		})
	}

	// --- Order Comments (Map: OrderID -> Comments) ---
	commentsByOrderID := map[string][]models.OrderComment{}
	for rowsOrderComments.Next() {
		var id, userID sql.NullInt64
		var content, userName, orderID sql.NullString
		var creationDate sql.NullTime
		if err := rowsOrderComments.Scan(&id, &userID, &content, &creationDate, &orderID, &userName); err != nil {
			return nil, err
		}
		commentsByOrderID[orderID.String] = append(commentsByOrderID[orderID.String], models.OrderComment{
			OrderID:      orderID.String,
			UserName:     nullStringToPtr(userName),
			Content:      content.String,
			CreationDate: nullTimePtr(creationDate),
		})
	}

	// --- Locations (Map: OrderID -> Locations) ---
	locationsByOrderID := map[string][]models.Location{}
	for rowsLocations.Next() {
		var locationID sql.NullInt64
		var locationName, locationDesc, orderID sql.NullString
		if err := rowsLocations.Scan(&orderID, &locationID, &locationName, &locationDesc); err != nil {
			return nil, err
		}
		locationsByOrderID[orderID.String] = append(locationsByOrderID[orderID.String], models.Location{
			OrderID:      orderID.String,
			LocationID:   locationID.Int64,
			LocationName: locationName.String,
			LocationDesc: nullStringToPtr(locationDesc),
		})
	}

	// --- Delivery Sessions ---
	deliverySessionsMap := map[string]*models.DeliverySession{} // Key: SessionID (Pointer to update orders inside)
	var allDeliverySessions []models.DeliverySession

	for rowsDeliverySessions.Next() {
		var profilePic, firstName, lastName, planningColor, status, id, userID sql.NullString
		var lat, lng sql.NullFloat64

		if err := rowsDeliverySessions.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			return nil, err
		}

		ds := models.DeliverySession{
			DeliverySessionID: id.String,
			Status:            status.String,
			Orders:            []models.Order{}, // Will be filled later
			DeliveryMan: models.OrderUser{
				UserID:         userID.String,
				FirstName:      &firstName.String,
				LastName:       &lastName.String,
				ProfilePicture: nullStringToPtr(profilePic),
				Lat:            nullFloat64Ptr(lat),
				Lng:            nullFloat64Ptr(lng),
				PlanningColor:  nullStringToPtr(planningColor),
			},
		}
		allDeliverySessions = append(allDeliverySessions, ds)
	}
	// Pointers map to easily append orders in the final loop
	for i := range allDeliverySessions {
		deliverySessionsMap[allDeliverySessions[i].DeliverySessionID] = &allDeliverySessions[i]
	}

	// =================================================================
	// ASSEMBLAGE FINAL (Loop Headers)
	// =================================================================
	orders := []models.Order{}

	for rowsHeader.Next() {
		var ord models.Order
		var customerID, customerNbOrders, priority, isDelivery, useCustomerTemporaryAddress, price, TVA, HT, deliveryFees, placesSettings sql.NullInt64
		var orderID, orderNum, orderType, state, brand, brandStatus, brandOrderID, brandOrderNum, estimatedReady, meansOfPayment, monnaie, cutleryNotes, dateCall, fulfillmentType, pagerNumber, merchantApproval, deliverySessionID, userID sql.NullString
		var customerLat, customerLng, customerTemporaryLat, customerTemporaryLng, userLat, userLng sql.NullFloat64
		var lastUpdate, creationDate sql.NullTime
		var scheduled, isPaid, isDistributed sql.NullBool

		// Customer String fields
		var cName, cTel, cTempPhone, cTempPhoneCode, cZoneCode, cAddr, cFloor, cDoor, cAddAddr, cBusName, cBirth, cInfo, cTempAddr, cTempFloor, cTempDoor, cTempAddAddr sql.NullString
		var delTel, delUserName sql.NullString

		if err := rowsHeader.Scan(&orderID, &orderNum, &orderType, &state, &scheduled, &brand, &brandStatus, &brandOrderID, &brandOrderNum, &estimatedReady, &meansOfPayment, &price, &TVA, &HT, &monnaie, &cutleryNotes,
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
		ord.OrderNum = nullStringToPtr(orderNum)
		ord.Brand = nullStringToPtr(brand)
		ord.BrandOrderID = nullStringToPtr(brandOrderID)
		ord.BrandOrderNum = nullStringToPtr(brandOrderNum)
		ord.BrandStatus = nullStringToPtr(brandStatus)
		ord.OrderType = nullStringToPtr(orderType)
		ord.CutleryNotes = nullStringToPtr(cutleryNotes)
		ord.State = nullStringToPtr(state)
		ord.Scheduled = scheduled.Bool
		ord.TTC = price.Int64
		ord.TVA = nullInt64ToPtr(TVA)
		ord.HT = nullInt64ToPtr(HT)
		ord.PlacesSettings = nullInt64ToPtr(placesSettings)
		ord.PagerNumber = nullStringToPtr(pagerNumber)
		ord.IsPaid = isPaid.Bool
		ord.IsDistributed = isDistributed.Bool
		ord.IsSNO = userID.String == "-1"
		ord.CallHour = nullStringToPtr(dateCall)
		ord.EstimatedReady = nullStringToPtr(estimatedReady)
		ord.IsDelivery = int(isDelivery.Int64)
		ord.MerchantApproval = merchantApproval.String
		ord.DeliveryFees = nullInt64ToPtr(deliveryFees)
		ord.CreationDate = nullTimePtr(creationDate)
		ord.FulfillmentType = nullStringToPtr(fulfillmentType)
		ord.LastUpdate = nullTimePtr(lastUpdate)

		// --- Customer ---
		if customerID.Valid {
			var cust models.Customer
			cust.CustomerID = &customerID.Int64
			cust.CustomerName = nullStringToPtr(cName)
			cust.CustomerTel = nullStringToPtr(cTel)
			cust.CustomerTemporaryPhone = nullStringToPtr(cTempPhone)
			cust.CustomerTemporaryPhoneCode = nullStringToPtr(cTempPhoneCode)
			nb := int(customerNbOrders.Int64)
			cust.CustomerNbOrders = &nb
			cust.CustomerAdditionalInfo = nullStringToPtr(cInfo)
			cust.CustomerZoneCode = nullStringToPtr(cZoneCode)

			if useCustomerTemporaryAddress.Int64 == 1 {
				cust.CustomerAddress = nullStringToPtr(cTempAddr)
				cust.CustomerLat = nullFloat64Ptr(customerTemporaryLat)
				cust.CustomerLng = nullFloat64Ptr(customerTemporaryLng)
				cust.CustomerFloorNumber = nullStringToPtr(cTempFloor)
				cust.CustomerDoorNumber = nullStringToPtr(cTempDoor)
				cust.CustomerAdditionalAddress = nullStringToPtr(cTempAddAddr)
			} else {
				cust.CustomerAddress = nullStringToPtr(cAddr)
				cust.CustomerLat = nullFloat64Ptr(customerLat)
				cust.CustomerLng = nullFloat64Ptr(customerLng)
				cust.CustomerFloorNumber = nullStringToPtr(cFloor)
				cust.CustomerDoorNumber = nullStringToPtr(cDoor)
				cust.CustomerAdditionalAddress = nullStringToPtr(cAddAddr)
			}
			ord.Customer = &cust
		}

		// --- Responsible (Delivery Man info on Order) ---
		if userID.Valid && userID.String != "0" {
			ord.Responsible = &models.OrderUser{
				UserID:    userID.String,
				Lat:       nullFloat64Ptr(userLat),
				Lng:       nullFloat64Ptr(userLng),
				FirstName: &delUserName.String, // Assuming userName contains name
				// Phone etc not explicitly selected in header join for u.*, assumed ok
			}
		}

		// --- Attach Children from Maps ---
		if prods, ok := productsByOrderID[ord.OrderID]; ok {
			ord.Products = prods
		} else {
			ord.Products = []models.ProductEntry{}
		}

		if pay, ok := paymentsByOrderID[ord.OrderID]; ok {
			ord.Payments = pay
		} else {
			ord.Payments = []models.Payment{}
		}

		if comm, ok := commentsByOrderID[ord.OrderID]; ok {
			ord.Comments = comm
		} else {
			ord.Comments = []models.OrderComment{}
		}

		if loc, ok := locationsByOrderID[ord.OrderID]; ok {
			ord.Location = loc // Location is usually a slice in your model? Or single? Assuming slice based on query
		} else {
			ord.Location = []models.Location{}
		}

		// --- Add to main list ---
		orders = append(orders, ord)

		// --- Add to Delivery Session if applicable ---
		if deliverySessionID.Valid {
			if session, ok := deliverySessionsMap[deliverySessionID.String]; ok {
				// Add this order to the session's order list
				session.Orders = append(session.Orders, ord)
			}
		}
	}

	// 3. COMMIT (Lecture seule mais bonne pratique)
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &models.PendingOrdersResponse{
		Orders:           orders,
		DeliverySessions: allDeliverySessions,
	}, nil
}
