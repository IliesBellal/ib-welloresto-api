//go:build postgres_integration

package customers

import (
	"context"
	"strconv"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
)

// Nécessite la migration 198 (customer_search_*, pg_trgm, unaccent). Sans elle
// le test est SAUTÉ avec un message explicite, pas en échec : on n'applique pas
// les migrations à la place de l'utilisateur (docs/recherche-clients.md §7).
//
//	DB_DIALECT=postgres POSTGRES_URL=... go test -tags postgres_integration \
//	  ./internal/modules/customers/ -run Search -v
func TestSearchCustomers_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var present bool
	if err := db.QueryRowContext(ctx, `SELECT to_regproc('customer_search_norm') IS NOT NULL AND to_regproc('customer_search_name') IS NOT NULL AND to_regproc('customer_search_phone') IS NOT NULL`).Scan(&present); err != nil {
		t.Fatalf("check migration 198: %v", err)
	}
	if !present {
		t.Skip("migration 198_customer_search non appliquée — test de recherche sauté")
	}

	const merchantID = "999931"
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM customer WHERE merchant_id = $1`, merchantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	seed := func(brand, first, last, tel, email string, nbOrders int, lastOrderDays *int, blocked bool) string {
		t.Helper()
		var id int64
		err := db.QueryRowContext(ctx, `
			INSERT INTO customer (merchant_id, customer_brand, customer_name, customer_first_name, customer_last_name,
			                      customer_tel, customer_email, customer_nb_orders, enabled, last_order_date, blocked_at)
			VALUES ($1, $2, trim($3::text || ' ' || $4::text), $3::text, $4::text, NULLIF($5::text, ''), NULLIF($6::text, ''), $7, true,
			        CASE WHEN $8::int IS NULL THEN NULL ELSE now() - ($8::int * interval '1 day') END,
			        CASE WHEN $9 THEN now() ELSE NULL END)
			RETURNING customer_id`,
			merchantID, brand, first, last, tel, email, nbOrders, lastOrderDays, blocked).Scan(&id)
		if err != nil {
			t.Fatalf("seed %s %s: %v", first, last, err)
		}
		return strconv.FormatInt(id, 10)
	}
	days := func(n int) *int { return &n }

	jean := seed("WELLO_RESTO", "Jean", "Dupont", "+33612345678", "jean.dupont@example.com", 5, days(10), false)
	helene := seed("WELLO_RESTO", "Hélène", "Dupont", "+33698765432", "", 1, days(400), false)
	jeanne := seed("WELLO_RESTO", "Jeanne", "Martin", "+33611111111", "", 20, days(3), false)
	marie := seed("WELLO_RESTO", "Marie", "Durand", "+33622222222", "", 1, days(60), false)
	omar := seed("WELLO_RESTO", "Omar", "Zidane", "+33633333333", "", 50, days(5), false)
	uber := seed("UBER_EATS", "Dupont", "Marketplace", "+33644444444", "", 9, days(1), false)
	blocked := seed("WELLO_RESTO", "Paul", "Bloqué", "+33655555555", "", 30, days(2), true)
	_ = uber

	repo := NewCustomerRepository(db)
	search := func(term string) ([]CustomerSearchResult, int) {
		t.Helper()
		res, total, err := repo.SearchCustomers(ctx, merchantID, term, "", "", 1, 10)
		if err != nil {
			t.Fatalf("SearchCustomers(%q): %v", term, err)
		}
		return res, total
	}
	ids := func(res []CustomerSearchResult) []string {
		out := make([]string, len(res))
		for i, r := range res {
			out[i] = r.CustomerID
		}
		return out
	}
	first := func(term string) string {
		t.Helper()
		res, _ := search(term)
		if len(res) == 0 {
			t.Fatalf("SearchCustomers(%q) : aucun résultat", term)
		}
		return res[0].CustomerID
	}

	// Prénom et nom, dans les deux ordres.
	for _, term := range []string{"jean dupont", "dupont jean", "DUPONT Jean", "jean dup"} {
		if got := first(term); got != jean {
			t.Errorf("%q : premier = %s, want Jean Dupont (%s)", term, got, jean)
		}
	}
	// « jean » seul : Jean (mot exact) avant Jeanne (préfixe), malgré les 20 commandes de Jeanne.
	if got := ids(mustSearch(t, search, "jean")); len(got) != 2 || got[0] != jean || got[1] != jeanne {
		t.Errorf("jean : %v, want [Jean Jeanne]", got)
	}
	// Accents : « helene » trouve « Hélène », et inversement.
	if got := first("helene"); got != helene {
		t.Errorf("helene : %s, want %s", got, helene)
	}
	if got := first("HÉLÈNE"); got != helene {
		t.Errorf("HÉLÈNE : %s, want %s", got, helene)
	}
	// Pertinence avant fidélité : « mar » est un début de mot chez Marie/Martin,
	// seulement contenu chez Omar (qui a pourtant 50 commandes).
	got := ids(mustSearch(t, search, "mar"))
	if len(got) < 3 || got[len(got)-1] != omar {
		t.Errorf("mar : %v, Omar (contenu) doit être dernier", got)
	}
	// Téléphone : national complet, avec espaces, international, fin de numéro.
	for _, term := range []string{"0612345678", "06 12 34 56 78", "+33 6 12 34 56 78", "0033612345678", "345678", "5678"} {
		if g := first(term); g != jean {
			t.Errorf("%q : %s, want Jean (%s)", term, g, jean)
		}
	}
	// Début de numéro saisi en national.
	if g := first("06 98 76"); g != helene {
		t.Errorf("06 98 76 : %s, want %s", g, helene)
	}
	// Email.
	if g := first("jean.dupont@example"); g != jean {
		t.Errorf("email : %s, want %s", g, jean)
	}
	// Les clients de places de marché ne remontent jamais (même nom que Jean).
	res, total := search("dupont")
	if total != 2 || len(res) != 2 {
		t.Errorf("dupont : total=%d len=%d, want 2 (Jean, Hélène ; pas le client UBER_EATS)", total, len(res))
	}
	// Un client bloqué reste trouvable (le POS affiche le badge), flag renseigné.
	if res, _ := search("paul bloqué"); len(res) != 1 || res[0].CustomerID != blocked || !res[0].Blocked {
		t.Errorf("paul bloqué : %+v", res)
	}
	// Saisie trop courte : rien, sans erreur.
	if res, total := search("m"); len(res) != 0 || total != 0 {
		t.Errorf("« m » ne doit rien renvoyer : %d / %d", len(res), total)
	}
	// Jokers LIKE neutralisés : « % » ne doit pas tout renvoyer.
	if res, _ := search("%%"); len(res) != 0 {
		t.Errorf("« %%%% » ne doit rien renvoyer : %d", len(res))
	}
	// Total cohérent avec une page plus petite, et page hors bornes.
	small, total, err := repo.SearchCustomers(ctx, merchantID, "dupont", "", "", 1, 1)
	if err != nil || len(small) != 1 || total != 2 {
		t.Errorf("page de 1 : len=%d total=%d err=%v", len(small), total, err)
	}
	beyond, total, err := repo.SearchCustomers(ctx, merchantID, "dupont", "", "", 5, 1)
	if err != nil || len(beyond) != 0 || total != 2 {
		t.Errorf("page 5 : len=%d total=%d err=%v, want (0, 2)", len(beyond), total, err)
	}
	// Tri explicite (back-office) respecté : par nombre de commandes décroissant.
	sorted, _, err := repo.SearchCustomers(ctx, merchantID, "dupont", "customer_nb_orders", "DESC", 1, 10)
	if err != nil || len(sorted) != 2 || sorted[0].CustomerID != jean {
		t.Errorf("tri explicite : %+v err=%v", sorted, err)
	}
	_ = marie

	// --- clients fréquents ---
	freq, _, err := repo.GetFrequentCustomers(ctx, merchantID, 10)
	if err != nil {
		t.Fatalf("GetFrequentCustomers: %v", err)
	}
	var freqIDs []string
	for _, c := range freq {
		freqIDs = append(freqIDs, c.CustomerID)
	}
	// omar (50) > jeanne (20) > jean (5) > marie (1). Exclus : Hélène (dernière
	// commande il y a 400 jours), le client UBER_EATS, le client bloqué.
	want := []string{omar, jeanne, jean, marie}
	if len(freqIDs) != len(want) {
		t.Fatalf("fréquents : %v, want %v", freqIDs, want)
	}
	for i := range want {
		if freqIDs[i] != want[i] {
			t.Fatalf("fréquents : %v, want %v", freqIDs, want)
		}
	}
	limited, _, err := repo.GetFrequentCustomers(ctx, merchantID, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limite 2 : len=%d err=%v", len(limited), err)
	}
}

func mustSearch(t *testing.T, search func(string) ([]CustomerSearchResult, int), term string) []CustomerSearchResult {
	t.Helper()
	res, _ := search(term)
	return res
}
