// Package assistant sert le « modèle » de l'assistant conversationnel du
// back-office (et, plus tard, du POS et de la borne), reçoit sa mesure
// d'usage et fait progresser le modèle à partir des demandes incomprises.
//
// Le moteur de compréhension tourne côté client (wello-back-office,
// src/lib/assistant/engine.ts) : l'API ne fait que servir le vocabulaire,
// stocker les événements et apprendre. Voir docs/assistant.md.
package assistant

import "time"

// ─── Contrat du modèle (miroir de src/lib/assistant/types.ts) ──────────────

// ModelSchema est la version du format JSON servi aux clients.
const ModelSchema = 1

type Thresholds struct {
	Accept       float64 `json:"accept"`
	AmbiguityGap float64 `json:"ambiguity_gap"`
	Suggest      float64 `json:"suggest"`
}

// DefaultThresholds — identiques à DEFAULT_THRESHOLDS du back-office.
var DefaultThresholds = Thresholds{Accept: 60, AmbiguityGap: 8, Suggest: 30}

type ModelTerm struct {
	ID     int64   `json:"id"`
	Text   string  `json:"text"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
	Source string  `json:"source"`
	Status string  `json:"status"`
}

type ModelIntent struct {
	Key         string                 `json:"key"`
	Label       string                 `json:"label"`
	Description *string                `json:"description,omitempty"`
	Kind        string                 `json:"kind"`
	Action      string                 `json:"action"`
	Params      map[string]interface{} `json:"params"`
	Slots       []interface{}          `json:"slots"`
	Permission  *string                `json:"permission"`
	Module      *string                `json:"module"`
	Confirm     bool                   `json:"confirm"`
	Sensitive   bool                   `json:"sensitive"`
	Surfaces    []string               `json:"surfaces"`
	Priority    int                    `json:"priority"`
	Examples    []string               `json:"examples"`
	Terms       []ModelTerm            `json:"terms"`
}

type ModelAlias struct {
	ID         int64  `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Alias      string `json:"alias"`
}

type Model struct {
	Schema      int                 `json:"schema"`
	Version     string              `json:"version"`
	Surface     string              `json:"surface"`
	GeneratedAt time.Time           `json:"generated_at"`
	Thresholds  Thresholds          `json:"thresholds"`
	Stopwords   []string            `json:"stopwords"`
	Hints       map[string][]string `json:"hints"`
	Intents     []ModelIntent       `json:"intents"`
	Aliases     []ModelAlias        `json:"aliases"`
}

// ModelResponse — `unchanged` quand la version demandée est déjà la bonne.
type ModelResponse struct {
	Unchanged bool   `json:"unchanged"`
	Version   string `json:"version,omitempty"`
	Model     *Model `json:"model,omitempty"`
}

// ─── Événements (miroir de src/lib/assistant/tracking.ts) ──────────────────

const (
	EventOpened            = "opened"
	EventClosed            = "closed"
	EventChipClicked       = "chip_clicked"
	EventQuerySubmitted    = "query_submitted"
	EventIntentMatched     = "intent_matched"
	EventIntentAmbiguous   = "intent_ambiguous"
	EventFallback          = "fallback"
	EventSuggestionClicked = "suggestion_clicked"
	EventConfirmed         = "confirmed"
	EventCancelled         = "cancelled"
	EventActionSucceeded   = "action_succeeded"
	EventActionFailed      = "action_failed"
	EventNavigated         = "navigated"
	EventInfoShown         = "info_shown"
	EventRephrased         = "rephrased"
)

// allowedEventTypes doit rester aligné sur ASSISTANT_EVENT_TYPES du back-office.
var allowedEventTypes = map[string]bool{
	"opened": true, "closed": true, "onboarding_seen": true, "model_loaded": true,
	"chip_clicked": true, "example_clicked": true, "query_submitted": true,
	"voice_started": true, "voice_result": true, "voice_error": true,
	"intent_matched": true, "intent_ambiguous": true, "fallback": true,
	"suggestion_clicked": true, "slot_requested": true, "slot_filled": true,
	"confirmation_shown": true, "confirmed": true, "cancelled": true,
	"action_succeeded": true, "action_failed": true, "navigated": true,
	"info_shown": true, "rephrased": true, "multi_request": true,
	"feedback": true, "learning_info_opened": true,
}

var allowedSurfaces = map[string]bool{"backoffice": true, "pos": true, "kiosk": true}
var allowedDevices = map[string]bool{"mobile": true, "tablet": true, "desktop": true}

const (
	maxEventsPerBatch = 50
	maxDataBytes      = 4 << 10
	// Horloge client tolérée : au-delà, l'heure serveur est retenue.
	maxClockSkew = 24 * time.Hour
)

type SessionInfo struct {
	ID           string `json:"id"`
	Surface      string `json:"surface"`
	StartedAt    int64  `json:"started_at"`
	OriginPath   string `json:"origin_path"`
	Device       string `json:"device"`
	Platform     string `json:"platform"`
	AppVersion   string `json:"app_version"`
	ModelVersion string `json:"model_version"`
	Online       bool   `json:"online"`
}

type TrackedEvent struct {
	EventID   string                 `json:"event_id"`
	Type      string                 `json:"type"`
	Seq       int                    `json:"seq"`
	TS        int64                  `json:"ts"`
	TurnID    string                 `json:"turn_id"`
	IntentKey string                 `json:"intent_key"`
	Score     *float64               `json:"score"`
	Data      map[string]interface{} `json:"data"`
}

type EventBatch struct {
	Session SessionInfo    `json:"session"`
	Events  []TrackedEvent `json:"events"`
}

// LearningSignal — porté par data.learning d'un événement action_succeeded
// (buildLearningSignal côté client).
type LearningSignal struct {
	SourceEventID string `json:"source_event_id"`
	QueryNorm     string `json:"query_norm"`
	IntentKey     string `json:"intent_key"`
	Term          *struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	} `json:"term"`
	Alias *struct {
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Text       string `json:"text"`
	} `json:"alias"`
}

// ─── Administration ────────────────────────────────────────────────────────

type TermRow struct {
	ID                int64      `json:"id"`
	IntentKey         string     `json:"intent_key"`
	IntentLabel       string     `json:"intent_label"`
	Text              string     `json:"text"`
	Kind              string     `json:"kind"`
	Weight            float64    `json:"weight"`
	Source            string     `json:"source"`
	Status            string     `json:"status"`
	MerchantID        string     `json:"merchant_id"`
	Occurrences       int        `json:"occurrences"`
	DistinctUsers     int        `json:"distinct_users"`
	DistinctDays      int        `json:"distinct_days"`
	DistinctMerchants int        `json:"distinct_merchants"`
	Matches           int        `json:"matches"`
	Negatives         int        `json:"negatives"`
	RequiresReview    bool       `json:"requires_review"`
	ReviewReason      *string    `json:"review_reason"`
	FirstSeenAt       *time.Time `json:"first_seen_at"`
	LastSeenAt        *time.Time `json:"last_seen_at"`
	PromotedAt        *time.Time `json:"promoted_at"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
}

type AliasRow struct {
	ID             int64      `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	EntityType     string     `json:"entity_type"`
	EntityID       string     `json:"entity_id"`
	Alias          string     `json:"alias"`
	Source         string     `json:"source"`
	Status         string     `json:"status"`
	Occurrences    int        `json:"occurrences"`
	DistinctUsers  int        `json:"distinct_users"`
	RequiresReview bool       `json:"requires_review"`
	ReviewReason   *string    `json:"review_reason"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
}

type UpdateTermRequest struct {
	Status    *string  `json:"status"`
	IntentKey *string  `json:"intent_key"`
	Weight    *float64 `json:"weight"`
	// Portée : "global" (merchant_id vidé) ou laissée telle quelle.
	Scope *string `json:"scope"`
}

type CreateTermRequest struct {
	IntentKey  string  `json:"intent_key"`
	Text       string  `json:"text"`
	Weight     float64 `json:"weight"`
	MerchantID string  `json:"merchant_id"`
}

type UpdateAliasRequest struct {
	Status string `json:"status"`
}

type FallbackGroup struct {
	QueryNorm   string             `json:"query_norm"`
	Count       int                `json:"count"`
	Users       int                `json:"users"`
	Merchants   int                `json:"merchants"`
	LastSeenAt  time.Time          `json:"last_seen_at"`
	Example     string             `json:"example"`
	ChosenAfter []ChosenAfterCount `json:"chosen_after"`
	Abandoned   int                `json:"abandoned"`
}

type ChosenAfterCount struct {
	IntentKey string `json:"intent_key"`
	Count     int    `json:"count"`
}

type Stats struct {
	From                string         `json:"from"`
	To                  string         `json:"to"`
	Sessions            int            `json:"sessions"`
	Users               int            `json:"users"`
	Merchants           int            `json:"merchants"`
	Queries             int            `json:"queries"`
	VoiceQueries        int            `json:"voice_queries"`
	ChipsClicked        int            `json:"chips_clicked"`
	Understood          int            `json:"understood"`
	Ambiguous           int            `json:"ambiguous"`
	Fallbacks           int            `json:"fallbacks"`
	UnderstandingRate   float64        `json:"understanding_rate"`
	SuccessRate         float64        `json:"success_rate"`
	ActionsSucceeded    int            `json:"actions_succeeded"`
	ActionsFailed       int            `json:"actions_failed"`
	Cancellations       int            `json:"cancellations"`
	Rephrases           int            `json:"rephrases"`
	MedianTimeToActionM *float64       `json:"median_time_to_action_ms"`
	MedianSessionMs     *float64       `json:"median_session_ms"`
	Outcomes            map[string]int `json:"outcomes"`
	TopIntents          []IntentCount  `json:"top_intents"`
	Devices             map[string]int `json:"devices"`
	ByWeek              []WeekPoint    `json:"by_week"`
	LearnedActive       int            `json:"learned_active"`
	LearnedCandidates   int            `json:"learned_candidates"`
	PendingReview       int            `json:"pending_review"`
}

type IntentCount struct {
	IntentKey string  `json:"intent_key"`
	Count     int     `json:"count"`
	Succeeded int     `json:"succeeded"`
	Cancelled int     `json:"cancelled"`
	AvgScore  float64 `json:"avg_score"`
}

type WeekPoint struct {
	Week              string  `json:"week"`
	Queries           int     `json:"queries"`
	UnderstandingRate float64 `json:"understanding_rate"`
}

type SessionEventRow struct {
	Seq       *int                   `json:"seq"`
	Type      string                 `json:"type"`
	ClientTS  time.Time              `json:"client_ts"`
	ElapsedMs *int                   `json:"elapsed_ms"`
	IntentKey *string                `json:"intent_key"`
	Score     *float64               `json:"score"`
	Data      map[string]interface{} `json:"data"`
}

// LearningReport — résultat d'un passage de la tâche d'apprentissage.
type LearningReport struct {
	TermsRecounted  int `json:"terms_recounted"`
	TermsPromoted   int `json:"terms_promoted"`
	TermsGlobalized int `json:"terms_globalized"`
	TermsDemoted    int `json:"terms_demoted"`
	TermsFlagged    int `json:"terms_flagged"`
	AliasesPromoted int `json:"aliases_promoted"`
	AliasesFlagged  int `json:"aliases_flagged"`
}
