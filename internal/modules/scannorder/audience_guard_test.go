package scannorder

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsAutomatedUserAgent(t *testing.T) {
	robots := []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"Googlebot-Image/1.0",
		"Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)",
		"Mozilla/5.0 (compatible; Baiduspider/2.0)",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 HeadlessChrome/120.0 Safari/537.36",
		"Mozilla/5.0 (Linux; Android 11) Chrome-Lighthouse",
		"curl/8.4.0",
		"python-requests/2.31.0",
		"Go-http-client/2.0",
	}
	for _, ua := range robots {
		if !isAutomatedUserAgent(ua) {
			t.Errorf("%q devrait être reconnu comme automate", ua)
		}
	}

	humans := []string{
		"",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (Linux; Android 13; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 11; CUBOT P40) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Linux; Android 11; CUBOT_X30) AppleWebKit/537.36 Chrome/120.0.0.0 Mobile Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 300.0 FBAN/FBIOS",
	}
	for _, ua := range humans {
		if isAutomatedUserAgent(ua) {
			t.Errorf("%q ne devrait pas être pris pour un automate", ua)
		}
	}
}

func TestAudienceClientKey(t *testing.T) {
	cases := map[string]struct {
		xff, remote, want string
	}{
		"dernière entrée, ajoutée par le proxy": {"203.0.113.7, 198.51.100.4", "10.0.0.1:443", "198.51.100.4"},
		"entrée unique":                       {"198.51.100.4", "10.0.0.1:443", "198.51.100.4"},
		"falsification à gauche ignorée":      {"1.1.1.1, 2.2.2.2, 198.51.100.4", "10.0.0.1:443", "198.51.100.4"},
		"sans en-tête : adresse de connexion": {"", "192.0.2.9:5000", "192.0.2.9"},
		"en-tête illisible : connexion":       {"n'importe quoi", "192.0.2.9:5000", "192.0.2.9"},
	}
	for name, c := range cases {
		r := httptest.NewRequest("POST", "/scannorder/events", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		got, err := AudienceClientKey(r)
		if err != nil || got != c.want {
			t.Errorf("%s : clé = %q (err %v), want %q", name, got, err, c.want)
		}
	}
}

func TestAudienceGuardAllow_PerMinuteBudget(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 10, 0, time.UTC)
	g := newAudienceGuard()
	g.now = func() time.Time { return now }

	for i := 0; i < audienceEventsPerMinute; i++ {
		if !g.allow("m:1") {
			t.Fatalf("événement %d refusé avant le plafond", i)
		}
	}
	if g.allow("m:1") {
		t.Fatal("le plafond de la minute devrait refuser")
	}
	if !g.allow("m:2") {
		t.Fatal("le plafond d'un établissement ne doit pas toucher les autres")
	}

	now = now.Add(time.Minute)
	if !g.allow("m:1") {
		t.Fatal("une nouvelle minute doit repartir de zéro")
	}
}

func TestAudienceGuardAllow_NilAcceptsEverything(t *testing.T) {
	var g *audienceGuard
	if !g.allow("m:1") || !g.productKnown(context.Background(), "1", "p", nil) {
		t.Fatal("un garde nil doit tout accepter (Service construit à vide)")
	}
}

func TestAudienceGuardProductKnown(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	g := newAudienceGuard()
	g.now = func() time.Time { return now }

	var loads atomic.Int32
	catalog := map[string]struct{}{"p1": {}}
	load := func(_ context.Context, merchantID string) (map[string]struct{}, error) {
		loads.Add(1)
		return catalog, nil
	}
	ctx := context.Background()

	if !g.productKnown(ctx, "1", "p1", load) {
		t.Fatal("p1 appartient à l'établissement")
	}
	if g.productKnown(ctx, "1", "inconnu", load) {
		t.Fatal("un produit inconnu doit être refusé")
	}
	if loads.Load() != 1 {
		t.Fatalf("une seule lecture attendue, %d", loads.Load())
	}

	// Produit créé entre-temps : reconnu, mais seulement après le délai de
	// rafraîchissement — un identifiant bidon ne relance pas une lecture par requête.
	catalog = map[string]struct{}{"p1": {}, "p2": {}}
	for i := 0; i < 50; i++ {
		g.productKnown(ctx, "1", "bidon", load)
	}
	if loads.Load() != 1 {
		t.Fatalf("des identifiants bidon ont relancé la lecture : %d lectures", loads.Load())
	}
	now = now.Add(audienceCatalogRefreshFloor + time.Second)
	if !g.productKnown(ctx, "1", "p2", load) {
		t.Fatal("p2, créé depuis, doit être reconnu après le délai de rafraîchissement")
	}
	if loads.Load() != 2 {
		t.Fatalf("deux lectures attendues, %d", loads.Load())
	}

	// Au-delà de la durée de vie, la liste est relue même pour un produit connu.
	now = now.Add(audienceCatalogTTL + time.Second)
	g.productKnown(ctx, "1", "p1", load)
	if loads.Load() != 3 {
		t.Fatalf("trois lectures attendues après expiration, %d", loads.Load())
	}
}

func TestAudienceGuardProductKnown_FailsOpen(t *testing.T) {
	g := newAudienceGuard()
	failing := func(context.Context, string) (map[string]struct{}, error) {
		return nil, errors.New("base indisponible")
	}
	if !g.productKnown(context.Background(), "1", "p1", failing) {
		t.Fatal("une lecture en échec doit accepter l'événement")
	}
}
