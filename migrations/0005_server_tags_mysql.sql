-- Atlas (MySQL): server tag system (docs/concepts.md §服务器标记).
-- Operator-set markers on a server record. JSON array of
-- {code,label,tier,public}; TEXT because MySQL cannot put a DEFAULT on
-- TEXT/JSON columns, so rows read NULL until an operator first writes tags
-- (scanServer treats NULL as "no tags"). The register upsert never touches
-- this column.

ALTER TABLE servers ADD COLUMN tags TEXT NULL;
