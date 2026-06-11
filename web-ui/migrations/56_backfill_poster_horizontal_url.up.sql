-- Backfill movie_metadata poster_horizontal_url from tmdb.info
UPDATE movie_metadata
SET poster_horizontal_url = 'https://image.tmdb.org/t/p/w500' || (i.metadata->>'backdrop_path')
FROM tmdb.info i
WHERE (
    movie_metadata.video_id = i.imdb_id
    OR movie_metadata.video_id = 'tmdb' || i.tmdb_id
)
AND i.metadata->>'backdrop_path' IS NOT NULL
AND i.metadata->>'backdrop_path' != ''
AND (movie_metadata.poster_horizontal_url IS NULL OR movie_metadata.poster_horizontal_url = '');

-- Backfill movie_metadata poster_horizontal_url from kinopoisk_unofficial.info
UPDATE movie_metadata
SET poster_horizontal_url = i.metadata->>'coverUrl'
FROM kinopoisk_unofficial.info i
WHERE (
    movie_metadata.video_id = i.imdb_id
    OR movie_metadata.video_id = 'kp' || i.kp_id
)
AND i.metadata->>'coverUrl' IS NOT NULL
AND i.metadata->>'coverUrl' != ''
AND (movie_metadata.poster_horizontal_url IS NULL OR movie_metadata.poster_horizontal_url = '');

-- Backfill series_metadata poster_horizontal_url from tmdb.info
UPDATE series_metadata
SET poster_horizontal_url = 'https://image.tmdb.org/t/p/w500' || (i.metadata->>'backdrop_path')
FROM tmdb.info i
WHERE (
    series_metadata.video_id = i.imdb_id
    OR series_metadata.video_id = 'tmdb' || i.tmdb_id
)
AND i.metadata->>'backdrop_path' IS NOT NULL
AND i.metadata->>'backdrop_path' != ''
AND (series_metadata.poster_horizontal_url IS NULL OR series_metadata.poster_horizontal_url = '');

-- Backfill series_metadata poster_horizontal_url from kinopoisk_unofficial.info
UPDATE series_metadata
SET poster_horizontal_url = i.metadata->>'coverUrl'
FROM kinopoisk_unofficial.info i
WHERE (
    series_metadata.video_id = i.imdb_id
    OR series_metadata.video_id = 'kp' || i.kp_id
)
AND i.metadata->>'coverUrl' IS NOT NULL
AND i.metadata->>'coverUrl' != ''
AND (series_metadata.poster_horizontal_url IS NULL OR series_metadata.poster_horizontal_url = '');
