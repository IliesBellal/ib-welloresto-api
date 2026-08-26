package scannorder

import (
	"context"
	"testing"

	"welloresto-api/internal/models"
)

// TestGetEffectivePrepMinutes_Manual verrouille le contrat du temps d'attente
// supplementaire temporaire sur le mode MANUAL (le seul joignable sans base :
// le mode AUTO delegue a orderLifeCycleSvc).
//
// L'enjeu principal est la non-regression : tant qu'aucun supplement n'est
// actif, la valeur annoncee doit rester exactement celle d'avant la
// fonctionnalite. row.ExtraPrepMinutes arrive deja filtre par son echeance
// (snoActiveExtraPrepMinutes), donc un supplement expire se presente ici comme
// un zero — c'est ce zero qui doit etre neutre.
func TestGetEffectivePrepMinutes_Manual(t *testing.T) {
	tests := []struct {
		name             string
		prepTime         int
		extraPrepMinutes int
		want             int
	}{
		{
			name:             "sans supplement, le temps de base est inchange",
			prepTime:         20,
			extraPrepMinutes: 0,
			want:             20,
		},
		{
			name:             "supplement actif, additionne au temps de base",
			prepTime:         20,
			extraPrepMinutes: 15,
			want:             35,
		},
		{
			name:             "supplement expire (filtre en SQL) traite comme absent",
			prepTime:         25,
			extraPrepMinutes: 0,
			want:             25,
		},
		{
			name:             "supplement negatif ignore, jamais soustrait",
			prepTime:         20,
			extraPrepMinutes: -10,
			want:             20,
		},
		{
			name:             "temps de base nul avec supplement actif",
			prepTime:         0,
			extraPrepMinutes: 10,
			want:             10,
		},
	}

	svc := &Service{}
	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := &models.MerchantRow{
				PrepTimeMode:     "MANUAL",
				PrepTime:         tc.prepTime,
				ExtraPrepMinutes: tc.extraPrepMinutes,
			}

			if got := svc.GetEffectivePrepMinutes(ctx, row); got != tc.want {
				t.Fatalf("GetEffectivePrepMinutes = %d, want %d", got, tc.want)
			}
		})
	}
}
