package kiosk

import (
	"errors"
	"reflect"
	"testing"

	"welloresto-api/internal/models"
)

func intPtr(v int) *int { return &v }

func TestNormalizeMarketingDuration(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		seconds *int
		want    *int
		wantErr bool
	}{
		{"image sans durée : suit le défaut", marketingKindImage, nil, nil, false},
		{"image à 0 : suit le défaut", marketingKindImage, intPtr(0), nil, false},
		{"image avec durée propre", marketingKindImage, intPtr(15), intPtr(15), false},
		{"borne basse", marketingKindImage, intPtr(3), intPtr(3), false},
		{"borne haute", marketingKindImage, intPtr(120), intPtr(120), false},
		{"sous la borne", marketingKindImage, intPtr(2), nil, true},
		{"au-dessus de la borne", marketingKindImage, intPtr(121), nil, true},
		{"négative", marketingKindImage, intPtr(-5), nil, true},
		{"vidéo : durée ignorée", marketingKindVideo, intPtr(30), nil, false},
		{"vidéo : même hors bornes", marketingKindVideo, intPtr(9999), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeMarketingDuration(tc.kind, tc.seconds)
			if tc.wantErr {
				if !errors.Is(err, models.ErrKioskMediaInvalid) {
					t.Fatalf("erreur = %v, attendu ErrKioskMediaInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToMarketingMediaResponse_ResolvesDuration(t *testing.T) {
	own := MarketingMediaRow{ID: "a", Kind: marketingKindImage, URL: "u", DurationSeconds: intPtr(20)}
	inherited := MarketingMediaRow{ID: "b", Kind: marketingKindImage, URL: "u"}

	if got := toMarketingMediaResponse(own, 10); got.DurationSeconds != 20 || !got.CustomDuration {
		t.Fatalf("durée propre : got %+v", got)
	}
	if got := toMarketingMediaResponse(inherited, 10); got.DurationSeconds != 10 || got.CustomDuration {
		t.Fatalf("durée héritée : got %+v", got)
	}
}

func TestToMarketingMediaResponses_NeverNil(t *testing.T) {
	// `[]` et non `null` : la borne décode la liste sans tester son absence.
	if got := toMarketingMediaResponses(nil, 10); got == nil || len(got) != 0 {
		t.Fatalf("got %#v, attendu une slice vide non nil", got)
	}
}

func TestMarketingDefaultDuration_Fallback(t *testing.T) {
	cases := []struct {
		name string
		row  *KioskSettingsRow
		want int
	}{
		{"ligne absente", nil, defaultMarketingDurationSeconds},
		{"ligne antérieure à la migration (0)", &KioskSettingsRow{}, defaultMarketingDurationSeconds},
		{"valeur hors bornes", &KioskSettingsRow{MarketingDefaultDurationSeconds: 500}, defaultMarketingDurationSeconds},
		{"valeur valide", &KioskSettingsRow{MarketingDefaultDurationSeconds: 25}, 25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketingDefaultDuration(tc.row); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultKioskSettingsRow_RibbonDefaults(t *testing.T) {
	row := defaultKioskSettingsRow("m1")
	if row.MarketingRibbonEnabled {
		t.Fatal("le ruban doit être désactivé par défaut")
	}
	// Un 0 écrit par UpsertSettings violerait le CHECK de la base.
	if !validMarketingDuration(row.MarketingDefaultDurationSeconds) {
		t.Fatalf("durée par défaut invalide : %d", row.MarketingDefaultDurationSeconds)
	}
}

func TestMergeMarketingOrder(t *testing.T) {
	existing := []string{"a", "b", "c", "d"}

	cases := []struct {
		name      string
		requested []string
		want      []string
		wantErr   bool
	}{
		{"ordre complet", []string{"d", "c", "b", "a"}, []string{"d", "c", "b", "a"}, false},
		{"liste périmée : les médias non cités suivent", []string{"c", "a"}, []string{"c", "a", "b", "d"}, false},
		{"liste vide : ordre inchangé", nil, []string{"a", "b", "c", "d"}, false},
		{"identifiant inconnu", []string{"a", "zzz"}, nil, true},
		{"identifiant en double", []string{"a", "a"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeMarketingOrder(existing, tc.requested)
			if tc.wantErr {
				if !errors.Is(err, models.ErrKioskMediaInvalid) {
					t.Fatalf("erreur = %v, attendu ErrKioskMediaInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue : %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSniffMarketingMime(t *testing.T) {
	pad := func(b []byte) []byte {
		out := make([]byte, 64)
		copy(out, b)
		return out
	}
	ftyp := func(brand string) []byte {
		return pad(append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p'}, []byte(brand)...))
	}

	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"mp4 isom", ftyp("isom"), "video/mp4"},
		{"mp4 mp42", ftyp("mp42"), "video/mp4"},
		{"mp4 M4V", ftyp("M4V "), "video/mp4"},
		{"HEIC pris pour une vidéo : refusé", ftyp("heic"), ""},
		{"AVIF : refusé", ftyp("avif"), ""},
		{"QuickTime : refusé", ftyp("qt  "), ""},
		{"webm", pad([]byte{0x1A, 0x45, 0xDF, 0xA3}), "video/webm"},
		{"png", pad([]byte("\x89PNG\r\n\x1a\n")), "image/png"},
		{"jpeg", pad([]byte{0xFF, 0xD8, 0xFF, 0xE0}), "image/jpeg"},
		{"webp", pad([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")), "image/webp"},
		{"gif : refusé", pad([]byte("GIF89a")), ""},
		{"texte renommé en .mp4 : refusé", pad([]byte("hello world")), ""},
		{"exécutable : refusé", pad([]byte("MZ\x90\x00")), ""},
		{"fichier vide", nil, ""},
		{"trop court", []byte{0x1A, 0x45}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sniffMarketingMime(tc.head); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
