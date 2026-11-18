package users

import "testing"

func TestCustomerAccountLimitNotification(t *testing.T) {
	if customerAccountLimitNotification(0, 0) != nil {
		t.Fatal("aucune ardoise à surveiller : pas de notification")
	}
	n := customerAccountLimitNotification(2, 1)
	if n == nil || n.Severity != "danger" || n.Type != "CUSTOMER_ACCOUNT_LIMIT" {
		t.Fatalf("notification = %+v", n)
	}
	if n.Description != "1 client au-delà de son plafond, 2 clients proches de leur plafond" {
		t.Fatalf("description = %q", n.Description)
	}
	if n := customerAccountLimitNotification(1, 0); n.Severity != "warning" || n.Description != "1 client proche de son plafond" {
		t.Fatalf("proche seulement = %+v", n)
	}
}
