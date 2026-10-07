package helpers

import "testing"

func TestScanNOrderOrderURL(t *testing.T) {
	const pub = "order--ffe6c970-0701-4dd2-88ce-4e61a0da1d86"

	tests := []struct {
		name    string
		baseURL string
		slug    string
		pubID   string
		want    string
	}{
		{"base configurée", "https://scannorder.welloresto.fr", "le-bistrot", pub,
			"https://scannorder.welloresto.fr/restaurant/le-bistrot/order/" + pub},
		{"slash final retiré", "https://scannorder.welloresto.fr/", "le-bistrot", pub,
			"https://scannorder.welloresto.fr/restaurant/le-bistrot/order/" + pub},
		{"base vide : repli sur le domaine public", "", "le-bistrot", pub,
			"https://scannorder.welloresto.fr/restaurant/le-bistrot/order/" + pub},
		{"base blanche : repli sur le domaine public", "   ", "le-bistrot", pub,
			"https://scannorder.welloresto.fr/restaurant/le-bistrot/order/" + pub},
		{"slug manquant", "https://scannorder.welloresto.fr", "", pub, ""},
		{"id public manquant", "https://scannorder.welloresto.fr", "le-bistrot", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScanNOrderOrderURL(tt.baseURL, tt.slug, tt.pubID); got != tt.want {
				t.Errorf("ScanNOrderOrderURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsOrderPublicID(t *testing.T) {
	tests := map[string]bool{
		"order--ffe6c970-0701-4dd2-88ce-4e61a0da1d86": true,
		GeneratePrefixedID(OrderPublicIDPrefix):       true,
		"31329":                                       false,
		"":                                            false,
	}
	for ref, want := range tests {
		if got := IsOrderPublicID(ref); got != want {
			t.Errorf("IsOrderPublicID(%q) = %v, want %v", ref, got, want)
		}
	}
}
