-- Numéro de révision du plan de salle, par établissement.
--
-- L'éditeur du back-office travaille en brouillon puis publie tout le plan en
-- une seule transaction (POST /locations/plan/publish). Il envoie la révision
-- sur laquelle son brouillon est basé ; si quelqu'un a publié entre-temps, la
-- révision courante est plus grande et la publication est refusée (409
-- plan_revision_conflict) au lieu d'écraser silencieusement son travail.
--
-- Une ligne par établissement, créée à la première publication (absence de
-- ligne = révision 0). Seules les publications par lot incrémentent ce
-- compteur ; les routes unitaires historiques (PATCH /locations/tables/{id}…)
-- ne le touchent pas.
--
-- ORDRE DE DÉPLOIEMENT : migration AVANT le code. Rejouable.

CREATE TABLE IF NOT EXISTS floor_plan_revisions (
    merchant_id varchar(64) PRIMARY KEY,
    revision    bigint      NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE floor_plan_revisions IS 'Révision du plan de salle par établissement (contrôle de concurrence de la publication par lot).';
