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
			SELECT m.resource_id 
			FROM movie m
			LEFT JOIN movie_metadata md ON m.movie_metadata_id = md.movie_metadata_id
			WHERE m.movie_metadata_id IS NULL OR md.poster_url = '' OR md.poster_url IS NULL OR md.updated_at < ?
			
			UNION
			
			SELECT s.resource_id
			FROM series s
			LEFT JOIN series_metadata sd ON s.series_metadata_id = sd.series_metadata_id
			WHERE s.series_metadata_id IS NULL OR sd.poster_url = '' OR sd.poster_url IS NULL OR sd.updated_at < ?

			UNION

			SELECT resource_id
			FROM media_info
			WHERE status = ? OR (status = ? AND updated_at < ?)
		) tmp
	`
	cutoff := time.Now().Add(-staleThreshold)
	processingCutoff := time.Now().Add(-6 * time.Hour)
	_, err := db.QueryContext(ctx, &ids, query, cutoff, cutoff, int16(MediaInfoStatusError), int16(MediaInfoStatusProcessing), processingCutoff)
	return ids, err
}

