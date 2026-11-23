package scannorder

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Signalement IndexNow des pages ScanNOrder modifiées (docs/scannorder-seo.md) :
// toutes les 15 minutes, les pages du sitemap (établissements et hubs de
// franchise) dont le lastmod a bougé depuis le passage précédent sont
// envoyées aux moteurs IndexNow (Bing, Yahoo…). Inactif sans INDEXNOW_KEY.

// indexNowWatermarkKey : date du dernier passage réussi (Redis, partagée
// entre instances).
const indexNowWatermarkKey = "scannorder:indexnow:watermark"

const (
	// indexNowFirstRunLookback : au premier passage (aucune date connue), on
	// ne renvoie que les pages modifiées la veille — la découverte du reste
	// passe par le sitemap.
	indexNowFirstRunLookback = 24 * time.Hour
	// indexNowOverlap : marge prise sur la date du passage précédent, pour
	// ne pas manquer une écriture enregistrée pendant ce passage. Un envoi en
	// double est sans effet.
	indexNowOverlap   = 2 * time.Minute
	indexNowStateTTL  = 30 * 24 * time.Hour
	indexNowRunBudget = 2 * time.Minute
)

type indexNowSubmitter interface {
	Submit(ctx context.Context, urls []string) error
}

type indexNowSitemap interface {
	GetSitemapEntries(ctx context.Context) ([]SitemapEntry, error)
	GetSitemapFranchises(ctx context.Context) ([]SitemapEntry, error)
}

// IndexNowState garde la date du dernier passage réussi (redis.Client).
type IndexNowState interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration) bool
}

type IndexNowNotifier struct {
	sitemap indexNowSitemap
	client  indexNowSubmitter
	state   IndexNowState // nil sans Redis : date gardée en mémoire seulement
	baseURL string
	log     *zap.Logger
	now     func() time.Time

	mu        sync.Mutex
	watermark time.Time
}

// NewIndexNowNotifier : client nil = signalement désactivé (Run ne fait rien).
func NewIndexNowNotifier(repo *Repository, client indexNowSubmitter, state IndexNowState, publicBaseURL string, log *zap.Logger) *IndexNowNotifier {
	return &IndexNowNotifier{
		sitemap: repo,
		client:  client,
		state:   state,
		baseURL: strings.TrimRight(publicBaseURL, "/"),
		log:     log,
		now:     time.Now,
	}
}

// Run signale les pages modifiées depuis le passage précédent et renvoie leur
// nombre. En cas d'échec, la date n'avance pas : le passage suivant renvoie.
func (n *IndexNowNotifier) Run(ctx context.Context) (int, error) {
	if n == nil || n.client == nil {
		return 0, nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, indexNowRunBudget)
	defer cancel()

	runStart := n.now().UTC()
	since := n.lastRun(ctx, runStart).Add(-indexNowOverlap)

	merchants, err := n.sitemap.GetSitemapEntries(ctx)
	if err != nil {
		return 0, err
	}
	franchises, err := n.sitemap.GetSitemapFranchises(ctx)
	if err != nil {
		return 0, err
	}

	var urls []string
	for _, e := range merchants {
		if e.UpdatedAt != nil && e.UpdatedAt.After(since) {
			urls = append(urls, n.baseURL+"/restaurant/"+url.PathEscape(e.Slug))
		}
	}
	for _, e := range franchises {
		if e.UpdatedAt != nil && e.UpdatedAt.After(since) {
			urls = append(urls, n.baseURL+"/hub/"+url.PathEscape(e.Slug))
		}
	}

	if len(urls) > 0 {
		if err := n.client.Submit(ctx, urls); err != nil {
			return 0, err
		}
	}
	n.saveRun(ctx, runStart)
	return len(urls), nil
}

func (n *IndexNowNotifier) lastRun(ctx context.Context, now time.Time) time.Time {
	if n.state != nil {
		if raw, ok := n.state.Get(ctx, indexNowWatermarkKey); ok {
			if t, err := time.Parse(time.RFC3339, raw); err == nil && t.After(n.watermark) {
				n.watermark = t
			}
		}
	}
	if n.watermark.IsZero() {
		return now.Add(-indexNowFirstRunLookback)
	}
	return n.watermark
}

func (n *IndexNowNotifier) saveRun(ctx context.Context, t time.Time) {
	n.watermark = t
	if n.state != nil && !n.state.Set(ctx, indexNowWatermarkKey, t.Format(time.RFC3339), indexNowStateTTL) {
		n.log.Warn("IndexNow : date du passage non enregistrée dans Redis (gardée en mémoire)")
	}
}
