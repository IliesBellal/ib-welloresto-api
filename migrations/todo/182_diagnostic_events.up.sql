-- Journal de diagnostic générique (docs/DIAGNOSTIC_EVENTS.md) : une ligne par
-- anomalie ou fait à garder pour comprendre un problème après coup, tous
-- domaines confondus. Premier usage : 'order_price_mismatch' (le total d'une
-- commande ne correspond pas à la somme de ses lignes), qui a laissé 12 € de
-- ventes sans base de TVA dans le registre 934 du 5 octobre 2026.
--
--   kind          type d'événement, en minuscules et underscores, stable : c'est
--                 la clé de filtre et de purge ('order_price_mismatch',
--                 'order_overpaid'...).
--   severity      'info', 'warn' ou 'error'.
--   merchant_id / user_id   contexte de la requête, quand il existe (texte : les
--                 identifiants utilisateur ne sont pas tous numériques).
--   request_id    identifiant de requête des logs applicatifs.
--   subject_type / subject_id   l'objet concerné ('order' / '38912').
--   data          le contenu propre au type, libre (jsonb).
--
-- Écrit par l'application hors de toute transaction métier, sans bloquer le
-- flux appelant : une panne d'écriture ne doit jamais empêcher d'encaisser (le
-- code retombe alors sur un log applicatif). Purgée par tâche planifiée
-- (180 jours). Aucune FK (convention de ce périmètre).
--
-- ORDRE DE DÉPLOIEMENT : avant ou avec le code ; si la table manque, le code
-- continue de fonctionner et journalise seulement dans les logs applicatifs.
CREATE TABLE IF NOT EXISTS diagnostic_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at   timestamptz NOT NULL DEFAULT now(),
    kind         varchar(64)  NOT NULL,
    severity     varchar(10)  NOT NULL DEFAULT 'warn',
    merchant_id  varchar(64),
    user_id      varchar(64),
    request_id   varchar(64),
    subject_type varchar(32),
    subject_id   varchar(64),
    data         jsonb        NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_diagnostic_events_kind_created
    ON diagnostic_events (kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_diagnostic_events_subject
    ON diagnostic_events (subject_type, subject_id);
CREATE INDEX IF NOT EXISTS idx_diagnostic_events_created
    ON diagnostic_events (created_at);
