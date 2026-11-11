package order_life_cycle

import (
	"testing"

	"welloresto-api/internal/models"
)

func ip(i int) *int { return &i }

func coursesString(cs []*int) []int {
	out := make([]int, len(cs))
	for i, c := range cs {
		if c != nil {
			out[i] = *c
		}
	}
	return out
}

func TestAssignCoursesFromSteps(t *testing.T) {
	tests := []struct {
		name  string
		steps []*int
		want  []int // 0 = hors suite
	}{
		{name: "entrée, plat, dessert", steps: []*int{ip(1), ip(2), ip(3)}, want: []int{1, 2, 3}},
		{name: "entrée et planche apéro ensemble", steps: []*int{ip(1), ip(1), ip(2), ip(3)}, want: []int{1, 1, 2, 3}},
		{name: "étapes absentes renumérotées (plat + dessert)", steps: []*int{ip(2), ip(3)}, want: []int{1, 2}},
		{name: "ordre de saisie quelconque", steps: []*int{ip(3), ip(1), ip(2), ip(1)}, want: []int{3, 1, 2, 1}},
		{name: "boisson sans étape hors suite", steps: []*int{nil, ip(2), nil, ip(4)}, want: []int{0, 1, 0, 2}},
		{name: "aucune étape", steps: []*int{nil, nil}, want: []int{0, 0}},
		{name: "étape 0 ignorée", steps: []*int{ip(0), ip(5)}, want: []int{0, 1}},
		{name: "panier vide", steps: []*int{}, want: []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coursesString(AssignCoursesFromSteps(tt.steps))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestNormalizeCourseDelay(t *testing.T) {
	if normalizeCourseDelay(nil) != nil || normalizeCourseDelay(ip(0)) != nil || normalizeCourseDelay(ip(-5)) != nil {
		t.Fatal("nil, 0 et négatif doivent donner un envoi manuel (nil)")
	}
	if d := normalizeCourseDelay(ip(15)); d == nil || *d != 15 {
		t.Fatalf("15 → %v", d)
	}
	if d := normalizeCourseDelay(ip(10000)); d == nil || *d != maxCourseDelayMinutes {
		t.Fatalf("délai borné attendu, obtenu %v", d)
	}
}

func TestCourseForInsert(t *testing.T) {
	empty := []models.OrderCoursePayload{}
	in := &models.OrderRequest{OrderType: "IN", Courses: &empty}
	takeAway := &models.OrderRequest{OrderType: "TAKE_AWAY", Courses: &empty}
	legacy := &models.OrderRequest{OrderType: "IN"} // caisse sans suites : champ absent

	p := &models.OrderProductPayload{Course: ip(2)}
	if c := courseForInsert(in, p); c == nil || *c != 2 {
		t.Fatalf("sur place : suite 2 attendue, obtenu %v", c)
	}
	if c := courseForInsert(takeAway, p); c != nil {
		t.Fatalf("à emporter : pas de suite, obtenu %v", *c)
	}
	if c := courseForInsert(legacy, p); c != nil {
		t.Fatalf("client sans suites : pas de suite, obtenu %v", *c)
	}
	if c := courseForInsert(in, &models.OrderProductPayload{}); c != nil {
		t.Fatalf("ligne hors suite : nil attendu, obtenu %v", *c)
	}
}
