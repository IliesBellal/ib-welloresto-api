-- PROMPT 27 Phase 2 — table de suivi des migrations appliquées à la main.
--
-- Ce dépôt n'a pas d'outil de migration (CLAUDE.md, "no migration tool") et
-- ne va pas s'en donner un : cette table ne rejoue rien automatiquement, elle
-- se contente d'enregistrer ce qui a réellement été joué, à la main, contre
-- une base donnée — le geste qui a manqué et qui a coûté deux sessions
-- entières de reconstitution d'état
-- (docs/migration-postgres/67-migration-status-audit.md).
--
-- Une seule ligne par migration RÉELLEMENT appliquée sur CETTE base précise.
-- Ne pas copier les lignes d'une base vers une autre : staging et production
-- divergent déjà (voir 67-migration-status-audit.md §3.2) et continueront de
-- diverger. Le remplissage rétroactif de chaque base se fait depuis son
-- propre diagnostic (cmd/diagnose_migrations), jamais par recopie.
--
-- Pas de suivi de rollback (down_applied) : ce projet n'a pas d'outil qui
-- rejoue les .down.sql automatiquement, un rollback reste un geste manuel
-- documenté ailleurs (commit, incident) — inutile de le tracer ici en plus.
--
-- Additive, table neuve : sans risque, aucun code déployé ne la lit avant le
-- contrôle au démarrage de l'API (internal/database/schemamigrations.go),
-- livré dans le même lot.
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     varchar(20)  PRIMARY KEY, -- "087", "094", "103a"/"103b" (les deux fichiers "103" partagent un numéro, voir migrations/migrations_numbering_test.go)
    filename    varchar(255) NOT NULL,    -- nom exact du fichier .up.sql, pour retrouver la source même si un numéro est renuméroté un jour (cf. le renommage historique 089->094)
    applied_at  timestamptz  NOT NULL DEFAULT now(),
    applied_by  varchar(150),             -- qui a joué la migration à la main (nom/email) ; NULL pour le remplissage rétroactif initial, dont l'auteur exact n'est plus traçable
    notes       text                      -- ex. "réécrite en syntaxe Postgres avant exécution, voir 101/102/103b"
);

CREATE INDEX IF NOT EXISTS idx_schema_migrations_applied_at ON schema_migrations (applied_at);
