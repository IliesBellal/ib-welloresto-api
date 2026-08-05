package integrations

import "testing"

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
