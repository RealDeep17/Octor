package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/adultposter"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/javguru"
	"github.com/webtor-io/web-ui/services/prowlarr"
	"github.com/webtor-io/web-ui/services/stashdb"
	"github.com/webtor-io/web-ui/services/tpdb"
	"golang.org/x/sync/singleflight"
)

var posterSingleFlight singleflight.Group

func RegisterAdultRoutes(
	r *gin.Engine,
	tpdbSvc *tpdb.Service,
	adminSvc *admin.Admin,
	redisClient *cs.RedisClient,
	javGuruSvc *javguru.Service,
	stashdbSvc *stashdb.Service,
	prowlarrSvc *prowlarr.Service,
	posterSvc *adultposter.Service,
) {
	if tpdbSvc == nil {
		log.Warn("TPDB service is nil, adult discovery endpoints will be disabled")
		return
	}

	adminGroup := r.Group("/discover/adult")
	adminGroup.Use(func(c *gin.Context) {
		if !auth.IsAdmin(c) && !adminSvc.HasAdmin(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: Admin access required"})
			c.Abort()
			return
		}
		c.Next()
	})

	adminGroup.GET("/catalog/:type/*path", handleCatalog(tpdbSvc, stashdbSvc, posterSvc, javGuruSvc))
	adminGroup.GET("/meta/adult/*path", handleMeta(tpdbSvc, stashdbSvc, redisClient, javGuruSvc, posterSvc))
	adminGroup.GET("/poster", handlePoster(posterSvc))
	adminGroup.HEAD("/poster", handlePoster(posterSvc))
	adminGroup.GET("/comet-proxy/stream/movie/*id", handleCometProxy(tpdbSvc, stashdbSvc, prowlarrSvc, redisClient))

	// Start background ticker for pre-warming catalogs and pruning posters every 4 hours
	go runInternalPreWarmer(tpdbSvc, stashdbSvc, posterSvc, javGuruSvc)
}

func runInternalPreWarmer(tpdbSvc *tpdb.Service, stashdbSvc *stashdb.Service, posterSvc *adultposter.Service, javGuruSvc *javguru.Service) {
	time.Sleep(1 * time.Minute)

	warm := func() {
		log.Info("Starting internal adult discovery catalog pre-warming...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		catalogs := []struct {
			id    string
			isJav bool
		}{
			{id: "trending", isJav: false},
			{id: "new", isJav: false},
			{id: "jav-recent", isJav: true},
			{id: "jav-subbed", isJav: true},
			{id: "jav-trending", isJav: true},
		}

		for _, cat := range catalogs {
			var scenes []tpdb.TpdbScene
			var err error
			if cat.isJav {
				if javGuruSvc != nil {
					if cat.id == "jav-recent" {
						scenes, err = javGuruSvc.FetchRecent(ctx, 1)
					} else if cat.id == "jav-subbed" {
						scenes, err = javGuruSvc.FetchSubbed(ctx, 1)
					} else if cat.id == "jav-trending" {
						scenes, err = javGuruSvc.FetchTrending(ctx)
					}
				}
			} else {
				scenes, err = stashdbSvc.FetchStashDBScenes(ctx, cat.id, 1, "")
			}

			if err != nil {
				log.WithError(err).Warnf("Internal pre-warmer: failed to fetch scenes for catalog %s", cat.id)
				continue
			}
			posterSvc.PreWarmPosters(scenes, cat.isJav)
		}

		log.Info("Internal adult discovery catalog pre-warming complete. Pruning old posters...")
		if err := posterSvc.PruneAdultPosters(7); err != nil {
			log.WithError(err).Warn("Internal pre-warmer: failed to prune adult posters")
		}
	}

	warm()

	ticker := time.NewTicker(4 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		warm()
	}
}

func preprocessSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
		}
	}
	return true
}

func getJavStudioName(title, sceneID, defaultStudio string) string {
	javCodeRx := regexp.MustCompile(`(?i)\b([a-zA-Z0-9]{2,10})-\d{3,8}\b`)
	if m := javCodeRx.FindStringSubmatch(title); len(m) > 1 {
		return strings.ToUpper(m[1])
	}
	if m := javCodeRx.FindStringSubmatch(sceneID); len(m) > 1 {
		return strings.ToUpper(m[1])
	}
	return defaultStudio
}

func handleCatalog(tpdbSvc *tpdb.Service, stashdbSvc *stashdb.Service, posterSvc *adultposter.Service, javGuruSvc *javguru.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Param("path"), "/")
		path = strings.TrimSuffix(path, ".json")
		parts := strings.Split(path, "/")
		catalogID := parts[0]

		skipStr := ""
		if len(parts) > 1 {
			skipStr = parts[1]
		}
		if skipStr == "" {
			skipStr = c.Query("skip")
		}
		skip := 0
		if skipStr != "" {
			skipStr = strings.TrimPrefix(skipStr, "skip=")
			if s, err := strconv.Atoi(skipStr); err == nil {
				skip = s
			}
		}

		perPage := 60
		page := (skip / perPage) + 1

		searchQuery := ""
		for _, part := range parts {
			if strings.HasPrefix(part, "search=") {
				searchQuery = strings.TrimPrefix(part, "search=")
				if decoded, err := url.QueryUnescape(searchQuery); err == nil {
					searchQuery = decoded
				}
			}
		}
		if searchQuery == "" {
			searchQuery = c.Query("search")
		} else {
			if decoded, err := url.QueryUnescape(searchQuery); err == nil {
				searchQuery = decoded
			}
		}

		var scenes []tpdb.TpdbScene
		var err error

		if searchQuery != "" {
			processedSearch := preprocessSearchQuery(searchQuery)
			if strings.HasPrefix(catalogID, "jav-") || c.Param("type") == "jav" {
				if javGuruSvc != nil {
					scenes, err = javGuruSvc.Search(c.Request.Context(), processedSearch, page)
				} else {
					scenes, err = tpdbSvc.SearchJavScenes(c.Request.Context(), processedSearch)
				}
			} else {
				processedSearch := preprocessSearchQuery(searchQuery)
				stashScenes, errStash := stashdbSvc.FetchStashDBScenes(c.Request.Context(), catalogID, page, processedSearch)

				if errStash == nil && len(stashScenes) >= 10 {
					scenes = stashScenes
				} else {
					var tpdbScenes []tpdb.TpdbScene
					if page == 1 {
						tpdbScenes, _ = tpdbSvc.SearchScenes(c.Request.Context(), processedSearch)
					}

					gayRx := regexp.MustCompile(`(?i)\b(gay|gays|bi-?empire|boys?|males?|\bmen\b|trans|t-?girls?|midgets?)\b`)
					seen := make(map[string]bool)
					normalize := func(t string) string {
						t = strings.ToLower(t)
						var sb strings.Builder
						for _, r := range t {
							if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
								sb.WriteRune(r)
							}
						}
						return sb.String()
					}
					for _, sc := range stashScenes {
						norm := normalize(sc.Title)
						if norm != "" && !seen[norm] {
							seen[norm] = true
							scenes = append(scenes, sc)
						}
					}
					for _, sc := range tpdbScenes {
						studioName := ""
						if sc.Site != nil {
							studioName = sc.Site.Name
						}
						if gayRx.MatchString(studioName) {
							continue
						}
						norm := normalize(sc.Title)
						if norm != "" && !seen[norm] {
							seen[norm] = true
							scenes = append(scenes, sc)
						}
					}
					if errStash != nil && len(scenes) == 0 {
						err = errStash
					}
				}
			}
		} else {
			if catalogID == "jav-recent" {
				if javGuruSvc != nil {
					scenes, err = javGuruSvc.FetchRecent(c.Request.Context(), page)
				}
			} else if catalogID == "jav-subbed" {
				if javGuruSvc != nil {
					scenes, err = javGuruSvc.FetchSubbed(c.Request.Context(), page)
				}
			} else if catalogID == "jav-trending" {
				if javGuruSvc != nil {
					scenes, err = javGuruSvc.FetchTrending(c.Request.Context())
				}
			} else {
				scenes, err = stashdbSvc.FetchStashDBScenes(c.Request.Context(), catalogID, page, "")
			}
		}

		if err != nil {
			log.WithError(err).Errorf("failed to fetch scenes for catalog %s", catalogID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		isJav := strings.HasPrefix(catalogID, "jav-") || c.Param("type") == "jav"

		go posterSvc.PreWarmPosters(scenes, isJav)

		metas := make([]gin.H, 0, len(scenes))
		for _, scene := range scenes {
			originalPoster := posterSvc.CleanOriginalImageURL(scene.Poster)
			if originalPoster == "" {
				originalPoster = posterSvc.CleanOriginalImageURL(scene.PosterImage)
			}
			originalBackground := posterSvc.CleanOriginalImageURL(scene.Image)

			if originalPoster == "" && originalBackground != "" {
				originalPoster = originalBackground
			}
			if originalBackground == "" && originalPoster != "" {
				originalBackground = originalPoster
			}

			proxiedPoster := ""
			if originalPoster != "" {
				proxiedPoster = fmt.Sprintf("/discover/adult/poster?url=%s&shape=portrait&isJav=%t", url.QueryEscape(originalPoster), isJav)
			}

			proxiedBackground := ""
			if originalBackground != "" {
				proxiedBackground = fmt.Sprintf("/discover/adult/poster?url=%s&shape=landscape&isJav=%t", url.QueryEscape(originalBackground), isJav)
			}

			year := "N/A"
			if len(scene.Date) >= 4 {
				year = scene.Date[:4]
			}

			idPrefix := "tpdb"
			if isJav {
				idPrefix = "tpdb_jav"
			} else if isUUID(scene.ID) {
				idPrefix = "stash"
			}

			studioName := ""
			if scene.Site != nil {
				studioName = scene.Site.Name
			}
			if isJav {
				studioName = getJavStudioName(scene.Title, scene.ID, studioName)
			}

			itemType := "porn"
			if isJav {
				itemType = "jav"
			}

			metas = append(metas, gin.H{
				"id":               fmt.Sprintf("%s:%s", idPrefix, scene.ID),
				"type":             itemType,
				"name":             scene.Title,
				"poster":           proxiedPoster,
				"posterHorizontal": proxiedBackground,
				"background":       proxiedBackground,
				"description":      scene.Description,
				"year":             year,
				"releaseInfo":      scene.Date,
				"studio":           studioName,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"metas": metas,
		})
	}
}

func handleMeta(
	tpdbSvc *tpdb.Service,
	stashdbSvc *stashdb.Service,
	redisClient *cs.RedisClient,
	javGuruSvc *javguru.Service,
	posterSvc *adultposter.Service,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := strings.TrimPrefix(c.Param("path"), "/")
		id := strings.TrimSuffix(path, ".json")
		parts := strings.Split(id, ":")
		if len(parts) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid scene id format"})
			return
		}
		prefix := parts[0]
		sceneID := parts[1]

		var scene *tpdb.TpdbScene
		var err error

		if prefix == "tpdb_jav" {
			if strings.HasPrefix(sceneID, "fallback_") {
				code := strings.TrimPrefix(sceneID, "fallback_")
				redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)
				if redisClient != nil {
					val, _ := redisClient.Get().Get(c.Request.Context(), redisKey).Result()
					if val != "" {
						_ = json.Unmarshal([]byte(val), &scene)
					}
				}
			} else {
				scene, err = tpdbSvc.FetchJavByID(c.Request.Context(), sceneID)
			}
		} else if prefix == "stash" {
			scene, err = stashdbSvc.FetchStashDBSingleScene(c.Request.Context(), sceneID)
		} else {
			scene, err = tpdbSvc.FetchSceneByID(c.Request.Context(), sceneID)
		}

		if err != nil || scene == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "scene not found"})
			return
		}

		isJav := prefix == "tpdb_jav"
		if isJav {
			code := ""
			javCodeRx := regexp.MustCompile(`(?i)\b([a-zA-Z0-9]{2,10})-\d{3,8}\b`)
			if m := javCodeRx.FindString(scene.Title); m != "" {
				code = strings.ToUpper(m)
			} else if m := javCodeRx.FindString(scene.ID); m != "" {
				code = strings.ToUpper(m)
			}

			if code != "" {
				if javGuruSvc != nil {
					jgPoster, err := javGuruSvc.ResolvePoster(c.Request.Context(), code)
					if err == nil && jgPoster != "" {
						scene.Poster = jgPoster
						scene.Image = jgPoster
						scene.PosterImage = jgPoster
					}
				}
			}
		}

		originalPoster := posterSvc.CleanOriginalImageURL(scene.Poster)
		if originalPoster == "" {
			originalPoster = posterSvc.CleanOriginalImageURL(scene.PosterImage)
		}
		originalBackground := posterSvc.CleanOriginalImageURL(scene.Image)

		if originalPoster == "" && originalBackground != "" {
			originalPoster = originalBackground
		}
		if originalBackground == "" && originalPoster != "" {
			originalBackground = originalPoster
		}

		proxiedPoster := ""
		if originalPoster != "" {
			proxiedPoster = fmt.Sprintf("/discover/adult/poster?url=%s&shape=portrait&isJav=%t", url.QueryEscape(originalPoster), isJav)
		}

		proxiedBackground := ""
		if originalBackground != "" {
			proxiedBackground = fmt.Sprintf("/discover/adult/poster?url=%s&shape=landscape&isJav=%t", url.QueryEscape(originalBackground), isJav)
		}

		year := "N/A"
		if len(scene.Date) >= 4 {
			year = scene.Date[:4]
		}

		studioName := ""
		if scene.Site != nil {
			studioName = scene.Site.Name
		}
		if isJav {
			studioName = getJavStudioName(scene.Title, scene.ID, studioName)
		}

		metaType := "porn"
		if isJav {
			metaType = "jav"
		}

		c.JSON(http.StatusOK, gin.H{
			"meta": gin.H{
				"id":               id,
				"type":             metaType,
				"name":             scene.Title,
				"poster":           proxiedPoster,
				"posterHorizontal": proxiedBackground,
				"background":       proxiedBackground,
				"description":      scene.Description,
				"year":             year,
				"releaseInfo":      scene.Date,
				"studio":           studioName,
			},
		})
	}
}

func handlePoster(posterSvc *adultposter.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		originalURL := c.Query("url")
		if originalURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing url query parameter"})
			return
		}

		shape := c.Query("shape")
		if shape == "" {
			shape = "portrait"
		}
		isJav := c.Query("isJav") == "true"

		filePath := posterSvc.GetCacheFilePath(originalURL, shape)

		if _, err := os.Stat(filePath); err == nil {
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(filePath)
			return
		}

		_, err, _ := posterSingleFlight.Do(filePath, func() (interface{}, error) {
			if _, err := os.Stat(filePath); err == nil {
				return nil, nil
			}
			return nil, posterSvc.WarmSinglePoster(originalURL, filePath, shape, isJav)
		})
		if err == nil {
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(filePath)
			return
		}

		log.WithError(err).Warnf("failed to crop/resize poster synchronously for %s, redirecting", originalURL)
		c.Redirect(http.StatusTemporaryRedirect, originalURL)
	}
}

func handleCometProxy(
	tpdbSvc *tpdb.Service,
	stashdbSvc *stashdb.Service,
	prowlarrSvc *prowlarr.Service,
	redisClient *cs.RedisClient,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		id = strings.TrimPrefix(id, "/")
		id = strings.TrimSuffix(id, ".json")

		if strings.HasPrefix(id, "tpdb_jav:") || strings.HasPrefix(id, "stash:") || strings.HasPrefix(id, "tpdb:") {
			handleAdultStreams(c, tpdbSvc, stashdbSvc, prowlarrSvc, redisClient, id)
			return
		}

		cometURL := "http://127.0.0.1:8001/e30="
		streamURL := fmt.Sprintf("%s/stream/movie/%s.json", cometURL, id)

		req, err := http.NewRequestWithContext(c.Request.Context(), "GET", streamURL, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			c.JSON(resp.StatusCode, gin.H{"error": fmt.Sprintf("comet status: %d", resp.StatusCode)})
			return
		}

		type CometStreamItem struct {
			Name     string `json:"name"`
			Title    string `json:"title"`
			URL      string `json:"url,omitempty"`
			InfoHash string `json:"infoHash,omitempty"`
			FileIdx  *int   `json:"fileIdx,omitempty"`
		}
		type CometStreamsResponse struct {
			Streams []CometStreamItem `json:"streams"`
		}

		var streamsResp CometStreamsResponse
		if err := json.NewDecoder(resp.Body).Decode(&streamsResp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		for i := range streamsResp.Streams {
			if strings.Contains(streamsResp.Streams[i].Name, "Comet") {
				streamsResp.Streams[i].Name = strings.ReplaceAll(streamsResp.Streams[i].Name, "Comet", "⚡ Octor")
			}
		}

		c.JSON(http.StatusOK, streamsResp)
	}
}

func handleAdultStreams(
	c *gin.Context,
	tpdbSvc *tpdb.Service,
	stashdbSvc *stashdb.Service,
	prowlarrSvc *prowlarr.Service,
	redisClient *cs.RedisClient,
	id string,
) {
	exhaustive := c.Query("exhaustive") == "true"

	parts := strings.SplitN(id, ":", 2)
	if len(parts) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid adult scene ID format"})
		return
	}
	prefix := parts[0]
	sceneID := parts[1]

	var scene *tpdb.TpdbScene
	var err error

	if prefix == "tpdb_jav" {
		if strings.HasPrefix(sceneID, "fallback_") {
			code := strings.TrimPrefix(sceneID, "fallback_")
			redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)
			if redisClient != nil {
				val, _ := redisClient.Get().Get(c.Request.Context(), redisKey).Result()
				if val != "" {
					_ = json.Unmarshal([]byte(val), &scene)
				}
			}
			if scene == nil {
				scene = &tpdb.TpdbScene{
					ID:    sceneID,
					Title: strings.ToUpper(code),
				}
			}
		} else {
			scene, err = tpdbSvc.FetchJavByID(c.Request.Context(), sceneID)
		}
	} else if prefix == "stash" {
		scene, err = stashdbSvc.FetchStashDBSingleScene(c.Request.Context(), sceneID)
	} else {
		scene, err = tpdbSvc.FetchSceneByID(c.Request.Context(), sceneID)
	}

	if err != nil || scene == nil {
		log.Errorf("Failed to resolve metadata for adult ID: %s, error: %v", id, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "metadata not found"})
		return
	}

	javMode := prefix == "tpdb_jav"
	streams, err := prowlarrSvc.SearchAdultStreams(c.Request.Context(), id, scene, exhaustive, javMode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, prowlarr.StreamsResponse{
		Streams: streams,
	})
}
