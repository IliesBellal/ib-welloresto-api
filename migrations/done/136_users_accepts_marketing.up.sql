-- LOT A Semaine 3, Chantier 14 (docs/decisions.md) : POST /v1/signup's
-- accepts_marketing consent (docs/WelloResto-Parcours-Client-v2.docx §5.6)
-- had no column to write to.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS accepts_marketing boolean NOT NULL DEFAULT false;
