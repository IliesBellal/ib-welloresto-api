//go:build postgres_integration

package customers

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
)

// Exige la migration 183_customer_blocking (colonnes customer.blocked_*, motifs
// deletion_reasons object 'customer' et leurs libellés FR).
func TestCustomerBlocking_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	bg := context.Background()

	const merchantA = "999931"
	const merchantB = "999932"

	cleanup := func() {
		_, _ = db.ExecContext(bg, `DELETE FROM customer WHERE merchant_id IN ($1, $2)`, merchantA, merchantB)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewCustomerRepository(db)
	svc := NewCustomersService(repo, nil)
	ctxA := middleware.WithUser(bg, &auth.UserLoginRow{UserID: "itest-block-user", MerchantID: merchantA})
	ctxB := middleware.WithUser(bg, &auth.UserLoginRow{UserID: "itest-block-user-b", MerchantID: merchantB})

	// --- motifs seedés : un id sans commentaire, "Autre" avec commentaire ---
	reasonOK, reasonOther := blockReasonIDs(t, db)

	// Chaque motif de blocage a son libellé FR (c'est ce que sert
	// GET /pos/deletion_reasons/customer, qui fait un INNER JOIN labels).
	var withoutLabel int
	if err := db.QueryRowContext(bg, `
		SELECT COUNT(*) FROM deletion_reasons dr
		WHERE dr.deletion_reason_object = 'customer'
		  AND NOT EXISTS (SELECT 1 FROM labels l
		                  WHERE l.label_type = 'deletion_reason' AND l.lang = 'FR'
		                    AND l.label_value = CAST(dr.deletion_reason_id AS varchar))`).Scan(&withoutLabel); err != nil {
		t.Fatalf("labels query: %v", err)
	}
	if withoutLabel != 0 {
		t.Fatalf("%d motif(s) 'customer' sans libellé FR", withoutLabel)
	}

	// Un motif d'une autre catégorie (annulation de commande) n'est pas un motif de blocage.
	var orderReasonID int
	if err := db.QueryRowContext(bg, `SELECT deletion_reason_id FROM deletion_reasons WHERE deletion_reason_object <> 'customer' LIMIT 1`).Scan(&orderReasonID); err == nil {
		if _, found, _ := repo.FindBlockReason(bg, strconv.Itoa(orderReasonID)); found {
			t.Fatalf("le motif %d (autre catégorie) ne doit pas valider un blocage", orderReasonID)
		}
	}

	// --- validation du motif ---
	if _, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{Phone: "06 11 22 33 44", ReasonID: "999999"}); !errors.Is(err, ErrBlockReasonInvalid) {
		t.Fatalf("motif inconnu: err = %v, want ErrBlockReasonInvalid", err)
	}
	if _, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{Phone: "06 11 22 33 44", ReasonID: ""}); !errors.Is(err, ErrBlockReasonInvalid) {
		t.Fatalf("motif absent: err = %v, want ErrBlockReasonInvalid", err)
	}
	if _, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{Phone: "06 11 22 33 44", ReasonID: reasonOther}); !errors.Is(err, ErrBlockCommentRequired) {
		t.Fatalf("'Autre' sans commentaire: err = %v, want ErrBlockCommentRequired", err)
	}
	if _, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{ReasonID: reasonOK}); !errors.Is(err, ErrBlockIdentityRequired) {
		t.Fatalf("sans identité: err = %v, want ErrBlockIdentityRequired", err)
	}
	// Aucun de ces refus n'a laissé de fiche.
	var n int
	_ = db.QueryRowContext(bg, `SELECT COUNT(*) FROM customer WHERE merchant_id = $1`, merchantA).Scan(&n)
	if n != 0 {
		t.Fatalf("des requêtes refusées ont créé %d fiche(s)", n)
	}

	// --- numéro inconnu : la fiche est créée, puis bloquée ---
	res, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{Phone: "06 12 34 56 78", ReasonID: reasonOK, Comment: " récidiviste "})
	if err != nil {
		t.Fatalf("BlockCustomer (numéro inconnu): %v", err)
	}
	if !res.Created || res.BlockedCount != 1 || res.CustomerID == "" {
		t.Fatalf("résultat inattendu: %+v", res)
	}

	for _, phone := range []string{"06 12 34 56 78", "0612345678", "+33612345678", "+33 6 12 34 56 78"} {
		if blocked, err := svc.IsPhoneBlocked(bg, merchantA, phone); err != nil || !blocked {
			t.Fatalf("IsPhoneBlocked(%q) = (%v, %v), want true", phone, blocked, err)
		}
	}
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantB, "0612345678"); blocked {
		t.Fatal("le blocage de l'établissement A ne doit pas toucher l'établissement B")
	}
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0698765432"); blocked {
		t.Fatal("un autre numéro ne doit pas être bloqué")
	}
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, ""); blocked {
		t.Fatal("un téléphone vide ne doit jamais être bloqué")
	}

	// Bloquer deux fois le même numéro ne crée pas de seconde fiche.
	again, err := svc.BlockCustomer(ctxA, BlockCustomerRequest{Phone: "+33612345678", ReasonID: reasonOK})
	if err != nil || again.Created || again.BlockedCount != 1 {
		t.Fatalf("second blocage: (%+v, %v), want created=false blocked_count=1", again, err)
	}

	// --- doublons : un blocage couvre toutes les fiches de la même identité ---
	dup1 := insertCustomer(t, db, merchantA, "0655555555", "dup@example.fr", true)
	dup2 := insertCustomer(t, db, merchantA, "0655555555", "", true)               // même téléphone
	dup3 := insertCustomer(t, db, merchantA, "0644444444", "DUP@example.fr", true) // même email, autre casse
	other := insertCustomer(t, db, merchantA, "0633333333", "autre@example.fr", true)

	res, err = svc.BlockCustomer(ctxA, BlockCustomerRequest{CustomerID: dup1, ReasonID: reasonOK})
	if err != nil {
		t.Fatalf("BlockCustomer (customer_id): %v", err)
	}
	if res.Created || res.BlockedCount != 3 || res.CustomerID != dup1 {
		t.Fatalf("blocage par customer_id: %+v, want 3 fiches (dup1, dup2, dup3)", res)
	}
	for _, id := range []string{dup1, dup2, dup3} {
		if !isRowBlocked(t, db, id) {
			t.Fatalf("la fiche en doublon %s doit être bloquée", id)
		}
	}
	if isRowBlocked(t, db, other) {
		t.Fatal("une fiche sans téléphone ni email en commun ne doit pas être bloquée")
	}
	// Le téléphone du doublon sans email est bloqué, même si FindCustomerByPhone
	// n'en retient qu'une fiche.
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0655555555"); !blocked {
		t.Fatal("IsPhoneBlocked doit voir les doublons")
	}
	// Le téléphone de la fiche dup3 (rattachée par l'email seul) l'est aussi.
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0644444444"); !blocked {
		t.Fatal("la fiche rattachée par l'email doit bloquer aussi son propre téléphone")
	}

	// --- client "supprimé" : le blocage reste actif et la fiche reste listée ---
	if _, err := db.ExecContext(bg, `UPDATE customer SET enabled = false WHERE customer_id = $1`, dup2); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0655555555"); !blocked {
		t.Fatal("supprimer une fiche ne doit pas lever le blocage")
	}
	listed, _, err := repo.ListCustomers(bg, merchantA, "", "", 1, 50)
	if err != nil {
		t.Fatalf("ListCustomers: %v", err)
	}
	foundDeletedBlocked, foundUnblocked := false, false
	for _, c := range listed {
		if c.CustomerID == dup2 && c.Blocked && c.BlockedReasonID != nil && *c.BlockedReasonID == reasonOK {
			foundDeletedBlocked = true
		}
		if c.CustomerID == other && !c.Blocked {
			foundUnblocked = true
		}
	}
	if !foundDeletedBlocked {
		t.Fatal("ListCustomers doit lister la fiche supprimée mais bloquée, avec son motif")
	}
	if !foundUnblocked {
		t.Fatal("ListCustomers doit exposer blocked=false pour un client normal")
	}

	// --- isolation entre établissements ---
	if _, err := svc.BlockCustomer(ctxB, BlockCustomerRequest{CustomerID: dup1, ReasonID: reasonOK}); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("bloquer la fiche d'un autre établissement: err = %v, want ErrCustomerNotFound", err)
	}
	if err := svc.UnblockCustomer(ctxB, dup1); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("débloquer la fiche d'un autre établissement: err = %v, want ErrCustomerNotFound", err)
	}
	if !isRowBlocked(t, db, dup1) {
		t.Fatal("la fiche de A doit rester bloquée après une tentative de B")
	}

	// --- déblocage : toute l'identité, fiche supprimée comprise ---
	if err := svc.UnblockCustomer(ctxA, dup1); err != nil {
		t.Fatalf("UnblockCustomer: %v", err)
	}
	for _, id := range []string{dup1, dup2, dup3} {
		if isRowBlocked(t, db, id) {
			t.Fatalf("la fiche %s doit être débloquée avec le reste de l'identité", id)
		}
	}
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0655555555"); blocked {
		t.Fatal("le téléphone doit être débloqué")
	}
	// Le premier numéro (autre identité) reste bloqué.
	if blocked, _ := svc.IsPhoneBlocked(bg, merchantA, "0612345678"); !blocked {
		t.Fatal("débloquer une identité ne doit pas toucher les autres")
	}
	var comment, reason, by *string
	_ = db.QueryRowContext(bg, `SELECT blocked_comment, blocked_reason_id, blocked_by FROM customer WHERE customer_id = $1`, dup1).Scan(&comment, &reason, &by)
	if comment != nil || reason != nil || by != nil {
		t.Fatal("le déblocage doit effacer motif, commentaire et auteur")
	}

	if err := svc.UnblockCustomer(ctxA, "999999999"); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("débloquer une fiche inconnue: err = %v, want ErrCustomerNotFound", err)
	}
	if err := svc.UnblockCustomer(ctxA, "pas-un-id"); !errors.Is(err, ErrCustomerNotFound) {
		t.Fatalf("débloquer un id non numérique: err = %v, want ErrCustomerNotFound", err)
	}
}

// blockReasonIDs retourne l'id d'un motif de blocage sans commentaire obligatoire
// et celui du motif qui en exige un ("Autre").
func blockReasonIDs(t *testing.T, db *sql.DB) (ok, other string) {
	t.Helper()
	ctx := context.Background()
	if err := db.QueryRowContext(ctx, `
		SELECT CAST(deletion_reason_id AS varchar) FROM deletion_reasons
		WHERE deletion_reason_object = 'customer' AND enabled AND NOT requires_comment
		ORDER BY deletion_reason_id LIMIT 1`).Scan(&ok); err != nil {
		t.Fatalf("aucun motif de blocage (migration 183 appliquée ?): %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT CAST(deletion_reason_id AS varchar) FROM deletion_reasons
		WHERE deletion_reason_object = 'customer' AND enabled AND requires_comment
		ORDER BY deletion_reason_id LIMIT 1`).Scan(&other); err != nil {
		t.Fatalf("aucun motif de blocage avec commentaire obligatoire: %v", err)
	}
	return ok, other
}

// insertCustomer crée une fiche client brute (téléphone déjà au format stocké
// pour un numéro national FR : on passe par la normalisation de production).
func insertCustomer(t *testing.T, db *sql.DB, merchantID, phone, email string, enabled bool) string {
	t.Helper()
	normalized, err := normalizeBlockPhone(phone)
	if err != nil {
		t.Fatalf("normalizeBlockPhone(%q): %v", phone, err)
	}
	var emailArg interface{}
	if email != "" {
		emailArg = email
	}
	var id int64
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO customer (merchant_id, customer_name, customer_tel, customer_email, enabled)
		VALUES ($1, 'itest-block', $2, $3, $4) RETURNING customer_id`,
		merchantID, normalized, emailArg, enabled).Scan(&id); err != nil {
		t.Fatalf("insertCustomer: %v", err)
	}
	return strconv.FormatInt(id, 10)
}

func isRowBlocked(t *testing.T, db *sql.DB, customerID string) bool {
	t.Helper()
	var blocked bool
	if err := db.QueryRowContext(context.Background(),
		`SELECT blocked_at IS NOT NULL FROM customer WHERE customer_id = $1`, customerID).Scan(&blocked); err != nil {
		t.Fatalf("isRowBlocked(%s): %v", customerID, err)
	}
	return blocked
}
