-- Reverts 131_merchant_presets_v2.up.sql.
DELETE FROM merchant_presets WHERE id IN (
    'prst-traditional-2',
    'prst-brasserie-2',
    'prst-pizzeria-2',
    'prst-fast_food-2',
    'prst-snack-2',
    'prst-bakery-2'
);

UPDATE merchant_presets SET is_active = true
WHERE code IN ('traditional', 'brasserie', 'pizzeria', 'fast_food', 'snack', 'bakery')
  AND version = 1;
