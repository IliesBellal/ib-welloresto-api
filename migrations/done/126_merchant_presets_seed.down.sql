-- Reverts 126_merchant_presets_seed.up.sql.
DELETE FROM merchant_presets WHERE id IN (
    'prst-traditional-1',
    'prst-brasserie-1',
    'prst-pizzeria-1',
    'prst-fast_food-1',
    'prst-snack-1',
    'prst-bakery-1'
);
