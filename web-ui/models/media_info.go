package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

type MediaInfo struct {
	tableName struct{} `pg:"media_info"`

	ResourceID string    `pg:"resource_id,pk"`
	Status     int16     `pg:"status,use_zero"`
	MediaType  *int16    `pg:"media_type"`
	Error      *string   `pg:"error"`
	RetryCount int16     `pg:"retry_count,use_zero"`
	CreatedAt  time.Time `pg:"created_at,default:now()"`
	UpdatedAt  time.Time `pg:"updated_at,default:now()"`

	// Relations
	Movies     []*Movie   `pg:"rel:has-many,fk:resource_id"`
	SeriesList []*Series  `pg:"rel:has-many,fk:resource_id"`
	Episodes   []*Episode `pg:"rel:has-many,fk:resource_id"`
}

type MediaInfoStatus int16

const (
	MediaInfoStatusProcessing  MediaInfoStatus = iota // 0 — locked by a worker
	MediaInfoStatusDone                               // 1 — has real linked metadata ✅
	MediaInfoStatusNoMedia                            // 2 — no video files (never retry)
	MediaInfoStatusNoMetadata                         // 3 — video found, no mapper matched (retry after 1h)
	MediaInfoStatusError                              // 4 — API/network failure (retry after 1h)
	MediaInfoStatusForbidden                          // 5 — permission denied (never auto-retry)
	MediaInfoStatusAbandoned                          // 6 — exceeded MAX_RETRIES, force-only
)

// MaxEnrichRetries is the number of NoMetadata outcomes before a resource
// is marked Abandoned and removed from automatic re-enrichment.
const MaxEnrichRetries = 3

type MediaInfoMediaType int16

const (
	MediaInfoMediaTypeMovieSingle MediaInfoMediaType = iota
	MediaInfoMediaTypeSeriesSplitScenes
	MediaInfoMediaTypeSeriesSingleSeason
	MediaInfoMediaTypeSeriesMultipleSeasons
	MediaInfoMediaTypeSeriesCompilation
	// MovieMultiple is a torrent that bundles several distinct films
	// (e.g. a "Home Alone Trilogy" pack). Each file is its own movie
	// with its own title and year — the enricher splits them into
	// separate movie rows so each can be matched against TMDB.
	MediaInfoMediaTypeMovieMultiple
)

func (s MediaInfoMediaType) String() string {
	switch s {
	case MediaInfoMediaTypeMovieSingle:
		return "MovieSingle"
	case MediaInfoMediaTypeSeriesSplitScenes:
		return "SeriesSplitScenes"
	case MediaInfoMediaTypeSeriesSingleSeason:
		return "SeriesSingleSeason"
	case MediaInfoMediaTypeSeriesMultipleSeasons:
		return "SeriesMultipleSeasons"
	case MediaInfoMediaTypeSeriesCompilation:
		return "SeriesCompilation"
	case MediaInfoMediaTypeMovieMultiple:
		return "MovieMultiple"
	default:
		return "Unknown"
	}
}

// TryInsertOrLockMediaInfo acquires an exclusive processing lock for the
// given resource. Returns the locked MediaInfo row on success, or nil
// when the resource should be skipped (recent/active/abandoned).
//
// Lock behaviour:
//
//	force=false (Smart Refresh, library-add, on-demand):
//	  - Processing < 15min  → SKIP  (another worker is active)
//	  - Processing > 15min  → STEAL (stale lock, worker is dead)
//	  - Done      < 24h     → SKIP  (metadata is fresh)
//	  - Done      > 24h     → STEAL (periodic refresh)
//	  - NoMetadata < 1h     → SKIP  (cooldown)
//	  - NoMetadata > 1h     → STEAL (retry, increments counter upstream)
//	  - Error      < 1h     → SKIP  (cooldown)
//	  - Error      > 1h     → STEAL (retry)
//	  - Abandoned            → SKIP  (exhausted retries — force only)
//	  - NoMedia / Forbidden  → SKIP  (permanent)
//
//	force=true (per-item ↻, Force All):
//	  - Any status (incl. Processing) → STEAL immediately, reset retry_count
//	  - This is the only way to unblock Abandoned resources.
func TryInsertOrLockMediaInfo(ctx context.Context, db *pg.DB, resourceID string, expire time.Duration, force bool) (*MediaInfo, error) {
	// Attempt to insert a new media_info row with "processing" status.
	info := MediaInfo{
		ResourceID: resourceID,
		Status:     int16(MediaInfoStatusProcessing),
	}

	_, err := db.Model(&info).Context(ctx).Insert()
	if err == nil {
		// INSERT succeeded — this worker owns the lock.
		return &info, nil
	}

	// Any error other than a duplicate-key violation is a real DB error.
	if !strings.Contains(err.Error(), "duplicate key") {
		return nil, err
	}

	// Row already exists. Two paths: force (steal) vs non-force (respect cooldowns).

	if force {
		// force=true: steal the lock unconditionally — no SKIP LOCKED.
		// A metadata fetch should never take more than 5 min; if a row is
		// still Processing when force hits, the previous worker is dead.
		tx, txErr := db.BeginContext(ctx)
		if txErr != nil {
			return nil, txErr
		}
		defer func() { _ = tx.Close() }()

		var existing MediaInfo
		txErr = tx.Model(&existing).
			Context(ctx).
			Where("resource_id = ?", resourceID).
			For("UPDATE"). // no SKIP LOCKED — force always steals
			Limit(1).
			Select()
		if txErr != nil {
			return nil, txErr
		}

		existing.Status = int16(MediaInfoStatusProcessing)
		existing.RetryCount = 0    // any force resets the retry counter
		existing.UpdatedAt = time.Now() // timer starts NOW for this item

		_, txErr = tx.Model(&existing).
			Context(ctx).
			Column("status", "retry_count", "updated_at").
			WherePK().
			Update()
		if txErr != nil {
			return nil, txErr
		}

		return &existing, tx.Commit()
	}

	// Non-force path: SELECT FOR UPDATE SKIP LOCKED with cooldown conditions.
	// Abandoned is intentionally excluded — only force can reach it.
	tx, txErr := db.BeginContext(ctx)
	if txErr != nil {
		return nil, txErr
	}
	defer func() { _ = tx.Close() }()

	var existing MediaInfo

	txErr = tx.Model(&existing).
		Context(ctx).
		Where("resource_id = ?", resourceID).
		Where(`(
			(status = ? AND updated_at < now() - INTERVAL ?)
			OR (status IN (?, ?) AND updated_at < now() - INTERVAL ?)
			OR (status = ? AND updated_at < now() - INTERVAL ?)
		)`,
			// Done: refresh after 24h
			int16(MediaInfoStatusDone),
			fmt.Sprintf("%d seconds", int(expire.Seconds())),
			// NoMetadata + Error: retry after 1h cooldown
			int16(MediaInfoStatusNoMetadata), int16(MediaInfoStatusError),
			"1 hour",
			// Processing: stale lock recovery after 15 min
			int16(MediaInfoStatusProcessing),
			"15 minutes",
		).
		For("UPDATE SKIP LOCKED").
		Limit(1).
		Select()

	if txErr != nil {
		if errors.Is(txErr, pg.ErrNoRows) {
			// Row is fresh, locked by another worker, or Abandoned — skip.
			return nil, nil
		}
		return nil, txErr
	}

	// Update status to "processing" (retry_count stays unchanged for non-force).
	// Promote to Processing — set updated_at explicitly so the 15-min stale
	// timer is measured from when this item starts processing, not from any
	// prior state transition.
	existing.Status = int16(MediaInfoStatusProcessing)
	existing.UpdatedAt = time.Now()

	_, txErr = tx.Model(&existing).
		Context(ctx).
		Column("status", "updated_at").
		WherePK().
		Update()
	if txErr != nil {
		return nil, txErr
	}

	return &existing, tx.Commit()
}

func UpdateMediaInfo(ctx context.Context, db *pg.DB, info *MediaInfo) error {
	_, err := db.Model(info).
		Context(ctx).
		Column("status", "media_type", "error", "retry_count").
		WherePK().
		Update()
	return err
}
