-- Kiosk module: per-merchant configurable application fees for Stripe
-- Terminal card-present payments (application_fee_amount), decoupled from
-- scannorder_settings.variable_fees/fixed_fees which CreateTerminalPaymentIntent
-- was reading from until now (see docs/KIOSK_DECISIONS.md, "Incrément Terminal 3").
--
-- Defaults (0.0070, 15) mirror scannorder_settings' current defaults for
-- cross-channel pricing consistency at rollout — existing merchants keep the
-- same effective commission after this migration, no backfill needed.
--
-- Not exposed in GET /kiosk/settings nor GET /pos/settings/kiosk/settings —
-- read only by kiosk.Repository.GetKioskFees (internal use).

ALTER TABLE kiosk_settings
  ADD COLUMN variable_fees DECIMAL(10,4) NOT NULL DEFAULT 0.0070
    COMMENT 'Frais variables plateforme (ex: 0.007 = 0.7%)'
    AFTER pay_at_counter_enabled,
  ADD COLUMN fixed_fees INT NOT NULL DEFAULT 15
    COMMENT 'Frais fixes plateforme en centimes (ex: 15 = 0.15€)'
    AFTER variable_fees;
