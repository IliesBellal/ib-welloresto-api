-- Assistant conversationnel (bouton flottant du back-office, réutilisable
-- plus tard sur le POS et la borne) : « modèle » d'intentions servi aux
-- clients, apprentissage automatique des synonymes, mesure d'usage.
-- Documentation : docs/assistant.md.
--
-- Tables :
--   assistant_intents           intentions (ce que l'assistant sait faire)
--   assistant_terms             mots-clés et phrases de chaque intention,
--                               pré-remplis (seed), saisis (manual) ou appris
--   assistant_entity_aliases    surnoms appris par établissement
--                               (« dessert du jour » → produit X)
--   assistant_learning_signals  observations brutes qui alimentent
--                               l'apprentissage (une ligne par signal)
--   assistant_lexicon           mots vides, mots d'orientation, liste noire
--   assistant_sessions          une ligne par ouverture de l'assistant
--   assistant_events            une ligne par interaction
--
-- Aucun lien de clé étrangère vers merchant / users : même choix que
-- diagnostic_events (journal technique, purgé, jamais bloquant).
--
-- ORDRE DE DÉPLOIEMENT : cette migration puis 207 (pré-remplissage) AVANT le
-- code. Sans elles, l'API répond un modèle vide (le back-office garde son
-- modèle embarqué) et ignore les événements. Rejouable.

CREATE TABLE IF NOT EXISTS assistant_intents (
    intent_key   varchar(96)  PRIMARY KEY,
    label        varchar(120) NOT NULL,
    description  text,
    kind         varchar(16)  NOT NULL,
    action       varchar(64)  NOT NULL,
    params       jsonb        NOT NULL DEFAULT '{}'::jsonb,
    slots        jsonb        NOT NULL DEFAULT '[]'::jsonb,
    permission   varchar(64),
    module       varchar(32),
    confirm      boolean      NOT NULL DEFAULT false,
    -- Intention sensible : jamais de promotion automatique d'un synonyme appris.
    sensitive    boolean      NOT NULL DEFAULT false,
    surfaces     text[]       NOT NULL DEFAULT ARRAY['backoffice']::text[],
    examples     jsonb        NOT NULL DEFAULT '[]'::jsonb,
    priority     integer      NOT NULL DEFAULT 0,
    enabled      boolean      NOT NULL DEFAULT true,
    created_at   timestamptz  NOT NULL DEFAULT now(),
    updated_at   timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT assistant_intents_kind_check CHECK (kind IN ('action', 'navigate', 'info', 'system'))
);

CREATE TABLE IF NOT EXISTS assistant_terms (
    id                 bigserial    PRIMARY KEY,
    intent_key         varchar(96)  NOT NULL REFERENCES assistant_intents (intent_key) ON UPDATE CASCADE ON DELETE CASCADE,
    -- Texte normalisé : minuscules, sans accents ni ponctuation (normalize.go).
    text               varchar(120) NOT NULL,
    kind               varchar(16)  NOT NULL DEFAULT 'keyword',
    weight             real         NOT NULL DEFAULT 1,
    source             varchar(16)  NOT NULL,
    status             varchar(16)  NOT NULL,
    -- '' = global ; sinon terme propre à un établissement.
    merchant_id        varchar(64)  NOT NULL DEFAULT '',
    -- Compteurs recalculés par la tâche d'apprentissage (fenêtre glissante).
    occurrences        integer      NOT NULL DEFAULT 0,
    distinct_users     integer      NOT NULL DEFAULT 0,
    distinct_days      integer      NOT NULL DEFAULT 0,
    distinct_merchants integer      NOT NULL DEFAULT 0,
    matches            integer      NOT NULL DEFAULT 0,
    negatives          integer      NOT NULL DEFAULT 0,
    -- Conflit (même texte appris pour une autre intention) ou intention sensible.
    requires_review    boolean      NOT NULL DEFAULT false,
    review_reason      varchar(32),
    first_seen_at      timestamptz,
    last_seen_at       timestamptz,
    promoted_at        timestamptz,
    reviewed_at        timestamptz,
    reviewed_by        varchar(64),
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT assistant_terms_kind_check   CHECK (kind IN ('keyword', 'phrase')),
    CONSTRAINT assistant_terms_source_check CHECK (source IN ('seed', 'manual', 'learned')),
    CONSTRAINT assistant_terms_status_check CHECK (status IN ('candidate', 'active', 'validated', 'rejected')),
    CONSTRAINT assistant_terms_weight_check CHECK (weight > 0 AND weight <= 5)
);
CREATE UNIQUE INDEX IF NOT EXISTS assistant_terms_unique ON assistant_terms (intent_key, text, merchant_id);
CREATE INDEX IF NOT EXISTS assistant_terms_status ON assistant_terms (status, source);
CREATE INDEX IF NOT EXISTS assistant_terms_merchant ON assistant_terms (merchant_id) WHERE merchant_id <> '';

CREATE TABLE IF NOT EXISTS assistant_entity_aliases (
    id              bigserial    PRIMARY KEY,
    merchant_id     varchar(64)  NOT NULL,
    entity_type     varchar(32)  NOT NULL,
    entity_id       varchar(64)  NOT NULL,
    alias           varchar(120) NOT NULL,
    source          varchar(16)  NOT NULL DEFAULT 'learned',
    status          varchar(16)  NOT NULL DEFAULT 'candidate',
    occurrences     integer      NOT NULL DEFAULT 0,
    distinct_users  integer      NOT NULL DEFAULT 0,
    requires_review boolean      NOT NULL DEFAULT false,
    review_reason   varchar(32),
    first_seen_at   timestamptz,
    last_seen_at    timestamptz,
    promoted_at     timestamptz,
    reviewed_at     timestamptz,
    reviewed_by     varchar(64),
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT assistant_aliases_source_check CHECK (source IN ('manual', 'learned')),
    CONSTRAINT assistant_aliases_status_check CHECK (status IN ('candidate', 'active', 'validated', 'rejected'))
);
CREATE UNIQUE INDEX IF NOT EXISTS assistant_aliases_unique ON assistant_entity_aliases (merchant_id, entity_type, alias, entity_id);
CREATE INDEX IF NOT EXISTS assistant_aliases_merchant_status ON assistant_entity_aliases (merchant_id, status);

CREATE TABLE IF NOT EXISTS assistant_learning_signals (
    id           bigserial   PRIMARY KEY,
    target_type  varchar(8)  NOT NULL,
    target_id    bigint      NOT NULL,
    -- learned : phrase apprise après une suggestion choisie et une action réussie
    -- matched : le terme a servi à comprendre une demande
    -- negative : demande comprise grâce au terme puis annulée ou reformulée
    kind         varchar(16) NOT NULL,
    merchant_id  varchar(64) NOT NULL,
    user_id      varchar(64) NOT NULL,
    event_id     uuid        NOT NULL,
    observed_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT assistant_signals_target_check CHECK (target_type IN ('term', 'alias')),
    CONSTRAINT assistant_signals_kind_check   CHECK (kind IN ('learned', 'matched', 'negative'))
);
-- Idempotence : un même événement renvoyé (réseau, file hors ligne) ne compte qu'une fois.
CREATE UNIQUE INDEX IF NOT EXISTS assistant_signals_unique ON assistant_learning_signals (event_id, target_type, target_id, kind);
CREATE INDEX IF NOT EXISTS assistant_signals_target ON assistant_learning_signals (target_type, target_id, observed_at);
CREATE INDEX IF NOT EXISTS assistant_signals_observed ON assistant_learning_signals (observed_at);

CREATE TABLE IF NOT EXISTS assistant_lexicon (
    id          bigserial   PRIMARY KEY,
    kind        varchar(16) NOT NULL,
    -- Pour kind = 'hint' : type d'intention visé (navigate, info, action).
    group_key   varchar(32) NOT NULL DEFAULT '',
    text        varchar(64) NOT NULL,
    source      varchar(16) NOT NULL DEFAULT 'manual',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT assistant_lexicon_kind_check   CHECK (kind IN ('stopword', 'hint', 'blacklist')),
    CONSTRAINT assistant_lexicon_source_check CHECK (source IN ('seed', 'manual'))
);
CREATE UNIQUE INDEX IF NOT EXISTS assistant_lexicon_unique ON assistant_lexicon (kind, group_key, text);

CREATE TABLE IF NOT EXISTS assistant_sessions (
    id                uuid         PRIMARY KEY,
    merchant_id       varchar(64)  NOT NULL,
    user_id           varchar(64)  NOT NULL,
    role_key          varchar(64),
    surface           varchar(16)  NOT NULL,
    origin_path       varchar(255),
    device            varchar(16),
    platform          varchar(32),
    app_version       varchar(32),
    model_version     varchar(64),
    started_at        timestamptz  NOT NULL,
    last_event_at     timestamptz,
    ended_at          timestamptz,
    duration_ms       integer,
    events_count      integer      NOT NULL DEFAULT 0,
    queries           integer      NOT NULL DEFAULT 0,
    voice_queries     integer      NOT NULL DEFAULT 0,
    understood        integer      NOT NULL DEFAULT 0,
    ambiguous         integer      NOT NULL DEFAULT 0,
    fallbacks         integer      NOT NULL DEFAULT 0,
    chips_clicked     integer      NOT NULL DEFAULT 0,
    actions_succeeded integer      NOT NULL DEFAULT 0,
    actions_failed    integer      NOT NULL DEFAULT 0,
    -- Première action réussie, en ms depuis l'ouverture.
    time_to_action_ms integer,
    -- action | info | navigate | fallback | abandon (posé à la fermeture)
    outcome           varchar(16),
    offline_seen      boolean      NOT NULL DEFAULT false,
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT assistant_sessions_surface_check CHECK (surface IN ('backoffice', 'pos', 'kiosk'))
);
CREATE INDEX IF NOT EXISTS assistant_sessions_merchant_started ON assistant_sessions (merchant_id, started_at);
CREATE INDEX IF NOT EXISTS assistant_sessions_started ON assistant_sessions (started_at);
CREATE INDEX IF NOT EXISTS assistant_sessions_user ON assistant_sessions (user_id, started_at);

CREATE TABLE IF NOT EXISTS assistant_events (
    id            bigserial    PRIMARY KEY,
    event_id      uuid         NOT NULL,
    session_id    uuid         NOT NULL REFERENCES assistant_sessions (id) ON DELETE CASCADE,
    merchant_id   varchar(64)  NOT NULL,
    user_id       varchar(64)  NOT NULL,
    type          varchar(32)  NOT NULL,
    seq           integer,
    turn_id       uuid,
    client_ts     timestamptz  NOT NULL,
    -- ms depuis l'ouverture de la session (temps passé, temps jusqu'à l'action).
    elapsed_ms    integer,
    intent_key    varchar(96),
    score         real,
    -- Requête normalisée, pour regrouper les replis sans lire le JSON.
    query_norm    varchar(255),
    data          jsonb        NOT NULL DEFAULT '{}'::jsonb,
    -- Texte libre retiré de data après la durée de conservation (RGPD).
    anonymized_at timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS assistant_events_event_id ON assistant_events (event_id);
CREATE INDEX IF NOT EXISTS assistant_events_session ON assistant_events (session_id, seq);
CREATE INDEX IF NOT EXISTS assistant_events_merchant_created ON assistant_events (merchant_id, created_at);
CREATE INDEX IF NOT EXISTS assistant_events_type_created ON assistant_events (type, created_at);
CREATE INDEX IF NOT EXISTS assistant_events_intent_created ON assistant_events (intent_key, created_at) WHERE intent_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS assistant_events_fallback_query ON assistant_events (query_norm, created_at) WHERE type = 'fallback';
CREATE INDEX IF NOT EXISTS assistant_events_to_anonymize ON assistant_events (created_at) WHERE anonymized_at IS NULL;
