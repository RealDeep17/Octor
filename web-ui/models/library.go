package models

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models/tmdb"
)

type SortType int

const (
	SortTypeRecentlyAdded SortType = iota
	SortTypeName
	SortTypeYear
	SortTypeRating
)

// String returns the i18n message key for this sort type.
// Resolved in templates via {{ t $.Lang .Title }}.
func (s SortType) String() string {
	switch s {
	case SortTypeRecentlyAdded:
		return "library.sort.recentlyAdded"
	case SortTypeName:
		return "library.sort.name"
	case SortTypeYear:
		return "library.sort.year"
	case SortTypeRating:
		return "library.sort.rating"
	default:
		return "library.sort.unknown"
	}
}

type Library struct {
	tableName struct{} `pg:"library"`

	UserID     uuid.UUID `pg:"user_id,pk"`
	ResourceID string    `pg:"resource_id,pk"`
	CreatedAt  time.Time `pg:"created_at"`

	Torrent   *TorrentResource `pg:"rel:has-one,fk:resource_id"`
	MediaInfo *MediaInfo       `pg:"rel:has-one,fk:resource_id"`
	User      *User            `pg:"rel:has-one,fk:user_id"`
	Name      string
}

func IsInLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, resourceID string) (bool, error) {
	exists, err := db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ?", uID, resourceID).
		Exists()
	if err != nil {
		return false, errors.Wrap(err, "failed to check library membership")
	}
	return exists, nil
}

func GetLibraryByName(ctx context.Context, db *pg.DB, uID uuid.UUID, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Where("library.user_id = ?", uID).
		Where("library.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by name")
	}
	return &lib, nil
}

func GetLibraryByTorrentName(ctx context.Context, db *pg.DB, uID uuid.UUID, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Join("join torrent_resource as t on t.resource_id = library.resource_id").
		Where("library.user_id = ?", uID).
		Where("t.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by name")
	}
	return &lib, nil
}

func AddTorrentToLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, resourceID string, info *metainfo.Info, displayName string, torrentSize int64) (*Library, error) {
	name := info.NameUtf8
	if name == "" {
		name = info.Name
	}

	filesCount := 1
	if info.Files != nil {
		filesCount = len(info.Files)
	}

	torrent := &TorrentResource{
		ResourceID:       resourceID,
		Name:             name,
		FileCount:        filesCount,
		SizeBytes:        info.TotalLength(),
		TorrentSizeBytes: torrentSize,
	}

	_, err := db.Model(torrent).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert torrent resource")
	}

	if displayName == "" {
		displayName = name
	}

	lib := &Library{
		UserID:     uID,
		ResourceID: resourceID,
		CreatedAt:  time.Now(),
		Name:       displayName,
	}

	_, err = db.Model(lib).
		Context(ctx).
		OnConflict("DO NOTHING").
		Insert()
	if err != nil {
		return nil, errors.Wrap(err, "failed to insert library entry")
	}

	return lib, nil
}

func RemoveFromLibrary(ctx context.Context, db *pg.DB, uID uuid.UUID, rID string) error {
	_, err := db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ? AND resource_id = ?", uID, rID).
		Delete()
	if err != nil {
		return errors.Wrap(err, "failed to remove from library")
	}
	return nil
}

func UpdateLibraryName(ctx context.Context, db *pg.DB, l *Library) error {
	_, err := db.Model(l).Context(ctx).WherePK().Column("name").Update()
	return err
}

func GetLibraryCounts(ctx context.Context, db *pg.DB, uID uuid.UUID) (torrents, movies, series, adult int, err error) {
	torrents, err = db.Model((*Library)(nil)).
		Context(ctx).
		Where("user_id = ?", uID).
		Count()
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count torrents")
	}

	movies, err = db.Model((*Movie)(nil)).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Where("l.user_id = ?", uID).
		Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND movie.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Count()
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count movies")
	}

	_, err = db.QueryOneContext(ctx, pg.Scan(&series), `
		SELECT COUNT(DISTINCT COALESCE(NULLIF(smd.video_id, ''), series.series_id::text))
		FROM series
		JOIN library as l ON series.resource_id = l.resource_id
		LEFT JOIN series_metadata as smd ON series.series_metadata_id = smd.series_metadata_id
		WHERE l.user_id = ?
	`, uID)
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count series")
	}

	adult, err = db.Model((*Movie)(nil)).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Where("l.user_id = ?", uID).
		Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR movie.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Count()
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count adult")
	}

	return
}

func GetLibraryTorrentsList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, q string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Where("library.user_id = ?", uID).
		Relation("Torrent").
		Relation("MediaInfo").
		Relation("MediaInfo.Movies.MovieMetadata").
		Relation("MediaInfo.SeriesList.SeriesMetadata")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC")
	case SortTypeRecentlyAdded:
		fallthrough
	default:
		query.OrderExpr("library.created_at DESC")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library list")
	}

	return list, nil
}

func GetMovieByID(ctx context.Context, db *pg.DB, uID uuid.UUID, movieID string) (*Movie, error) {
	var m Movie

	query := db.Model(&m).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Where("movie.movie_id = ?", movieID).
		Where("l.user_id = ?", uID).
		Limit(1)

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie")
	}

	return &m, nil
}

func GetMoviesByVideoID(ctx context.Context, db *pg.DB, uID uuid.UUID, videoID string) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Where("l.user_id = ?", uID).
		Where("mmd.video_id = ?", videoID).
		Relation("MovieMetadata")

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie list")
	}

	return list, nil
}

func GetLibraryMovieTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, q string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
		Where("library.user_id = ?", uID).
		Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND m.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC")
	case SortTypeRecentlyAdded:
		fallthrough
	default:
		query.OrderExpr("library.created_at DESC")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie torrent list")
	}

	return list, nil
}

func GetLibrarySeriesTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, q string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join series as s").
		JoinOn("s.resource_id = library.resource_id").
		Where("library.user_id = ?", uID).
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC")
	case SortTypeRecentlyAdded:
		fallthrough
	default:
		query.OrderExpr("library.created_at DESC")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie torrent list")
	}

	return list, nil
}

// GetLibraryMovieList loads movies in the user's library, optionally filtered
// by watched state. watchedFilter is one of "", "unwatched", or "watched" and
// matches against movie_status.watched column.
func GetLibraryMovieList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string, q string) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Join("left join movie_status as ums").
		JoinOn("ums.user_id = l.user_id AND ums.video_id = mmd.video_id AND ums.watched = true").
		Where("l.user_id = ?", uID).
		Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND movie.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("MovieMetadata")

	if q != "" {
		query.Where("mmd.title ILIKE ? OR movie.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch watchedFilter {
	case "unwatched":
		query.Where("ums.video_id IS NULL")
	case "watched":
		query.Where("ums.video_id IS NOT NULL")
	}

	switch sort {
	case SortTypeRecentlyAdded:
		query.OrderExpr("l.created_at DESC")
	case SortTypeName:
		query.OrderExpr("COALESCE(mmd.title, movie.title) ASC")
	case SortTypeYear:
		query.OrderExpr("COALESCE(mmd.year, movie.year) DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch movie list")
	}

	if len(list) > 0 {
		var videoIDs []string
		for _, m := range list {
			if m.MovieMetadata != nil && m.MovieMetadata.VideoID != "" {
				videoIDs = append(videoIDs, m.MovieMetadata.VideoID)
			}
		}
		if len(videoIDs) > 0 {
			statusMap, err := GetMovieStatusMap(ctx, db, uID, videoIDs)
			if err == nil {
				for _, m := range list {
					if m.MovieMetadata != nil {
						if st, ok := statusMap[m.MovieMetadata.VideoID]; ok {
							m.UserWatched = st.Watched
							m.UserRating = st.Rating
							m.UserPosterLayout = st.PosterLayout
						}
					}
				}
			}
		}
	}

	return list, nil
}

// GetLibrarySeriesList is the series counterpart to GetLibraryMovieList.
// Filtering considers only series-level series_status (manual declaration
// or auto_all_episodes); per-episode rows are not included in the filter.
func GetLibrarySeriesList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string, q string) ([]*Series, error) {
	var list []*Series

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("series.resource_id = l.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Join("left join series_status as uss").
		JoinOn("uss.user_id = l.user_id AND uss.video_id = smd.video_id AND uss.watched = true").
		Where("l.user_id = ?", uID).
		Relation("SeriesMetadata").
		Relation("Episodes.EpisodeMetadata")

	if q != "" {
		query.Where("smd.title ILIKE ? OR series.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch watchedFilter {
	case "unwatched":
		query.Where("uss.video_id IS NULL")
	case "watched":
		query.Where("uss.video_id IS NOT NULL")
	}

	switch sort {
	case SortTypeRecentlyAdded:
		query.OrderExpr("l.created_at DESC")
	case SortTypeName:
		query.OrderExpr("smd.title ASC")
	case SortTypeYear:
		query.OrderExpr("smd.year DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("smd.rating DESC NULLS LAST")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch series list")
	}

	if len(list) > 0 {
		var videoIDs []string
		for _, s := range list {
			if s.SeriesMetadata != nil && s.SeriesMetadata.VideoID != "" {
				videoIDs = append(videoIDs, s.SeriesMetadata.VideoID)
			}
		}
		if len(videoIDs) > 0 {
			statusMap, err := GetSeriesStatusMap(ctx, db, uID, videoIDs)
			if err == nil {
				for _, s := range list {
					if s.SeriesMetadata != nil {
						if st, ok := statusMap[s.SeriesMetadata.VideoID]; ok {
							s.UserWatched = st.Watched
							s.UserRating = st.Rating
							s.UserPosterLayout = st.PosterLayout
						}
					}
				}
			}
		}
		PopulateSeriesAnimeFlags(ctx, db, list)
	}

	return list, nil
}

func isAnimeText(str string) bool {
	if str == "" {
		return false
	}
	sLower := strings.ToLower(str)
	tokens := strings.FieldsFunc(sLower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	tokenSet := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		tokenSet[token] = struct{}{}
	}

	keywords := []string{
		"anime", "vostfr", "vost", "subsplease", "horriblesubs", "erai-raws",
		"erai-raw", "pas-raws", "pas-raw", "judas", "asw", "yameii", "golumpa",
		"noobsub", "noobsubs", "ffa", "nep", "dual-audio", "dual audio", "multiscr",
		"multi-audio", "sub-esp", "sub-english", "sub-eng", "sub_eng",
	}
	for _, kw := range keywords {
		if strings.Contains(kw, " ") || strings.Contains(kw, "-") || strings.Contains(kw, "_") {
			if strings.Contains(sLower, kw) {
				return true
			}
			continue
		}
		if _, ok := tokenSet[kw]; ok {
			return true
		}
	}

	for _, r := range str {
		if (r >= 0x3040 && r <= 0x309F) || // Hiragana
			(r >= 0x30A0 && r <= 0x30FF) || // Katakana
			(r >= 0x4E00 && r <= 0x9FFF) { // Kanji
			return true
		}
	}

	if strings.Contains(str, "[") && strings.Contains(str, "]") {
		parts := strings.Split(sLower, "/")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "[") {
				return true
			}
			if strings.Count(part, "[") >= 2 && strings.Count(part, "]") >= 2 {
				return true
			}
		}
	}

	return false
}

func isAnimeHeuristic(s *Series) bool {
	if s == nil {
		return false
	}
	if isAnimeText(s.Title) {
		return true
	}
	if s.SeriesMetadata != nil && isAnimeText(s.SeriesMetadata.Title) {
		return true
	}
	for _, ep := range s.Episodes {
		if ep == nil {
			continue
		}
		if ep.Path != nil && isAnimeText(*ep.Path) {
			return true
		}
		if ep.Title != nil && isAnimeText(*ep.Title) {
			return true
		}
	}
	return false
}

func isTmdbInfoAnime(info *tmdb.Info) bool {
	if info == nil || info.Metadata == nil {
		return false
	}

	// Check origin country is JP
	isJP := false
	if oc, ok := info.Metadata["origin_country"].([]any); ok {
		for _, c := range oc {
			if str, ok := c.(string); ok && str == "JP" {
				isJP = true
				break
			}
		}
	}

	// Check genre ID 16 (Animation)
	isAnimation := false
	if genres, ok := info.Metadata["genres"].([]any); ok {
		for _, g := range genres {
			if gMap, ok := g.(map[string]any); ok {
				if idVal, ok := gMap["id"]; ok {
					var id float64
					switch v := idVal.(type) {
					case float64:
						id = v
					case int:
						id = float64(v)
					case int64:
						id = float64(v)
					}
					if id == 16 {
						isAnimation = true
						break
					}
				}
			}
		}
	}

	return isJP && isAnimation
}

func PopulateSeriesAnimeFlags(ctx context.Context, db *pg.DB, list []*Series) {
	if len(list) == 0 {
		return
	}

	var imdbIDs []string
	var tmdbIDs []int
	videoToSeries := make(map[string][]*Series)

	for _, s := range list {
		// Default to heuristics
		s.IsAnime = isAnimeHeuristic(s)

		if s.SeriesMetadata != nil && s.SeriesMetadata.VideoID != "" {
			videoID := s.SeriesMetadata.VideoID
			videoToSeries[videoID] = append(videoToSeries[videoID], s)

			if strings.HasPrefix(videoID, "tmdb") {
				if id, err := strconv.Atoi(strings.TrimPrefix(videoID, "tmdb")); err == nil {
					tmdbIDs = append(tmdbIDs, id)
				}
			} else if strings.HasPrefix(videoID, "tt") {
				imdbIDs = append(imdbIDs, videoID)
			}
		}
	}

	if len(imdbIDs) == 0 && len(tmdbIDs) == 0 {
		return
	}

	var tmdbInfos []*tmdb.Info
	query := db.Model(&tmdbInfos).Context(ctx)

	var exprs []string
	var args []any
	if len(imdbIDs) > 0 {
		exprs = append(exprs, "imdb_id IN (?)")
		args = append(args, pg.In(imdbIDs))
	}
	if len(tmdbIDs) > 0 {
		exprs = append(exprs, "tmdb_id IN (?)")
		args = append(args, pg.In(tmdbIDs))
	}

	if len(exprs) > 0 {
		query.Where(strings.Join(exprs, " OR "), args...)
		err := query.Select()
		if err == nil {
			for _, info := range tmdbInfos {
				isAnime := isTmdbInfoAnime(info)
				if isAnime {
					if info.ImdbID != nil {
						for _, s := range videoToSeries[*info.ImdbID] {
							s.IsAnime = true
						}
					}
					tmdbVideoID := "tmdb" + strconv.Itoa(info.TmdbID)
					for _, s := range videoToSeries[tmdbVideoID] {
						s.IsAnime = true
					}
				}
			}
		}
	}
}

func GetLibraryByNameAny(ctx context.Context, db *pg.DB, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Where("library.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by name")
	}
	return &lib, nil
}

func GetLibraryByTorrentNameAny(ctx context.Context, db *pg.DB, name string) (*Library, error) {
	var lib Library
	err := db.Model(&lib).
		Context(ctx).
		Join("join torrent_resource as t on t.resource_id = library.resource_id").
		Where("t.name = ?", name).
		Relation("Torrent").
		Limit(1).
		Select()
	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch library torrent by torrent name")
	}
	return &lib, nil
}

func GetLibraryTorrentsListAll(ctx context.Context, db *pg.DB, sort SortType, q string) ([]*Library, error) {
	var list []*Library
	query := db.Model(&list).
		Context(ctx).
		ColumnExpr("DISTINCT ON (library.resource_id) library.*").
		Relation("Torrent").
		Relation("MediaInfo").
		Relation("MediaInfo.Movies.MovieMetadata").
		Relation("MediaInfo.SeriesList.SeriesMetadata")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	applyAllUsersLibrarySort(query, sort)
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user library list")
	}
	return list, nil
}

func GetLibraryMovieTorrentListAll(ctx context.Context, db *pg.DB, sort SortType, q string) ([]*Library, error) {
	var list []*Library
	query := db.Model(&list).
		Context(ctx).
		ColumnExpr("DISTINCT ON (library.resource_id) library.*").
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
		Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND m.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	applyAllUsersLibrarySort(query, sort)
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user movie torrent list")
	}
	return list, nil
}

func GetLibrarySeriesTorrentListAll(ctx context.Context, db *pg.DB, sort SortType, q string) ([]*Library, error) {
	var list []*Library
	query := db.Model(&list).
		Context(ctx).
		ColumnExpr("DISTINCT ON (library.resource_id) library.*").
		Join("join series as s").
		JoinOn("s.resource_id = library.resource_id").
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	applyAllUsersLibrarySort(query, sort)
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user series torrent list")
	}
	return list, nil
}

func applyAllUsersLibrarySort(query *pg.Query, sort SortType) {
	// DISTINCT ON requires resource_id to lead ORDER BY. The secondary key keeps
	// the chosen representative deterministic while preserving deduplication.
	switch sort {
	case SortTypeName:
		query.OrderExpr("library.resource_id, torrent.name ASC")
	default:
		query.OrderExpr("library.resource_id, library.created_at DESC")
	}
}

func GetLibraryAdultTorrentList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, q string) ([]*Library, error) {
	var list []*Library

	query := db.Model(&list).
		Context(ctx).
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
		Where("library.user_id = ?", uID).
		Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR m.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	switch sort {
	case SortTypeName:
		query.OrderExpr("torrent.name ASC")
	case SortTypeRecentlyAdded:
		fallthrough
	default:
		query.OrderExpr("library.created_at DESC")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch adult torrent list")
	}

	return list, nil
}

func GetLibraryAdultList(ctx context.Context, db *pg.DB, uID uuid.UUID, sort SortType, watchedFilter string, q string) ([]*Movie, error) {
	var list []*Movie

	query := db.Model(&list).
		Context(ctx).
		Join("join library as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Join("left join movie_status as ums").
		JoinOn("ums.user_id = l.user_id AND ums.video_id = mmd.video_id AND ums.watched = true").
		Where("l.user_id = ?", uID).
		Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR movie.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("MovieMetadata")

	if q != "" {
		query.Where("mmd.title ILIKE ? OR movie.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch watchedFilter {
	case "unwatched":
		query.Where("ums.video_id IS NULL")
	case "watched":
		query.Where("ums.video_id IS NOT NULL")
	}

	switch sort {
	case SortTypeRecentlyAdded:
		query.OrderExpr("l.created_at DESC")
	case SortTypeName:
		query.OrderExpr("COALESCE(mmd.title, movie.title) ASC")
	case SortTypeYear:
		query.OrderExpr("COALESCE(mmd.year, movie.year) DESC NULLS LAST")
	case SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
	}

	err := query.Select()
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch adult list")
	}

	if len(list) > 0 {
		var videoIDs []string
		for _, m := range list {
			if m.MovieMetadata != nil && m.MovieMetadata.VideoID != "" {
				videoIDs = append(videoIDs, m.MovieMetadata.VideoID)
			}
		}
		if len(videoIDs) > 0 {
			statusMap, err := GetMovieStatusMap(ctx, db, uID, videoIDs)
			if err == nil {
				for _, m := range list {
					if m.MovieMetadata != nil {
						if st, ok := statusMap[m.MovieMetadata.VideoID]; ok {
							m.UserWatched = st.Watched
							m.UserRating = st.Rating
							m.UserPosterLayout = st.PosterLayout
						}
					}
				}
			}
		}
	}

	return list, nil
}

func GetLibraryAdultTorrentListAll(ctx context.Context, db *pg.DB, sort SortType, q string) ([]*Library, error) {
	var list []*Library
	query := db.Model(&list).
		Context(ctx).
		ColumnExpr("DISTINCT ON (library.resource_id) library.*").
		Join("join movie as m").
		JoinOn("m.resource_id = library.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("m.movie_metadata_id = mmd.movie_metadata_id").
		Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR m.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("Torrent")

	if q != "" {
		query.Where("torrent.name ILIKE ?", "%"+q+"%")
	}

	applyAllUsersLibrarySort(query, sort)
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user adult torrent list")
	}
	return list, nil
}

// MergeSeriesByVideoID deduplicates a flat list of Series by their VideoID
// (from series_metadata), merging episodes from all matching torrents into
// one representative entry per show. Series without metadata are kept as-is.
func MergeSeriesByVideoID(list []*Series) []*Series {
	type group struct {
		primary *Series
		seen    map[string]bool
	}
	groups := map[string]*group{}
	var order []string
	var noMeta []*Series

	for _, s := range list {
		if s.SeriesMetadata == nil || s.SeriesMetadata.VideoID == "" {
			noMeta = append(noMeta, s)
			continue
		}
		vid := s.SeriesMetadata.VideoID
		if _, exists := groups[vid]; !exists {
			g := &group{primary: s, seen: map[string]bool{}}
			for _, ep := range s.Episodes {
				g.seen[seriesEpisodeKey(ep)] = true
			}
			groups[vid] = g
			order = append(order, vid)
		} else {
			g := groups[vid]
			for _, ep := range s.Episodes {
				key := seriesEpisodeKey(ep)
				if !g.seen[key] {
					g.seen[key] = true
					g.primary.Episodes = append(g.primary.Episodes, ep)
				}
			}
		}
	}

	result := make([]*Series, 0, len(order)+len(noMeta))
	for _, vid := range order {
		merged := groups[vid].primary
		sort.Slice(merged.Episodes, func(i, j int) bool {
			si, ei := seriesEpNums(merged.Episodes[i])
			sj, ej := seriesEpNums(merged.Episodes[j])
			if si != sj {
				return si < sj
			}
			return ei < ej
		})
		result = append(result, merged)
	}
	return append(result, noMeta...)
}

func seriesEpisodeKey(ep *Episode) string {
	var sea, epn int16
	if ep.Season != nil {
		sea = *ep.Season
	}
	if ep.Episode != nil {
		epn = *ep.Episode
	}
	return fmt.Sprintf("%d:%d", sea, epn)
}

func seriesEpNums(ep *Episode) (int16, int16) {
	var sea, epn int16
	if ep.Season != nil {
		sea = *ep.Season
	}
	if ep.Episode != nil {
		epn = *ep.Episode
	}
	return sea, epn
}
