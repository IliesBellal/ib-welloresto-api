package menu

import (
	"context"
	"sync"
	"time"

	"welloresto-api/internal/modules/notification"
)

// realtimeBroadcaster diffuse un message WebSocket à tous les devices d'un
// merchant. Satisfait par *notification.NotificationService ; déclaré en
// interface pour garder le notifier testable sans hub réel.
type realtimeBroadcaster interface {
	BroadcastToMerchant(merchantID string, payload map[string]interface{}) bool
}

// menuCacheInvalidator purge les caches Redis dérivés du catalogue. Satisfait
// par *redisclient.Client (dont la méthode est déjà nil-receiver-safe).
type menuCacheInvalidator interface {
	InvalidateMerchantMenuCaches(ctx context.Context, merchantID string)
}

// defaultMenuBroadcastWindow borne la diffusion à un événement par merchant et
// par seconde. Voir MenuChangeNotifier.Changed pour le raisonnement.
const defaultMenuBroadcastWindow = time.Second

// MenuChangeNotifier centralise tout ce qui doit se produire après une mutation
// du catalogue produits : purge des caches Redis dérivés (menus scannorder et
// kiosk, upsell) puis diffusion de l'événement WebSocket `menu_updated`.
//
// Point d'entrée unique voulu : avant, la purge de cache était appelée à 42
// endroits de MenuService et le marquage `last_menu_update` à 44 endroits du
// dépôt, sans que les deux coïncident exactement — quatre mutations de
// composants purgeaient `last_menu_update` sans purger le cache. Faire passer
// tout le monde par Changed supprime la classe de bug.
//
// # Pourquoi la diffusion est amortie mais pas la purge
//
// La purge de cache doit avoir lieu à *chaque* mutation, immédiatement : la
// retarder laisserait une vitrine servir un menu périmé. La diffusion, elle,
// est amortie : chaque événement fait recharger le menu complet par toutes les
// bornes et tous les POS du merchant, et `Hub.BroadcastToMerchant` désinscrit
// silencieusement un client dont le buffer de 256 messages déborde. Une rafale
// non amortie pourrait donc déconnecter les POS qu'elle prétend synchroniser.
//
// # Front montant + front descendant
//
// Une modification isolée (le cas courant : un produit passé en rupture) part
// immédiatement. Si d'autres arrivent dans la foulée, une diffusion de
// clôture est programmée en fin de fenêtre. Un simple « cooldown » sans front
// descendant aurait laissé les clients sur l'état du *début* de la rafale : ils
// auraient rechargé pendant que les écritures continuaient, sans jamais être
// prévenus de la fin.
type MenuChangeNotifier struct {
	cache       menuCacheInvalidator
	broadcaster realtimeBroadcaster
	window      time.Duration

	mu      sync.Mutex
	lastAt  map[string]time.Time
	pending map[string]struct{}

	// Points d'injection pour les tests : permettent de piloter le temps sans
	// faire dormir la suite de tests.
	now   func() time.Time
	after func(time.Duration, func())
}

// NewMenuChangeNotifier câble le notifier. Les deux dépendances sont
// optionnelles : sans broadcaster la diffusion est inactive, sans cache la
// purge l'est — dans les deux cas la mutation métier aboutit normalement.
func NewMenuChangeNotifier(cache menuCacheInvalidator, broadcaster realtimeBroadcaster) *MenuChangeNotifier {
	return &MenuChangeNotifier{
		cache:       cache,
		broadcaster: broadcaster,
		window:      defaultMenuBroadcastWindow,
		lastAt:      make(map[string]time.Time),
		pending:     make(map[string]struct{}),
		now:         time.Now,
		after: func(d time.Duration, f func()) {
			time.AfterFunc(d, f)
		},
	}
}

// Changed signale une mutation aboutie du catalogue d'un merchant.
//
// À appeler après le succès de l'écriture et **hors transaction** : le client
// rappelle l'API dès réception, il ne doit pas pouvoir lire un état pas encore
// commité. C'est la raison pour laquelle la diffusion vit ici, au niveau
// service, et non dans MenuRepository.setMenuUpdated qui tourne à l'intérieur
// des transactions.
//
// Best-effort de bout en bout : ne renvoie pas d'erreur et n'en propage aucune.
func (n *MenuChangeNotifier) Changed(ctx context.Context, merchantID string) {
	if n == nil || merchantID == "" {
		return
	}

	if n.cache != nil {
		n.cache.InvalidateMerchantMenuCaches(ctx, merchantID)
	}

	n.scheduleBroadcast(merchantID)
}

func (n *MenuChangeNotifier) scheduleBroadcast(merchantID string) {
	if n.broadcaster == nil {
		return
	}

	n.mu.Lock()
	now := n.now()
	last, seen := n.lastAt[merchantID]

	// Front montant : rien n'est parti récemment, on diffuse tout de suite.
	if !seen || now.Sub(last) >= n.window {
		n.lastAt[merchantID] = now
		n.mu.Unlock()
		n.broadcast(merchantID)
		return
	}

	// Une clôture est déjà programmée : cette mutation y sera incluse.
	if _, alreadyPending := n.pending[merchantID]; alreadyPending {
		n.mu.Unlock()
		return
	}

	n.pending[merchantID] = struct{}{}
	delay := n.window - now.Sub(last)
	n.mu.Unlock()

	n.after(delay, func() { n.flush(merchantID) })
}

// flush émet la diffusion de clôture d'une rafale.
func (n *MenuChangeNotifier) flush(merchantID string) {
	n.mu.Lock()
	if _, isPending := n.pending[merchantID]; !isPending {
		n.mu.Unlock()
		return
	}
	delete(n.pending, merchantID)
	n.lastAt[merchantID] = n.now()
	n.mu.Unlock()

	n.broadcast(merchantID)
}

// broadcast émet l'événement. Volontairement sans `last_menu_update` : c'est
// une notification sans état (décision D2), le client va rechercher le menu par
// son endpoint habituel. L'y joindre aurait coûté une lecture en base sur un
// chemin amorti, pour une valeur de toute façon approximative au moment où le
// client la lirait.
func (n *MenuChangeNotifier) broadcast(merchantID string) {
	n.broadcaster.BroadcastToMerchant(merchantID, map[string]interface{}{
		"type":        notification.WSEventMenuUpdated,
		"merchant_id": merchantID,
	})
}
