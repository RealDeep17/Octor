package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/tpdb"
)

type Service struct {
	client      *http.Client
	redis       *cs.RedisClient
	prowlarrURL string
	apiKey      string
	cache       struct {
		sync.Mutex
		indexers  []ProwlarrIndexer
		updatedAt time.Time
	}
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

type IndexerWithCatInfo struct {
	ID                int
	Name              string
	SupportsAdultCats bool
}

var (
	javCodeRx        = regexp.MustCompile(`(?i)\b([a-zA-Z]{2,10})[\s\-_]?(\d{3,8})\b`)
	siteripRx        = regexp.MustCompile(`(?i)\b(siterips?|site-rips?|megapacks?|mega-packs?|packs?)\b`)
	adultCodecRx     = regexp.MustCompile(`(?i)\b(hevc|h265|x265|h264|x264|avc|av1|10bit|8bit)\b`)
	adultSourceRx    = regexp.MustCompile(`(?i)\b(web-dl|webdl|webrip|web|bluray|brrip|bdrip|hdtv|dvdrip|siterip|site-rip)\b`)
	adultUploaderRx1 = regexp.MustCompile(`(?i)\[([a-zA-Z0-9_.-]{3,15})\]\s*$`)
	adultUploaderRx2 = regexp.MustCompile(`(?i)-([a-zA-Z0-9_.-]{3,15})$`)
)

func New(client *http.Client, redis *cs.RedisClient, prowlarrURL string, apiKey string) *Service {
	return &Service{
		client:      client,
		redis:       redis,
		prowlarrURL: prowlarrURL,
		apiKey:      apiKey,
	}
}

func (s *Service) getEnabledProwlarrIndexers(ctx context.Context) ([]ProwlarrIndexer, error) {
	s.cache.Lock()
	defer s.cache.Unlock()

	if len(s.cache.indexers) > 0 && time.Since(s.cache.updatedAt) < 10*time.Minute {
		copied := make([]ProwlarrIndexer, len(s.cache.indexers))
		copy(copied, s.cache.indexers)
		return copied, nil
	}

	if s.prowlarrURL == "" || s.apiKey == "" {
		return nil, fmt.Errorf("Prowlarr is not configured")
	}
	apiURL := fmt.Sprintf("%s/api/v1/indexer?apikey=%s", s.prowlarrURL, s.apiKey)
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

	s.cache.indexers = enabled
	s.cache.updatedAt = time.Now()

	copied := make([]ProwlarrIndexer, len(enabled))
	copy(copied, enabled)
	return copied, nil
}

func (s *Service) GetProwlarrIndexersWithCatInfo(ctx context.Context, names []string) []IndexerWithCatInfo {
	enabled, err := s.getEnabledProwlarrIndexers(ctx)
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

func (s *Service) searchProwlarrDirect(ctx context.Context, query string, indexerIDs []int, javMode bool, noCategories bool) ([]ProwlarrResultItem, error) {
	if s.prowlarrURL == "" || s.apiKey == "" {
		return nil, fmt.Errorf("Prowlarr is not configured")
	}

	var apiURL string
	if javMode || noCategories {
		apiURL = fmt.Sprintf("%s/api/v1/search?query=%s&apikey=%s&type=search",
			s.prowlarrURL, url.QueryEscape(query), s.apiKey)
	} else {
		apiURL = fmt.Sprintf("%s/api/v1/search?query=%s&apikey=%s&categories=5000&categories=6000&type=search",
			s.prowlarrURL, url.QueryEscape(query), s.apiKey)
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

func cleanForComparison(str string) string {
	str = strings.ToLower(str)
	str = strings.ReplaceAll(str, " ", "")
	str = strings.ReplaceAll(str, ".", "")
	str = strings.ReplaceAll(str, "-", "")
	str = strings.ReplaceAll(str, "_", "")
	return str
}

func extractJAVCode(s string) string {
	m := javCodeRx.FindStringSubmatch(s)
	if len(m) == 3 {
		return strings.ToLower(m[1] + m[2])
	}
	return ""
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
				if scene != nil {
					for _, perf := range scene.Performers {
						pName := cleanForComparison(perf.Name)
						if pName != "" && strings.Contains(titleLowClean, pName) {
							perfMatched = true
							break
						}
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

func extractAdultExtraNameInfo(title string) []string {
	var tags []string

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

	cleanTitle := regexp.MustCompile(`(?i)\.(mp4|mkv|avi|wmv|flv)\s*$`).ReplaceAllString(title, "")
	cleanTitle = strings.TrimSpace(cleanTitle)

	isIgnoredUploader := func(u string) bool {
		u = strings.ToLower(strings.TrimSpace(u))
		if len(u) < 2 {
			return true
		}

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

	if m := adultUploaderRx2.FindStringSubmatch(cleanTitle); len(m) > 1 {
		u := m[1]
		if !isIgnoredUploader(u) {
			tags = append(tags, u)
		}
	}

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

func (s *Service) GetProwlarrIndexersWithNames(ctx context.Context, names []string) []IndexerWithCatInfo {
	return s.GetProwlarrIndexersWithCatInfo(ctx, names)
}

func (s *Service) SearchAdultStreams(ctx context.Context, id string, scene *tpdb.TpdbScene, exhaustive bool, javMode bool) ([]StreamItem, error) {
	// Cache Key check
	redisKey := fmt.Sprintf("adult_streams_cache:%s:ex:%v", id, exhaustive)
	if s.redis != nil {
		cachedVal, err := s.redis.Get().Get(ctx, redisKey).Result()
		if err == nil && cachedVal != "" {
			var cachedResp StreamsResponse
			if err := json.Unmarshal([]byte(cachedVal), &cachedResp); err == nil {
				log.Infof("Returning cached streams for adult ID: %s (exhaustive: %v)", id, exhaustive)
				return cachedResp.Streams, nil
			}
		}
	}

	var primaryQueries []string
	var fallbackQueries []string
	var studioName string
	var parentStudio string

	if javMode {
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

		if studioQueryName != "" && yyShort != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", studioQueryName, yyShort, mm, dd)), " "))
		}
		if studioQueryName != "" && titleFull != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", studioQueryName, titleFull)), " "))
		}
		if studioQueryName != "" && yy != "" {
			primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", studioQueryName, yy, mm, dd)), " "))
		}
		if yyShort != "" && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					primaryQueries = append(primaryQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s %s %s", perfName, yyShort, mm, dd)), " "))
				}
			}
		}

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

		if titleFull != "" {
			fallbackQueries = append(fallbackQueries, titleFull)
		}
		if studioQueryName != "" && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					fallbackQueries = append(fallbackQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", studioQueryName, perfName)), " "))
				}
			}
		}
		if parentStudioQueryName != "" && !strings.EqualFold(parentStudioQueryName, studioQueryName) && len(scene.Performers) > 0 {
			for _, perf := range scene.Performers {
				perfName := strings.TrimSpace(perf.Name)
				if perfName != "" {
					fallbackQueries = append(fallbackQueries, strings.Join(strings.Fields(fmt.Sprintf("%s %s", parentStudioQueryName, perfName)), " "))
				}
			}
		}
	}

	var primaryNames []string
	var secondaryNames []string
	if javMode {
		primaryNames = []string{"sukebei.nyaa.si", "Knaben"}
		secondaryNames = []string{"Tokyo Toshokan", "nekoBT", "Nyaa.si", "The Pirate Bay", "Zilean DMM", "Zilean"}
	} else {
		primaryNames = []string{"The Pirate Bay", "Knaben"}
		secondaryNames = []string{"sukebei.nyaa.si", "TorrentGalaxyClone", "XXXClub", "xxxtor", "Zilean DMM", "Zilean"}
	}

	primaryIndexers := s.GetProwlarrIndexersWithCatInfo(ctx, primaryNames)
	secondaryIndexers := s.GetProwlarrIndexersWithCatInfo(ctx, secondaryNames)

	var torrents []ProwlarrResultItem
	var mu sync.Mutex

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
					reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					res, err := s.searchProwlarrDirect(reqCtx, query, []int{indexer.ID}, javMode, !indexer.SupportsAdultCats)
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

		var allQueries []string
		allQueries = append(allQueries, primaryQueries...)
		allQueries = append(allQueries, fallbackQueries...)

		for _, q := range allQueries {
			q = strings.TrimSpace(q)
			if q == "" {
				continue
			}

			if len(zileanIDs) > 0 {
				log.Infof("Searching Zilean DMM cache for query: %q", q)
				reqCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				res, err := s.searchProwlarrDirect(reqCtx, q, zileanIDs, true, true)
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
						reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
						defer cancel()
						res, err := s.searchProwlarrDirect(reqCtx, q, []int{indexer.ID}, javMode, !indexer.SupportsAdultCats)
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

	// Resolve infohashes in parallel
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
					resolvedHash, err := resolveInfoHashFromTorrent(ctx, torrents[idx].DownloadURL)
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

	var filteredTorrents []ProwlarrResultItem
	for _, item := range torrents {
		if siteripRx.MatchString(item.Title) {
			log.Infof("Skipping siterip/pack torrent: %s", item.Title)
			continue
		}
		filteredTorrents = append(filteredTorrents, item)
	}

	if len(filteredTorrents) == 0 {
		log.Warnf("SearchAdultStreams: 0 results after all queries and filtering for id=%s", id)
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
		getSeeders := func(st string) int {
			idx := strings.LastIndex(st, "Seeders: ")
			if idx != -1 {
				val := st[idx+len("Seeders: "):]
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

	// Cache in redis for 15 mins
	if s.redis != nil && len(streamItems) > 0 {
		resp := StreamsResponse{
			Streams: streamItems,
		}
		if respBytes, err := json.Marshal(resp); err == nil {
			_ = s.redis.Get().Set(ctx, redisKey, string(respBytes), 15*time.Minute).Err()
		}
	}

	return streamItems, nil
}
