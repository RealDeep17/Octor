package discover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/javguru"
	"github.com/webtor-io/web-ui/services/tpdb"
)

func getInfraDataPath(subpath string) string {
	return cs.GetInfraDataPath(subpath)
}

var CacheDir = getInfraDataPath("cache/adult-posters")

var (
	resolvedStudioIDs           []string
	resolvedStudioNameToID      map[string]string
	resolvedStudiosLock         sync.RWMutex
	globalRedisClient           *cs.RedisClient
	globalJavGuruSvc            *javguru.Service
	javCodeRx                   = regexp.MustCompile(`(?i)\b([a-zA-Z]{2,10})[\s\-_]?(\d{3,8})\b`)
	siteripRx                   = regexp.MustCompile(`(?i)\b(siterips?|site-rips?|megapacks?|mega-packs?|packs?)\b`)
	globalProwlarrIndexersCache struct {
		sync.Mutex
		indexers  []ProwlarrIndexer
		updatedAt time.Time
	}
)

func RegisterAdultRoutes(r *gin.Engine, tpdbSvc *tpdb.Service, adminSvc *admin.Admin, redisClient *cs.RedisClient, javGuruSvc *javguru.Service) {
	if tpdbSvc == nil {
		log.Warn("TPDB service is nil, adult discovery endpoints will be disabled")
		return
	}
	globalRedisClient = redisClient
	globalJavGuruSvc = javGuruSvc

	// Start dynamic studio resolving in background
	go resolveStudiosJob()

	// Server-side gated group enforcing admin authorization
	adminGroup := r.Group("/discover/adult")
	adminGroup.Use(func(c *gin.Context) {
		if !auth.IsAdmin(c) && !adminSvc.HasAdmin(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: Admin access required"})
			c.Abort()
			return
		}
		c.Next()
	})

	adminGroup.GET("/catalog/:type/*path", handleCatalog(tpdbSvc))
	adminGroup.GET("/meta/adult/*path", handleMeta(tpdbSvc))
	adminGroup.GET("/poster", handlePoster())
	adminGroup.HEAD("/poster", handlePoster())
	adminGroup.GET("/comet-proxy/stream/movie/*id", handleCometProxy(tpdbSvc))

	// Start background ticker for pre-warming catalogs and pruning posters every 4 hours
	go runInternalPreWarmer(tpdbSvc)
}

var fallbackStudioNameToID = map[string]string{
	"vixen":              "732560e2-c42b-426c-9471-f92e0ab77609",
	"blacked":            "b281cfc0-d31e-450f-936b-35201aa14234",
	"tushy":              "829bf298-2457-41ec-b8eb-9d107a0e3037",
	"blacked raw":        "800c4c53-a5cf-4063-9b92-2bed1024eaf4",
	"tushy raw":          "3901a1c9-7df9-42b7-8db1-9be0613afb65",
	"brazzers":           "eec95cdd-9f58-4fc7-b7d1-e98786453d27",
	"bang bros":          "525f8c32-d14f-42c0-939c-bb5d8eae8bcf",
	"naughty america":    "2be8463b-0505-479e-a07d-5abc7a6edd54",
	"adult time":         "6fa161d2-3c28-44b7-86a2-d98f5a8373aa",
	"reality kings":      "4bed0748-a3aa-4e22-8c46-f5ef732b5149",
	"jules jordan":       "b8e5b226-503b-4852-ad89-5e76853728e3",
	"digital playground": "1e8d8028-fb8c-4286-b742-e62e5667c576",
	"twistys":            "ec61a0b4-d5ae-4c87-9d2d-68e6674790f6",
	"mile high media":    "6070e7f0-b8a1-463b-bf0f-0cb3499fd00c",
	"newsensations":      "3e8fb21e-1c95-44f6-839c-8f24ae7d48f9",
	"property sex":       "b4a8d39e-6ed8-401e-b8d1-3e3f107b70ca",
	"elegant angel":      "475bdee5-3b4a-4153-9706-01f457a59e4a",
	"private":            "e83ce38f-ddf4-45a7-800e-c199392a09c4",
	"ddf network":        "a7cba57b-cc9d-47ae-8468-af1e20b6f61a",
	"dane jones":         "046ea0c9-c1ae-47bf-9a7e-ba27706da1c7",
	"dorcel":             "21397546-2256-4ed9-9a5b-08c10ca56e3e",
	"archangel":          "c1cf3e96-6324-4d61-8114-a42f12aef348",
}

func resolveStudiosJob() {
	log.Info("Resolving mainstream adult studios dynamically from StashDB (recursively up to 3 levels)...")
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		log.Warn("STASHDB_API_KEY is not configured; using fallback studio list")
		resolvedStudiosLock.Lock()
		resolvedStudioIDs = fallbackStudioIDs
		resolvedStudioNameToID = fallbackStudioNameToID
		resolvedStudiosLock.Unlock()
		return
	}

	parentIDs := []string{
		"b62bc449-c3d9-49ff-9a16-8f5b1bfa20b9", // Vixen Media Group
		"eec95cdd-9f58-4fc7-b7d1-e98786453d27", // Brazzers
		"525f8c32-d14f-42c0-939c-bb5d8eae8bcf", // BangBros
		"568c160d-ad70-42f3-9e17-03a69641b14e", // TeamSkeet
		"2be8463b-0505-479e-a07d-5abc7a6edd54", // Naughty America
		"6fa161d2-3c28-44b7-86a2-d98f5a8373aa", // Adult Time
		"4bed0748-a3aa-4e22-8c46-f5ef732b5149", // Reality Kings
		"b8e5b226-503b-4852-ad89-5e76853728e3", // Jules Jordan
		"1e8d8028-fb8c-4286-b742-e62e5667c576", // Digital Playground
		"ec61a0b4-d5ae-4c87-9d2d-68e6674790f6", // Twistys
		"6070e7f0-b8a1-463b-bf0f-0cb3499fd00c", // Mile High Media
		"de45e4b3-7204-4393-a2b3-c485dd106644", // Pornbox
		"c5f4f1d2-bf00-4e7a-9fc1-3d4e171dd246", // Nubiles Porn
		"8218160c-2dcb-4fe8-8acb-9937c5a2a61f", // Metro HD
		"3e8fb21e-1c95-44f6-839c-8f24ae7d48f9", // New Sensations
		"9be5eddc-a9f2-4e5c-9bf1-086ddf08a907", // BLT Innovations
		"e42f2af3-d410-4bb4-bb5c-1b5fbbb07eae", // Alex Adams Media
		"b4a8d39e-6ed8-401e-b8d1-3e3f107b70ca", // Property Sex
		"475bdee5-3b4a-4153-9706-01f457a59e4a", // Elegant Angel
		"e83ce38f-ddf4-45a7-800e-c199392a09c4", // Private
		"a7cba57b-cc9d-47ae-8468-af1e20b6f61a", // DDF Network
		"046ea0c9-c1ae-47bf-9a7e-ba27706da1c7", // Dane Jones
		"21397546-2256-4ed9-9a5b-08c10ca56e3e", // Dorcel
		"c1cf3e96-6324-4d61-8114-a42f12aef348", // ArchAngel Video
		"2c9d7ab5-3db2-4744-af76-d9e920c4dd9b", // MetArt Network
		"1faa9636-6eb8-4c3b-bcf9-d34670c21506", // Deeper
		"842cc5ba-4f5c-4bba-bc4e-04637f4d5c13", // SpyFam
		"70b41fb2-aa50-46bc-bb66-090c38be7149", // Devil's Film (Network)
		"dba7de20-ee98-4563-90a6-c697dc54e375", // Cherry Pimps
		"647792dd-9ccc-49b4-8b85-b7f4de0b5d48", // Sweet Sinner
		"f6ae4372-00df-463f-9551-442d6650b855", // Wicked Pictures
		"1a6c64c5-dde7-477c-addb-c3d1381cf593", // Digital Sin
		"233babe6-ff73-40ef-8650-cb2c95ab6a4e", // Penthouse
		"8c4e72ad-4dfb-4e00-bb28-ab92cb9c61a0", // BaDoink
		"91792902-ba1e-4c5f-b7f5-c129e626ce8a", // JoyMii
		"643bb8a1-9556-443d-950b-e016218c2ed4", // Scoreland
		"bf892f77-11a3-483e-8a41-2a94146dc4c2", // 21 Sextury (Network)
		"d22943f5-9bb5-495e-8b01-6e12d2fffc80", // X-Art
		"f47c35d9-a62b-417a-a492-33bab9c9cd64", // Mom Lover (Network)
		"75fab6e9-275b-4348-936f-3184d034e19b", // ExploitedX
		"98c4c6c5-cf89-487c-91cf-902aa121eb3b", // MYLF
		"a37b47ad-0854-4f74-84ba-9d8c70ba1dd9", // Fakehub
		"059a602f-7ba6-419e-a422-e688c87359be", // Fantasy Massage (Network)
		"3396ac96-80e9-40aa-825f-fcc09159835f", // Dogfart Network
		"eb711259-b4fa-49a4-a572-ebfaba0f93e7", // NF Media
		"b464d27d-2611-4f78-b7ee-68065a305b6e", // Fuck You Cash
	}

	excludeRx := regexp.MustCompile(`(?i)\b(lesbians?|gays?|bi-?empire|boys?|males?|men|trans|t-?girls?|midgets?)\b`)

	resolvedIDs := make(map[string]bool)
	for _, id := range parentIDs {
		resolvedIDs[id] = true
	}

	newStudioNameToID := make(map[string]string)
	for name, id := range fallbackStudioNameToID {
		newStudioNameToID[strings.ToLower(name)] = id
	}

	cl := &http.Client{Timeout: 15 * time.Second}
	currentParents := parentIDs

	// Query StashDB recursively up to 3 levels
	for level := 1; level <= 3; level++ {
		if len(currentParents) == 0 {
			break
		}

		var nextParents []string
		page := 1

		for {
			query := `query GetChildStudios($parent_ids: [ID!]!, $page: Int!) {
				queryStudios(input: {
					parent: { value: $parent_ids, modifier: INCLUDES }
					page: $page
					per_page: 500
				}) {
					studios {
						id
						name
					}
				}
			}`

			body := map[string]interface{}{
				"query": query,
				"variables": map[string]interface{}{
					"parent_ids": currentParents,
					"page":       page,
				},
			}

			bodyBytes, err := json.Marshal(body)
			if err != nil {
				break
			}

			req, err := http.NewRequest("POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
			if err != nil {
				break
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("ApiKey", apiKey)

			resp, err := cl.Do(req)
			if err != nil {
				log.WithError(err).Warn("StashDB queryStudios request failed")
				break
			}

			type StudioEntry struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			type QueryStudiosResp struct {
				Data struct {
					QueryStudios struct {
						Studios []StudioEntry `json:"studios"`
					} `json:"queryStudios"`
				} `json:"data"`
			}

			var qResp QueryStudiosResp
			err = json.NewDecoder(resp.Body).Decode(&qResp)
			resp.Body.Close()

			if err != nil {
				break
			}

			studios := qResp.Data.QueryStudios.Studios
			if len(studios) == 0 {
				break
			}

			for _, s := range studios {
				if resolvedIDs[s.ID] {
					continue
				}
				if !excludeRx.MatchString(s.Name) {
					resolvedIDs[s.ID] = true
					newStudioNameToID[strings.ToLower(s.Name)] = s.ID
					nextParents = append(nextParents, s.ID)
				}
			}

			if len(studios) < 500 {
				break
			}
			page++
		}
		currentParents = nextParents
	}

	var allChildIDs []string
	for id := range resolvedIDs {
		allChildIDs = append(allChildIDs, id)
	}

	resolvedStudiosLock.Lock()
	if len(allChildIDs) > len(parentIDs) {
		resolvedStudioIDs = allChildIDs
		resolvedStudioNameToID = newStudioNameToID
		log.Infof("Successfully resolved %d straight mainstream studios recursively from StashDB dynamically!", len(allChildIDs))
	} else {
		resolvedStudioIDs = fallbackStudioIDs
		resolvedStudioNameToID = fallbackStudioNameToID
		log.Infof("StashDB query failed or returned no children; falling back to pre-resolved studios")
	}
	resolvedStudiosLock.Unlock()
}

func runInternalPreWarmer(tpdbSvc *tpdb.Service) {
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
				if globalJavGuruSvc != nil {
					if cat.id == "jav-recent" {
						scenes, err = globalJavGuruSvc.FetchRecent(ctx, 1)
					} else if cat.id == "jav-subbed" {
						scenes, err = globalJavGuruSvc.FetchSubbed(ctx, 1)
					} else if cat.id == "jav-trending" {
						scenes, err = globalJavGuruSvc.FetchTrending(ctx)
					}
				}
			} else {
				scenes, err = fetchStashDBScenes(ctx, cat.id, 1, "")
			}

			if err != nil {
				log.WithError(err).Warnf("Internal pre-warmer: failed to fetch scenes for catalog %s", cat.id)
				continue
			}
			PreWarmPosters(scenes, cat.isJav)
		}

		log.Info("Internal adult discovery catalog pre-warming complete. Pruning old posters...")
		if err := PruneAdultPosters(7); err != nil {
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

type StashDBSceneListItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`
	Details string `json:"details"`
	Studio  struct {
		Name string `json:"name"`
	} `json:"studio"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
}

var wpThumbnailRegex = regexp.MustCompile(`-\d+x\d+(\.[a-zA-Z0-9]+)$`)

func cleanOriginalImageURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	cleaned := urlStr
	if strings.Contains(cleaned, "/wp-content/uploads/") {
		cleaned = strings.ReplaceAll(cleaned, "/thumbs/", "/")
	}
	cleaned = wpThumbnailRegex.ReplaceAllString(cleaned, "$1")
	if strings.Contains(cleaned, "pics.dmm.co.jp") {
		cleaned = strings.ReplaceAll(cleaned, "ps.jpg", "pl.jpg")
	}
	return cleaned
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

func findStudioIDByName(ctx context.Context, apiKey, name string) (string, error) {
	query := `query FindStudio($name: String!) {
		queryStudios(input: {
			name: $name
		}) {
			studios {
				id
				name
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"name": name,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	cl := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stashdb queryStudios status: %d", resp.StatusCode)
	}

	type QueryStudiosResp struct {
		Data struct {
			QueryStudios struct {
				Studios []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"studios"`
			} `json:"queryStudios"`
		} `json:"data"`
	}

	var qResp QueryStudiosResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return "", err
	}

	studios := qResp.Data.QueryStudios.Studios
	if len(studios) > 0 {
		return studios[0].ID, nil
	}

	return "", fmt.Errorf("studio not found: %s", name)
}

// findPerformerIDByName queries StashDB for a performer by name (INCLUDES match).
// Returns the performer's UUID or empty string if not found.
func findPerformerIDByName(ctx context.Context, apiKey, name string) (string, error) {
	query := `query FindPerformer($name: String!) {
		queryPerformers(input: {
			name: $name
			per_page: 5
		}) {
			performers {
				id
				name
				gender
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"name": name,
		},
	}
	bodyBytes, _ := json.Marshal(body)

	cl := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stashdb queryPerformers status: %d", resp.StatusCode)
	}

	type PerformerEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Gender string `json:"gender"`
	}
	type QueryPerformersResp struct {
		Data struct {
			QueryPerformers struct {
				Performers []PerformerEntry `json:"performers"`
			} `json:"queryPerformers"`
		} `json:"data"`
	}

	var qResp QueryPerformersResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return "", err
	}

	// Prefer FEMALE performers; skip MALE-only results (avoids gay/trans noise)
	for _, p := range qResp.Data.QueryPerformers.Performers {
		if strings.EqualFold(p.Gender, "FEMALE") || strings.EqualFold(p.Gender, "TRANSGENDER_FEMALE") {
			return p.ID, nil
		}
	}
	// Accept any gender match if no female found (e.g. male performers in straight scenes)
	for _, p := range qResp.Data.QueryPerformers.Performers {
		if p.ID != "" {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("performer not found: %s", name)
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
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func preprocessSearchQuery(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

func fetchStashDBScenes(ctx context.Context, sortBy string, page int, searchQuery string) ([]tpdb.TpdbScene, error) {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("STASHDB_API_KEY not configured")
	}

	resolvedStudiosLock.RLock()
	studios := resolvedStudioIDs
	resolvedStudiosLock.RUnlock()

	if len(studios) == 0 {
		studios = fallbackStudioIDs
	}

	sortEnum := "DATE"
	direction := "DESC"
	if sortBy == "trending" || sortBy == "top-rated" {
		sortEnum = "TRENDING"
	}

	query := `query GetScenes($studios: MultiIDCriterionInput, $performers: MultiIDCriterionInput, $page: Int!, $sort: SceneSortEnum!, $direction: SortDirectionEnum!, $text: String) {
		queryScenes(input: { page: $page, per_page: 60, sort: $sort, direction: $direction, studios: $studios, performers: $performers, text: $text }) {
			scenes {
				id
				title
				date
				details
				studio {
					name
				}
				images {
					url
				}
			}
		}
	}`

	var searchStudioID string
	if searchQuery != "" {
		processedQuery := strings.ToLower(preprocessSearchQuery(searchQuery))
		// squash removes all spaces for fuzzy matching: "puretaboo" == "pure taboo"
		squash := func(s string) string { return strings.ReplaceAll(s, " ", "") }
		squashedQuery := squash(processedQuery)

		resolvedStudiosLock.RLock()
		if id, ok := resolvedStudioNameToID[processedQuery]; ok {
			searchStudioID = id
		} else {
			for name, id := range resolvedStudioNameToID {
				// Match with spaces (partial) OR with spaces stripped (handles puretaboo, pervmom, etc.)
				if name == processedQuery ||
					strings.Contains(processedQuery, name) ||
					strings.Contains(name, processedQuery) ||
					squash(name) == squashedQuery {
					searchStudioID = id
					break
				}
			}
		}
		resolvedStudiosLock.RUnlock()

		if searchStudioID == "" {
			// Query StashDB dynamically — try both the original query and the squashed version
			id, err := findStudioIDByName(ctx, apiKey, processedQuery)
			if (err != nil || id == "") && squashedQuery != processedQuery {
				// e.g. "puretaboo" didn't match; StashDB won't find it either
				// but the squashed form won't help since StashDB doesn't do that
				// — so skip; performer lookup below is the next fallback
				_ = err
			}
			if err == nil && id != "" {
				searchStudioID = id
				resolvedStudiosLock.Lock()
				resolvedStudioNameToID[processedQuery] = id
				resolvedStudiosLock.Unlock()
			}
		}
	}

	// If no studio match found, try to find a performer by that name
	var searchPerformerID string
	if searchQuery != "" && searchStudioID == "" {
		if pid, err := findPerformerIDByName(ctx, apiKey, preprocessSearchQuery(searchQuery)); err == nil && pid != "" {
			searchPerformerID = pid
		}
	}

	vars := map[string]interface{}{
		"page":      page,
		"sort":      sortEnum,
		"direction": direction,
	}
	if searchStudioID != "" {
		// Studio search: filter by that specific studio only — no text filter.
		// Scene titles don't repeat the studio name, so adding text: kills results.
		vars["studios"] = map[string]interface{}{
			"value":    []string{searchStudioID},
			"modifier": "INCLUDES",
		}
	} else if searchPerformerID != "" {
		// Performer search: filter by performer AND restrict to mainstream studios
		vars["performers"] = map[string]interface{}{
			"value":    []string{searchPerformerID},
			"modifier": "INCLUDES",
		}
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	} else if searchQuery != "" {
		// Text-only fallback: still restrict to mainstream studios to prevent gay noise
		vars["text"] = preprocessSearchQuery(searchQuery)
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	} else {
		vars["studios"] = map[string]interface{}{
			"value":    studios,
			"modifier": "INCLUDES",
		}
	}

	body := map[string]interface{}{
		"query":     query,
		"variables": vars,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	cl := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stashdb status: %d", resp.StatusCode)
	}

	type QueryScenesResp struct {
		Data struct {
			QueryScenes struct {
				Scenes []StashDBSceneListItem `json:"scenes"`
			} `json:"queryScenes"`
		} `json:"data"`
	}

	var qResp QueryScenesResp
	if err := json.NewDecoder(resp.Body).Decode(&qResp); err != nil {
		return nil, err
	}

	scenes := qResp.Data.QueryScenes.Scenes
	tpdbScenes := make([]tpdb.TpdbScene, len(scenes))
	for i, s := range scenes {
		poster := ""
		if len(s.Images) > 0 {
			poster = s.Images[0].URL
		}
		tpdbScenes[i] = tpdb.TpdbScene{
			ID:          s.ID,
			Title:       s.Title,
			Description: s.Details,
			Date:        s.Date,
			Poster:      poster,
			Site: &tpdb.TpdbSceneSite{
				Name: s.Studio.Name,
			},
		}
	}

	return tpdbScenes, nil
}

type WppItem struct {
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Date string `json:"date"`
}

func fetchJavGuruRecent(ctx context.Context, page int) ([]struct{ Title, PubDate string }, error) {
	u := fmt.Sprintf("https://jav.guru/wp-json/wp/v2/posts?categories=40&per_page=24&page=%d", page)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jav.guru WP-JSON Recent returned status %d", resp.StatusCode)
	}

	var items []WppItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	entries := make([]struct{ Title, PubDate string }, len(items))
	for i, item := range items {
		entries[i] = struct{ Title, PubDate string }{
			Title:   item.Title.Rendered,
			PubDate: item.Date,
		}
	}
	return entries, nil
}

func fetchJavGuruEnglishSubbed(ctx context.Context, page int) ([]struct{ Title, PubDate string }, error) {
	u := fmt.Sprintf("https://jav.guru/wp-json/wp/v2/posts?categories=1826&per_page=24&page=%d", page)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jav.guru WP-JSON Subbed returned status %d", resp.StatusCode)
	}

	var items []WppItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	entries := make([]struct{ Title, PubDate string }, len(items))
	for i, item := range items {
		entries[i] = struct{ Title, PubDate string }{
			Title:   item.Title.Rendered,
			PubDate: item.Date,
		}
	}
	return entries, nil
}

func fetchJavGuruTrending(ctx context.Context) ([]struct{ Title, PubDate string }, error) {
	u := "https://jav.guru/wp-json/wordpress-popular-posts/v1/popular-posts?limit=60"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jav.guru WPP API returned status %d", resp.StatusCode)
	}

	var items []WppItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	entries := make([]struct{ Title, PubDate string }, len(items))
	for i, item := range items {
		entries[i] = struct{ Title, PubDate string }{
			Title:   item.Title.Rendered,
			PubDate: item.Date,
		}
	}
	return entries, nil
}

func matchJavCodeFuzzy(cleanedURL, cleanCode string) bool {
	if strings.Contains(cleanedURL, cleanCode) {
		return true
	}

	// Parse letters and numbers
	rx := regexp.MustCompile(`^([a-z0-9]+?)(\d+)$`)
	m := rx.FindStringSubmatch(cleanCode)
	if len(m) == 3 {
		letters := m[1]
		numbers := m[2]
		trimmedNumbers := strings.TrimLeft(numbers, "0")
		if trimmedNumbers == "" {
			trimmedNumbers = "0"
		}

		if strings.Contains(cleanedURL, letters) && strings.Contains(cleanedURL, trimmedNumbers) {
			return true
		}
	}
	return false
}

func fetchJavGuruPoster(ctx context.Context, queryOrCode string) (string, error) {
	if queryOrCode == "" || strings.HasPrefix(queryOrCode, "NOCODE_") {
		return "", fmt.Errorf("invalid query")
	}

	// Determine if this is a JAV code or a full title search
	isFullTitleSearch := strings.Contains(queryOrCode, " ") || len(queryOrCode) > 15
	cleanCode := strings.ToLower(strings.ReplaceAll(queryOrCode, "-", ""))

	// 1. Fetch JAV Guru search page HTML
	searchURL := fmt.Sprintf("https://jav.guru/?s=%s", url.QueryEscape(queryOrCode))
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status: %d", resp.StatusCode)
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	htmlStr := string(htmlBytes)

	// 2. Scan for matching post links in the search page
	linkRx := regexp.MustCompile(`(?i)href=["'](https?://jav\.guru/\d+/[^"']+)["']`)
	matches := linkRx.FindAllStringSubmatch(htmlStr, -1)

	var postLink string
	if !isFullTitleSearch {
		for _, m := range matches {
			if len(m) > 1 {
				link := m[1]
				linkLower := strings.ToLower(link)
				// Clean both link and code of hyphens/spaces for a perfectly robust comparison
				linkClean := strings.ReplaceAll(linkLower, "-", "")
				if matchJavCodeFuzzy(linkClean, cleanCode) {
					postLink = link
					break
				}
			}
		}
	}

	// Fallback/Full Title: pick the first post link in search results
	if postLink == "" {
		for _, m := range matches {
			if len(m) > 1 {
				link := m[1]
				// Avoid standard static pages
				if !strings.Contains(link, "/category/") && !strings.Contains(link, "/tag/") && !strings.Contains(link, "/author/") {
					postLink = link
					break
				}
			}
		}
	}

	if postLink == "" {
		return "", fmt.Errorf("no matching post link found in search page")
	}

	// 3. Fetch the raw post page HTML
	htmlReq, err := http.NewRequestWithContext(ctx, "GET", postLink, nil)
	if err != nil {
		return "", err
	}
	htmlReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	htmlResp, err := cl.Do(htmlReq)
	if err != nil {
		return "", err
	}
	defer htmlResp.Body.Close()

	if htmlResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("post page status: %d", htmlResp.StatusCode)
	}

	postHtmlBytes, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		return "", err
	}
	postHtmlStr := string(postHtmlBytes)

	// 4. Parse first original wide image matching JAV code
	imgRx := regexp.MustCompile(`(?i)<img[^>]+src=["'](https?://[^"']+\.(?:jpg|png|webp|jpeg))["']`)
	allImgs := imgRx.FindAllStringSubmatch(postHtmlStr, -1)

	for _, imgMatch := range allImgs {
		if len(imgMatch) > 1 {
			imgURL := imgMatch[1]
			imgURLUnescaped := strings.ToLower(imgURL)

			if !isFullTitleSearch {
				// Clean URL of hyphens/spaces for a perfectly robust comparison
				cleanedURL := strings.ReplaceAll(imgURLUnescaped, "-", "")
				if matchJavCodeFuzzy(cleanedURL, cleanCode) &&
					!strings.Contains(imgURLUnescaped, "logo") &&
					!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
					return cleanOriginalImageURL(imgURL), nil
				}
			} else {
				// For full title search, pick the first main upload horizontal cover image
				if strings.Contains(imgURLUnescaped, "/wp-content/uploads/") &&
					!strings.Contains(imgURLUnescaped, "logo") &&
					!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
					return cleanOriginalImageURL(imgURL), nil
				}
			}
		}
	}

	// Secondary fallback: First main upload horizontal image on the page
	for _, imgMatch := range allImgs {
		if len(imgMatch) > 1 {
			imgURL := imgMatch[1]
			imgURLUnescaped := strings.ToLower(imgURL)
			if strings.Contains(imgURLUnescaped, "/wp-content/uploads/") &&
				!strings.Contains(imgURLUnescaped, "logo") &&
				!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
				return cleanOriginalImageURL(imgURL), nil
			}
		}
	}

	return "", fmt.Errorf("no cover image found on post page")
}

func resolveSingleJavScene(ctx context.Context, tpdbSvc *tpdb.Service, code, javGuruTitle, pubDate string) (tpdb.TpdbScene, error) {
	redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)

	if globalRedisClient != nil {
		val, err := globalRedisClient.Get().Get(ctx, redisKey).Result()
		if err == nil && val != "" {
			var cachedScene tpdb.TpdbScene
			if err := json.Unmarshal([]byte(val), &cachedScene); err == nil {
				return cachedScene, nil
			}
		}
	}

	var scene tpdb.TpdbScene
	var found bool
	if !strings.HasPrefix(code, "NOCODE_") {
		scenes, err := tpdbSvc.SearchJavScenes(ctx, code)
		if err == nil && len(scenes) > 0 {
			scene = scenes[0]
			found = true
		}
	}

	searchQuery := code
	if strings.HasPrefix(code, "NOCODE_") {
		searchQuery = javGuruTitle
	}

	jgPoster, err := fetchJavGuruPoster(ctx, searchQuery)
	if err == nil && jgPoster != "" {
		scene.Poster = jgPoster
		scene.Image = jgPoster
		scene.PosterImage = jgPoster
	}

	if !found {
		scene = tpdb.TpdbScene{
			ID:    "fallback_" + code,
			Title: javGuruTitle,
			Date:  pubDate,
		}
		if jgPoster != "" {
			scene.Poster = jgPoster
			scene.Image = jgPoster
			scene.PosterImage = jgPoster
		}
	}

	if globalRedisClient != nil {
		sceneJSON, _ := json.Marshal(scene)
		globalRedisClient.Get().Set(ctx, redisKey, string(sceneJSON), 4*time.Hour)
	}

	return scene, nil
}

func resolveJavScenes(ctx context.Context, tpdbSvc *tpdb.Service, entries []struct{ Title, PubDate string }) []tpdb.TpdbScene {
	var scenes []tpdb.TpdbScene
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 10)
	for _, entry := range entries {
		wg.Add(1)
		go func(t, d string) {
			defer wg.Done()
			codeRx := regexp.MustCompile(`(?i)\b([a-z0-9]{2,10}-\d{3,6})\b`)
			codeMatch := codeRx.FindString(t)

			var code string
			if codeMatch != "" {
				code = strings.ToUpper(codeMatch)
			} else {
				h := sha256.Sum256([]byte(t))
				code = "NOCODE_" + hex.EncodeToString(h[:8])
			}

			sem <- struct{}{}
			scene, err := resolveSingleJavScene(ctx, tpdbSvc, code, t, d)
			<-sem
			if err == nil {
				mu.Lock()
				scenes = append(scenes, scene)
				mu.Unlock()
			}
		}(entry.Title, entry.PubDate)
	}
	wg.Wait()

	sceneMap := make(map[string]tpdb.TpdbScene)
	for _, s := range scenes {
		sceneMap[s.ID] = s
	}

	var ordered []tpdb.TpdbScene
	for _, entry := range entries {
		codeRx := regexp.MustCompile(`(?i)\b([a-z0-9]{2,10}-\d{3,6})\b`)
		codeMatch := codeRx.FindString(entry.Title)

		var code string
		if codeMatch != "" {
			code = strings.ToUpper(codeMatch)
		} else {
			h := sha256.Sum256([]byte(entry.Title))
			code = "NOCODE_" + hex.EncodeToString(h[:8])
		}

		for _, s := range scenes {
			if s.ID == "fallback_"+code || s.ID == code || strings.Contains(strings.ToUpper(s.Title), code) {
				ordered = append(ordered, s)
				break
			}
		}
	}

	seen := make(map[string]bool)
	var finalScenes []tpdb.TpdbScene
	for _, s := range ordered {
		if !seen[s.ID] {
			seen[s.ID] = true
			finalScenes = append(finalScenes, s)
		}
	}

	return finalScenes
}

func handleCatalog(tpdbSvc *tpdb.Service) gin.HandlerFunc {
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
				if globalJavGuruSvc != nil {
					scenes, err = globalJavGuruSvc.Search(c.Request.Context(), processedSearch, page)
				} else {
					scenes, err = tpdbSvc.SearchJavScenes(c.Request.Context(), processedSearch)
				}
			} else {
				// Strategy: run StashDB first (it handles studio+performer lookup internally).
				// If StashDB returns ≥10 results, it found a studio/performer match — skip TPDB (no noise).
				// If StashDB returns <10, supplement with TPDB page-1 only (filtered for gay content).
				processedSearch := preprocessSearchQuery(searchQuery)

				stashScenes, errStash := fetchStashDBScenes(c.Request.Context(), catalogID, page, processedSearch)

				if errStash == nil && len(stashScenes) >= 10 {
					// StashDB found a studio/performer — use results directly, no TPDB noise
					scenes = stashScenes
				} else {
					// Supplement with TPDB (page 1 only, gay-filtered)
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
					for _, s := range stashScenes {
						norm := normalize(s.Title)
						if norm != "" && !seen[norm] {
							seen[norm] = true
							scenes = append(scenes, s)
						}
					}
					for _, s := range tpdbScenes {
						studioName := ""
						if s.Site != nil {
							studioName = s.Site.Name
						}
						if gayRx.MatchString(studioName) {
							continue
						}
						norm := normalize(s.Title)
						if norm != "" && !seen[norm] {
							seen[norm] = true
							scenes = append(scenes, s)
						}
					}
					if errStash != nil && len(scenes) == 0 {
						err = errStash
					}
				}
			}
		} else {
			if catalogID == "jav-recent" {
				if globalJavGuruSvc != nil {
					scenes, err = globalJavGuruSvc.FetchRecent(c.Request.Context(), page)
				}
			} else if catalogID == "jav-subbed" {
				if globalJavGuruSvc != nil {
					scenes, err = globalJavGuruSvc.FetchSubbed(c.Request.Context(), page)
				}
			} else if catalogID == "jav-trending" {
				if globalJavGuruSvc != nil {
					scenes, err = globalJavGuruSvc.FetchTrending(c.Request.Context())
				}
			} else {
				scenes, err = fetchStashDBScenes(c.Request.Context(), catalogID, page, "")
			}
		}

		if err != nil {
			log.WithError(err).Errorf("failed to fetch scenes for catalog %s", catalogID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		isJav := strings.HasPrefix(catalogID, "jav-") || c.Param("type") == "jav"

		go PreWarmPosters(scenes, isJav)

		metas := make([]gin.H, 0, len(scenes))
		for _, scene := range scenes {
			originalPoster := cleanOriginalImageURL(scene.Poster)
			if originalPoster == "" {
				originalPoster = cleanOriginalImageURL(scene.PosterImage)
			}
			originalBackground := cleanOriginalImageURL(scene.Image)

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

func fetchStashDBSingleScene(ctx context.Context, id string) (*tpdb.TpdbScene, error) {
	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("STASHDB_API_KEY not configured")
	}

	query := `query ($id: ID!) {
		findScene(id: $id) {
			id
			title
			release_date
			details
			studio {
				name
				parent {
					name
				}
			}
			performers {
				performer {
					name
				}
			}
			images {
				url
			}
		}
	}`

	body := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"id": id,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	cl := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://stashdb.org/graphql", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stashdb graphql status: %d", resp.StatusCode)
	}

	type FindSceneResp struct {
		Data struct {
			FindScene *struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				ReleaseDate string `json:"release_date"`
				Details     string `json:"details"`
				Studio      struct {
					Name   string `json:"name"`
					Parent *struct {
						Name string `json:"name"`
					} `json:"parent"`
				} `json:"studio"`
				Performers []struct {
					Performer struct {
						Name string `json:"name"`
					} `json:"performer"`
				} `json:"performers"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"findScene"`
		} `json:"data"`
	}

	var fResp FindSceneResp
	if err := json.NewDecoder(resp.Body).Decode(&fResp); err != nil {
		return nil, err
	}

	fs := fResp.Data.FindScene
	if fs == nil {
		return nil, fmt.Errorf("scene not found in stashdb: %s", id)
	}

	poster := ""
	if len(fs.Images) > 0 {
		poster = fs.Images[0].URL
	}

	var performers []tpdb.TpdbScenePerformer
	for _, p := range fs.Performers {
		if p.Performer.Name != "" {
			performers = append(performers, tpdb.TpdbScenePerformer{
				Name: p.Performer.Name,
			})
		}
	}

	parentName := ""
	if fs.Studio.Parent != nil {
		parentName = strings.TrimSpace(fs.Studio.Parent.Name)
	}

	return &tpdb.TpdbScene{
		ID:          fs.ID,
		Title:       fs.Title,
		Description: fs.Details,
		Date:        fs.ReleaseDate,
		Poster:      poster,
		Site: &tpdb.TpdbSceneSite{
			Name:   fs.Studio.Name,
			Parent: parentName,
		},
		Performers: performers,
	}, nil
}

func handleMeta(tpdbSvc *tpdb.Service) gin.HandlerFunc {
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
				if globalRedisClient != nil {
					val, _ := globalRedisClient.Get().Get(c.Request.Context(), redisKey).Result()
					if val != "" {
						_ = json.Unmarshal([]byte(val), &scene)
					}
				}
			} else {
				scene, err = tpdbSvc.FetchJavByID(c.Request.Context(), sceneID)
			}
		} else if prefix == "stash" {
			scene, err = fetchStashDBSingleScene(c.Request.Context(), sceneID)
		} else {
			scene, err = tpdbSvc.FetchSceneByID(c.Request.Context(), sceneID)
		}

		if err != nil || scene == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "scene not found"})
			return
		}

		isJav := prefix == "tpdb_jav"
		if isJav {
			// Extract code
			code := ""
			javCodeRx := regexp.MustCompile(`(?i)\b([a-zA-Z0-9]{2,10})-\d{3,8}\b`)
			if m := javCodeRx.FindString(scene.Title); m != "" {
				code = strings.ToUpper(m)
			} else if m := javCodeRx.FindString(scene.ID); m != "" {
				code = strings.ToUpper(m)
			}

			if code != "" {
				if globalJavGuruSvc != nil {
					jgPoster, err := globalJavGuruSvc.ResolvePoster(c.Request.Context(), code)
					if err == nil && jgPoster != "" {
						scene.Poster = jgPoster
						scene.Image = jgPoster
						scene.PosterImage = jgPoster
					}
				}
			}
		}

		originalPoster := cleanOriginalImageURL(scene.Poster)
		if originalPoster == "" {
			originalPoster = cleanOriginalImageURL(scene.PosterImage)
		}
		originalBackground := cleanOriginalImageURL(scene.Image)

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

func handlePoster() gin.HandlerFunc {
	return func(c *gin.Context) {
		originalURL := c.Query("url")
		if originalURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing url query parameter"})
			return
		}

		shape := c.Query("shape") // "portrait" or "landscape"
		if shape == "" {
			shape = "portrait"
		}
		isJav := c.Query("isJav") == "true"

		_ = os.MkdirAll(CacheDir, 0755)

		hash := sha256.Sum256([]byte(originalURL + "::" + shape))
		hexHash := hex.EncodeToString(hash[:])
		fileName := hexHash + ".jpg"
		filePath := filepath.Join(CacheDir, fileName)

		if _, err := os.Stat(filePath); err == nil {
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(filePath)
			return
		}

		// Download and crop synchronously
		err := warmSinglePoster(originalURL, filePath, shape, isJav)
		if err == nil {
			c.Header("Cache-Control", "public, max-age=604800")
			c.File(filePath)
			return
		}

		log.WithError(err).Warnf("failed to crop/resize poster synchronously for %s, redirecting", originalURL)
		c.Redirect(http.StatusTemporaryRedirect, originalURL)
	}
}

func getDMMFallbackURLs(urlStr string) []string {
	parts := strings.Split(urlStr, "/")
	if len(parts) == 0 {
		return nil
	}
	filename := strings.ToLower(parts[len(parts)-1])

	// Parse prefix and number from filename or JAV code
	// e.g., "dldss-492-1.jpg" -> prefix="dldss", num=492
	// e.g., "fns192.jpg" -> prefix="fns", num=192
	// e.g., "1fns00205pl.jpg" -> prefix="fns", num=205
	rx := regexp.MustCompile(`(?:^|[^a-z0-9])([a-z]{2,8})[^a-z0-9]*(\d{3,6})`)
	m := rx.FindStringSubmatch(filename)

	// If it doesn't match the filename, try the whole URL
	if len(m) < 3 {
		m = rx.FindStringSubmatch(strings.ToLower(urlStr))
	}

	if len(m) < 3 {
		return nil
	}

	prefix := m[1]
	numStr := m[2]

	var num int
	fmt.Sscanf(numStr, "%d", &num)

	cids := []string{
		// 1. Direct prefix + number (e.g. jufe399)
		fmt.Sprintf("%s%d", prefix, num),
		// 2. Direct prefix + 3-digit padded number
		fmt.Sprintf("%s%03d", prefix, num),
		// 3. Direct prefix + 5-digit padded number (e.g. fns00192)
		fmt.Sprintf("%s%05d", prefix, num),
		// 4. Prefix 1 + 5-digit padded number (very common for publisher labels e.g. 1fns00192, 1dldss00492)
		fmt.Sprintf("1%s%05d", prefix, num),
		// 5. Prefix 30 + 5-digit padded number (common for VR releases e.g. 30ure00136)
		fmt.Sprintf("30%s%05d", prefix, num),
		// 6. Prefix h_1143 + 5-digit padded number
		fmt.Sprintf("h_1143%s%05d", prefix, num),
		// 7. Prefix h_1143 + 3-digit padded number
		fmt.Sprintf("h_1143%s%03d", prefix, num),
	}

	var urls []string
	seen := make(map[string]bool)
	for _, cid := range cids {
		if seen[cid] {
			continue
		}
		seen[cid] = true
		urls = append(urls,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/movie/adult/%s/%spl.jpg", cid, cid),
			fmt.Sprintf("https://pics.dmm.co.jp/digital/video/%s/%spl.jpg", cid, cid),
		)
	}
	return urls
}

func warmSinglePoster(urlStr, destPath, shape string, isJav bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resp *http.Response
	var err error

	// Create an HTTP client that stops at the first redirect (e.g. 302 to "now_printing" placeholder)
	cl := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if isJav {
		fallbacks := getDMMFallbackURLs(urlStr)
		for _, fallbackURL := range fallbacks {
			var req *http.Request
			req, err = http.NewRequestWithContext(ctx, "GET", fallbackURL, nil)
			if err == nil {
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
				resp, err = cl.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					break
				}
				if resp != nil {
					resp.Body.Close()
					resp = nil
				}
			}
		}
	}

	// Secondary fallback: fetch via DuckDuckGo Image Proxy if DMM fails (handles Cloudflare challenges for custom manual uploads)
	if (resp == nil || resp.StatusCode != http.StatusOK) && isJav {
		ddgURL := fmt.Sprintf("https://external-content.duckduckgo.com/iu/?u=%s", url.QueryEscape(urlStr))
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "GET", ddgURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
			resp, err = cl.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				// Success via DDG proxy!
			} else if resp != nil {
				resp.Body.Close()
				resp = nil
			}
		}
	}

	// Tertiary fallback: direct fetch as last resort
	if resp == nil || resp.StatusCode != http.StatusOK {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", "https://jav.guru/")
		resp, err = cl.Do(req)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download original image, status: %d", resp.StatusCode)
	}

	srcImg, err := imaging.Decode(resp.Body)
	if err != nil {
		return err
	}

	bounds := srcImg.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var processed image.Image
	if isJav && shape == "portrait" {
		if w > h {
			// Determine if it is a full DVD jacket (front cover on right) or a generic landscape backdrop
			urlLower := strings.ToLower(urlStr)
			isJavJacket := strings.Contains(urlLower, "dmm.co.jp") ||
				strings.Contains(urlLower, "jav.guru") ||
				strings.Contains(urlLower, "javmiku") ||
				strings.Contains(urlLower, "javnorth") ||
				strings.Contains(urlLower, "pl.jpg")

			if isJavJacket {
				// If it's a horizontal cover, crop the right 50% to get the front cover (portrait aspect)
				processed = imaging.Crop(srcImg, image.Rect(w/2, 0, w, h))
				processed = imaging.Resize(processed, 500, 0, imaging.Linear)
			} else {
				// Generic scene screenshot / backdrop: crop center to 2:3 aspect ratio
				processed = imaging.Fill(srcImg, 500, 750, imaging.Center, imaging.Linear)
			}
		} else {
			// Already vertical, do not crop! Just resize
			processed = imaging.Resize(srcImg, 500, 0, imaging.Linear)
		}
	} else {
		if shape == "portrait" {
			if w > h {
				// Horizontal image: crop center to 2:3 aspect ratio
				processed = imaging.Fill(srcImg, 500, 750, imaging.Center, imaging.Linear)
			} else {
				processed = imaging.Resize(srcImg, 500, 0, imaging.Linear)
			}
		} else {
			// Landscape (horizontals): do not crop, just resize width to 900 preserving aspect ratio
			processed = imaging.Resize(srcImg, 900, 0, imaging.Linear)
		}
	}

	tempPath := destPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
		_ = os.Remove(tempPath)
	}()

	err = imaging.Encode(out, processed, imaging.JPEG, imaging.JPEGQuality(85))
	if err != nil {
		return err
	}

	_ = out.Close()
	return os.Rename(tempPath, destPath)
}

func PreWarmPosters(scenes []tpdb.TpdbScene, isJav bool) {
	_ = os.MkdirAll(CacheDir, 0755)

	for _, scene := range scenes {
		originalPoster := cleanOriginalImageURL(scene.Poster)
		if originalPoster == "" {
			originalPoster = cleanOriginalImageURL(scene.PosterImage)
		}
		if originalPoster == "" {
			originalPoster = cleanOriginalImageURL(scene.Image)
		}
		if originalPoster == "" {
			continue
		}

		hash := sha256.Sum256([]byte(originalPoster + "::" + "portrait"))
		hexHash := hex.EncodeToString(hash[:])
		fileName := hexHash + ".jpg"
		filePath := filepath.Join(CacheDir, fileName)

		if _, err := os.Stat(filePath); err == nil {
			continue
		}

		log.Infof("Pre-warming adult discovery poster for scene: %s", scene.Title)
		err := warmSinglePoster(originalPoster, filePath, "portrait", isJav)
		if err != nil {
			log.WithError(err).Warnf("failed to pre-warm adult poster for: %s", originalPoster)
		}
	}
}

func PruneAdultPosters(days int) error {
	if days <= 0 {
		days = 7
	}
	thresholdDuration := time.Duration(days) * 24 * time.Hour
	cutoff := time.Now().Add(-thresholdDuration)

	dirEntries, err := os.ReadDir(CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	prunedCount := 0
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(CacheDir, entry.Name())
			err = os.Remove(filePath)
			if err == nil {
				prunedCount++
			}
		}
	}

	if prunedCount > 0 {
		log.Infof("Pruned %d expired adult discovery posters from local disk cache", prunedCount)
	}
	return nil
}

func handleCometProxy(tpdbSvc *tpdb.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		id = strings.TrimPrefix(id, "/")
		id = strings.TrimSuffix(id, ".json")

		// Divert adult streams to self-contained Torrentio resolver
		if strings.HasPrefix(id, "tpdb_jav:") || strings.HasPrefix(id, "stash:") || strings.HasPrefix(id, "tpdb:") {
			handleAdultStreams(c, tpdbSvc, id)
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

		type StreamItem struct {
			Name     string `json:"name"`
			Title    string `json:"title"`
			URL      string `json:"url,omitempty"`
			InfoHash string `json:"infoHash,omitempty"`
			FileIdx  *int   `json:"fileIdx,omitempty"`
		}
		type StreamsResponse struct {
			Streams []StreamItem `json:"streams"`
		}

		var streamsResp StreamsResponse
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

type ProwlarrResultItem struct {
	Title       string `json:"title"`
	InfoHash    string `json:"infoHash,omitempty"`
	MagnetURL   string `json:"magnetUrl,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Seeders     int    `json:"seeders,omitempty"`
	Leechers    int    `json:"leechers,omitempty"`
}

type ProwlarrIndexer struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Enable       bool   `json:"enable"`
	Capabilities struct {
		Categories []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"categories"`
	} `json:"capabilities"`
}

// IndexerWithCatInfo holds an indexer ID, name, and whether it supports adult categories (5000/6000)
type IndexerWithCatInfo struct {
	ID                int
	Name              string
	SupportsAdultCats bool
}

func getProwlarrIndexerIDs(ctx context.Context, names []string) []int {
	results := getProwlarrIndexersWithCatInfo(ctx, names)
	ids := make([]int, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids
}

func getEnabledProwlarrIndexers(ctx context.Context) ([]ProwlarrIndexer, error) {
	globalProwlarrIndexersCache.Lock()
	defer globalProwlarrIndexersCache.Unlock()

	if len(globalProwlarrIndexersCache.indexers) > 0 && time.Since(globalProwlarrIndexersCache.updatedAt) < 10*time.Minute {
		copied := make([]ProwlarrIndexer, len(globalProwlarrIndexersCache.indexers))
		copy(copied, globalProwlarrIndexersCache.indexers)
		return copied, nil
	}

	prowlarrURL := os.Getenv("PROWLARR_URL")
	apiKey := os.Getenv("PROWLARR_API_KEY")
	if prowlarrURL == "" || apiKey == "" {
		return nil, fmt.Errorf("Prowlarr is not configured")
	}
	apiURL := fmt.Sprintf("%s/api/v1/indexer?apikey=%s", prowlarrURL, apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code from Prowlarr indexer: %d", resp.StatusCode)
	}
	var indexers []ProwlarrIndexer
	if err := json.NewDecoder(resp.Body).Decode(&indexers); err != nil {
		return nil, err
	}

	var enabled []ProwlarrIndexer
	for _, ind := range indexers {
		if ind.Enable {
			enabled = append(enabled, ind)
		}
	}

	globalProwlarrIndexersCache.indexers = enabled
	globalProwlarrIndexersCache.updatedAt = time.Now()

	copied := make([]ProwlarrIndexer, len(enabled))
	copy(copied, enabled)
	return copied, nil
}

func getProwlarrIndexersWithCatInfo(ctx context.Context, names []string) []IndexerWithCatInfo {
	enabled, err := getEnabledProwlarrIndexers(ctx)
	if err != nil {
		log.Warnf("Failed to fetch enabled Prowlarr indexers: %v", err)
		return nil
	}

	var results []IndexerWithCatInfo
	for _, ind := range enabled {
		indName := strings.ToLower(ind.Name)
		matched := false
		for _, n := range names {
			target := strings.ToLower(n)
			if indName == target || strings.Contains(indName, target) || strings.Contains(target, indName) {
				matched = true
				break
			}
		}
		if matched {
			supportsAdult := false
			for _, cat := range ind.Capabilities.Categories {
				if cat.ID == 5000 || cat.ID == 6000 || cat.ID == 6010 || cat.ID == 6020 || cat.ID == 6030 {
					supportsAdult = true
					break
				}
			}
			results = append(results, IndexerWithCatInfo{
				ID:                ind.ID,
				Name:              ind.Name,
				SupportsAdultCats: supportsAdult,
			})
		}
	}
	return results
}

func cleanAdultStudioName(name string) string {
	name = strings.TrimSpace(name)
	nameLower := strings.ToLower(name)

	suffixes := []string{" media group", " network", " entertainment", " productions", " studios", " distribution", " group"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(nameLower, suffix) {
			name = name[:len(name)-len(suffix)]
			nameLower = strings.ToLower(name)
		}
	}
	return strings.TrimSpace(name)
}

func searchProwlarrDirect(ctx context.Context, query string, indexerIDs []int, javMode bool, noCategories bool) ([]ProwlarrResultItem, error) {
	prowlarrURL := os.Getenv("PROWLARR_URL")
	apiKey := os.Getenv("PROWLARR_API_KEY")
	if prowlarrURL == "" || apiKey == "" {
		return nil, fmt.Errorf("Prowlarr is not configured")
	}

	var apiURL string
	if javMode || noCategories {
		// JAV content or indexers that don't support adult categories — search without category filter
		apiURL = fmt.Sprintf("%s/api/v1/search?query=%s&apikey=%s&type=search",
			prowlarrURL, url.QueryEscape(query), apiKey)
	} else {
		// Filter strictly to categories=5000 (Adult/Porn) and categories=6000 (XXX) to prevent SFW indexer leaks
		apiURL = fmt.Sprintf("%s/api/v1/search?query=%s&apikey=%s&categories=5000&categories=6000&type=search",
			prowlarrURL, url.QueryEscape(query), apiKey)
	}

	if len(indexerIDs) > 0 {
		var ids []string
		for _, id := range indexerIDs {
			ids = append(ids, fmt.Sprintf("indexerIds=%d", id))
		}
		apiURL += "&" + strings.Join(ids, "&")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prowlarr API returned status: %d", resp.StatusCode)
	}

	var results []ProwlarrResultItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}

type StreamItem struct {
	Name      string `json:"name"`
	Title     string `json:"title"`
	URL       string `json:"url,omitempty"`
	InfoHash  string `json:"infoHash,omitempty"`
	FileIdx   *int   `json:"fileIdx,omitempty"`
	MagnetURL string `json:"magnetUrl,omitempty"`
}

type StreamsResponse struct {
	Streams []StreamItem `json:"streams"`
}

func cleanForComparison(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func filterRelevantResults(res []ProwlarrResultItem, q string, javMode bool, scene *tpdb.TpdbScene, studioName string, parentStudio string) []ProwlarrResultItem {
	var relevant []ProwlarrResultItem
	if javMode {
		queryJav := extractJAVCode(q)
		for _, r := range res {
			titleLow := strings.ToLower(r.Title)
			if queryJav != "" {
				titleJav := extractJAVCode(r.Title)
				if titleJav == queryJav {
					relevant = append(relevant, r)
				} else {
					titleLowClean := strings.NewReplacer("-", "", "_", "", " ", "").Replace(titleLow)
					if strings.Contains(titleLowClean, queryJav) {
						relevant = append(relevant, r)
					}
				}
			} else {
				queryLow := strings.ToLower(q)
				words := strings.Fields(queryLow)
				matched := false
				for _, w := range words {
					if len(w) >= 3 && strings.Contains(titleLow, w) {
						matched = true
						break
					}
				}
				if matched {
					relevant = append(relevant, r)
				}
			}
		}
	} else {
		isDateQuery := false
		var dateMM, dateDD string
		parts := strings.Fields(q)
		if len(parts) >= 3 {
			last := parts[len(parts)-1]
			prev := parts[len(parts)-2]
			if len(last) == 2 && len(prev) == 2 {
				if _, e1 := strconv.Atoi(last); e1 == nil {
					if _, e2 := strconv.Atoi(prev); e2 == nil {
						isDateQuery = true
						dateMM = prev
						dateDD = last
					}
				}
			}
		}
		for _, r := range res {
			titleLow := strings.ToLower(r.Title)

			// Enforce SFW leak prevention filter for general-purpose indexers
			isGeneral := false
			indName := strings.ToLower(r.Indexer)
			if strings.Contains(indName, "zilean") || strings.Contains(indName, "pirate") || strings.Contains(indName, "knaben") {
				isGeneral = true
			}
			if isGeneral {
				titleLowClean := cleanForComparison(r.Title)
				studioMatched := false
				if studioName != "" && strings.Contains(titleLowClean, cleanForComparison(studioName)) {
					studioMatched = true
				}
				if parentStudio != "" && strings.Contains(titleLowClean, cleanForComparison(parentStudio)) {
					studioMatched = true
				}
				perfMatched := false
				for _, perf := range scene.Performers {
					pName := cleanForComparison(perf.Name)
					if pName != "" && strings.Contains(titleLowClean, pName) {
						perfMatched = true
						break
					}
				}
				if !studioMatched && !perfMatched {
					continue
				}
			}

			if isDateQuery {
				if strings.Contains(titleLow, dateMM+" "+dateDD) ||
					strings.Contains(titleLow, dateMM+"."+dateDD) ||
					strings.Contains(titleLow, dateMM+"-"+dateDD) ||
					strings.Contains(titleLow, dateMM+dateDD) ||
					strings.Contains(titleLow, dateDD+"."+dateMM) ||
					strings.Contains(titleLow, dateDD+"-"+dateMM) {
					relevant = append(relevant, r)
				}
			} else {
				stopWords := map[string]bool{
					"a": true, "an": true, "the": true, "and": true, "or": true, "in": true,
					"of": true, "to": true, "is": true, "at": true, "by": true, "for": true,
					"on": true, "me": true, "my": true, "her": true, "his": true, "with": true,
					"from": true, "it": true, "was": true, "not": true, "are": true, "be": true,
				}
				queryLow := strings.ToLower(q)
				sigWords := []string{}
				for _, w := range strings.Fields(queryLow) {
					if len(w) >= 3 && !stopWords[w] {
						sigWords = append(sigWords, w)
					}
				}
				if len(sigWords) == 0 {
					for _, w := range strings.Fields(queryLow) {
						if len(w) >= 2 && !stopWords[w] {
							sigWords = append(sigWords, w)
						}
					}
				}
				matches := 0
				for _, w := range sigWords {
					if strings.Contains(titleLow, w) {
						matches++
					}
				}
				minMatch := 2
				if len(sigWords) <= 2 {
					minMatch = 1
				} else if len(sigWords) <= 4 {
					minMatch = 2
				} else {
					minMatch = 3
				}
				if matches >= minMatch && len(sigWords) > 0 {
					relevant = append(relevant, r)
				}
			}
		}
	}
	return relevant
}

func handleAdultStreams(c *gin.Context, tpdbSvc *tpdb.Service, id string) {
	exhaustive := c.Query("exhaustive") == "true"

	// 1. Redis cache lookup (segregated by exhaustive parameter)
	redisKey := fmt.Sprintf("adult_streams_cache:%s:ex:%v", id, exhaustive)
	if globalRedisClient != nil {
		cachedVal, err := globalRedisClient.Get().Get(c.Request.Context(), redisKey).Result()
		if err == nil && cachedVal != "" {
			var cachedResp StreamsResponse
			if err := json.Unmarshal([]byte(cachedVal), &cachedResp); err == nil {
				log.Infof("Returning cached streams for adult ID: %s (exhaustive: %v)", id, exhaustive)
				c.JSON(http.StatusOK, cachedResp)
				return
			}
		}
	}

	parts := strings.SplitN(id, ":", 2)
	if len(parts) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid adult scene ID format"})
		return
	}
	prefix := parts[0]
	sceneID := parts[1]

	var scene *tpdb.TpdbScene
	var err error

	// 2. Resolve metadata from origin databases
	if prefix == "tpdb_jav" {
		if strings.HasPrefix(sceneID, "fallback_") {
			code := strings.TrimPrefix(sceneID, "fallback_")
			redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)
			if globalRedisClient != nil {
				val, _ := globalRedisClient.Get().Get(c.Request.Context(), redisKey).Result()
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
		scene, err = fetchStashDBSingleScene(c.Request.Context(), sceneID)
	} else {
		scene, err = tpdbSvc.FetchSceneByID(c.Request.Context(), sceneID)
	}

	if err != nil || scene == nil {
		log.Errorf("Failed to resolve metadata for adult ID: %s, error: %v", id, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "metadata not found"})
		return
	}

	// 3. Formulate highly clean search terms
	var primaryQueries []string
	var fallbackQueries []string
	var studioName string
	var parentStudio string

	if prefix == "tpdb_jav" {
		code := ""
		m := javCodeRx.FindStringSubmatch(scene.Title)
		if len(m) != 3 {
			m = javCodeRx.FindStringSubmatch(scene.ID)
		}
		if len(m) == 3 {
			letters := strings.ToUpper(m[1])
			numbers := m[2]
			code = letters + "-" + numbers
			primaryQueries = append(primaryQueries, code)
			primaryQueries = append(primaryQueries, letters+numbers)
		} else {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(scene.Title), " "))
		}
	} else {
		if scene.Site != nil {
			studioName = cleanAdultStudioName(scene.Site.Name)
		}

		var yy, yyShort, mm, dd string
		if len(scene.Date) >= 10 {
			dateParts := strings.Split(scene.Date, "-")
			if len(dateParts) == 3 {
				yy = dateParts[0]
				yyShort = dateParts[0][2:]
				mm = dateParts[1]
				dd = dateParts[2]
			}
		}

		cleanedTitle := scene.Title
		cleanedTitle = strings.NewReplacer(":", " ", "&", " ", "'", "", "\"", "", "-", " ", "(", "", ")", "").Replace(cleanedTitle)
		words := strings.Fields(cleanedTitle)
		if len(words) > 10 {
			words = words[:10]
		}
		titleFull := strings.Join(words, " ")

		// Resolve parent studio
		if scene.Site != nil && scene.Site.Parent != "" {
			parentStudio = cleanAdultStudioName(scene.Site.Parent)
		} else if studioWords := strings.Fields(studioName); len(studioWords) > 1 {
			firstWord := studioWords[0]
			genericWords := map[string]bool{"the": true, "big": true, "hot": true, "new": true, "my": true, "all": true, "sexy": true}
			if !genericWords[strings.ToLower(firstWord)] && len(firstWord) >= 4 {
				parentStudio = firstWord
			}
		}

		studioQueryName := strings.ReplaceAll(studioName, " ", "")
		parentStudioQueryName := strings.ReplaceAll(parentStudio, " ", "")

		// --- 1. Primary Queries (Tier 1) ---
		// Studio + Date (2-digit year)
		if studioQueryName != "" && yyShort != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", studioQueryName, yyShort, mm, dd)), " "))
		}
		// Studio + Title
		if studioQueryName != "" && titleFull != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", studioQueryName, titleFull)), " "))
		}
		// Studio + Date (4-digit year)
		if studioQueryName != "" && yy != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", studioQueryName, yy, mm, dd)), " "))
		}
		// Performer + Date (2-digit year) - DO NOT add performer yyyy mm dd
		if yyShort != "" && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", perfName, yyShort, mm, dd)), " "))
				}
			}
		}

		// Parent Studio fallback for primary queries
		if parentStudioQueryName != "" && !strings.EqualFold(parentStudioQueryName, studioQueryName) {
			if yyShort != "" {
				primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", parentStudioQueryName, yyShort, mm, dd)), " "))
			}
			if titleFull != "" {
				primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", parentStudioQueryName, titleFull)), " "))
			}
			if yy != "" {
				primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", parentStudioQueryName, yy, mm, dd)), " "))
			}
		}

		// --- 2. Fallback Queries (Tier 2 only) ---
		// Title-only
		if titleFull != "" {
			fallbackQueries = append(fallbackQueries, titleFull)
		}
		// Studio + Performer
		if studioQueryName != "" && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					fallbackQueries = append(fallbackQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", studioQueryName, perfName)), " "))
				}
			}
		}
		// Parent Studio + Performer
		if parentStudioQueryName != "" && !strings.EqualFold(parentStudioQueryName, studioQueryName) && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					fallbackQueries = append(fallbackQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", parentStudioQueryName, perfName)), " "))
				}
			}
		}
	}

	javMode := prefix == "tpdb_jav"

	// Split preferred indexers based on JAV mode into Primary and Secondary categories
	var primaryNames []string
	var secondaryNames []string
	if javMode {
		primaryNames = []string{"sukebei.nyaa.si", "Knaben"}
		secondaryNames = []string{"Tokyo Toshokan", "nekoBT", "Nyaa.si", "The Pirate Bay", "Zilean DMM", "Zilean"}
	} else {
		primaryNames = []string{"The Pirate Bay", "Knaben"}
		secondaryNames = []string{"sukebei.nyaa.si", "TorrentGalaxyClone", "XXXClub", "xxxtor", "Zilean DMM", "Zilean"}
	}

	primaryIndexers := getProwlarrIndexersWithCatInfo(c.Request.Context(), primaryNames)
	secondaryIndexers := getProwlarrIndexersWithCatInfo(c.Request.Context(), secondaryNames)

	var torrents []ProwlarrResultItem
	var mu sync.Mutex

	// Tier 1 Search: Query primary indexers in parallel across primary queries ONLY
	if len(primaryIndexers) > 0 {
		var wg sync.WaitGroup
		for _, q := range primaryQueries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}
			for _, ind := range primaryIndexers {
				wg.Add(1)
				go func(query string, indexer IndexerWithCatInfo) {
					defer wg.Done()
					reqCtx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
					defer cancel()
					res, err := searchProwlarrDirect(reqCtx, query, []int{indexer.ID}, javMode, !indexer.SupportsAdultCats)
					if err == nil && len(res) > 0 {
						filtered := filterRelevantResults(res, query, javMode, scene, studioName, parentStudio)
						if len(filtered) > 0 {
							mu.Lock()
							torrents = append(torrents, filtered...)
							mu.Unlock()
						}
					}
				}(q, ind)
			}
		}
		wg.Wait()
	}

	// Tier 2 Search: Query secondary indexers sequentially if Tier 1 returned 0 results, or if exhaustive mode is requested
	if exhaustive || len(torrents) == 0 {
		zileanIDs := []int{}
		var mainSecondaryIndexers []IndexerWithCatInfo
		for _, ind := range secondaryIndexers {
			if strings.Contains(strings.ToLower(ind.Name), "zilean") {
				zileanIDs = append(zileanIDs, ind.ID)
			} else {
				mainSecondaryIndexers = append(mainSecondaryIndexers, ind)
			}
		}

		// Combined queries for Tier 2 fallback: primary queries first, then fallback queries
		var allQueries []string
		allQueries = append(allQueries, primaryQueries...)
		allQueries = append(allQueries, fallbackQueries...)

		for _, q := range allQueries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}

			// 1. Zilean DMM cache check (local, fast)
			if len(zileanIDs) > 0 {
				log.Infof("Searching Zilean DMM cache for query: %q", q)
				reqCtx, cancel := context.WithTimeout(c.Request.Context(), 1500*time.Millisecond)
				res, err := searchProwlarrDirect(reqCtx, q, zileanIDs, true, true)
				cancel()
				if err == nil && len(res) > 0 {
					filtered := filterRelevantResults(res, q, javMode, scene, studioName, parentStudio)
					if len(filtered) > 0 {
						torrents = append(torrents, filtered...)
						log.Infof("Found %d relevant torrents in Zilean cache for query %q", len(filtered), q)
						if !exhaustive {
							break
						}
					}
				}
			}

			// 2. Live indexers search (sequential query blocks, parallelized within each block)
			indexersToQuery := mainSecondaryIndexers
			isFallback := false
			for _, fq := range fallbackQueries {
				if fq == q {
					isFallback = true
					break
				}
			}
			if isFallback {
				indexersToQuery = append(indexersToQuery, primaryIndexers...)
			}

			if len(indexersToQuery) > 0 && (exhaustive || len(torrents) == 0) {
				log.Infof("Searching indexers sequentially for query: %q", q)
				var qWg sync.WaitGroup
				var qMu sync.Mutex
				var qResults []ProwlarrResultItem

				for _, ind := range indexersToQuery {
					qWg.Add(1)
					go func(indexer IndexerWithCatInfo) {
						defer qWg.Done()
						reqCtx, cancel := context.WithTimeout(c.Request.Context(), 4*time.Second)
						defer cancel()
						res, err := searchProwlarrDirect(reqCtx, q, []int{indexer.ID}, javMode, !indexer.SupportsAdultCats)
						if err == nil && len(res) > 0 {
							filtered := filterRelevantResults(res, q, javMode, scene, studioName, parentStudio)
							if len(filtered) > 0 {
								qMu.Lock()
								qResults = append(qResults, filtered...)
								qMu.Unlock()
							}
						}
					}(ind)
				}
				qWg.Wait()

				if len(qResults) > 0 {
					torrents = append(torrents, qResults...)
					if !exhaustive {
						break
					}
				}
			}
		}
	}

	// 4.5 Resolve infohashes for items that only have a DownloadURL (in parallel)
	{
		var wg sync.WaitGroup
		for i := range torrents {
			if parsed := tryParseInfoHash(torrents[i].InfoHash); parsed != "" {
				torrents[i].InfoHash = parsed
			} else if parsed := tryParseInfoHash(torrents[i].MagnetURL); parsed != "" {
				torrents[i].InfoHash = parsed
			} else if parsed := tryParseInfoHash(torrents[i].DownloadURL); parsed != "" {
				torrents[i].InfoHash = parsed
			}

			if torrents[i].InfoHash == "" && torrents[i].DownloadURL != "" && !strings.HasPrefix(strings.ToLower(torrents[i].DownloadURL), "magnet:") {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					resolvedHash, err := resolveInfoHashFromTorrent(c.Request.Context(), torrents[idx].DownloadURL)
					if err == nil && resolvedHash != "" {
						torrents[idx].InfoHash = resolvedHash
						log.Infof("Successfully resolved infohash %s from DownloadURL for: %s", resolvedHash, torrents[idx].Title)
					} else if err != nil {
						log.Warnf("Failed to resolve infohash from DownloadURL %s: %v", torrents[idx].DownloadURL, err)
					}
				}(i)
			}
		}
		wg.Wait()
	}

	// 5. Filter siterips and deduplicate by InfoHash (keeping longest title)
	var filteredTorrents []ProwlarrResultItem
	for _, item := range torrents {
		if siteripRx.MatchString(item.Title) {
			log.Infof("Skipping siterip/pack torrent: %s", item.Title)
			continue
		}
		filteredTorrents = append(filteredTorrents, item)
	}

	if len(filteredTorrents) == 0 {
		log.Warnf("handleAdultStreams: 0 results after all queries and filtering for id=%s", id)
	}

	bestTorrents := make(map[string]ProwlarrResultItem)
	var orderedHashes []string
	for _, item := range filteredTorrents {
		hash := tryParseInfoHash(item.InfoHash)
		if hash == "" {
			hash = tryParseInfoHash(item.MagnetURL)
		}
		if hash == "" {
			hash = tryParseInfoHash(item.DownloadURL)
		}
		if hash == "" {
			continue
		}
		existing, exists := bestTorrents[hash]
		if !exists {
			bestTorrents[hash] = item
			orderedHashes = append(orderedHashes, hash)
		} else if len(item.Title) > len(existing.Title) {
			bestTorrents[hash] = item
		}
	}

	streamItems := []StreamItem{}
	for _, hash := range orderedHashes {
		item := bestTorrents[hash]

		// Detect resolution quality (default to 1080p for better advanced filtering in StreamModal)
		resolution := "1080p"
		titleLower := strings.ToLower(item.Title)
		if strings.Contains(titleLower, "2160p") || strings.Contains(titleLower, "4k") {
			resolution = "4K"
		} else if strings.Contains(titleLower, "1080p") || strings.Contains(titleLower, "fhd") {
			resolution = "1080p"
		} else if strings.Contains(titleLower, "720p") {
			resolution = "720p"
		} else if strings.Contains(titleLower, "sd") || strings.Contains(titleLower, "480p") || strings.Contains(titleLower, "360p") {
			resolution = "SD"
		}

		sizeGB := float64(item.Size) / (1024 * 1024 * 1024)
		indexerLabel := item.Indexer
		if indexerLabel == "" {
			indexerLabel = "Prowlarr"
		}

		titleString := fmt.Sprintf("%s\n💾 %.2f GB | 👥 Seeders: %d | ⚙️ %s", item.Title, sizeGB, item.Seeders, indexerLabel)

		trackers := []string{
			"udp://tracker.opentrackr.org:1337/announce",
			"udp://open.stealth.si:80/announce",
			"udp://explodie.org:6969/announce",
			"udp://tracker.tiny-vps.com:6969/announce",
			"udp://open.demonii.si:1337/announce",
			"udp://tracker.torrent.eu.org:451/announce",
		}

		magnet := item.MagnetURL
		// ALWAYS override Prowlarr HTTP proxy download links with a direct magnet link to bypass Prowlarr downtime/timeouts on click
		if magnet == "" || strings.HasPrefix(strings.ToLower(magnet), "http://") || strings.HasPrefix(strings.ToLower(magnet), "https://") {
			var trs []string
			for _, tr := range trackers {
				trs = append(trs, "&tr="+url.QueryEscape(tr))
			}
			magnet = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s%s", hash, url.QueryEscape(item.Title), strings.Join(trs, ""))
		}

		var nameParts []string
		nameParts = append(nameParts, "⚡ Octor")
		nameParts = append(nameParts, resolution)
		nameParts = append(nameParts, extractAdultExtraNameInfo(item.Title)...)

		streamItems = append(streamItems, StreamItem{
			Name:      strings.Join(nameParts, "\n"),
			Title:     titleString,
			InfoHash:  hash,
			FileIdx:   nil,
			MagnetURL: magnet,
		})
	}

	sort.Slice(streamItems, func(i, j int) bool {
		getSeeders := func(s string) int {
			idx := strings.LastIndex(s, "Seeders: ")
			if idx != -1 {
				val := s[idx+len("Seeders: "):]
				end := 0
				for end < len(val) && val[end] >= '0' && val[end] <= '9' {
					end++
				}
				if end > 0 {
					if num, err := strconv.Atoi(val[:end]); err == nil {
						return num
					}
				}
			}
			return 0
		}
		return getSeeders(streamItems[i].Title) > getSeeders(streamItems[j].Title)
	})

	resp := StreamsResponse{
		Streams: streamItems,
	}

	if globalRedisClient != nil && len(streamItems) > 0 {
		if respBytes, err := json.Marshal(resp); err == nil {
			_ = globalRedisClient.Get().Set(c.Request.Context(), redisKey, string(respBytes), 15*time.Minute).Err()
		}
	}

	c.JSON(http.StatusOK, resp)
}

func resolveInfoHashFromTorrent(ctx context.Context, downloadURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download torrent returned status: %d", resp.StatusCode)
	}

	mi, err := metainfo.Load(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.ToLower(mi.HashInfoBytes().HexString()), nil
}

func tryParseInfoHash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 40 || len(s) == 32 || len(s) == 64 {
		isClean := true
		for _, r := range s {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				isClean = false
				break
			}
		}
		if isClean {
			return strings.ToLower(s)
		}
	}
	idx := strings.Index(s, "xt=urn:btih:")
	if idx != -1 {
		hashPart := s[idx+len("xt=urn:btih:"):]
		ampIdx := strings.Index(hashPart, "&")
		if ampIdx != -1 {
			hashPart = hashPart[:ampIdx]
		}
		hashPart = strings.ToLower(strings.TrimSpace(hashPart))
		if len(hashPart) == 40 || len(hashPart) == 32 || len(hashPart) == 64 {
			return hashPart
		}
	}
	return ""
}

func extractJAVCode(s string) string {
	m := javCodeRx.FindStringSubmatch(s)
	if len(m) == 3 {
		return strings.ToLower(m[1] + m[2])
	}
	return ""
}

var (
	adultCodecRx     = regexp.MustCompile(`(?i)\b(hevc|h265|x265|h264|x264|avc|av1|10bit|8bit)\b`)
	adultSourceRx    = regexp.MustCompile(`(?i)\b(web-dl|webdl|webrip|web|bluray|brrip|bdrip|hdtv|dvdrip|siterip|site-rip)\b`)
	adultUploaderRx1 = regexp.MustCompile(`(?i)\[([a-zA-Z0-9_.-]{3,15})\]\s*$`)
	adultUploaderRx2 = regexp.MustCompile(`(?i)-([a-zA-Z0-9_.-]{3,15})$`)
)

func extractAdultExtraNameInfo(title string) []string {
	var tags []string

	// 1. Codec extraction
	if m := adultCodecRx.FindString(title); m != "" {
		mLower := strings.ToLower(m)
		if strings.Contains(mLower, "265") || strings.Contains(mLower, "hevc") {
			tags = append(tags, "HEVC")
		} else if strings.Contains(mLower, "264") || strings.Contains(mLower, "avc") {
			tags = append(tags, "AVC")
		} else if strings.Contains(mLower, "av1") {
			tags = append(tags, "AV1")
		} else {
			tags = append(tags, m)
		}
	}

	// 2. Source extraction
	if m := adultSourceRx.FindString(title); m != "" {
		mLower := strings.ToLower(m)
		if strings.Contains(mLower, "web-dl") || strings.Contains(mLower, "webdl") {
			tags = append(tags, "WEB-DL")
		} else if strings.Contains(mLower, "webrip") {
			tags = append(tags, "WEBRip")
		} else if strings.Contains(mLower, "bluray") || strings.Contains(mLower, "bdrip") || strings.Contains(mLower, "brrip") {
			tags = append(tags, "BluRay")
		} else if strings.Contains(mLower, "siterip") || strings.Contains(mLower, "site-rip") {
			tags = append(tags, "SiteRip")
		} else if strings.Contains(mLower, "hdtv") {
			tags = append(tags, "HDTV")
		} else if strings.Contains(mLower, "dvdrip") {
			tags = append(tags, "DVDRip")
		}
	}

	// 3. Clean extensions/junk from the end of title before uploader matching
	cleanTitle := regexp.MustCompile(`(?i)\.(mp4|mkv|avi|wmv|flv)\s*$`).ReplaceAllString(title, "")
	cleanTitle = strings.TrimSpace(cleanTitle)

	// 4. Uploader / Release Group extraction
	isIgnoredUploader := func(u string) bool {
		u = strings.ToLower(strings.TrimSpace(u))
		if len(u) < 2 {
			return true
		}

		// Heuristic: ignore tags that are mostly numbers (resolutions, dates, etc.)
		digits := 0
		letters := 0
		for _, r := range u {
			if r >= '0' && r <= '9' {
				digits++
			} else if r >= 'a' && r <= 'z' {
				letters++
			}
		}
		if digits > letters || letters == 0 {
			return true
		}

		ignored := map[string]bool{
			"mp4": true, "mkv": true, "avi": true, "wmv": true, "flv": true,
			"hevc": true, "h265": true, "x265": true, "h264": true, "x264": true,
			"avc": true, "av1": true, "10bit": true, "8bit": true,
			"web-dl": true, "webdl": true, "webrip": true, "web": true,
			"bluray": true, "brrip": true, "bdrip": true, "hdtv": true,
			"dvdrip": true, "siterip": true, "site-rip": true,
			"xxx": true, "uncensored": true, "censored": true, "jav": true,
			"sub": true, "eng": true, "raw": true, "hd": true, "fhd": true,
			"qhd": true, "uhd": true, "sd": true,
		}
		if ignored[u] {
			return true
		}

		if javCodeRx.MatchString(u) {
			return true
		}

		return false
	}

	// Extract all bracketed tags anywhere in the title
	bracketRx := regexp.MustCompile(`(?i)\[([a-zA-Z0-9_.-]{3,15})\]`)
	bracketMatches := bracketRx.FindAllStringSubmatch(cleanTitle, -1)
	for _, m := range bracketMatches {
		if len(m) > 1 {
			u := m[1]
			if !isIgnoredUploader(u) {
				tags = append(tags, u)
			}
		}
	}

	// Also extract hyphenated suffix (e.g. -KTR) at the end of the title
	if m := adultUploaderRx2.FindStringSubmatch(cleanTitle); len(m) > 1 {
		u := m[1]
		if !isIgnoredUploader(u) {
			tags = append(tags, u)
		}
	}

	// Deduplicate tags
	uniqueTags := []string{}
	seenTags := make(map[string]bool)
	for _, t := range tags {
		tLower := strings.ToLower(t)
		if !seenTags[tLower] {
			seenTags[tLower] = true
			uniqueTags = append(uniqueTags, t)
		}
	}

	return uniqueTags
}
