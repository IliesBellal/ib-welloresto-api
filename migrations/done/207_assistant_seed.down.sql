-- GÉNÉRÉ par wello-back-office : npm run assistant:seed-sql
-- Source : src/lib/assistant/seed.ts (même vocabulaire que le modèle embarqué
-- du back-office). Ne pas modifier à la main : modifier le seed et régénérer.
--
-- Retire le pré-remplissage. Les intentions pré-remplies sont supprimées, AVEC
-- les synonymes appris qui leur sont rattachés (ON DELETE CASCADE).

BEGIN;

DELETE FROM assistant_lexicon WHERE source = 'seed';
DELETE FROM assistant_intents WHERE intent_key IN (
    'menu.product.out_of_stock',
    'menu.product.back_in_stock',
    'menu.product.create',
    'info.revenue',
    'info.top_products',
    'info.out_of_stock_list',
    'info.staff_on_shift',
    'info.bookings',
    'system.help',
    'system.logout',
    'nav.home',
    'nav.dashboard-analysis',
    'nav.dashboard-order-history',
    'nav.products',
    'nav.categories',
    'nav.tags',
    'nav.ingredients',
    'nav.price-grid',
    'nav.modifiers',
    'nav.promotions',
    'nav.floor-plan',
    'nav.reservations-list',
    'nav.reservations-settings',
    'nav.customers-list',
    'nav.loyalty-programs',
    'nav.equipiers',
    'nav.planning',
    'nav.my-planning',
    'nav.pointages',
    'nav.conges-echanges',
    'nav.equipe-parametres',
    'nav.roles',
    'nav.stocks',
    'nav.cash-registers-history',
    'nav.vat-declaration',
    'nav.financial-report',
    'nav.fiscal-archives',
    'nav.attestation',
    'nav.integrations-overview',
    'nav.market-categories',
    'nav.scannorder',
    'nav.uber-eats',
    'nav.deliveroo',
    'nav.accounting-sync',
    'nav.franchise-manage',
    'nav.franchise-hub',
    'nav.franchise-stats',
    'nav.haccp-activity',
    'nav.haccp-settings',
    'nav.kiosk-devices',
    'nav.kiosk-settings',
    'nav.cds',
    'nav.establishment',
    'nav.printers',
    'nav.production-profiles',
    'nav.profile',
    'nav.my-permissions',
    'nav.subscription'
);

COMMIT;
