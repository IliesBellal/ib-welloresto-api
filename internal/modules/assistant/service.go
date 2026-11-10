package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx"
	"welloresto-api/internal/logger"
	"welloresto-api/internal/middleware"
	"welloresto-api/internal/models"
	"welloresto-api/internal/modules/auth"
)

type Service struct {
	repo *Repository

	mu    sync.Mutex
	cache map[string]cachedGlobal // par surface
}

// cachedGlobal : partie commune à tous les établissements (intentions,
// lexique), gardée 60 s pour une version donnée.
type cachedGlobal struct {
	at      time.Time
	intents []ModelIntent
	lexicon *Lexicon
}

const globalCacheTTL = 60 * time.Second

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, cache: map[string]cachedGlobal{}}
}

// isMissingTable : migrations 206/207 pas encore appliquées.
func isMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

// ─── Modèle ────────────────────────────────────────────────────────────────

// GetModel renvoie le modèle de l'établissement de l'utilisateur, ou
// `unchanged` si le client a déjà cette version. Les intentions ne sont pas
// filtrées par droits : le client le fait (la version reste commune à tous les
// rôles d'un établissement) et chaque action reste protégée par sa propre route.
func (s *Service) GetModel(ctx context.Context, surface, knownVersion string) (*ModelResponse, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !allowedSurfaces[surface] {
		return nil, models.ErrInvalidInput
	}

	version, err := s.repo.Fingerprint(ctx, surface, user.MerchantID)
	if err != nil {
		if isMissingTable(err) {
			return &ModelResponse{Model: emptyModel(surface)}, nil
		}
		return nil, err
	}
	if knownVersion != "" && knownVersion == version {
		return &ModelResponse{Unchanged: true, Version: version}, nil
	}

	global, err := s.global(ctx, surface)
	if err != nil {
		return nil, err
	}
	terms, err := s.repo.LoadTerms(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}
	aliases, err := s.repo.LoadAliases(ctx, user.MerchantID)
	if err != nil {
		return nil, err
	}

	intents := make([]ModelIntent, len(global.intents))
	for i, it := range global.intents {
		it.Terms = append([]ModelTerm{}, terms[it.Key]...)
		intents[i] = it
	}
	hints := map[string][]string{}
	for k, v := range global.lexicon.Hints {
		hints[k] = v
	}
	return &ModelResponse{Model: &Model{
		Schema:      ModelSchema,
		Version:     version,
		Surface:     surface,
		GeneratedAt: time.Now().UTC(),
		Thresholds:  DefaultThresholds,
		Stopwords:   global.lexicon.Stopwords,
		Hints:       hints,
		Intents:     intents,
		Aliases:     aliases,
	}}, nil
}

func (s *Service) global(ctx context.Context, surface string) (cachedGlobal, error) {
	s.mu.Lock()
	c, ok := s.cache[surface]
	s.mu.Unlock()
	if ok && time.Since(c.at) < globalCacheTTL {
		return c, nil
	}
	intents, err := s.repo.LoadIntents(ctx, surface)
	if err != nil {
		return cachedGlobal{}, err
	}
	lex, err := s.repo.LoadLexicon(ctx)
	if err != nil {
		return cachedGlobal{}, err
	}
	c = cachedGlobal{at: time.Now(), intents: intents, lexicon: lex}
	s.mu.Lock()
	s.cache[surface] = c
	s.mu.Unlock()
	return c, nil
}

// invalidate : après une modification d'administration.
func (s *Service) invalidate() {
	s.mu.Lock()
	s.cache = map[string]cachedGlobal{}
	s.mu.Unlock()
}

func emptyModel(surface string) *Model {
	return &Model{
		Schema: ModelSchema, Version: "empty", Surface: surface, GeneratedAt: time.Now().UTC(),
		Thresholds: DefaultThresholds, Stopwords: []string{}, Hints: map[string][]string{},
		Intents: []ModelIntent{}, Aliases: []ModelAlias{},
	}
}

// ─── Événements ────────────────────────────────────────────────────────────

var allowedOutcomes = map[string]bool{"action": true, "info": true, "navigate": true, "fallback": true, "abandon": true}
var allowedEntityTypes = map[string]bool{"product": true, "employee": true, "customer": true}

const (
	maxLearnedTermTokens  = 4
	maxLearnedAliasTokens = 5
	maxTextLen            = 120
)

// validateBatch contrôle la forme du lot avant toute requête.
func validateBatch(b *EventBatch) error {
	if _, err := uuid.Parse(b.Session.ID); err != nil {
		return models.ErrInvalidInput
	}
	if !allowedSurfaces[b.Session.Surface] {
		return models.ErrInvalidInput
	}
	if b.Session.Device != "" && !allowedDevices[b.Session.Device] {
		b.Session.Device = ""
	}
	if len(b.Events) == 0 || len(b.Events) > maxEventsPerBatch {
		return models.ErrInvalidInput
	}
	for i := range b.Events {
		e := &b.Events[i]
		if _, err := uuid.Parse(e.EventID); err != nil {
			return models.ErrInvalidInput
		}
		if !allowedEventTypes[e.Type] || e.TS <= 0 {
			return models.ErrInvalidInput
		}
		if e.TurnID != "" {
			if _, err := uuid.Parse(e.TurnID); err != nil {
				e.TurnID = ""
			}
		}
		if len(e.IntentKey) > 96 {
			e.IntentKey = e.IntentKey[:96]
		}
	}
	b.Session.OriginPath = truncate(b.Session.OriginPath, 255)
	b.Session.Platform = truncate(b.Session.Platform, 32)
	b.Session.AppVersion = truncate(b.Session.AppVersion, 32)
	b.Session.ModelVersion = truncate(b.Session.ModelVersion, 64)
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func clampTime(ms int64, now time.Time) time.Time {
	t := time.UnixMilli(ms)
	if t.After(now.Add(maxClockSkew)) || t.Before(now.Add(-maxClockSkew*30)) {
		return now
	}
	return t
}

// RecordEvents enregistre un lot. Le marchand, l'utilisateur et le rôle
// viennent du jeton, jamais du client.
func (s *Service) RecordEvents(ctx context.Context, batch *EventBatch) error {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return err
	}
	if err := validateBatch(batch); err != nil {
		return err
	}
	now := time.Now()
	startedAt := clampTime(batch.Session.StartedAt, now)

	rows := make([]eventRow, 0, len(batch.Events))
	for _, e := range batch.Events {
		data := e.Data
		if data == nil {
			data = map[string]interface{}{}
		}
		raw, err := json.Marshal(data)
		if err != nil || len(raw) > maxDataBytes {
			raw = []byte(`{"truncated":true}`)
		}
		row := eventRow{TrackedEvent: e, ClientTS: clampTime(e.TS, now), DataJSON: string(raw)}
		if elapsed := row.ClientTS.Sub(startedAt).Milliseconds(); elapsed >= 0 && elapsed < int64(maxClockSkew/time.Millisecond) {
			v := int(elapsed)
			row.ElapsedMs = &v
		}
		if qn, ok := data["query_norm"].(string); ok {
			if n := truncate(Normalize(qn), 255); n != "" {
				row.QueryNorm = &n
			}
		}
		rows = append(rows, row)
	}

	session := sessionRow{
		SessionInfo: batch.Session,
		MerchantID:  user.MerchantID,
		UserID:      user.UserID,
		RoleKey:     roleKey(user),
		StartedAt:   startedAt,
	}

	err = s.repo.tx(ctx, func(q *dbx.DB) error {
		if err := s.repo.UpsertSession(ctx, q, session); err != nil {
			return err
		}
		inserted, err := s.repo.InsertEvents(ctx, q, session.ID, user.MerchantID, user.UserID, rows)
		if err != nil {
			return err
		}
		fresh := make([]eventRow, 0, len(inserted))
		for _, r := range rows {
			if inserted[r.EventID] {
				fresh = append(fresh, r)
			}
		}
		if len(fresh) == 0 {
			return nil
		}
		if err := s.repo.ApplySessionDelta(ctx, q, session.ID, computeDelta(fresh)); err != nil {
			return err
		}
		return s.recordLearning(ctx, q, user.MerchantID, user.UserID, fresh)
	})
	if isMissingTable(err) {
		logger.FromContext(ctx).Warn("assistant: tables absentes (migration 206), événements ignorés")
		return nil
	}
	return err
}

func roleKey(u *auth.UserLoginRow) *string {
	if u.RoleSystemKey != nil && *u.RoleSystemKey != "" {
		return u.RoleSystemKey
	}
	if u.RoleID != nil && *u.RoleID != "" {
		k := "custom:" + *u.RoleID
		return &k
	}
	return nil
}

// computeDelta : compteurs de session apportés par des événements nouveaux.
func computeDelta(events []eventRow) sessionDelta {
	var d sessionDelta
	for _, e := range events {
		d.Events++
		if e.ClientTS.After(d.LastEventAt) {
			d.LastEventAt = e.ClientTS
		}
		switch e.Type {
		case EventQuerySubmitted:
			d.Queries++
			if input, _ := e.Data["input"].(string); input == "voice" {
				d.Voice++
			}
		case EventIntentMatched:
			d.Understood++
		case EventIntentAmbiguous:
			d.Ambiguous++
		case EventFallback:
			d.Fallbacks++
		case EventChipClicked:
			d.Chips++
		case EventActionSucceeded:
			d.Succeeded++
			if e.ElapsedMs != nil && (d.TimeToActionMs == nil || *e.ElapsedMs < *d.TimeToActionMs) {
				v := *e.ElapsedMs
				d.TimeToActionMs = &v
			}
		case EventActionFailed:
			d.Failed++
		case EventClosed:
			t := e.ClientTS
			d.EndedAt = &t
			d.DurationMs = e.ElapsedMs
			if o, _ := e.Data["outcome"].(string); allowedOutcomes[o] {
				d.Outcome = &o
			}
		}
	}
	return d
}

// recordLearning transforme les événements en signaux d'apprentissage.
func (s *Service) recordLearning(ctx context.Context, q *dbx.DB, merchantID, userID string, events []eventRow) error {
	var signals []signalRow
	var learnings []struct {
		eventID string
		sig     LearningSignal
	}

	for _, e := range events {
		switch e.Type {
		case EventIntentMatched:
			signals = append(signals, idSignals(e, "matched")...)
		case EventCancelled, EventRephrased:
			signals = append(signals, idSignals(e, "negative")...)
		case EventActionSucceeded:
			raw, ok := e.Data["learning"]
			if !ok {
				continue
			}
			b, _ := json.Marshal(raw)
			var sig LearningSignal
			if json.Unmarshal(b, &sig) == nil && sig.IntentKey != "" {
				learnings = append(learnings, struct {
					eventID string
					sig     LearningSignal
				}{e.EventID, sig})
			}
		}
	}

	if len(learnings) > 0 {
		keys := make([]string, 0, len(learnings))
		for _, l := range learnings {
			keys = append(keys, l.sig.IntentKey)
		}
		flags, err := s.repo.IntentFlags(ctx, keys)
		if err != nil {
			return err
		}
		lex, err := s.repo.LoadLexicon(ctx)
		if err != nil {
			return err
		}
		for _, l := range learnings {
			f, ok := flags[l.sig.IntentKey]
			if !ok || !f.Enabled {
				continue
			}
			if l.sig.Term != nil {
				if text, kind, ok := cleanLearnedText(l.sig.Term.Text, lex, maxLearnedTermTokens); ok {
					var review *string
					if f.Sensitive {
						r := "sensitive"
						review = &r
					}
					id, err := s.repo.UpsertLearnedTerm(ctx, q, l.sig.IntentKey, text, kind, merchantID, review)
					if err != nil {
						return err
					}
					signals = append(signals, signalRow{TargetType: "term", TargetID: id, Kind: "learned", EventID: l.eventID})
				}
			}
			if a := l.sig.Alias; a != nil && allowedEntityTypes[a.EntityType] && a.EntityID != "" && len(a.EntityID) <= 64 {
				if text, _, ok := cleanLearnedText(a.Text, lex, maxLearnedAliasTokens); ok {
					id, err := s.repo.UpsertLearnedAlias(ctx, q, merchantID, a.EntityType, a.EntityID, text)
					if err != nil {
						return err
					}
					signals = append(signals, signalRow{TargetType: "alias", TargetID: id, Kind: "learned", EventID: l.eventID})
				}
			}
		}
	}
	if len(signals) == 0 {
		return nil
	}
	return s.repo.InsertSignals(ctx, q, merchantID, userID, signals)
}

// idSignals lit data.term_ids et data.alias_ids (termes et surnoms APPRIS qui
// ont servi à comprendre la demande).
func idSignals(e eventRow, kind string) []signalRow {
	var out []signalRow
	for _, field := range []struct{ key, target string }{{"term_ids", "term"}, {"alias_ids", "alias"}} {
		list, _ := e.Data[field.key].([]interface{})
		for i, v := range list {
			if i >= 10 {
				break
			}
			if f, ok := v.(float64); ok && f > 0 {
				out = append(out, signalRow{TargetType: field.target, TargetID: int64(f), Kind: kind, EventID: e.EventID})
			}
		}
	}
	return out
}

// cleanLearnedText normalise et refuse ce qui n'est pas apprenable : vide,
// trop long, uniquement des mots vides ou de la liste noire.
func cleanLearnedText(text string, lex *Lexicon, maxTokens int) (string, string, bool) {
	n := Normalize(text)
	if n == "" || len(n) > maxTextLen {
		return "", "", false
	}
	tokens := Tokens(n)
	stop := map[string]bool{}
	for _, w := range lex.Stopwords {
		stop[w] = true
	}
	significant := 0
	for _, t := range tokens {
		if !stop[t] && !lex.Blacklist[t] {
			significant++
		}
	}
	if significant == 0 || significant > maxTokens || len(tokens) > maxTokens+3 {
		return "", "", false
	}
	kind := "keyword"
	if len(tokens) > 1 {
		kind = "phrase"
	}
	return n, kind, true
}

// ─── Administration ────────────────────────────────────────────────────────

var termStatuses = map[string]bool{"candidate": true, "active": true, "validated": true, "rejected": true}

func (s *Service) UpdateTerm(ctx context.Context, id int64, req *UpdateTermRequest) (*TermRow, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if req.Status != nil && !termStatuses[*req.Status] {
		return nil, models.ErrInvalidInput
	}
	if req.Weight != nil && (*req.Weight <= 0 || *req.Weight > 5) {
		return nil, models.ErrInvalidInput
	}
	if req.Scope != nil && *req.Scope != "global" {
		return nil, models.ErrInvalidInput
	}
	row, err := s.repo.UpdateTerm(ctx, id, req, user.UserID)
	if err == nil {
		s.invalidate()
	}
	return row, err
}

func (s *Service) CreateTerm(ctx context.Context, req *CreateTermRequest) (*TermRow, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	text := Normalize(req.Text)
	if text == "" || len(text) > maxTextLen || strings.TrimSpace(req.IntentKey) == "" {
		return nil, models.ErrInvalidInput
	}
	if req.Weight == 0 {
		req.Weight = 1
		if strings.Contains(text, " ") {
			req.Weight = 1.5
		}
	}
	if req.Weight <= 0 || req.Weight > 5 || len(req.MerchantID) > 64 {
		return nil, models.ErrInvalidInput
	}
	row, err := s.repo.CreateTerm(ctx, req.IntentKey, text, req.Weight, req.MerchantID, user.UserID)
	if err == nil {
		s.invalidate()
	}
	return row, err
}

func (s *Service) UpdateAlias(ctx context.Context, id int64, req *UpdateAliasRequest) (*AliasRow, error) {
	user, err := middleware.UserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if !termStatuses[req.Status] {
		return nil, models.ErrInvalidInput
	}
	return s.repo.UpdateAlias(ctx, id, req.Status, user.UserID)
}

func (s *Service) RunLearningNow(ctx context.Context) (*LearningReport, error) {
	report, err := RunLearning(ctx, s.repo.db)
	if err == nil {
		s.invalidate()
		logger.FromContext(ctx).Info("assistant: apprentissage manuel", zap.Any("report", report))
	}
	return report, err
}
