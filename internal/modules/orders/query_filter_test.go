package orders

import (
	"reflect"
	"testing"
)

func TestNewFilter_SingleValue(t *testing.T) {
	f := NewFilter(" AND o.order_id = ? ", "order_42")

	if f.SQL != " AND o.order_id = ? " {
		t.Fatalf("unexpected SQL: %q", f.SQL)
	}
	if !reflect.DeepEqual(f.Args, []interface{}{"order_42"}) {
		t.Fatalf("unexpected Args: %v", f.Args)
	}
}

func TestInFilter_BuildsPlaceholdersAndOrderedArgs(t *testing.T) {
	f := InFilter("o.order_id", []string{"order_1", "order_2", "order_3"})

	wantSQL := " AND o.order_id IN (?,?,?) "
	if f.SQL != wantSQL {
		t.Fatalf("SQL = %q, want %q", f.SQL, wantSQL)
	}

	wantArgs := []interface{}{"order_1", "order_2", "order_3"}
	if !reflect.DeepEqual(f.Args, wantArgs) {
		t.Fatalf("Args = %v, want %v", f.Args, wantArgs)
	}
}

func TestInFilter_EmptyValues_ReturnsEmptyFilter(t *testing.T) {
	f := InFilter("o.order_id", nil)
	if f.SQL != "" || len(f.Args) != 0 {
		t.Fatalf("expected empty filter, got %+v", f)
	}
}

func TestInFilter_NoValueEverInterpolatedIntoSQL(t *testing.T) {
	// Régression : la valeur d'un ID ne doit jamais apparaître dans le SQL lui-même,
	// uniquement dans Args, quel que soit son contenu (y compris des caractères
	// qui casseraient une requête construite par interpolation, ex: guillemet simple).
	f := InFilter("o.order_id", []string{"a' OR '1'='1"})

	if f.SQL != " AND o.order_id IN (?) " {
		t.Fatalf("SQL should only contain a placeholder, got %q", f.SQL)
	}
	if len(f.Args) != 1 || f.Args[0] != "a' OR '1'='1" {
		t.Fatalf("value should be carried in Args untouched, got %v", f.Args)
	}
}

func TestQueryFilter_Append_PreservesOrder(t *testing.T) {
	base := NewFilter(" AND o.merchant_id = ? ", "merchant_1")
	extra := InFilter("o.order_id", []string{"a", "b"})

	combined := base.Append(extra)

	wantSQL := " AND o.merchant_id = ?  AND o.order_id IN (?,?) "
	if combined.SQL != wantSQL {
		t.Fatalf("SQL = %q, want %q", combined.SQL, wantSQL)
	}

	wantArgs := []interface{}{"merchant_1", "a", "b"}
	if !reflect.DeepEqual(combined.Args, wantArgs) {
		t.Fatalf("Args = %v, want %v", combined.Args, wantArgs)
	}
}
