package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// userAgent : Open Food Facts et Openverse demandent un User-Agent
// identifiant l'application.
const userAgent = "WelloRestoImageBank/1.0 (+https://welloresto.fr)"

// fetcher : GET avec reprises (429, 5xx, erreurs réseau) et pause minimale
// entre deux appels vers un même service.
type fetcher struct {
	client  *http.Client
	retries int
	backoff time.Duration
	last    map[string]time.Time
	minGap  map[string]time.Duration
	// maxRetries : nombre de reprises par service (défaut : retries).
	maxRetries map[string]int
	sleep      func(time.Duration)
}

func newFetcher() *fetcher {
	return &fetcher{
		client:  &http.Client{Timeout: 90 * time.Second},
		retries: 4,
		backoff: 5 * time.Second,
		last:    map[string]time.Time{},
		minGap:  map[string]time.Duration{},
		// Une photo inaccessible n'est réessayée qu'une fois : on passe au
		// candidat suivant plutôt que d'attendre.
		maxRetries: map[string]int{"image": 1},
		sleep:      time.Sleep,
	}
}

// pace attend que l'intervalle minimal du service soit écoulé.
func (f *fetcher) pace(service string) {
	if gap := f.minGap[service]; gap > 0 {
		if wait := gap - time.Since(f.last[service]); wait > 0 {
			f.sleep(wait)
		}
	}
	f.last[service] = time.Now()
}

// get renvoie le corps d'une réponse 200. headers facultatifs.
func (f *fetcher) get(ctx context.Context, service, url string, headers map[string]string, maxBytes int64) ([]byte, error) {
	var lastErr error
	retries := f.retries
	if r, ok := f.maxRetries[service]; ok {
		retries = r
	}
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			f.sleep(f.backoff * time.Duration(1<<(attempt-1)))
		}
		f.pace(service)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK && readErr == nil:
			if int64(len(body)) > maxBytes {
				return nil, fmt.Errorf("réponse trop volumineuse (%s)", url)
			}
			return body, nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 && s <= 120 {
				f.sleep(time.Duration(s) * time.Second)
			}
		case readErr != nil:
			lastErr = readErr
		default:
			return nil, fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
		}
	}
	return nil, fmt.Errorf("échec après %d essais : %w", retries+1, lastErr)
}
