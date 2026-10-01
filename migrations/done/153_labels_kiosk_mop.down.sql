-- Rollback de 153_labels_kiosk_mop.up.sql.
DELETE FROM labels WHERE label_value = 'KIOSK' AND label_type = 'mop' AND lang = 'FR' AND label = 'Borne de commande';
