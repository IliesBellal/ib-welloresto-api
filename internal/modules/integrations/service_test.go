package integrations

import (
	"context"
	"testing"
	"time"
)

func TestBuildScanNOrderAccessURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		slug    string
		want    *string
	}{
		{
			name:    "base et slug renseignés",
			baseURL: "https://scannorder.welloresto.fr",
			slug:    "le-bistrot",
			want:    strPtr("https://scannorder.welloresto.fr/restaurant/le-bistrot"),
		},
		{
			name:    "slash final de la base URL absorbé",
			baseURL: "https://scannorder.welloresto.fr/",
			slug:    "le-bistrot",
			want:    strPtr("https://scannorder.welloresto.fr/restaurant/le-bistrot"),
		},
		{
			name:    "base URL non configurée",
			baseURL: "",
			slug:    "le-bistrot",
			want:    nil,
		},
		{
			name:    "merchant sans QR principal",
			baseURL: "https://scannorder.welloresto.fr",
			slug:    "",
			want:    nil,
		},
		{
			name:    "slug blanc traité comme absent",
			baseURL: "https://scannorder.welloresto.fr",
			slug:    "   ",
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildScanNOrderAccessURL(tc.baseURL, tc.slug)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("buildScanNOrderAccessURL = %q, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("buildScanNOrderAccessURL = nil, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("buildScanNOrderAccessURL = %q, want %q", *got, *tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func intPtr(i int) *int { return &i }

// TestSetWaitTimeIntegrations_Validation couvre les refus qui interviennent
// avant tout appel plateforme — donc sans base ni API externe. Un payload
// invalide doit etre rejete la, jamais applique a moitie.
func TestSetWaitTimeIntegrations_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  *SetWaitTimeRequest
	}{
		{
			name: "supplement nul",
			req: &SetWaitTimeRequest{
				WaitTimeMinutes:      0,
				AffectedIntegrations: []string{"scannorder"},
			},
		},
		{
			name: "supplement negatif",
			req: &SetWaitTimeRequest{
				WaitTimeMinutes:      -5,
				AffectedIntegrations: []string{"scannorder"},
			},
		},
		{
			name: "aucune plateforme",
			req: &SetWaitTimeRequest{
				WaitTimeMinutes:      10,
				AffectedIntegrations: []string{},
			},
		},
		{
			name: "fenetre d'application nulle",
			req: &SetWaitTimeRequest{
				WaitTimeMinutes:      10,
				AffectedIntegrations: []string{"scannorder"},
				DurationMinutes:      intPtr(0),
			},
		},
	}

	svc := &Service{}
	ctx := context.Background()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.SetWaitTimeIntegrations(ctx, "merchant-1", tc.req)
			if err == nil {
				t.Fatal("SetWaitTimeIntegrations = nil error, want une erreur de validation")
			}
		})
	}
}

// TestSetWaitTimeIntegrations_UnknownPlatform verifie qu'une plateforme inconnue
// ne fait pas echouer l'appel : elle est loguee et ignoree, comme dans
// CloseTemporaryIntegrations. Le comportement des deux endpoints jumeaux doit
// rester aligne — c'est aussi ce qui rend le test executable sans base, aucune
// branche plateforme n'etant atteinte.
func TestSetWaitTimeIntegrations_UnknownPlatform(t *testing.T) {
	svc := &Service{}
	before := time.Now().UTC()

	appliedUntil, affected, err := svc.SetWaitTimeIntegrations(context.Background(), "merchant-1", &SetWaitTimeRequest{
		WaitTimeMinutes:      10,
		AffectedIntegrations: []string{"just_eat"},
	})
	if err != nil {
		t.Fatalf("SetWaitTimeIntegrations: %v", err)
	}

	if len(affected) != 1 || affected[0] != "just_eat" {
		t.Fatalf("affected = %v, want [just_eat]", affected)
	}

	// Fenetre par defaut appliquee faute de duration_minutes explicite.
	wantAtLeast := before.Add(time.Duration(defaultWaitTimeWindowMinutes) * time.Minute)
	if appliedUntil.Before(wantAtLeast) {
		t.Fatalf("appliedUntil = %v, want >= %v", appliedUntil, wantAtLeast)
	}
}

// TestSetWaitTimeIntegrations_DeliverooExcluded verrouille le refus explicite de
// Deliveroo sur le supplement temporaire : la plateforme ne doit jamais etre
// annoncee comme traitee, sous peine de laisser croire au restaurateur qu'un
// delai est applique alors que rien n'a ete pousse.
//
// Le test passe sans base ni API externe justement parce que la branche
// deliveroo sort avant tout appel — c'est la propriete verifiee ici.
func TestSetWaitTimeIntegrations_DeliverooExcluded(t *testing.T) {
	svc := &Service{}

	_, affected, err := svc.SetWaitTimeIntegrations(context.Background(), "merchant-1", &SetWaitTimeRequest{
		WaitTimeMinutes:      10,
		AffectedIntegrations: []string{"deliveroo", "Deliveroo", "DELIVEROO"},
	})
	if err == nil {
		t.Fatal("SetWaitTimeIntegrations = nil error, want une erreur : aucune plateforme applicable")
	}
	if len(affected) != 0 {
		t.Fatalf("affected = %v, want vide — deliveroo ne doit jamais etre annonce comme traite", affected)
	}
}

// TestNormalizeIntegrationName fige la tolerance de nommage partagee par les
// deux endpoints globaux : le POS Flutter et le back-office envoient
// "uber_eats", mais un client tiers peut envoyer "Uber-Eats".
func TestNormalizeIntegrationName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "uber_eats", want: "uber_eats"},
		{in: "Uber-Eats", want: "uber_eats"},
		{in: "  DELIVEROO  ", want: "deliveroo"},
		{in: "scannorder", want: "scannorder"},
		{in: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeIntegrationName(tc.in); got != tc.want {
				t.Fatalf("normalizeIntegrationName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
