package javguru

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/tpdb"
)

type Service struct {
	client *http.Client
	redis  *cs.RedisClient
}

func New(cl *http.Client, redis *cs.RedisClient) *Service {
	return &Service{
		client: cl,
		redis:  redis,
	}
}

type WppItem struct {
	ID    int `json:"id"`
	Title struct {
		Rendered string `json:"rendered"`
	} `json:"title"`
	Date string `json:"date"`
	Link string `json:"link"`
}

func cleanImageURL(imgURL string) string {
	if imgURL == "" {
		return ""
	}
	// 1. Remove "/thumbs/" from the path
	cleanPoster := strings.ReplaceAll(imgURL, "/thumbs/", "/")
	// 2. Remove the trailing "-WxH" suffix (e.g. "-211x300", "-550x374")
	wpThumbnailRegex := regexp.MustCompile(`-\d+x\d+(\.[a-zA-Z0-9]+)$`)
	originalPoster := wpThumbnailRegex.ReplaceAllString(cleanPoster, "$1")
	return originalPoster
}

func matchJavCodeFuzzy(cleanedURL, cleanCode string) bool {
	if strings.Contains(cleanedURL, cleanCode) {
		return true
	}

	// Parse letters and numbers (e.g. "fns205" -> letters="fns", numbers="205")
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

func extractDate(imgURL, postURL string) string {
	rx := regexp.MustCompile(`/(20\d{2})/(\d{2})/`)
	if m := rx.FindStringSubmatch(imgURL); len(m) == 3 {
		return fmt.Sprintf("%s-%s-01", m[1], m[2])
	}
	if m := rx.FindStringSubmatch(postURL); len(m) == 3 {
		return fmt.Sprintf("%s-%s-01", m[1], m[2])
	}
	return time.Now().Format("2006-01-02")
}

func (s *Service) scrapeCatalog(ctx context.Context, searchURL string) ([]tpdb.TpdbScene, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrapper status: %d", resp.StatusCode)
	}

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	htmlStr := string(htmlBytes)

	var scenes []tpdb.TpdbScene

	// Split by article container
	blocks := strings.Split(htmlStr, `<div class="inside-article"`)
	if len(blocks) <= 1 {
		blocks = strings.Split(htmlStr, `<article`)
	}

	codeRx := regexp.MustCompile(`(?i)\b([a-z0-9]{2,10}-\d{3,6})\b`)
	linkRx := regexp.MustCompile(`(?i)href=["'](https?://jav\.guru/\d+/[^"']+)["']`)
	imgRx := regexp.MustCompile(`(?i)src=["'](https?://[^"']+\.(?:jpg|png|webp|jpeg))["']`)
	altRx := regexp.MustCompile(`(?i)alt=["']([^"']+)["']`)

	for _, block := range blocks[1:] {
		linkMatch := linkRx.FindStringSubmatch(block)
		if len(linkMatch) < 2 {
			continue
		}
		link := linkMatch[1]

		imgMatch := imgRx.FindStringSubmatch(block)
		if len(imgMatch) < 2 {
			continue
		}
		imgURL := imgMatch[1]

		altMatch := altRx.FindStringSubmatch(block)
		title := ""
		if len(altMatch) >= 2 {
			title = html.UnescapeString(altMatch[1])
		}

		if strings.Contains(link, "/category/") || strings.Contains(link, "/tag/") || title == "" {
			continue
		}

		code := "NOCODE"
		if codeMatch := codeRx.FindString(title); codeMatch != "" {
			code = strings.ToUpper(codeMatch)
		}

		originalPoster := cleanImageURL(imgURL)
		pubDate := extractDate(imgURL, link)

		scene := tpdb.TpdbScene{
			ID:          "fallback_" + code,
			Title:       title,
			Date:        pubDate,
			Poster:      originalPoster,
			Image:       originalPoster,
			PosterImage: originalPoster,
			Site: &tpdb.TpdbSceneSite{
				Name: "JAV Guru",
			},
		}

		// Pre-populate the Redis cache with our directly resolved poster to prevent future detail-page scrapes!
		if s.redis != nil && code != "NOCODE" {
			redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)
			sceneJSON, _ := json.Marshal(scene)
			s.redis.Get().Set(ctx, redisKey, string(sceneJSON), 4*time.Hour)
		}

		scenes = append(scenes, scene)
	}

	return scenes, nil
}

func (s *Service) FetchRecent(ctx context.Context, page int) ([]tpdb.TpdbScene, error) {
	perPage := 60
	itemsPerPage := 24

	startIdx := (page - 1) * perPage
	endIdx := page * perPage

	startPage := (startIdx / itemsPerPage) + 1
	endPage := ((endIdx - 1) / itemsPerPage) + 1

	numPages := endPage - startPage + 1
	scenes := make([][]tpdb.TpdbScene, numPages)

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Fetch JAV Guru pages in parallel and store them in exact slice order to preserve chronology
	for i := 0; i < numPages; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			pNum := startPage + offset
			var u string
			if pNum == 1 {
				u = "https://jav.guru/"
			} else {
				u = fmt.Sprintf("https://jav.guru/page/%d/", pNum)
			}
			res, err := s.scrapeCatalog(ctx, u)
			if err == nil {
				mu.Lock()
				scenes[offset] = res
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	var combined []tpdb.TpdbScene
	for _, pageScenes := range scenes {
		combined = append(combined, pageScenes...)
	}

	seen := make(map[string]bool)
	var uniqueScenes []tpdb.TpdbScene
	for _, sc := range combined {
		if !seen[sc.ID] {
			seen[sc.ID] = true
			uniqueScenes = append(uniqueScenes, sc)
		}
	}

	localStart := startIdx - (startPage-1)*itemsPerPage
	localEnd := localStart + perPage

	if localStart >= len(uniqueScenes) {
		return nil, nil
	}
	if localEnd > len(uniqueScenes) {
		localEnd = len(uniqueScenes)
	}

	return uniqueScenes[localStart:localEnd], nil
}

func (s *Service) FetchSubbed(ctx context.Context, page int) ([]tpdb.TpdbScene, error) {
	// First page: fetch popular English Subbed posts (most viewed) via JSON API
	if page == 1 {
		u := "https://jav.guru/wp-json/wordpress-popular-posts/v1/popular-posts?limit=60&cat=1826"
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var items []WppItem
			if err := json.NewDecoder(resp.Body).Decode(&items); err == nil {
				return s.resolveWppItems(ctx, items), nil
			}
		}
	}

	// Fallback/subsequent pages: scrape category HTML directly, fetching 3 pages in parallel with exact offset slicing!
	perPage := 60
	itemsPerPage := 24

	startIdx := (page - 1) * perPage
	endIdx := page * perPage

	startPage := (startIdx / itemsPerPage) + 1
	endPage := ((endIdx - 1) / itemsPerPage) + 1

	numPages := endPage - startPage + 1
	scenes := make([][]tpdb.TpdbScene, numPages)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < numPages; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			pNum := startPage + offset
			var u string
			if pNum == 1 {
				u = "https://jav.guru/category/english-subbed/"
			} else {
				u = fmt.Sprintf("https://jav.guru/category/english-subbed/page/%d/", pNum)
			}
			res, err := s.scrapeCatalog(ctx, u)
			if err == nil {
				mu.Lock()
				scenes[offset] = res
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	var combined []tpdb.TpdbScene
	for _, pageScenes := range scenes {
		combined = append(combined, pageScenes...)
	}

	seen := make(map[string]bool)
	var uniqueScenes []tpdb.TpdbScene
	for _, sc := range combined {
		if !seen[sc.ID] {
			seen[sc.ID] = true
			uniqueScenes = append(uniqueScenes, sc)
		}
	}

	localStart := startIdx - (startPage-1)*itemsPerPage
	localEnd := localStart + perPage

	if localStart >= len(uniqueScenes) {
		return nil, nil
	}
	if localEnd > len(uniqueScenes) {
		localEnd = len(uniqueScenes)
	}

	return uniqueScenes[localStart:localEnd], nil
}

func (s *Service) FetchTrending(ctx context.Context) ([]tpdb.TpdbScene, error) {
	u := "https://jav.guru/most-watched-rank/"
	return s.scrapeCatalog(ctx, u)
}

func (s *Service) Search(ctx context.Context, query string, page int) ([]tpdb.TpdbScene, error) {
	perPage := 60
	itemsPerPage := 24

	startIdx := (page - 1) * perPage
	endIdx := page * perPage

	startPage := (startIdx / itemsPerPage) + 1
	endPage := ((endIdx - 1) / itemsPerPage) + 1

	numPages := endPage - startPage + 1
	scenes := make([][]tpdb.TpdbScene, numPages)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < numPages; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			pNum := startPage + offset
			var u string
			if pNum == 1 {
				u = fmt.Sprintf("https://jav.guru/?s=%s", url.QueryEscape(query))
			} else {
				u = fmt.Sprintf("https://jav.guru/page/%d/?s=%s", pNum, url.QueryEscape(query))
			}
			res, err := s.scrapeCatalog(ctx, u)
			if err == nil {
				mu.Lock()
				scenes[offset] = res
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	var combined []tpdb.TpdbScene
	for _, pageScenes := range scenes {
		combined = append(combined, pageScenes...)
	}

	seen := make(map[string]bool)
	var uniqueScenes []tpdb.TpdbScene
	for _, sc := range combined {
		if !seen[sc.ID] {
			seen[sc.ID] = true
			uniqueScenes = append(uniqueScenes, sc)
		}
	}

	localStart := startIdx - (startPage-1)*itemsPerPage
	localEnd := localStart + perPage

	if localStart >= len(uniqueScenes) {
		return nil, nil
	}
	if localEnd > len(uniqueScenes) {
		localEnd = len(uniqueScenes)
	}

	return uniqueScenes[localStart:localEnd], nil
}

func (s *Service) resolveWppItems(ctx context.Context, items []WppItem) []tpdb.TpdbScene {
	var scenes []tpdb.TpdbScene
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, 10)
	for _, item := range items {
		wg.Add(1)
		go func(it WppItem) {
			defer wg.Done()
			codeRx := regexp.MustCompile(`(?i)\b([a-z0-9]{2,10}-\d{3,6})\b`)
			title := html.UnescapeString(it.Title.Rendered)
			codeMatch := codeRx.FindString(title)

			var code string
			if codeMatch != "" {
				code = strings.ToUpper(codeMatch)
			} else {
				h := sha256.Sum256([]byte(title))
				code = "NOCODE_" + hex.EncodeToString(h[:8])
			}

			sem <- struct{}{}
			scene, err := s.ResolveSingleJavScene(ctx, code, title, it.Date)
			<-sem
			if err == nil {
				mu.Lock()
				scenes = append(scenes, scene)
				mu.Unlock()
			}
		}(item)
	}
	wg.Wait()

	// Maintain original popular ranking order
	var ordered []tpdb.TpdbScene
	for _, item := range items {
		title := html.UnescapeString(item.Title.Rendered)
		codeRx := regexp.MustCompile(`(?i)\b([a-z0-9]{2,10}-\d{3,6})\b`)
		codeMatch := codeRx.FindString(title)

		var code string
		if codeMatch != "" {
			code = strings.ToUpper(codeMatch)
		} else {
			h := sha256.Sum256([]byte(title))
			code = "NOCODE_" + hex.EncodeToString(h[:8])
		}

		for _, s := range scenes {
			if s.ID == "fallback_"+code || s.ID == code || strings.Contains(strings.ToUpper(s.Title), code) {
				ordered = append(ordered, s)
				break
			}
		}
	}

	return ordered
}

func (s *Service) ResolveSingleJavScene(ctx context.Context, code, javGuruTitle, pubDate string) (tpdb.TpdbScene, error) {
	redisKey := fmt.Sprintf("jav_scene_by_code:%s", code)

	if s.redis != nil && !strings.HasPrefix(code, "NOCODE_") {
		val, err := s.redis.Get().Get(ctx, redisKey).Result()
		if err == nil && val != "" {
			var cachedScene tpdb.TpdbScene
			if err := json.Unmarshal([]byte(val), &cachedScene); err == nil {
				return cachedScene, nil
			}
		}
	}

	jgPoster, err := s.ResolvePoster(ctx, code)
	if err != nil && strings.HasPrefix(code, "NOCODE_") {
		jgPoster, err = s.ResolvePoster(ctx, javGuruTitle)
	}

	scene := tpdb.TpdbScene{
		ID:          "fallback_" + code,
		Title:       javGuruTitle,
		Date:        pubDate,
		Site: &tpdb.TpdbSceneSite{
			Name: "JAV Guru",
		},
	}

	if err == nil && jgPoster != "" {
		scene.Poster = jgPoster
		scene.Image = jgPoster
		scene.PosterImage = jgPoster
	}

	if s.redis != nil && !strings.HasPrefix(code, "NOCODE_") {
		sceneJSON, _ := json.Marshal(scene)
		s.redis.Get().Set(ctx, redisKey, string(sceneJSON), 4*time.Hour)
	}

	return scene, nil
}

func (s *Service) ResolvePoster(ctx context.Context, queryOrCode string) (string, error) {
	if queryOrCode == "" || strings.HasPrefix(queryOrCode, "NOCODE_") {
		return "", fmt.Errorf("invalid query")
	}

	isFullTitleSearch := strings.Contains(queryOrCode, " ") || len(queryOrCode) > 15
	cleanCode := strings.ToLower(strings.ReplaceAll(queryOrCode, "-", ""))

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

	linkRx := regexp.MustCompile(`(?i)href=["'](https?://jav\.guru/\d+/[^"']+)["']`)
	matches := linkRx.FindAllStringSubmatch(htmlStr, -1)

	var postLink string
	if !isFullTitleSearch {
		for _, m := range matches {
			if len(m) > 1 {
				link := m[1]
				linkLower := strings.ToLower(link)
				linkClean := strings.ReplaceAll(linkLower, "-", "")
				if matchJavCodeFuzzy(linkClean, cleanCode) {
					postLink = link
					break
				}
			}
		}
	}

	if postLink == "" {
		for _, m := range matches {
			if len(m) > 1 {
				link := m[1]
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

	imgRx := regexp.MustCompile(`(?i)<img[^>]+src=["'](https?://[^"']+\.(?:jpg|png|webp|jpeg))["']`)
	allImgs := imgRx.FindAllStringSubmatch(postHtmlStr, -1)

	for _, imgMatch := range allImgs {
		if len(imgMatch) > 1 {
			imgURL := imgMatch[1]
			imgURLUnescaped := strings.ToLower(imgURL)

			if !isFullTitleSearch {
				cleanedURL := strings.ReplaceAll(imgURLUnescaped, "-", "")
				if matchJavCodeFuzzy(cleanedURL, cleanCode) &&
					!strings.Contains(imgURLUnescaped, "logo") &&
					!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
					return cleanImageURL(imgURL), nil
				}
			} else {
				if strings.Contains(imgURLUnescaped, "/wp-content/uploads/") &&
					!strings.Contains(imgURLUnescaped, "logo") &&
					!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
					return cleanImageURL(imgURL), nil
				}
			}
		}
	}

	for _, imgMatch := range allImgs {
		if len(imgMatch) > 1 {
			imgURL := imgMatch[1]
			imgURLUnescaped := strings.ToLower(imgURL)
			if strings.Contains(imgURLUnescaped, "/wp-content/uploads/") &&
				!strings.Contains(imgURLUnescaped, "logo") &&
				!strings.Contains(imgURLUnescaped, "wordpress-popular-posts") {
				return cleanImageURL(imgURL), nil
			}
		}
	}

	return "", fmt.Errorf("no cover image found on post page")
}
