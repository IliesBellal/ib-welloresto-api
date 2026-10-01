-- Nom de l'écran choisi par le restaurateur au moment de générer le code
-- d'enrôlement (back-office).
--
-- Jusqu'ici l'écran se nommait lui-même à l'enrôlement (« Écran Android
-- Box »), et le restaurateur devait le renommer ensuite. Le nom est
-- désormais porté par le code : il est recopié dans cds_displays.name quand
-- l'appareil consomme le code, et prime sur le nom envoyé par l'appareil.
--
-- Nullable : un code généré sans nom (ancien back-office, ou champ laissé
-- vide) retombe sur le nom envoyé par l'appareil, comme avant. Aucune
-- donnée existante à migrer.
--
-- Migration 147 déjà appliquée sur staging : on ajoute une colonne plutôt
-- que de la modifier.
ALTER TABLE cds_enrollment_codes
    ADD COLUMN IF NOT EXISTS display_name varchar(100);

COMMENT ON COLUMN cds_enrollment_codes.display_name IS 'Nom choisi au back-office pour l''écran à enrôler. Recopié dans cds_displays.name à la consommation du code ; prioritaire sur le nom envoyé par l''appareil.';
