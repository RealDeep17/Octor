-- tmdb.info is queried by imdb_id in three hot paths:
--   MapByID("tt..."), GetTmdbID, resolveLocalizeIDs.
-- Without this index every lookup is a sequential scan that grows
-- linearly with the cache size.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_tmdb_info_imdb_id
    ON tmdb.info (imdb_id)
    WHERE imdb_id IS NOT NULL;
