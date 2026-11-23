package scannorder

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.uber.org/zap"
)

type fakeSitemap struct {
	merchants, franchises []SitemapEntry
}

func (f fakeSitemap) GetSitemapEntries(context.Context) ([]SitemapEntry, error) {
	return f.merchants, nil
}
func (f fakeSitemap) GetSitemapFranchises(context.Context) ([]SitemapEntry, error) {
	return f.franchises, nil
}

type fakeSubmitter struct {
	calls [][]string
	err   error
}

func (f *fakeSubmitter) Submit(_ context.Context, urls []string) error {
	f.calls = append(f.calls, urls)
	return f.err
}

type memState map[string]string

func (m memState) Get(_ context.Context, k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memState) Set(_ context.Context, k, v string, _ time.Duration) bool {
	m[k] = v
	return true
}

func TestIndexNowNotifier(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	sub := &fakeSubmitter{}
	state := memState{}
	n := &IndexNowNotifier{
		sitemap: fakeSitemap{
			merchants: []SitemapEntry{
				{Slug: "recent", UpdatedAt: at(-time.Hour)},
				{Slug: "ancien", UpdatedAt: at(-48 * time.Hour)},
				{Slug: "sans-date"},
			},
			franchises: []SitemapEntry{{Slug: "franchise-x", UpdatedAt: at(-10 * time.Minute)}},
		},
		client: sub, state: state, baseURL: "https://sno.example", log: zap.NewNop(),
		now: func() time.Time { return now },
	}

	// Premier passage : pages modifiées dans les dernières 24 h seulement.
	count, err := n.Run(context.Background())
	if err != nil || count != 2 {
		t.Fatalf("Run = %d, %v ; want 2", count, err)
	}
	want := []string{"https://sno.example/restaurant/recent", "https://sno.example/hub/franchise-x"}
	if !reflect.DeepEqual(sub.calls[0], want) {
		t.Fatalf("URL = %v, want %v", sub.calls[0], want)
	}
	if state[indexNowWatermarkKey] != now.Format(time.RFC3339) {
		t.Fatalf("date enregistrée = %q", state[indexNowWatermarkKey])
	}

	// Passage suivant, rien de neuf : aucun appel.
	now = now.Add(15 * time.Minute)
	if count, err := n.Run(context.Background()); err != nil || count != 0 || len(sub.calls) != 1 {
		t.Fatalf("Run sans changement = %d, %v (%d appels)", count, err, len(sub.calls))
	}

	// Une page modifiée, envoi en échec : la date n'avance pas, renvoi au passage suivant.
	n.sitemap = fakeSitemap{merchants: []SitemapEntry{{Slug: "recent", UpdatedAt: at(-time.Minute)}}}
	sub.err = errors.New("HTTP 429")
	saved := state[indexNowWatermarkKey]
	if _, err := n.Run(context.Background()); err == nil || state[indexNowWatermarkKey] != saved {
		t.Fatalf("échec : err = %v, date %q → %q", err, saved, state[indexNowWatermarkKey])
	}
	sub.err = nil
	now = now.Add(15 * time.Minute)
	if count, err := n.Run(context.Background()); err != nil || count != 1 {
		t.Fatalf("renvoi après échec = %d, %v", count, err)
	}
}

func TestIndexNowNotifier_Disabled(t *testing.T) {
	var n *IndexNowNotifier
	if count, err := n.Run(context.Background()); count != 0 || err != nil {
		t.Fatalf("notifier nil = %d, %v", count, err)
	}
	n = NewIndexNowNotifier(nil, nil, nil, "https://sno.example", zap.NewNop())
	if count, err := n.Run(context.Background()); count != 0 || err != nil {
		t.Fatalf("sans client = %d, %v", count, err)
	}
}
