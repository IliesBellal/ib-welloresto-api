-- 062 — kiosks.device_id : identifiant device résolu côté OS (Android ID /
-- identifierForVendor iOS), capturé à l'enrôlement pour permettre une future
-- ré-identification si le refresh token est perdu (stockage effacé, refresh
-- token expiré après une longue coupure) — voir
-- docs/KIOSK_ENROLLMENT_RESILIENCE_AUDIT.md. Ce n'est pas un secret : aucune
-- valeur d'authentification n'est attachée à ce champ à ce stade (fondation
-- uniquement — l'endpoint /kiosk/auth/reclaim n'existe pas encore).
--
-- Pas de contrainte UNIQUE ici : l'unicité applicative (un device_id
-- dupliqué doit bloquer un futur reclaim plutôt que faire échouer une
-- insertion) sera gérée au niveau de ce futur endpoint, pas en DB.
-- VARCHAR(191) plutôt que 255 pour rester indexable sans dépasser la limite
-- de clé InnoDB (utf8mb4) si un index est ajouté plus tard.
--
-- Les bornes déjà enrôlées avant ce déploiement gardent device_id = NULL
-- (défaut de la colonne) et continuent sur le flow d'enrôlement classique —
-- aucun backfill de données requis.

ALTER TABLE kiosks
    ADD COLUMN device_id VARCHAR(191) NULL DEFAULT NULL;
