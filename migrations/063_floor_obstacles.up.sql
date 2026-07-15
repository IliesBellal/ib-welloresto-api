-- ============================================================================
-- 063 — floor_obstacles : murs, bar, escaliers, portes du plan de salle.
--
-- Prérequis du moteur de placement automatique (bloc ultérieur) qui calculera
-- l'adjacence entre tables en vérifiant qu'aucun obstacle ne coupe la ligne
-- entre deux tables.
--
-- merchant_id en VARCHAR(64) : même convention que floors.merchant_id et
-- locations.merchant_id (baseline 050). floor_id en VARCHAR(64) : aligné sur
-- floors.id post-062 (conversion INT → VARCHAR(64) des tables du plan de salle).
-- ============================================================================

CREATE TABLE floor_obstacles (
  id          VARCHAR(64)  NOT NULL,
  floor_id    VARCHAR(64)  NOT NULL,
  merchant_id VARCHAR(64)  NOT NULL,
  type        ENUM('wall','bar','stairs','door') NOT NULL,
  x           FLOAT        NOT NULL DEFAULT 0,
  y           FLOAT        NOT NULL DEFAULT 0,
  width       FLOAT        NOT NULL DEFAULT 60,
  height      FLOAT        NOT NULL DEFAULT 20,
  angle       FLOAT        NOT NULL DEFAULT 0,
  direction   FLOAT                 DEFAULT NULL
              COMMENT 'Portes uniquement : angle d ouverture de l arc (degrés)',
  enabled     TINYINT(1)   NOT NULL DEFAULT 1,
  created_at  DATETIME     NOT NULL DEFAULT UTC_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_floor_obstacles_floor_id    (floor_id),
  INDEX idx_floor_obstacles_merchant_id (merchant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
