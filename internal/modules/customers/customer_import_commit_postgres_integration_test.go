//go:build postgres_integration

package customers

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	authpkg "welloresto-api/internal/modules/auth"
	"welloresto-api/internal/modules/customers/importer"
)

// Vérification réelle du commit d'import clients contre Postgres : c'est la
// seule étape qui écrit, et son contrat — tout ou rien, idempotent,
// préservation de creation_date, mise à jour partielle — ne se prouve pas
// avec un mock SQL.
//
//	DB_DIALECT=postgres POSTGRES_URL=postgres://…:5433/welloresto_dev \
//	  go test -tags postgres_integration ./internal/modules/customers/...
//
// Nécessite la migration 084 (import_customers_mapping). Chaque cas travaille
// sur son propre marchand et nettoie derrière lui.

func boolPtrI(b bool) *bool { return &b }

func itestCommitMerchant(t *testing.T, db *sql.DB, suffix string) string {
	t.Helper()
	merchantID := "itest-cust-import-" + suffix
	itestCommitCleanup(t, db, merchantID)
	t.Cleanup(func() { itestCommitCleanup(t, db, merchantID) })
	return merchantID
}

func itestCommitCleanup(t *testing.T, db *sql.DB, merchantID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, `DELETE FROM import_customers_mapping WHERE merchant_id = $1`, merchantID)
	_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
}

func itestCommitService(db *sql.DB, store customerImportPreviewStore) *CustomerImportService {
	repo := NewCustomerRepository(db)
	return NewCustomerImportService(repo, repo, db, importer.DefaultRegistry(), store)
}

func itestCommitContext(merchantID string) context.Context {
	return middleware.WithUser(context.Background(), &authpkg.UserLoginRow{
		UserID:     "u-itest-cust-import",
		MerchantID: merchantID,
	})
}

// seedItestCustomer insère un client directement (hors du chemin d'import),
// pour représenter l'état "existant" contre lequel le commit doit se
// revalider.
func seedItestCustomer(t *testing.T, db *sql.DB, merchantID, name, email, phone string) int {
	t.Helper()
	ctx := context.Background()

	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO customer (merchant_id, customer_brand, customer_name, customer_email, customer_tel, enabled)
		VALUES ($1, 'WELLO_RESTO', $2, $3, $4, true)
		RETURNING customer_id`, merchantID, name, email, phone).Scan(&id)
	if err != nil {
		t.Fatalf("seed customer %s: %v", name, err)
	}
	return int(id)
}

func storeSnapshotForItest(t *testing.T, store *fakeCustomerImportStore, merchantID, token string, snapshot *importer.PreviewSnapshot) {
	t.Helper()
	payload, err := snapshot.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	store.values["customer:import:preview:"+merchantID+":"+token] = payload
}

func readCustomer(t *testing.T, db *sql.DB, customerID int) (name, email, phone string, consent bool, creationDate time.Time) {
	t.Helper()
	var nName, nEmail, nPhone sql.NullString
	var nConsent sql.NullBool
	var nCreated sql.NullTime
	err := db.QueryRowContext(context.Background(), `
		SELECT customer_name, customer_email, customer_tel, advertising_consent, creation_date
		FROM customer WHERE customer_id = $1`, customerID).
		Scan(&nName, &nEmail, &nPhone, &nConsent, &nCreated)
	if err != nil {
		t.Fatalf("readCustomer(%d): %v", customerID, err)
	}
	return nName.String, nEmail.String, nPhone.String, nConsent.Bool, nCreated.Time
}

func countRows(t *testing.T, db *sql.DB, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("countRows(%q): %v", query, err)
	}
	return n
}

func mappedWelloID(t *testing.T, db *sql.DB, merchantID, provider, externalID string) int {
	t.Helper()
	var id int
	err := db.QueryRowContext(context.Background(), `
		SELECT wello_id FROM import_customers_mapping
		WHERE merchant_id = $1 AND provider = $2 AND external_id = $3`, merchantID, provider, externalID).Scan(&id)
	if err != nil {
		t.Fatalf("mappedWelloID(%s): %v", externalID, err)
	}
	return id
}

// --- Scénario end-to-end : create + update + skip + recreate ---

func TestCommitImport_Postgres_EndToEnd(t *testing.T) {
	db := pgtest.Open(t)
	merchantID := itestCommitMerchant(t, db, "e2e")

	updateTargetID := seedItestCustomer(t, db, merchantID, "Ancien Nom", "update-target@example.com", "+33600000001")
	skipTargetID := seedItestCustomer(t, db, merchantID, "Ne Pas Toucher", "skip-target@example.com", "+33600000002")
	staleTargetID := seedItestCustomer(t, db, merchantID, "Va Disparaitre", "recreate@example.com", "+33600000003")

	// import_customers_mapping pointe vers staleTargetID, qu'on supprime
	// ensuite : le mapping devient périmé (mapping_stale).
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO import_customers_mapping (merchant_id, provider, external_id, wello_id)
		VALUES ($1, 'zelty', 'Z-recreate', $2)`, merchantID, staleTargetID); err != nil {
		t.Fatalf("seed mapping perime: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `DELETE FROM customer WHERE customer_id = $1`, staleTargetID); err != nil {
		t.Fatalf("suppression de la cible perimee: %v", err)
	}

	snapshot := &importer.PreviewSnapshot{
		MerchantID: merchantID,
		Provider:   "zelty",
		Customers: []importer.CanonicalCustomer{
			{ExternalID: "Z-create", Name: "Nouveau Client", Email: strPtr("nouveau@example.com"), AdvertisingConsent: boolPtrI(true)},
			{ExternalID: "Z-update", Name: "Jean Modifie", Email: strPtr("update-target@example.com"), AdvertisingConsent: boolPtrI(true)},
			{ExternalID: "Z-skip", Name: "Ignore Moi", Email: strPtr("skip-target@example.com"), AdvertisingConsent: boolPtrI(false)},
			{ExternalID: "Z-recreate", Name: "Je Reviens", Email: strPtr("recreate@example.com"), AdvertisingConsent: boolPtrI(false)},
		},
		Rows: []importer.PreviewRow{
			{ExternalID: "Z-create", Status: importer.StatusCreate, Resolution: importer.ResolutionCreate},
			{ExternalID: "Z-update", Status: importer.StatusDuplicate, MatchedBy: importer.MatchedByEmail, MatchedCustomerID: updateTargetID, Resolution: importer.ResolutionSkip},
			{ExternalID: "Z-skip", Status: importer.StatusDuplicate, MatchedBy: importer.MatchedByEmail, MatchedCustomerID: skipTargetID, Resolution: importer.ResolutionSkip},
			{ExternalID: "Z-recreate", Status: importer.StatusMappingStale, MatchedCustomerID: staleTargetID, Resolution: importer.ResolutionRecreate},
		},
	}

	store := newFakeCustomerImportStore()
	storeSnapshotForItest(t, store, merchantID, "tok-e2e", snapshot)
	service := itestCommitService(db, store)

	summary, blockers, err := service.CommitImport(itestCommitContext(merchantID), CommitRequest{
		Token:     "tok-e2e",
		Decisions: []importer.CommitRowDecision{{ExternalID: "Z-update", Resolution: importer.ResolutionUpdate}},
	})
	if err != nil {
		t.Fatalf("CommitImport: %v", err)
	}
	if len(blockers) != 0 {
		t.Fatalf("blockers = %+v, want aucun", blockers)
	}
	if summary.Created != 1 || summary.Updated != 1 || summary.Recreated != 1 || summary.Skipped != 1 {
		t.Fatalf("summary = %+v, want {1,1,1,1}", *summary)
	}

	// Le client "update" a bien été modifié.
	name, email, _, consent, _ := readCustomer(t, db, updateTargetID)
	if name != "Jean Modifie" || email != "update-target@example.com" || !consent {
		t.Fatalf("update-target apres commit: name=%q email=%q consent=%v", name, email, consent)
	}

	// Le client "skip" n'a PAS été touché.
	name, _, _, _, _ = readCustomer(t, db, skipTargetID)
	if name != "Ne Pas Toucher" {
		t.Fatalf("skip-target modifie alors qu'il devait etre ignore: name=%q", name)
	}

	// Le mapping recreate pointe vers un NOUVEL id, distinct de l'ancien
	// (supprimé).
	newRecreateID := mappedWelloID(t, db, merchantID, "zelty", "Z-recreate")
	if newRecreateID == staleTargetID {
		t.Fatal("le mapping recreate pointe toujours vers l'ancien id supprime")
	}

	// Mapping écrit pour create/update/recreate, mais PAS pour skip.
	if got := countRows(t, db, `SELECT COUNT(*) FROM import_customers_mapping WHERE merchant_id = $1`, merchantID); got != 3 {
		t.Fatalf("import_customers_mapping = %d lignes, want 3 (create+update+recreate, pas skip)", got)
	}

	// --- Idempotence : rejouer le même lot ---
	snapshot2 := &importer.PreviewSnapshot{
		MerchantID: merchantID,
		Provider:   "zelty",
		Customers:  snapshot.Customers,
		Rows: []importer.PreviewRow{
			{ExternalID: "Z-create", Status: importer.StatusAlreadyImported, Resolution: importer.ResolutionSkip},
			{ExternalID: "Z-update", Status: importer.StatusAlreadyImported, Resolution: importer.ResolutionSkip},
			{ExternalID: "Z-skip", Status: importer.StatusDuplicate, MatchedBy: importer.MatchedByEmail, Resolution: importer.ResolutionSkip},
			{ExternalID: "Z-recreate", Status: importer.StatusAlreadyImported, Resolution: importer.ResolutionSkip},
		},
	}
	storeSnapshotForItest(t, store, merchantID, "tok-replay", snapshot2)

	customersBefore := countRows(t, db, `SELECT COUNT(*) FROM customer WHERE merchant_id = $1`, merchantID)
	mappingsBefore := countRows(t, db, `SELECT COUNT(*) FROM import_customers_mapping WHERE merchant_id = $1`, merchantID)

	summary2, blockers2, err := service.CommitImport(itestCommitContext(merchantID), CommitRequest{Token: "tok-replay"})
	if err != nil {
		t.Fatalf("CommitImport (replay): %v", err)
	}
	if len(blockers2) != 0 {
		t.Fatalf("blockers (replay) = %+v, want aucun", blockers2)
	}
	if summary2.Created != 0 || summary2.Updated != 0 || summary2.Recreated != 0 || summary2.Skipped != 4 {
		t.Fatalf("summary (replay) = %+v, want tout en skip", *summary2)
	}

	customersAfter := countRows(t, db, `SELECT COUNT(*) FROM customer WHERE merchant_id = $1`, merchantID)
	mappingsAfter := countRows(t, db, `SELECT COUNT(*) FROM import_customers_mapping WHERE merchant_id = $1`, merchantID)
	if customersAfter != customersBefore {
		t.Fatalf("customer: %d avant/%d apres le rejeu, want inchange (aucune ligne dupliquee)", customersBefore, customersAfter)
	}
	if mappingsAfter != mappingsBefore {
		t.Fatalf("import_customers_mapping: %d avant/%d apres le rejeu, want inchange", mappingsBefore, mappingsAfter)
	}
}

// --- ROLLBACK total : une erreur au milieu du lot n'écrit rien ---

func TestCommitImport_Postgres_RollbackIsAllOrNothing(t *testing.T) {
	db := pgtest.Open(t)
	merchantID := itestCommitMerchant(t, db, "rollback")

	service := itestCommitService(db, newFakeCustomerImportStore())

	// Construits directement (pas via BuildCommitPlan) : le test a besoin
	// d'injecter une valeur invalide (customer_birthdate, colonne `date`) sur
	// la 2e ligne, ce qu'aucune donnée canonique valide ne produit — c'est le
	// moyen le plus direct de forcer l'échec SQL d'une ligne au milieu du lot.
	plan := &importer.CommitPlan{
		Creates: []importer.CommitAction{
			{ExternalID: "R1", Customer: models.Customer{
				MerchantID: merchantID, CustomerBrand: strPtr(models.BrandWelloResto), CustomerName: strPtr("Client Un"),
			}},
			{ExternalID: "R2", Customer: models.Customer{
				MerchantID: merchantID, CustomerBrand: strPtr(models.BrandWelloResto), CustomerName: strPtr("Client Deux"),
				CustomerBirthdate: strPtr("pas-une-date"),
			}},
			{ExternalID: "R3", Customer: models.Customer{
				MerchantID: merchantID, CustomerBrand: strPtr(models.BrandWelloResto), CustomerName: strPtr("Client Trois"),
			}},
		},
	}

	// materializeImportTx est non-exportee mais ce fichier de test partage le
	// paquet customers (pas de suffixe _test sur le nom du paquet).
	_, err := service.materializeImportTx(itestCommitContext(merchantID), merchantID, "zelty", plan)
	if err == nil {
		t.Fatal("materializeImportTx = nil, want une erreur (ligne R2 invalide)")
	}

	if got := countRows(t, db, `SELECT COUNT(*) FROM customer WHERE merchant_id = $1`, merchantID); got != 0 {
		t.Fatalf("customer = %d lignes apres rollback, want 0 (tout ou rien)", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM import_customers_mapping WHERE merchant_id = $1`, merchantID); got != 0 {
		t.Fatalf("import_customers_mapping = %d lignes apres rollback, want 0", got)
	}
}

// --- C2 : creation_date Zelty préservée, pas now() ---

func TestCommitImport_Postgres_PreservesCreationDate(t *testing.T) {
	db := pgtest.Open(t)
	merchantID := itestCommitMerchant(t, db, "credate")

	registeredAt := time.Date(2019, 6, 15, 10, 30, 0, 0, time.UTC)
	snapshot := &importer.PreviewSnapshot{
		MerchantID: merchantID,
		Provider:   "zelty",
		Customers: []importer.CanonicalCustomer{
			{ExternalID: "CD1", Name: "Client Ancien", Email: strPtr("ancien@example.com"), CreationDate: &registeredAt},
		},
		Rows: []importer.PreviewRow{
			{ExternalID: "CD1", Status: importer.StatusCreate, Resolution: importer.ResolutionCreate},
		},
	}

	store := newFakeCustomerImportStore()
	storeSnapshotForItest(t, store, merchantID, "tok-credate", snapshot)
	service := itestCommitService(db, store)

	summary, blockers, err := service.CommitImport(itestCommitContext(merchantID), CommitRequest{Token: "tok-credate"})
	if err != nil || len(blockers) != 0 {
		t.Fatalf("CommitImport: summary=%+v blockers=%+v err=%v", summary, blockers, err)
	}

	newID := mappedWelloID(t, db, merchantID, "zelty", "CD1")
	_, _, _, _, creationDate := readCustomer(t, db, newID)

	if creationDate.Format("2006-01-02") != "2019-06-15" {
		t.Fatalf("creation_date = %v, want 2019-06-15 (date d'inscription Zelty preservee, pas now())", creationDate)
	}
	if time.Since(creationDate) < 365*24*time.Hour {
		t.Fatalf("creation_date = %v, ressemble a now() plutot qu'a la date preservee", creationDate)
	}
}

// --- C3 : une mise à jour sans email dans le fichier ne vide pas l'email existant ---

func TestCommitImport_Postgres_PartialUpdatePreservesAbsentEmail(t *testing.T) {
	db := pgtest.Open(t)
	merchantID := itestCommitMerchant(t, db, "partial")

	targetID := seedItestCustomer(t, db, merchantID, "Nom Initial", "email-existant@example.com", "+33600000009")

	// Le fichier ne fournit PAS d'email pour cette ligne (cas Zelty tolérant
	// courant) — seul le téléphone permet le rapprochement.
	snapshot := &importer.PreviewSnapshot{
		MerchantID: merchantID,
		Provider:   "zelty",
		Customers: []importer.CanonicalCustomer{
			{ExternalID: "P1", Name: "Nom Mis A Jour", Phone: strPtr("+33600000009")},
		},
		Rows: []importer.PreviewRow{
			{ExternalID: "P1", Status: importer.StatusDuplicate, MatchedBy: importer.MatchedByPhone, MatchedCustomerID: targetID, Resolution: importer.ResolutionSkip},
		},
	}

	store := newFakeCustomerImportStore()
	storeSnapshotForItest(t, store, merchantID, "tok-partial", snapshot)
	service := itestCommitService(db, store)

	summary, blockers, err := service.CommitImport(itestCommitContext(merchantID), CommitRequest{
		Token:     "tok-partial",
		Decisions: []importer.CommitRowDecision{{ExternalID: "P1", Resolution: importer.ResolutionUpdate}},
	})
	if err != nil || len(blockers) != 0 {
		t.Fatalf("CommitImport: summary=%+v blockers=%+v err=%v", summary, blockers, err)
	}
	if summary.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", summary.Updated)
	}

	name, email, phone, _, _ := readCustomer(t, db, targetID)
	if name != "Nom Mis A Jour" {
		t.Fatalf("customer_name = %q, want mis a jour", name)
	}
	if email != "email-existant@example.com" {
		t.Fatalf("customer_email = %q, want preserve (le fichier n'en fournissait pas)", email)
	}
	if phone != "+33600000009" {
		t.Fatalf("customer_tel = %q, want inchange", phone)
	}
}
