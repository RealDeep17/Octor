package models

import (
	"context"

	"github.com/go-pg/pg/v10"
)

// ResourceHasLinkedMetadata returns true when the given resource has at least
// one movie_metadata or series_metadata row linked with a non-empty poster_url
// or video_id. Used by Enrich() to decide whether to write Done vs NoMetadata.
func ResourceHasLinkedMetadata(ctx context.Context, db *pg.DB, resourceID string) (bool, error) {
	var exists bool
	_, err := db.QueryOneContext(ctx, pg.Scan(&exists), `
		SELECT EXISTS (
			SELECT 1
			FROM movie m
			JOIN movie_metadata md ON m.movie_metadata_id = md.movie_metadata_id
			WHERE m.resource_id = ?
			  AND (
			    (md.poster_url IS NOT NULL AND md.poster_url != '') OR
			    (md.video_id   IS NOT NULL AND md.video_id   != '')
			  )
			  LIMIT 1
			  ) OR EXISTS (
			  SELECT 1
			  FROM series s
			  JOIN series_metadata sd ON s.series_metadata_id = sd.series_metadata_id
			  WHERE s.resource_id = ?
			  AND (
			    (sd.poster_url IS NOT NULL AND sd.poster_url != '') OR
			    (sd.video_id   IS NOT NULL AND sd.video_id   != '')
			  )			LIMIT 1
		)
	`, resourceID, resourceID)
	if err != nil {
		return false, err
	}
	return exists, nil
}
