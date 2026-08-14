package importer

import "testing"

func ptrStr(s string) *string { return &s }

func customerRow(t *testing.T, res *PreviewResult, externalID string) PreviewRow {
	t.Helper()
	for _, row := range res.Rows {
		if row.ExternalID == externalID {
			return row
		}
	}
	t.Fatalf("ligne %q absente du resultat", externalID)
	return PreviewRow{}
}

func TestBuildPreviewCreate(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Name: "Jean Dupont", Email: ptrStr("jean@example.com")},
		},
	}

	res := BuildPreview(imp, PreviewLookups{})

	row := customerRow(t, res, "Z1")
	if row.Status != StatusCreate {
		t.Fatalf("Status = %q, want %q", row.Status, StatusCreate)
	}
	if row.Resolution != ResolutionCreate {
		t.Fatalf("Resolution = %q, want %q", row.Resolution, ResolutionCreate)
	}
	if res.Summary.ToCreate != 1 || res.Summary.Total != 1 {
		t.Fatalf("Summary = %+v, want ToCreate=1 Total=1", res.Summary)
	}
}

func TestBuildPreviewDuplicateEmailOnly(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("jean@example.com")},
		},
	}
	lk := PreviewLookups{ByEmail: map[string]int{"jean@example.com": 42}}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusDuplicate {
		t.Fatalf("Status = %q, want %q", row.Status, StatusDuplicate)
	}
	if row.MatchedBy != MatchedByEmail {
		t.Fatalf("MatchedBy = %q, want %q", row.MatchedBy, MatchedByEmail)
	}
	if row.MatchedCustomerID != 42 {
		t.Fatalf("MatchedCustomerID = %d, want 42", row.MatchedCustomerID)
	}
	if row.Resolution != ResolutionSkip {
		t.Fatalf("Resolution = %q, want %q", row.Resolution, ResolutionSkip)
	}
	if res.Summary.Duplicates != 1 {
		t.Fatalf("Duplicates = %d, want 1", res.Summary.Duplicates)
	}
}

func TestBuildPreviewDuplicatePhoneOnly(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Phone: ptrStr("+33612345678")},
		},
	}
	lk := PreviewLookups{ByPhone: map[string]int{"+33612345678": 7}}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusDuplicate || row.MatchedBy != MatchedByPhone || row.MatchedCustomerID != 7 {
		t.Fatalf("row = %+v, want duplicate/phone/7", row)
	}
}

// Email et téléphone désignent le MÊME client existant : un seul
// rapprochement, matched_by = both.
func TestBuildPreviewDuplicateBothSameCustomer(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("jean@example.com"), Phone: ptrStr("+33612345678")},
		},
	}
	lk := PreviewLookups{
		ByEmail: map[string]int{"jean@example.com": 42},
		ByPhone: map[string]int{"+33612345678": 42},
	}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusDuplicate {
		t.Fatalf("Status = %q, want %q", row.Status, StatusDuplicate)
	}
	if row.MatchedBy != MatchedByBoth {
		t.Fatalf("MatchedBy = %q, want %q", row.MatchedBy, MatchedByBoth)
	}
	if row.MatchedCustomerID != 42 {
		t.Fatalf("MatchedCustomerID = %d, want 42", row.MatchedCustomerID)
	}
	if res.Summary.Duplicates != 1 || res.Summary.Conflicts != 0 {
		t.Fatalf("Summary = %+v, want Duplicates=1 Conflicts=0", res.Summary)
	}
}

// Email et téléphone désignent DEUX clients différents : conflict, pas
// duplicate, avec les deux IDs portés séparément et un warning dédié.
func TestBuildPreviewConflict(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("jean@example.com"), Phone: ptrStr("+33612345678")},
		},
	}
	lk := PreviewLookups{
		ByEmail: map[string]int{"jean@example.com": 42},
		ByPhone: map[string]int{"+33612345678": 99},
	}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusConflict {
		t.Fatalf("Status = %q, want %q", row.Status, StatusConflict)
	}
	if row.EmailCustomerID != 42 || row.PhoneCustomerID != 99 {
		t.Fatalf("row = %+v, want EmailCustomerID=42 PhoneCustomerID=99", row)
	}
	if row.Resolution != ResolutionSkip {
		t.Fatalf("Resolution = %q, want %q", row.Resolution, ResolutionSkip)
	}
	if row.MatchedBy != "" || row.MatchedCustomerID != 0 {
		t.Fatalf("row de conflict ne doit pas porter MatchedBy/MatchedCustomerID: %+v", row)
	}
	if res.Summary.Conflicts != 1 || res.Summary.Duplicates != 0 {
		t.Fatalf("Summary = %+v, want Conflicts=1 Duplicates=0", res.Summary)
	}

	found := false
	for _, w := range res.Warnings {
		if w.Code == WarnDuplicateConflict && w.Ref == "Z1" {
			found = true
		}
	}
	if !found {
		t.Fatal("warning duplicate_conflict absent")
	}
}

func TestBuildPreviewAlreadyImported(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{{ExternalID: "Z1"}},
	}
	lk := PreviewLookups{ByMapping: map[string]MappingEntry{
		"Z1": {CustomerID: 10, TargetExists: true},
	}}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusAlreadyImported {
		t.Fatalf("Status = %q, want %q", row.Status, StatusAlreadyImported)
	}
	if row.MatchedCustomerID != 10 {
		t.Fatalf("MatchedCustomerID = %d, want 10", row.MatchedCustomerID)
	}
	if row.Resolution != ResolutionSkip {
		t.Fatalf("Resolution = %q, want %q (defaut already_imported)", row.Resolution, ResolutionSkip)
	}
	if res.Summary.AlreadyImported != 1 {
		t.Fatalf("AlreadyImported = %d, want 1", res.Summary.AlreadyImported)
	}
}

func TestBuildPreviewMappingStale(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{{ExternalID: "Z1"}},
	}
	lk := PreviewLookups{ByMapping: map[string]MappingEntry{
		"Z1": {CustomerID: 10, TargetExists: false},
	}}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusMappingStale {
		t.Fatalf("Status = %q, want %q", row.Status, StatusMappingStale)
	}
	if row.Resolution != ResolutionRecreate {
		t.Fatalf("Resolution = %q, want %q (defaut mapping_stale)", row.Resolution, ResolutionRecreate)
	}
	if res.Summary.MappingStale != 1 {
		t.Fatalf("MappingStale = %d, want 1", res.Summary.MappingStale)
	}
}

// Une ligne sans email ni téléphone ne peut jamais être dédupliquée : elle
// est toujours "create", même si (par construction impossible ici) des
// lookups existaient.
func TestBuildPreviewNoContactAlwaysCreate(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{{ExternalID: "Z1", Name: "Sans Contact"}},
	}

	res := BuildPreview(imp, PreviewLookups{
		ByEmail: map[string]int{"jean@example.com": 1},
		ByPhone: map[string]int{"+33612345678": 2},
	})

	row := customerRow(t, res, "Z1")
	if row.Status != StatusCreate {
		t.Fatalf("Status = %q, want %q", row.Status, StatusCreate)
	}
}

// Le mapping l'emporte sur la dédup : même si email/téléphone matchent un
// autre client, un external_id déjà mappé n'est jamais soumis à la dédup.
func TestBuildPreviewMappingTakesPriorityOverDedup(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("jean@example.com")},
		},
	}
	lk := PreviewLookups{
		ByEmail:   map[string]int{"jean@example.com": 999},
		ByMapping: map[string]MappingEntry{"Z1": {CustomerID: 10, TargetExists: true}},
	}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusAlreadyImported {
		t.Fatalf("Status = %q, want %q (le mapping doit primer)", row.Status, StatusAlreadyImported)
	}
	if row.MatchedCustomerID != 10 {
		t.Fatalf("MatchedCustomerID = %d, want 10 (celui du mapping, pas 999 du dedup)", row.MatchedCustomerID)
	}
}

func TestBuildPreviewSummaryCounters(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "create-1"},
			{ExternalID: "create-2"},
			{ExternalID: "dup-1", Email: ptrStr("a@example.com")},
			{ExternalID: "conflict-1", Email: ptrStr("b@example.com"), Phone: ptrStr("+33600000001")},
			{ExternalID: "already-1"},
			{ExternalID: "stale-1"},
		},
	}
	lk := PreviewLookups{
		ByEmail: map[string]int{"a@example.com": 1, "b@example.com": 2},
		ByPhone: map[string]int{"+33600000001": 3},
		ByMapping: map[string]MappingEntry{
			"already-1": {CustomerID: 5, TargetExists: true},
			"stale-1":   {CustomerID: 6, TargetExists: false},
		},
	}

	res := BuildPreview(imp, lk)

	want := PreviewSummary{Total: 6, ToCreate: 2, Duplicates: 1, Conflicts: 1, AlreadyImported: 1, MappingStale: 1}
	if res.Summary != want {
		t.Fatalf("Summary = %+v, want %+v", res.Summary, want)
	}
}

// Warning du parser (ex: missing_contact) et warning de la preview
// (duplicate_conflict) doivent tous les deux figurer dans Warnings.
func TestBuildPreviewMergesParserAndPreviewWarnings(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("a@example.com"), Phone: ptrStr("+33600000001")},
		},
		Warnings: []Warning{{Code: WarnMissingName, Ref: "Z1", Message: "ni nom ni prenom"}},
	}
	lk := PreviewLookups{
		ByEmail: map[string]int{"a@example.com": 1},
		ByPhone: map[string]int{"+33600000001": 2},
	}

	res := BuildPreview(imp, lk)

	hasParserWarning := false
	hasConflictWarning := false
	for _, w := range res.Warnings {
		if w.Code == WarnMissingName {
			hasParserWarning = true
		}
		if w.Code == WarnDuplicateConflict {
			hasConflictWarning = true
		}
	}
	if !hasParserWarning {
		t.Fatal("warning du parser absent de PreviewResult.Warnings")
	}
	if !hasConflictWarning {
		t.Fatal("warning de la preview absent de PreviewResult.Warnings")
	}
}

// Email comparé en minuscule : une casse différente entre le fichier et la
// base doit quand même matcher.
func TestBuildPreviewEmailMatchIsCaseInsensitive(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Email: ptrStr("Jean.DUPONT@Example.COM")},
		},
	}
	lk := PreviewLookups{ByEmail: map[string]int{"jean.dupont@example.com": 42}}

	res := BuildPreview(imp, lk)

	row := customerRow(t, res, "Z1")
	if row.Status != StatusDuplicate || row.MatchedCustomerID != 42 {
		t.Fatalf("row = %+v, want duplicate/42 malgre la casse", row)
	}
}

// Deux lignes "create" qui partagent le même téléphone déclenchent le
// warning informatif, non bloquant.
func TestBuildPreviewIntraFileSharedPhoneWarning(t *testing.T) {
	imp := &IntermediateCustomerImport{
		Customers: []CanonicalCustomer{
			{ExternalID: "Z1", Phone: ptrStr("+33612345678")},
			{ExternalID: "Z2", Phone: ptrStr("+33612345678")},
			{ExternalID: "Z3", Phone: ptrStr("+33698765432")},
		},
	}

	res := BuildPreview(imp, PreviewLookups{})

	if res.Summary.ToCreate != 3 {
		t.Fatalf("ToCreate = %d, want 3 (le warning ne bloque rien)", res.Summary.ToCreate)
	}

	sharedWarnings := 0
	for _, w := range res.Warnings {
		if w.Code == WarnIntraFileSharedPhone {
			sharedWarnings++
			if w.Ref != "Z1" && w.Ref != "Z2" {
				t.Fatalf("warning intra_file_shared_phone inattendu sur %q", w.Ref)
			}
		}
	}
	if sharedWarnings != 2 {
		t.Fatalf("warnings intra_file_shared_phone = %d, want 2 (Z1 et Z2)", sharedWarnings)
	}
}

func TestBuildPreviewEmptyImport(t *testing.T) {
	res := BuildPreview(&IntermediateCustomerImport{}, PreviewLookups{})
	if res.Summary.Total != 0 {
		t.Fatalf("Total = %d, want 0", res.Summary.Total)
	}
	if len(res.Rows) != 0 {
		t.Fatalf("Rows = %v, want vide", res.Rows)
	}
}
