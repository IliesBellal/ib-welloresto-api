package order_life_cycle

import (
	"context"
	"testing"
	"time"

	"welloresto-api/internal/models"
)

func TestApplyManualProductionReadyAt(t *testing.T) {
	// Livraison 00:10, trajet 25 min : cuisine auto à 23:45 la veille.
	auto := time.Date(2026, 11, 16, 23, 45, 0, 0, time.UTC)
	at := func(day, h, m int) *time.Time {
		v := time.Date(2026, 11, day, h, m, 0, 0, time.UTC)
		return &v
	}

	cases := []struct {
		name       string
		manual     *time.Time
		wantManual bool
		want       time.Time
	}{
		{"pas d'heure manuelle", nil, false, auto},
		{"avancée de 20 min", at(16, 23, 25), true, *at(16, 23, 25)},
		{"égale à l'auto = auto", at(16, 23, 45), false, auto},
		{"plus tard que l'auto refusée", at(17, 0, 5), false, auto},
		// Client qui a mis 23:30 sur le jour de livraison (17) au lieu de la
		// veille : ~24 h plus tard, refusé.
		{"mauvais jour après minuit", at(17, 23, 30), false, auto},
		// Ou sur le jour d'avant la veille : ~24 h trop tôt, refusé aussi.
		{"mauvais jour avant minuit", at(15, 23, 30), false, auto},
		{"avance maximale acceptée", at(16, 11, 45), true, *at(16, 11, 45)},
		{"avance au-delà de 12 h refusée", at(16, 11, 40), false, auto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, manual := applyManualProductionReadyAt(auto, c.manual)
			if manual != c.wantManual {
				t.Fatalf("manual = %v, want %v", manual, c.wantManual)
			}
			if !got.(time.Time).Equal(c.want) {
				t.Fatalf("at = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResolveProductionReadyAt(t *testing.T) {
	ready := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	travel := 20 * 60
	at := func(h, m int) time.Time { return time.Date(2026, 11, 18, h, m, 0, 0, time.UTC) }

	cases := []struct {
		name      string
		ready     interface{}
		scheduled bool
		travel    *int
		want      interface{}
	}{
		{"programmée livrée : tampon + trajet", ready, true, &travel, at(18, 36)},
		{"programmée sans trajet (emporter, sur place) : tampon seul", ready, true, nil, at(18, 56)},
		{"non programmée : inchangée", ready, false, nil, ready},
		{"non programmée avec trajet : inchangée", ready, false, &travel, ready},
		{"sans heure : nil", nil, true, &travel, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveProductionReadyAt(c.ready, c.scheduled, c.travel)
			if c.want == nil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if !got.(time.Time).Equal(c.want.(time.Time)) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestApplyManualProductionReadyAtWithoutAuto(t *testing.T) {
	manual := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	got, isManual := applyManualProductionReadyAt(nil, &manual)
	if got != nil || isManual {
		t.Fatalf("sans heure calculée : got %v / %v, want nil / false", got, isManual)
	}
}

func TestResolveRequestProductionReadyAt(t *testing.T) {
	ctx := context.Background()
	auto := time.Date(2026, 11, 16, 18, 15, 0, 0, time.UTC)
	existing := time.Date(2026, 11, 16, 17, 50, 0, 0, time.UTC)

	req := func(brand string, override *string) *models.RequestObject {
		return &models.RequestObject{Order: models.OrderRequest{Brand: brand, ProductionReadyOverride: override}}
	}

	t.Run("champ absent : l'heure enregistrée est gardée", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, nil), auto, &existing)
		if !manual || !got.(time.Time).Equal(existing) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("chaîne vide : retour à l'auto", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, strPtr("")), auto, &existing)
		if manual || !got.(time.Time).Equal(auto) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("heure demandée (fuseau Paris)", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, strPtr("2026-11-16T19:05:00+01:00")), auto, &existing)
		want := time.Date(2026, 11, 16, 18, 5, 0, 0, time.UTC)
		if !manual || !got.(time.Time).Equal(want) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("heure envoyée par la caisse (Dart, millisecondes)", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, strPtr("2026-11-16T18:05:00.000Z")), auto, &existing)
		want := time.Date(2026, 11, 16, 18, 5, 0, 0, time.UTC)
		if !manual || !got.(time.Time).Equal(want) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("heure illisible : auto", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, strPtr("18h05")), auto, &existing)
		if manual || !got.(time.Time).Equal(auto) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("plateforme : toujours auto", func(t *testing.T) {
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandUberEats, strPtr("2026-11-16T18:05:00Z")), auto, &existing)
		if manual || !got.(time.Time).Equal(auto) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
	t.Run("heure enregistrée devenue trop tardive : auto", func(t *testing.T) {
		earlierAuto := time.Date(2026, 11, 16, 17, 40, 0, 0, time.UTC)
		got, manual := resolveRequestProductionReadyAt(ctx, req(models.BrandWelloResto, nil), earlierAuto, &existing)
		if manual || !got.(time.Time).Equal(earlierAuto) {
			t.Fatalf("got %v / %v", got, manual)
		}
	})
}
