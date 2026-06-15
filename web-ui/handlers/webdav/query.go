package webdav

import (
	"context"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

// applyAllUsersLibrarySort matches the ordering requirements for DISTINCT ON resource_id queries.
func applyAllUsersLibrarySort(query *pg.Query, sort models.SortType) {
	switch sort {
	case models.SortTypeName:
		query.OrderExpr("library.resource_id, torrent.name ASC")
	default:
		query.OrderExpr("library.resource_id, library.created_at DESC")
	}
}

// getLibraryTorrentsListLightweightPaginated loads user library entries in pages of 1,000,
// preloading only the Torrent relation to prevent memory-heavy metadata graph loads.
func getLibraryTorrentsListLightweightPaginated(ctx context.Context, db *pg.DB, uID uuid.UUID, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			Where("library.user_id = ?", uID).
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		switch sort {
		case models.SortTypeName:
			query.OrderExpr("torrent.name ASC")
		default:
			query.OrderExpr("library.created_at DESC")
		}

		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated library list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibraryMovieTorrentListLightweightPaginated lists only movies in pages of 1,000, preloading only Torrent.
func getLibraryMovieTorrentListLightweightPaginated(ctx context.Context, db *pg.DB, uID uuid.UUID, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			Join("join movie as m").
			JoinOn("m.resource_id = library.resource_id").
			Join("left join movie_metadata as mmd").
			JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
			Where("library.user_id = ?", uID).
			Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND m.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		switch sort {
		case models.SortTypeName:
			query.OrderExpr("torrent.name ASC")
		default:
			query.OrderExpr("library.created_at DESC")
		}

		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated movie torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibrarySeriesTorrentListLightweightPaginated lists only tv series in pages of 1,000, preloading only Torrent.
func getLibrarySeriesTorrentListLightweightPaginated(ctx context.Context, db *pg.DB, uID uuid.UUID, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			Join("join series as s").
			JoinOn("s.resource_id = library.resource_id").
			Where("library.user_id = ?", uID).
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		switch sort {
		case models.SortTypeName:
			query.OrderExpr("torrent.name ASC")
		default:
			query.OrderExpr("library.created_at DESC")
		}

		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated series torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibraryAdultTorrentListLightweightPaginated lists only adult content in pages of 1,000, preloading only Torrent.
func getLibraryAdultTorrentListLightweightPaginated(ctx context.Context, db *pg.DB, uID uuid.UUID, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			Join("join movie as m").
			JoinOn("m.resource_id = library.resource_id").
			Join("left join movie_metadata as mmd").
			JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
			Where("library.user_id = ?", uID).
			Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR m.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		switch sort {
		case models.SortTypeName:
			query.OrderExpr("torrent.name ASC")
		default:
			query.OrderExpr("library.created_at DESC")
		}

		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated adult torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibraryTorrentsListAllLightweightPaginated lists all user library entries in pages of 1,000, preloading only Torrent.
func getLibraryTorrentsListAllLightweightPaginated(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			ColumnExpr("DISTINCT ON (library.resource_id) library.*").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		applyAllUsersLibrarySort(query, sort)
		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated all-user library list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibraryMovieTorrentListAllLightweightPaginated lists movies for all users in pages of 1,000, preloading only Torrent.
func getLibraryMovieTorrentListAllLightweightPaginated(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			ColumnExpr("DISTINCT ON (library.resource_id) library.*").
			Join("join movie as m").
			JoinOn("m.resource_id = library.resource_id").
			Join("left join movie_metadata as mmd").
			JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
			Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND m.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		applyAllUsersLibrarySort(query, sort)
		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated all-user movie torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibrarySeriesTorrentListAllLightweightPaginated lists tv series for all users in pages of 1,000, preloading only Torrent.
func getLibrarySeriesTorrentListAllLightweightPaginated(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			ColumnExpr("DISTINCT ON (library.resource_id) library.*").
			Join("join series as s").
			JoinOn("s.resource_id = library.resource_id").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		applyAllUsersLibrarySort(query, sort)
		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated all-user series torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}

// getLibraryAdultTorrentListAllLightweightPaginated lists adult content for all users in pages of 1,000, preloading only Torrent.
func getLibraryAdultTorrentListAllLightweightPaginated(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Library, error) {
	var allList []*models.Library
	pageSize := 1000
	offset := 0

	for {
		var page []*models.Library
		query := db.Model(&page).
			Context(ctx).
			ColumnExpr("DISTINCT ON (library.resource_id) library.*").
			Join("join movie as m").
			JoinOn("m.resource_id = library.resource_id").
			Join("left join movie_metadata as mmd").
			JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
			Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR m.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
			Relation("Torrent").
			Limit(pageSize).
			Offset(offset)

		if q != "" {
			query.Where("torrent.name ILIKE ?", "%"+q+"%")
		}

		applyAllUsersLibrarySort(query, sort)
		err := query.Select()
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch paginated all-user adult torrent list")
		}

		if len(page) == 0 {
			break
		}

		allList = append(allList, page...)
		if len(page) < pageSize {
			break
		}
		offset += pageSize
	}

	return allList, nil
}
