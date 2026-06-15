package models

import (
	"context"
	"encoding/json"
	"io"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
)

type VideoStatusExport struct {
	Movies       []MovieStatus   `json:"movies"`
	Series       []SeriesStatus  `json:"series"`
	Episodes     []EpisodeStatus `json:"episodes"`
	WatchHistory []WatchHistory  `json:"watch_history"`
}

// ExportVideoStatus returns all watch and rating status data for a user.
func ExportVideoStatus(ctx context.Context, db *pg.DB, userID uuid.UUID) (*VideoStatusExport, error) {
	export := &VideoStatusExport{
		Movies:       []MovieStatus{},
		Series:       []SeriesStatus{},
		Episodes:     []EpisodeStatus{},
		WatchHistory: []WatchHistory{},
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

	// Fetch watch history
	err = db.Model(&export.WatchHistory).
		Context(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Select()
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return nil, errors.Wrap(err, "failed to export watch history")
	}

	return export, nil
}

// ExportVideoStatusStream streams all watch and rating status data for a user in JSON format directly to the writer in pages.
func ExportVideoStatusStream(ctx context.Context, db *pg.DB, userID uuid.UUID, w io.Writer) error {
	enc := json.NewEncoder(w)
	limit := 100

	if _, err := w.Write([]byte(`{"movies":[`)); err != nil {
		return err
	}

	// Stream movies
	offset := 0
	first := true
	for {
		var movies []MovieStatus
		err := db.Model(&movies).
			Context(ctx).
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Limit(limit).
			Offset(offset).
			Select()
		if err != nil && !errors.Is(err, pg.ErrNoRows) {
			return errors.Wrap(err, "failed to export movie status")
		}
		if len(movies) == 0 {
			break
		}
		for _, m := range movies {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			first = false
			if err := enc.Encode(m); err != nil {
				return err
			}
		}
		if len(movies) < limit {
			break
		}
		offset += limit
	}

	if _, err := w.Write([]byte(`],"series":[`)); err != nil {
		return err
	}

	// Stream series
	offset = 0
	first = true
	for {
		var series []SeriesStatus
		err := db.Model(&series).
			Context(ctx).
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Limit(limit).
			Offset(offset).
			Select()
		if err != nil && !errors.Is(err, pg.ErrNoRows) {
			return errors.Wrap(err, "failed to export series status")
		}
		if len(series) == 0 {
			break
		}
		for _, s := range series {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			first = false
			if err := enc.Encode(s); err != nil {
				return err
			}
		}
		if len(series) < limit {
			break
		}
		offset += limit
	}

	if _, err := w.Write([]byte(`],"episodes":[`)); err != nil {
		return err
	}

	// Stream episodes
	offset = 0
	first = true
	for {
		var episodes []EpisodeStatus
		err := db.Model(&episodes).
			Context(ctx).
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Limit(limit).
			Offset(offset).
			Select()
		if err != nil && !errors.Is(err, pg.ErrNoRows) {
			return errors.Wrap(err, "failed to export episode status")
		}
		if len(episodes) == 0 {
			break
		}
		for _, e := range episodes {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			first = false
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
		if len(episodes) < limit {
			break
		}
		offset += limit
	}

	if _, err := w.Write([]byte(`],"watch_history":[`)); err != nil {
		return err
	}

	// Stream watch history
	offset = 0
	first = true
	for {
		var history []WatchHistory
		err := db.Model(&history).
			Context(ctx).
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Limit(limit).
			Offset(offset).
			Select()
		if err != nil && !errors.Is(err, pg.ErrNoRows) {
			return errors.Wrap(err, "failed to export watch history")
		}
		if len(history) == 0 {
			break
		}
		for _, h := range history {
			if !first {
				if _, err := w.Write([]byte(",")); err != nil {
					return err
				}
			}
			first = false
			if err := enc.Encode(h); err != nil {
				return err
			}
		}
		if len(history) < limit {
			break
		}
		offset += limit
	}

	if _, err := w.Write([]byte(`]}`)); err != nil {
		return err
	}

	return nil
}

// DeleteAllVideoStatus erases all watch and rating status data for a user.
func DeleteAllVideoStatus(ctx context.Context, db *pg.DB, userID uuid.UUID) error {
	return db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Delete movies
		_, err := tx.Model((*MovieStatus)(nil)).
			Context(ctx).
			Where("user_id = ? ", userID).
			Delete()
		if err != nil {
			return errors.Wrap(err, "failed to delete all movie status")
		}

		// Delete series
		_, err = tx.Model((*SeriesStatus)(nil)).
			Context(ctx).
			Where("user_id = ? ", userID).
			Delete()
		if err != nil {
			return errors.Wrap(err, "failed to delete all series status")
		}

		// Delete episodes
		_, err = tx.Model((*EpisodeStatus)(nil)).
			Context(ctx).
			Where("user_id = ? ", userID).
			Delete()
		if err != nil {
			return errors.Wrap(err, "failed to delete all episode status")
		}

		// Delete watch history
		if err := DeleteAllWatchHistory(ctx, tx, userID); err != nil {
			return errors.Wrap(err, "failed to delete all watch history")
		}

		return nil
	})
}
