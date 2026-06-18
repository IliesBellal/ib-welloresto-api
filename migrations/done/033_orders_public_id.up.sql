-- Adds an opaque, non-sequential public identifier to orders, for use in
-- customer-facing URLs (e.g. the phase-2 delivery tracking page,
-- GET /track/{ref} - see docs/DELIVERY_DESIGN.md §4). Replaces the sequential
-- order_id as the externally-shared reference, removing the enumeration risk
-- previously noted for that endpoint.
--
-- [A VERIFIER] RANDOM_BYTES() requires MySQL >= 5.6.36 (with the relevant patch)
-- and is NOT available on MariaDB. If the target server does not support it,
-- replace RANDOM_BYTES(16) with UUID() below - note that UUID() v1 embeds a
-- timestamp and a MAC-address-derived node ID, making it less opaque than
-- RANDOM_BYTES, but still non-sequential.

ALTER TABLE orders
    ADD COLUMN public_id VARCHAR(45) NULL,
    ADD UNIQUE INDEX idx_orders_public_id (public_id);
