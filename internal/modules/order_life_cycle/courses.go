package order_life_cycle

// Suites : envoi des plats d'une commande sur place en plusieurs temps
// (entrées, puis plats, puis desserts). Voir docs/order-lifecycle.md,
// section « Suites », et la migration 212.
//
// Règles :
//   - orderitems.course porte le numéro de suite de la ligne, order_courses
//     l'état de chaque suite (mode d'envoi, envoi programmé, envoi réel) ;
//   - les suites partent toujours dans l'ordre : envoyer la suite N envoie les
//     suites < N encore en attente. Les suites envoyées forment donc toujours
//     le début de la liste ;
//   - une suite envoyée est verrouillée : ses lignes n'en sortent plus ;
//   - seules les commandes sur place (IN) ont des suites.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

const (
	orderTypeIn = "IN"

	// maxCourseDelayMinutes borne le délai d'envoi automatique d'une suite.
	maxCourseDelayMinutes = 240
	// maxCourseFireDelaySeconds borne le délai d'envoi de « Réclamer »
	// (fenêtre d'annulation côté caisse, 5 s en pratique).
	maxCourseFireDelaySeconds = 60
)

// FiredCourseOrder : commande dont au moins une suite vient de partir.
type FiredCourseOrder struct {
	OrderID    string
	MerchantID string
}

// AssignCoursesFromSteps attribue un numéro de suite à chaque ligne à partir
// de l'étape de sa catégorie : les étapes présentes dans la commande sont
// numérotées 1, 2, 3… dans l'ordre (une commande « plat + dessert » donne
// S1 = plat, S2 = dessert). Une ligne sans étape reste hors suite (nil).
func AssignCoursesFromSteps(steps []*int) []*int {
	distinct := map[int]bool{}
	for _, s := range steps {
		if s != nil && *s > 0 {
			distinct[*s] = true
		}
	}
	ordered := make([]int, 0, len(distinct))
	for s := range distinct {
		ordered = append(ordered, s)
	}
	sort.Ints(ordered)
	rank := make(map[int]int, len(ordered))
	for i, s := range ordered {
		rank[s] = i + 1
	}

	courses := make([]*int, len(steps))
	for i, s := range steps {
		if s == nil || *s <= 0 {
			continue
		}
		c := rank[*s]
		courses[i] = &c
	}
	return courses
}

// normalizeCourseDelay : nil, 0 ou négatif = envoi manuel ; borné à 4 h.
func normalizeCourseDelay(delay *int) *int {
	if delay == nil || *delay <= 0 {
		return nil
	}
	d := *delay
	if d > maxCourseDelayMinutes {
		d = maxCourseDelayMinutes
	}
	return &d
}

// usesCourses : la caisse (ou Scan&Order) gère les suites pour cette
// commande. Un payload sans champ « courses » vient d'un client qui ne les
// connaît pas : on ne touche alors à rien.
func usesCourses(order *models.OrderRequest) bool {
	return order != nil && order.Courses != nil
}

// courseForInsert : numéro de suite à écrire à l'insertion d'une ligne.
func courseForInsert(order *models.OrderRequest, p *models.OrderProductPayload) *int {
	if !usesCourses(order) || order.OrderType != orderTypeIn || p.Course == nil || *p.Course <= 0 {
		return nil
	}
	c := *p.Course
	return &c
}

// writeOrderItemCourses écrit le numéro de suite des lignes EXISTANTES de la
// commande (les nouvelles l'ont reçu à l'insertion). Une ligne ne change de
// suite que si sa suite actuelle est encore en attente : une ligne déjà
// partie en cuisine (suite envoyée, ou hors suite) n'est jamais remise en
// attente.
func (r *OrdersLifeCycleRepository) writeOrderItemCourses(ctx context.Context, orderID string, products []models.OrderProductPayload) error {
	db := dbx.GetDB(ctx, r.database)
	for i := range products {
		p := &products[i]
		if p.OrderItemID == nil || strings.TrimSpace(*p.OrderItemID) == "" {
			continue
		}
		var course *int
		if p.Course != nil && *p.Course > 0 {
			c := *p.Course
			course = &c
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE orderitems oi
			SET course = ?
			WHERE oi.order_id = ? AND oi.order_item_id = ?
			  AND oi.course IS DISTINCT FROM ?
			  AND EXISTS (
			      SELECT 1 FROM order_courses oc
			      WHERE oc.order_id = oi.order_id AND oc.course = oi.course AND oc.fired_at IS NULL
			  )`,
			course, orderID, *p.OrderItemID, course); err != nil {
			return fmt.Errorf("write order item course (order_item_id=%s): %w", *p.OrderItemID, err)
		}
	}
	return nil
}

// clearOrderCourses retire toutes les suites d'une commande (passée hors
// sur place) : toutes ses lignes partent tout de suite.
func (r *OrdersLifeCycleRepository) clearOrderCourses(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)
	if _, err := db.ExecContext(ctx,
		`UPDATE orderitems SET course = NULL WHERE order_id = ? AND course IS NOT NULL`, orderID); err != nil {
		return fmt.Errorf("clear order item courses: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM order_courses WHERE order_id = ?`, orderID); err != nil {
		return fmt.Errorf("clear order courses: %w", err)
	}
	return nil
}

// SyncOrderCourses aligne order_courses sur les lignes de la commande et les
// réglages reçus, après l'écriture des lignes :
//  1. supprime les suites en attente qui n'ont plus de ligne ;
//  2. crée les suites manquantes et applique les réglages des suites en
//     attente (un changement de délai annule l'envoi programmé, recalculé
//     ensuite) ;
//  3. envoie les suites marquées FireNow ;
//  4. programme l'envoi de la prochaine suite à délai.
func (r *OrdersLifeCycleRepository) SyncOrderCourses(ctx context.Context, merchantID, orderID string, settings []models.OrderCoursePayload, userID string) error {
	db := dbx.GetDB(ctx, r.database)

	present := map[int]bool{}
	{
		rows, err := db.QueryContext(ctx, `
			SELECT DISTINCT course FROM orderitems
			WHERE order_id = ? AND course IS NOT NULL AND quantity > 0`, orderID)
		if err != nil {
			return fmt.Errorf("list order item courses: %w", err)
		}
		for rows.Next() {
			var c int
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return err
			}
			present[c] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	{
		rows, err := db.QueryContext(ctx,
			`SELECT course FROM order_courses WHERE order_id = ? AND fired_at IS NULL`, orderID)
		if err != nil {
			return fmt.Errorf("list pending order courses: %w", err)
		}
		var stale []int
		for rows.Next() {
			var c int
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return err
			}
			if !present[c] {
				stale = append(stale, c)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range stale {
			if _, err := db.ExecContext(ctx,
				`DELETE FROM order_courses WHERE order_id = ? AND course = ? AND fired_at IS NULL`, orderID, c); err != nil {
				return fmt.Errorf("delete stale order course %d: %w", c, err)
			}
		}
	}

	settingsByCourse := make(map[int]models.OrderCoursePayload, len(settings))
	for _, s := range settings {
		if s.Course > 0 {
			settingsByCourse[s.Course] = s
		}
	}

	courses := make([]int, 0, len(present))
	for c := range present {
		courses = append(courses, c)
	}
	sort.Ints(courses)

	fireUpTo := 0
	for _, c := range courses {
		s, hasSettings := settingsByCourse[c]
		if !hasSettings {
			if _, err := db.ExecContext(ctx, `
				INSERT INTO order_courses (order_id, merchant_id, course, delay_minutes)
				VALUES (?, ?, ?, NULL)
				ON CONFLICT (order_id, course) DO NOTHING`,
				orderID, merchantID, c); err != nil {
				return fmt.Errorf("insert order course %d: %w", c, err)
			}
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO order_courses (order_id, merchant_id, course, delay_minutes)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (order_id, course) DO UPDATE SET
				delay_minutes = EXCLUDED.delay_minutes,
				fire_at = CASE
					WHEN order_courses.delay_minutes IS DISTINCT FROM EXCLUDED.delay_minutes THEN NULL
					ELSE order_courses.fire_at
				END
			WHERE order_courses.fired_at IS NULL`,
			orderID, merchantID, c, normalizeCourseDelay(s.DelayMinutes)); err != nil {
			return fmt.Errorf("upsert order course %d: %w", c, err)
		}
		if s.FireNow && c > fireUpTo {
			fireUpTo = c
		}
	}

	if fireUpTo > 0 {
		if _, err := r.FireOrderCoursesUpTo(ctx, merchantID, orderID, fireUpTo, userID); err != nil {
			return err
		}
		return nil // FireOrderCoursesUpTo programme déjà les délais
	}
	return r.scheduleDelayedOrderCourses(ctx, orderID)
}

// FireOrderCoursesUpTo envoie la suite course et toutes les suites
// précédentes encore en attente. Renvoie le nombre de suites envoyées.
func (r *OrdersLifeCycleRepository) FireOrderCoursesUpTo(ctx context.Context, merchantID, orderID string, course int, userID string) (int64, error) {
	db := dbx.GetDB(ctx, r.database)
	var firedBy *string
	if strings.TrimSpace(userID) != "" {
		firedBy = &userID
	}
	res, err := db.ExecContext(ctx, `
		UPDATE order_courses
		SET fired_at = now(), fired_by = ?
		WHERE order_id = ? AND merchant_id = ? AND course <= ? AND fired_at IS NULL`,
		firedBy, orderID, merchantID, course)
	if err != nil {
		return 0, fmt.Errorf("fire order courses: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := r.scheduleDelayedOrderCourses(ctx, orderID); err != nil {
		return n, err
	}
	return n, nil
}

// ScheduleOrderCourseFire programme l'envoi de la suite dans delaySeconds
// (fenêtre d'annulation de « Réclamer ») ; la tâche FireDueOrderCourses
// l'exécute. ErrNotFound si la suite n'existe pas, ErrCourseAlreadyFired si
// elle est déjà partie.
func (r *OrdersLifeCycleRepository) ScheduleOrderCourseFire(ctx context.Context, merchantID, orderID string, course, delaySeconds int) error {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE order_courses
		SET fire_at = now() + make_interval(secs => CAST(? AS integer))
		WHERE order_id = ? AND merchant_id = ? AND course = ? AND fired_at IS NULL`,
		delaySeconds, orderID, merchantID, course)
	if err != nil {
		return fmt.Errorf("schedule order course fire: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r.courseNotPendingError(ctx, merchantID, orderID, course)
	}
	return nil
}

// CancelOrderCourseFire annule l'envoi programmé d'une suite en attente, qui
// repasse en envoi manuel (le délai éventuel est retiré : « Annuler » veut
// dire « ne l'envoie pas »).
func (r *OrdersLifeCycleRepository) CancelOrderCourseFire(ctx context.Context, merchantID, orderID string, course int) error {
	db := dbx.GetDB(ctx, r.database)
	res, err := db.ExecContext(ctx, `
		UPDATE order_courses
		SET fire_at = NULL, delay_minutes = NULL
		WHERE order_id = ? AND merchant_id = ? AND course = ? AND fired_at IS NULL`,
		orderID, merchantID, course)
	if err != nil {
		return fmt.Errorf("cancel order course fire: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return r.courseNotPendingError(ctx, merchantID, orderID, course)
	}
	return nil
}

func (r *OrdersLifeCycleRepository) courseNotPendingError(ctx context.Context, merchantID, orderID string, course int) error {
	db := dbx.GetDB(ctx, r.database)
	var fired bool
	err := db.QueryRowContext(ctx, `
		SELECT fired_at IS NOT NULL FROM order_courses
		WHERE order_id = ? AND merchant_id = ? AND course = ?`,
		orderID, merchantID, course).Scan(&fired)
	if err != nil {
		return models.ErrNotFound
	}
	if fired {
		return models.ErrCourseAlreadyFired
	}
	return models.ErrNotFound
}

// scheduleDelayedOrderCourses programme la première suite en attente dont
// l'envoi est automatique : délai compté depuis l'envoi de la suite
// précédente (depuis maintenant s'il n'y en a pas). Jamais dans le passé :
// un délai réglé après coup part à la prochaine passe de la tâche.
func (r *OrdersLifeCycleRepository) scheduleDelayedOrderCourses(ctx context.Context, orderID string) error {
	db := dbx.GetDB(ctx, r.database)
	if _, err := db.ExecContext(ctx, `
		UPDATE order_courses oc
		SET fire_at = GREATEST(
			now(),
			COALESCE(
				(SELECT MAX(p.fired_at) FROM order_courses p WHERE p.order_id = oc.order_id AND p.course < oc.course),
				now()
			) + make_interval(mins => oc.delay_minutes)
		)
		WHERE oc.order_id = ?
		  AND oc.fired_at IS NULL AND oc.fire_at IS NULL AND oc.delay_minutes IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM order_courses q
		      WHERE q.order_id = oc.order_id AND q.course < oc.course AND q.fired_at IS NULL
		  )`, orderID); err != nil {
		return fmt.Errorf("schedule delayed order courses: %w", err)
	}
	return nil
}

// FireDueOrderCourses envoie les suites dont l'envoi programmé est échu
// (avec les suites précédentes encore en attente), sur les commandes encore
// ouvertes. Sûr entre instances : l'UPDATE est atomique, une suite ne part
// qu'une fois. Les envois programmés de commandes closes ou annulées sont
// abandonnés.
func (r *OrdersLifeCycleRepository) FireDueOrderCourses(ctx context.Context) ([]FiredCourseOrder, error) {
	db := dbx.GetDB(ctx, r.database)

	rows, err := db.QueryContext(ctx, `
		WITH due AS (
			SELECT oc.order_id, MAX(oc.course) AS course
			FROM order_courses oc
			INNER JOIN orders o ON o.order_id = oc.order_id
			WHERE oc.fired_at IS NULL AND oc.fire_at IS NOT NULL AND oc.fire_at <= now()
			  AND o.state = 'OPEN'
			  AND COALESCE(o.brand_status, '') NOT IN ('DENIED', 'CANCELED')
			GROUP BY oc.order_id
		)
		UPDATE order_courses oc
		SET fired_at = now()
		FROM due
		WHERE oc.order_id = due.order_id AND oc.course <= due.course AND oc.fired_at IS NULL
		RETURNING oc.order_id, oc.merchant_id`)
	if err != nil {
		return nil, fmt.Errorf("fire due order courses: %w", err)
	}
	seen := map[string]bool{}
	var fired []FiredCourseOrder
	for rows.Next() {
		var o FiredCourseOrder
		if err := rows.Scan(&o.OrderID, &o.MerchantID); err != nil {
			rows.Close()
			return nil, err
		}
		if !seen[o.OrderID] {
			seen[o.OrderID] = true
			fired = append(fired, o)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, o := range fired {
		if err := r.scheduleDelayedOrderCourses(ctx, o.OrderID); err != nil {
			logger.FromContext(ctx).Warn("schedule next delayed course failed",
				zap.String("order_id", o.OrderID), zap.Error(err))
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE order_courses oc
		SET fire_at = NULL
		FROM orders o
		WHERE o.order_id = oc.order_id
		  AND oc.fired_at IS NULL AND oc.fire_at IS NOT NULL AND oc.fire_at <= now()
		  AND (o.state <> 'OPEN' OR COALESCE(o.brand_status, '') IN ('DENIED', 'CANCELED'))`); err != nil {
		logger.FromContext(ctx).Warn("drop due courses of closed orders failed", zap.Error(err))
	}

	return fired, nil
}

// CourseStepsForProducts : étape de catégorie de chaque produit qui en a une.
func (r *OrdersLifeCycleRepository) CourseStepsForProducts(ctx context.Context, merchantID string, productIDs []string) (map[string]int, error) {
	steps := map[string]int{}
	if len(productIDs) == 0 {
		return steps, nil
	}
	db := dbx.GetDB(ctx, r.database)

	placeholders := make([]string, len(productIDs))
	args := make([]interface{}, 0, len(productIDs)+1)
	args = append(args, merchantID)
	for i, id := range productIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT p.product_id, pc.course_step
		FROM products p
		INNER JOIN productcateg pc ON pc.merchant_categ_id = p.category AND pc.merchant_id = p.merchant_id
		WHERE p.merchant_id = ? AND p.product_id IN (`+strings.Join(placeholders, ", ")+`)
		  AND pc.course_step IS NOT NULL`, args...)
	if err != nil {
		return nil, fmt.Errorf("course steps for products: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var productID string
		var step int
		if err := rows.Scan(&productID, &step); err != nil {
			return nil, err
		}
		steps[productID] = step
	}
	return steps, rows.Err()
}

// SnoCourseDelayMinutes : délai d'envoi des suites suivantes des commandes
// Scan&Order sur place (0 = manuel).
func (r *OrdersLifeCycleRepository) SnoCourseDelayMinutes(ctx context.Context, merchantID string) (int, error) {
	db := dbx.GetDB(ctx, r.database)
	var delay int
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(sno_course_delay_minutes, 0) FROM merchant_parameters WHERE merchant_id = ?`,
		merchantID).Scan(&delay)
	if err != nil {
		return 0, err
	}
	return delay, nil
}
