package assistant

import (
	"testing"
	"time"
)

// Mêmes cas que src/lib/assistant/__tests__/engine.test.ts côté back-office :
// un terme appris doit être stocké exactement sous la forme comparée par le client.
func TestNormalize_ParityWithClient(t *testing.T) {
	cases := map[string]string{
		"Aujourd'hui, c'est l'Été !": "aujourd hui c est l ete",
		"  Œuf   à la COQUE ":        "oeuf a la coque",
		"Plus de Tiramisù…":          "plus de tiramisu",
		"Crème brûlée (x2)":          "creme brulee x2",
		"":                           "",
		"!!!":                        "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestDecideTerm(t *testing.T) {
	base := termStats{Status: "candidate", Source: "learned", Occurrences: 3, Users: 2, Days: 1, Share: 1}
	cases := []struct {
		name string
		mod  func(s *termStats)
		want termDecision
	}{
		{"seuil atteint : 3 fois, 2 personnes", func(s *termStats) {}, decisionPromote},
		{"une seule personne mais 3 jours", func(s *termStats) { s.Users = 1; s.Days = 3 }, decisionPromote},
		{"une seule personne, un seul jour", func(s *termStats) { s.Users = 1 }, decisionNone},
		{"pas assez d'occurrences", func(s *termStats) { s.Occurrences = 2 }, decisionNone},
		{"intention sensible : revue humaine", func(s *termStats) { s.Sensitive = true }, decisionFlagSensitive},
		{"sensible déjà signalé", func(s *termStats) { s.Sensitive = true; s.RequiresReview = true }, decisionNone},
		{"conflit entre intentions", func(s *termStats) { s.Share = 0.6 }, decisionFlagConflict},
		{"rétrogradé, attend un humain", func(s *termStats) { s.RequiresReview = true }, decisionNone},
		{"terme pré-rempli jamais touché", func(s *termStats) { s.Source = "seed" }, decisionNone},
		{"actif trop souvent annulé", func(s *termStats) { s.Status = "active"; s.Matches = 10; s.Negatives = 4 }, decisionDemote},
		{"actif, annulations rares", func(s *termStats) { s.Status = "active"; s.Matches = 10; s.Negatives = 2 }, decisionNone},
		{"actif, trop peu d'usages pour juger", func(s *termStats) { s.Status = "active"; s.Matches = 4; s.Negatives = 4 }, decisionNone},
		{"rejeté par un humain : définitif", func(s *termStats) { s.Status = "rejected"; s.Occurrences = 50 }, decisionNone},
	}
	for _, c := range cases {
		s := base
		c.mod(&s)
		if got := decideTerm(s); got != c.want {
			t.Errorf("%s : obtenu %v, attendu %v", c.name, got, c.want)
		}
	}
}

func TestDecideAlias(t *testing.T) {
	if got := decideAlias(termStats{Status: "candidate", Source: "learned", Occurrences: 2, Share: 1}); got != decisionPromote {
		t.Errorf("2 occurrences : obtenu %v", got)
	}
	if got := decideAlias(termStats{Status: "candidate", Source: "learned", Occurrences: 1, Share: 1}); got != decisionNone {
		t.Errorf("1 occurrence : obtenu %v", got)
	}
	if got := decideAlias(termStats{Status: "candidate", Source: "learned", Occurrences: 4, Share: 0.5}); got != decisionFlagConflict {
		t.Errorf("même surnom pour deux produits : obtenu %v", got)
	}
}

func TestValidateBatch(t *testing.T) {
	valid := func() *EventBatch {
		return &EventBatch{
			Session: SessionInfo{ID: "0b8f2f6e-1d4c-4d8f-9a3c-2a7b9b1c0d11", Surface: "backoffice", Device: "mobile"},
			Events:  []TrackedEvent{{EventID: "5a0e7c51-7f4d-4f43-8d42-0c3c7a3e9b10", Type: "opened", TS: time.Now().UnixMilli()}},
		}
	}
	if err := validateBatch(valid()); err != nil {
		t.Fatalf("lot valide refusé : %v", err)
	}
	bad := []func(b *EventBatch){
		func(b *EventBatch) { b.Session.ID = "pas-un-uuid" },
		func(b *EventBatch) { b.Session.Surface = "tv" },
		func(b *EventBatch) { b.Events = nil },
		func(b *EventBatch) { b.Events[0].Type = "drop_table" },
		func(b *EventBatch) { b.Events[0].EventID = "x" },
		func(b *EventBatch) { b.Events[0].TS = 0 },
		func(b *EventBatch) {
			for i := 0; i < maxEventsPerBatch; i++ {
				b.Events = append(b.Events, b.Events[0])
			}
		},
	}
	for i, mod := range bad {
		b := valid()
		mod(b)
		if err := validateBatch(b); err == nil {
			t.Errorf("cas %d : lot invalide accepté", i)
		}
	}
	b := valid()
	b.Session.Device = "frigo"
	b.Events[0].TurnID = "x"
	if err := validateBatch(b); err != nil || b.Session.Device != "" || b.Events[0].TurnID != "" {
		t.Errorf("valeurs facultatives invalides non neutralisées : %+v", b)
	}
}

func TestCleanLearnedText(t *testing.T) {
	lex := &Lexicon{Stopwords: []string{"le", "en", "de", "du"}, Blacklist: map[string]bool{"truc": true, "mets": true}}
	cases := []struct {
		in       string
		want     string
		kind     string
		accepted bool
	}{
		{"Sommeil", "sommeil", "keyword", true},
		{"en pause", "en pause", "phrase", true},
		{"dessert du jour", "dessert du jour", "phrase", true},
		{"le truc", "", "", false},
		{"mets", "", "", false},
		{"un deux trois quatre cinq", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		got, kind, ok := cleanLearnedText(c.in, lex, maxLearnedTermTokens)
		if ok != c.accepted || got != c.want || kind != c.kind {
			t.Errorf("cleanLearnedText(%q) = %q %q %v", c.in, got, kind, ok)
		}
	}
}

func TestComputeDelta(t *testing.T) {
	t0 := time.Now()
	ms := func(v int) *int { return &v }
	d := computeDelta([]eventRow{
		{TrackedEvent: TrackedEvent{Type: "query_submitted", Data: map[string]interface{}{"input": "voice"}}, ClientTS: t0},
		{TrackedEvent: TrackedEvent{Type: "fallback"}, ClientTS: t0},
		{TrackedEvent: TrackedEvent{Type: "action_succeeded"}, ClientTS: t0.Add(4 * time.Second), ElapsedMs: ms(4000)},
		{TrackedEvent: TrackedEvent{Type: "action_succeeded"}, ClientTS: t0.Add(9 * time.Second), ElapsedMs: ms(9000)},
		{TrackedEvent: TrackedEvent{Type: "closed", Data: map[string]interface{}{"outcome": "action"}}, ClientTS: t0.Add(12 * time.Second), ElapsedMs: ms(12000)},
	})
	if d.Events != 5 || d.Queries != 1 || d.Voice != 1 || d.Fallbacks != 1 || d.Succeeded != 2 {
		t.Errorf("compteurs : %+v", d)
	}
	if d.TimeToActionMs == nil || *d.TimeToActionMs != 4000 {
		t.Errorf("temps jusqu'à l'action : %v", d.TimeToActionMs)
	}
	if d.Outcome == nil || *d.Outcome != "action" || d.DurationMs == nil || *d.DurationMs != 12000 {
		t.Errorf("fermeture : %+v", d)
	}
}

func TestIdSignals(t *testing.T) {
	e := eventRow{TrackedEvent: TrackedEvent{EventID: "e", Data: map[string]interface{}{
		"term_ids":  []interface{}{float64(4), "x", float64(-1), float64(9)},
		"alias_ids": []interface{}{float64(2)},
	}}}
	got := idSignals(e, "matched")
	if len(got) != 3 || got[0].TargetID != 4 || got[1].TargetID != 9 || got[2].TargetType != "alias" {
		t.Errorf("signaux : %+v", got)
	}
}

func TestGroupKey(t *testing.T) {
	stop := map[string]bool{"de": true, "le": true}
	if a, b := groupKey("plus de saumon", stop), groupKey("saumon plus", stop); a != b {
		t.Errorf("regroupement : %q ≠ %q", a, b)
	}
}
