package library

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

type RateFormData struct {
	VideoID       string
	Type          string // "movie" or "series"
	CurrentRating int
}

type IndexData struct {
	Args          *shared.IndexArgs
	Items         []any
	TorrentCount  int
	MovieCount    int
	SeriesCount   int
	RateForm      *RateFormData
}

func (s *Handler) bindIndexArgs(c *gin.Context) (args *shared.IndexArgs) {
	args = &shared.IndexArgs{}
	if c.Query("sort") == "" {
		args.Sort = models.SortTypeRecentlyAdded
	} else {
		if ss, err := strconv.Atoi(c.Query("sort")); err == nil {
			args.Sort = models.SortType(ss)
		}
	}
	if c.Param("type") == "" {
		args.Section = shared.SectionTypeTorrents
	} else {
		args.Section = shared.SectionType(c.Param("type"))
	}
	switch shared.WatchedFilter(c.Query("watched")) {
	case shared.WatchedFilterUnwatched:
		args.Watched = shared.WatchedFilterUnwatched
	case shared.WatchedFilterWatched:
		args.Watched = shared.WatchedFilterWatched
	default:
		args.Watched = shared.WatchedFilterAll
	}
	args.Query = strings.TrimSpace(c.Query("q"))
	return
}

func (s *Handler) index(c *gin.Context) {
	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/login"))
		return
	}
	args := s.bindIndexArgs(c)
	data := &IndexData{
		Args: args,
	}

	switch args.Section {
	case shared.SectionTypeTorrents:
		list, err := models.GetLibraryTorrentsList(c.Request.Context(), db, user.ID, args.Sort, args.Query)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		data.Items = make([]any, len(list))
		for i, v := range list {
			data.Items[i] = v
		}
	case shared.SectionTypeMovies:
		list, err := models.GetLibraryMovieList(c.Request.Context(), db, user.ID, args.Sort, string(args.Watched), args.Query)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		data.Items = make([]any, len(list))
		for i, v := range list {
			data.Items[i] = v
		}
	case shared.SectionTypeSeries:
		list, err := models.GetLibrarySeriesList(c.Request.Context(), db, user.ID, args.Sort, string(args.Watched), args.Query)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		data.Items = make([]any, len(list))
		for i, v := range list {
			data.Items[i] = v
		}
	}

	tc, mc, sc, err := models.GetLibraryCounts(c.Request.Context(), db, user.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	data.TorrentCount = tc
	data.MovieCount = mc
	data.SeriesCount = sc

	if c.Query("rate-form") != "" {
		data.RateForm = &RateFormData{
			VideoID:       c.Query("video_id"),
			Type:          c.Query("rate-form"),
			CurrentRating: 0,
		}
		if r, err := strconv.Atoi(c.Query("rating")); err == nil {
			data.RateForm.CurrentRating = r
		}
	}

	h := s.tb.Build("library/index")
	if c.Query("from") != "" {
		h.WithLayout("")
	}
	h.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}
