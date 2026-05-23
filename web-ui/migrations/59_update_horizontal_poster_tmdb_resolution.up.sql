UPDATE movie_metadata SET poster_horizontal_url = REPLACE(poster_horizontal_url, '/w500/', '/w780/') WHERE poster_horizontal_url LIKE '%/w500/%';
UPDATE series_metadata SET poster_horizontal_url = REPLACE(poster_horizontal_url, '/w500/', '/w780/') WHERE poster_horizontal_url LIKE '%/w500/%';
