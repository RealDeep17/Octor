package library

import (
	"context"
	"fmt"
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
	default:
		args.Watched = shared.WatchedFilterAll
	}
	args.Query = strings.TrimSpace(c.Query("q"))
	args.IsAdmin = s.admin.HasAdmin(c)
	args.GroupBy = shared.GroupBy(c.Query("group"))
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
		tree := buildTorrentTree(list)
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
	if c.Query("from") != "" {
		h.WithLayout("")
	}
	h.HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

type TorrentNode struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"` // "folder" or "item"
	Item     *models.Library `json:"item,omitempty"`
	Children []*TorrentNode  `json:"children,omitempty"`
}

func getTorrentCategory(lib *models.Library) (category string, groupKey string, groupName string) {
	isAdult := false
	isJAV := false

	if lib.MediaInfo != nil {
		for _, m := range lib.MediaInfo.Movies {
			hasAdultID := false
			if m.MovieMetadata != nil {
				vid := m.MovieMetadata.VideoID
				if strings.HasPrefix(vid, "tpdb:") || strings.HasPrefix(vid, "tpdb_jav:") || strings.HasPrefix(vid, "stash:") {
					hasAdultID = true
					if strings.HasPrefix(vid, "tpdb_jav:") {
						isJAV = true
					}
				}
			}
			hasAdultPath := false
			if m.Path != nil {
				lowerPath := strings.ToLower(*m.Path)
				if strings.Contains(lowerPath, "jav") {
					isJAV = true
					isAdult = true
				}
				keywords := []string{"porn", "adult", "xxx", "brazzers", "bangbros", "hentai", "slut", "pornstar", "nude"}
				for _, kw := range keywords {
					if strings.Contains(lowerPath, kw) {
						hasAdultPath = true
						break
					}
				}
			}
			if hasAdultID || hasAdultPath {
				isAdult = true
			}
		}
	}

	if isAdult {
		if isJAV {
			return "adult_jav", "", ""
		}
		return "adult_porn", "", ""
	}

	if lib.MediaInfo != nil && len(lib.MediaInfo.SeriesList) > 0 {
		series := lib.MediaInfo.SeriesList[0]
		groupKey := "series_unknown"
		groupName := "Unknown Series"
		if series.SeriesMetadata != nil {
			if series.SeriesMetadata.VideoID != "" {
				groupKey = series.SeriesMetadata.VideoID
			} else if series.SeriesMetadata.Title != "" {
				groupKey = "title:" + series.SeriesMetadata.Title
			}
			if series.SeriesMetadata.Title != "" {
				groupName = series.SeriesMetadata.Title
				if series.SeriesMetadata.Year != nil && *series.SeriesMetadata.Year > 0 {
					groupName = fmt.Sprintf("%s (%d)", series.SeriesMetadata.Title, *series.SeriesMetadata.Year)
				}
			}
		} else if series.Title != "" {
			groupKey = "title:" + series.Title
			groupName = series.Title
		} else {
			groupKey = "title:" + lib.Name
			groupName = lib.Name
		}
		return "series", groupKey, groupName
	}

	if lib.MediaInfo != nil && len(lib.MediaInfo.Movies) > 0 {
		movie := lib.MediaInfo.Movies[0]
		groupKey := "movie_unknown"
		groupName := "Unknown Movie"
		if movie.MovieMetadata != nil {
			if movie.MovieMetadata.VideoID != "" {
				groupKey = movie.MovieMetadata.VideoID
			} else if movie.MovieMetadata.Title != "" {
				groupKey = "title:" + movie.MovieMetadata.Title
			}
			if movie.MovieMetadata.Title != "" {
				groupName = movie.MovieMetadata.Title
				if movie.MovieMetadata.Year != nil && *movie.MovieMetadata.Year > 0 {
					groupName = fmt.Sprintf("%s (%d)", movie.MovieMetadata.Title, *movie.MovieMetadata.Year)
				}
			}
		} else if movie.Title != "" {
			groupKey = "title:" + movie.Title
			groupName = movie.Title
		} else {
			groupKey = "title:" + lib.Name
			groupName = lib.Name
		}
		return "movie", groupKey, groupName
	}

	return "other", "", ""
}

func buildTorrentTree(list []*models.Library) []*TorrentNode {
	movieGroups := make(map[string][]*models.Library)
	movieGroupNames := make(map[string]string)
	var movieGroupOrder []string

	seriesGroups := make(map[string][]*models.Library)
	seriesGroupNames := make(map[string]string)
	var seriesGroupOrder []string

	var adultJAVList []*models.Library
	var adultPornList []*models.Library
	var otherList []*models.Library

	for _, lib := range list {
		cat, key, groupName := getTorrentCategory(lib)
		switch cat {
		case "movie":
			if _, ok := movieGroups[key]; !ok {
				movieGroupOrder = append(movieGroupOrder, key)
				movieGroupNames[key] = groupName
			}
			movieGroups[key] = append(movieGroups[key], lib)
		case "series":
			if _, ok := seriesGroups[key]; !ok {
				seriesGroupOrder = append(seriesGroupOrder, key)
				seriesGroupNames[key] = groupName
			}
			seriesGroups[key] = append(seriesGroups[key], lib)
		case "adult_jav":
			adultJAVList = append(adultJAVList, lib)
		case "adult_porn":
			adultPornList = append(adultPornList, lib)
		default:
			otherList = append(otherList, lib)
		}
	}

	var movieChildren []*TorrentNode
	for _, key := range movieGroupOrder {
		libs := movieGroups[key]
		if len(libs) == 1 {
			movieChildren = append(movieChildren, &TorrentNode{
				Name: movieGroupNames[key],
				Type: "item",
				Item: libs[0],
			})
		} else {
			var subChildren []*TorrentNode
			for _, lib := range libs {
				subChildren = append(subChildren, &TorrentNode{
					Name: lib.Name,
					Type: "item",
					Item: lib,
				})
			}
			movieChildren = append(movieChildren, &TorrentNode{
				Name:     movieGroupNames[key],
				Type:     "folder",
				Children: subChildren,
			})
		}
	}

	var seriesChildren []*TorrentNode
	for _, key := range seriesGroupOrder {
		libs := seriesGroups[key]
		if len(libs) == 1 {
			seriesChildren = append(seriesChildren, &TorrentNode{
				Name: seriesGroupNames[key],
				Type: "item",
				Item: libs[0],
			})
		} else {
			var subChildren []*TorrentNode
			for _, lib := range libs {
				subChildren = append(subChildren, &TorrentNode{
					Name: lib.Name,
					Type: "item",
					Item: lib,
				})
			}
			seriesChildren = append(seriesChildren, &TorrentNode{
				Name:     seriesGroupNames[key],
				Type:     "folder",
				Children: subChildren,
			})
		}
	}

	var adultChildren []*TorrentNode
	if len(adultJAVList) > 0 {
		var javChildren []*TorrentNode
		for _, lib := range adultJAVList {
			javChildren = append(javChildren, &TorrentNode{
				Name: lib.Name,
				Type: "item",
				Item: lib,
			})
		}
		adultChildren = append(adultChildren, &TorrentNode{
			Name:     "JAV",
			Type:     "folder",
			Children: javChildren,
		})
	}
	if len(adultPornList) > 0 {
		var pornChildren []*TorrentNode
		for _, lib := range adultPornList {
			pornChildren = append(pornChildren, &TorrentNode{
				Name: lib.Name,
				Type: "item",
				Item: lib,
			})
		}
		adultChildren = append(adultChildren, &TorrentNode{
			Name:     "Porn",
			Type:     "folder",
			Children: pornChildren,
		})
	}

	var otherChildren []*TorrentNode
	for _, lib := range otherList {
		otherChildren = append(otherChildren, &TorrentNode{
			Name: lib.Name,
			Type: "item",
			Item: lib,
		})
	}

	var rootNodes []*TorrentNode
	if len(movieChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Movies",
			Type:     "folder",
			Children: movieChildren,
		})
	}
	if len(seriesChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "TV Series",
			Type:     "folder",
			Children: seriesChildren,
		})
	}
	if len(adultChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Adult",
			Type:     "folder",
			Children: adultChildren,
		})
	}
	if len(otherChildren) > 0 {
		rootNodes = append(rootNodes, &TorrentNode{
			Name:     "Others",
			Type:     "folder",
			Children: otherChildren,
		})
	}

	return rootNodes
}
