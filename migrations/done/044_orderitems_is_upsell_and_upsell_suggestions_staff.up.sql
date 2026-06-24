-- Attribution upsell : marque les lignes de commande créées depuis une suggestion
-- upsell. NF525 : on ajoute uniquement une colonne avec DEFAULT, aucune colonne
-- existante n'est modifiée et aucun calcul de hash/signature fiscale ne dépend
-- de la structure de orderitems (le hash NF525 porte sur le Receipt déjà figé,
-- pas sur le schéma de la table).
ALTER TABLE orderitems
    ADD COLUMN IF NOT EXISTS is_upsell TINYINT(1) NOT NULL DEFAULT 0 AFTER delay_id;

-- Préparation d'un futur funnel d'attribution serveur sur les suggestions upsell.
-- Colonne créée mais non peuplée dans ce sprint.
ALTER TABLE upsell_suggestions
    ADD COLUMN IF NOT EXISTS staff_member_id VARCHAR(64) NULL DEFAULT NULL;
