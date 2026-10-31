//go:build postgres_integration

package order_life_cycle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/customers"
)

// La création d'une commande Uber Eats / Deliveroo est atomique : tout ou rien.
// Les étapes tolérantes (commentaires…) gardent leur comportement, et le résultat
// d'une création réussie est identique à celui du chemin sans transaction.
func TestCreateOrder_MarketplaceIsAtomic_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()

	var merchantID string
	cleanupFor := func(mid string) {
		if mid == "" {
			return
		}
		for _, q := range []string{
			`DELETE FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`,
			`DELETE FROM order_item_configuration WHERE order_item_id IN (SELECT order_item_id FROM orderitems WHERE merchant_id = $1)`,
			`DELETE FROM payments WHERE merchant_id = $1`,
			`DELETE FROM orderitems WHERE merchant_id = $1`,
			`DELETE FROM orders WHERE merchant_id = $1`,
			`DELETE FROM customer WHERE merchant_id = $1`,
			`DELETE FROM products WHERE merchant_Id = $1`,
			`DELETE FROM merchant_parameters WHERE merchant_id = $1`,
			`DELETE FROM merchant WHERE id = $1`,
		} {
			_, _ = db.ExecContext(ctx, q, mid)
		}
	}
	var oldID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM merchant WHERE siret = 'siret-olc-tx' LIMIT 1`).Scan(&oldID); err == nil {
		cleanupFor(strconv.FormatInt(oldID, 10))
	}
	t.Cleanup(func() { cleanupFor(merchantID) })

	var merchantIntID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO merchant (fullname, address, street_number, street, zip_code, city, siret, web_site, merchanttel, token, timezone)
		VALUES ('ITest OLC Tx', 'a', '1', 's', '75001', 'Paris', 'siret-olc-tx', 'https://x', '06', 'mtok-olc-tx', 'UTC')
		RETURNING id`).Scan(&merchantIntID); err != nil {
		t.Fatalf("seed merchant: %v", err)
	}
	merchantID = strconv.FormatInt(merchantIntID, 10)
	if _, err := db.ExecContext(ctx, `INSERT INTO merchant_parameters (merchant_id, last_menu_update) VALUES ($1, now())`, merchantID); err != nil {
		t.Fatalf("seed params: %v", err)
	}
	var prodA, prodB int64
	for name, dest := range map[string]*int64{"itest-tx-a": &prodA, "itest-tx-b": &prodB} {
		if err := db.QueryRowContext(ctx, `INSERT INTO products (merchant_Id, name, price, category, status) VALUES ($1, $2, 1000, 'c', '1') RETURNING product_id`, merchantID, name).Scan(dest); err != nil {
			t.Fatalf("seed product %s: %v", name, err)
		}
	}
	idA, idB := strconv.FormatInt(prodA, 10), strconv.FormatInt(prodB, 10)

	repo := NewOrdersLifeCycleRepository(db, customers.NewCustomerRepository(db))

	// newReq : même commande pour toutes les marques ; payment > 0 ajoute un paiement.
	seq := 0
	newReq := func(brand, createdBy string, payment int) *models.RequestObject {
		seq++
		bid := fmt.Sprintf("itest-tx-%d", seq)
		num := fmt.Sprintf("TX%d", seq)
		register := brand
		comment := "Sans oignons"
		name := "Samantha K"
		req := &models.RequestObject{
			MerchantID: merchantID,
			Order: models.OrderRequest{
				Brand: brand, BrandOrderID: &bid, BrandOrderNum: &num, CashRegisterId: &register,
				TTC: 2000, OrderType: models.OrderTypeDelivery, CreatedBy: &createdBy, Comment: &comment,
				MerchantApproval: models.MerchantApprovalPendingApproval,
				Customer:         &models.CustomerRequest{Name: &name, CustomerBrand: brand},
				Products: []models.OrderProductPayload{
					{ProductID: idA, ProductName: "A", Quantity: 1, Price: 1000,
						Comment: &models.OrderItemCommentPayload{Content: "Bien cuite", UserID: createdBy}},
					{ProductID: idB, ProductName: "B", Quantity: 2, Price: 500},
				},
			},
		}
		if payment > 0 {
			req.Order.Payments = []models.PaymentPayload{{Amount: payment, MOP: models.PaymentUberEats}}
		}
		return req
	}

	count := func(q string, args ...any) int {
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s : %v", q, err)
		}
		return n
	}
	// fingerprint résume ce qu'une commande a écrit (hors identifiants et numéros).
	type fingerprint struct {
		Items    []string
		Comments []string
		Payments []string
		IsPaid   bool
		Price    int
	}
	fingerprintOf := func(orderID string) fingerprint {
		var f fingerprint
		rows, err := db.QueryContext(ctx, `SELECT product_id, quantity, price, base_price, COALESCE(tva_id, -9), COALESCE(tva_rate, -9) FROM orderitems WHERE order_id = $1`, orderID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var pid, qty, price, base, tvaID int
			var rate float64
			_ = rows.Scan(&pid, &qty, &price, &base, &tvaID, &rate)
			f.Items = append(f.Items, fmt.Sprintf("%d×%d@%d/%d tva %d %.1f", pid, qty, price, base, tvaID, rate))
		}
		rows.Close()
		rows, err = db.QueryContext(ctx, `SELECT COALESCE(order_item_id::text, 'order'), content FROM order_comments WHERE order_id = $1`, orderID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var scope, content string
			_ = rows.Scan(&scope, &content)
			if scope != "order" {
				scope = "item"
			}
			f.Comments = append(f.Comments, scope+": "+content)
		}
		rows.Close()
		rows, err = db.QueryContext(ctx, `SELECT mop, amount FROM payments WHERE order_id = $1`, orderID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var mop string
			var amount int
			_ = rows.Scan(&mop, &amount)
			f.Payments = append(f.Payments, fmt.Sprintf("%s %d", mop, amount))
		}
		rows.Close()
		if err := db.QueryRowContext(ctx, `SELECT isPaid, price FROM orders WHERE order_id = $1`, orderID).Scan(&f.IsPaid, &f.Price); err != nil {
			t.Fatal(err)
		}
		sort.Strings(f.Items)
		sort.Strings(f.Comments)
		sort.Strings(f.Payments)
		return f
	}

	t.Run("création réussie : tout est écrit, comme sans transaction", func(t *testing.T) {
		res, err := repo.CreateOrder(ctx, newReq(models.BrandUberEats, "WEBHOOK_UBER_EATS", 2000))
		if err != nil || res.Status != "success" {
			t.Fatalf("CreateOrder(Uber Eats) = (%+v, %v)", res, err)
		}
		// Référence : la même commande par le chemin sans transaction (autre marque).
		ref, err := repo.CreateOrder(ctx, newReq(models.BrandWelloResto, "WEBHOOK_UBER_EATS", 2000))
		if err != nil || ref.Status != "success" {
			t.Fatalf("CreateOrder(référence) = (%+v, %v)", ref, err)
		}

		got, want := fingerprintOf(res.OrderID), fingerprintOf(ref.OrderID)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("la commande transactionnelle diffère de la référence :\n got %+v\nwant %+v", got, want)
		}
		if len(got.Items) != 2 || len(got.Comments) != 2 || len(got.Payments) != 1 || !got.IsPaid {
			t.Fatalf("contenu incomplet : %+v", got)
		}
		for _, item := range got.Items {
			if strings.Contains(item, "tva -9") {
				t.Fatalf("la TVA doit être figée sur chaque ligne : %s", item)
			}
		}
	})

	t.Run("échec après l'en-tête : aucune trace", func(t *testing.T) {
		before := map[string]int{
			"orders":   count(`SELECT count(*) FROM orders WHERE merchant_id = $1`, merchantID),
			"items":    count(`SELECT count(*) FROM orderitems WHERE merchant_id = $1`, merchantID),
			"comments": count(`SELECT count(*) FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, merchantID),
			"payments": count(`SELECT count(*) FROM payments WHERE merchant_id = $1`, merchantID),
			"customer": count(`SELECT count(*) FROM customer WHERE merchant_id = $1`, merchantID),
		}

		// paiement supérieur au total : refusé tout à la fin de la création, après
		// le client, l'en-tête, les lignes et les commentaires
		res, err := repo.CreateOrder(ctx, newReq(models.BrandUberEats, "WEBHOOK_UBER_EATS", 2500))
		var notPaid *models.OrderNotFullyPaidError
		if !errors.As(err, &notPaid) || res != nil {
			t.Fatalf("CreateOrder = (%+v, %v), want une erreur de paiement et aucun résultat", res, err)
		}

		after := map[string]int{
			"orders":   count(`SELECT count(*) FROM orders WHERE merchant_id = $1`, merchantID),
			"items":    count(`SELECT count(*) FROM orderitems WHERE merchant_id = $1`, merchantID),
			"comments": count(`SELECT count(*) FROM order_comments WHERE order_id IN (SELECT order_id FROM orders WHERE merchant_id = $1)`, merchantID),
			"payments": count(`SELECT count(*) FROM payments WHERE merchant_id = $1`, merchantID),
			"customer": count(`SELECT count(*) FROM customer WHERE merchant_id = $1`, merchantID),
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("une création échouée a laissé des traces :\navant %v\naprès %v", before, after)
		}
	})

	t.Run("client non enregistrable : la commande est créée sans client, son nom dans le commentaire", func(t *testing.T) {
		// customer.customer_name est limité à 50 caractères : l'insertion du client échoue.
		tooLong := strings.Repeat("Z", 60)
		req := newReq(models.BrandUberEats, "WEBHOOK_UBER_EATS", 2000)
		req.Order.Customer.Name = &tooLong

		res, err := repo.CreateOrder(ctx, req)
		if err != nil || res.Status != "success" {
			t.Fatalf("CreateOrder = (%+v, %v), want une commande créée malgré le client", res, err)
		}

		var customerID sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT customer_id::text FROM orders WHERE order_id = $1`, res.OrderID).Scan(&customerID); err != nil {
			t.Fatal(err)
		}
		if customerID.Valid {
			t.Errorf("la commande ne doit pas avoir de client, got %s", customerID.String)
		}
		if n := count(`SELECT count(*) FROM customer WHERE merchant_id = $1 AND customer_name LIKE 'ZZZ%'`, merchantID); n != 0 {
			t.Errorf("%d client(s) laissé(s) par l'insertion échouée", n)
		}
		f := fingerprintOf(res.OrderID)
		if len(f.Items) != 2 || len(f.Payments) != 1 || !f.IsPaid {
			t.Errorf("la suite de la création doit se dérouler : %+v", f)
		}
		var comment string
		if err := db.QueryRowContext(ctx, `SELECT content FROM order_comments WHERE order_id = $1 AND order_item_id IS NULL`, res.OrderID).Scan(&comment); err != nil {
			t.Fatal(err)
		}
		if comment != "Sans oignons\nClient : "+tooLong {
			t.Errorf("commentaire = %q, want le texte du client suivi de son nom", comment)
		}
	})

	t.Run("hors plateforme l'échec du client reste une erreur, comme avant", func(t *testing.T) {
		before := count(`SELECT count(*) FROM orders WHERE merchant_id = $1`, merchantID)
		tooLong := strings.Repeat("Z", 60)
		req := newReq(models.BrandWelloResto, "WEBHOOK_UBER_EATS", 0)
		req.Order.Customer.Name = &tooLong

		res, err := repo.CreateOrder(ctx, req)
		if err == nil {
			t.Fatalf("CreateOrder = (%+v, nil), want l'erreur d'enregistrement du client", res)
		}
		if after := count(`SELECT count(*) FROM orders WHERE merchant_id = $1`, merchantID); after != before {
			t.Errorf("%d commande(s) créée(s) malgré l'erreur", after-before)
		}
	})

	t.Run("le numéro temporaire et son code sont enregistrés avec le client", func(t *testing.T) {
		req := newReq(models.BrandUberEats, "WEBHOOK_UBER_EATS", 2000)
		phone, code := "+33 6 44 60 35 83", "320 82 206"
		req.Order.Customer.TemporaryPhone, req.Order.Customer.TemporaryPhoneCode = &phone, &code

		res, err := repo.CreateOrder(ctx, req)
		if err != nil || res.Status != "success" {
			t.Fatalf("CreateOrder = (%+v, %v)", res, err)
		}
		var gotPhone, gotCode sql.NullString
		if err := db.QueryRowContext(ctx, `
			SELECT c.customer_temporary_phone, c.customer_temporary_phone_code
			FROM orders o JOIN customer c ON c.customer_id = o.customer_id WHERE o.order_id = $1`, res.OrderID).Scan(&gotPhone, &gotCode); err != nil {
			t.Fatal(err)
		}
		if gotPhone.String != phone || gotCode.String != code {
			t.Errorf("numéro temporaire = %q / %q, want %q / %q", gotPhone.String, gotCode.String, phone, code)
		}
	})

	t.Run("étape tolérante en échec : la commande est créée, comme avant", func(t *testing.T) {
		// created_by de 25 caractères : accepté par orders (40) et payments (64),
		// refusé par order_comments.user_id (20) — le commentaire de commande et
		// celui de la ligne échouent, la commande doit être créée sans eux.
		longUser := "WEBHOOK_UBER_EATS_LONGER_"
		res, err := repo.CreateOrder(ctx, newReq(models.BrandUberEats, longUser, 2000))
		if err != nil || res.Status != "success" {
			t.Fatalf("CreateOrder(commentaire impossible) = (%+v, %v), want une commande créée", res, err)
		}
		f := fingerprintOf(res.OrderID)
		if len(f.Items) != 2 || len(f.Payments) != 1 || !f.IsPaid {
			t.Fatalf("la suite de la création doit se dérouler après l'échec toléré : %+v", f)
		}
		if len(f.Comments) != 0 {
			t.Fatalf("aucun commentaire ne peut être écrit (user_id trop long) : %v", f.Comments)
		}
	})
}
