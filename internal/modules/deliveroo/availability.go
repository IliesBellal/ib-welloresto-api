package deliveroo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/logger"
	"welloresto-api/internal/modules/marketplacecatalog"
)

// Alignement des disponibilités : WelloResto fait foi.
//
// L'envoi est toujours calculé depuis l'ÉTAT COURANT de WelloResto comparé à
// l'état lu chez Deliveroo, jamais depuis un événement : deux changements
// rapprochés (« disponible » puis « épuisé » depuis la caisse) donnent le même
// résultat quel que soit l'ordre d'arrivée, et un envoi raté est rattrapé au
// réveil suivant. Seule la différence est envoyée, en une requête.
//
//	WelloResto             Deliveroo
//	available              available (ni épuisé ni masqué)
//	out_of_stock           unavailable (remis disponible chaque matin par Deliveroo)
//	not_available          hidden
//	retiré / décoché       hidden en attendant la republication du menu

const (
	// availabilityDebounce regroupe les rafales de changements (action de
	// groupe, enregistrements successifs) en un seul alignement.
	availabilityDebounce = 3 * time.Second
	// availabilityFallback : réveil de secours si l'alignement immédiat échoue
	// ou si le serveur redémarre avant de l'avoir fait.
	availabilityFallback = 2 * time.Minute
	reconcileTimeout     = 90 * time.Second
)

// itemState : état d'un article chez Deliveroo.
type itemState string

const (
	stateAvailable   itemState = "available"
	stateUnavailable itemState = "unavailable"
	stateHidden      itemState = "hidden"
)

// diffAvailability compare l'état voulu (WelloResto) à l'état courant
// (Deliveroo) pour les articles publiés et renvoie les seuls changements.
// Un article publié absent du catalogue (retiré, décoché) est voulu masqué.
func diffAvailability(published []string, cat *marketplacecatalog.Catalog, cur *UnavailabilitiesResponse) []ItemUnavailability {
	unavailable := make(map[string]bool, len(cur.UnavailableIDs))
	for _, id := range cur.UnavailableIDs {
		unavailable[id] = true
	}
	hidden := make(map[string]bool, len(cur.HiddenIDs))
	for _, id := range cur.HiddenIDs {
		hidden[id] = true
	}

	var out []ItemUnavailability
	for _, id := range published {
		want := itemState(UnavailabilityStatus(marketplacecatalog.Hidden))
		if p := cat.Products[id]; p != nil {
			want = itemState(UnavailabilityStatus(p.Availability))
		}
		have := stateAvailable
		switch {
		case hidden[id]:
			have = stateHidden
		case unavailable[id]:
			have = stateUnavailable
		}
		if want != have {
			out = append(out, ItemUnavailability{ItemID: id, Status: string(want)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out
}

// ReconcileAvailability lit l'état chez Deliveroo, le compare à WelloResto et
// envoie la différence. Renvoie le nombre d'articles modifiés. Sans menu publié,
// ne fait rien.
func (s *DeliverooService) ReconcileAvailability(ctx context.Context, acc PublishedAccount) (int, error) {
	published, err := s.repo.LastPublishedItemIDs(ctx, acc.MerchantID, acc.LocationID)
	if err != nil {
		return 0, err
	}
	if len(published) == 0 {
		return 0, nil
	}
	cat, err := s.loadCatalog(ctx, acc.MerchantID)
	if errors.Is(err, marketplacecatalog.ErrNothingToPublish) {
		cat, err = &marketplacecatalog.Catalog{Products: map[string]*marketplacecatalog.Product{}}, nil
	}
	if err != nil {
		return 0, err
	}
	cur, err := s.client.GetUnavailabilities(ctx, acc.BrandID, PublishedMenuID, acc.LocationID)
	if err != nil {
		return 0, fmt.Errorf("lecture des indisponibilités: %w", err)
	}
	changes := diffAvailability(published, cat, cur)
	if len(changes) == 0 {
		return 0, nil
	}
	if err := s.client.UpdateIndividualUnavailabilities(ctx, acc.BrandID, PublishedMenuID, acc.LocationID,
		ItemUnavailabilitiesRequest{ItemUnavailabilities: changes}); err != nil {
		return 0, fmt.Errorf("envoi des indisponibilités: %w", err)
	}
	return len(changes), nil
}

// availabilityRunner sérialise les alignements d'un même compte et regroupe
// les demandes rapprochées.
type availabilityRunner struct {
	mu        sync.Mutex
	scheduled map[string]bool // un alignement est déjà programmé
	running   map[string]bool // un alignement est en cours
	rerun     map[string]bool // une demande est arrivée pendant l'exécution
}

func accountKey(a PublishedAccount) string { return a.MerchantID + "/" + a.LocationID }

// OnMenuChanged est appelé à chaque modification du catalogue d'un
// établissement (MenuChangeNotifier) : marque le menu à revérifier et programme
// l'alignement des disponibilités de ses comptes Deliveroo publiés. Sans effet,
// et presque sans coût, pour un établissement sans Deliveroo.
func (s *DeliverooService) OnMenuChanged(merchantID string) {
	if merchantID == "" || s.menuLoader == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log := logger.FromContext(ctx)
		defer func() {
			if r := recover(); r != nil {
				log.Error(fmt.Sprintf("[DELIVEROO] panic OnMenuChanged: %v", r))
			}
		}()

		accounts, err := s.repo.GetPublishedAccounts(ctx, merchantID)
		if err != nil {
			log.Error("[DELIVEROO] comptes publiés illisibles (migration 202 jouée ?)", zap.String("merchant_id", merchantID), zap.Error(err))
			return
		}
		if len(accounts) == 0 {
			return
		}
		if err := s.repo.MarkMenuDirty(ctx, merchantID); err != nil {
			log.Error("[DELIVEROO] menu non marqué à revérifier", zap.String("merchant_id", merchantID), zap.Error(err))
		}
		for _, acc := range accounts {
			if err := s.repo.RequestCheckSoon(ctx, acc.MerchantID, acc.LocationID, availabilityFallback, checkChanged); err != nil {
				log.Warn("[DELIVEROO] réveil de secours non programmé", zap.String("location_id", acc.LocationID), zap.Error(err))
			}
			s.scheduleAvailability(acc)
		}
	}()
}

// scheduleAvailability programme un alignement dans availabilityDebounce, sauf
// si un est déjà programmé.
func (s *DeliverooService) scheduleAvailability(acc PublishedAccount) {
	r := &s.avail
	key := accountKey(acc)
	r.mu.Lock()
	if r.scheduled == nil {
		r.scheduled, r.running, r.rerun = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	if r.scheduled[key] {
		r.mu.Unlock()
		return
	}
	r.scheduled[key] = true
	r.mu.Unlock()

	time.AfterFunc(availabilityDebounce, func() {
		r.mu.Lock()
		delete(r.scheduled, key)
		if r.running[key] {
			r.rerun[key] = true // l'exécution en cours refera un tour
			r.mu.Unlock()
			return
		}
		r.running[key] = true
		r.mu.Unlock()
		s.runAvailabilityLoop(acc)
	})
}

func (s *DeliverooService) runAvailabilityLoop(acc PublishedAccount) {
	r := &s.avail
	key := accountKey(acc)
	for {
		s.alignAndPlan(acc, checkChanged)

		r.mu.Lock()
		if r.rerun[key] {
			delete(r.rerun, key)
			r.mu.Unlock()
			continue
		}
		delete(r.running, key)
		r.mu.Unlock()
		return
	}
}

// alignAndPlan aligne les disponibilités d'un compte puis programme son
// prochain réveil. En cas d'échec, un nouveau réveil est programmé bientôt.
func (s *DeliverooService) alignAndPlan(acc PublishedAccount, kind string) {
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	log := logger.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("[DELIVEROO] panic alignement: %v", r))
		}
	}()

	n, err := s.ReconcileAvailability(ctx, acc)
	if err != nil {
		log.Error("[DELIVEROO] alignement des disponibilités échoué",
			zap.String("merchant_id", acc.MerchantID), zap.String("location_id", acc.LocationID), zap.Error(err))
		_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.LocationID, time.Now().Add(availabilityFallback), checkRetry, 0, err.Error())
		return
	}
	if n > 0 {
		log.Info("[DELIVEROO] disponibilités alignées",
			zap.String("merchant_id", acc.MerchantID), zap.String("location_id", acc.LocationID), zap.Int("articles", n))
	}
	s.planNext(ctx, acc, kind)
}
