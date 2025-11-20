package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

type OrdersRepository struct {
	db  *sql.DB
	log *zap.Logger
}

func NewOrdersRepository(db *sql.DB, log *zap.Logger) *OrdersRepository {
	return &OrdersRepository{db: db, log: log}
}

// ==================================================================================
// PUBLIC METHODS
// ==================================================================================

// GetPendingOrders : Récupère toutes les commandes en cours (Legacy Logic)
func (r *OrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	r.log.Info("GetPendingOrders START", zap.String("merchant_id", merchantID))
	deliverySessionRepo := DeliverySessionsRepository{db: r.db, log: r.log}

	// 1. Construire le filtre spécifique pour "Pending"
	filter := " AND ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING')) OR ds.id IS NOT NULL) "

	// Ajout filtre APP
	if app == "1" || app == "WR_DELIVERY" {
		filter += " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		filter += " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// 2. Appeler la méthode partagée
	orders, err := r.fetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}

	// 3. Récupérer les sessions (spécifique à cet endpoint)
	// Note: On pourrait aussi factoriser ça si besoin, mais c'est assez rapide.
	sessions, err := deliverySessionRepo.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// 4. Lier les commandes aux sessions pour l'objet de réponse
	// (Optimisation: Map pour éviter double boucle)
	ordersMap := make(map[string]models.Order)
	for _, o := range orders {
		ordersMap[o.OrderID] = o
	}

	return &models.PendingOrdersResponse{
		Orders:           orders,
		DeliverySessions: sessions,
	}, nil
}

// GetOrder : Récupère une seule commande par son ID (Réutilise toute la logique !)
func (r *OrdersRepository) GetOrder(ctx context.Context, merchantID string, orderID string) (*models.Order, error) {
	r.log.Info("GetOrder START", zap.String("order_id", orderID))

	// Filtre strict sur l'ID
	filter := fmt.Sprintf(" AND o.order_id = '%s' ", orderID)

	orders, err := r.fetchAndBuildOrders(ctx, merchantID, filter)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, sql.ErrNoRows
	}

	return &orders[0], nil
}

// ==================================================================================
// PRIVATE SHARED BUILDER (THE CORE)
// ==================================================================================

// fetchAndBuildOrders exécute les 11 requêtes avec un filtre additionnel (WHERE clause)
// C'est ici qu'on optimise et qu'on log.
func (r *OrdersRepository) fetchAndBuildOrders(ctx context.Context, merchantID string, additionalFilter string) ([]models.Order, error) {
	startTotal := time.Now()

	// Helper pour logger et exécuter
	runQuery := func(stepName string, query string, args ...interface{}) (*sql.Rows, error) {
		t0 := time.Now()
		rows, err := r.db.QueryContext(ctx, query, args...)
		elapsed := time.Since(t0)

		if err != nil {
			r.log.Error("Query FAILED", zap.String("step", stepName), zap.Error(err))
			return nil, err
		}

		// LOG CRITIQUE : Si une étape prend > 1s, on le voit tout de suite
		if elapsed.Seconds() > 1.0 {
			r.log.Warn("Slow Query Detected", zap.String("step", stepName), zap.Duration("duration", elapsed))
		} else {
			r.log.Debug("Query OK", zap.String("step", stepName), zap.Duration("duration", elapsed))
		}
		return rows, nil
	}

	// --- 1. HEADER ---
	// On injecte 'additionalFilter' qui contient soit "state='OPEN'" soit "order_id=X"
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
		WHERE o.merchant_id = ? ` + additionalFilter

	rowsHeader, err := runQuery("1_Headers", qHeader, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsHeader.Close()

	// --- 2. PRODUCTS ---
	// On utilise le même filtre sur 'o' (orders) car on join dessus
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
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE oi.quantity > 0 AND o.merchant_id = ? ` + additionalFilter

	rowsProducts, err := runQuery("2_Products", qProducts, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsProducts.Close()

	// --- 3. COMPONENTS (Optimisation possible: filtrer par orderID si liste courte, sinon global) ---
	// Ici on garde global car c'est souvent du statique, MAIS si c'est lent, il faut filtrer sur les produits des orders concernés.
	// Pour l'instant on garde tel quel, c'est souvent rapide.
	qProductComponents := `
		SELECT r.product_id, c.component_id, c.name, c.component_price as price, c.status,
		rq.quantity, uomd.uom_desc
		FROM components c
		INNER JOIN requires rq ON c.component_id = rq.component_id AND rq.enabled IS TRUE
		INNER JOIN recipes r ON r.recipe_id = rq.recipe_id
		INNER JOIN unit_of_measure_desc uomd ON uomd.lang = 'FR' AND uomd.id = rq.unit_of_measure
		WHERE c.merchant_id = ? AND c.available = '1' AND rq.enabled IS TRUE`
	rowsProdComp, err := runQuery("3_Components", qProductComponents, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsProdComp.Close()

	// --- 4. EXTRAS ---
	qExtras := `
		SELECT e.order_item_id, e.id, e.order_id, e.product_id, ce.name, e.component_id, e.price
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN extra e on e.order_item_id = oi.order_item_id
		INNER JOIN components ce on e.component_id = ce.component_id and ce.merchant_id = o.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsExtras, err := runQuery("4_Extras", qExtras, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsExtras.Close()

	// --- 5. WITHOUTS ---
	qWithouts := `
		SELECT w.order_item_id, w.id, w.order_id, w.product_id, cw.name, w.component_id
		FROM orders o
		INNER JOIN orderitems oi on o.order_id = oi.order_id and oi.merchant_id = o.merchant_id
		INNER JOIN without w on w.order_item_id = oi.order_item_id
		INNER JOIN components cw on w.component_id = cw.component_id and cw.merchant_id = o.merchant_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsWithouts, err := runQuery("5_Withouts", qWithouts, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsWithouts.Close()

	// --- 6. PAYMENTS ---
	qPayments := `
		SELECT p.order_id, p.payment_id, p.mop, p.amount, p.payment_date, p.enabled
		from payments p
		INNER JOIN orders o on o.order_id = p.order_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsPayments, err := runQuery("6_Payments", qPayments, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsPayments.Close()

	// --- 7. CLIENTS SNO ---
	qClients := `
		SELECT DISTINCT ss.user_code, ss.user_name, oi.order_item_id, so.quantity
		FROM orderitems oi
		INNER JOIN session_orderitem so on so.order_item_id = oi.order_item_id
		INNER JOIN scannorder_session ss on so.user_code = ss.user_code
		INNER JOIN orders o ON o.order_id = oi.order_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE oi.merchant_id = ? ` + additionalFilter
	rowsClients, err := runQuery("7_ClientsSNO", qClients, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsClients.Close()

	// --- 8. ORDER COMMENTS ---
	qOrderComments := `
		SELECT oc.id, oc.user_id, oc.content, oc.creation_date, oc.order_id, u.userName
		from order_comments oc
		inner join orders o on o.order_id = oc.order_id
		left join users u on u.user_id = oc.user_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? and oc.order_item_id is null ` + additionalFilter
	rowsOrderComments, err := runQuery("8_OrderComments", qOrderComments, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsOrderComments.Close()

	// --- 9. LOCATIONS ---
	qLocations := `
		SELECT ol.order_id, ol.location_id, l.location_name, l.location_desc
		FROM orders o
		INNER JOIN order_location ol on ol.order_id = o.order_id
		INNER JOIN locations l on l.merchant_id = o.merchant_id and l.location_id = ol.location_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsLocations, err := runQuery("9_Locations", qLocations, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsLocations.Close()

	// --- 10. CONFIG ATTRIBUTES ---
	qConfigAttr := `
		SELECT oi.order_item_id, ca.id, ca.title, ca.max_options, ca.attribute_type
		FROM orders o
		INNER JOIN orderitems oi on oi.order_id = o.order_id
		INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
		INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsConfigAttr, err := runQuery("10_ConfigAttrs", qConfigAttr, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttr.Close()

	// --- 11. CONFIG OPTIONS ---
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
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? ` + additionalFilter
	rowsConfigAttrOptions, err := runQuery("11_ConfigOpts", qConfigAttrOptions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttrOptions.Close()

	r.log.Info("Queries DONE, starting mapping", zap.Duration("sql_duration", time.Since(startTotal)))
	mapStart := time.Now()

	// =================================================================
	// MAPPING LOGIC (COPIED & ADAPTED FROM YOUR CODE)
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

	// --- Config Attributes ---
	configurableAttributesMap := map[string][]models.ConfigurableAttribute{}
	for rowsConfigAttr.Next() {
		var id, orderItemID, title, attrType sql.NullString
		var maxOptions sql.NullInt64
		if err := rowsConfigAttr.Scan(&orderItemID, &id, &title, &maxOptions, &attrType); err != nil {
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

	// --- Components ---
	componentsMap := map[string][]models.ComponentUsage{}
	for rowsProdComp.Next() {
		var productID, name, uom sql.NullString
		var compID, price, status sql.NullInt64
		var qty sql.NullFloat64
		if err := rowsProdComp.Scan(&productID, &compID, &name, &price, &status, &qty, &uom); err != nil {
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

	// --- Extras ---
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

	// --- Withouts ---
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

	// --- Clients SNO ---
	snoClientsMap := map[string][]interface{}{}
	for rowsClients.Next() {
		var userCode, userName, orderItemID sql.NullString
		var quantity sql.NullInt64
		if err := rowsClients.Scan(&userCode, &userName, &orderItemID, &quantity); err != nil {
			return nil, err
		}
		clientObj := map[string]interface{}{"user_code": userCode.String, "user_name": userName.String, "quantity": quantity.Int64}
		snoClientsMap[orderItemID.String] = append(snoClientsMap[orderItemID.String], clientObj)
	}

	// --- Products ---
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

		var comment interface{}
		if commentContent.Valid {
			comment = map[string]interface{}{"user_id": commentUserID.String, "content": commentContent.String, "creation_date": nilIfZeroTime(commentCreation)}
		} else {
			comment = map[string]interface{}{"user_id": nil, "content": nil, "creation_date": nil}
		}

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

	// --- Payments ---
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
			OrderID: orderID.String, PaymentID: paymentID.Int64, MOP: mop.String, Amount: amount.Float64, PaymentDate: nullTimePtr(paymentDate), Enabled: int(enabled.Int64),
		})
	}

	// --- Order Comments ---
	commentsByOrderID := map[string][]models.OrderComment{}
	for rowsOrderComments.Next() {
		var id, userID sql.NullInt64
		var content, userName, orderID sql.NullString
		var creationDate sql.NullTime
		if err := rowsOrderComments.Scan(&id, &userID, &content, &creationDate, &orderID, &userName); err != nil {
			return nil, err
		}
		commentsByOrderID[orderID.String] = append(commentsByOrderID[orderID.String], models.OrderComment{
			OrderID: orderID.String, UserName: nullStringToPtr(userName), Content: content.String, CreationDate: nullTimePtr(creationDate),
		})
	}

	// --- Locations ---
	locationsByOrderID := map[string][]models.Location{}
	for rowsLocations.Next() {
		var locationID sql.NullInt64
		var locationName, locationDesc, orderID sql.NullString
		if err := rowsLocations.Scan(&orderID, &locationID, &locationName, &locationDesc); err != nil {
			return nil, err
		}
		locationsByOrderID[orderID.String] = append(locationsByOrderID[orderID.String], models.Location{
			OrderID: orderID.String, LocationID: locationID.Int64, LocationName: locationName.String, LocationDesc: nullStringToPtr(locationDesc),
		})
	}

	// --- FINAL ASSEMBLY (Headers) ---
	var orders []models.Order

	for rowsHeader.Next() {
		var ord models.Order
		var customerID, customerNbOrders, priority, isDelivery, useCustomerTemporaryAddress, price, TVA, HT, deliveryFees, placesSettings sql.NullInt64
		var orderID, orderNum, orderType, state, brand, brandStatus, brandOrderID, brandOrderNum, estimatedReady, meansOfPayment, monnaie, cutleryNotes, dateCall, fulfillmentType, pagerNumber, merchantApproval, deliverySessionID, userID sql.NullString
		var customerLat, customerLng, customerTemporaryLat, customerTemporaryLng, userLat, userLng sql.NullFloat64
		var lastUpdate, creationDate sql.NullTime
		var scheduled, isPaid, isDistributed sql.NullBool
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
		ord.DeliverySessionID = &deliverySessionID.String
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

	r.log.Info("fetchAndBuildOrders END", zap.Int("orders_count", len(orders)), zap.Duration("total_duration", time.Since(startTotal)), zap.Duration("map_duration", time.Since(mapStart)))
	return orders, nil
}
