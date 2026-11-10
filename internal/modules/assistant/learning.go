package assistant

import (
	"context"
	"database/sql"
	"time"

	"go.uber.org/zap"

	"welloresto-api/internal/database/dbx"
)

// Apprentissage autonome du modèle (docs/assistant.md, « Auto-apprentissage »).
//
// Cycle de vie d'un synonyme appris :
//   candidate → active (établissement) → active (global) → validated / rejected
//
// Un candidat naît quand un utilisateur choisit une suggestion après un repli
// puis réussit l'action (signal « learned », écrit à l'ingestion). Cette tâche,
// horaire, recompte les signaux sur une fenêtre glissante et applique les
// seuils ci-dessous. Un humain peut tout corriger depuis la plateforme admin ;
// un rejet humain est définitif.

const (
	// Fenêtre des signaux pris en compte.
	learningWindow = 180 * 24 * time.Hour

	// Promotion d'un terme dans son établissement : au moins 3 occurrences,
	// par 2 personnes différentes, OU par la même personne sur 3 jours
	// différents (petit restaurant tenu par une seule personne).
	termMinOccurrences = 3
	termMinUsers       = 2
	termMinDays        = 3

	// Le même texte appris pour plusieurs intentions : promotion seulement si
	// celle-ci réunit au moins 80 % des occurrences de l'établissement.
	minConflictShare = 0.8

	// Passage en global : actif dans au moins 3 établissements.
	termMinMerchantsForGlobal = 3

	// Rétrogradation : au moins 5 utilisations, dont plus de 30 % annulées ou
	// reformulées.
	demoteMinMatches    = 5
	demoteNegativeShare = 0.3

	// Surnom d'entité : 2 occurrences suffisent (il est propre au restaurant).
	aliasMinOccurrences = 2

	// Poids d'un terme appris non validé par un humain, au service du modèle.
	learnedWeightFactor = 0.8
)

// Conservation (RGPD) : texte libre retiré des événements après 90 jours ;
// événements et sessions supprimés après 400 jours ; signaux après 365 jours.
const (
	textRetention   = 90 * 24 * time.Hour
	eventRetention  = 400 * 24 * time.Hour
	signalRetention = 365 * 24 * time.Hour
	retentionBatch  = 5000
)

type termDecision int

const (
	decisionNone termDecision = iota
	decisionPromote
	decisionFlagSensitive
	decisionFlagConflict
	decisionDemote
)

type termStats struct {
	Status         string
	Source         string
	Occurrences    int
	Users          int
	Days           int
	Matches        int
	Negatives      int
	Share          float64
	Sensitive      bool
	RequiresReview bool
}

// decideTerm est pure : toute la politique de promotion tient ici (testée).
func decideTerm(s termStats) termDecision {
	if s.Source != "learned" {
		return decisionNone
	}
	switch s.Status {
	case "candidate":
		if s.Occurrences < termMinOccurrences || (s.Users < termMinUsers && s.Days < termMinDays) {
			return decisionNone
		}
		if s.Sensitive {
			if s.RequiresReview {
				return decisionNone
			}
			return decisionFlagSensitive
		}
		if s.Share < minConflictShare {
			if s.RequiresReview {
				return decisionNone
			}
			return decisionFlagConflict
		}
		if s.RequiresReview {
			// Déjà rétrogradé pour retours négatifs : un humain doit trancher.
			return decisionNone
		}
		return decisionPromote
	case "active":
		if s.Matches >= demoteMinMatches && float64(s.Negatives)/float64(s.Matches) > demoteNegativeShare {
			return decisionDemote
		}
	}
	return decisionNone
}

func decideAlias(s termStats) termDecision {
	switch s.Status {
	case "candidate":
		if s.Occurrences < aliasMinOccurrences || s.RequiresReview {
			return decisionNone
		}
		if s.Share < minConflictShare {
			return decisionFlagConflict
		}
		return decisionPromote
	case "active":
		if s.Source == "learned" && s.Matches >= demoteMinMatches && float64(s.Negatives)/float64(s.Matches) > demoteNegativeShare {
			return decisionDemote
		}
	}
	return decisionNone
}

// RunLearning fait un passage complet. Idempotent ; appelé chaque heure par
// la tâche RunAssistantLearning et à la demande depuis l'administration.
func RunLearning(ctx context.Context, database *sql.DB) (*LearningReport, error) {
	db := dbx.GetDB(ctx, database)
	report := &LearningReport{}
	since := time.Now().Add(-learningWindow)

	// 1. Compteurs des termes à partir des signaux (sans toucher updated_at).
	res, err := db.ExecContext(ctx, `WITH s AS (
			SELECT target_id,
				COUNT(*) FILTER (WHERE kind = 'learned')                                       AS occ,
				COUNT(DISTINCT user_id) FILTER (WHERE kind = 'learned')                         AS users,
				COUNT(DISTINCT (observed_at AT TIME ZONE 'Europe/Paris')::date) FILTER (WHERE kind = 'learned') AS days,
				COUNT(*) FILTER (WHERE kind = 'matched')                                       AS matches,
				COUNT(*) FILTER (WHERE kind = 'negative')                                      AS negatives
			FROM assistant_learning_signals
			WHERE target_type = 'term' AND observed_at >= ?
			GROUP BY target_id
		)
		UPDATE assistant_terms t SET
			occurrences = s.occ, distinct_users = s.users, distinct_days = s.days,
			matches = s.matches, negatives = s.negatives
		FROM s
		WHERE t.id = s.target_id
		  AND (t.occurrences, t.distinct_users, t.distinct_days, t.matches, t.negatives)
		      IS DISTINCT FROM (s.occ, s.users, s.days, s.matches, s.negatives)`, since)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	report.TermsRecounted = int(n)

	// 2. Décision pour chaque terme appris (part du texte dans l'établissement
	//    pour détecter les conflits entre intentions).
	rows, err := db.QueryContext(ctx, `SELECT t.id, t.status, t.source, t.occurrences, t.distinct_users, t.distinct_days,
			t.matches, t.negatives, t.requires_review, i.sensitive,
			COALESCE(t.occurrences::float8 / NULLIF(tot.total, 0), 1)
		FROM assistant_terms t
		JOIN assistant_intents i ON i.intent_key = t.intent_key
		JOIN (
			SELECT merchant_id, text, SUM(occurrences) AS total
			FROM assistant_terms WHERE source = 'learned'
			GROUP BY merchant_id, text
		) tot ON tot.merchant_id = t.merchant_id AND tot.text = t.text
		WHERE t.source = 'learned' AND t.status IN ('candidate', 'active')`)
	if err != nil {
		return nil, err
	}
	type pending struct {
		id int64
		d  termDecision
	}
	var todo []pending
	for rows.Next() {
		var id int64
		var s termStats
		if err := rows.Scan(&id, &s.Status, &s.Source, &s.Occurrences, &s.Users, &s.Days, &s.Matches, &s.Negatives,
			&s.RequiresReview, &s.Sensitive, &s.Share); err != nil {
			rows.Close()
			return nil, err
		}
		if d := decideTerm(s); d != decisionNone {
			todo = append(todo, pending{id, d})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range todo {
		var q string
		switch p.d {
		case decisionPromote:
			q = `UPDATE assistant_terms SET status = 'active', promoted_at = now(), requires_review = false, review_reason = NULL, updated_at = now() WHERE id = ?`
			report.TermsPromoted++
		case decisionFlagSensitive:
			q = `UPDATE assistant_terms SET requires_review = true, review_reason = 'sensitive' WHERE id = ?`
			report.TermsFlagged++
		case decisionFlagConflict:
			q = `UPDATE assistant_terms SET requires_review = true, review_reason = 'conflict' WHERE id = ?`
			report.TermsFlagged++
		case decisionDemote:
			q = `UPDATE assistant_terms SET status = 'candidate', requires_review = true, review_reason = 'negative_feedback', updated_at = now() WHERE id = ?`
			report.TermsDemoted++
		}
		if _, err := db.ExecContext(ctx, q, p.id); err != nil {
			return nil, err
		}
	}

	// 3. Passage en global : actif dans assez d'établissements. Un terme global
	//    rejeté par un humain le reste.
	grows, err := db.QueryContext(ctx, `SELECT intent_key, text, MIN(kind), COUNT(DISTINCT merchant_id)
		FROM assistant_terms
		WHERE source = 'learned' AND merchant_id <> '' AND status IN ('active', 'validated')
		GROUP BY intent_key, text
		HAVING COUNT(DISTINCT merchant_id) >= ?`, termMinMerchantsForGlobal)
	if err != nil {
		return nil, err
	}
	type global struct {
		intent, text, kind string
		merchants          int
	}
	var globals []global
	for grows.Next() {
		var g global
		if err := grows.Scan(&g.intent, &g.text, &g.kind, &g.merchants); err != nil {
			grows.Close()
			return nil, err
		}
		globals = append(globals, g)
	}
	grows.Close()
	for _, g := range globals {
		var promoted bool
		err := db.QueryRowContext(ctx, `INSERT INTO assistant_terms
				(intent_key, text, kind, weight, source, status, merchant_id, distinct_merchants, first_seen_at, last_seen_at, promoted_at)
			VALUES (?, ?, ?, 1, 'learned', 'active', '', ?, now(), now(), now())
			ON CONFLICT (intent_key, text, merchant_id) DO UPDATE SET
				distinct_merchants = EXCLUDED.distinct_merchants,
				status      = CASE WHEN assistant_terms.status = 'candidate' AND NOT assistant_terms.requires_review THEN 'active' ELSE assistant_terms.status END,
				promoted_at = CASE WHEN assistant_terms.status = 'candidate' AND NOT assistant_terms.requires_review THEN now() ELSE assistant_terms.promoted_at END,
				updated_at  = CASE WHEN assistant_terms.status = 'candidate' AND NOT assistant_terms.requires_review THEN now() ELSE assistant_terms.updated_at END
			RETURNING (xmax = 0) OR promoted_at = now()`,
			g.intent, g.text, g.kind, g.merchants,
		).Scan(&promoted)
		if err != nil {
			return nil, err
		}
		if promoted {
			report.TermsGlobalized++
		}
	}

	// 4. Surnoms d'entités.
	if _, err := db.ExecContext(ctx, `WITH s AS (
			SELECT target_id,
				COUNT(*) FILTER (WHERE kind = 'learned')               AS occ,
				COUNT(DISTINCT user_id) FILTER (WHERE kind = 'learned') AS users
			FROM assistant_learning_signals
			WHERE target_type = 'alias' AND observed_at >= ?
			GROUP BY target_id
		)
		UPDATE assistant_entity_aliases a SET occurrences = s.occ, distinct_users = s.users
		FROM s
		WHERE a.id = s.target_id AND (a.occurrences, a.distinct_users) IS DISTINCT FROM (s.occ, s.users)`, since); err != nil {
		return nil, err
	}
	arows, err := db.QueryContext(ctx, `SELECT a.id, a.status, a.source, a.occurrences, a.requires_review,
			COALESCE(a.occurrences::float8 / NULLIF(tot.total, 0), 1),
			COALESCE(sig.matches, 0), COALESCE(sig.negatives, 0)
		FROM assistant_entity_aliases a
		JOIN (
			SELECT merchant_id, entity_type, alias, SUM(occurrences) AS total
			FROM assistant_entity_aliases GROUP BY merchant_id, entity_type, alias
		) tot ON tot.merchant_id = a.merchant_id AND tot.entity_type = a.entity_type AND tot.alias = a.alias
		LEFT JOIN (
			SELECT target_id,
				COUNT(*) FILTER (WHERE kind = 'matched')  AS matches,
				COUNT(*) FILTER (WHERE kind = 'negative') AS negatives
			FROM assistant_learning_signals
			WHERE target_type = 'alias' AND observed_at >= ?
			GROUP BY target_id
		) sig ON sig.target_id = a.id
		WHERE a.status IN ('candidate', 'active')`, since)
	if err != nil {
		return nil, err
	}
	var aliasTodo []pending
	for arows.Next() {
		var id int64
		var s termStats
		if err := arows.Scan(&id, &s.Status, &s.Source, &s.Occurrences, &s.RequiresReview, &s.Share, &s.Matches, &s.Negatives); err != nil {
			arows.Close()
			return nil, err
		}
		if d := decideAlias(s); d != decisionNone {
			aliasTodo = append(aliasTodo, pending{id, d})
		}
	}
	arows.Close()
	for _, p := range aliasTodo {
		var q string
		switch p.d {
		case decisionPromote:
			q = `UPDATE assistant_entity_aliases SET status = 'active', promoted_at = now(), updated_at = now() WHERE id = ?`
			report.AliasesPromoted++
		case decisionFlagConflict:
			q = `UPDATE assistant_entity_aliases SET requires_review = true, review_reason = 'conflict' WHERE id = ?`
			report.AliasesFlagged++
		case decisionDemote:
			q = `UPDATE assistant_entity_aliases SET status = 'candidate', requires_review = true, review_reason = 'negative_feedback', updated_at = now() WHERE id = ?`
			report.AliasesFlagged++
		}
		if _, err := db.ExecContext(ctx, q, p.id); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// RunRetention anonymise puis purge par lots (verrous courts).
func RunRetention(ctx context.Context, database *sql.DB, log *zap.Logger) {
	db := dbx.GetDB(ctx, database)
	now := time.Now()

	batched := func(label, query string, arg interface{}) {
		var total int64
		for {
			res, err := db.ExecContext(ctx, query, arg, retentionBatch)
			if err != nil {
				log.Error("[CRON] assistant retention: échec", zap.String("step", label), zap.Error(err), zap.Int64("done", total))
				return
			}
			n, _ := res.RowsAffected()
			total += n
			if n < retentionBatch {
				break
			}
		}
		log.Info("[CRON] assistant retention", zap.String("step", label), zap.Int64("rows", total))
	}

	batched("anonymize_text", `UPDATE assistant_events SET
			data = data - 'text' - 'query_norm' - 'transcript' - 'previous_text' - 'comment' - 'learning' - 'alternatives',
			query_norm = NULL,
			anonymized_at = now()
		WHERE id IN (SELECT id FROM assistant_events WHERE anonymized_at IS NULL AND created_at < ? LIMIT ?)`,
		now.Add(-textRetention))
	batched("delete_events", `DELETE FROM assistant_events WHERE id IN (
			SELECT id FROM assistant_events WHERE created_at < ? LIMIT ?)`, now.Add(-eventRetention))
	batched("delete_sessions", `DELETE FROM assistant_sessions WHERE id IN (
			SELECT id FROM assistant_sessions WHERE started_at < ? LIMIT ?)`, now.Add(-eventRetention))
	batched("delete_signals", `DELETE FROM assistant_learning_signals WHERE id IN (
			SELECT id FROM assistant_learning_signals WHERE observed_at < ? LIMIT ?)`, now.Add(-signalRetention))
}
