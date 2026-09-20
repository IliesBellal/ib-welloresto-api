package cds

import (
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// TestResolveStatus verrouille la règle de CDS_DECISIONS.md D1, qui est le
// piège central de ce module : ni isDistributed ni brand_status ne suffisent
// seuls.
//
// Le cas qui compte vraiment est celui de la commande sur place : son
// brand_status vaut 'DONE' une fois distribuée, jamais READY_*. Une
// implémentation qui ne regarderait que brand_status la laisserait
// éternellement en « En préparation » sur l'écran.
func TestResolveStatus(t *testing.T) {
	tests := []struct {
		name          string
		isDistributed bool
		brandStatus   string
		want          string
	}{
		{
			name:          "commande sur place distribuee - DONE mais bien prete",
			isDistributed: true,
			brandStatus:   "DONE",
			want:          "ready",
		},
		{
			name:          "commande a emporter distribuee",
			isDistributed: true,
			brandStatus:   "READY_FOR_TAKE_AWAY",
			want:          "ready",
		},
		{
			name:          "commande livraison distribuee",
			isDistributed: true,
			brandStatus:   "READY_FOR_HANDOFF",
			want:          "ready",
		},
		{
			// SetReadyForDistribution (PATCH /orders/{id}/distributed) pose
			// READY_* sans toucher isDistributed : ce chemin doit compter.
			name:          "prete via SetReadyForDistribution sans isDistributed",
			isDistributed: false,
			brandStatus:   "READY_FOR_TAKE_AWAY",
			want:          "ready",
		},
		{
			name:          "en preparation",
			isDistributed: false,
			brandStatus:   "PENDING",
			want:          "preparing",
		},
		{
			// MarkProductsBackToProduction remet isDistributed a false et
			// brand_status a PENDING : la commande redescend en preparation.
			name:          "retour en production",
			isDistributed: false,
			brandStatus:   "PENDING",
			want:          "preparing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := boardOrderRow{
				IsDistributed: tt.isDistributed,
				BrandStatus:   nullStr(tt.brandStatus),
			}
			if got := resolveStatus(row); got != tt.want {
				t.Errorf("resolveStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveLabel verrouille la cascade de CDS_DECISIONS.md D3 :
// prénom client -> pager -> order_num, puis les replis.
func TestResolveLabel(t *testing.T) {
	tests := []struct {
		name      string
		row       boardOrderRow
		wantLabel string
		wantKind  string
	}{
		{
			name: "prenom client prioritaire sur tout le reste",
			row: boardOrderRow{
				CustomerFirstName: nullStr("Paul"),
				PagerNumber:       nullStr("12"),
				OrderNum:          nullStr("123"),
			},
			wantLabel: "Paul",
			wantKind:  "first_name",
		},
		{
			name: "pager quand pas de prenom",
			row: boardOrderRow{
				PagerNumber: nullStr("12"),
				OrderNum:    nullStr("123"),
			},
			wantLabel: "12",
			wantKind:  "pager",
		},
		{
			name:      "order_num quand ni prenom ni pager",
			row:       boardOrderRow{OrderNum: nullStr("123")},
			wantLabel: "123",
			wantKind:  "order_num",
		},
		{
			name:      "numero plateforme pour une commande marketplace anonyme",
			row:       boardOrderRow{BrandOrderNum: nullStr("UE-889")},
			wantLabel: "UE-889",
			wantKind:  "brand_order_num",
		},
		{
			// Un bloc sans libelle n'a aucune valeur pour le client : on
			// affiche toujours quelque chose, meme illisible.
			name:      "repli sur la fin de l'order_id",
			row:       boardOrderRow{OrderID: "order-abcdef1234"},
			wantLabel: "1234",
			wantKind:  "fallback",
		},
		{
			// Un champ rempli d'espaces ne doit pas court-circuiter la
			// cascade et produire un bloc visuellement vide.
			name: "champ blanc ignore",
			row: boardOrderRow{
				CustomerFirstName: nullStr("   "),
				OrderNum:          nullStr("123"),
			},
			wantLabel: "123",
			wantKind:  "order_num",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, kind := resolveLabel(tt.row)
			if label != tt.wantLabel || kind != tt.wantKind {
				t.Errorf("resolveLabel() = (%q, %q), want (%q, %q)", label, kind, tt.wantLabel, tt.wantKind)
			}
		})
	}
}

// TestGenerateEnrollmentCode vérifie le format imposé par la saisie à la
// télécommande (D14/D16) : exactement 6 chiffres, zéros de tête compris.
//
// Un code tiré à 42 doit s'afficher "000042" et non "42", sinon le pavé
// numérique attendrait six frappes pour un code qui n'en compte que deux.
func TestGenerateEnrollmentCode(t *testing.T) {
	seen := map[string]bool{}

	for i := 0; i < 200; i++ {
		code, err := generateEnrollmentCode()
		if err != nil {
			t.Fatalf("generateEnrollmentCode() error = %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("generateEnrollmentCode() = %q, want exactly 6 characters", code)
		}
		if _, err := strconv.Atoi(code); err != nil {
			t.Fatalf("generateEnrollmentCode() = %q, want digits only", code)
		}
		seen[code] = true
	}

	// Garde-fou minimal contre un générateur constant ou quasi constant. Sur
	// 200 tirages dans un espace de 10^6, les collisions sont négligeables :
	// moins de 150 valeurs distinctes signalerait un problème réel, pas une
	// malchance.
	if len(seen) < 150 {
		t.Errorf("generateEnrollmentCode() produced only %d distinct values over 200 draws", len(seen))
	}
}

// TestTruncateRunes verrouille la régression du premier enrôlement réel :
// `ERROR: value too long for type character varying(50) (SQLSTATE 22001)`.
//
// L'application Flutter envoyait Platform.operatingSystemVersion, qui sur
// Android est toute la chaîne noyau (« Linux 5.15.x-android13-… #1 SMP
// PREEMPT … »), bien au-delà des 50 caractères de cds_displays.os_version.
func TestTruncateRunes(t *testing.T) {
	kernel := "Linux 5.15.104-android13-8-27890426-abogki #1 SMP PREEMPT Mon Jan 1 00:00:00 UTC 2024"
	if got := truncateRunes(kernel, maxOSVersionLen); len([]rune(got)) != maxOSVersionLen {
		t.Errorf("truncateRunes(kernel, %d) = %d runes, want exactly %d", maxOSVersionLen, len([]rune(got)), maxOSVersionLen)
	}

	if got := truncateRunes("Android 13", maxOSVersionLen); got != "Android 13" {
		t.Errorf("une chaine courte ne doit pas etre modifiee, got %q", got)
	}

	// Le comptage se fait en caracteres : 60 « é » tiennent sur 120 octets
	// mais 60 caracteres, et doivent etre coupes a 50 sans corrompre l'UTF-8.
	accents := strings.Repeat("é", 60)
	got := truncateRunes(accents, 50)
	if len([]rune(got)) != 50 {
		t.Errorf("truncateRunes sur accents = %d runes, want 50", len([]rune(got)))
	}
	if !utf8.ValidString(got) {
		t.Error("truncateRunes ne doit jamais produire de l'UTF-8 invalide")
	}
}

// TestResolveEnrollmentName verrouille la règle de nommage : le nom choisi au
// back-office prime sur celui que l'appareil s'est donné.
func TestResolveEnrollmentName(t *testing.T) {
	str := func(s string) *string { return &s }

	tests := []struct {
		name       string
		codeName   *string
		deviceName string
		want       string
		wantErr    bool
	}{
		{"le nom du code prime sur celui de l'appareil", str("Comptoir"), "Ecran Android Box", "Comptoir", false},
		{"espaces du nom du code ignores", str("  Comptoir  "), "Ecran Android Box", "Comptoir", false},
		{"un code sans nom retombe sur l'appareil", nil, "Ecran Android Box", "Ecran Android Box", false},
		{"un nom de code vide retombe sur l'appareil", str("   "), "Ecran Android Box", "Ecran Android Box", false},
		// Le nom de l'appareil n'a pas a etre valide quand le code porte le
		// sien : un ancien client qui enverrait un nom vide doit pouvoir
		// s'enroler.
		{"nom d'appareil invalide sans consequence si le code a un nom", str("Comptoir"), "", "Comptoir", false},
		{"ni nom de code ni nom d'appareil", nil, "", "", true},
		{"nom d'appareil trop long sans nom de code", nil, strings.Repeat("a", 101), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveEnrollmentName(tt.codeName, tt.deviceName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveEnrollmentName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("resolveEnrollmentName() = %q, want %q", got, tt.want)
			}
		})
	}
}
