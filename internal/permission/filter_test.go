package permission

import (
	"reflect"
	"testing"
)

// TestFilterValid is the output-side counterpart of keys_gen_test.go (which
// keeps the Go catalog honest against the DB catalog) and
// TestRBACPermissionCoverage (which keeps the catalog honest against the
// router): this one keeps whatever the login response emits honest against
// the catalog — a key that isn't in All must never survive the trip out.
func TestFilterValid(t *testing.T) {
	in := []string{
		string(StaffManage),
		"totally.bogus.key",
		string(SettingsManage),
		"pos.access", // retired in RBAC lot 8 — must not resurrect if stale data lingers
	}
	want := []string{string(StaffManage), string(SettingsManage)}

	got := FilterValid(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterValid(%v) = %v, want %v", in, got, want)
	}
}

func TestFilterValid_AllCatalogKeysSurvive(t *testing.T) {
	in := make([]string, 0, len(All))
	for _, k := range All {
		in = append(in, string(k))
	}

	got := FilterValid(in)
	if len(got) != len(All) {
		t.Fatalf("FilterValid(all catalog keys) dropped %d key(s): got %v, want all %d keys preserved", len(All)-len(got), got, len(All))
	}
}

func TestFilterValid_EmptyAndNilInput(t *testing.T) {
	if got := FilterValid(nil); len(got) != 0 {
		t.Fatalf("FilterValid(nil) = %v, want empty", got)
	}
	if got := FilterValid([]string{}); len(got) != 0 {
		t.Fatalf("FilterValid([]string{}) = %v, want empty", got)
	}
}
