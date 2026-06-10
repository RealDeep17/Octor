package models

import (
	"context"
	"errors"
	"time"

	"github.com/go-pg/pg/v10"
)

type TorrentResource struct {
	tableName struct{} `pg:"torrent_resource"`

	ResourceID       string    `pg:"resource_id,pk"`
	Name             string    `pg:"name"`
	FileCount        int       `pg:"file_count"`
	SizeBytes        int64     `pg:"size_bytes"`
	CreatedAt        time.Time `pg:"created_at"`
	TorrentSizeBytes int64     `pg:"torrent_size_bytes"`

	LibraryEntries []*Library `pg:"rel:has-many,fk:resource_id"`
	MediaInfo      *MediaInfo `pg:"rel:has-one,fk:resource_id"`
}

func GetResourcesWithoutMediaInfo(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).
		Context(ctx).
		Where("media_info.resource_id IS NULL").
		Join("LEFT JOIN media_info ON media_info.resource_id = torrent_resource.resource_id").
		Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetAllResources(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).Context(ctx).Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetActiveResources(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).
		Context(ctx).
		Where("EXISTS (SELECT 1 FROM library WHERE library.resource_id = torrent_resource.resource_id) OR EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = torrent_resource.resource_id)").
		Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}


func GetErrorResources(ctx context.Context, db *pg.DB) ([]*TorrentResource, error) {
	var resources []*TorrentResource

	err := db.Model(&resources).
		Context(ctx).
		Join("JOIN media_info ON media_info.resource_id = torrent_resource.resource_id").
		Where("media_info.status in (?)", pg.In([]int16{
			int16(MediaInfoStatusProcessing),
			int16(MediaInfoStatusError),
		})).
		Select()

	if err != nil {
		return nil, err
	}
	return resources, nil
}

func GetResourceByID(ctx context.Context, db *pg.DB, id string) (*TorrentResource, error) {
	var resource TorrentResource
	err := db.Model(&resource).Context(ctx).Where("resource_id = ?", id).Limit(1).Select()

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &resource, nil
}

func GetStaleOrMissingMetadataResourceIDs(ctx context.Context, db *pg.DB, staleThreshold time.Duration) ([]string, error) {
	var ids []string
	query := `
		SELECT DISTINCT resource_id FROM (
			-- Movies with missing or stale metadata (poster gone / expired)
			SELECT m.resource_id
			FROM movie m
			LEFT JOIN movie_metadata md ON m.movie_metadata_id = md.movie_metadata_id
			WHERE m.movie_metadata_id IS NULL
			   OR md.poster_url = '' OR md.poster_url IS NULL
			   OR md.updated_at < ?

			UNION

			-- Series with missing or stale metadata
			SELECT s.resource_id
			FROM series s
			LEFT JOIN series_metadata sd ON s.series_metadata_id = sd.series_metadata_id
			WHERE s.series_metadata_id IS NULL
			   OR sd.poster_url = '' OR sd.poster_url IS NULL
			   OR sd.updated_at < ?

			UNION

			-- Error rows past 1h cooldown (retryable)
			SELECT resource_id FROM media_info
			WHERE status = ? AND updated_at < now() - INTERVAL '1 hour'

			UNION

			-- NoMetadata rows past 1h cooldown (retryable, not Abandoned)
			SELECT resource_id FROM media_info
			WHERE status = ? AND updated_at < now() - INTERVAL '1 hour'

			UNION

			-- Stale Processing locks (> 15 min = dead worker)
			SELECT resource_id FROM media_info
			WHERE status = ? AND updated_at < now() - INTERVAL '15 minutes'

			-- NOTE: Abandoned (status=6) is intentionally excluded.
			-- Only force=true (per-item ↻ or Force All) can reach Abandoned rows.
		) tmp
	`
	cutoff := time.Now().Add(-staleThreshold)
	_, err := db.QueryContext(ctx, &ids, query,
		cutoff,                           // movie metadata stale cutoff
		cutoff,                           // series metadata stale cutoff
		int16(MediaInfoStatusError),      // Error rows
		int16(MediaInfoStatusNoMetadata), // NoMetadata rows
		int16(MediaInfoStatusProcessing), // stale Processing rows
	)
	return ids, err
}

func PruneOneTimers(ctx context.Context, db *pg.DB, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)

	// Delete from torrent_resource first (where created_at < cutoff, and resource_id not in library/pledge)
	res1, err := db.Model((*TorrentResource)(nil)).
		Context(ctx).
		Where("created_at < ?", cutoff).
		Where("NOT EXISTS (SELECT 1 FROM library WHERE library.resource_id = torrent_resource.resource_id)").
		Where("NOT EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = torrent_resource.resource_id)").
		Delete()
	if err != nil {
		return 0, err
	}

	// Delete from media_info (where created_at < cutoff, and resource_id not in library/pledge)
	// Deleting from media_info cascades to movie, series, episode
	res2, err := db.Model((*MediaInfo)(nil)).
		Context(ctx).
		Where("created_at < ?", cutoff).
		Where("NOT EXISTS (SELECT 1 FROM library WHERE library.resource_id = media_info.resource_id)").
		Where("NOT EXISTS (SELECT 1 FROM vault.pledge WHERE pledge.resource_id = media_info.resource_id)").
		Delete()
	if err != nil {
		return int64(res1.RowsAffected()), err
	}

	return int64(res1.RowsAffected() + res2.RowsAffected()), nil
}

