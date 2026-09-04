-- PROMPT 10 (Annulations tab, docs/analytics §5.3, wello-back-office repo):
-- adds the one new permission key the analytics catalog was missing —
-- reports.staff_performance.read. It guards the only data on the Analyse
-- page that identifies a physical employee and evaluates their performance
-- (per-server cancellation ranking today, per-server upsell ranking once
-- lot 11 wires it) — a class of sensitivity the catalog already expresses
-- for customer data (customers.manage, is_sensitive=true) but never had to
-- express for staff data before this screen existed.
--
-- Deliberately NOT reused: reports.sales.read already gates the aggregate
-- side of this same tab (POST /analytics/cancellations) and every other
-- analytics endpoint. Since RequirePermission takes exactly one
-- permission.Key (no AnyOf/AllOf since RBAC lot 2), a single shared key
-- across both endpoints would mean a user with only reports.sales.read
-- could read the nominative ranking through this door even though the
-- customer-data precedent (customers.manage) says a nominative breakdown
-- gets its own right.
--
-- sort_order 135: between reports.financial.read (130) and settings.manage
-- (140) — same reports.* group, positioned right after its sibling.
--
-- Idempotent like migrations 095/097/103 (ON CONFLICT DO NOTHING).
-- Single source of truth: internal/permission/keys_gen.go must carry the
-- matching Go constant in the same change (keys_gen_test.go enforces this),
-- and the key must guard at least one real route in cmd/api/routes.go
-- (routes_rbac_permission_coverage_test.go enforces this).

INSERT INTO permissions (key, domain, label, description, is_sensitive, sort_order) VALUES
    ('reports.staff_performance.read', 'reports', 'Consulter les analyses nominatives par salarié',
     'Accéder aux classements et statistiques nominatifs par membre de l''équipe (upsell, annulations).', true, 135)
ON CONFLICT (key) DO NOTHING;
