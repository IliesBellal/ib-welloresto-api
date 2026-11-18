package order_life_cycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"

	"go.uber.org/zap"
)

// Heure cuisine manuelle (docs/order-lifecycle.md, « Heure cuisine
// manuelle ») : le personnel peut AVANCER la deadline cuisine d'une commande
// (production_ready_at) sans toucher à son heure de livraison, pour produire
// ensemble des commandes proches. orders.production_ready_manual dit que la
// valeur en base est ce choix et non le calcul automatique
// (resolveProductionReadyAt).
//
// v1 : on ne peut que l'avancer (jamais plus tard que l'heure calculée, ce
// qui retarderait la livraison), et pas sur une commande Uber Eats / Deliveroo
// (l'heure de retrait y appartient à la plateforme).

// platingBuffer : durée tampon, en dur et identique pour toutes les commandes
// programmées, entre la fin de cuisson (production_ready_at) et l'heure
// promise (estimated_ready) : mise en assiette, packaging emporter,
// préparation de la livraison. Quatre minutes : la livraison (sac scellé,
// remise au livreur) fixe le minimum, la minute en trop sur place est
// négligeable. Ne s'applique pas aux commandes « dès que possible »
// (resolveProductionReadyAt).
const platingBuffer = 4 * time.Minute

// maxProductionReadyAdvance borne l'avance possible. Bien au-delà de tout
// regroupement réel : sert surtout à refuser une heure tombée sur le mauvais
// jour (passage de minuit mal géré par un client : 23h45 envoyé pour le
// lendemain au lieu de la veille, soit ~24 h d'écart).
const maxProductionReadyAdvance = 12 * time.Hour

// ErrProductionReadyOutOfRange : heure cuisine manuelle plus tardive que
// l'heure calculée, ou trop en avance.
var ErrProductionReadyOutOfRange = fmt.Errorf("%w: heure cuisine hors bornes", models.ErrInvalidInput)

// ErrProductionReadyMarketplace : commande d'une plateforme (v1).
var ErrProductionReadyMarketplace = fmt.Errorf("%w: heure cuisine non modifiable sur une commande plateforme", models.ErrInvalidInput)

// validManualProductionReadyAt dit si manual peut remplacer l'heure calculée
// auto : strictement plus tôt, au plus maxProductionReadyAdvance avant. Une
// heure égale à l'heure calculée n'est pas un choix manuel (retour à l'auto).
func validManualProductionReadyAt(auto, manual time.Time) bool {
	return manual.Before(auto) && !manual.Before(auto.Add(-maxProductionReadyAdvance))
}

// applyManualProductionReadyAt renvoie la deadline cuisine à enregistrer et
// si elle est manuelle : manual s'il est valide face à l'heure calculée auto
// (time.Time ou nil, comme resolveProductionReadyAt), sinon auto. Une heure
// manuelle devenue incohérente (heure de livraison avancée, trajet rallongé)
// retombe donc d'elle-même sur le calcul automatique.
func applyManualProductionReadyAt(auto interface{}, manual *time.Time) (interface{}, bool) {
	a, ok := auto.(time.Time)
	if !ok || manual == nil {
		return auto, false
	}
	m := manual.UTC()
	if !validManualProductionReadyAt(a, m) {
		return auto, false
	}
	return m, true
}

func isMarketplaceBrand(brand string) bool {
	return brand == models.BrandUberEats || brand == models.BrandDeliveroo
}

// parseProductionReadyOverride lit OrderRequest.ProductionReadyOverride :
// keep = champ absent (garder l'heure manuelle déjà en base), sinon manual
// est l'heure demandée (nil = retour au calcul automatique, chaîne vide ou
// illisible).
func parseProductionReadyOverride(ctx context.Context, value *string) (keep bool, manual *time.Time) {
	if value == nil {
		return true, nil
	}
	raw := strings.TrimSpace(*value)
	if raw == "" {
		return false, nil
	}
	t, ok := normalizeEstimatedReady(raw).(time.Time)
	if !ok {
		logger.FromContext(ctx).Warn("production_ready_override illisible, heure cuisine remise en automatique",
			zap.String("value", raw))
		return false, nil
	}
	return false, &t
}

// resolveRequestProductionReadyAt : deadline cuisine d'une création / mise à
// jour de commande, en tenant compte de l'heure manuelle demandée (ou déjà
// enregistrée si le payload ne la mentionne pas, existing).
func resolveRequestProductionReadyAt(ctx context.Context, req *models.RequestObject, auto interface{}, existing *time.Time) (interface{}, bool) {
	if isMarketplaceBrand(req.Order.Brand) {
		return auto, false
	}
	keep, manual := parseProductionReadyOverride(ctx, req.Order.ProductionReadyOverride)
	if keep {
		manual = existing
	}
	return applyManualProductionReadyAt(auto, manual)
}

// existingManualProductionReadyAt : heure cuisine manuelle enregistrée sur la
// commande, nil s'il n'y en a pas.
func (r *OrdersLifeCycleRepository) existingManualProductionReadyAt(ctx context.Context, orderID *string) (*time.Time, error) {
	if orderID == nil || *orderID == "" {
		return nil, nil
	}
	db := dbx.GetDB(ctx, r.database)
	var at sql.NullTime
	var manual bool
	err := db.QueryRowContext(ctx,
		`SELECT production_ready_at, production_ready_manual FROM orders WHERE order_id = ?`,
		*orderID,
	).Scan(&at, &manual)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !manual || !at.Valid {
		return nil, nil
	}
	t := at.Time.UTC()
	return &t, nil
}

// productionReadyInputs : ce qu'il faut pour recalculer la deadline cuisine
// d'une commande existante.
type productionReadyInputs struct {
	State          string
	Brand          string
	Scheduled      bool
	EstimatedReady interface{} // time.Time ou nil, comme normalizeEstimatedReady
	TravelSeconds  *int
}

func (r *OrdersLifeCycleRepository) loadProductionReadyInputs(ctx context.Context, merchantID, orderID string) (*productionReadyInputs, error) {
	db := dbx.GetDB(ctx, r.database)
	var state, brand sql.NullString
	var scheduled sql.NullBool
	var estimatedReady sql.NullTime
	var travel sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT state, brand, scheduled, estimated_ready, delivery_travel_seconds
		FROM orders
		WHERE order_id = ? AND merchant_id = ?`,
		orderID, merchantID,
	).Scan(&state, &brand, &scheduled, &estimatedReady, &travel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	in := &productionReadyInputs{
		State:     state.String,
		Brand:     brand.String,
		Scheduled: scheduled.Bool,
	}
	if estimatedReady.Valid {
		in.EstimatedReady = estimatedReady.Time.UTC()
	}
	if travel.Valid {
		seconds := int(travel.Int64)
		in.TravelSeconds = &seconds
	}
	return in, nil
}

func (r *OrdersLifeCycleRepository) updateProductionReadyAt(ctx context.Context, merchantID, orderID string, at interface{}, manual bool) error {
	db := dbx.GetDB(ctx, r.database)
	// last_update n'est pas touché : il date les changements de contenu (les
	// cartes de production en affichent l'ancienneté), pas un réglage d'heure.
	_, err := db.ExecContext(ctx, `
		UPDATE orders
		SET production_ready_at = ?, production_ready_manual = ?
		WHERE order_id = ? AND merchant_id = ?`,
		at, manual, orderID, merchantID,
	)
	return err
}

// SetProductionReadyAtRequest : corps de PATCH
// /orders/{order_id}/production-ready-at. ProductionReadyAt en ISO 8601 ;
// null ou absent = retour au calcul automatique.
type SetProductionReadyAtRequest struct {
	ProductionReadyAt *string `json:"production_ready_at"`
}

// SetProductionReadyAt change la deadline cuisine d'une commande ouverte sans
// toucher à son heure de livraison (estimated_ready, delivery_arrival_at).
func (s *OrdersLifeCycleService) SetProductionReadyAt(ctx context.Context, orderID string, req *SetProductionReadyAtRequest) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	if orderID == "" {
		return models.ErrMissingResourceID
	}

	in, err := s.ordersLifeCycleRepo.loadProductionReadyInputs(ctx, user.MerchantID, orderID)
	if err != nil {
		return err
	}
	if in.State != "OPEN" {
		return models.ErrOrderClosed
	}
	if isMarketplaceBrand(in.Brand) {
		return ErrProductionReadyMarketplace
	}

	auto := resolveProductionReadyAt(in.EstimatedReady, in.Scheduled, in.TravelSeconds)
	at, manual := auto, false
	if req != nil && req.ProductionReadyAt != nil && strings.TrimSpace(*req.ProductionReadyAt) != "" {
		wanted, ok := normalizeEstimatedReady(strings.TrimSpace(*req.ProductionReadyAt)).(time.Time)
		if !ok {
			return fmt.Errorf("%w: production_ready_at illisible", models.ErrInvalidInput)
		}
		autoTime, hasAuto := auto.(time.Time)
		if !hasAuto {
			// Pas d'heure calculée (commande sans estimated_ready) : rien à
			// avancer, et aucune borne pour juger la valeur demandée.
			return ErrProductionReadyOutOfRange
		}
		if wanted.Equal(autoTime) {
			// Même heure que le calcul : c'est un retour à l'automatique.
		} else if !validManualProductionReadyAt(autoTime, wanted) {
			return ErrProductionReadyOutOfRange
		} else {
			at, manual = wanted, true
		}
	}

	if err := s.ordersLifeCycleRepo.updateProductionReadyAt(ctx, user.MerchantID, orderID, at, manual); err != nil {
		return err
	}
	// Même suite qu'un changement de suites : cache Redis purgé, caisses
	// prévenues (OrderUpdate) — elles rechargent la commande ; aucun ticket ne
	// part, l'impression ne compare que les produits.
	s.afterCoursesChanged(ctx, user.MerchantID, orderID)
	return nil
}
