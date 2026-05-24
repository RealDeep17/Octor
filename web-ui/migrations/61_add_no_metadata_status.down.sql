-- Revert migration 61: restore original status values
UPDATE media_info SET status = 3 WHERE status = 4; -- Error: 4 → 3
UPDATE media_info SET status = 4 WHERE status = 5; -- Forbidden: 5 → 4
UPDATE media_info SET status = 1 WHERE status IN (3, 6);
ALTER TABLE media_info DROP COLUMN IF EXISTS retry_count;
