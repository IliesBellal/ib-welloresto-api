package idempotency

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

type fakeStore struct {
	mu   sync.Mutex
	data map[string]string
	down bool // simule Redis indisponible : Get/SetNX/Set échouent sans erreur visible
}

func newFake() *fakeStore { return &fakeStore{data: map[string]string{}} }

func (f *fakeStore) Get(_ context.Context, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return "", false
	}
	v, ok := f.data[key]
	return v, ok
}

func (f *fakeStore) Set(_ context.Context, key, value string, _ time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false
	}
	f.data[key] = value
	return true
}

func (f *fakeStore) SetNX(_ context.Context, key, value string, _ time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return false
	}
	if _, ok := f.data[key]; ok {
		return false
	}
	f.data[key] = value
	return true
}

func (f *fakeStore) Delete(_ context.Context, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return true
}

var nop = zap.NewNop()

func TestRun_succes_puisDoublonIgnore(t *testing.T) {
	store := newFake()
	calls := 0
	fn := func() error { calls++; return nil }

	skipped, err := Run(context.Background(), store, nop, "evt-1", time.Hour, fn)
	if skipped || err != nil || calls != 1 {
		t.Fatalf("1er appel = (skipped %v, err %v, calls %d)", skipped, err, calls)
	}
	skipped, err = Run(context.Background(), store, nop, "evt-1", time.Hour, fn)
	if !skipped || err != nil || calls != 1 {
		t.Fatalf("doublon = (skipped %v, err %v, calls %d), want ignoré", skipped, err, calls)
	}
}

// Le défaut corrigé : un échec ne doit pas empêcher la nouvelle tentative de la
// plateforme d'être traitée.
func TestRun_echec_laNouvelleTentativeEstTraitee(t *testing.T) {
	store := newFake()
	calls := 0
	boom := errors.New("base indisponible")

	_, err := Run(context.Background(), store, nop, "evt-2", time.Hour, func() error { calls++; return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("erreur = %v, want %v", err, boom)
	}
	if _, found := store.Get(context.Background(), "evt-2"); found {
		t.Fatal("la réservation doit être levée après un échec")
	}

	skipped, err := Run(context.Background(), store, nop, "evt-2", time.Hour, func() error { calls++; return nil })
	if skipped || err != nil || calls != 2 {
		t.Fatalf("nouvelle tentative = (skipped %v, err %v, calls %d), want traitée", skipped, err, calls)
	}
}

func TestRun_panique_devientErreurEtLeveLaReservation(t *testing.T) {
	store := newFake()

	skipped, err := Run(context.Background(), store, nop, "evt-3", time.Hour, func() error { panic("index out of range") })
	if skipped || err == nil {
		t.Fatalf("panique = (skipped %v, err %v), want une erreur", skipped, err)
	}
	if _, found := store.Get(context.Background(), "evt-3"); found {
		t.Fatal("la réservation doit être levée après une panique")
	}
}

func TestRun_traitementEnCours_unDoublonSimultaneEstIgnore(t *testing.T) {
	store := newFake()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		_, _ = Run(context.Background(), store, nop, "evt-4", time.Hour, func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	calls := 0
	skipped, err := Run(context.Background(), store, nop, "evt-4", time.Hour, func() error { calls++; return nil })
	if !skipped || err != nil || calls != 0 {
		t.Fatalf("doublon simultané = (skipped %v, err %v, calls %d), want ignoré", skipped, err, calls)
	}
	close(release)
	<-done
}

// Redis indisponible ne doit jamais faire perdre un événement.
func TestRun_redisIndisponible_traiteSansReservation(t *testing.T) {
	store := newFake()
	store.down = true
	calls := 0

	skipped, err := Run(context.Background(), store, nop, "evt-5", time.Hour, func() error { calls++; return nil })
	if skipped || err != nil || calls != 1 {
		t.Fatalf("Redis down = (skipped %v, err %v, calls %d), want traité", skipped, err, calls)
	}
}

func TestRun_sansStore_traiteToujours(t *testing.T) {
	calls := 0
	skipped, err := Run(context.Background(), nil, nop, "evt-6", time.Hour, func() error { calls++; return nil })
	if skipped || err != nil || calls != 1 {
		t.Fatalf("sans store = (skipped %v, err %v, calls %d)", skipped, err, calls)
	}
}
