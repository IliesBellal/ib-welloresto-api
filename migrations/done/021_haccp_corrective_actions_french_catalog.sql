INSERT INTO haccp_corrective_actions (id, code, label, description, severity_scope)
VALUES
    (
        'haccp-ca-adjust-thermostat',
        'adjust_thermostat',
        'Abaissement de la température de la zone',
        'Abaisser la température de la zone ou ajuster le thermostat, puis vérifier un nouveau relevé.',
        'both'
    ),
    (
        'haccp-ca-move-product',
        'move_product',
        'Denrées déplacées',
        'Déplacer immédiatement les denrées vers une zone de stockage conforme.',
        'both'
    ),
    (
        'haccp-ca-discard-product',
        'discard_product',
        'Denrées éliminées',
        'Éliminer les denrées non conformes selon les règles HACCP.',
        'critical'
    ),
    (
        'haccp-ca-clean-sensor',
        'clean_or_replace_sensor',
        'Sonde nettoyée ou remplacée',
        'Nettoyer ou remplacer la sonde, puis refaire une prise de température.',
        'both'
    ),
    (
        'haccp-ca-other',
        'other',
        'Autre',
        'À utiliser si aucune action corrective prédéfinie ne correspond au cas observé.',
        'both'
    ),
    (
        'haccp-ca-door-open-closed',
        'door_open_then_closed',
        'Porte ouverte puis refermée',
        'Constater l''ouverture accidentelle de la porte, la refermer et surveiller le retour à une température conforme.',
        'both'
    ),
    (
        'haccp-ca-equipment-out-of-service',
        'equipment_out_of_service',
        'Enceinte HS',
        'Déclarer l''enceinte hors service et isoler immédiatement les denrées à risque.',
        'critical'
    ),
    (
        'haccp-ca-call-technician',
        'call_technician',
        'Appel d''un technicien',
        'Contacter un technicien pour diagnostic ou intervention sur l''équipement.',
        'critical'
    ),
    (
        'haccp-ca-defrost-in-progress',
        'defrost_in_progress',
        'Enceinte en cours de dégivrage',
        'Indiquer qu''un cycle de dégivrage est en cours et contrôler à nouveau la température à son issue.',
        'both'
    )
ON DUPLICATE KEY UPDATE
    label = VALUES(label),
    description = VALUES(description);