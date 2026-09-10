-- RBAC lot 2, section 1: closes the gap opened by lot 1.
--
-- Lot 1's pre-check found that access_wrreception was still a live gate on
-- PATCH /pos/status (open/close the establishment) and, on explicit
-- instruction, removed it without a replacement — leaving that route with no
-- authorization check at all for the time it took to reach lot 2. This adds
-- the permission that replaces it.
--
-- sort_order 15 places it right after pos.access (10) and before
-- pos.ticket.reopen (20): both are "core" POS actions, distinct from the
-- ticket/discount/refund/cash-drawer actions that follow.
--
-- Idempotent like migration 095 (ON CONFLICT DO NOTHING) — safe to replay.
INSERT INTO permissions (key, domain, label, is_sensitive, sort_order) VALUES
    ('pos.status.manage', 'pos', 'Ouvrir et fermer l''établissement', false, 15)
ON CONFLICT (key) DO NOTHING;
