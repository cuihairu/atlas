-- Atlas (MySQL): server routing weight (docs/roadmap.md Routing 策略扩展).
-- Operator-set steering weight for routing recommendations (灰度放量 knob);
-- 100 = neutral, 0 = unset. Admin-owned column: the register upsert never
-- touches it (same rule as tags).

ALTER TABLE servers ADD COLUMN weight INT NOT NULL DEFAULT 0;
