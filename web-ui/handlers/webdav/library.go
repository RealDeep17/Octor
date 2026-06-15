package webdav

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type Library interface {
	GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error)
}

type AllLibrary struct{}

func (s *AllLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return getLibraryTorrentsListLightweightPaginated(ctx, db, uID, models.SortTypeName, "")
}

var _ Library = (*AllLibrary)(nil)

type MovieLibrary struct{}

func (s *MovieLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return getLibraryMovieTorrentListLightweightPaginated(ctx, db, uID, models.SortTypeName, "")
}

var _ Library = (*MovieLibrary)(nil)

type SeriesLibrary struct{}

func (s *SeriesLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return getLibrarySeriesTorrentListLightweightPaginated(ctx, db, uID, models.SortTypeName, "")
}

var _ Library = (*SeriesLibrary)(nil)

type AdultLibrary struct{}

func (s *AdultLibrary) GetContent(ctx context.Context, db *pg.DB, uID uuid.UUID) ([]*models.Library, error) {
	return getLibraryAdultTorrentListLightweightPaginated(ctx, db, uID, models.SortTypeName, "")
}

var _ Library = (*AdultLibrary)(nil)
