package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
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

// GetPendingOrders : Récupère toutes les commandes en cours (Optimisé)
func (r *OrdersRepository) GetPendingOrders(ctx context.Context, merchantID, app string) (*models.PendingOrdersResponse, error) {
	r.log.Info("GetPendingOrders START", zap.String("merchant_id", merchantID))

	// On a besoin du repo session pour récupérer les sessions à la fin
	deliverySessionRepo := NewDeliverySessionsRepository(r.db, r.log)

	// ========================================================================
	// ÉTAPE 1 : OPTIMISATION - Récupérer les IDs d'abord
	// ========================================================================

	// 1.a. On construit la clause WHERE complexe ici
	criteria := " AND ((o.state IN ('OPEN') AND o.brand_status NOT IN('ONLINE_PAYMENT_PENDING')) OR ds.id IS NOT NULL) "

	// Ajout filtre APP
	if app == "1" || app == "WR_DELIVERY" {
		criteria += " AND o.order_type = 'DELIVERY' AND o.fulfillment_type = 'DELIVERY_BY_RESTAURANT' "
	} else if app == "2" || app == "WR_WAITER" {
		criteria += " AND o.order_type NOT IN ('DELIVERY','TAKE_AWAY') "
	}

	// 1.b. Requête légère pour récupérer UNIQUEMENT les IDs
	// On doit inclure les JOINs ici pour que le filtre fonctionne (alias 'o' et 'ds')
	qIDs := `SELECT DISTINCT o.order_id
             FROM orders o
             LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id
             LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id AND ds.status IN ('1','PENDING')
             WHERE o.merchant_id = ? ` + criteria

	rows, err := r.db.QueryContext(ctx, qIDs, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending order ids: %w", err)
	}
	defer rows.Close()

	var orderIDs []string
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, oid)
	}

	// ========================================================================
	// CAS VIDE : Si aucune commande ne correspond, on sort tout de suite
	// ========================================================================
	if len(orderIDs) == 0 {
		// On retourne vide, mais on récupère quand même les sessions vides si nécessaire,
		// ou on retourne tout vide. Selon ton besoin métier.
		// Ici je retourne tout vide pour être rapide.
		return &models.PendingOrdersResponse{
			Orders:           []models.Order{},
			DeliverySessions: []models.DeliverySession{},
		}, nil
	}

	// ========================================================================
	// ÉTAPE 2 : Appeler le constructeur avec le filtre OPTIMISÉ (IN)
	// ========================================================================

	// Construction de la chaîne "IN ('id1', 'id2')"
	idsStr := ""
	for i, oid := range orderIDs {
		if i > 0 {
			idsStr += ","
		}
		idsStr += fmt.Sprintf("'%s'", oid)
	}

	// Le filtre magique qui va rendre les 11 requêtes suivantes instantanées
	filterOptimized := fmt.Sprintf(" AND o.order_id IN (%s) ", idsStr)

	orders, err := r.fetchAndBuildOrders(ctx, merchantID, filterOptimized)
	if err != nil {
		return nil, err
	}

	// ========================================================================
	// ÉTAPE 3 : Récupérer les sessions et finaliser
	// ========================================================================

	// Récupérer les sessions (spécifique à cet endpoint)
	// Note : comme on est dans le même package 'repositories', on a accès aux méthodes privées (minuscule)
	sessions, err := deliverySessionRepo.fetchDeliverySessions(ctx, merchantID, "status IN ('1','PENDING')")
	if err != nil {
		return nil, err
	}

	// Assemblage final
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
	r.log.Info("fetchAndBuildOrders START", zap.String("merchant_id", merchantID))

	// Begin transaction (read-only)
	// Note: On utilise le ctx parent. Si la requête HTTP est annulée, la transaction s'arrêtera proprement.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
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
		WHERE o.merchant_id = ? ` + additionalFilter

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
				OrderID: orderID.String, LocationID: locationID.String, LocationName: locationName.String, LocationDesc: nullStringToPtr(locationDesc),
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
		WHERE o.merchant_id = ? ` + additionalFilter

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
		WHERE o.merchant_id = ? ` + additionalFilter

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
		WHERE oi.merchant_id = ? ` + additionalFilter

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
		WHERE o.merchant_id = ? ` + additionalFilter

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
		WHERE o.merchant_id = ? ` + additionalFilter

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
		WHERE o.merchant_id = ? and oc.order_item_id is null ` + additionalFilter

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
				OrderID: orderID.String, UserName: nullStringToPtr(userName), Content: content.String, CreationDate: nullTimePtr(creationDate),
			})
		}
		r.log.Info("order_comment loaded")
	}

	// --- 6. PAYMENTS ---
	paymentsByOrderID := map[string][]models.Payment{}
	{
		step := "payments"
		q := `
		SELECT oc.id, oc.user_id, oc.content, oc.creation_date, oc.order_id, u.userName
		from order_comments oc
		inner join orders o on o.order_id = oc.order_id
		left join users u on u.user_id = oc.user_id
		LEFT JOIN delivery_session_order dso ON dso.order_id = o.order_id 
		LEFT JOIN delivery_session ds ON ds.id = dso.delivery_session_id 
		WHERE o.merchant_id = ? and oc.order_item_id is null ` + additionalFilter

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
				return nil, err
			}
			paymentsByOrderID[orderID.String] = append(paymentsByOrderID[orderID.String], models.Payment{
				OrderID: orderID.String, PaymentID: paymentID.Int64, MOP: mop.String, Amount: amount.Float64, PaymentDate: nullTimePtr(paymentDate), Enabled: int(enabled.Int64),
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
		WHERE oi.quantity > 0 AND o.merchant_id = ? ` + additionalFilter

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
					OrderID: orderID.String, UserName: &commentUserID.String, Content: commentContent.String, CreationDate: nullTimePtr(commentCreation),
				}
			} else {
				comment = models.OrderComment{}
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
		r.log.Info("products loaded")
	}

	// --- 1. HEADER ---
	// On injecte 'additionalFilter' qui contient soit "state='OPEN'" soit "order_id=X"
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
		WHERE o.merchant_id = ? ` + additionalFilter

		rows, err := runQuery(step, q, merchantID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var ord models.Order
			var customerID, customerNbOrders, priority, isDelivery, useCustomerTemporaryAddress, price, TVA, HT, deliveryFees, placesSettings sql.NullInt64
			var orderID, orderNum, orderType, state, brand, brandStatus, brandOrderID, brandOrderNum, estimatedReady, meansOfPayment, monnaie, cutleryNotes, dateCall, fulfillmentType, pagerNumber, merchantApproval, deliverySessionID, userID sql.NullString
			var customerLat, customerLng, customerTemporaryLat, customerTemporaryLng, userLat, userLng sql.NullFloat64
			var lastUpdate, creationDate sql.NullTime
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
			if userID.Valid && userID.String != "0" && false {
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
		r.log.Info("header loaded")
	}

	r.log.Info(
		"fetchAndBuildOrders END",
		zap.Int("orders_count", len(orders)),
		zap.Duration("total_duration", time.Since(startTotal)))
	return orders, nil
}
