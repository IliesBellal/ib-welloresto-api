package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

// Requêtes de la plateforme admin (staff WelloResto, cross-tenant).

type TermFilter struct {
	Status     string
	Source     string
	IntentKey  string
	MerchantID string
	Review     *bool
	Limit      int
	Offset     int
}

const termSelect = `SELECT t.id, t.intent_key, i.label, t.text, t.kind, t.weight, t.source, t.status, t.merchant_id,
		t.occurrences, t.distinct_users, t.distinct_days, t.distinct_merchants, t.matches, t.negatives,
		t.requires_review, t.review_reason, t.first_seen_at, t.last_seen_at, t.promoted_at, t.reviewed_at
	FROM assistant_terms t JOIN assistant_intents i ON i.intent_key = t.intent_key`

func scanTerm(sc interface{ Scan(...interface{}) error }) (*TermRow, error) {
	var t TermRow
	err := sc.Scan(&t.ID, &t.IntentKey, &t.IntentLabel, &t.Text, &t.Kind, &t.Weight, &t.Source, &t.Status, &t.MerchantID,
		&t.Occurrences, &t.DistinctUsers, &t.DistinctDays, &t.DistinctMerchants, &t.Matches, &t.Negatives,
		&t.RequiresReview, &t.ReviewReason, &t.FirstSeenAt, &t.LastSeenAt, &t.PromotedAt, &t.ReviewedAt)
	return &t, err
}

func (r *Repository) ListTerms(ctx context.Context, f TermFilter) ([]TermRow, error) {
	db := dbx.GetDB(ctx, r.db)
	where := []string{"1 = 1"}
	args := []interface{}{}
	add := func(cond string, v interface{}) {
		where = append(where, cond)
		args = append(args, v)
	}
	if f.Status != "" {
		add("t.status = ?", f.Status)
	}
	if f.Source != "" {
		add("t.source = ?", f.Source)
	}
	if f.IntentKey != "" {
		add("t.intent_key = ?", f.IntentKey)
	}
	if f.MerchantID != "" {
		add("t.merchant_id = ?", f.MerchantID)
	}
	if f.Review != nil {
		add("t.requires_review = ?", *f.Review)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := db.QueryContext(ctx, termSelect+` WHERE `+strings.Join(where, " AND ")+`
		ORDER BY t.requires_review DESC, t.last_seen_at DESC NULLS LAST, t.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TermRow{}
	for rows.Next() {
		t, err := scanTerm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *Repository) getTerm(ctx context.Context, id int64) (*TermRow, error) {
	t, err := scanTerm(dbx.GetDB(ctx, r.db).QueryRowContext(ctx, termSelect+` WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	return t, err
}

// UpdateTerm : décision humaine. Valider ou rejeter lève le drapeau de revue ;
// passer en global déplace le terme (fusion si un terme global identique existe).
func (r *Repository) UpdateTerm(ctx context.Context, id int64, req *UpdateTermRequest, reviewer string) (*TermRow, error) {
	if _, err := r.getTerm(ctx, id); err != nil {
		return nil, err
	}
	sets := []string{"reviewed_at = now()", "reviewed_by = ?", "updated_at = now()"}
	args := []interface{}{reviewer}
	if req.Status != nil {
		sets = append(sets, "status = ?", "requires_review = false", "review_reason = NULL")
		args = append(args, *req.Status)
		if *req.Status == "active" {
			sets = append(sets, "promoted_at = COALESCE(promoted_at, now())")
		}
	}
	if req.IntentKey != nil {
		sets = append(sets, "intent_key = ?")
		args = append(args, *req.IntentKey)
	}
	if req.Weight != nil {
		sets = append(sets, "weight = ?")
		args = append(args, *req.Weight)
	}
	if req.Scope != nil && *req.Scope == "global" {
		sets = append(sets, "merchant_id = ''")
	}
	args = append(args, id)
	_, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `UPDATE assistant_terms SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if dbx.IsDuplicateEntry(err) {
		return nil, models.ErrInvalidInput // même texte déjà présent pour cette intention et cette portée
	}
	if dbx.IsForeignKeyViolation(err) {
		return nil, models.ErrNotFound // intention inconnue
	}
	if err != nil {
		return nil, err
	}
	return r.getTerm(ctx, id)
}

func (r *Repository) CreateTerm(ctx context.Context, intentKey, text string, weight float64, merchantID, reviewer string) (*TermRow, error) {
	kind := "keyword"
	if strings.Contains(text, " ") {
		kind = "phrase"
	}
	var id int64
	err := dbx.GetDB(ctx, r.db).QueryRowContext(ctx, `INSERT INTO assistant_terms
			(intent_key, text, kind, weight, source, status, merchant_id, reviewed_at, reviewed_by)
		VALUES (?, ?, ?, ?, 'manual', 'validated', ?, now(), ?)
		ON CONFLICT (intent_key, text, merchant_id) DO UPDATE SET
			status = 'validated', weight = EXCLUDED.weight, requires_review = false, review_reason = NULL,
			reviewed_at = now(), reviewed_by = EXCLUDED.reviewed_by, updated_at = now()
		RETURNING id`, intentKey, text, kind, weight, merchantID, reviewer).Scan(&id)
	if dbx.IsForeignKeyViolation(err) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.getTerm(ctx, id)
}

func (r *Repository) ListAliases(ctx context.Context, status, merchantID string, limit int) ([]AliasRow, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if merchantID != "" {
		where = append(where, "merchant_id = ?")
		args = append(args, merchantID)
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `SELECT id, merchant_id, entity_type, entity_id, alias, source, status,
			occurrences, distinct_users, requires_review, review_reason, last_seen_at
		FROM assistant_entity_aliases WHERE `+strings.Join(where, " AND ")+`
		ORDER BY requires_review DESC, last_seen_at DESC NULLS LAST LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AliasRow{}
	for rows.Next() {
		var a AliasRow
		if err := rows.Scan(&a.ID, &a.MerchantID, &a.EntityType, &a.EntityID, &a.Alias, &a.Source, &a.Status,
			&a.Occurrences, &a.DistinctUsers, &a.RequiresReview, &a.ReviewReason, &a.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateAlias(ctx context.Context, id int64, status, reviewer string) (*AliasRow, error) {
	res, err := dbx.GetDB(ctx, r.db).ExecContext(ctx, `UPDATE assistant_entity_aliases SET status = ?, requires_review = false,
			review_reason = NULL, reviewed_at = now(), reviewed_by = ?, updated_at = now() WHERE id = ?`, status, reviewer, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, models.ErrNotFound
	}
	var a AliasRow
	err = dbx.GetDB(ctx, r.db).QueryRowContext(ctx, `SELECT id, merchant_id, entity_type, entity_id, alias, source, status,
			occurrences, distinct_users, requires_review, review_reason, last_seen_at
		FROM assistant_entity_aliases WHERE id = ?`, id).Scan(&a.ID, &a.MerchantID, &a.EntityType, &a.EntityID, &a.Alias,
		&a.Source, &a.Status, &a.Occurrences, &a.DistinctUsers, &a.RequiresReview, &a.ReviewReason, &a.LastSeenAt)
	return &a, err
}

type IntentRow struct {
	ModelIntent
	Enabled      bool `json:"enabled"`
	TermsCount   int  `json:"terms_count"`
	LearnedCount int  `json:"learned_count"`
}

func (r *Repository) ListIntents(ctx context.Context) ([]IntentRow, error) {
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `SELECT i.intent_key, i.label, i.kind, i.action, i.permission, i.sensitive,
			i.enabled, array_to_string(i.surfaces, ','), i.priority,
			COUNT(t.id) FILTER (WHERE t.status IN ('active', 'validated')),
			COUNT(t.id) FILTER (WHERE t.source = 'learned')
		FROM assistant_intents i LEFT JOIN assistant_terms t ON t.intent_key = i.intent_key
		GROUP BY i.intent_key ORDER BY i.kind, i.intent_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IntentRow{}
	for rows.Next() {
		var it IntentRow
		var surfaces string
		if err := rows.Scan(&it.Key, &it.Label, &it.Kind, &it.Action, &it.Permission, &it.Sensitive, &it.Enabled,
			&surfaces, &it.Priority, &it.TermsCount, &it.LearnedCount); err != nil {
			return nil, err
		}
		it.Surfaces = strings.Split(surfaces, ",")
		out = append(out, it)
	}
	return out, rows.Err()
}

// FallbackGroups : requêtes incomprises, regroupées par mots significatifs
// (« plus de saumon » et « saumon plus » ensemble), avec ce que les gens ont
// choisi ensuite. C'est la liste de travail de l'amélioration du modèle.
func (r *Repository) FallbackGroups(ctx context.Context, from, to time.Time, limit int) ([]FallbackGroup, error) {
	db := dbx.GetDB(ctx, r.db)
	lex, err := r.LoadLexicon(ctx)
	if err != nil {
		return nil, err
	}
	stop := map[string]bool{}
	for _, w := range lex.Stopwords {
		stop[w] = true
	}

	rows, err := db.QueryContext(ctx, `SELECT f.query_norm, COUNT(*), COUNT(DISTINCT f.user_id), COUNT(DISTINCT f.merchant_id),
			MAX(f.created_at), MIN(f.data->>'text'),
			COUNT(*) FILTER (WHERE NOT EXISTS (
				SELECT 1 FROM assistant_events s WHERE s.turn_id = f.turn_id AND s.type = 'suggestion_clicked'))
		FROM assistant_events f
		WHERE f.type = 'fallback' AND f.query_norm IS NOT NULL AND f.created_at >= ? AND f.created_at < ?
		GROUP BY f.query_norm
		ORDER BY COUNT(*) DESC
		LIMIT 1000`, from, to)
	if err != nil {
		return nil, err
	}
	groups := map[string]*FallbackGroup{}
	keyOf := map[string]string{}
	for rows.Next() {
		var g FallbackGroup
		var example sql.NullString
		if err := rows.Scan(&g.QueryNorm, &g.Count, &g.Users, &g.Merchants, &g.LastSeenAt, &example, &g.Abandoned); err != nil {
			rows.Close()
			return nil, err
		}
		key := groupKey(g.QueryNorm, stop)
		keyOf[g.QueryNorm] = key
		if cur, ok := groups[key]; ok {
			cur.Count += g.Count
			cur.Users += g.Users // approximation : un même utilisateur peut compter deux fois
			cur.Abandoned += g.Abandoned
			if g.LastSeenAt.After(cur.LastSeenAt) {
				cur.LastSeenAt = g.LastSeenAt
			}
			continue
		}
		g.Example = example.String
		g.ChosenAfter = []ChosenAfterCount{}
		groups[key] = &g
	}
	rows.Close()

	crows, err := db.QueryContext(ctx, `SELECT f.query_norm, s.intent_key, COUNT(*)
		FROM assistant_events f
		JOIN assistant_events s ON s.turn_id = f.turn_id AND s.type = 'suggestion_clicked' AND s.intent_key IS NOT NULL
		WHERE f.type = 'fallback' AND f.query_norm IS NOT NULL AND f.created_at >= ? AND f.created_at < ?
		GROUP BY f.query_norm, s.intent_key`, from, to)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var qn, intent string
		var n int
		if err := crows.Scan(&qn, &intent, &n); err != nil {
			return nil, err
		}
		g, ok := groups[keyOf[qn]]
		if !ok {
			continue
		}
		merged := false
		for i := range g.ChosenAfter {
			if g.ChosenAfter[i].IntentKey == intent {
				g.ChosenAfter[i].Count += n
				merged = true
			}
		}
		if !merged {
			g.ChosenAfter = append(g.ChosenAfter, ChosenAfterCount{IntentKey: intent, Count: n})
		}
	}

	out := make([]FallbackGroup, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g.ChosenAfter, func(i, j int) bool { return g.ChosenAfter[i].Count > g.ChosenAfter[j].Count })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// groupKey : mots significatifs triés, sans doublon.
func groupKey(queryNorm string, stop map[string]bool) string {
	seen := map[string]bool{}
	words := []string{}
	for _, t := range Tokens(queryNorm) {
		if stop[t] || seen[t] {
			continue
		}
		seen[t] = true
		words = append(words, t)
	}
	if len(words) == 0 {
		return queryNorm
	}
	sort.Strings(words)
	return strings.Join(words, " ")
}

// Stats : indicateurs d'usage sur une période, tous établissements ou un seul.
func (r *Repository) Stats(ctx context.Context, from, to time.Time, merchantID string) (*Stats, error) {
	db := dbx.GetDB(ctx, r.db)
	st := &Stats{
		From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
		Outcomes: map[string]int{}, Devices: map[string]int{}, TopIntents: []IntentCount{}, ByWeek: []WeekPoint{},
	}
	scope := ""
	args := []interface{}{from, to}
	if merchantID != "" {
		scope = " AND merchant_id = ?"
		args = append(args, merchantID)
	}

	var successful, engaged int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT user_id), COUNT(DISTINCT merchant_id),
			COALESCE(SUM(queries), 0), COALESCE(SUM(voice_queries), 0), COALESCE(SUM(chips_clicked), 0),
			COALESCE(SUM(understood), 0), COALESCE(SUM(ambiguous), 0), COALESCE(SUM(fallbacks), 0),
			COALESCE(SUM(actions_succeeded), 0), COALESCE(SUM(actions_failed), 0),
			COUNT(*) FILTER (WHERE outcome IN ('action', 'info', 'navigate')),
			COUNT(*) FILTER (WHERE queries > 0 OR chips_clicked > 0),
			percentile_cont(0.5) WITHIN GROUP (ORDER BY time_to_action_ms),
			percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms)
		FROM assistant_sessions WHERE started_at >= ? AND started_at < ?`+scope, args...).Scan(
		&st.Sessions, &st.Users, &st.Merchants, &st.Queries, &st.VoiceQueries, &st.ChipsClicked,
		&st.Understood, &st.Ambiguous, &st.Fallbacks, &st.ActionsSucceeded, &st.ActionsFailed,
		&successful, &engaged, &st.MedianTimeToActionM, &st.MedianSessionMs)
	if err != nil {
		return nil, err
	}
	if total := st.Understood + st.Ambiguous + st.Fallbacks; total > 0 {
		st.UnderstandingRate = round3(float64(st.Understood) / float64(total))
	}
	if engaged > 0 {
		st.SuccessRate = round3(float64(successful) / float64(engaged))
	}

	if err := groupCount(ctx, db, `SELECT COALESCE(outcome, 'open'), COUNT(*) FROM assistant_sessions
		WHERE started_at >= ? AND started_at < ?`+scope+` GROUP BY 1`, args, st.Outcomes); err != nil {
		return nil, err
	}
	if err := groupCount(ctx, db, `SELECT COALESCE(device, 'unknown'), COUNT(*) FROM assistant_sessions
		WHERE started_at >= ? AND started_at < ?`+scope+` GROUP BY 1`, args, st.Devices); err != nil {
		return nil, err
	}

	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE type = 'cancelled'), COUNT(*) FILTER (WHERE type = 'rephrased')
		FROM assistant_events WHERE created_at >= ? AND created_at < ?`+scope, args...).Scan(&st.Cancellations, &st.Rephrases)

	irows, err := db.QueryContext(ctx, `SELECT intent_key,
			COUNT(*) FILTER (WHERE type IN ('intent_matched', 'suggestion_clicked', 'chip_clicked')),
			COUNT(*) FILTER (WHERE type = 'action_succeeded'),
			COUNT(*) FILTER (WHERE type = 'cancelled'),
			COALESCE(AVG(score) FILTER (WHERE type = 'intent_matched'), 0)
		FROM assistant_events
		WHERE intent_key IS NOT NULL AND created_at >= ? AND created_at < ?`+scope+`
		GROUP BY intent_key ORDER BY 2 DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	for irows.Next() {
		var ic IntentCount
		if err := irows.Scan(&ic.IntentKey, &ic.Count, &ic.Succeeded, &ic.Cancelled, &ic.AvgScore); err != nil {
			irows.Close()
			return nil, err
		}
		ic.AvgScore = round3(ic.AvgScore)
		st.TopIntents = append(st.TopIntents, ic)
	}
	irows.Close()

	wrows, err := db.QueryContext(ctx, `SELECT to_char(date_trunc('week', created_at), 'YYYY-MM-DD'),
			COUNT(*) FILTER (WHERE type = 'query_submitted'),
			COUNT(*) FILTER (WHERE type = 'intent_matched'),
			COUNT(*) FILTER (WHERE type IN ('intent_matched', 'intent_ambiguous', 'fallback'))
		FROM assistant_events WHERE created_at >= ? AND created_at < ?`+scope+`
		GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return nil, err
	}
	for wrows.Next() {
		var w WeekPoint
		var matched, answered int
		if err := wrows.Scan(&w.Week, &w.Queries, &matched, &answered); err != nil {
			wrows.Close()
			return nil, err
		}
		if answered > 0 {
			w.UnderstandingRate = round3(float64(matched) / float64(answered))
		}
		st.ByWeek = append(st.ByWeek, w)
	}
	wrows.Close()

	_ = db.QueryRowContext(ctx, `SELECT
			COUNT(*) FILTER (WHERE source = 'learned' AND status = 'active'),
			COUNT(*) FILTER (WHERE source = 'learned' AND status = 'candidate'),
			COUNT(*) FILTER (WHERE requires_review)
		FROM assistant_terms`).Scan(&st.LearnedActive, &st.LearnedCandidates, &st.PendingReview)
	return st, nil
}

func groupCount(ctx context.Context, db *dbx.DB, q string, args []interface{}, into map[string]int) error {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		into[k] = n
	}
	return rows.Err()
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

// SessionEvents : rejeu chronologique d'une session.
func (r *Repository) SessionEvents(ctx context.Context, sessionID string) ([]SessionEventRow, error) {
	rows, err := dbx.GetDB(ctx, r.db).QueryContext(ctx, `SELECT seq, type, client_ts, elapsed_ms, intent_key, score, data::text
		FROM assistant_events WHERE session_id = ? ORDER BY seq NULLS LAST, client_ts`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionEventRow{}
	for rows.Next() {
		var e SessionEventRow
		var data string
		if err := rows.Scan(&e.Seq, &e.Type, &e.ClientTS, &e.ElapsedMs, &e.IntentKey, &e.Score, &data); err != nil {
			return nil, err
		}
		e.Data = map[string]interface{}{}
		_ = json.Unmarshal([]byte(data), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}
