UPDATE movie_metadata SET poster_horizontal_url = REPLACE(poster_horizontal_url, '/w780/', '/w500/') WHERE poster_horizontal_url LIKE '%/w780/%';
UPDATE series_metadata SET poster_horizontal_url = REPLACE(poster_horizontal_url, '/w780/', '/w500/') WHERE poster_horizontal_url LIKE '%/w780/%';
