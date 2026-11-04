package ubereats

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
// L'envoi est toujours calculé depuis l'ÉTAT COURANT de WelloResto comparé à ce
// qu'on a déjà envoyé à Uber Eats (marketplace_item_states : Uber Eats ne permet
// pas de relire la suspension d'un article), jamais depuis un événement. Deux
// changements rapprochés (« disponible » puis « épuisé » depuis la caisse)
// donnent le même résultat quel que soit l'ordre d'arrivée, et un envoi raté est
// rattrapé au réveil suivant. Seule la différence est envoyée, article par article.
//
//	WelloResto             Uber Eats
//	available              en vente (suspension retirée)
//	out_of_stock           suspendu jusqu'à nouvel ordre (suspendForever)
//	not_available          suspendu en attendant la republication du menu, qui le retire
//	retiré / décoché       suspendu en attendant la republication du menu, qui le retire
//
// Uber Eats ne remet jamais un article en vente seul : c'est WelloResto qui le fait.

const (
	// availabilityDebounce regroupe les rafales de changements (action de
	// groupe, enregistrements successifs) en un seul alignement.
	availabilityDebounce = 3 * time.Second
	// availabilityFallback : réveil de secours si l'alignement immédiat échoue ou
	// si le serveur redémarre avant de l'avoir fait.
	availabilityFallback = 2 * time.Minute
	// reconcileTimeout borne un alignement (une requête par article modifié).
	reconcileTimeout = 3 * time.Minute
)

// TODO(options) : la disponibilité des OPTIONS n'est pas alignée — seuls les
// articles produits le sont. À intégrer aux marketplaces (voir le TODO(options)
// de internal/modules/deliveroo/availability.go) : chez Uber Eats une option est
// un article à part entière (« opt_<id> »), donc une rupture d'option passerait
// par le même SetItemSuspension ; il faudra mémoriser leur état dans
// marketplace_item_states et les ajouter aux identifiants publiés.

// itemChange : un article à mettre à jour chez Uber Eats.
type itemChange struct {
	ItemID string
	Want   marketplacecatalog.Availability // Available ou SoldOut
}

// diffAvailability compare l'état voulu (WelloResto) à l'état envoyé pour les
// articles publiés et renvoie les seuls changements. Un article sans état
// mémorisé est (ré)envoyé. Un article publié absent du catalogue, ou masqué,
// est voulu épuisé en attendant la republication du menu.
func diffAvailability(published []string, cat *marketplacecatalog.Catalog, sent map[string]marketplacecatalog.Availability) []itemChange {
	var out []itemChange
	for _, id := range published {
		want := marketplacecatalog.SoldOut
		if p := cat.Products[id]; p != nil && p.Availability == marketplacecatalog.Available {
			want = marketplacecatalog.Available
		}
		if have, ok := sent[id]; ok && have == want {
			continue
		}
		out = append(out, itemChange{ItemID: id, Want: want})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out
}

// ReconcileAvailability compare WelloResto à l'état envoyé et envoie la
// différence. Renvoie le nombre d'articles modifiés. Sans menu publié, ne fait
// rien. Un article en erreur n'empêche pas les suivants ; la première erreur est
// renvoyée et l'article sera retenté au prochain réveil.
func (s *UberEatsService) ReconcileAvailability(ctx context.Context, acc PublishedAccount) (int, error) {
	if s.menuLoader == nil {
		return 0, nil
	}
	lock := s.locks.get(acc.MerchantID + "/" + acc.StoreID)
	lock.Lock()
	defer lock.Unlock()

	published, err := s.repo.LastPublishedItemIDs(ctx, acc.MerchantID, acc.StoreID)
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
	sent, err := s.repo.GetItemStates(ctx, acc.MerchantID, acc.StoreID)
	if err != nil {
		return 0, err
	}
	changes := diffAvailability(published, cat, sent)
	if len(changes) == 0 {
		return 0, nil
	}

	token, err := s.GetValidToken(ctx)
	if err != nil {
		return 0, err
	}
	log := logger.FromContext(ctx)
	done := 0
	var firstErr error
	for _, ch := range changes {
		if _, err := s.client.SetItemSuspension(ctx, acc.StoreID, ch.ItemID, token, ch.Want == marketplacecatalog.SoldOut); err != nil {
			log.Warn("[UBER EATS] mise à jour d'article refusée",
				zap.String("store_id", acc.StoreID), zap.String("item_id", ch.ItemID), zap.Error(err))
			if firstErr == nil {
				firstErr = fmt.Errorf("article %s: %w", ch.ItemID, err)
			}
			continue
		}
		if err := s.repo.SetItemState(ctx, acc.MerchantID, acc.StoreID, ch.ItemID, ch.Want); err != nil {
			log.Warn("[UBER EATS] état envoyé non mémorisé", zap.String("item_id", ch.ItemID), zap.Error(err))
		}
		done++
	}
	return done, firstErr
}

// uberRunner sérialise les alignements d'un même compte et regroupe les demandes
// rapprochées.
type uberRunner struct {
	mu        sync.Mutex
	scheduled map[string]bool // un alignement est déjà programmé
	running   map[string]bool // un alignement est en cours
	rerun     map[string]bool // une demande est arrivée pendant l'exécution
}

func accountKey(a PublishedAccount) string { return a.MerchantID + "/" + a.StoreID }

// OnMenuChanged est appelé à chaque modification du catalogue d'un établissement
// (MenuChangeNotifier) : marque le menu à revérifier et programme l'alignement
// des disponibilités de ses comptes Uber Eats publiés. Sans effet, et presque
// sans coût, pour un établissement sans Uber Eats.
func (s *UberEatsService) OnMenuChanged(merchantID string) {
	if merchantID == "" || s.menuLoader == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log := logger.FromContext(ctx)
		defer func() {
			if r := recover(); r != nil {
				log.Error(fmt.Sprintf("[UBER EATS] panic OnMenuChanged: %v", r))
			}
		}()

		accounts, err := s.repo.GetPublishedAccounts(ctx, merchantID)
		if err != nil {
			log.Error("[UBER EATS] comptes publiés illisibles (migration 202 jouée ?)", zap.String("merchant_id", merchantID), zap.Error(err))
			return
		}
		if len(accounts) == 0 {
			return
		}
		if err := s.repo.MarkMenuDirty(ctx, merchantID); err != nil {
			log.Error("[UBER EATS] menu non marqué à revérifier", zap.String("merchant_id", merchantID), zap.Error(err))
		}
		for _, acc := range accounts {
			if err := s.repo.RequestCheckSoon(ctx, acc.MerchantID, acc.StoreID, availabilityFallback, checkChanged); err != nil {
				log.Warn("[UBER EATS] réveil de secours non programmé", zap.String("store_id", acc.StoreID), zap.Error(err))
			}
			s.scheduleAvailability(acc)
		}
	}()
}

// scheduleAvailability programme un alignement dans availabilityDebounce, sauf si
// un est déjà programmé.
func (s *UberEatsService) scheduleAvailability(acc PublishedAccount) {
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

func (s *UberEatsService) runAvailabilityLoop(acc PublishedAccount) {
	r := &s.avail
	key := accountKey(acc)
	for {
		s.alignAndPlan(acc)

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

// alignAndPlan aligne les disponibilités d'un compte puis programme son prochain
// réveil. En cas d'échec, un nouveau réveil est programmé bientôt.
func (s *UberEatsService) alignAndPlan(acc PublishedAccount) {
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	log := logger.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Sprintf("[UBER EATS] panic alignement: %v", r))
		}
	}()

	n, err := s.ReconcileAvailability(ctx, acc)
	if err != nil {
		log.Error("[UBER EATS] alignement des disponibilités échoué",
			zap.String("merchant_id", acc.MerchantID), zap.String("store_id", acc.StoreID), zap.Error(err))
		_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, time.Now().Add(availabilityFallback), checkRetry, err.Error())
		return
	}
	if n > 0 {
		log.Info("[UBER EATS] disponibilités alignées",
			zap.String("merchant_id", acc.MerchantID), zap.String("store_id", acc.StoreID), zap.Int("articles", n))
	}
	// Le changement qui a déclenché l'alignement est traité : le prochain réveil
	// est le filet de sécurité (la republication du menu, elle, suit le marqueur
	// « menu modifié » après quelques minutes de calme).
	_ = s.repo.ScheduleCheck(ctx, acc.MerchantID, acc.StoreID, nextSafetyCheck(time.Now()), checkSafety, "")
}
