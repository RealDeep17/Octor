package series

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// EpisodeTorrent is one torrent file that carries an episode.
type EpisodeTorrent struct {
	ResourceID  string
	FolderName  string // parent dir inside the torrent, empty for root files
	FileName    string // just the base filename
	PlayURL     string // /<hash>?file=<encoded_path>
	TorrentName string // TorrentResource name
	Size        int64  // Torrent size in bytes
}

// EpisodeGroup groups all torrent copies of a single S×E.
type EpisodeGroup struct {
	Season     int
	EpisodeNum int
	Title      string // from episode_metadata, may be empty
	StillURL   string // thumbnail, may be empty
	Torrents   []EpisodeTorrent
}

// SeasonGroup holds all episodes within one season.
type SeasonGroup struct {
	Season     int
	SeasonName string
	Episodes   []EpisodeGroup
}

// SeriesPageData is the view-model for templates/views/series/index.html.
type SeriesPageData struct {
	VideoID             string
	Title               string
	Year                *int16
	Rating              float64
	PosterURL           string
	PosterHorizontalURL string
	Plot                string
	Seasons             []SeasonGroup
	TotalEpisodes       int
	TotalTorrents       int
	UserPosterLayout    string
}

func (h *Handler) get(c *gin.Context) {
	videoID := c.Param("video_id")
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		c.Redirect(http.StatusFound, "/auth/sign-in")
		return
	}

	targetUserID := user.ID
	if (auth.IsAdmin(c) || h.admin.IsAdminUser(user)) && c.Query("user") != "" {
		if parsedID, err := uuid.FromString(c.Query("user")); err == nil {
			targetUserID = parsedID
		}
	}

	db := h.pg.Get()

	seriesList, err := models.GetSeriesByVideoID(c.Request.Context(), db, targetUserID, videoID)
	if err != nil || len(seriesList) == 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Fetch TorrentResource names for torrent mapping
	resourceIDs := []string{}
	for _, s := range seriesList {
		resourceIDs = append(resourceIDs, s.ResourceID)
	}

	var trs []*models.TorrentResource
	trMap := map[string]*models.TorrentResource{}
	if len(resourceIDs) > 0 {
		err = db.Model(&trs).Where("resource_id IN (?)", pg.In(resourceIDs)).Select()
		if err == nil {
			for _, tr := range trs {
				trMap[tr.ResourceID] = tr
			}
		}
	}

	// Fetch user's layout preference for poster aspect ratio
	layoutPref := ""
	status, err := models.GetSeriesStatus(c.Request.Context(), db, targetUserID, videoID)
	if err == nil && status != nil && status.PosterLayout != "" {
		layoutPref = status.PosterLayout
	}

	data := buildPageData(videoID, seriesList, trMap, layoutPref)
	h.tb.Build("series/index").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

func buildPageData(videoID string, seriesList []*models.Series, trMap map[string]*models.TorrentResource, layoutPref string) *SeriesPageData {
	// Extract show-level metadata from first series with it
	var showTitle string
	var showYear *int16
	var showRating float64
	var posterURL, posterHorizontalURL, plotStr string
	for _, s := range seriesList {
		if s.SeriesMetadata != nil && s.SeriesMetadata.VideoMetadata != nil {
			md := s.SeriesMetadata.VideoMetadata
			if showTitle == "" {
				showTitle = md.Title
			}
			showYear = md.Year
			if md.Rating != nil {
				showRating = *md.Rating
			}
			plotStr = md.Plot
			if md.VideoID != "" {
				posterURL = fmt.Sprintf("/lib/series/poster/%s/240.jpg", md.VideoID)
				
				// Replicate Helper.HasEnrichedPosterHorizontal logic
				hasHorizontal := md.PosterHorizontalURL != "" ||
					strings.Contains(md.PosterURL, "theporndb.net") ||
					strings.Contains(md.PosterURL, "stashdb.org") ||
					strings.HasPrefix(md.VideoID, "tpdb:") ||
					strings.HasPrefix(md.VideoID, "tpdb=") ||
					strings.HasPrefix(md.VideoID, "tpdb_jav:") ||
					strings.HasPrefix(md.VideoID, "tpdb_jav=") ||
					strings.HasPrefix(md.VideoID, "stash:") ||
					strings.HasPrefix(md.VideoID, "stash=")
				
				if hasHorizontal {
					posterHorizontalURL = fmt.Sprintf("/lib/series/poster-h/%s/480.jpg", md.VideoID)
				}
			}
			break
		}
		if showTitle == "" && s.Title != "" {
			showTitle = s.Title
		}
	}
	if showTitle == "" {
		showTitle = videoID
	}

	// Resolve default layoutPref for adult content
	if layoutPref == "" {
		layoutPref = "vertical"
		for _, s := range seriesList {
			if s.SeriesMetadata != nil && s.SeriesMetadata.VideoMetadata != nil {
				md := s.SeriesMetadata.VideoMetadata
				if strings.Contains(md.PosterURL, "theporndb.net") ||
					strings.Contains(md.PosterURL, "stashdb.org") ||
					strings.HasPrefix(md.VideoID, "tpdb:") ||
					strings.HasPrefix(md.VideoID, "tpdb=") ||
					strings.HasPrefix(md.VideoID, "tpdb_jav:") ||
					strings.HasPrefix(md.VideoID, "tpdb_jav=") ||
					strings.HasPrefix(md.VideoID, "stash:") ||
					strings.HasPrefix(md.VideoID, "stash=") {
					layoutPref = "horizontal"
				}
				break
			}
		}
	}

	type epKey struct{ season, episode int }
	type rawEntry struct {
		resourceID string
		path       string
		epTitle    string
		stillURL   string
		size       int64
	}

	epMap := map[epKey][]rawEntry{}
	epOrder := []epKey{}
	seenKey := map[epKey]bool{}
	torrentIDs := map[string]bool{}

	type epTemp struct {
		episode    *models.Episode
		sea        int
		epNum      int
		filePath   string
		epSize     int64
		resourceID string
	}
	var temps []epTemp
	occupied := map[int]map[int]bool{}

	for _, s := range seriesList {
		torrentIDs[s.ResourceID] = true
		for _, e := range s.Episodes {
			var sea, epNum int
			if e.Season != nil {
				sea = int(*e.Season)
			}
			if e.Episode != nil {
				epNum = int(*e.Episode)
			}
			filePath := ""
			if e.Path != nil {
				filePath = *e.Path
			}

			// Map to virtual season if it's Season 0
			sea = detectVirtualSeason(filePath, sea)

			if _, ok := occupied[sea]; !ok {
				occupied[sea] = map[int]bool{}
			}
			if epNum != 0 {
				occupied[sea][epNum] = true
			}

			var epSize int64 = 0
			if e.Metadata != nil {
				if szVal, ok := e.Metadata["size"]; ok {
					switch v := szVal.(type) {
					case float64:
						epSize = int64(v)
					case int64:
						epSize = v
					case int:
						epSize = int64(v)
					case float32:
						epSize = int64(v)
					}
				}
			}

			temps = append(temps, epTemp{
				episode:    e,
				sea:        sea,
				epNum:      epNum,
				filePath:   filePath,
				epSize:     epSize,
				resourceID: e.ResourceID,
			})
		}
	}

	// Assign unique episode numbers to avoid collisions
	assignedKeys := map[epKey]bool{}
	for i, t := range temps {
		sea := t.sea
		epNum := t.epNum
		key := epKey{sea, epNum}
		if epNum == 0 || assignedKeys[key] {
			nextEp := 1
			for occupied[sea][nextEp] || assignedKeys[epKey{sea, nextEp}] {
				nextEp++
			}
			epNum = nextEp
			key = epKey{sea, epNum}
		}
		assignedKeys[key] = true
		temps[i].epNum = epNum
	}

	// Populate epMap and epOrder using the resolved keys
	for _, t := range temps {
		sea := t.sea
		epNum := t.epNum
		key := epKey{sea, epNum}

		epTitle := ""
		stillURL := ""
		e := t.episode
		if e.EpisodeMetadata != nil {
			if e.EpisodeMetadata.Title != nil {
				epTitle = *e.EpisodeMetadata.Title
			}
			if e.EpisodeMetadata.StillURL != nil && *e.EpisodeMetadata.StillURL != "" {
				stillURL = fmt.Sprintf("/lib/episode/still/%s/%d/%d/300.jpg", e.EpisodeMetadata.VideoID, sea, epNum)
			}
		}
		if epTitle == "" && e.Title != nil {
			epTitle = *e.Title
		}
		if epTitle == "" && t.filePath != "" {
			_, fname := splitPath(t.filePath)
			epTitle = cleanFilename(fname)
		}

		epMap[key] = append(epMap[key], rawEntry{
			resourceID: t.resourceID,
			path:       t.filePath,
			epTitle:    epTitle,
			stillURL:   stillURL,
			size:       t.epSize,
		})
		if !seenKey[key] {
			seenKey[key] = true
			epOrder = append(epOrder, key)
		}
	}

	sort.Slice(epOrder, func(i, j int) bool {
		if epOrder[i].season != epOrder[j].season {
			return epOrder[i].season < epOrder[j].season
		}
		return epOrder[i].episode < epOrder[j].episode
	})

	seasonMap := map[int]*SeasonGroup{}
	var seasonOrder []int

	for _, key := range epOrder {
		entries := epMap[key]

		epTitle := ""
		stillURL := ""
		if len(entries) > 0 {
			epTitle = entries[0].epTitle
			stillURL = entries[0].stillURL
		}

		torrents := make([]EpisodeTorrent, 0, len(entries))
		for _, e := range entries {
			folder, fname := splitPath(e.path)
			playURL := fmt.Sprintf("/%s#action=stream", e.resourceID)
			if e.path != "" {
				playURL = fmt.Sprintf("/%s?file=%s#action=stream", e.resourceID, url.QueryEscape(e.path))
			}
			trName := ""
			var size int64 = 0
			if tr, ok := trMap[e.resourceID]; ok && tr != nil {
				trName = tr.Name
				size = tr.SizeBytes
			}
			if e.size > 0 {
				size = e.size
			}
			torrents = append(torrents, EpisodeTorrent{
				ResourceID:  e.resourceID,
				FolderName:  folder,
				FileName:    fname,
				PlayURL:     playURL,
				TorrentName: trName,
				Size:        size,
			})
		}

		eg := EpisodeGroup{
			Season:     key.season,
			EpisodeNum: key.episode,
			Title:      epTitle,
			StillURL:   stillURL,
			Torrents:   torrents,
		}

		if _, ok := seasonMap[key.season]; !ok {
			seasonMap[key.season] = &SeasonGroup{Season: key.season}
			seasonOrder = append(seasonOrder, key.season)
		}
		seasonMap[key.season].Episodes = append(seasonMap[key.season].Episodes, eg)
	}

	seasons := make([]SeasonGroup, 0, len(seasonOrder))
	totalEpisodes := 0
	for _, s := range seasonOrder {
		sg := seasonMap[s]
		sg.SeasonName = getSeasonName(sg.Season)
		totalEpisodes += len(sg.Episodes)
		seasons = append(seasons, *sg)
	}

	return &SeriesPageData{
		VideoID:             videoID,
		Title:               showTitle,
		Year:                showYear,
		Rating:              showRating,
		PosterURL:           posterURL,
		PosterHorizontalURL: posterHorizontalURL,
		Plot:                plotStr,
		Seasons:             seasons,
		TotalEpisodes:       totalEpisodes,
		TotalTorrents:       len(torrentIDs),
		UserPosterLayout:    layoutPref,
	}
}

// splitPath splits a torrent file path like "/Folder/file.mkv" into
// folder="Folder" and file="file.mkv". Root-level files return folder="".
func splitPath(p string) (folder, file string) {
	p = strings.TrimPrefix(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[:idx], p[idx+1:]
	}
	return "", p
}

func detectVirtualSeason(filePath string, defaultSeason int) int {
	if defaultSeason != 0 || filePath == "" {
		return defaultSeason
	}
	lower := strings.ToLower(filePath)
	parts := strings.Split(lower, "/")

	// Skip the root torrent directory prefix (e.g. /[Judas] Code Geass ... (Movies + OVAs))
	// to prevent matching keyword tokens in the torrent name itself.
	startIndex := 0
	if len(parts) > 0 && parts[0] == "" {
		startIndex = 2 // Skip the leading empty string and the torrent root directory
	} else {
		startIndex = 1 // Skip the torrent root directory
	}

	for i := startIndex; i < len(parts)-1; i++ {
		part := parts[i]
		if strings.Contains(part, "ova") {
			return 1001
		}
		if strings.Contains(part, "movie") {
			return 1002
		}
		if strings.Contains(part, "extra") {
			return 1003
		}
		if strings.Contains(part, "opening") || strings.Contains(part, "ending") || strings.Contains(part, "ncop") || strings.Contains(part, "nced") {
			return 1004
		}
	}
	filename := parts[len(parts)-1]
	if strings.Contains(filename, "ova") {
		return 1001
	}
	if strings.Contains(filename, "movie") {
		return 1002
	}
	if strings.Contains(filename, "extra") {
		return 1003
	}
	if strings.Contains(filename, "opening") || strings.Contains(filename, "ending") || strings.Contains(filename, "ncop") || strings.Contains(filename, "nced") {
		return 1004
	}
	return defaultSeason
}

func getSeasonName(season int) string {
	switch season {
	case 0:
		return "Specials"
	case 1001:
		return "OVAs"
	case 1002:
		return "Movies"
	case 1003:
		return "Extras"
	case 1004:
		return "Openings & Endings"
	default:
		return fmt.Sprintf("Season %d", season)
	}
}

func cleanFilename(filename string) string {
	if idx := strings.LastIndex(filename, "."); idx >= 0 {
		filename = filename[:idx]
	}
	for {
		start := strings.Index(filename, "[")
		if start < 0 {
			break
		}
		end := strings.Index(filename[start:], "]")
		if end < 0 {
			break
		}
		filename = filename[:start] + filename[start+end+1:]
	}
	for {
		start := strings.Index(filename, "(")
		if start < 0 {
			break
		}
		end := strings.Index(filename[start:], ")")
		if end < 0 {
			break
		}
		filename = filename[:start] + filename[start+end+1:]
	}
	filename = strings.Join(strings.Fields(filename), " ")
	return filename
}
