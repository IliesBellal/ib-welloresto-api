-- LOT A Semaine 2, Chantier 5b (docs/decisions.md) : seed des six archétypes
-- de marchand.
--
-- ATTENTION — contenu NON tiré de docs/parcours-client-v2.docx §5.5.2 (fichier
-- non accessible pendant ce chantier). Labels, catégories, tailles de plan de
-- salle, temps de préparation et codes NAF sont une proposition raisonnable,
-- à vérifier contre le document de référence. Voir docs/decisions.md pour le
-- détail de cette réserve.
--
-- config (JSONB) suit exactement le schéma confirmé au chantier 5b :
-- channels / floor_plan / kitchen / categories / payments / prep_times /
-- covers_required / suggested_modules (ce dernier informatif uniquement,
-- jamais appliqué par ApplyPreset). Deux champs évoqués par le brief initial
-- (kitchen.grouping, printing) ont été retirés faute de colonne réelle ;
-- "payments" pointe sur cash_register_required_for_ordering/waiter_app_can_cash_in,
-- les deux seules colonnes merchant_parameters qui s'en approchent.
--
-- Idempotent (ON CONFLICT (code, version) DO NOTHING), même convention que
-- 095_roles_permissions_catalog.

INSERT INTO merchant_presets (id, code, version, label, description, naf_codes, config, is_active) VALUES

('prst-traditional-1', 'traditional', 1,
 'Restaurant traditionnel',
 'Service à table classique, carte structurée en entrées/plats/desserts.',
 ARRAY['56.10A'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 12, "seats": 4, "shape": "rectangle"}
    ]},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Entrées", "Plats", "Desserts", "Boissons", "Vins"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 20, "minimum_preparation_time": 600, "maximum_preparation_time": 3600},
    "covers_required": true,
    "suggested_modules": ["planning", "haccp"]
 }'::jsonb,
 true),

('prst-brasserie-1', 'brasserie', 1,
 'Brasserie',
 'Service à table élargi, salle et terrasse, carte bar + restauration.',
 ARRAY['56.10A', '56.30Z'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 10, "seats": 4, "shape": "rectangle"},
        {"name": "Terrasse", "table_count": 6, "seats": 2, "shape": "square"}
    ]},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Entrées", "Plats", "Desserts", "Boissons", "Bar"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 15, "minimum_preparation_time": 300, "maximum_preparation_time": 2400},
    "covers_required": true,
    "suggested_modules": ["planning", "haccp"]
 }'::jsonb,
 true),

('prst-pizzeria-1', 'pizzeria', 1,
 'Pizzeria',
 'Sur place, à emporter et livraison, carte centrée pizzas.',
 ARRAY['56.10A', '56.10C'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 6, "seats": 4, "shape": "rectangle"}
    ]},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": false},
    "categories": ["Pizzas", "Entrées", "Desserts", "Boissons"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": true},
    "prep_times": {"mode": "AUTO", "preparation_time": 15, "minimum_preparation_time": 600, "maximum_preparation_time": 1800},
    "covers_required": false,
    "suggested_modules": ["delivery", "haccp"]
 }'::jsonb,
 true),

('prst-fast_food-1', 'fast_food', 1,
 'Fast-food',
 'Comptoir, forte rotation, sur place/à emporter/livraison.',
 ARRAY['56.10C'],
 '{
    "channels": {"on_site": true, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": true, "zones": [
        {"name": "Salle", "table_count": 8, "seats": 4, "shape": "square"}
    ]},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": true},
    "categories": ["Menus", "Burgers", "Accompagnements", "Boissons", "Desserts"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 8, "minimum_preparation_time": 180, "maximum_preparation_time": 900},
    "covers_required": false,
    "suggested_modules": ["kiosk", "scannorder"]
 }'::jsonb,
 true),

('prst-snack-1', 'snack', 1,
 'Snack',
 'Comptoir sans salle, à emporter et livraison.',
 ARRAY['56.10C'],
 '{
    "channels": {"on_site": false, "take_away": true, "delivery": true},
    "floor_plan": {"enabled": false, "zones": []},
    "kitchen": {"display": "PRODUCT_FOCUS", "call_numbers": true},
    "categories": ["Sandwichs", "Salades", "Boissons", "Snacks"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 5, "minimum_preparation_time": 120, "maximum_preparation_time": 600},
    "covers_required": false,
    "suggested_modules": ["kiosk", "scannorder"]
 }'::jsonb,
 true),

('prst-bakery-1', 'bakery', 1,
 'Boulangerie',
 'Comptoir sans salle, produits majoritairement prêts à la vente.',
 ARRAY['10.71C', '47.24Z'],
 '{
    "channels": {"on_site": false, "take_away": true, "delivery": false},
    "floor_plan": {"enabled": false, "zones": []},
    "kitchen": {"display": "CLASSIC", "call_numbers": false},
    "categories": ["Pains", "Viennoiseries", "Pâtisseries", "Sandwichs", "Boissons"],
    "payments": {"cash_register_required_for_ordering": true, "waiter_app_can_cash_in": false},
    "prep_times": {"mode": "AUTO", "preparation_time": 10, "minimum_preparation_time": 300, "maximum_preparation_time": 1200},
    "covers_required": false,
    "suggested_modules": []
 }'::jsonb,
 true)

ON CONFLICT (code, version) DO NOTHING;
