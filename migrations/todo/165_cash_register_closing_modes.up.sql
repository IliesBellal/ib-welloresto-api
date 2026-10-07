-- Modes de clôture des registres de caisse (docs/EXPORT_COMPTABLE_MODES_CLOTURE.md).
--
-- MANUAL : clôture actuelle (fermeture, relevé de caisse et de TPE saisi en
-- cash_registers_custom_items, validation). L'export comptable reste celui
-- d'aujourd'hui (encaissements = réel des registres validés).
-- AUTO : le restaurateur ferme le registre sans relevé, la validation est
-- automatique ; l'export comptable ventile la TVA à partir des paiements et
-- se filtre par canal.
--
-- merchant_closing_modes : historique des modes, planifié par l'équipe
-- WelloResto uniquement (endpoints /admin, users.is_platform_staff), pour un
-- début de mois. Le mode en vigueur à une date est la ligne de date d'effet la
-- plus récente <= cette date ; sans ligne, AUTO (défaut des nouveaux
-- établissements). Les dates sont des dates du calendrier de l'établissement
-- (merchant.timezone). Une ligne dont la date d'effet est passée n'est jamais
-- modifiée ni supprimée : c'est elle qui permet de régénérer un ancien export.
--
-- cash_registers.closing_mode : mode inscrit sur le registre à son ouverture
-- (d'après sa date d'ouverture), pour qu'un registre ouvert le 31 au soir et
-- fermé le 1er garde le mode du 31.
--
-- ORDRE DE DÉPLOIEMENT : appliquer AVANT de déployer le code (le code lit et
-- écrit ces objets). Rejouable : IF NOT EXISTS / ON CONFLICT / WHERE ... IS NULL.

CREATE TABLE IF NOT EXISTS merchant_closing_modes (
    id             bigserial    PRIMARY KEY,
    merchant_id    varchar(64)  NOT NULL,
    mode           varchar(10)  NOT NULL,
    effective_from date         NOT NULL,
    created_by     varchar(64)  NOT NULL,
    created_at     timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT chk_merchant_closing_modes_mode CHECK (mode IN ('MANUAL', 'AUTO')),
    CONSTRAINT uq_merchant_closing_modes_merchant_date UNIQUE (merchant_id, effective_from)
);

COMMENT ON TABLE merchant_closing_modes IS 'Historique des modes de clôture des registres (MANUAL / AUTO) par établissement, planifié par l''équipe WelloResto. Mode en vigueur à une date = ligne de date d''effet la plus récente <= cette date ; aucune ligne = AUTO.';
COMMENT ON COLUMN merchant_closing_modes.effective_from IS 'Premier jour (calendrier de l''établissement, merchant.timezone) à partir duquel les registres ouverts suivent ce mode.';

ALTER TABLE cash_registers
    ADD COLUMN IF NOT EXISTS closing_mode varchar(10) NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_cash_registers_closing_mode') THEN
        ALTER TABLE cash_registers
            ADD CONSTRAINT chk_cash_registers_closing_mode
            CHECK (closing_mode IS NULL OR closing_mode IN ('MANUAL', 'AUTO'));
    END IF;
END $$;

COMMENT ON COLUMN cash_registers.closing_mode IS 'Mode de clôture (MANUAL / AUTO) inscrit à l''ouverture du registre d''après merchant_closing_modes. NULL = registre antérieur à la migration 165, traité comme MANUAL.';

-- Tous les établissements existants restent en clôture manuelle, depuis
-- toujours. Base : la table merchant (et non merchant_parameters, où un
-- établissement peut ne pas avoir de ligne).
INSERT INTO merchant_closing_modes (merchant_id, mode, effective_from, created_by)
SELECT CAST(m.id AS varchar(64)), 'MANUAL', DATE '1970-01-01', 'migration-165'
FROM merchant m
ON CONFLICT (merchant_id, effective_from) DO NOTHING;

-- Tous les registres existants ont été clôturés en manuel.
UPDATE cash_registers SET closing_mode = 'MANUAL' WHERE closing_mode IS NULL;
