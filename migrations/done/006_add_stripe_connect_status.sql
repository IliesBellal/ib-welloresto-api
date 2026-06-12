-- Migration 006: Add verification_status cache to stripe_accounts
-- Cached from Stripe webhooks (account.updated) to avoid blocking the dashboard on a Stripe API call.

ALTER TABLE stripe_accounts
    ADD COLUMN verification_status VARCHAR(50) NOT NULL DEFAULT 'action_required'
        COMMENT '"verified" | "action_required" — mirrored from Stripe account.charges_enabled + payouts_enabled';
