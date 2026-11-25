-- Atlas: server routing weight (docs/roadmap.md Routing strategy extension).
-- Operator-set steering weight for routing recommendations (canary ramp knob):
-- routing ranks candidates on effective load = load × 100 / weight, so a
-- weight above 100 attracts more recommendations and below 100 dampens.
-- 100 = neutral, 0 = unset (both behave the same). Admin-owned column: the
-- register upsert never touches it (same rule as tags).

BEGIN;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS weight INT NOT NULL DEFAULT 0;

COMMIT;
