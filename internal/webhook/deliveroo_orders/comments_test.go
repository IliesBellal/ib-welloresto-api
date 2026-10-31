package deliveroo_orders

import "testing"

func TestOrderComment(t *testing.T) {
	cases := []struct {
		name string
		ord  DeliverooOrder
		want string
	}{
		{"rien", DeliverooOrder{}, ""},
		{"notes du client", DeliverooOrder{OrderNotes: "  Sans oignons  "}, "Sans oignons"},
		{"pas de couverts : rien à signaler", DeliverooOrder{CutleryNotes: "PAS DE COUVERTS"}, ""},
		{"pas de couverts, casse libre", DeliverooOrder{CutleryNotes: "Pas de couverts"}, ""},
		{"couverts reproduits tels que reçus", DeliverooOrder{CutleryNotes: "2 couverts svp"}, "Couverts : 2 couverts svp"},
		{"consignes de livraison", DeliverooOrder{Delivery: &DeliverooDelivery{DeliveryNotes: "Code 1234B, 3e étage"}}, "Livraison : Code 1234B, 3e étage"},
		{"livraison sans consigne", DeliverooOrder{Delivery: &DeliverooDelivery{}}, ""},
		{
			"tout ensemble",
			DeliverooOrder{OrderNotes: "Bien cuite", CutleryNotes: "1 couvert", Delivery: &DeliverooDelivery{DeliveryNotes: "Sonner"}},
			"Bien cuite\nCouverts : 1 couvert\nLivraison : Sonner",
		},
	}
	for _, c := range cases {
		if got := orderComment(c.ord); got != c.want {
			t.Errorf("%s : %q, want %q", c.name, got, c.want)
		}
	}
}
