package models

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type VideoStatusExport struct {
	Movies   []MovieStatus   `json:"movies"`
	Series   []SeriesStatus  `json:"series"`
	Episodes []EpisodeStatus `json:"episodes"`
}

// ExportVideoStatus returns all watch and rating status data for a user.
func ExportVideoStatus(ctx context.Context, db *pg.DB, userID uuid.UUID) (*VideoStatusExport, error) {
	export := &VideoStatusExport{
		Movies:   []MovieStatus{},
		Series:   []SeriesStatus{},
		Episodes: []EpisodeStatus{},
	}

	// Fetch movies
	err := db.Model(&export.Movies).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to export movie status")
	}

	// Fetch series
	err = db.Model(&export.Series).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to export series status")
	}

	// Fetch episodes
	err = db.Model(&export.Episodes).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to export episode status")
	}

	return export, nil
}

// DeleteAllVideoStatus erases all watch and rating status data for a user.
func DeleteAllVideoStatus(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	// Delete movies
	_, err := db.Model((*MovieStatus)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete all movie status")
	}

	// Delete series
	_, err = db.Model((*SeriesStatus)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete all series status")
	}

	// Delete episodes
	_, err = db.Model((*EpisodeStatus)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to delete all episode status")
	}

	return nil
}
