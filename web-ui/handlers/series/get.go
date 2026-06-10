package series

import (
	"crypto/sha1"
	"encoding/hex"
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
	ItemID      string
	IsActive    bool
}

// EpisodeGroup groups all torrent copies of a single S×E.
type EpisodeGroup struct {
	Season     int
	EpisodeNum int
	ShortLabel string
	LongLabel  string
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
	ActiveSeason        int
	ActiveEpisode       int
}

func (h *Handler) get(c *gin.Context) {
	videoID := c.Param("video_id")
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		c.Redirect(http.StatusFound, "/auth/sign-in")
		return
	}

	targetUserID := user.ID
	isOmni := false
	if (auth.IsAdmin(c) || h.admin.IsAdminUser(user)) && c.Query("user") != "" {
		if c.Query("user") == "all" || c.Query("user") == "omni" {
			targetUserID = uuid.Nil
			isOmni = true
		} else if parsedID, err := uuid.FromString(c.Query("user")); err == nil {
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
	statusUserID := targetUserID
	if isOmni {
		statusUserID = user.ID
	}
	status, err := models.GetSeriesStatus(c.Request.Context(), db, statusUserID, videoID)
	if err == nil && status != nil && status.PosterLayout != "" {
		layoutPref = status.PosterLayout
	}

	// Find the most recently watched episode from watch history
	var history []*models.WatchHistory
	if len(resourceIDs) > 0 {
		historyQuery := db.Model(&history)
		if !isOmni {
			historyQuery.Where("user_id = ?", targetUserID)
		} else {
			historyQuery.Where("user_id = ?", user.ID)
		}
		_ = historyQuery.
			Where("resource_id IN (?)", pg.In(resourceIDs)).
			Order("updated_at DESC").
			Limit(1).
			Select()
	}

	var activeResourceID, activePath string
	if len(history) > 0 {
		activeResourceID = history[0].ResourceID
		activePath = history[0].Path
	}

	data := buildPageData(videoID, seriesList, trMap, layoutPref, activeResourceID, activePath)
	h.tb.Build("series/index").HTML(http.StatusOK, web.NewContext(c).WithData(data))
}

func buildPageData(videoID string, seriesList []*models.Series, trMap map[string]*models.TorrentResource, layoutPref string, activeResourceID string, activePath string) *SeriesPageData {
	activeSeason := -1
	activeEpisode := -1
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

			// Sanity cap: parsers sometimes mis-read bare episode numbers
			// (e.g. "- 03 [BD].mkv") as a season value, producing fake seasons
			// like 61, 89, 91 (Ascendance of a Bookworm). Any season > 100 is
			// clearly an artifact — treat it as unclassified (sea=0) so the
			// episode falls through to path-keyword detection or Specials.
			if sea > 100 {
				sea = 0
			}

			// Virtual-season reclassification: only skip when this episode has
			// a definitive S×E pair (both season AND episode > 0). If either is
			// missing, the file may be an OVA, Movie, Extra, Featurette, or
			// Opening/Ending identifiable by its path keywords.
			//   sea=3,ep=15  → guard false → stays Season 3 (Community S03E15)
			//   sea=2,ep=0   → guard true  → finds "endings" → 1004 (Code Geass NCOP)
			//   sea=0,ep=1   → guard true  → finds "ova"     → 1001 (Code Geass OVA04)
			//   sea=9,ep=0   → guard true  → finds "featurette" → 1003 (Peaky Blinders)
			if !(sea > 0 && epNum > 0) {
				sea = detectVirtualSeason(filePath, sea)
			}

			if sea < 1000 {
				if _, ok := occupied[sea]; !ok {
					occupied[sea] = map[int]bool{}
				}
				if epNum != 0 {
					occupied[sea][epNum] = true
				}
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

	// Separate normal vs virtual season items
	var normalTemps []epTemp
	virtualTempsMap := map[int][]epTemp{}
	for _, t := range temps {
		if t.sea >= 1000 {
			virtualTempsMap[t.sea] = append(virtualTempsMap[t.sea], t)
		} else {
			normalTemps = append(normalTemps, t)
		}
	}

	// Assign sequential episode numbers ONLY to episodes that have no episode
	// number from the DB (epNum==0). Episodes that already have a parsed
	// episode number keep it even if another file from the same torrent shares
	// the same S×E — they will be grouped together in the same EpisodeGroup
	// as multiple torrent options (e.g. regular episode + deleted scene both
	// labelled S02E01 from the same pack appear as two choices for EP2).
	type resEpKey struct {
		resourceID string
		sea        int
		epNum      int
	}
	assignedNulls := map[resEpKey]bool{}
	for i, t := range normalTemps {
		sea := t.sea
		epNum := t.epNum
		if epNum == 0 {
			// No episode number — find the next free slot for this torrent+season.
			nextEp := 1
			for occupied[sea][nextEp] || assignedNulls[resEpKey{t.resourceID, sea, nextEp}] {
				nextEp++
			}
			epNum = nextEp
			assignedNulls[resEpKey{t.resourceID, sea, epNum}] = true
		}
		normalTemps[i].epNum = epNum
	}

	// For virtual seasons (sea >= 1000), sort them alphabetically by filePath
	// and assign sequential episode numbers from 1 to N.
	for _, items := range virtualTempsMap {
		sort.Slice(items, func(i, j int) bool {
			return strings.Compare(strings.ToLower(items[i].filePath), strings.ToLower(items[j].filePath)) < 0
		})
		for idx := range items {
			items[idx].epNum = idx + 1
		}
	}

	// Reassemble temps
	var newTemps []epTemp
	newTemps = append(newTemps, normalTemps...)
	for _, sea := range []int{1001, 1002, 1003, 1004} {
		newTemps = append(newTemps, virtualTempsMap[sea]...)
	}
	for sea, items := range virtualTempsMap {
		if sea != 1001 && sea != 1002 && sea != 1003 && sea != 1004 {
			newTemps = append(newTemps, items...)
		}
	}
	temps = newTemps

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
				stillURL = fmt.Sprintf("/lib/episode/still/%s/%d/%d/300.jpg", e.EpisodeMetadata.VideoID, e.EpisodeMetadata.Season, e.EpisodeMetadata.Episode)
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
		si, sj := epOrder[i].season, epOrder[j].season
		// Season 0 (Specials/S00) should appear after all real seasons but
		// before virtual seasons (1001+). Map it to 999 for sorting purposes.
		if si == 0 {
			si = 999
		}
		if sj == 0 {
			sj = 999
		}
		if si != sj {
			return si < sj
		}
		return epOrder[i].episode < epOrder[j].episode
	})

	seasonMap := map[int]*SeasonGroup{}
	var seasonOrder []int

	for _, key := range epOrder {
		entries := epMap[key]

		// Sort entries within the group by file size descending so the
		// largest file (the real episode) is always first. Deleted scenes,
		// extras, and other supplemental files are typically much smaller
		// and will appear below the primary episode in the torrent list.
		// The first entry also determines the episode title and thumbnail.
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].size > entries[j].size
		})

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
			itemID := e.resourceID
			if e.path != "" {
				rawPath := e.path
				if !strings.HasPrefix(rawPath, "/") {
					rawPath = "/" + rawPath
				}
				h := sha1.New()
				h.Write([]byte(rawPath))
				itemID = hex.EncodeToString(h.Sum(nil))
			}
			isActive := (e.resourceID == activeResourceID && e.path == activePath)
			if isActive {
				activeSeason = key.season
				activeEpisode = key.episode
			}
			torrents = append(torrents, EpisodeTorrent{
				ResourceID:  e.resourceID,
				FolderName:  folder,
				FileName:    fname,
				PlayURL:     playURL,
				TorrentName: trName,
				Size:        size,
				ItemID:      itemID,
				IsActive:    isActive,
			})
		}

		// Derive label using smart filename detection for virtual seasons,
		// so "Extra 1" becomes "Blooper 1" or "Making Of 1" etc.
		var firstPath string
		if len(entries) > 0 {
			_, firstPath = splitPath(entries[0].path)
		}
		shortLabel, longLabel := virtualEpLabel(key.season, key.episode, firstPath)

		eg := EpisodeGroup{
			Season:     key.season,
			EpisodeNum: key.episode,
			ShortLabel: shortLabel,
			LongLabel:  longLabel,
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
		ActiveSeason:        activeSeason,
		ActiveEpisode:       activeEpisode,
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

// detectVirtualSeason inspects a file path for virtual-season keywords and
// returns the appropriate virtual season number (1001–1004), or defaultSeason
// if no keyword matches. Pass the episode's current sea value as defaultSeason
// so that e.g. a Season-2 NCOP with no keyword returns 2 (stays in Season 2)
// rather than 0 (Specials).
//
// Called ONLY when the episode is missing at least one of season or episode
// from the DB (i.e. !(sea>0 && epNum>0)). Real S×E episodes are never passed
// here and therefore never reclassified.
func detectVirtualSeason(filePath string, defaultSeason int) int {
	if filePath == "" {
		return defaultSeason
	}
	lower := strings.ToLower(filePath)
	parts := strings.Split(lower, "/")

	// Skip the torrent root directory (first non-empty segment) to prevent
	// matching keywords embedded in the torrent pack name itself
	// (e.g. "[Judas] Code Geass (Seasons 1-2 + Movies + OVAs)" contains
	// "ova" and "movie" but those refer to the pack, not to individual files).
	startIndex := 0
	if len(parts) > 0 && parts[0] == "" {
		startIndex = 2 // leading-slash path: skip empty token + root dir
	} else {
		startIndex = 1 // no leading slash: skip root dir
	}

	match := func(part string) int {
		if strings.Contains(part, "ova") {
			return 1001
		}
		if strings.Contains(part, "movie") {
			return 1002
		}
		if strings.Contains(part, "extra") || strings.Contains(part, "featurette") {
			return 1003
		}
		if strings.Contains(part, "opening") || strings.Contains(part, "ending") ||
			strings.Contains(part, "ncop") || strings.Contains(part, "nced") {
			return 1004
		}
		return 0
	}

	// Check intermediate directory components (between root dir and filename)
	for i := startIndex; i < len(parts)-1; i++ {
		if v := match(parts[i]); v != 0 {
			return v
		}
	}
	// Fall back to filename
	if len(parts) > 0 {
		if v := match(parts[len(parts)-1]); v != 0 {
			return v
		}
	}
	return defaultSeason
}

// virtualEpLabel returns the short and long display labels for a virtual-season
// episode. Instead of always showing "Extra 1", "Extra 2" etc., it inspects
// the base filename to pick a more descriptive prefix:
//
//	"blooper"          → "Blooper N"
//	"making of"        → "Making Of N"
//	"behind the scene" → "BTS N"
//	"featurette"       → "Featurette N"
//	"interview"        → "Interview N"
//	"promo"            → "Promo N"
//	"ncop"             → "NCOP N"
//	"nced"             → "NCED N"
//	"special"          → "Special N"  (OVA group)
//	otherwise          → season-default (OVA N / Movie N / Extra N / Clip N / EP N)
func virtualEpLabel(season, epNum int, filename string) (short, long string) {
	f := strings.ToLower(filename)

	if season == 1003 {
		// Extras: try to pick a descriptive sub-type from the filename
		switch {
		case strings.Contains(f, "blooper"):
			short = fmt.Sprintf("Blooper %d", epNum)
			long = fmt.Sprintf("Blooper %d", epNum)
		case strings.Contains(f, "making of") || strings.Contains(f, "making-of"):
			short = fmt.Sprintf("Making Of %d", epNum)
			long = fmt.Sprintf("Making Of %d", epNum)
		case strings.Contains(f, "behind the scene") || strings.Contains(f, "behind-the-scene"):
			short = fmt.Sprintf("BTS %d", epNum)
			long = fmt.Sprintf("Behind the Scenes %d", epNum)
		case strings.Contains(f, "featurette"):
			short = fmt.Sprintf("Feat %d", epNum)
			long = fmt.Sprintf("Featurette %d", epNum)
		case strings.Contains(f, "interview"):
			short = fmt.Sprintf("Intv %d", epNum)
			long = fmt.Sprintf("Interview %d", epNum)
		case strings.Contains(f, "promo"):
			short = fmt.Sprintf("Promo %d", epNum)
			long = fmt.Sprintf("Promo %d", epNum)
		default:
			short = fmt.Sprintf("Extra %d", epNum)
			long = fmt.Sprintf("Extra %d", epNum)
		}
		return
	}

	if season == 1004 {
		switch {
		case strings.Contains(f, "ncop"):
			short = fmt.Sprintf("NCOP %d", epNum)
			long = fmt.Sprintf("NCOP %d", epNum)
		case strings.Contains(f, "nced"):
			short = fmt.Sprintf("NCED %d", epNum)
			long = fmt.Sprintf("NCED %d", epNum)
		case strings.Contains(f, "opening"):
			short = fmt.Sprintf("OP %d", epNum)
			long = fmt.Sprintf("Opening %d", epNum)
		case strings.Contains(f, "ending"):
			short = fmt.Sprintf("ED %d", epNum)
			long = fmt.Sprintf("Ending %d", epNum)
		default:
			short = fmt.Sprintf("Clip %d", epNum)
			long = fmt.Sprintf("Clip %d", epNum)
		}
		return
	}

	switch season {
	case 1001:
		if strings.Contains(f, "special") {
			short = fmt.Sprintf("Special %d", epNum)
			long = fmt.Sprintf("Special %d", epNum)
		} else {
			short = fmt.Sprintf("OVA %d", epNum)
			long = fmt.Sprintf("OVA %d", epNum)
		}
	case 1002:
		short = fmt.Sprintf("Movie %d", epNum)
		long = fmt.Sprintf("Movie %d", epNum)
	default:
		short = fmt.Sprintf("EP%d", epNum)
		long = fmt.Sprintf("Episode %d", epNum)
	}
	return
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
