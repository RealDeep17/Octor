package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

type LibraryData struct {
	Args         *shared.IndexArgs
	Users        []UserOption
	SelectedUser string
	Items        []any
	TorrentCount int
	MovieCount   int
	SeriesCount  int
	AdultCount   int
}

func (h *Handler) library(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	userID, selected, err := selectedUserID(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	section := parseSection(c)
	q := strings.TrimSpace(c.Query("q"))
	sort := parseSort(c)
	items, err := h.loadLibrary(ctx, db, userID, section, sort, q)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	if h.enricher != nil {
		lang := i18n.GetLang(c)
		for _, item := range items {
			if video, ok := item.(AdminVideoItem); ok {
				h.enricher.Localize(ctx, video.Content.GetMetadata(), lang)
			}
		}
	}
	users, err := h.loadUsers(ctx, db, selected)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	tc, mc, sc, ac, err := h.loadLibraryCounts(ctx, db, userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	h.tb.Build("admin/library").HTML(http.StatusOK, web.NewContext(c).WithData(&LibraryData{
		Args: &shared.IndexArgs{
			Section: section,
			Sort:    sort,
			Query:   q,
		},
		Users:        users,
		SelectedUser: selected,
		Items:        items,
		TorrentCount: tc,
		MovieCount:   mc,
		SeriesCount:  sc,
		AdultCount:   ac,
	}))
}

func (h *Handler) remove(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx := c.Request.Context()
	rID := c.PostForm("resource_id")
	uIDRaw := c.PostForm("user_id")
	alsoVault := c.PostForm("also_vault") == "true"
	allUsers := c.PostForm("all_users") == "true"

	if rID == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	if allUsers {
		_, err := db.Model((*models.Library)(nil)).
			Context(ctx).
			Where("resource_id = ?", rID).
			Delete()
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to remove from library"))
			return
		}
		if alsoVault && h.vault != nil {
			pledges, err := vaultModels.GetResourcePledges(ctx, db, rID)
			if err == nil {
				for _, p := range pledges {
					pCopy := p
					_ = h.vault.RemovePledge(ctx, &pCopy)
				}
			}
		}
		web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
		return
	}

	if uIDRaw == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	uID, err := uuid.FromString(uIDRaw)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid user id"))
		return
	}

	if err := models.RemoveFromLibrary(ctx, db, uID, rID); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to remove from library"))
		return
	}

	if alsoVault && h.vault != nil {
		resource, err := h.vault.GetResource(ctx, rID)
		if err == nil && resource != nil {
			pledge, err := h.vault.GetPledge(ctx, &auth.User{ID: uID}, resource)
			if err == nil && pledge != nil {
				_ = h.vault.RemovePledge(ctx, pledge)
				if h.api != nil {
					claims := api.GetClaimsFromContext(c)
					purgeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					_ = h.api.PurgeResourceCache(purgeCtx, claims, rID)
				}
			}
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}

func (h *Handler) removeMultiple(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx := c.Request.Context()

	var req struct {
		UserIDs     []string `form:"user_ids[]"`
		ResourceIDs []string `form:"resource_ids[]"`
		AllUsers    string   `form:"all_users"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	allUsers := req.AllUsers == "true"
	alsoVault := c.PostForm("also_vault") == "true"

	for i, rIDRaw := range req.ResourceIDs {
		rID := strings.TrimSpace(rIDRaw)
		if rID == "" {
			continue
		}

		if allUsers {
			_, _ = db.Model((*models.Library)(nil)).
				Context(ctx).
				Where("resource_id = ?", rID).
				Delete()
			if alsoVault && h.vault != nil {
				pledges, err := vaultModels.GetResourcePledges(ctx, db, rID)
				if err == nil {
					for _, p := range pledges {
						pCopy := p
						_ = h.vault.RemovePledge(ctx, &pCopy)
					}
				}
			}
		} else {
			if i >= len(req.UserIDs) {
				continue
			}
			uIDRaw := strings.TrimSpace(req.UserIDs[i])
			if uIDRaw == "" {
				continue
			}

			uID, err := uuid.FromString(uIDRaw)
			if err != nil {
				continue
			}

			_ = models.RemoveFromLibrary(ctx, db, uID, rID)
		}
	}
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}

func (h *Handler) loadUsers(ctx context.Context, db *pg.DB, selected string) ([]UserOption, error) {
	var users []models.User
	if err := db.Model(&users).Context(ctx).Order("email ASC").Select(); err != nil {
		return nil, errors.Wrap(err, "failed to load users")
	}

	type UserStats struct {
		UserID        uuid.UUID `pg:"user_id"`
		VaultedCount  int       `pg:"vaulted_count"`
		VaultingCount int       `pg:"vaulting_count"`
	}
	var stats []UserStats
	_, err := db.QueryContext(ctx, &stats, `
		SELECT 
			p.user_id,
			COUNT(CASE WHEN r.vaulted = true AND r.expired = false THEN 1 END) as vaulted_count,
			COUNT(CASE WHEN r.vaulted = false AND r.expired = false THEN 1 END) as vaulting_count
		FROM vault.pledge p
		JOIN vault.resource r ON p.resource_id = r.resource_id
		GROUP BY p.user_id
	`)

	statsMap := make(map[uuid.UUID]UserStats)
	var totalVaulted, totalVaulting int
	if err == nil {
		for _, s := range stats {
			statsMap[s.UserID] = s
			totalVaulted += s.VaultedCount
			totalVaulting += s.VaultingCount
		}
	} else {
		log.WithError(err).Warn("failed to load user vault stats for admin dropdown")
	}

	opts := []UserOption{{
		ID:            "",
		Email:         "All users",
		Selected:      selected == "",
		Tier:          "",
		VaultedCount:  totalVaulted,
		VaultingCount: totalVaulting,
	}}
	for _, u := range users {
		id := u.UserID.String()
		s := statsMap[u.UserID]
		tier := u.Tier
		if tier == "paid" {
			tier = "Paid"
		} else if tier == "free" || tier == "" {
			tier = "Free"
		} else if len(tier) > 0 {
			tier = strings.ToUpper(tier[:1]) + strings.ToLower(tier[1:])
		}
		opts = append(opts, UserOption{
			ID:            id,
			Email:         u.Email,
			Selected:      selected == id,
			Tier:          tier,
			VaultedCount:  s.VaultedCount,
			VaultingCount: s.VaultingCount,
		})
	}
	return opts, nil
}

func selectedUserID(c *gin.Context) (*uuid.UUID, string, error) {
	raw := c.Query("user")
	if raw == "" {
		return nil, "", nil
	}
	id, err := uuid.FromString(raw)
	if err != nil {
		return nil, raw, errors.Wrap(err, "invalid user id")
	}
	return &id, raw, nil
}

func parseSort(c *gin.Context) models.SortType {
	if c.Query("sort") == "" {
		return models.SortTypeRecentlyAdded
	}
	n, err := strconv.Atoi(c.Query("sort"))
	if err != nil {
		return models.SortTypeRecentlyAdded
	}
	return models.SortType(n)
}

func parseSection(c *gin.Context) shared.SectionType {
	s := shared.SectionType(c.Param("type"))
	switch s {
	case shared.SectionTypeMovies, shared.SectionTypeSeries, shared.SectionTypeAdult:
		return s
	default:
		return shared.SectionTypeTorrents
	}
}

func (h *Handler) loadLibrary(ctx context.Context, db *pg.DB, userID *uuid.UUID, section shared.SectionType, sort models.SortType, q string) ([]any, error) {
	switch section {
	case shared.SectionTypeMovies:
		return h.loadMovieItems(ctx, db, userID, sort, q)
	case shared.SectionTypeSeries:
		return h.loadSeriesItems(ctx, db, userID, sort, q)
	case shared.SectionTypeAdult:
		return h.loadAdultItems(ctx, db, userID, sort, q)
	default:
		return h.loadTorrentItems(ctx, db, userID, sort, q)
	}
}

func (h *Handler) loadTorrentItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType, q string) ([]any, error) {
	var list []*models.Library
	var err error
	if userID != nil {
		list, err = models.GetLibraryTorrentsList(ctx, db, *userID, sort, q)
	} else {
		list, err = models.GetLibraryTorrentsListAll(ctx, db, sort, q)
	}
	if err != nil {
		return nil, err
	}
	owners, err := h.loadOwnerSummaries(ctx, db, libraryResourceIDs(list), userID)
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		if owner := owners[item.ResourceID]; owner != nil {
			item.User = &models.User{Email: owner.OwnerLabel(), UserID: owner.PrimaryUserID}
			if item.UserID == uuid.Nil {
				item.UserID = owner.PrimaryUserID
			}
		}
	}
	tree := shared.BuildTorrentTree(ctx, db, list, true, sort)
	items := make([]any, len(tree))
	for i, v := range tree {
		items[i] = v
	}
	return items, nil
}

func (h *Handler) loadMovieItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType, q string) ([]any, error) {
	var list []*models.Movie
	var err error
	if userID != nil {
		list, err = models.GetLibraryMovieList(ctx, db, *userID, sort, "", q)
	} else {
		list, err = h.getAllUserMovies(ctx, db, sort, q)
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.ResourceID)
	}
	owners, err := h.loadOwnerSummaries(ctx, db, ids, userID)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(list))
	for _, item := range list {
		items = append(items, h.videoItem(item, item.ResourceID, item.CreatedAt, owners[item.ResourceID]))
	}
	return items, nil
}

func (h *Handler) loadSeriesItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType, q string) ([]any, error) {
	var list []*models.Series
	var err error
	if userID != nil {
		list, err = models.GetLibrarySeriesList(ctx, db, *userID, sort, "", q)
	} else {
		list, err = h.getAllUserSeries(ctx, db, sort, q)
	}
	if err != nil {
		return nil, err
	}

	allResourceIDs := make([]string, 0, len(list))
	for _, item := range list {
		allResourceIDs = append(allResourceIDs, item.ResourceID)
	}
	ownersByResource, err := h.loadOwnerSummaries(ctx, db, allResourceIDs, userID)
	if err != nil {
		return nil, err
	}

	mergedList := models.MergeSeriesByVideoID(list)

	ownersByGroup := map[string]*AdminVideoItem{}
	for _, item := range list {
		var groupKey string
		if item.SeriesMetadata != nil && item.SeriesMetadata.VideoID != "" {
			groupKey = item.SeriesMetadata.VideoID
		} else {
			groupKey = item.ResourceID
		}

		owner := ownersByResource[item.ResourceID]
		if owner == nil {
			continue
		}

		agg := ownersByGroup[groupKey]
		if agg == nil {
			agg = &AdminVideoItem{
				ResourceID:    item.ResourceID,
				PrimaryEmail:  owner.PrimaryEmail,
				PrimaryUserID: owner.PrimaryUserID,
				OwnerIDs:      append([]uuid.UUID(nil), owner.OwnerIDs...),
				OwnerEmails:   append([]string(nil), owner.OwnerEmails...),
			}
			ownersByGroup[groupKey] = agg
		} else {
			for _, uid := range owner.OwnerIDs {
				found := false
				for _, existing := range agg.OwnerIDs {
					if existing == uid {
						found = true
						break
					}
				}
				if !found {
					agg.OwnerIDs = append(agg.OwnerIDs, uid)
				}
			}
			for _, email := range owner.OwnerEmails {
				found := false
				for _, existing := range agg.OwnerEmails {
					if existing == email {
						found = true
						break
					}
				}
				if !found {
					agg.OwnerEmails = append(agg.OwnerEmails, email)
				}
			}
		}
	}

	for _, agg := range ownersByGroup {
		agg.OwnerCount = len(agg.OwnerIDs)
	}

	items := make([]any, 0, len(mergedList))
	for _, item := range mergedList {
		var groupKey string
		if item.SeriesMetadata != nil && item.SeriesMetadata.VideoID != "" {
			groupKey = item.SeriesMetadata.VideoID
		} else {
			groupKey = item.ResourceID
		}
		items = append(items, h.videoItem(item, item.ResourceID, item.CreatedAt, ownersByGroup[groupKey]))
	}
	return items, nil
}

func (h *Handler) getAllUserMovies(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Movie, error) {
	var list []*models.Movie
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Where("(mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND movie.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("MovieMetadata")

	if q != "" {
		query.Where("mmd.title ILIKE ? OR movie.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch sort {
	case models.SortTypeName:
		query.OrderExpr("mmd.title ASC NULLS LAST, movie.title ASC")
	case models.SortTypeYear:
		query.OrderExpr("mmd.year DESC NULLS LAST, movie.year DESC NULLS LAST")
	case models.SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
	default:
		query.OrderExpr("movie.created_at DESC")
	}
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user movie list")
	}
	return list, nil
}

func (h *Handler) getAllUserSeries(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Series, error) {
	var list []*models.Series
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("series.resource_id = l.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Relation("SeriesMetadata").
		Relation("Episodes.EpisodeMetadata")

	if q != "" {
		query.Where("smd.title ILIKE ? OR series.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch sort {
	case models.SortTypeName:
		query.OrderExpr("smd.title ASC NULLS LAST, series.title ASC")
	case models.SortTypeYear:
		query.OrderExpr("smd.year DESC NULLS LAST, series.year DESC NULLS LAST")
	case models.SortTypeRating:
		query.OrderExpr("smd.rating DESC NULLS LAST")
	default:
		query.OrderExpr("series.created_at DESC")
	}
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user series list")
	}
	models.PopulateSeriesAnimeFlags(ctx, db, list)
	return list, nil
}

func (h *Handler) videoItem(content models.VideoContentWithMetadata, resourceID string, createdAt time.Time, owner *AdminVideoItem) AdminVideoItem {
	item := AdminVideoItem{Content: content, ResourceID: resourceID, CreatedAt: createdAt, OwnerCount: 1}
	if owner != nil {
		item.OwnerCount = owner.OwnerCount
		item.OwnerEmails = owner.OwnerEmails
		item.OwnerIDs = owner.OwnerIDs
		item.PrimaryEmail = owner.PrimaryEmail
		item.PrimaryUserID = owner.PrimaryUserID
	}
	return item
}

func libraryResourceIDs(list []*models.Library) []string {
	ids := make([]string, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.ResourceID)
	}
	return ids
}

func (h *Handler) loadOwnerSummaries(ctx context.Context, db *pg.DB, resourceIDs []string, userID *uuid.UUID) (map[string]*AdminVideoItem, error) {
	res := map[string]*AdminVideoItem{}
	if len(resourceIDs) == 0 {
		return res, nil
	}
	var rows []OwnerSummary
	query := db.Model((*models.Library)(nil)).
		Context(ctx).
		ColumnExpr("library.resource_id").
		ColumnExpr("count(distinct library.user_id) AS user_count").
		ColumnExpr("string_agg(distinct u.email, ', ') AS owner_emails").
		ColumnExpr("string_agg(distinct u.user_id::text, ', ') AS owner_ids").
		ColumnExpr("min(u.email) AS primary_email").
		ColumnExpr("min(u.user_id::text)::uuid AS primary_user_id").
		Join("join \"user\" as u").
		JoinOn("u.user_id = library.user_id").
		Where("library.resource_id IN (?)", pg.In(resourceIDs)).
		Group("library.resource_id")
	if userID != nil {
		query.Where("library.user_id = ?", *userID)
	}
	if err := query.Select(&rows); err != nil {
		return nil, errors.Wrap(err, "failed to load owner summaries")
	}
	for _, row := range rows {
		emails := splitEmails(row.OwnerEmails)
		ids := splitIDs(row.OwnerIDs)
		res[row.ResourceID] = &AdminVideoItem{
			ResourceID:    row.ResourceID,
			OwnerCount:    row.UserCount,
			OwnerEmails:   emails,
			OwnerIDs:      ids,
			PrimaryEmail:  row.PrimaryEmail,
			PrimaryUserID: row.PrimaryUserID,
		}
	}
	return res, nil
}

func splitIDs(s string) []uuid.UUID {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	out := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			if id, err := uuid.FromString(part); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

func splitEmails(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (h *Handler) loadLibraryCounts(ctx context.Context, db *pg.DB, userID *uuid.UUID) (torrents, movies, series, adult int, err error) {
	if userID != nil {
		return models.GetLibraryCounts(ctx, db, *userID)
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&torrents), "select count(distinct resource_id) from library")
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count all-user torrents")
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&movies), "select count(distinct movie.resource_id) from movie join library as l on movie.resource_id = l.resource_id left join movie_metadata as mmd on movie.movie_metadata_id = mmd.movie_metadata_id where (mmd.video_id IS NULL OR (mmd.video_id NOT LIKE 'tpdb:%' AND mmd.video_id NOT LIKE 'tpdb_jav:%' AND mmd.video_id NOT LIKE 'stash:%')) AND movie.path !~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'")
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count all-user movies")
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&series), `
		SELECT COUNT(DISTINCT COALESCE(NULLIF(smd.video_id, ''), series.series_id::text))
		FROM series
		JOIN (SELECT DISTINCT resource_id FROM library) as l ON series.resource_id = l.resource_id
		LEFT JOIN series_metadata as smd ON series.series_metadata_id = smd.series_metadata_id
	`)
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count all-user series")
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&adult), "select count(distinct movie.resource_id) from movie join library as l on movie.resource_id = l.resource_id left join movie_metadata as mmd on movie.movie_metadata_id = mmd.movie_metadata_id where (mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR movie.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'")
	if err != nil {
		return 0, 0, 0, 0, errors.Wrap(err, "failed to count all-user adult")
	}
	return
}

func (h *Handler) loadAdultItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType, q string) ([]any, error) {
	var list []*models.Movie
	var err error
	if userID != nil {
		list, err = models.GetLibraryAdultList(ctx, db, *userID, sort, "", q)
	} else {
		list, err = h.getAllUserAdults(ctx, db, sort, q)
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		ids = append(ids, item.ResourceID)
	}
	owners, err := h.loadOwnerSummaries(ctx, db, ids, userID)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(list))
	for _, item := range list {
		items = append(items, h.videoItem(item, item.ResourceID, item.CreatedAt, owners[item.ResourceID]))
	}
	return items, nil
}

func (h *Handler) getAllUserAdults(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Movie, error) {
	var list []*models.Movie
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Where("(mmd.video_id LIKE 'tpdb:%' OR mmd.video_id LIKE 'tpdb_jav:%' OR mmd.video_id LIKE 'stash:%') OR movie.path ~* '(porn|adult|xxx|jav|brazzers|bangbros|hentai|slut|pornstar|nude)'").
		Relation("MovieMetadata")

	if q != "" {
		query.Where("mmd.title ILIKE ? OR movie.title ILIKE ?", "%"+q+"%", "%"+q+"%")
	}

	switch sort {
	case models.SortTypeName:
		query.OrderExpr("mmd.title ASC NULLS LAST, movie.title ASC")
	case models.SortTypeYear:
		query.OrderExpr("mmd.year DESC NULLS LAST, movie.year DESC NULLS LAST")
	case models.SortTypeRating:
		query.OrderExpr("mmd.rating DESC NULLS LAST")
	default:
		query.OrderExpr("movie.created_at DESC")
	}
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to fetch all-user adult list")
	}
	return list, nil
}
