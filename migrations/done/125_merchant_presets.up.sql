-- LOT A Semaine 2, Chantier 5a (docs/decisions.md) : table des archétypes
-- de marchand (merchant_presets) + colonnes d'application/activation sur
-- merchant. Prérequis à ApplyPreset (chantier 5c) et à POST /v1/signup
-- (chantier 6).

CREATE TABLE merchant_presets (
    id          text PRIMARY KEY,
    code        text NOT NULL,
    version     integer NOT NULL,
    label       text NOT NULL,
    description text NOT NULL,
    naf_codes   text[] NOT NULL DEFAULT '{}',
    config      jsonb NOT NULL,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (code, version)
);
COMMENT ON TABLE merchant_presets IS 'Archétypes de marchand (traditional, brasserie, pizzeria, fast_food, snack, bakery) : préréglages appliqués une fois à la création via ApplyPreset. Versionné (code, version) — un marchand fige la version qu''il a reçue (merchant.preset_version), jamais rétroactif.';
COMMENT ON COLUMN merchant_presets.naf_codes IS 'Codes NAF associés à cet archétype, pour une future suggestion automatique côté /v1/signup — non exploité par ce chantier.';
COMMENT ON COLUMN merchant_presets.config IS 'Voir docs/decisions.md (Chantier 5b) pour la correspondance champ par champ avec merchant_parameters/productcateg/floors/locations. Validé contre un schéma applicatif à l''écriture (ApplyPreset), pas de contrainte JSONB en base.';

-- ============================================================================
-- Colonnes d'application/activation sur merchant
-- ============================================================================
ALTER TABLE merchant
    ADD COLUMN IF NOT EXISTS preset_code text,
    ADD COLUMN IF NOT EXISTS preset_version integer,
    ADD COLUMN IF NOT EXISTS place_id text,
    ADD COLUMN IF NOT EXISTS activation_state text NOT NULL DEFAULT 'SETUP',
    ADD COLUMN IF NOT EXISTS went_live_at timestamptz,
    ADD COLUMN IF NOT EXISTS signup_channel text,
    ADD COLUMN IF NOT EXISTS signup_source jsonb;

COMMENT ON COLUMN merchant.preset_code IS 'Archétype appliqué à la création (merchant_presets.code). NULL pour un marchand créé avant ce chantier ou créé sans preset.';
COMMENT ON COLUMN merchant.preset_version IS 'Version figée de l''archétype au moment de la création (merchant_presets.version) — jamais mise à jour rétroactivement si l''archétype évolue.';
COMMENT ON COLUMN merchant.place_id IS 'Identifiant Google Places, saisi/résolu au signup (chantier 6) — non renseigné pour les marchands créés via /pos/create.';
COMMENT ON COLUMN merchant.activation_state IS 'SETUP (en cours de configuration, self-onboarding) ou LIVE (exploitation réelle). Piloté par le parcours de signup (semaine 2) puis par la déduction d''événements métier (semaine 3) — jamais coché manuellement.';
COMMENT ON COLUMN merchant.signup_channel IS 'Canal d''inscription (ex. ''self_signup'', ''sales'') — NULL pour un marchand créé via /pos/create.';
COMMENT ON COLUMN merchant.signup_source IS 'Contexte libre capturé au signup (UTM, référent, etc.) — NULL pour un marchand créé via /pos/create.';

-- ============================================================================
-- ATTENTION — rattrapage obligatoire (le point signalé par le chantier)
-- ============================================================================
-- activation_state a un défaut 'SETUP' : sans cette instruction, TOUT le parc
-- de marchands déjà en exploitation (créés avant ce chantier, jamais passés
-- par un quelconque champ d'activation) se retrouverait marqué comme "en
-- cours de configuration" dès l'ALTER TABLE ci-dessus (Postgres applique le
-- DEFAULT à toutes les lignes existantes lors d'un ADD COLUMN NOT NULL DEFAULT).
-- went_live_at est rétroactivement fixé à creation_date, faute de meilleure
-- date connue pour un marchand antérieur à ce chantier.
UPDATE merchant
SET activation_state = 'LIVE',
    went_live_at = COALESCE(went_live_at, creation_date)
WHERE activation_state = 'SETUP';
