-- Atlas: server routing weight (docs/roadmap.md Routing 策略扩展).
-- Operator-set steering weight for routing recommendations (灰度放量 knob):
-- routing ranks candidates on effective load = load × 100 / weight, so a
-- weight above 100 attracts more recommendations and below 100 dampens.
-- 100 = neutral, 0 = unset (both behave the same). Admin-owned column: the
-- register upsert never touches it (same rule as tags).

BEGIN;

ALTER TABLE servers ADD COLUMN IF NOT EXISTS weight INT NOT NULL DEFAULT 0;

COMMIT;
