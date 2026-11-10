package assistant

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/models"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// ─── Modèle ────────────────────────────────────────────────────────────────

// Fingerprint résume tout ce qui compose le modèle d'un établissement. Les
// compteurs recalculés chaque heure ne touchent pas updated_at : seule une
// vraie modification (statut, poids, texte) change la version.
func (r *Repository) Fingerprint(ctx context.Context, surface, merchantID string) (string, error) {
	db := dbx.GetDB(ctx, r.db)
	var parts [7]string
	err := db.QueryRowContext(ctx, `SELECT
		(SELECT COALESCE(MAX(updated_at)::text, '') FROM assistant_intents),
		(SELECT COUNT(*)::text FROM assistant_intents WHERE enabled AND ? = ANY(surfaces)),
		(SELECT COALESCE(MAX(updated_at)::text, '') FROM assistant_terms WHERE merchant_id IN ('', ?)),
		(SELECT COUNT(*)::text FROM assistant_terms WHERE merchant_id IN ('', ?) AND status IN ('active', 'validated')),
		(SELECT COALESCE(MAX(updated_at)::text, '') FROM assistant_entity_aliases WHERE merchant_id = ?),
		(SELECT COUNT(*)::text FROM assistant_entity_aliases WHERE merchant_id = ? AND status IN ('active', 'validated')),
		(SELECT COALESCE(MAX(updated_at)::text, '') || ':' || COUNT(*)::text FROM assistant_lexicon)`,
		surface, merchantID, merchantID, merchantID, merchantID,
	).Scan(&parts[0], &parts[1], &parts[2], &parts[3], &parts[4], &parts[5], &parts[6])
	if err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%d|%s|%s", ModelSchema, surface, strings.Join(parts[:], "|"))))
	return "v1-" + hex.EncodeToString(sum[:])[:16], nil
}

func (r *Repository) LoadIntents(ctx context.Context, surface string) ([]ModelIntent, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `SELECT intent_key, label, description, kind, action, params::text, slots::text,
			permission, module, confirm, sensitive, array_to_string(surfaces, ','), examples::text, priority
		FROM assistant_intents
		WHERE enabled AND ? = ANY(surfaces)
		ORDER BY priority DESC, intent_key`, surface)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ModelIntent, 0, 64)
	for rows.Next() {
		var it ModelIntent
		var params, slots, examples, surfaces string
		if err := rows.Scan(&it.Key, &it.Label, &it.Description, &it.Kind, &it.Action, &params, &slots,
			&it.Permission, &it.Module, &it.Confirm, &it.Sensitive, &surfaces, &examples, &it.Priority); err != nil {
			return nil, err
		}
		it.Params = map[string]interface{}{}
		it.Slots = []interface{}{}
		it.Examples = []string{}
		_ = json.Unmarshal([]byte(params), &it.Params)
		_ = json.Unmarshal([]byte(slots), &it.Slots)
		_ = json.Unmarshal([]byte(examples), &it.Examples)
		it.Surfaces = strings.Split(surfaces, ",")
		it.Terms = []ModelTerm{}
		out = append(out, it)
	}
	return out, rows.Err()
}

// LoadTerms renvoie les termes servis (actifs ou validés), globaux puis
// propres à l'établissement, regroupés par intention.
func (r *Repository) LoadTerms(ctx context.Context, merchantID string) (map[string][]ModelTerm, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `SELECT id, intent_key, text, kind, weight, source, status
		FROM assistant_terms
		WHERE status IN ('active', 'validated') AND merchant_id IN ('', ?)
		ORDER BY intent_key, merchant_id, id`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]ModelTerm{}
	for rows.Next() {
		var key string
		var t ModelTerm
		if err := rows.Scan(&t.ID, &key, &t.Text, &t.Kind, &t.Weight, &t.Source, &t.Status); err != nil {
			return nil, err
		}
		// Un terme appris pas encore validé par un humain compte moins.
		if t.Source == "learned" && t.Status == "active" {
			t.Weight *= learnedWeightFactor
		}
		out[key] = append(out[key], t)
	}
	return out, rows.Err()
}

func (r *Repository) LoadAliases(ctx context.Context, merchantID string) ([]ModelAlias, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `SELECT id, entity_type, entity_id, alias
		FROM assistant_entity_aliases
		WHERE merchant_id = ? AND status IN ('active', 'validated')
		ORDER BY id`, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelAlias{}
	for rows.Next() {
		var a ModelAlias
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Alias); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type Lexicon struct {
	Stopwords []string
	Hints     map[string][]string
	Blacklist map[string]bool
}

func (r *Repository) LoadLexicon(ctx context.Context) (*Lexicon, error) {
	db := dbx.GetDB(ctx, r.db)
	rows, err := db.QueryContext(ctx, `SELECT kind, group_key, text FROM assistant_lexicon ORDER BY kind, group_key, text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lex := &Lexicon{Stopwords: []string{}, Hints: map[string][]string{}, Blacklist: map[string]bool{}}
	for rows.Next() {
		var kind, group, text string
		if err := rows.Scan(&kind, &group, &text); err != nil {
			return nil, err
		}
		switch kind {
		case "stopword":
			lex.Stopwords = append(lex.Stopwords, text)
		case "hint":
			lex.Hints[group] = append(lex.Hints[group], text)
		case "blacklist":
			lex.Blacklist[text] = true
		}
	}
	return lex, rows.Err()
}

type intentFlags struct {
	Enabled   bool
	Sensitive bool
}

func (r *Repository) IntentFlags(ctx context.Context, keys []string) (map[string]intentFlags, error) {
	out := map[string]intentFlags{}
	if len(keys) == 0 {
		return out, nil
	}
	db := dbx.GetDB(ctx, r.db)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	args := make([]interface{}, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	rows, err := db.QueryContext(ctx, `SELECT intent_key, enabled, sensitive FROM assistant_intents WHERE intent_key IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var f intentFlags
		if err := rows.Scan(&k, &f.Enabled, &f.Sensitive); err != nil {
			return nil, err
		}
		out[k] = f
	}
	return out, rows.Err()
}

// ─── Événements ────────────────────────────────────────────────────────────

type sessionRow struct {
	SessionInfo
	MerchantID string
	UserID     string
	RoleKey    *string
	StartedAt  time.Time
}

type eventRow struct {
	TrackedEvent
	ClientTS  time.Time
	ElapsedMs *int
	QueryNorm *string
	DataJSON  string
}

// sessionDelta : compteurs à ajouter à la session pour les événements NOUVEAUX.
type sessionDelta struct {
	Events, Queries, Voice, Understood, Ambiguous, Fallbacks, Chips, Succeeded, Failed int
	LastEventAt                                                                        time.Time
	EndedAt                                                                            *time.Time
	DurationMs                                                                         *int
	Outcome                                                                            *string
	TimeToActionMs                                                                     *int
}

// tx exécute fn dans une transaction (requêtes ré-écrites pour le dialecte).
func (r *Repository) tx(ctx context.Context, fn func(q *dbx.DB) error) error {
	t, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(dbx.Wrap(t)); err != nil {
		_ = t.Rollback()
		return err
	}
	return t.Commit()
}

// UpsertSession crée la session si besoin. Refuse une session existante
// appartenant à un autre utilisateur (identifiant forgé ou réutilisé).
func (r *Repository) UpsertSession(ctx context.Context, q *dbx.DB, s sessionRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO assistant_sessions
		(id, merchant_id, user_id, role_key, surface, origin_path, device, platform, app_version, model_version, started_at, offline_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			model_version = EXCLUDED.model_version,
			offline_seen  = assistant_sessions.offline_seen OR EXCLUDED.offline_seen,
			updated_at    = now()
		WHERE assistant_sessions.merchant_id = EXCLUDED.merchant_id AND assistant_sessions.user_id = EXCLUDED.user_id`,
		s.ID, s.MerchantID, s.UserID, s.RoleKey, s.Surface, nullIfEmpty(s.OriginPath), nullIfEmpty(s.Device),
		nullIfEmpty(s.Platform), nullIfEmpty(s.AppVersion), nullIfEmpty(s.ModelVersion), s.StartedAt, !s.Online,
	)
	if err != nil {
		return err
	}
	var merchantID, userID string
	if err := q.QueryRowContext(ctx, `SELECT merchant_id, user_id FROM assistant_sessions WHERE id = ?`, s.ID).Scan(&merchantID, &userID); err != nil {
		return err
	}
	if merchantID != s.MerchantID || userID != s.UserID {
		return models.ErrForbidden
	}
	return nil
}

// InsertEvents insère les événements et renvoie les identifiants réellement
// nouveaux (un lot renvoyé après une coupure réseau est ignoré).
func (r *Repository) InsertEvents(ctx context.Context, q *dbx.DB, sessionID, merchantID, userID string, events []eventRow) (map[string]bool, error) {
	inserted := map[string]bool{}
	if len(events) == 0 {
		return inserted, nil
	}
	var sb strings.Builder
	args := make([]interface{}, 0, len(events)*13)
	sb.WriteString(`INSERT INTO assistant_events
		(event_id, session_id, merchant_id, user_id, type, seq, turn_id, client_ts, elapsed_ms, intent_key, score, query_norm, data)
		VALUES `)
	for i, e := range events {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb)")
		args = append(args, e.EventID, sessionID, merchantID, userID, e.Type, e.Seq, nullIfEmpty(e.TurnID), e.ClientTS,
			e.ElapsedMs, nullIfEmpty(e.IntentKey), e.Score, e.QueryNorm, e.DataJSON)
	}
	sb.WriteString(" ON CONFLICT (event_id) DO NOTHING RETURNING event_id::text")
	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		inserted[id] = true
	}
	return inserted, rows.Err()
}

func (r *Repository) ApplySessionDelta(ctx context.Context, q *dbx.DB, sessionID string, d sessionDelta) error {
	_, err := q.ExecContext(ctx, `UPDATE assistant_sessions SET
			events_count      = events_count + ?,
			queries           = queries + ?,
			voice_queries     = voice_queries + ?,
			understood        = understood + ?,
			ambiguous         = ambiguous + ?,
			fallbacks         = fallbacks + ?,
			chips_clicked     = chips_clicked + ?,
			actions_succeeded = actions_succeeded + ?,
			actions_failed    = actions_failed + ?,
			last_event_at     = GREATEST(COALESCE(last_event_at, ?), ?),
			ended_at          = COALESCE(?, ended_at),
			duration_ms       = COALESCE(?, duration_ms),
			outcome           = COALESCE(?, outcome),
			time_to_action_ms = COALESCE(time_to_action_ms, ?),
			updated_at        = now()
		WHERE id = ?`,
		d.Events, d.Queries, d.Voice, d.Understood, d.Ambiguous, d.Fallbacks, d.Chips, d.Succeeded, d.Failed,
		d.LastEventAt, d.LastEventAt, d.EndedAt, d.DurationMs, d.Outcome, d.TimeToActionMs, sessionID,
	)
	return err
}

// UpsertLearnedTerm enregistre un synonyme candidat (propre à l'établissement).
// Un terme existant (pré-rempli, validé, rejeté) garde son statut.
func (r *Repository) UpsertLearnedTerm(ctx context.Context, q *dbx.DB, intentKey, text, kind, merchantID string, review *string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO assistant_terms
			(intent_key, text, kind, weight, source, status, merchant_id, first_seen_at, last_seen_at, requires_review, review_reason)
		VALUES (?, ?, ?, 1, 'learned', 'candidate', ?, now(), now(), ?, ?)
		ON CONFLICT (intent_key, text, merchant_id) DO UPDATE SET last_seen_at = now()
		RETURNING id`,
		intentKey, text, kind, merchantID, review != nil, review,
	).Scan(&id)
	return id, err
}

func (r *Repository) UpsertLearnedAlias(ctx context.Context, q *dbx.DB, merchantID, entityType, entityID, alias string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO assistant_entity_aliases
			(merchant_id, entity_type, entity_id, alias, source, status, first_seen_at, last_seen_at)
		VALUES (?, ?, ?, ?, 'learned', 'candidate', now(), now())
		ON CONFLICT (merchant_id, entity_type, alias, entity_id) DO UPDATE SET last_seen_at = now()
		RETURNING id`,
		merchantID, entityType, entityID, alias,
	).Scan(&id)
	return id, err
}

type signalRow struct {
	TargetType string
	TargetID   int64
	Kind       string
	EventID    string
}

// InsertSignals n'enregistre que les cibles qui existent et appartiennent à
// l'établissement (ou sont globales) : un client ne peut pas faire monter le
// compteur d'un terme d'un autre établissement.
func (r *Repository) InsertSignals(ctx context.Context, q *dbx.DB, merchantID, userID string, signals []signalRow) error {
	for _, s := range signals {
		var guard string
		if s.TargetType == "term" {
			guard = `EXISTS (SELECT 1 FROM assistant_terms WHERE id = ? AND merchant_id IN ('', ?))`
		} else {
			guard = `EXISTS (SELECT 1 FROM assistant_entity_aliases WHERE id = ? AND merchant_id = ?)`
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO assistant_learning_signals (target_type, target_id, kind, merchant_id, user_id, event_id)
			SELECT ?::varchar, ?::bigint, ?::varchar, ?::varchar, ?::varchar, ?::uuid WHERE `+guard+`
			ON CONFLICT (event_id, target_type, target_id, kind) DO NOTHING`,
			s.TargetType, s.TargetID, s.Kind, merchantID, userID, s.EventID, s.TargetID, merchantID,
		); err != nil {
			return err
		}
	}
	return nil
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
