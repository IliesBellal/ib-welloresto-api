-- Periodes de vacances/fermeture exceptionnelle d'un etablissement.
-- Consommee par pos.GetPOSStatus (internal/modules/pos/repository.go) au meme
-- titre que planning_holiday_overrides (migration 014) : si l'instant courant
-- (heure locale du marchand) tombe dans une periode enabled, l'etablissement
-- est force ferme, comme s'il etait hors horaires d'ouverture.
--
-- start_at/end_at sont des timestamp "naifs" (sans fuseau), au meme titre que
-- hours_of_operation.valid_from/valid_to : la valeur stockee est l'heure
-- locale du marchand telle que saisie, comparee telle quelle a l'heure
-- locale courante (voir openinghours.FetchActiveSlots pour le meme motif).
CREATE TABLE IF NOT EXISTS planning_vacation_periods (
    id varchar(64) NOT NULL,
    merchant_id varchar(64) NOT NULL,
    label varchar(255),
    start_at timestamp NOT NULL,
    end_at timestamp NOT NULL,
    enabled boolean NOT NULL DEFAULT TRUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (id)
);

CREATE INDEX IF NOT EXISTS idx_planning_vacation_periods_merchant_range
    ON planning_vacation_periods (merchant_id, start_at, end_at);
