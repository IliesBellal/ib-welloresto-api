-- Synchronisation comptable (docs/accounting-sync.md) : chaque établissement peut
-- relier UN logiciel de comptabilité (Pennylane pour l'instant) auquel Wello Resto
-- envoie, chaque jour, l'écriture de ventes de sa clôture fiscale journalière.
--
-- Deux tables :
--
--   accounting_connections : la connexion d'un établissement à un outil. Une seule
--     par établissement (clé primaire = merchant_id) ; en changer = déconnecter puis
--     reconnecter. Les identifiants (jeton d'API ou jetons OAuth) sont chiffrés en
--     AES-256-GCM par l'application (variable ACCOUNTING_ENCRYPTION_KEY) : jamais en
--     clair, jamais renvoyés par l'API.
--       status      : 'setup'  connecté, correspondance des comptes pas encore validée
--                     'active' la synchronisation tourne
--                     'error'  identifiants refusés par l'outil, reconnexion nécessaire
--       config      : correspondance des comptes comptables (numéros, propres à
--                     l'établissement), indépendante de l'outil.
--       state       : identifiants internes à l'outil résolus à partir de config
--                     (ex. id de journal et de comptes Pennylane).
--       sync_from   : premier jour envoyé (lendemain de l'activation) ; les jours
--                     antérieurs relèvent de la reprise d'historique, faite à part.
--
--   accounting_sync_items : un journal, une ligne par établissement, outil et jour
--     envoyé. L'unicité (merchant_id, provider, period_start) garantit qu'un jour
--     n'est jamais envoyé deux fois, y compris si le traitement est rejoué.
--       status : 'sent'      écriture créée chez l'outil (external_id renseigné)
--                'failed'    échec avéré (l'outil a répondu par un refus) : retenté
--                'uncertain' l'envoi a pu aboutir sans qu'on le sache (délai dépassé,
--                            erreur serveur) : PAS retenté automatiquement, car l'API
--                            Pennylane n'a pas de clé d'idempotence et un second envoi
--                            créerait un doublon ; à vérifier chez l'outil puis à
--                            relancer depuis le back-office
--                'skipped'   jour sans vente ni encaissement, rien à envoyer
--                'abandoned' trop d'échecs, relance manuelle nécessaire
--       closure_hash : empreinte de la clôture fiscale envoyée (traçabilité).
--       entry        : l'écriture telle qu'envoyée (audit).
--       difference   : écart (centimes) entre ventes et encaissements, porté par le
--                      compte d'écart de l'écriture.
--
-- Aucune FK (convention de ce périmètre). ORDRE DE DÉPLOIEMENT : avant le code (la
-- tâche horaire lit ces tables). La variable ACCOUNTING_ENCRYPTION_KEY
-- (`openssl rand -base64 32`) doit être définie avant la première connexion.
CREATE TABLE IF NOT EXISTS accounting_connections (
    merchant_id   varchar(64)  PRIMARY KEY,
    provider      varchar(32)  NOT NULL,
    status        varchar(16)  NOT NULL DEFAULT 'setup' CHECK (status IN ('setup', 'active', 'error')),
    credentials   bytea        NOT NULL,
    config        jsonb        NOT NULL DEFAULT '{}'::jsonb,
    state         jsonb        NOT NULL DEFAULT '{}'::jsonb,
    account_label varchar(255),
    sync_from     date,
    connected_at  timestamptz  NOT NULL DEFAULT now(),
    activated_at  timestamptz,
    last_sync_at  timestamptz,
    last_error    text,
    updated_at    timestamptz  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_accounting_connections_active ON accounting_connections (merchant_id) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS accounting_sync_items (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    merchant_id  varchar(64)  NOT NULL,
    provider     varchar(32)  NOT NULL,
    period_start date         NOT NULL,
    closure_id   bigint,
    closure_hash varchar(64),
    status       varchar(16)  NOT NULL CHECK (status IN ('sent', 'failed', 'uncertain', 'skipped', 'abandoned')),
    attempts     integer      NOT NULL DEFAULT 0,
    external_id  varchar(128),
    entry        jsonb,
    difference   bigint       NOT NULL DEFAULT 0,
    last_error   text,
    created_at   timestamptz  NOT NULL DEFAULT now(),
    sent_at      timestamptz,
    updated_at   timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT uq_accounting_sync_items_day UNIQUE (merchant_id, provider, period_start)
);

CREATE INDEX IF NOT EXISTS idx_accounting_sync_items_retry ON accounting_sync_items (merchant_id, period_start) WHERE status = 'failed';

COMMENT ON TABLE accounting_connections IS 'Connexion d''un établissement à un logiciel de comptabilité (une seule par établissement). Identifiants chiffrés par l''application.';
COMMENT ON TABLE accounting_sync_items IS 'Journal des jours envoyés au logiciel de comptabilité, un par établissement, outil et jour (unicité = pas de doublon).';
COMMENT ON COLUMN accounting_sync_items.status IS 'sent | failed (retenté) | uncertain (jamais retenté seul : peut exister chez l''outil) | skipped | abandoned';
