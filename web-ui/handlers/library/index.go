package library

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
	"golang.org/x/sync/errgroup"
)

type RateFormData struct {
	VideoID       string
	Type          string // "movie" or "series"
	CurrentRating int
}

type Group struct {
	Title     string
	PosterURL string
	Items     []any
}

type IndexData struct {
	Args          *shared.IndexArgs
	Items         []any
	TorrentCount  int
	MovieCount    int
	SeriesCount   int
	AdultCount    int
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
	case shared.WatchedFilterVaulted:
		args.Watched = shared.WatchedFilterVaulted
	default:
		args.Watched = shared.WatchedFilterAll
	}
	args.Query = strings.TrimSpace(c.Query("q"))
	args.IsAdmin = s.admin.HasAdmin(c)
	args.GroupBy = shared.GroupBy(c.Query("group"))
	args.Subtype = strings.TrimSpace(c.Query("subtype"))
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
	if args.Section == shared.SectionTypeAdult && !args.IsAdmin {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
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
		tree := shared.BuildTorrentTree(c.Request.Context(), db, list, args.IsAdmin, args.Sort)
		data.Items = make([]any, len(tree))
		for i, v := range tree {
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
		rawList, err := models.GetLibrarySeriesList(c.Request.Context(), db, user.ID, args.Sort, string(args.Watched), args.Query)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		list := models.MergeSeriesByVideoID(rawList)
		if args.Subtype != "" && args.Subtype != "all" {
			var filtered []*models.Series
			for _, s := range list {
				if args.Subtype == "anime" && s.IsAnime {
					filtered = append(filtered, s)
				} else if args.Subtype == "tv" && !s.IsAnime {
					filtered = append(filtered, s)
				}
			}
			list = filtered
		}
		data.Items = make([]any, len(list))
		for i, v := range list {
			data.Items[i] = v
		}
	case shared.SectionTypeAdult:
		list, err := models.GetLibraryAdultList(c.Request.Context(), db, user.ID, args.Sort, string(args.Watched), args.Query)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		if args.Subtype != "" && args.Subtype != "all" {
			var filtered []*models.Movie
			for _, m := range list {
				if args.Subtype == "jav" && m.IsJav() {
					filtered = append(filtered, m)
				} else if args.Subtype == "porn" && m.IsPorn() {
					filtered = append(filtered, m)
				}
			}
			list = filtered
		}

		if args.GroupBy == shared.GroupByStudio {
			groups := make(map[string]*Group)
			var groupOrder []string
			for _, m := range list {
				studio := "Unknown Studio"
				if m.Metadata != nil {
					if s, ok := m.Metadata["Director"].(string); ok && s != "" && s != "N/A" {
						studio = s
					}
				}
				if studio == "Unknown Studio" && m.Path != nil {
					if _, s := enrich.IsAdultPath(*m.Path); s != "" {
						studio = s
					}
				}
				if _, ok := groups[studio]; !ok {
					groups[studio] = &Group{
						Title: studio,
					}
					groupOrder = append(groupOrder, studio)
				}
				groups[studio].Items = append(groups[studio].Items, m)
			}
			if s.tpdb != nil {
				g, gctx := errgroup.WithContext(c.Request.Context())
				gctx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()
				var mu sync.Mutex
				for _, studio := range groupOrder {
					studio := studio
					g.Go(func() error {
						poster, _ := s.tpdb.FetchStudioPoster(gctx, studio)
						mu.Lock()
						groups[studio].PosterURL = poster
						mu.Unlock()
						return nil
					})
				}
				_ = g.Wait()
			}
			data.Items = make([]any, len(groupOrder))
			for i, title := range groupOrder {
				data.Items[i] = groups[title]
			}
		} else if args.GroupBy == shared.GroupByPerformer {
			groups := make(map[string]*Group)
			var groupOrder []string
			uniquePerfs := make(map[string]bool)

			for _, m := range list {
				performers := []string{"Unknown Performer"}
				if m.Metadata != nil {
					if s, ok := m.Metadata["Actors"].(string); ok && s != "" && s != "N/A" {
						performers = strings.Split(s, ", ")
					}
				}
				for _, perf := range performers {
					if _, ok := groups[perf]; !ok {
						groups[perf] = &Group{
							Title: perf,
						}
						groupOrder = append(groupOrder, perf)
						uniquePerfs[perf] = true
					}
					groups[perf].Items = append(groups[perf].Items, m)
				}
			}
			if s.tpdb != nil {
				g, gctx := errgroup.WithContext(c.Request.Context())
				gctx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()
				var mu sync.Mutex
				for perf := range uniquePerfs {
					perf := perf
					g.Go(func() error {
						poster, _ := s.tpdb.FetchPerformerPoster(gctx, perf)
						mu.Lock()
						groups[perf].PosterURL = poster
						mu.Unlock()
						return nil
					})
				}
				_ = g.Wait()
			}
			data.Items = make([]any, len(groupOrder))
			for i, title := range groupOrder {
				data.Items[i] = groups[title]
			}
		} else {
			data.Items = make([]any, len(list))
			for i, v := range list {
				data.Items[i] = v
			}
		}
	}

	tc, mc, sc, ac, err := models.GetLibraryCounts(c.Request.Context(), db, user.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	data.TorrentCount = tc
	data.MovieCount = mc
	data.SeriesCount = sc
	data.AdultCount = ac


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
	h.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}
