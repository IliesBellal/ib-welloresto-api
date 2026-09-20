-- Module CDS (Customer Display System) — écran d'affichage client temps réel.
-- Voir wello-resto-customer-display-system/docs/CDS_DECISIONS.md.
--
-- Tables volontairement distinctes de kiosks/kiosk_* (décision D10) : un CDS
-- est un écran passif, son token ne doit ouvrir aucune route /kiosk/* (qui
-- permettrait de créer et d'annuler des commandes), il ne consomme pas le
-- quota facturé subscriptions.max_kiosks, et il apparaît dans un parc séparé
-- côté back-office. Le mécanisme d'enrôlement lui-même est un clone de celui
-- du module kiosk (hachage HMAC-SHA256 + pepper, access token auto-porteur,
-- refresh token tournant).
--
-- Conventions reprises des migrations Postgres récentes : id varchar(64)
-- généré côté Go (helpers.GeneratePrefixedID), aucune FK vers les tables
-- historiques (merchant, orders), FK autorisée entre tables du module.

-- ---------------------------------------------------------------------------
-- cds_displays — parc d'écrans
-- ---------------------------------------------------------------------------
-- Pas de colonne `enabled` ni de statut 'inactive' (décision D15) : un écran
-- est enrôlé ou révoqué, il n'y a pas d'état désactivé. Le staff qui veut
-- éteindre un écran éteint le moniteur. Conséquence utile : le plafond de
-- 4 écrans (D4) se compte sur status <> 'revoked' et ne peut pas être
-- contourné en désactivant des lignes pour en enrôler d'autres.
CREATE TABLE IF NOT EXISTS cds_displays (
    id                  varchar(64) PRIMARY KEY,
    merchant_id         varchar(64) NOT NULL,
    name                varchar(100) NOT NULL,
    location_id         varchar(64),
    status              varchar(20) NOT NULL DEFAULT 'active',
    app_version         varchar(20),
    hardware_model      varchar(100),
    os_version          varchar(50),
    -- Identifiant dérivé de l'OS (Android ID), pas un secret : sert à la
    -- ré-identification si le stockage local est perdu (POST /cds/auth/reclaim),
    -- même rôle que kiosks.device_id.
    device_id           varchar(128),
    admin_pin_encrypted bytea,
    last_heartbeat_at   timestamptz,
    last_ip             varchar(45),
    last_error          text,
    last_error_at       timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz,
    CONSTRAINT cds_displays_status_check
        CHECK (status IN ('pending', 'active', 'revoked'))
);

CREATE INDEX IF NOT EXISTS idx_cds_displays_merchant ON cds_displays (merchant_id);
CREATE INDEX IF NOT EXISTS idx_cds_displays_status ON cds_displays (merchant_id, status);
CREATE INDEX IF NOT EXISTS idx_cds_displays_device_id
    ON cds_displays (device_id) WHERE device_id IS NOT NULL;

COMMENT ON TABLE cds_displays IS 'Parc d''écrans d''affichage client (CDS). Distinct de kiosks : écran passif, aucune écriture métier possible — voir CDS_DECISIONS.md D10.';

-- ---------------------------------------------------------------------------
-- cds_enrollment_codes — codes d'enrôlement à usage unique
-- ---------------------------------------------------------------------------
-- code_hash = HMAC-SHA256(code, pepper), jamais le code en clair — même
-- mécanisme que kiosk_enrollment_codes et users_rights.pin_hash.
--
-- ⚠ SÉCURITÉ : le code CDS fait 6 chiffres (10^6), contre 8 caractères sur
-- un alphabet de 32 pour le kiosk (32^8 ≈ 10^12) — contrainte de saisie à la
-- télécommande (D14/D16). L'index UNIQUE ci-dessous est global : une
-- tentative aveugle est testée contre tous les codes en attente de la
-- plateforme. En l'absence de rate limiting dans cette API, c'est un risque
-- tracé — voir docs/audits/2026-09-19-enrollment-rate-limiting.md. Le TTL
-- court (10 min par défaut) réduit la taille de la cible sans limiter le
-- débit des tentatives.
CREATE TABLE IF NOT EXISTS cds_enrollment_codes (
    id                 varchar(64) PRIMARY KEY,
    merchant_id        varchar(64) NOT NULL,
    code_hash          varchar(64) NOT NULL,
    display_id         varchar(64) REFERENCES cds_displays (id),
    expires_at         timestamptz NOT NULL,
    used_at            timestamptz,
    created_by_user_id varchar(64),
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_cds_enrollment_code_hash
    ON cds_enrollment_codes (code_hash);
CREATE INDEX IF NOT EXISTS idx_cds_enrollment_merchant
    ON cds_enrollment_codes (merchant_id);

-- ---------------------------------------------------------------------------
-- cds_device_tokens — refresh tokens
-- ---------------------------------------------------------------------------
-- L'access token n'est PAS stocké : il est auto-porteur et signé HMAC-SHA256
-- (même choix que kiosk, voir KIOSK_DECISIONS.md G.1). Seul le refresh token
-- est persisté, haché.
CREATE TABLE IF NOT EXISTS cds_device_tokens (
    id           varchar(64) PRIMARY KEY,
    display_id   varchar(64) NOT NULL REFERENCES cds_displays (id),
    token_hash   varchar(64) NOT NULL,
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_cds_device_token_hash
    ON cds_device_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_cds_device_token_display
    ON cds_device_tokens (display_id);

-- ---------------------------------------------------------------------------
-- cds_settings — paramètres, PAR ÉCRAN
-- ---------------------------------------------------------------------------
-- Scopée par display_id et non par merchant_id — écart volontaire avec
-- kiosk_settings (D2) : c'est tout l'intérêt de la fonctionnalité. Un petit
-- écran en zone de retrait peut n'afficher que les livraisons Uber Eats et
-- Deliveroo, pendant qu'un grand écran au comptoir n'affiche que les
-- commandes internes sur place et à emporter.
--
-- order_types / channels : filtres combinés en ET, appliqués en SQL dans la
-- projection GET /cds/orders. Stockés en jsonb (tableau de chaînes) plutôt
-- qu'en text[] : c'est la convention du dépôt pour les listes structurées
-- (merchant_presets.config, users.signup_source), scannées en []byte puis
-- json.Unmarshal côté Go. Aucun text[] n'existe ailleurs dans ce schéma, et
-- le driver pgx/v5 est utilisé via database/sql sans helper d'array.
CREATE TABLE IF NOT EXISTS cds_settings (
    display_id           varchar(64) PRIMARY KEY REFERENCES cds_displays (id),
    layout_mode          varchar(20) NOT NULL DEFAULT 'two_zones',
    preparing_zone_ratio integer NOT NULL DEFAULT 35,
    order_types          jsonb NOT NULL DEFAULT '["IN","TAKE_AWAY","DELIVERY"]'::jsonb,
    channels             jsonb NOT NULL DEFAULT '["WELLO_RESTO","UBER_EATS","DELIVEROO"]'::jsonb,
    -- Compte à rebours basé sur orders.estimated_ready (D11). Désactivé par
    -- défaut : cette estimation est figée à la création de la commande et
    -- jamais recalculée, l'afficher est un choix éditorial du restaurateur.
    show_wait_time       boolean NOT NULL DEFAULT false,
    marketing_enabled    boolean NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz,
    CONSTRAINT cds_settings_layout_mode_check
        CHECK (layout_mode IN ('two_zones', 'three_zones')),
    CONSTRAINT cds_settings_preparing_zone_ratio_check
        CHECK (preparing_zone_ratio BETWEEN 20 AND 60)
);

COMMENT ON COLUMN cds_settings.order_types IS 'Filtre sur orders.order_type (IN/TAKE_AWAY/DELIVERY), combiné en ET avec channels — voir CDS_DECISIONS.md D2.';
COMMENT ON COLUMN cds_settings.channels IS 'Filtre sur orders.brand (WELLO_RESTO/UBER_EATS/DELIVEROO). Orthogonal à order_types : une commande Uber Eats porte order_type=DELIVERY ou TAKE_AWAY, ce n''est pas un type à part.';

-- ---------------------------------------------------------------------------
-- cds_media_items — rotation de la zone marketing
-- ---------------------------------------------------------------------------
-- Table dédiée et non des colonnes plates dans cds_settings (D6) : la zone
-- marketing enchaîne plusieurs médias en boucle. Une image ou un QR s'affiche
-- duration_seconds ; une vidéo est jouée en entier, muette, et
-- duration_seconds est ignoré.
CREATE TABLE IF NOT EXISTS cds_media_items (
    id               varchar(64) PRIMARY KEY,
    display_id       varchar(64) NOT NULL REFERENCES cds_displays (id),
    kind             varchar(10) NOT NULL,
    url              text,
    qr_payload       text,
    duration_seconds integer NOT NULL DEFAULT 10,
    sort_order       integer NOT NULL DEFAULT 0,
    enabled          boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz,
    CONSTRAINT cds_media_items_kind_check
        CHECK (kind IN ('image', 'video', 'qr')),
    -- Un média 'qr' ne porte pas de fichier (seulement qr_payload) ; image et
    -- vidéo portent une URL R2. Contrainte exprimée ici pour qu'aucune ligne
    -- inexploitable n'atteigne l'écran.
    CONSTRAINT cds_media_items_payload_check CHECK (
        (kind = 'qr' AND qr_payload IS NOT NULL)
        OR (kind IN ('image', 'video') AND url IS NOT NULL)
    ),
    CONSTRAINT cds_media_items_duration_check
        CHECK (duration_seconds BETWEEN 3 AND 120)
);

CREATE INDEX IF NOT EXISTS idx_cds_media_items_display
    ON cds_media_items (display_id, sort_order);
