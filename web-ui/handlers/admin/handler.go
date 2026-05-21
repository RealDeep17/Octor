package admin

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
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
	api      *api.Api
}

type UserOption struct {
	ID       string
	Email    string
	Selected bool
}

type OwnerSummary struct {
	ResourceID   string    `pg:"resource_id"`
	UserCount    int       `pg:"user_count"`
	OwnerEmails  string    `pg:"owner_emails"`
	OwnerIDs     string    `pg:"owner_ids"`
	PrimaryEmail string    `pg:"primary_email"`
	PrimaryUserID uuid.UUID `pg:"primary_user_id"`
}

type AdminVideoItem struct {
	Content       models.VideoContentWithMetadata
	ResourceID    string
	CreatedAt     time.Time
	OwnerCount    int
	OwnerEmails   []string
	OwnerIDs      []uuid.UUID
	PrimaryEmail  string
	PrimaryUserID uuid.UUID
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

type AdminPledgeDisplay struct {
	vaultModels.Pledge
	WorkerStatus *vault.Resource
	SeedCount    int
}

func (a AdminPledgeDisplay) ShowProgress() bool {
	return a.Resource != nil &&
		a.Resource.Funded &&
		!a.Resource.Vaulted &&
		!a.Resource.Expired
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
	Args         *shared.IndexArgs
	Users        []UserOption
	SelectedUser string
	Pledges      []AdminPledgeDisplay
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, v *vault.Vault, en *enrich.Enricher, admin *adminsvc.Admin, sapi *api.Api) {
	h := &Handler{
		tb:       tm.MustRegisterViews("admin/*").WithLayout("main"),
		pg:       pg,
		vault:    v,
		enricher: en,
		admin:    admin,
		api:      sapi,
	}
	gr := r.Group("/admin")
	gr.Use(admin.Require())
	gr.GET("", func(c *gin.Context) { c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/admin/library")) })
	gr.GET("/library", h.library)
	gr.GET("/library/:type", h.library)
	gr.POST("/library/remove", h.remove)
	gr.GET("/vault", h.vaultIndex)
	gr.POST("/vault/remove", h.removePledge)
	gr.GET("/status", h.status)
	gr.GET("/drive", h.driveIndex)
	gr.GET("/drive/*path", h.driveIndex)
}

type DriveItem struct {
	Name    string
	Path    string
	Size    int64
	IsDir   bool
	ModTime time.Time
}

type DriveData struct {
	Args        *shared.IndexArgs
	Path        string
	ParentPath  string
	Items       []DriveItem
	Breadcrumbs []struct {
		Name string
		Path string
	}
}

func (h *Handler) driveIndex(c *gin.Context) {
	root := "/srv/octor/infra-data/drive-mount-vfs"
	path := c.Param("path")
	fullPath := filepath.Join(root, path)

	// Security check: ensure path is within root
	if !strings.HasPrefix(fullPath, root) {
		c.Status(http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
		} else {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
		}
		return
	}

	var items []DriveItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, DriveItem{
			Name:    entry.Name(),
			Path:    filepath.Join(path, entry.Name()),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}

	// Breadcrumbs
	var bc []struct {
		Name string
		Path string
	}
	bc = append(bc, struct {
		Name string
		Path string
	}{Name: "Root", Path: ""})
	
	parts := strings.Split(strings.Trim(path, "/"), "/")
	curr := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		curr = filepath.Join(curr, p)
		bc = append(bc, struct {
			Name string
			Path string
		}{Name: p, Path: curr})
	}

	parent := ""
	if path != "" && path != "/" {
		parent = filepath.Dir(strings.TrimSuffix(path, "/"))
		if parent == "." {
			parent = ""
		}
	}

	h.tb.Build("admin/drive").HTML(http.StatusOK, web.NewContext(c).WithData(&DriveData{
		Path:        path,
		ParentPath:  parent,
		Items:       items,
		Breadcrumbs: bc,
	}))
}

func (h *Handler) removePledge(c *gin.Context) {
	uIDRaw := c.PostForm("user_id")
	rID := c.PostForm("resource_id")

	if uIDRaw == "" || rID == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	uID, err := uuid.FromString(uIDRaw)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid user id"))
		return
	}

	resource, err := h.vault.GetResource(c.Request.Context(), rID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get resource"))
		return
	}

	if resource == nil {
		c.Status(http.StatusNotFound)
		return
	}

	pledge, err := h.vault.GetPledge(c.Request.Context(), &auth.User{ID: uID}, resource)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get pledge"))
		return
	}

	if pledge == nil {
		c.Status(http.StatusNotFound)
		return
	}

	if err := h.vault.RemovePledge(c.Request.Context(), pledge); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to remove pledge"))
		return
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}

type StatusData struct {
	CPUUsage    float64
	RAMUsed     int64
	RAMTotal    int64
	RAMPercent  float64
	DiskUsed    int64
	DiskTotal   int64
	DiskPercent float64
	StreamCount int
	SeedCount   int
}

func (h *Handler) status(c *gin.Context) {
	data := &StatusData{
		CPUUsage:    h.getCPUUsage(),
		RAMUsed:     h.getRAMUsed(),
		RAMTotal:    h.getRAMTotal(),
		DiskUsed:    h.getDiskUsed(),
		DiskTotal:   h.getDiskTotal(),
		StreamCount: h.getStreamCount(),
		SeedCount:   h.getSeedCount(),
	}
	if data.RAMTotal > 0 {
		data.RAMPercent = float64(data.RAMUsed) / float64(data.RAMTotal) * 100
	}
	if data.DiskTotal > 0 {
		data.DiskPercent = float64(data.DiskUsed) / float64(data.DiskTotal) * 100
	}

	h.tb.Build("admin/status").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

func (h *Handler) getCPUUsage() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	line := scanner.Text()
	parts := strings.Fields(line)
	if len(parts) < 5 || parts[0] != "cpu" {
		return 0
	}
	var total uint64
	for i := 1; i < len(parts); i++ {
		val, _ := strconv.ParseUint(parts[i], 10, 64)
		total += val
	}
	idle, _ := strconv.ParseUint(parts[4], 10, 64)
	time.Sleep(100 * time.Millisecond)
	f2, _ := os.Open("/proc/stat")
	defer f2.Close()
	scanner2 := bufio.NewScanner(f2)
	scanner2.Scan()
	parts2 := strings.Fields(scanner2.Text())
	var total2 uint64
	for i := 1; i < len(parts2); i++ {
		val, _ := strconv.ParseUint(parts2[i], 10, 64)
		total2 += val
	}
	idle2, _ := strconv.ParseUint(parts2[4], 10, 64)
	if total2 == total {
		return 0
	}
	return float64(100) * (1 - float64(idle2-idle)/float64(total2-total))
}

func (h *Handler) getRAMUsed() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var total, free, buffers, cached uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &total)
		} else if strings.HasPrefix(line, "MemFree:") {
			fmt.Sscanf(line, "MemFree: %d", &free)
		} else if strings.HasPrefix(line, "Buffers:") {
			fmt.Sscanf(line, "Buffers: %d", &buffers)
		} else if strings.HasPrefix(line, "Cached:") {
			fmt.Sscanf(line, "Cached: %d", &cached)
		}
	}
	return int64(total - free - buffers - cached) * 1024
}

func (h *Handler) getRAMTotal() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var total uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &total)
			break
		}
	}
	return int64(total) * 1024
}

func (h *Handler) getDiskUsed() int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/srv", &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks - stat.Bfree) * int64(stat.Bsize)
}

func (h *Handler) getDiskTotal() int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/srv", &stat); err != nil {
		return 0
	}
	return int64(stat.Blocks) * int64(stat.Bsize)
}

func (h *Handler) getStreamCount() int {
	resp, err := http.Get("http://localhost:53086/metrics")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "vault_worker_leases_held ") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				val, _ := strconv.Atoi(fields[1])
				return val
			}
		}
	}
	return 0
}

func (h *Handler) getSeedCount() int {
	resp, err := http.Get("http://localhost:53054/metrics")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "torrent_web_seeder_active_torrents_count ") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				val, _ := strconv.Atoi(fields[1])
				return val
			}
		}
	}
	return 0
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

	if rID == "" || uIDRaw == "" {
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
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
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
	tc, mc, sc, err := h.loadLibraryCounts(ctx, db, userID)
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
	}))
}

func (h *Handler) loadLibrary(ctx context.Context, db *pg.DB, userID *uuid.UUID, section shared.SectionType, sort models.SortType, q string) ([]any, error) {
	switch section {
	case shared.SectionTypeMovies:
		return h.loadMovieItems(ctx, db, userID, sort, q)
	case shared.SectionTypeSeries:
		return h.loadSeriesItems(ctx, db, userID, sort, q)
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
	items := make([]any, 0, len(list))
	for _, item := range list {
		if owner := owners[item.ResourceID]; owner != nil {
			item.User = &models.User{Email: owner.OwnerLabel(), UserID: owner.PrimaryUserID}
			if item.UserID == uuid.Nil {
				item.UserID = owner.PrimaryUserID
			}
		}
		items = append(items, item)
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

func (h *Handler) getAllUserMovies(ctx context.Context, db *pg.DB, sort models.SortType, q string) ([]*models.Movie, error) {
	var list []*models.Movie
	query := db.Model(&list).
		Context(ctx).
		Join("join (select distinct resource_id from library) as l").
		JoinOn("movie.resource_id = l.resource_id").
		Join("left join movie_metadata as mmd").
		JoinOn("movie.movie_metadata_id = mmd.movie_metadata_id").
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
		Relation("SeriesMetadata")

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
		ColumnExpr("min(u.user_id::text) AS primary_user_id").
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
	q := strings.TrimSpace(c.Query("q"))
	var pledges []vaultModels.Pledge
	query := db.Model(&pledges).
		Context(ctx).
		Relation("Resource").
		Relation("User").
		Order("pledge.created_at DESC")
	if userID != nil {
		query.Where("pledge.user_id = ?", *userID)
	}
	if q != "" {
		query.Where("resource.name ILIKE ?", "%"+q+"%")
	}
	if err := query.Select(); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to load admin vault"))
		return
	}

	enrichedPledges := make([]AdminPledgeDisplay, 0, len(pledges))
	for _, p := range pledges {
		item := AdminPledgeDisplay{Pledge: p}
		// Fetch worker status and seeds for active/fundable resources
		if p.Resource != nil && p.Resource.Funded && !p.Resource.Vaulted && !p.Resource.Expired {
			status, err := h.vault.GetVaultAPIResource(ctx, p.ResourceID)
			if err == nil && status != nil {
				item.WorkerStatus = status
			}
			// Attempt one-shot seed count fetch
			item.SeedCount = h.getLiveSeeds(ctx, c, p.ResourceID)
		}
		enrichedPledges = append(enrichedPledges, item)
	}

	users, err := h.loadUsers(ctx, db, selected)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	h.tb.Build("admin/vault").HTML(http.StatusOK, web.NewContext(c).WithData(&VaultData{
		Args: &shared.IndexArgs{
			Query: q,
		},
		Users:        users,
		SelectedUser: selected,
		Pledges:      enrichedPledges,
	}))
}

func (h *Handler) getLiveSeeds(ctx context.Context, c *gin.Context, resourceID string) int {
	wcc := web.NewContext(c)
	er, err := h.api.ExportResourceContent(ctx, wcc.ApiClaims, resourceID, resourceID, "")
	if err != nil {
		return 0
	}
	statsURL, ok := er.ExportItems["stats"]
	if !ok || statsURL.URL == "" {
		return 0
	}
	
	// Create a shorter context for the SSE peek
	shortCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	
	ch, err := h.api.Stats(shortCtx, statsURL.URL)
	if err != nil {
		return 0
	}
	
	select {
	case event, ok := <-ch:
		if ok {
			return event.Peers
		}
	case <-shortCtx.Done():
		return 0
	}
	return 0
}
