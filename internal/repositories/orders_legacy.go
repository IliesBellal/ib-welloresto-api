package repositories

import (
	"context"
	"database/sql"
	"time"

	"welloresto-api/internal/models"
)

// LegacyOrdersRepository implements the PHP-style (legacy) data retrieval for pending orders
type LegacyOrdersRepository struct {
	db *sql.DB
}

func NewLegacyOrdersRepository(db *sql.DB) *LegacyOrdersRepository {
	return &LegacyOrdersRepository{db: db}
}

// GetPendingOrders returns orders + delivery sessions (sessions also included in the orders list as requested)
func (r *LegacyOrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	// We'll follow the same multiple-query aggregation pattern as the PHP code.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// build query_filter per app param
	queryFilter := ""
	if app == "1" || app == "WR_DELIVERY" {
		queryFilter = " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		queryFilter = " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

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

	rowsHeader, err := tx.QueryContext(ctx, qHeader, merchantID)
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

	rowsProducts, err := tx.QueryContext(ctx, qProducts, merchantID)
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
AND rq.enabled IS TRUE
`
	rowsProdComp, err := tx.QueryContext(ctx, qProductComponents, merchantID)
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
WHERE o.state = 'OPEN' and o.merchant_id = ?
`
	rowsExtras, err := tx.QueryContext(ctx, qExtras, merchantID)
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
WHERE o.state = 'OPEN' and o.merchant_id = ?
`
	rowsWithouts, err := tx.QueryContext(ctx, qWithouts, merchantID)
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
	rowsPayments, err := tx.QueryContext(ctx, qPayments, merchantID)
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
	rowsClients, err := tx.QueryContext(ctx, qClients, merchantID)
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
	rowsOrderComments, err := tx.QueryContext(ctx, qOrderComments, merchantID)
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
	rowsLocations, err := tx.QueryContext(ctx, qLocations, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsLocations.Close()

	// 10) configurable attributes + options for order items
	qConfigAttr := `
SELECT oi.order_item_id, ca.id, ca.title, ca.max_options, ca.attribute_type
FROM orders o
INNER JOIN orderitems oi on oi.order_id = o.order_id
INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsConfigAttr, err := tx.QueryContext(ctx, qConfigAttr, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttr.Close()

	qConfigAttrOptions := `
SELECT ca.id as configurable_attribute_id, oi.order_item_id, cao.id, cao.title, cao.extra_price, case when oic.id is null then 0 else 1 end as selected,
case when oic.quantity is null then 0 else oic.quantity end as quantity, cao.max_quantity
FROM orders o
INNER JOIN orderitems oi on oi.order_id = o.order_id
INNER JOIN product_configurable_attribute pca on pca.product_id = oi.product_id
INNER JOIN configurable_attributes ca on ca.id = pca.configurable_attribute_id
INNER JOIN configurable_attribute_options cao on cao.configurable_attribute_id = ca.id
LEFT JOIN order_item_configuration oic on oic.order_item_id = oi.order_item_id and cao.id = oic.configuration_attribute_option_id
WHERE o.state = 'OPEN' and o.merchant_id = ?`
	rowsConfigAttrOptions, err := tx.QueryContext(ctx, qConfigAttrOptions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsConfigAttrOptions.Close()

	// 11) delivery sessions (for sessions-only endpoint & to include in orders result)
	qDeliverySessions := `
SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
FROM delivery_session ds
INNER JOIN users u on u.user_id = ds.user_id
WHERE status IN ('1','PENDING')
AND ds.merchant_id = ?`
	rowsDeliverySessions, err := tx.QueryContext(ctx, qDeliverySessions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rowsDeliverySessions.Close()

	// ---------- Parse result sets and aggregate ----------

	// configurable attr options map[(order_item_id, attr_id)] -> []options
	type optKey struct {
		OrderItemID string
		AttrID      string
	}
	configurableOptionsMap := map[optKey][]models.ConfigurableOption{}

	for rowsConfigAttrOptions.Next() {
		var attrID sql.NullString
		var orderItemID sql.NullString
		var id sql.NullString
		var title sql.NullString
		var extraPrice int
		var selected sql.NullInt64
		var quantity sql.NullInt64
		var maxQuantity sql.NullInt64

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

	// configurable attributes per order_item
	configurableAttributesMap := map[string][]models.ConfigurableAttribute{}
	for rowsConfigAttr.Next() {
		var id sql.NullString
		var orderItemID sql.NullString
		var title sql.NullString
		var maxOptions sql.NullInt64
		var attrType sql.NullString

		if err := rowsConfigAttr.Scan(&orderItemID, &id, &title, &maxOptions, &attrType); err != nil {
			return nil, err
		}
		// fetch options from map
		key := optKey{OrderItemID: orderItemID.String, AttrID: id.String}
		opts := []models.ConfigurableOption{}
		for _, o := range configurableOptionsMap[key] {
			opts = append(opts, o)
		}
		configurableAttributesMap[orderItemID.String] = append(configurableAttributesMap[orderItemID.String], models.ConfigurableAttribute{
			ID:            id.String,
			OrderItemID:   orderItemID.String,
			AttributeType: attrType.String,
			Title:         title.String,
			MaxOptions:    int(maxOptions.Int64),
			Options:       nil, // we will map options inside order product assembly as "options" under attribute; keep structure consistent below
		})
	}

	// product components global list
	productComponents := []models.ComponentUsage{}
	for rowsProdComp.Next() {
		var productID sql.NullString
		var compID sql.NullInt64
		var name sql.NullString
		var price sql.NullInt64
		var status sql.NullInt64
		var qty sql.NullFloat64
		var uom sql.NullString

		if err := rowsProdComp.Scan(&productID, &compID, &name, &price, &status, &qty, &uom); err != nil {
			return nil, err
		}
		productComponents = append(productComponents, models.ComponentUsage{
			ComponentID:   compID.Int64,
			Name:          name.String,
			ProductID:     productID.String,
			Price:         price.Int64,
			Quantity:      qty.Float64,
			UnitOfMeasure: uom.String,
			Status:        int(status.Int64),
		})
	}

	// extras
	extras := []models.OrderProductExtra{}
	for rowsExtras.Next() {
		var orderItemID, id, orderID, productID, compID sql.NullString
		var name sql.NullString
		var price sql.NullFloat64
		if err := rowsExtras.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID, &price); err != nil {
			return nil, err
		}
		extras = append(extras, models.OrderProductExtra{
			ID:          id.String,
			OrderItemID: orderItemID.String,
			OrderID:     orderID.String,
			ProductID:   productID.String,
			Name:        name.String,
			ComponentID: compID.String,
			Price:       price.Float64,
		})
	}

	// withouts
	withouts := []models.OrderProductWithout{}
	for rowsWithouts.Next() {
		var orderItemID, id, orderID, productID, compID sql.NullString
		var name sql.NullString
		if err := rowsWithouts.Scan(&orderItemID, &id, &orderID, &productID, &name, &compID); err != nil {
			return nil, err
		}
		withouts = append(withouts, models.OrderProductWithout{
			ID:          id.String,
			OrderItemID: orderItemID.String,
			OrderID:     orderID.String,
			ProductID:   productID.String,
			Name:        name.String,
			ComponentID: compID.String,
			Price:       0,
		})
	}

	// products map (order_item_id -> product)
	productsMap := map[string]models.ProductEntry{}
	for rowsProducts.Next() {
		var orderID sql.NullInt64
		var quantity, paidQuantity sql.NullInt64
		var price sql.NullInt64
		var productID sql.NullString
		var name, productDesc, categName sql.NullString
		var orderItemID sql.NullString
		var isPaid, isDistributed sql.NullInt64
		var orderedOn sql.NullTime
		var basePrice sql.NullInt64
		var discountID sql.NullInt64
		var discountName sql.NullString
		var readyForDistribution, distributedQuantity sql.NullInt64
		var tvaIn, tvaDelivery, tvaTakeAway sql.NullFloat64
		var delayID sql.NullString
		var commentContent sql.NullString
		var commentUserID sql.NullString
		var commentCreation sql.NullTime
		var priceTakeAway, priceDelivery sql.NullInt64
		var imageURL sql.NullString
		var productionStatus sql.NullString
		var productionDoneQty sql.NullInt64
		var productionColor sql.NullString
		var availableIn, availableTakeAway, availableDelivery sql.NullBool

		if err := rowsProducts.Scan(&orderID, &quantity, &paidQuantity, &price, &productID, &name, &productDesc, &categName, &orderItemID, &isPaid, &isDistributed, &orderedOn, &basePrice, &discountID, &discountName, &readyForDistribution, &distributedQuantity, &tvaIn, &tvaDelivery, &tvaTakeAway, &delayID, &commentContent, &commentUserID, &commentCreation,
			&priceTakeAway, &priceDelivery, &imageURL, &productionStatus, &productionDoneQty, &productionColor, &availableIn, &availableTakeAway, &availableDelivery); err != nil {
			return nil, err
		}

		// collect components for this product
		currentComponents := []models.ComponentUsage{}
		for _, c := range productComponents {
			if c.ProductID == productID.String {
				currentComponents = append(currentComponents, c)
			}
		}
		// extras and withouts for this order_item
		currentExtras := []models.OrderProductExtra{}
		for _, e := range extras {
			if e.OrderItemID == orderItemID.String {
				currentExtras = append(currentExtras, e)
			}
		}
		currentWithouts := []models.OrderProductWithout{}
		for _, w := range withouts {
			if w.OrderItemID == orderItemID.String {
				currentWithouts = append(currentWithouts, w)
			}
		}

		// configuration attributes for this order_item
		currentConfigAttrs := []models.ConfigurableAttribute{}
		if arr, ok := configurableAttributesMap[orderItemID.String]; ok {
			// we must attach options for each attr
			for _, attr := range arr {
				// fetch options
				key := optKey{OrderItemID: orderItemID.String, AttrID: attr.ID}
				options := []models.ConfigurableOption{}
				for _, o := range configurableOptionsMap[key] {
					options = append(options, o)
				}
				attr.Options = options
				currentConfigAttrs = append(currentConfigAttrs, attr)
			}
		}

		// comment object (single) -- we keep same shape as PHP (user_id, content, creation_date)
		var comment interface{}
		if commentContent.Valid {
			comment = map[string]interface{}{
				"user_id":       commentUserID.String,
				"content":       commentContent.String,
				"creation_date": nilIfZeroTime(commentCreation),
			}
		} else {
			comment = map[string]interface{}{
				"user_id":       nil,
				"content":       nil,
				"creation_date": nil,
			}
		}

		op := models.ProductEntry{
			OrderID:                      orderID.Int64,
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
			Extra:                        currentExtras,
			Without:                      currentWithouts,
			Components:                   currentComponents,
			Customers:                    []interface{}{},
			Comment:                      comment,
		}
		op.Configuration.Attributes = currentConfigAttrs

		productsMap[orderItemID.String] = op
	}

	// payments
	payments := []models.Payment{}
	for rowsPayments.Next() {
		var orderID, paymentID sql.NullInt64
		var mop sql.NullString
		var amount sql.NullFloat64
		var paymentDate sql.NullTime
		var enabled sql.NullInt64
		if err := rowsPayments.Scan(&orderID, &paymentID, &mop, &amount, &paymentDate, &enabled); err != nil {
			return nil, err
		}
		payments = append(payments, models.Payment{
			OrderID:     orderID.Int64,
			PaymentID:   paymentID.Int64,
			MOP:         mop.String,
			Amount:      amount.Float64,
			PaymentDate: nullTimePtr(paymentDate),
			Enabled:     int(enabled.Int64),
		})
	}

	// clients (SNO)
	type SNOClient struct {
		UserCode    string
		UserName    string
		OrderItemID string
		Quantity    int
	}
	snoClients := []SNOClient{}
	for rowsClients.Next() {
		var userCode, userName, orderItemID sql.NullString
		var quantity sql.NullInt64
		if err := rowsClients.Scan(&userCode, &userName, &orderItemID, &quantity); err != nil {
			return nil, err
		}
		snoClients = append(snoClients, SNOClient{
			UserCode:    userCode.String,
			UserName:    userName.String,
			OrderItemID: orderItemID.String,
			Quantity:    int(quantity.Int64),
		})
	}

	// order-level comments
	orderComments := []models.OrderComment{}
	for rowsOrderComments.Next() {
		var id sql.NullInt64
		var userID sql.NullInt64
		var content sql.NullString
		var creationDate sql.NullTime
		var orderID sql.NullInt64
		var userName sql.NullString
		if err := rowsOrderComments.Scan(&id, &userID, &content, &creationDate, &orderID, &userName); err != nil {
			return nil, err
		}
		orderComments = append(orderComments, models.OrderComment{
			OrderID:      orderID.Int64,
			UserName:     nullStringToPtr(userName),
			Content:      content.String,
			CreationDate: nullTimePtr(creationDate),
		})
	}

	// locations
	locations := []models.Location{}
	for rowsLocations.Next() {
		var orderID, locationID sql.NullInt64
		var locationName, locationDesc sql.NullString
		if err := rowsLocations.Scan(&orderID, &locationID, &locationName, &locationDesc); err != nil {
			return nil, err
		}
		locations = append(locations, models.Location{
			OrderID:      orderID.Int64,
			LocationID:   locationID.Int64,
			LocationName: locationName.String,
			LocationDesc: nullStringToPtr(locationDesc),
		})
	}

	// delivery sessions build
	deliverySessions := []models.DeliverySession{}
	for rowsDeliverySessions.Next() {
		var id, userID sql.NullInt64
		var profilePic, firstName, lastName sql.NullString
		var lat, lng sql.NullFloat64
		var planningColor sql.NullString
		var status sql.NullString
		if err := rowsDeliverySessions.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			return nil, err
		}
		ds := models.DeliverySession{
			DeliverySessionID: id.Int64,
			Status:            status.String,
			Orders:            []models.Order{},
			DeliveryMan: models.DeliveryManInfo{
				UserID:         userID.Int64,
				FirstName:      firstName.String,
				LastName:       lastName.String,
				ProfilePicture: nullStringToPtr(profilePic),
				Lat:            nullFloat64Ptr(lat),
				Lng:            nullFloat64Ptr(lng),
				PlanningColor:  nullStringToPtr(planningColor),
			},
		}
		deliverySessions = append(deliverySessions, ds)
	}

	// Finally, iterate header rows and assemble order objects
	orders := []models.Order{}
	for rowsHeader.Next() {
		// scan header columns (mirror order in qHeader)
		var ord models.Order
		var orderID sql.NullInt64
		var orderNum, orderType, state, scheduled, brand, brandStatus, brandOrderID, brandOrderNum sql.NullString
		var estimatedReady sql.NullString
		var meansOfPayment sql.NullString
		var price sql.NullFloat64
		var TVA, HT sql.NullFloat64
		var monnaie sql.NullString
		var cutleryNotes sql.NullString
		var isPaid, isDistributed sql.NullInt64
		var dateCall sql.NullString
		var isDelivery sql.NullInt64
		var merchantApproval sql.NullInt64
		var deliveryFees sql.NullFloat64
		var lastUpdate sql.NullTime
		var fulfillmentType sql.NullString
		var useCustomerTemporaryAddress sql.NullInt64
		var creationDate sql.NullTime
		var placesSettings sql.NullString
		var pagerNumber sql.NullString
		// customer fields
		var customerID sql.NullInt64
		var customerName, customerTel sql.NullString
		var customerLat, customerLng sql.NullFloat64
		var customerTemporaryPhone, customerTemporaryPhoneCode sql.NullString
		var customerNbOrders sql.NullInt64
		var customerZoneCode sql.NullString
		var customerAddress, customerFloorNumber, customerDoorNumber, customerAdditionalAddress sql.NullString
		var customerBusinessName, customerBirthdate, customerAdditionalInfo sql.NullString
		var customerTemporaryAddress sql.NullString
		var customerTemporaryLat, customerTemporaryLng sql.NullFloat64
		var customerTemporaryFloorNumber, customerTemporaryDoorNumber, customerTemporaryAdditionalAddress sql.NullString
		// responsible fields
		var userID sql.NullInt64
		var userLat, userLng sql.NullFloat64
		var deliveryTel sql.NullString
		var userName sql.NullString
		// delivery session id + priority
		var deliverySessionID sql.NullInt64
		var priority sql.NullInt64

		if err := rowsHeader.Scan(&orderID, &orderNum, &orderType, &state, &scheduled, &brand, &brandStatus, &brandOrderID, &brandOrderNum, &estimatedReady, &meansOfPayment, &price, &TVA, &HT, &monnaie, &cutleryNotes,
			&isPaid, &isDistributed, &dateCall, &isDelivery, &merchantApproval, &deliveryFees, &lastUpdate, &fulfillmentType, &useCustomerTemporaryAddress, &creationDate, &placesSettings, &pagerNumber,
			&customerID, &customerName, &customerTel, &customerLat, &customerLng, &customerTemporaryPhone, &customerTemporaryPhoneCode, &customerNbOrders, &customerZoneCode,
			&customerAddress, &customerFloorNumber, &customerDoorNumber, &customerAdditionalAddress, &customerBusinessName, &customerBirthdate, &customerAdditionalInfo,
			&customerTemporaryAddress, &customerTemporaryLat, &customerTemporaryLng, &customerTemporaryFloorNumber, &customerTemporaryDoorNumber, &customerTemporaryAdditionalAddress,
			&userID, &userLat, &userLng, &deliveryTel, &userName,
			&deliverySessionID, &priority); err != nil {
			return nil, err
		}

		ord.OrderID = orderID.Int64
		ord.OrderNum = nullStringToPtr(orderNum)
		ord.Brand = nullStringToPtr(brand)
		ord.BrandOrderID = nullStringToPtr(brandOrderID)
		ord.BrandOrderNum = nullStringToPtr(brandOrderNum)
		ord.BrandStatus = nullStringToPtr(brandStatus)
		ord.OrderType = nullStringToPtr(orderType)
		ord.CutleryNotes = nullStringToPtr(cutleryNotes)
		ord.State = nullStringToPtr(state)
		ord.Scheduled = nullStringToPtr(scheduled)
		ord.TTC = price.Float64
		ord.TVA = nullFloat64ToPtr(TVA)
		ord.HT = nullFloat64ToPtr(HT)
		ord.PlacesSettings = nullStringToPtr(placesSettings)
		ord.PagerNumber = nullStringToPtr(pagerNumber)
		ord.IsPaid = int(isPaid.Int64)
		ord.IsDistributed = int(isDistributed.Int64)
		ord.IsSNO = (userID.Int64 == -1)
		ord.CallHour = nullStringToPtr(dateCall)
		ord.EstimatedReady = nullStringToPtr(estimatedReady)
		ord.IsDelivery = int(isDelivery.Int64)
		ord.MerchantApproval = int(merchantApproval.Int64)
		ord.DeliveryFees = nullFloat64ToPtr(deliveryFees)
		ord.CreationDate = nullTimePtr(creationDate)
		ord.FulfillmentType = nullStringToPtr(fulfillmentType)
		ord.LastUpdate = nullTimePtr(lastUpdate)

		// customer object mapping following the PHP logic (temporary address override)
		var cust models.Customer
		if customerID.Valid {
			useTemp := useCustomerTemporaryAddress.Int64 == 1
			if useTemp {
				cust.CustomerAddress = nullStringToPtr(customerTemporaryAddress)
				cust.CustomerLat = nullFloat64Ptr(customerTemporaryLat)
				cust.CustomerLng = nullFloat64Ptr(customerTemporaryLng)
				cust.CustomerFloorNumber = nullStringToPtr(customerTemporaryFloorNumber)
				cust.CustomerDoorNumber = nullStringToPtr(customerTemporaryDoorNumber)
				cust.CustomerAdditionalAddress = nullStringToPtr(customerTemporaryAdditionalAddress)
			} else {
				cust.CustomerAddress = nullStringToPtr(customerAddress)
				cust.CustomerLat = nullFloat64Ptr(customerLat)
				cust.CustomerLng = nullFloat64Ptr(customerLng)
				cust.CustomerFloorNumber = nullStringToPtr(customerFloorNumber)
				cust.CustomerDoorNumber = nullStringToPtr(customerDoorNumber)
				cust.CustomerAdditionalAddress = nullStringToPtr(customerAdditionalAddress)
			}
			cust.CustomerID = &customerID.Int64
			cust.CustomerName = nullStringToPtr(customerName)
			cust.CustomerTel = nullStringToPtr(customerTel)
			cust.CustomerTemporaryPhone = nullStringToPtr(customerTemporaryPhone)
			cust.CustomerTemporaryPhoneCode = nullStringToPtr(customerTemporaryPhoneCode)
			nbOrders := int(customerNbOrders.Int64)
			cust.CustomerNbOrders = &nbOrders
			cust.CustomerAdditionalInfo = nullStringToPtr(customerAdditionalInfo)
			cust.CustomerZoneCode = nullStringToPtr(customerZoneCode)
		} else {
			// nil customer
			ord.Customer = nil
		}
		if customerID.Valid {
			ord.Customer = &cust
		}

		// payments for this order
		actualPayments := []models.Payment{}
		for _, p := range payments {
			if p.OrderID == ord.OrderID {
				actualPayments = append(actualPayments, p)
			}
		}
		ord.Payments = actualPayments

		// comments for the order
		actualComments := []models.OrderComment{}
		for _, c := range orderComments {
			if c.OrderID == ord.OrderID {
				actualComments = append(actualComments, c)
			}
		}
		ord.Comments = actualComments

		// locations
		actualLocations := []models.Location{}
		for _, l := range locations {
			if l.OrderID == ord.OrderID {
				// remove order_id in returned location as PHP did
				actualLocations = append(actualLocations, models.Location{
					LocationID:   l.LocationID,
					LocationName: l.LocationName,
					LocationDesc: l.LocationDesc,
				})
			}
		}
		ord.Location = actualLocations

		// responsible
		if userID.Valid {
			ord.Responsible = &models.Responsible{
				ID:   &userID.Int64,
				Lat:  nullFloat64Ptr(userLat),
				Lng:  nullFloat64Ptr(userLng),
				Tel:  nullStringToPtr(deliveryTel),
				Name: nullStringToPtr(userName),
			}
		}

		// products attached to this order
		actualProducts := []models.ProductEntry{}
		for _, p := range productsMap {
			if p.OrderID == ord.OrderID {
				// customers (SNO) attach
				custs := []interface{}{}
				for _, sc := range snoClients {
					if sc.OrderItemID == p.OrderItemID {
						custs = append(custs, map[string]interface{}{
							"user_code": sc.UserCode,
							"user_name": sc.UserName,
							"quantity":  sc.Quantity,
						})
					}
				}
				p.Customers = custs

				// configuration attributes -> already in p.Configuration.Attributes
				actualProducts = append(actualProducts, p)
			}
		}
		ord.Products = actualProducts

		ord.Priority = nilIfNullInt64(priority)
		orders = append(orders, ord)

		// if this order belongs to a delivery session, attach to that session
		if deliverySessionID.Valid {
			for i := range deliverySessions {
				if deliverySessions[i].DeliverySessionID == deliverySessionID.Int64 {
					deliverySessions[i].Orders = append(deliverySessions[i].Orders, ord)
					break
				}
			}
		}
	}

	// IMPORTANT: we also include delivery sessions inside the orders response (they are already populated with orders)
	resp := &models.PendingOrdersResponse{
		Orders:           orders,
		DeliverySessions: deliverySessions,
		Timings:          nil,
	}

	// commit tx (read-only commit is OK)
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return resp, nil
}

// GetDeliverySessions returns only the delivery sessions (same retrieval as above but returns sessions only)
func (r *LegacyOrdersRepository) GetDeliverySessions(ctx context.Context, merchantID string) ([]models.DeliverySession, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	qDeliverySessions := `
SELECT id, u.user_id, u.profile_picture, u.first_name, u.last_name, u.lat, u.lng, u.planning_color, ds.status
FROM delivery_session ds
INNER JOIN users u on u.user_id = ds.user_id
WHERE status IN ('1','PENDING')
AND ds.merchant_id = ?`
	rows, err := tx.QueryContext(ctx, qDeliverySessions, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []models.DeliverySession{}
	for rows.Next() {
		var id, userID sql.NullInt64
		var profilePic, firstName, lastName sql.NullString
		var lat, lng sql.NullFloat64
		var planningColor sql.NullString
		var status sql.NullString

		if err := rows.Scan(&id, &userID, &profilePic, &firstName, &lastName, &lat, &lng, &planningColor, &status); err != nil {
			return nil, err
		}
		ds := models.DeliverySession{
			DeliverySessionID: id.Int64,
			Status:            status.String,
			Orders:            []models.Order{},
			DeliveryMan: models.DeliveryManInfo{
				UserID:         userID.Int64,
				FirstName:      firstName.String,
				LastName:       lastName.String,
				ProfilePicture: nullStringToPtr(profilePic),
				Lat:            nullFloat64Ptr(lat),
				Lng:            nullFloat64Ptr(lng),
				PlanningColor:  nullStringToPtr(planningColor),
			},
		}
		sessions = append(sessions, ds)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sessions, nil
}

// ---------- helper functions to handle sql.Null* -> pointers ----------

func nullStringToPtr(s sql.NullString) *string {
	if s.Valid {
		ss := s.String
		return &ss
	}
	return nil
}
func nullFloat64ToPtr(f sql.NullFloat64) *float64 {
	if f.Valid {
		v := f.Float64
		return &v
	}
	return nil
}
func nullTimePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		v := t.Time
		return &v
	}
	return nil
}
func nilIfZeroTime(t sql.NullTime) *time.Time {
	if t.Valid {
		return &t.Time
	}
	return nil
}
func nullInt64ToPtr(n sql.NullInt64) *int64 {
	if n.Valid {
		v := n.Int64
		return &v
	}
	return nil
}
func nullFloat64ToPtrOrNil(n sql.NullFloat64) *float64 {
	if n.Valid {
		v := n.Float64
		return &v
	}
	return nil
}
func nullFloat64Ptr(f sql.NullFloat64) *float64 {
	if f.Valid {
		v := f.Float64
		return &v
	}
	return nil
}
func nilIfNullFloat64(discountID sql.NullInt64, price float64) *float64 {
	if !discountID.Valid {
		return nil
	}
	v := price
	return &v
}
func nilIfNullInt64Discount(discountID sql.NullInt64, price int64) *int64 {
	if !discountID.Valid {
		return nil
	}
	v := price
	return &v
}
func nilIfNullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}
func nilIfNullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
func nilIfNullInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}
