-- LOT A Semaine 3, Chantier 9 (docs/decisions.md) : contenu correct des six
-- archétypes, remplaçant le contenu de 126_merchant_presets_seed (v1), qui
-- n'était qu'une proposition raisonnable non vérifiée contre le document de
-- référence (voir l'en-tête de ce fichier).
--
-- Deux bugs de v1 corrigés par ce contenu : "snack" avait manage_on_site =
-- false (un kebab ne pourrait pas encaisser sur place) ; "fast_food" avait un
-- plan de salle alors que la spécification prévoit un fonctionnement par
-- numéro de commande appelé, sans salle.
--
-- Nouvelle version plutôt qu'écrasement en place : merchant.preset_version
-- est figé sur le marchand à la création (voir 125_merchant_presets.up.sql).
-- Écraser le contenu des lignes v1 réécrirait rétroactivement la
-- configuration de référence d'un marchand déjà créé avec cette version.
-- Aucun marchand n'utilise encore de preset (colonne preset_code toujours
-- NULL en production à ce stade du projet), donc ce n'est pas un risque
-- aujourd'hui, mais le mécanisme doit être correct dès maintenant.
--
-- Champ renommé : "payments" -> "cash_handling". Il ne pointe que sur
-- cash_register_required_for_ordering/waiter_app_can_cash_in, qui ne sont pas
-- des moyens de paiement — le lot C introduira les vrais moyens d'encaissement
-- paramétrables sous un champ "payments" et il y aurait eu collision de nom.
-- Rien ne consomme encore ce champ (voir internal/modules/presets), donc
-- aucune donnée applicative n'est affectée par le renommage.
--
-- ATTENTION — ON CONFLICT (code, version) DO NOTHING rend ce fichier
-- idempotent (rejouable sans erreur) mais PAS réactualisable : une fois cette
-- migration appliquée quelque part, corriger un contenu v2 (comme la
-- répartition des tables) exige une v3, jamais une modification de ce
-- fichier suivie d'un nouveau `go run` — le INSERT ne réécrirait rien sur un
-- (code, version) déjà présent. Cette migration a été corrigée en place
-- pendant ce chantier (répartition Comptoir de brasserie) uniquement parce
-- qu'elle n'avait pas encore quitté ce dépôt (todo/, jamais appliquée en
-- production) ; ce n'est plus permis une fois passée en done/.

UPDATE merchant_presets SET is_active = false
WHERE code IN ('traditional', 'brasserie', 'pizzeria', 'fast_food', 'snack', 'bakery')
  AND version = 1;

INSERT INTO merchant_presets (id, code, version, label, description, naf_codes, config, is_active) VALUES

('prst-traditional-2', 'traditional', 2,
 'Restaurant traditionnel',
 'Service à table classique, salle et terrasse, carte structurée en entrées/plats/desserts.',
 ARRAY['56.10A'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 14, "seats": 4, "shape": "rectangle"},
        {"name": "Terrasse", "table_count": 6, "seats": 4, "shape": "square"}
    ]},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Entrées", "Plats", "Desserts", "Boissons", "Vins", "Menus"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 20, "minimum_preparation_time": 600, "maximum_preparation_time": 3600},
    "covers_required": true,
    "suggested_modules": ["reservation", "planning"]
 }'::jsonb,
 true),

('prst-brasserie-2', 'brasserie', 2,
 'Brasserie',
 'Service à table élargi, salle, comptoir et terrasse, carte bar + restauration.',
 ARRAY['56.30Z'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 13, "seats": 4, "shape": "rectangle"},
        {"name": "Comptoir", "table_count": 4, "seats": 1, "shape": "circle"},
        {"name": "Terrasse", "table_count": 8, "seats": 2, "shape": "square"}
    ]},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Boissons", "Bières", "Cocktails", "Planches", "Plats", "Desserts"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 15, "minimum_preparation_time": 300, "maximum_preparation_time": 2400},
    "covers_required": false,
    "suggested_modules": ["planning", "haccp"]
 }'::jsonb,
 true),

('prst-pizzeria-2', 'pizzeria', 2,
 'Pizzeria',
 'Sur place, à emporter et livraison, carte élargie autour de la pizza.',
 ARRAY['56.10A', '56.10C'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 10, "seats": 4, "shape": "rectangle"}
    ]},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": false},
    "categories": ["Pizzas", "Pâtes", "Salades", "Boissons", "Desserts", "Menus"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 25, "minimum_preparation_time": 900, "maximum_preparation_time": 3600},
    "covers_required": false,
    "suggested_modules": ["marketplaces", "delivery"]
 }'::jsonb,
 true),

('prst-fast_food-2', 'fast_food', 2,
 'Fast-food',
 'Comptoir, forte rotation, sur place/à emporter/livraison, sans plan de salle : fonctionnement par numéro de commande appelé.',
 ARRAY['56.10C'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": false, "zones": []},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": true},
    "categories": ["Burgers", "Menus", "Accompagnements", "Boissons", "Desserts"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 8, "minimum_preparation_time": 300, "maximum_preparation_time": 1800},
    "covers_required": false,
    "suggested_modules": ["kiosk", "marketplaces", "delivery"]
 }'::jsonb,
 true),

('prst-snack-2', 'snack', 2,
 'Snack',
 'Comptoir sans salle, sur place/à emporter/livraison, fonctionnement par numéro de commande appelé.',
 ARRAY['56.10C', '47.81Z'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": false, "zones": []},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": true},
    "categories": ["Sandwichs", "Assiettes", "Tacos", "Accompagnements", "Boissons", "Desserts"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 10, "minimum_preparation_time": 300, "maximum_preparation_time": 1800},
    "covers_required": false,
    "suggested_modules": ["marketplaces", "haccp"]
 }'::jsonb,
 true),

('prst-bakery-2', 'bakery', 2,
 'Boulangerie',
 'Comptoir, vente sur place et à emporter, plan de salle désactivé par défaut.',
 ARRAY['10.71C', '10.71D', '47.24Z'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": false, "zones": []},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Pains", "Viennoiseries", "Pâtisseries", "Sandwichs", "Boissons chaudes", "Formules"],
    "cash_handling": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 5, "minimum_preparation_time": 120, "maximum_preparation_time": 600},
    "covers_required": false,
    "suggested_modules": ["haccp", "planning"]
 }'::jsonb,
 true)

ON CONFLICT (code, version) DO NOTHING;
