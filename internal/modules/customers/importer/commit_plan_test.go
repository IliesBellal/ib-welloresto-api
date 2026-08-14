package importer

import "testing"

const testCommitMerchantID = "merchant-commit-1"

func newCommitSnapshot(customers []CanonicalCustomer, rows []PreviewRow) *PreviewSnapshot {
	return &PreviewSnapshot{MerchantID: testCommitMerchantID, Provider: ZeltySlug, Customers: customers, Rows: rows}
}

func findAction(t *testing.T, actions []CommitAction, externalID string) CommitAction {
	t.Helper()
	for _, a := range actions {
		if a.ExternalID == externalID {
			return a
		}
	}
	t.Fatalf("action %q absente", externalID)
	return CommitAction{}
}

func findBlocker(blockers []CommitBlocker, code, ref string) bool {
	for _, b := range blockers {
		if b.Code == code && b.Ref == ref {
			return true
		}
	}
	return false
}

// --- create ---

func TestBuildCommitPlanCreate(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)

	plan, blockers := BuildCommitPlan(snap, CommitDecisions{}, PreviewLookups{})
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	if len(plan.Creates) != 1 || plan.Creates[0].ExternalID != "Z1" {
		t.Fatalf("Creates = %+v, want 1 action Z1", plan.Creates)
	}
	if plan.Creates[0].TargetCustomerID != nil {
		t.Fatal("TargetCustomerID doit etre nil pour un create")
	}
	if plan.Creates[0].Customer.MerchantID != testCommitMerchantID {
		t.Fatalf("MerchantID = %q, want %q", plan.Creates[0].Customer.MerchantID, testCommitMerchantID)
	}
}

// --- import_anyway ---

func TestBuildCommitPlanImportAnyway(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusDuplicate, MatchedBy: MatchedByEmail, MatchedCustomerID: 42, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionImportAnyway}}}
	fresh := PreviewLookups{ByEmail: map[string]int{"jean@example.com": 42}}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun (import_anyway cree malgre le doublon)", blockers)
	}
	if len(plan.Creates) != 1 {
		t.Fatalf("Creates = %+v, want 1", plan.Creates)
	}
}

// --- skip ---

func TestBuildCommitPlanSkip(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionSkip}}}

	plan, blockers := BuildCommitPlan(snap, dec, PreviewLookups{})
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun (skip est toujours valide)", blockers)
	}
	if plan.SkippedCount != 1 {
		t.Fatalf("SkippedCount = %d, want 1", plan.SkippedCount)
	}
	if len(plan.Creates) != 0 {
		t.Fatalf("Creates = %+v, want aucun", plan.Creates)
	}
}

// --- update ---

func TestBuildCommitPlanUpdate(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusDuplicate, MatchedBy: MatchedByEmail, MatchedCustomerID: 42, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdate}}}
	fresh := PreviewLookups{ByEmail: map[string]int{"jean@example.com": 42}}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	action := findAction(t, plan.Updates, "Z1")
	if action.TargetCustomerID == nil || *action.TargetCustomerID != 42 {
		t.Fatalf("TargetCustomerID = %v, want 42", action.TargetCustomerID)
	}
}

// --- update_to_email / update_to_phone (conflit) ---

func TestBuildCommitPlanUpdateToEmail(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com"), Phone: ptrStr("+33612345678")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusConflict, EmailCustomerID: 42, PhoneCustomerID: 99, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdateToEmail}}}
	fresh := PreviewLookups{
		ByEmail: map[string]int{"jean@example.com": 42},
		ByPhone: map[string]int{"+33612345678": 99},
	}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	action := findAction(t, plan.Updates, "Z1")
	if action.TargetCustomerID == nil || *action.TargetCustomerID != 42 {
		t.Fatalf("TargetCustomerID = %v, want 42 (email)", action.TargetCustomerID)
	}
}

func TestBuildCommitPlanUpdateToPhone(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com"), Phone: ptrStr("+33612345678")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusConflict, EmailCustomerID: 42, PhoneCustomerID: 99, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdateToPhone}}}
	fresh := PreviewLookups{
		ByEmail: map[string]int{"jean@example.com": 42},
		ByPhone: map[string]int{"+33612345678": 99},
	}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	action := findAction(t, plan.Updates, "Z1")
	if action.TargetCustomerID == nil || *action.TargetCustomerID != 99 {
		t.Fatalf("TargetCustomerID = %v, want 99 (telephone)", action.TargetCustomerID)
	}
}

// --- recreate ---

func TestBuildCommitPlanRecreateMappingStale(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusMappingStale, MatchedCustomerID: 7, Resolution: ResolutionRecreate}},
	)
	fresh := PreviewLookups{ByMapping: map[string]MappingEntry{"Z1": {CustomerID: 7, TargetExists: false}}}

	plan, blockers := BuildCommitPlan(snap, CommitDecisions{}, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	if len(plan.Recreates) != 1 || plan.Recreates[0].ExternalID != "Z1" {
		t.Fatalf("Recreates = %+v, want 1 action Z1", plan.Recreates)
	}
}

func TestBuildCommitPlanRecreateAlreadyImported(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusAlreadyImported, MatchedCustomerID: 7, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionRecreate}}}
	fresh := PreviewLookups{ByMapping: map[string]MappingEntry{"Z1": {CustomerID: 7, TargetExists: true}}}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun (recreate valide pour already_imported aussi)", blockers)
	}
	if len(plan.Recreates) != 1 {
		t.Fatalf("Recreates = %+v, want 1", plan.Recreates)
	}
}

// already_imported laisse en "skip" (defaut du snapshot, aucune decision) doit rester un skip.
func TestBuildCommitPlanAlreadyImportedDefaultSkip(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusAlreadyImported, MatchedCustomerID: 7, Resolution: ResolutionSkip}},
	)
	fresh := PreviewLookups{ByMapping: map[string]MappingEntry{"Z1": {CustomerID: 7, TargetExists: true}}}

	plan, blockers := BuildCommitPlan(snap, CommitDecisions{}, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	if plan.SkippedCount != 1 {
		t.Fatalf("SkippedCount = %d, want 1", plan.SkippedCount)
	}
}

// --- blockers ---

func TestBuildCommitPlanUnknownDecisionBlocker(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z-inconnu", Resolution: ResolutionSkip}}}

	plan, blockers := BuildCommitPlan(snap, dec, PreviewLookups{})
	if plan != nil {
		t.Fatal("plan doit etre nil quand il y a un blocker")
	}
	if !findBlocker(blockers, BlockerUnknownDecision, "Z-inconnu") {
		t.Fatalf("blockers = %+v, want unknown_decision sur Z-inconnu", blockers)
	}
}

// Une ligne "create" en preview, dont un doublon email est apparu depuis (un
// autre import concurrent, par exemple) : le commit ne fait pas confiance a
// la preview et bloque, sauf si l'utilisateur a choisi import_anyway.
func TestBuildCommitPlanNewDuplicateDetectedBlocker(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)
	fresh := PreviewLookups{ByEmail: map[string]int{"jean@example.com": 42}}

	plan, blockers := BuildCommitPlan(snap, CommitDecisions{}, fresh)
	if plan != nil {
		t.Fatal("plan doit etre nil")
	}
	if !findBlocker(blockers, BlockerNewDuplicateDetected, "Z1") {
		t.Fatalf("blockers = %+v, want new_duplicate_detected sur Z1", blockers)
	}
}

func TestBuildCommitPlanInvalidUpdateTargetBlocker(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusDuplicate, MatchedBy: MatchedByEmail, MatchedCustomerID: 42, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdate}}}
	// Le client cible a disparu depuis la preview : fresh ne le trouve plus.
	fresh := PreviewLookups{}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if plan != nil {
		t.Fatal("plan doit etre nil")
	}
	if !findBlocker(blockers, BlockerInvalidUpdateTarget, "Z1") {
		t.Fatalf("blockers = %+v, want invalid_update_target sur Z1", blockers)
	}
}

func TestBuildCommitPlanInvalidDecisionBlockerUnknownResolution(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: "resolution-inconnue"}}}

	plan, blockers := BuildCommitPlan(snap, dec, PreviewLookups{})
	if plan != nil {
		t.Fatal("plan doit etre nil")
	}
	if !findBlocker(blockers, BlockerInvalidDecision, "Z1") {
		t.Fatalf("blockers = %+v, want invalid_decision sur Z1", blockers)
	}
}

// "recreate" sur une ligne jamais mappee (create pur) est incoherent.
func TestBuildCommitPlanInvalidDecisionRecreateWithoutMapping(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionRecreate}}}

	plan, blockers := BuildCommitPlan(snap, dec, PreviewLookups{})
	if plan != nil {
		t.Fatal("plan doit etre nil")
	}
	if !findBlocker(blockers, BlockerInvalidDecision, "Z1") {
		t.Fatalf("blockers = %+v, want invalid_decision sur Z1", blockers)
	}
}

// "update" sur une ligne deja mappee (already_imported) est incoherent :
// c'est "skip" ou "recreate" qui s'appliquent, pas une mise a jour directe.
func TestBuildCommitPlanInvalidDecisionUpdateOnMappedRow(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1"}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusAlreadyImported, MatchedCustomerID: 7, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdate}}}
	fresh := PreviewLookups{ByMapping: map[string]MappingEntry{"Z1": {CustomerID: 7, TargetExists: true}}}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if plan != nil {
		t.Fatal("plan doit etre nil")
	}
	if !findBlocker(blockers, BlockerInvalidDecision, "Z1") {
		t.Fatalf("blockers = %+v, want invalid_decision sur Z1", blockers)
	}
}

// --- revalidation contre des lookups frais differents de la preview ---

// La preview avait matche Z1 au client 42 par email ; entre-temps ce client a
// ete supprime (fresh ne le trouve plus), mais un AUTRE client a repris ce
// meme email entre-temps sous un nouvel id (99). Le commit doit utiliser 99,
// pas le 42 fige dans le snapshot.
func TestBuildCommitPlanRevalidatesAgainstFreshNotPreviewMatch(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{{ExternalID: "Z1", Email: ptrStr("jean@example.com")}},
		[]PreviewRow{{ExternalID: "Z1", Status: StatusDuplicate, MatchedBy: MatchedByEmail, MatchedCustomerID: 42, Resolution: ResolutionSkip}},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z1", Resolution: ResolutionUpdate}}}
	fresh := PreviewLookups{ByEmail: map[string]int{"jean@example.com": 99}}

	plan, blockers := BuildCommitPlan(snap, dec, fresh)
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	action := findAction(t, plan.Updates, "Z1")
	if action.TargetCustomerID == nil || *action.TargetCustomerID != 99 {
		t.Fatalf("TargetCustomerID = %v, want 99 (frais), pas 42 (preview)", action.TargetCustomerID)
	}
}

// --- fusion decisions + defauts ---

func TestBuildCommitPlanFusionKeepsDefaultWhenNoDecision(t *testing.T) {
	snap := newCommitSnapshot(
		[]CanonicalCustomer{
			{ExternalID: "Z1"}, // pas de decision -> garde le defaut "create"
			{ExternalID: "Z2"}, // decision explicite -> "skip"
		},
		[]PreviewRow{
			{ExternalID: "Z1", Status: StatusCreate, Resolution: ResolutionCreate},
			{ExternalID: "Z2", Status: StatusCreate, Resolution: ResolutionCreate},
		},
	)
	dec := CommitDecisions{Decisions: []CommitRowDecision{{ExternalID: "Z2", Resolution: ResolutionSkip}}}

	plan, blockers := BuildCommitPlan(snap, dec, PreviewLookups{})
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	if len(plan.Creates) != 1 || plan.Creates[0].ExternalID != "Z1" {
		t.Fatalf("Creates = %+v, want seulement Z1 (defaut conserve)", plan.Creates)
	}
	if plan.SkippedCount != 1 {
		t.Fatalf("SkippedCount = %d, want 1 (Z2 override)", plan.SkippedCount)
	}
}

// --- C1 : advertising_consent toujours explicite ---

func TestBuildCommitCustomerAdvertisingConsentAlwaysExplicit(t *testing.T) {
	trueVal := true
	cases := []struct {
		name  string
		input *bool
		want  bool
	}{
		{"consentement true", &trueVal, true},
		{"consentement nil (parser en faute)", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			customer := buildCommitCustomer("m1", CanonicalCustomer{ExternalID: "Z1", AdvertisingConsent: tc.input})
			if customer.AdvertisingConsent == nil {
				t.Fatal("AdvertisingConsent = nil, want toujours une valeur explicite")
			}
			if *customer.AdvertisingConsent != tc.want {
				t.Fatalf("AdvertisingConsent = %v, want %v", *customer.AdvertisingConsent, tc.want)
			}
		})
	}
}

// --- C3 : les champs absents du canonique restent nil (mise a jour partielle) ---

func TestBuildCommitCustomerOmitsAbsentFields(t *testing.T) {
	customer := buildCommitCustomer("m1", CanonicalCustomer{
		ExternalID: "Z1",
		Name:       "Jean Dupont",
		// Email, Phone, BusinessName, Birthdate, AdditionalInfo, DeliveryNotes,
		// CreationDate, Address : tous absents.
	})

	if customer.CustomerEmail != nil {
		t.Fatalf("CustomerEmail = %v, want nil (absent du fichier)", customer.CustomerEmail)
	}
	if customer.CustomerTel != nil {
		t.Fatalf("CustomerTel = %v, want nil", customer.CustomerTel)
	}
	if customer.CustomerBusinessName != nil {
		t.Fatalf("CustomerBusinessName = %v, want nil", customer.CustomerBusinessName)
	}
	if customer.CustomerBirthdate != nil {
		t.Fatalf("CustomerBirthdate = %v, want nil", customer.CustomerBirthdate)
	}
	if customer.CustomerAddress != nil {
		t.Fatalf("CustomerAddress = %v, want nil", customer.CustomerAddress)
	}
	if customer.CreationDate != nil {
		t.Fatalf("CreationDate = %v, want nil", customer.CreationDate)
	}
	if customer.CustomerName == nil || *customer.CustomerName != "Jean Dupont" {
		t.Fatalf("CustomerName = %v, want Jean Dupont (fourni)", customer.CustomerName)
	}
}
