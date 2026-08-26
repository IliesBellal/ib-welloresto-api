package menu

import (
	"context"
	"sync"
	"testing"
	"time"

	"welloresto-api/internal/modules/notification"
)

type recordingBroadcaster struct {
	mu       sync.Mutex
	merchant []string
	payloads []map[string]interface{}
}

func (r *recordingBroadcaster) BroadcastToMerchant(merchantID string, payload map[string]interface{}) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.merchant = append(r.merchant, merchantID)
	r.payloads = append(r.payloads, payload)
	return true
}

func (r *recordingBroadcaster) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.merchant)
}

type recordingCache struct {
	merchants []string
}

func (c *recordingCache) InvalidateMerchantMenuCaches(_ context.Context, merchantID string) {
	c.merchants = append(c.merchants, merchantID)
}

// fakeClock pilote le temps et les minuteries sans faire dormir le test.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []pendingTimer
}

type pendingTimer struct {
	at time.Time
	fn func()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = append(c.pending, pendingTimer{at: c.now.Add(d), fn: fn})
}

// Advance fait avancer l'horloge et declenche les minuteries echues.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	due := make([]func(), 0, len(c.pending))
	remaining := c.pending[:0]
	for _, t := range c.pending {
		if !t.at.After(c.now) {
			due = append(due, t.fn)
		} else {
			remaining = append(remaining, t)
		}
	}
	c.pending = remaining
	c.mu.Unlock()

	for _, fn := range due {
		fn()
	}
}

func newTestNotifier(cache menuCacheInvalidator, b realtimeBroadcaster) (*MenuChangeNotifier, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)}
	n := NewMenuChangeNotifier(cache, b)
	n.now = clock.Now
	n.after = clock.After
	return n, clock
}

func TestChanged_BroadcastsImmediatelyOnFirstChange(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n, _ := newTestNotifier(nil, broadcaster)

	n.Changed(context.Background(), "m-1")

	if got := broadcaster.count(); got != 1 {
		t.Fatalf("%d diffusion(s), attendu 1 — une modification isolée doit partir tout de suite", got)
	}
	payload := broadcaster.payloads[0]
	if payload["type"] != notification.WSEventMenuUpdated {
		t.Errorf("type = %v, attendu %q", payload["type"], notification.WSEventMenuUpdated)
	}
	if payload["merchant_id"] != "m-1" {
		t.Errorf("merchant_id = %v, attendu %q", payload["merchant_id"], "m-1")
	}
}

// Le coeur de l'amortissement : une rafale ne doit produire qu'un front
// montant et un front descendant, pas un evenement par mutation.
func TestChanged_CoalescesBurstIntoTwoBroadcasts(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n, clock := newTestNotifier(nil, broadcaster)

	for i := 0; i < 50; i++ {
		n.Changed(context.Background(), "m-1")
		clock.Advance(10 * time.Millisecond)
	}

	// 50 mutations sur 500 ms : le front montant est parti, la cloture est
	// encore programmee.
	if got := broadcaster.count(); got != 1 {
		t.Fatalf("%d diffusion(s) pendant la rafale, attendu 1", got)
	}

	clock.Advance(time.Second)

	if got := broadcaster.count(); got != 2 {
		t.Fatalf("%d diffusion(s) après la rafale, attendu 2 (front montant + clôture)", got)
	}
}

// Sans front descendant, les clients resteraient sur l'etat du debut de la
// rafale : ils rechargent a t=0 pendant que les ecritures continuent.
func TestChanged_ClosingBroadcastFollowsLastMutation(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n, clock := newTestNotifier(nil, broadcaster)

	n.Changed(context.Background(), "m-1") // front montant
	clock.Advance(200 * time.Millisecond)
	n.Changed(context.Background(), "m-1") // dans la fenêtre → différée

	if got := broadcaster.count(); got != 1 {
		t.Fatalf("%d diffusion(s) avant échéance, attendu 1", got)
	}

	clock.Advance(800 * time.Millisecond) // fin de la fenêtre

	if got := broadcaster.count(); got != 2 {
		t.Fatalf("%d diffusion(s) après échéance, attendu 2", got)
	}
}

func TestChanged_SeparateMerchantsDoNotShareWindow(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n, _ := newTestNotifier(nil, broadcaster)

	n.Changed(context.Background(), "m-1")
	n.Changed(context.Background(), "m-2")

	if got := broadcaster.count(); got != 2 {
		t.Fatalf("%d diffusion(s), attendu 2 — l'amortissement est par merchant", got)
	}
}

func TestChanged_SpacedOutChangesEachBroadcast(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n, clock := newTestNotifier(nil, broadcaster)

	for i := 0; i < 3; i++ {
		n.Changed(context.Background(), "m-1")
		clock.Advance(2 * time.Second)
	}

	if got := broadcaster.count(); got != 3 {
		t.Fatalf("%d diffusion(s), attendu 3 — hors fenêtre, chaque modification part", got)
	}
}

// La purge de cache ne doit jamais etre amortie : une vitrine servirait un
// menu perime.
func TestChanged_CacheInvalidatedOnEveryChange(t *testing.T) {
	cache := &recordingCache{}
	broadcaster := &recordingBroadcaster{}
	n, clock := newTestNotifier(cache, broadcaster)

	for i := 0; i < 5; i++ {
		n.Changed(context.Background(), "m-1")
		clock.Advance(10 * time.Millisecond)
	}

	if len(cache.merchants) != 5 {
		t.Fatalf("%d purge(s) de cache, attendu 5 — la purge n'est pas amortie", len(cache.merchants))
	}
	if got := broadcaster.count(); got != 1 {
		t.Fatalf("%d diffusion(s), attendu 1 — la diffusion, elle, est amortie", got)
	}
}

func TestChanged_NilSafety(t *testing.T) {
	// Ni hub, ni cache : la mutation métier doit aboutir sans paniquer.
	n, _ := newTestNotifier(nil, nil)
	n.Changed(context.Background(), "m-1")

	// Notifier nil (service câblé sans temps réel).
	var nilNotifier *MenuChangeNotifier
	nilNotifier.Changed(context.Background(), "m-1")
}

func TestChanged_IgnoresEmptyMerchantID(t *testing.T) {
	cache := &recordingCache{}
	broadcaster := &recordingBroadcaster{}
	n, _ := newTestNotifier(cache, broadcaster)

	n.Changed(context.Background(), "")

	if got := broadcaster.count(); got != 0 {
		t.Errorf("%d diffusion(s), attendu 0", got)
	}
	if len(cache.merchants) != 0 {
		t.Errorf("%d purge(s), attendu 0", len(cache.merchants))
	}
}

// L'amortissement est protege par un mutex : le hub est partage par toutes les
// requetes HTTP en vol.
func TestChanged_ConcurrentCallsAreSafe(t *testing.T) {
	broadcaster := &recordingBroadcaster{}
	n := NewMenuChangeNotifier(nil, broadcaster)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Changed(context.Background(), "m-1")
		}()
	}
	wg.Wait()

	// Horloge réelle ici : on vérifie l'absence de course (via -race), et que
	// la rafale n'a pas produit 100 diffusions.
	if got := broadcaster.count(); got > 2 {
		t.Fatalf("%d diffusions pour 100 mutations simultanées, amortissement inopérant", got)
	}
}
