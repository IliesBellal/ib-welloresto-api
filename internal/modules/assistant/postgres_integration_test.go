//go:build postgres_integration

package assistant

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx/pgtest"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/modules/auth"
)

// Cycle complet contre Postgres (migrations 206 et 207 appliquées) :
// modèle servi → événements → apprentissage → modèle enrichi → rétrogradation.
func TestAssistant_LearningCycle_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	const merchant = "itest-assistant-m1"

	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM assistant_sessions WHERE merchant_id = $1`, merchant)
		_, _ = db.ExecContext(ctx, `DELETE FROM assistant_learning_signals WHERE merchant_id = $1`, merchant)
		_, _ = db.ExecContext(ctx, `DELETE FROM assistant_terms WHERE merchant_id = $1 OR (source = 'learned' AND text = 'sommeil')`, merchant)
		_, _ = db.ExecContext(ctx, `DELETE FROM assistant_entity_aliases WHERE merchant_id = $1`, merchant)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewRepository(db)
	svc := NewService(repo)
	as := func(user string) context.Context {
		return middleware.WithUser(ctx, &auth.UserLoginRow{UserID: user, MerchantID: merchant})
	}

	// 1. Modèle pré-rempli.
	res, err := svc.GetModel(as("u1"), "backoffice", "")
	if err != nil || res.Model == nil {
		t.Fatalf("GetModel: %v %+v", err, res)
	}
	if len(res.Model.Intents) < 50 || len(res.Model.Stopwords) == 0 {
		t.Fatalf("modèle incomplet : %d intentions, %d mots vides", len(res.Model.Intents), len(res.Model.Stopwords))
	}
	v1 := res.Model.Version
	if again, _ := svc.GetModel(as("u1"), "backoffice", v1); !again.Unchanged {
		t.Fatalf("même version : unchanged attendu")
	}

	// 2. Trois succès après repli, par deux personnes : « sommeil » → rupture.
	learn := func(user string, withAlias bool) {
		turn := uuid.NewString()
		learning := map[string]interface{}{
			"intent_key": "menu.product.out_of_stock",
			"query_norm": "la margherita est en sommeil",
			"term":       map[string]interface{}{"text": "sommeil", "kind": "keyword"},
		}
		if withAlias {
			learning["alias"] = map[string]interface{}{"entity_type": "product", "entity_id": "p1", "text": "dessert du jour"}
		}
		now := time.Now().UnixMilli()
		batch := &EventBatch{
			Session: SessionInfo{ID: uuid.NewString(), Surface: "backoffice", StartedAt: now - 5000, Device: "mobile", Online: true},
			Events: []TrackedEvent{
				{EventID: uuid.NewString(), Type: "opened", Seq: 1, TS: now - 5000},
				{EventID: uuid.NewString(), Type: "query_submitted", Seq: 2, TS: now - 4000, TurnID: turn,
					Data: map[string]interface{}{"text": "La margherita est en sommeil", "query_norm": "la margherita est en sommeil", "input": "text"}},
				{EventID: uuid.NewString(), Type: "fallback", Seq: 3, TS: now - 3900, TurnID: turn,
					Data: map[string]interface{}{"query_norm": "la margherita est en sommeil", "text": "La margherita est en sommeil"}},
				{EventID: uuid.NewString(), Type: "suggestion_clicked", Seq: 4, TS: now - 3000, TurnID: turn, IntentKey: "menu.product.out_of_stock"},
				{EventID: uuid.NewString(), Type: "action_succeeded", Seq: 5, TS: now - 2000, TurnID: turn, IntentKey: "menu.product.out_of_stock",
					Data: map[string]interface{}{"learning": learning}},
				{EventID: uuid.NewString(), Type: "closed", Seq: 6, TS: now, Data: map[string]interface{}{"outcome": "action"}},
			},
		}
		if err := svc.RecordEvents(as(user), batch); err != nil {
			t.Fatalf("RecordEvents(%s): %v", user, err)
		}
		// Renvoi du même lot (coupure réseau) : sans effet.
		if err := svc.RecordEvents(as(user), batch); err != nil {
			t.Fatalf("renvoi du lot: %v", err)
		}
	}
	learn("u1", true)
	learn("u1", true)

	// Une seule personne, un seul jour : le terme attend ; le surnom (propre
	// au restaurant) est promu dès 2 occurrences.
	if rep, err := RunLearning(ctx, db); err != nil || rep.TermsPromoted != 0 || rep.AliasesPromoted != 1 {
		t.Fatalf("premier passage : %+v, %v", rep, err)
	}
	learn("u2", false)
	rep, err := RunLearning(ctx, db)
	if err != nil {
		t.Fatalf("RunLearning: %v", err)
	}
	if rep.TermsPromoted != 1 || rep.AliasesPromoted != 0 {
		t.Fatalf("promotion attendue du terme seul : %+v", rep)
	}

	var occ, users int
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status, occurrences, distinct_users FROM assistant_terms
		WHERE merchant_id = $1 AND text = 'sommeil'`, merchant).Scan(&status, &occ, &users); err != nil {
		t.Fatalf("terme appris introuvable: %v", err)
	}
	if status != "active" || occ != 3 || users != 2 {
		t.Fatalf("terme appris : %s, %d occurrences, %d personnes (lots renvoyés comptés deux fois ?)", status, occ, users)
	}

	// 3. Le modèle de l'établissement contient le terme (poids réduit) et le surnom.
	res, err = svc.GetModel(as("u1"), "backoffice", v1)
	if err != nil || res.Unchanged || res.Model == nil {
		t.Fatalf("le modèle aurait dû changer : %v %+v", err, res)
	}
	var learnedID int64
	for _, it := range res.Model.Intents {
		if it.Key != "menu.product.out_of_stock" {
			continue
		}
		for _, term := range it.Terms {
			if term.Text == "sommeil" {
				learnedID = term.ID
				if term.Weight >= 1 {
					t.Errorf("poids d'un terme appris non validé : %v", term.Weight)
				}
			}
		}
	}
	if learnedID == 0 || len(res.Model.Aliases) != 1 || res.Model.Aliases[0].Alias != "dessert du jour" {
		t.Fatalf("terme ou surnom absent du modèle : term=%d aliases=%+v", learnedID, res.Model.Aliases)
	}
	// Un autre établissement ne le voit pas.
	other := middleware.WithUser(ctx, &auth.UserLoginRow{UserID: "u9", MerchantID: "itest-assistant-other"})
	if res2, _ := svc.GetModel(other, "backoffice", ""); res2 != nil && res2.Model != nil {
		for _, it := range res2.Model.Intents {
			for _, term := range it.Terms {
				if term.Text == "sommeil" {
					t.Fatalf("terme d'établissement servi à un autre établissement")
				}
			}
		}
	}

	// 4. Le terme sert puis est annulé trop souvent : rétrogradé.
	now := time.Now().UnixMilli()
	events := []TrackedEvent{}
	for i := 0; i < 6; i++ {
		events = append(events, TrackedEvent{EventID: uuid.NewString(), Type: "intent_matched", Seq: 2 * i, TS: now,
			IntentKey: "menu.product.out_of_stock", Data: map[string]interface{}{"term_ids": []interface{}{float64(learnedID)}}})
		if i < 3 {
			events = append(events, TrackedEvent{EventID: uuid.NewString(), Type: "cancelled", Seq: 2*i + 1, TS: now,
				IntentKey: "menu.product.out_of_stock", Data: map[string]interface{}{"term_ids": []interface{}{float64(learnedID)}}})
		}
	}
	if err := svc.RecordEvents(as("u1"), &EventBatch{
		Session: SessionInfo{ID: uuid.NewString(), Surface: "backoffice", StartedAt: now},
		Events:  events,
	}); err != nil {
		t.Fatalf("RecordEvents (retours négatifs): %v", err)
	}
	rep, err = RunLearning(ctx, db)
	if err != nil || rep.TermsDemoted != 1 {
		t.Fatalf("rétrogradation attendue : %+v %v", rep, err)
	}

	// 5. Une session ne peut pas être reprise par un autre utilisateur.
	var sid string
	_ = db.QueryRowContext(ctx, `SELECT id::text FROM assistant_sessions WHERE merchant_id = $1 LIMIT 1`, merchant).Scan(&sid)
	err = svc.RecordEvents(as("intrus"), &EventBatch{
		Session: SessionInfo{ID: sid, Surface: "backoffice", StartedAt: now},
		Events:  []TrackedEvent{{EventID: uuid.NewString(), Type: "opened", TS: now}},
	})
	if err == nil {
		t.Fatalf("session d'un autre utilisateur acceptée")
	}

	// 6. Indicateurs et replis pour l'administration.
	from := time.Now().Add(-time.Hour)
	st, err := repo.Stats(ctx, from, time.Now().Add(time.Hour), merchant)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Sessions < 3 || st.Fallbacks != 3 || st.ActionsSucceeded != 3 || st.MedianTimeToActionM == nil {
		t.Fatalf("stats : %+v", st)
	}
	groups, err := repo.FallbackGroups(ctx, from, time.Now().Add(time.Hour), 10)
	if err != nil || len(groups) == 0 {
		t.Fatalf("FallbackGroups: %v %+v", err, groups)
	}
	found := false
	for _, g := range groups {
		if g.QueryNorm == "la margherita est en sommeil" {
			found = true
			if g.Count != 3 || len(g.ChosenAfter) != 1 || g.ChosenAfter[0].IntentKey != "menu.product.out_of_stock" {
				t.Fatalf("groupe de repli : %+v", g)
			}
		}
	}
	if !found {
		t.Fatalf("repli absent : %s", fmt.Sprint(groups))
	}
}

// Un synonyme actif dans 3 établissements devient global ; la purge RGPD
// retire le texte libre des vieux événements.
func TestAssistant_GlobalizationAndRetention_Postgres(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	merchants := []string{"itest-glob-1", "itest-glob-2", "itest-glob-3"}
	cleanup := func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM assistant_terms WHERE text = 'itest pause cafe'`)
		for _, m := range merchants {
			_, _ = db.ExecContext(ctx, `DELETE FROM assistant_sessions WHERE merchant_id = $1`, m)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, m := range merchants {
		if _, err := db.ExecContext(ctx, `INSERT INTO assistant_terms (intent_key, text, kind, source, status, merchant_id)
			VALUES ('menu.product.out_of_stock', 'itest pause cafe', 'phrase', 'learned', 'active', $1)`, m); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	rep, err := RunLearning(ctx, db)
	if err != nil || rep.TermsGlobalized != 1 {
		t.Fatalf("passage en global attendu : %+v %v", rep, err)
	}
	var status string
	var dm int
	if err := db.QueryRowContext(ctx, `SELECT status, distinct_merchants FROM assistant_terms
		WHERE text = 'itest pause cafe' AND merchant_id = ''`).Scan(&status, &dm); err != nil || status != "active" || dm != 3 {
		t.Fatalf("terme global : %s %d %v", status, dm, err)
	}
	if rep, _ := RunLearning(ctx, db); rep.TermsGlobalized != 0 {
		t.Fatalf("second passage : rien de neuf attendu, %+v", rep)
	}

	// Événement vieux de 100 jours : texte retiré, reste conservé.
	sid := uuid.NewString()
	old := time.Now().Add(-100 * 24 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO assistant_sessions (id, merchant_id, user_id, surface, started_at) VALUES ($1, $2, 'u', 'backoffice', $3)`,
		sid, merchants[0], old); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO assistant_events (event_id, session_id, merchant_id, user_id, type, client_ts, query_norm, data, created_at)
		VALUES ($1, $2, $3, 'u', 'fallback', $4, 'annule la commande de dupont', '{"text":"Annule la commande de Dupont","score":12}', $4)`,
		uuid.NewString(), sid, merchants[0], old); err != nil {
		t.Fatalf("event: %v", err)
	}
	RunRetention(ctx, db, zap.NewNop())
	var data string
	var qn *string
	if err := db.QueryRowContext(ctx, `SELECT data::text, query_norm FROM assistant_events WHERE session_id = $1`, sid).Scan(&data, &qn); err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if qn != nil || data != `{"score": 12}` {
		t.Fatalf("anonymisation : query_norm=%v data=%s", qn, data)
	}
}
