package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var (
	sha1HexRx   = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	sha256HexRx = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	base32Rx    = regexp.MustCompile(`^[2-7a-zA-Z]{32}$`)
)

func isValidInfoHash(h string) bool {
	return sha1HexRx.MatchString(h) || sha256HexRx.MatchString(h) || base32Rx.MatchString(h)
}


type CachedSearch struct {
	Results   []ProwlarrResultItem
	ExpiredAt time.Time
}

type ProwlarrClient struct {
	url                string
	apiKey             string
	magnetOnly         bool
	client             *http.Client
	indexerCache       []int
	indexerCacheExpiry time.Time
	indexerCacheLock   sync.Mutex
	searchCache        map[string]CachedSearch
	searchCacheLock    sync.Mutex
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
	Guid        string `json:"guid,omitempty"`
	PublishDate string `json:"publishDate,omitempty"`
	Files       int    `json:"files,omitempty"`
}

const (
	prowlarrURLFlag        = "prowlarr-url"
	prowlarrAPIKeyFlag     = "prowlarr-apikey"
	prowlarrMagnetOnlyFlag = "prowlarr-magnet-only"
)

func RegisterProwlarrFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   prowlarrURLFlag,
			Usage:  "Prowlarr base URL",
			Value:  "",
			EnvVar: "PROWLARR_URL",
		},
		cli.StringFlag{
			Name:   prowlarrAPIKeyFlag,
			Usage:  "Prowlarr API key",
			Value:  "",
			EnvVar: "PROWLARR_API_KEY",
		},
		cli.BoolFlag{
			Name:   prowlarrMagnetOnlyFlag,
			Usage:  "Only return search results with pre-existing valid magnet links or infohashes",
			EnvVar: "PROWLARR_MAGNET_ONLY",
		},
	)
}

func NewProwlarrClient(c *cli.Context) *ProwlarrClient {
	return &ProwlarrClient{
		url:         c.String(prowlarrURLFlag),
		apiKey:      c.String(prowlarrAPIKeyFlag),
		magnetOnly:  c.Bool(prowlarrMagnetOnlyFlag),
		searchCache: make(map[string]CachedSearch),
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (s *ProwlarrClient) IsEnabled() bool {
	return s.url != "" && s.apiKey != ""
}

func normalizeInfoHash(infoHash string, magnetURL string) string {
	var hash string
	if infoHash != "" {
		hash = infoHash
	} else if magnetURL != "" {
		idx := strings.Index(magnetURL, "xt=urn:btih:")
		if idx != -1 {
			hashPart := magnetURL[idx+len("xt=urn:btih:"):]
			ampIdx := strings.Index(hashPart, "&")
			if ampIdx != -1 {
				hashPart = hashPart[:ampIdx]
			}
			hash = hashPart
		}
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash != "" && isValidInfoHash(hash) {
		return hash
	}
	return ""
}


type ProwlarrIndexer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Enable   bool   `json:"enable"`
	Priority int    `json:"priority"`
}

func (s *ProwlarrClient) getTopIndexerIDs() ([]int, error) {
	s.indexerCacheLock.Lock()
	if len(s.indexerCache) > 0 && time.Now().Before(s.indexerCacheExpiry) {
		ids := make([]int, len(s.indexerCache))
		copy(ids, s.indexerCache)
		s.indexerCacheLock.Unlock()
		return ids, nil
	}
	s.indexerCacheLock.Unlock()

	apiUrl := fmt.Sprintf("%s/api/v1/indexer", s.url)
	u, err := url.Parse(apiUrl)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("apikey", s.apiKey)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indexer request returned status: %d", resp.StatusCode)
	}

	var indexers []ProwlarrIndexer
	if err := json.NewDecoder(resp.Body).Decode(&indexers); err != nil {
		return nil, err
	}

	var ids []int
	for _, idx := range indexers {
		if idx.Enable && idx.Priority <= 10 {
			ids = append(ids, idx.ID)
		}
	}

	s.indexerCacheLock.Lock()
	s.indexerCache = make([]int, len(ids))
	copy(s.indexerCache, ids)
	s.indexerCacheExpiry = time.Now().Add(5 * time.Minute)
	s.indexerCacheLock.Unlock()

	return ids, nil
}

func (s *ProwlarrClient) searchSingleIndexer(query string, indexerID int) ([]ProwlarrResultItem, error) {
	apiUrl := fmt.Sprintf("%s/api/v1/search", s.url)
	u, err := url.Parse(apiUrl)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("apikey", s.apiKey)
	q.Set("type", "search")
	if indexerID != -2 {
		q.Set("indexerIds", fmt.Sprintf("%d", indexerID))
	}
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prowlarr search returned status %d", resp.StatusCode)
	}

	var results []ProwlarrResultItem
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	return results, nil
}

func (s *ProwlarrClient) Search(query string) ([]ProwlarrResultItem, error) {
	if !s.IsEnabled() {
		return nil, errors.New("Prowlarr client is not configured")
	}

	queryKey := strings.ToLower(strings.TrimSpace(query))
	s.searchCacheLock.Lock()
	if cached, ok := s.searchCache[queryKey]; ok && time.Now().Before(cached.ExpiredAt) {
		resultsCopy := make([]ProwlarrResultItem, len(cached.Results))
		copy(resultsCopy, cached.Results)
		s.searchCacheLock.Unlock()
		log.Infof("Returning cached Prowlarr search results for query: %q", query)
		return resultsCopy, nil
	}
	s.searchCacheLock.Unlock()

	indexerIDs, err := s.getTopIndexerIDs()
	if err != nil {
		log.Warnf("failed to get top indexers from Prowlarr: %v, falling back to all indexers", err)
	}

	var results []ProwlarrResultItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	doneChan := make(chan struct{})

	if len(indexerIDs) > 0 {
		for _, id := range indexerIDs {
			wg.Add(1)
			go func(idxID int) {
				defer wg.Done()
				items, err := s.searchSingleIndexer(query, idxID)
				if err != nil {
					log.Warnf("Prowlarr search failed for indexer %d: %v", idxID, err)
					return
				}
				mu.Lock()
				results = append(results, items...)
				mu.Unlock()
			}(id)
		}
		go func() {
			wg.Wait()
			close(doneChan)
		}()
	} else {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := s.searchSingleIndexer(query, -2)
			if err != nil {
				log.Warnf("failed fallback Prowlarr search query: %v", err)
				return
			}
			mu.Lock()
			results = items
			mu.Unlock()
		}()
		go func() {
			wg.Wait()
			close(doneChan)
		}()
	}

	select {
	case <-doneChan:
		log.Debug("All Prowlarr indexers completed search in time")
	case <-time.After(6 * time.Second):
		log.Warn("Prowlarr search timed out after 6s waiting for slow indexers, returning partial results")
	}

	var deduped []ProwlarrResultItem
	seenHashes := make(map[string]bool)

	for _, item := range results {
		if item.Seeders < 2 {
			continue
		}
		hash := normalizeInfoHash(item.InfoHash, item.MagnetURL)
		if hash != "" {
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
			item.InfoHash = hash
			// Keep existing MagnetURL (Prowlarr proxy) as-is — it lets the
			// REST API download the .torrent file directly (~2s).
			// Only build a fallback magnet: URI when nothing else is available.
			if item.MagnetURL == "" {
				item.MagnetURL = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", item.InfoHash, url.QueryEscape(item.Title))
			}
		} else {
			if s.magnetOnly {
				continue
			}
			item.InfoHash = ""
			item.MagnetURL = ""
		}

		log.Debugf("Prowlarr result: %s indexer=%s seeders=%d size=%d hasMagnet=%v hasDL=%v",
			item.Title, item.Indexer, item.Seeders, item.Size, item.MagnetURL != "", item.DownloadURL != "")
		deduped = append(deduped, item)
	}

	sort.SliceStable(deduped, func(i, j int) bool {
		hasMagnetI := deduped[i].InfoHash != "" || deduped[i].MagnetURL != ""
		hasMagnetJ := deduped[j].InfoHash != "" || deduped[j].MagnetURL != ""
		if hasMagnetI && !hasMagnetJ {
			return true
		}
		if !hasMagnetI && hasMagnetJ {
			return false
		}
		return false
	})

	s.searchCacheLock.Lock()
	resultsCopy := make([]ProwlarrResultItem, len(deduped))
	copy(resultsCopy, deduped)
	s.searchCache[queryKey] = CachedSearch{
		Results:   resultsCopy,
		ExpiredAt: time.Now().Add(5 * time.Minute),
	}
	s.searchCacheLock.Unlock()

	return deduped, nil
}
