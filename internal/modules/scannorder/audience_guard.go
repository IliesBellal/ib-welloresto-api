package scannorder

// Garde-fous de POST /scannorder/events (endpoint public, sans authentification).
// Aucun n'ajoute d'entrée-sortie au chemin normal : tout est en mémoire, sauf
// la liste des produits d'un établissement, lue au plus une fois toutes les
// audienceCatalogTTL. En cas de doute (erreur de lecture, délai dépassé),
// l'événement est accepté : mieux vaut une ligne en trop qu'une statistique
// perdue (décision D30 : la mesure ne gêne jamais).

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"welloresto-api/internal/helpers"
)

const (
	// AudienceGlobalPerMinute : filet sur l'adresse de connexion (RemoteAddr).
	// Derrière le reverse proxy de l'hébergeur c'est l'adresse du proxy, donc un
	// seuil commun à tous les visiteurs : il doit rester très au-dessus du
	// trafic réel (l'ancien seuil de 120/min coupait les statistiques dès une
	// poignée de visiteurs simultanés). Le coût en base est borné par
	// audienceEventsPerMinute, pas par ce seuil.
	AudienceGlobalPerMinute = 3000

	// AudienceClientPerMinute : seuil par visiteur (voir AudienceClientKey).
	AudienceClientPerMinute = 120

	// Seuils par visiteur et par établissement (MerchantClientKey) des autres
	// routes publiques, volontairement larges : les clients d'un restaurant
	// partagent souvent son wifi, donc une même adresse. Lectures : la page de
	// suivi relit la commande toutes les 10 s (6/min par client), le menu toutes
	// les 90 s. Calculs (pricing, upsell, zone de livraison) : à chaque
	// modification du panier. Écritures : création et annulation de commande.
	ScanNOrderReadsPerMinute   = 1200
	ScanNOrderComputePerMinute = 600
	ScanNOrderWritesPerMinute  = 60

	// ScanNOrderMaxBodyBytes : corps maximal d'une requête /scannorder (une
	// commande de plusieurs dizaines d'articles avec options pèse quelques
	// dizaines de Ko). Appliqué à la racine du routeur, avant les middlewares
	// de journalisation qui lisent tout le corps.
	ScanNOrderMaxBodyBytes = 512 << 10

	// audienceEventsPerMinute : événements acceptés par minute et par
	// établissement (ou par franchise pour les événements du hub). Un
	// restaurant très fréquenté reste loin sous ce plafond ; au-delà, les
	// événements en trop sont ignorés sans erreur.
	audienceEventsPerMinute = 600

	// audienceMaxBudgetKeys : garde-fou mémoire du compteur (les clés sont des
	// établissements ou franchises existants, donc bornées en pratique).
	audienceMaxBudgetKeys = 10000

	// audienceCatalogTTL : durée de vie de la liste des produits d'un
	// établissement. audienceCatalogRefreshFloor : un produit inconnu ne
	// relance la lecture qu'après ce délai, pour qu'un produit créé à l'instant
	// soit reconnu vite sans qu'un identifiant bidon puisse déclencher une
	// lecture par requête.
	audienceCatalogTTL          = 5 * time.Minute
	audienceCatalogRefreshFloor = 30 * time.Second
	audienceCatalogLoadTimeout  = time.Second
)

// automatedUserAgent : robots d'indexation et clients HTTP sans navigateur.
// Volontairement étroit — un faux positif perd une statistique, un faux négatif
// n'en coûte qu'une ligne. « \bbot » ne retient pas « Cubot » ; un User-Agent
// vide est accepté (certains outils de confidentialité le suppriment).
var automatedUserAgent = regexp.MustCompile(`(?i)(\bbot\b|[a-z]bot[/-]|crawler|spider|slurp|headless|lighthouse|pagespeed|\bcurl/|\bwget/|python-requests|python-urllib|go-http-client|java/\d|libwww-perl|httpclient)`)

func isAutomatedUserAgent(ua string) bool {
	return ua != "" && automatedUserAgent.MatchString(ua)
}

// AudienceClientKey : clé du seuil par visiteur (voir helpers.ProxyClientIP).
func AudienceClientKey(r *http.Request) (string, error) {
	return helpers.ProxyClientIP(r), nil
}

// MerchantClientKey : clé des seuils des routes /scannorder/{merchant_slug}/… —
// visiteur (voir AudienceClientKey) et établissement. Si l'adresse vue est en
// fait celle d'un proxy partagé, le seuil devient un seuil par établissement,
// jamais un seuil commun à toute la plateforme. Doit être posée en middleware
// inline (r.With) : le paramètre de route n'est résolu qu'après le routage.
func MerchantClientKey(r *http.Request) (string, error) {
	return helpers.ProxyClientIP(r) + "|" + chi.URLParam(r, "merchant_slug"), nil
}

type audienceWindow struct {
	minute int64
	count  int
}

type audienceCatalog struct {
	loadedAt time.Time
	products map[string]struct{}
}

// audienceGuard : plafond d'événements par établissement et reconnaissance des
// produits. Le zéro-valeur n'est pas utilisable ; un *audienceGuard nil accepte
// tout (tests qui construisent un Service vide).
type audienceGuard struct {
	now func() time.Time

	mu       sync.Mutex
	windows  map[string]*audienceWindow
	catalogs map[string]*audienceCatalog

	loads singleflight.Group
}

func newAudienceGuard() *audienceGuard {
	return &audienceGuard{
		now:      time.Now,
		windows:  map[string]*audienceWindow{},
		catalogs: map[string]*audienceCatalog{},
	}
}

// allow compte un événement pour key (« m:<établissement> » ou
// « f:<franchise> ») dans la minute courante.
func (g *audienceGuard) allow(key string) bool {
	if g == nil {
		return true
	}
	minute := g.now().Unix() / 60
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.windows[key]
	if w == nil {
		if len(g.windows) >= audienceMaxBudgetKeys {
			for k, old := range g.windows {
				if old.minute != minute {
					delete(g.windows, k)
				}
			}
		}
		w = &audienceWindow{minute: minute}
		g.windows[key] = w
	}
	if w.minute != minute {
		w.minute, w.count = minute, 0
	}
	if w.count >= audienceEventsPerMinute {
		return false
	}
	w.count++
	return true
}

// productKnown indique si productID appartient à l'établissement. load lit la
// liste complète des produits ; toute erreur ou dépassement de délai accepte
// l'événement.
func (g *audienceGuard) productKnown(ctx context.Context, merchantID, productID string, load func(context.Context, string) (map[string]struct{}, error)) bool {
	if g == nil {
		return true
	}
	now := g.now()

	g.mu.Lock()
	entry := g.catalogs[merchantID]
	g.mu.Unlock()

	if entry != nil {
		age := now.Sub(entry.loadedAt)
		if age < audienceCatalogTTL {
			if _, ok := entry.products[productID]; ok {
				return true
			}
			if age < audienceCatalogRefreshFloor {
				return false
			}
		}
	}

	// Une seule lecture à la fois par établissement ; elle ne dépend pas de
	// la requête qui la lance (annulée, elle priverait les autres).
	v, err, _ := g.loads.Do(merchantID, func() (interface{}, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), audienceCatalogLoadTimeout)
		defer cancel()
		products, err := load(loadCtx, merchantID)
		if err != nil {
			return nil, err
		}
		fresh := &audienceCatalog{loadedAt: g.now(), products: products}
		g.mu.Lock()
		g.catalogs[merchantID] = fresh
		g.mu.Unlock()
		return fresh, nil
	})
	if err != nil {
		return true
	}
	_, ok := v.(*audienceCatalog).products[productID]
	return ok
}
