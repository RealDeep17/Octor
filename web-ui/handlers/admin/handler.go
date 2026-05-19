package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb       template.Builder[*web.Context]
	pg       *cs.PG
	vault    *vault.Vault
	enricher *enrich.Enricher
	admin    *adminsvc.Admin
}

type UserOption struct {
	ID       string
	Email    string
	Selected bool
}

type OwnerSummary struct {
	ResourceID   string `pg:"resource_id"`
	UserCount    int    `pg:"user_count"`
	OwnerEmails  string `pg:"owner_emails"`
	PrimaryEmail string `pg:"primary_email"`
}

type AdminVideoItem struct {
	Content      models.VideoContentWithMetadata
	ResourceID   string
	CreatedAt    time.Time
	OwnerCount   int
	OwnerEmails  []string
	PrimaryEmail string
}

func (i AdminVideoItem) OwnerLabel() string {
	if i.OwnerCount <= 0 {
		return "No users"
	}
	if i.OwnerCount == 1 {
		if i.PrimaryEmail != "" {
			return i.PrimaryEmail
		}
		if len(i.OwnerEmails) > 0 {
			return i.OwnerEmails[0]
		}
		return "1 user"
	}
	return fmt.Sprintf("%d users", i.OwnerCount)
}

func (i AdminVideoItem) OwnerTitle() string {
	if len(i.OwnerEmails) == 0 {
		return i.OwnerLabel()
	}
	return strings.Join(i.OwnerEmails, ", ")
}

type LibraryData struct {
	Args         *shared.IndexArgs
	Users        []UserOption
	SelectedUser string
	Items        []any
	TorrentCount int
	MovieCount   int
	SeriesCount  int
}

type VaultData struct {
	Users        []UserOption
	SelectedUser string
	Pledges      []vaultModels.Pledge
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, v *vault.Vault, en *enrich.Enricher, admin *adminsvc.Admin) {
	h := &Handler{
		tb:       tm.MustRegisterViews("admin/*").WithLayout("main"),
		pg:       pg,
		vault:    v,
		enricher: en,
		admin:    admin,
	}
	gr := r.Group("/admin")
	gr.Use(admin.Require())
	gr.GET("", func(c *gin.Context) { c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/admin/library")) })
	gr.GET("/library", h.library)
	gr.GET("/library/:type", h.library)
	gr.GET("/vault", h.vaultIndex)
}

func (h *Handler) db() (*pg.DB, error) {
	db := h.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return db, nil
}

func (h *Handler) loadUsers(ctx context.Context, db *pg.DB, selected string) ([]UserOption, error) {
	var users []models.User
	if err := db.Model(&users).Context(ctx).Order("email ASC").Select(); err != nil {
		return nil, errors.Wrap(err, "failed to load users")
	}
	opts := []UserOption{{ID: "", Email: "All users", Selected: selected == ""}}
	for _, u := range users {
		id := u.UserID.String()
		opts = append(opts, UserOption{ID: id, Email: u.Email, Selected: selected == id})
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
	case shared.SectionTypeMovies, shared.SectionTypeSeries:
		return s
	default:
		return shared.SectionTypeTorrents
	}
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
	args := &shared.IndexArgs{Section: section, Sort: parseSort(c), Watched: shared.WatchedFilterAll}
	items, err := h.loadLibrary(ctx, db, userID, section, args.Sort)
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
	tc, mc, sc, err := h.loadLibraryCounts(ctx, db, userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	h.tb.Build("admin/library").HTML(http.StatusOK, web.NewContext(c).WithData(&LibraryData{
		Args:         args,
		Users:        users,
		SelectedUser: selected,
		Items:        items,
		TorrentCount: tc,
		MovieCount:   mc,
		SeriesCount:  sc,
	}))
}

func (h *Handler) loadLibrary(ctx context.Context, db *pg.DB, userID *uuid.UUID, section shared.SectionType, sort models.SortType) ([]any, error) {
	switch section {
	case shared.SectionTypeMovies:
		return h.loadMovieItems(ctx, db, userID, sort)
	case shared.SectionTypeSeries:
		return h.loadSeriesItems(ctx, db, userID, sort)
	default:
		return h.loadTorrentItems(ctx, db, userID, sort)
	}
}

func (h *Handler) loadTorrentItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType) ([]any, error) {
	var list []*models.Library
	var err error
	if userID != nil {
		list, err = models.GetLibraryTorrentsList(ctx, db, *userID, sort)
	} else {
		list, err = models.GetLibraryTorrentsListAll(ctx, db, sort)
	}
	if err != nil {
		return nil, err
	}
	owners, err := h.loadOwnerSummaries(ctx, db, libraryResourceIDs(list), userID)
	if err != nil {
		return nil, err
	}
	items := make([]any, 0, len(list))
	for _, item := range list {
		if owner := owners[item.ResourceID]; owner != nil {
			item.User = &models.User{Email: owner.OwnerLabel()}
		}
		items = append(items, item)
	}
	return items, nil
}

func (h *Handler) loadMovieItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType) ([]any, error) {
	var list []*models.Movie
	var err error
	if userID != nil {
		list, err = models.GetLibraryMovieList(ctx, db, *userID, sort, "")
	} else {
		list, err = h.getAllUserMovies(ctx, db, sort)
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

func (h *Handler) loadSeriesItems(ctx context.Context, db *pg.DB, userID *uuid.UUID, sort models.SortType) ([]any, error) {
	var list []*models.Series
	var err error
	if userID != nil {
		list, err = models.GetLibrarySeriesList(ctx, db, *userID, sort, "")
	} else {
		list, err = h.getAllUserSeries(ctx, db, sort)
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

func (h *Handler) getAllUserMovies(ctx context.Context, db *pg.DB, sort models.SortType) ([]*models.Movie, error) {
	var list []*models.Movie
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
		Relation("MovieMetadata")
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

func (h *Handler) getAllUserSeries(ctx context.Context, db *pg.DB, sort models.SortType) ([]*models.Series, error) {
	var list []*models.Series
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("series.resource_id = l.resource_id").
		Join("left join series_metadata as smd").
		JoinOn("series.series_metadata_id = smd.series_metadata_id").
		Relation("SeriesMetadata")
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
	return list, nil
}

func (h *Handler) videoItem(content models.VideoContentWithMetadata, resourceID string, createdAt time.Time, owner *AdminVideoItem) AdminVideoItem {
	item := AdminVideoItem{Content: content, ResourceID: resourceID, CreatedAt: createdAt, OwnerCount: 1}
	if owner != nil {
		item.OwnerCount = owner.OwnerCount
		item.OwnerEmails = owner.OwnerEmails
		item.PrimaryEmail = owner.PrimaryEmail
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
	query := db.Model(&rows).
		Context(ctx).
		TableExpr("library AS l").
		ColumnExpr("l.resource_id").
		ColumnExpr("count(distinct l.user_id) AS user_count").
		ColumnExpr("string_agg(distinct u.email, ', ' ORDER BY u.email) AS owner_emails").
		ColumnExpr("min(u.email) AS primary_email").
		Join("join \"user\" as u").
		JoinOn("u.user_id = l.user_id").
		Where("l.resource_id IN (?)", pg.In(resourceIDs)).
		Group("l.resource_id")
	if userID != nil {
		query.Where("l.user_id = ?", *userID)
	}
	if err := query.Select(); err != nil {
		return nil, errors.Wrap(err, "failed to load owner summaries")
	}
	for _, row := range rows {
		emails := splitEmails(row.OwnerEmails)
		res[row.ResourceID] = &AdminVideoItem{
			ResourceID:   row.ResourceID,
			OwnerCount:   row.UserCount,
			OwnerEmails:  emails,
			PrimaryEmail: row.PrimaryEmail,
		}
	}
	return res, nil
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

func (h *Handler) loadLibraryCounts(ctx context.Context, db *pg.DB, userID *uuid.UUID) (torrents, movies, series int, err error) {
	if userID != nil {
		return models.GetLibraryCounts(ctx, db, *userID)
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&torrents), "select count(distinct resource_id) from library")
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count all-user torrents")
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&movies), "select count(distinct movie.resource_id) from movie join library as l on movie.resource_id = l.resource_id")
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count all-user movies")
	}
	_, err = db.QueryOneContext(ctx, pg.Scan(&series), "select count(distinct series.resource_id) from series join library as l on series.resource_id = l.resource_id")
	if err != nil {
		return 0, 0, 0, errors.Wrap(err, "failed to count all-user series")
	}
	return
}

func (h *Handler) vaultIndex(c *gin.Context) {
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
	var pledges []vaultModels.Pledge
	query := db.Model(&pledges).
		Context(ctx).
		Relation("Resource").
		Relation("User").
		Order("pledge.created_at DESC")
	if userID != nil {
		query.Where("pledge.user_id = ?", *userID)
	}
	if err := query.Select(); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to load admin vault"))
		return
	}
	users, err := h.loadUsers(ctx, db, selected)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	h.tb.Build("admin/vault").HTML(http.StatusOK, web.NewContext(c).WithData(&VaultData{
		Users:        users,
		SelectedUser: selected,
		Pledges:      pledges,
	}))
}
