-- Migration 10: Add NoMetadata/Abandoned statuses and retry_count
-- Status values after this migration:
--   0 = Processing
--   1 = Done (has real linked metadata)
--   2 = NoMedia (no video files)
--   3 = NoMetadata (NEW: video found, no mapper matched — retryable with 1h cooldown)
--   4 = Error (was 3)
--   5 = Forbidden (was 4)
--   6 = Abandoned (NEW: exceeded MAX_RETRIES, only force can retry)

-- 1. Add retry_count column
ALTER TABLE media_info ADD COLUMN IF NOT EXISTS retry_count int2 NOT NULL DEFAULT 0;

-- 2. Renumber existing Error (3→4) and Forbidden (4→5) — order matters
UPDATE media_info SET status = 5 WHERE status = 4; -- Forbidden: 4 → 5
UPDATE media_info SET status = 4 WHERE status = 3; -- Error:     3 → 4

-- 3. Backfill: Done rows with no real linked metadata → NoMetadata (3)
--    These are resources where enrichment "completed" but no mapper actually matched.
UPDATE media_info mi
SET status = 3
WHERE mi.status = 1  -- currently Done
  AND NOT EXISTS (
    SELECT 1 FROM movie m
    JOIN movie_metadata md ON m.movie_metadata_id = md.movie_metadata_id
    WHERE m.resource_id = mi.resource_id
      AND (
        (md.poster_url  IS NOT NULL AND md.poster_url  != '') OR
        (md.video_id    IS NOT NULL AND md.video_id    != '')
      )
  )
  AND NOT EXISTS (
    SELECT 1 FROM series s
    JOIN series_metadata sd ON s.series_metadata_id = sd.series_metadata_id
    WHERE s.resource_id = mi.resource_id
      AND (
        (sd.poster_url  IS NOT NULL AND sd.poster_url  != '') OR
        (sd.video_id    IS NOT NULL AND sd.video_id    != '')
      )
  );
