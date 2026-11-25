-- Atlas (MySQL): server routing weight (docs/roadmap.md Routing strategy extension).
-- Operator-set steering weight for routing recommendations (canary ramp knob);
-- 100 = neutral, 0 = unset. Admin-owned column: the register upsert never
-- touches it (same rule as tags).

ALTER TABLE servers ADD COLUMN weight INT NOT NULL DEFAULT 0;
