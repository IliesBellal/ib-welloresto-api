package version

import "testing"

func TestMajorRoot(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })
	for v, want := range map[string]string{"2.0.0": "2", "2.4.1": "2", "3.0.0": "3", "10.1.0": "10"} {
		Version = v
		if got := MajorRoot(); got != want {
			t.Fatalf("MajorRoot(%s) = %s, want %s", v, got, want)
		}
	}
	Version = "2.3.1"
	if got := MinorPattern(); got != "2.x.y" {
		t.Fatalf("MinorPattern = %s", got)
	}
}
