package tpdb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mozillazg/go-unidecode"
	"github.com/webtor-io/sidecar/internal/httpclient"
	"github.com/webtor-io/sidecar/internal/scene"
	
	log "github.com/sirupsen/logrus"
)

type Client struct {
	apiKey  string
	baseURL string
}

func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.theporndb.net"
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

type tpdbSite struct {
	Name    string      `json:"name"`
	Parent  *tpdbParent `json:"parent"`
	Network *tpdbParent `json:"network"`
}

type tpdbParent struct {
	Name string `json:"name"`
}

type tpdbPerformer struct {
	Name      string         `json:"name"`
	Performer *tpdbPerformer `json:"performer"`
}

type tpdbTag struct {
	Name string `json:"name"`
}

type tpdbScene struct {
	ObjectID       any             `json:"_id"`
	ID             any             `json:"id"`
	Slug           string          `json:"slug"`
	Title          string          `json:"title"`
	Date           string          `json:"date"`
	ReleaseDate    string          `json:"release_date"`
	ProductionDate string          `json:"production_date"`
	Site           *tpdbSite       `json:"site"`
	Description    string          `json:"description"`
	Details        string          `json:"details"`
	Performers     []tpdbPerformer `json:"performers"`
	Tags           []tpdbTag       `json:"tags"`
	Poster         string          `json:"poster"`
	Posters        map[string]any  `json:"posters"`
	PosterImage    string          `json:"poster_image"`
	Image          string          `json:"image"`
	Duration       any             `json:"duration"`
	Rating         any             `json:"rating"`
	URL            string          `json:"url"`
	ExternalID     string          `json:"external_id"`
}

type tpdbResponse struct {
	Data []tpdbScene `json:"data"`
}

type tpdbSingleResponse struct {
	Data tpdbScene `json:"data"`
}

var rxNonAlphaNum = regexp.MustCompile(`[^a-z0-9]`)

func (c *Client) getHeaders() http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+c.apiKey)
	h.Set("Accept", "application/json")
	h.Set("User-Agent", "octor-sidecar/2")
	return h
}

func toFloat64Ptr(val any) *float64 {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f
		}
	}
	return nil
}

func (c *Client) normalize(d tpdbScene, source string) scene.Scene {
	var urls []string
	if d.Poster != "" {
		urls = append(urls, d.Poster)
	}
	if d.Posters != nil {
		for _, key := range []string{"poster", "full", "large"} {
			if val, exists := d.Posters[key]; exists {
				if strVal, ok := val.(string); ok && strVal != "" {
					found := false
					for _, u := range urls {
						if u == strVal {
							found = true
							break
						}
					}
					if !found {
						urls = append(urls, strVal)
					}
				}
			}
		}
	}
	for _, val := range []string{d.PosterImage, d.Image} {
		if val != "" {
			found := false
			for _, u := range urls {
				if u == val {
					found = true
					break
				}
			}
			if !found {
				urls = append(urls, val)
			}
		}
	}

	poster := ""
	for _, u := range urls {
		if strings.Contains(u, "theporndb.net") {
			poster = u
			break
		}
	}
	if poster == "" && len(urls) > 0 {
		for _, u := range urls {
			if !strings.Contains(u, "digitaloceanspaces") {
				poster = u
				break
			}
		}
		if poster == "" {
			poster = urls[0]
		}
	}

	siteName := ""
	parentName := ""
	networkName := ""
	if d.Site != nil {
		siteName = d.Site.Name
		if d.Site.Parent != nil {
			parentName = d.Site.Parent.Name
		}
		if d.Site.Network != nil {
			networkName = d.Site.Network.Name
		}
	}

	desc := d.Description
	if desc == "" {
		desc = d.Details
	}

	var performers []scene.Performer
	for _, p := range d.Performers {
		name := p.Name
		if name == "" && p.Performer != nil {
			name = p.Performer.Name
		}
		if name != "" {
			performers = append(performers, scene.Performer{Name: name})
		}
	}

	var tags []string
	for _, t := range d.Tags {
		if t.Name != "" {
			tags = append(tags, t.Name)
		}
	}

	dateStr := d.Date
	if dateStr == "" {
		dateStr = d.ReleaseDate
	}
	if dateStr == "" {
		dateStr = d.ProductionDate
	}

	sceneID := ""
	if d.ObjectID != nil {
		switch idVal := d.ObjectID.(type) {
		case string:
			sceneID = idVal
		case float64:
			sceneID = strconv.FormatFloat(idVal, 'f', -1, 64)
		}
	}
	if sceneID == "" && d.ID != nil {
		switch idVal := d.ID.(type) {
		case string:
			sceneID = idVal
		case float64:
			sceneID = strconv.FormatFloat(idVal, 'f', -1, 64)
		}
	}
	if sceneID == "" {
		sceneID = d.Slug
	}

	return scene.Scene{
		ID:          sceneID,
		Title:       d.Title,
		Date:        dateStr,
		Site:        siteName,
		Parent:      parentName,
		Network:     networkName,
		Description: desc,
		Performers:  performers,
		Tags:        tags,
		Poster:      poster,
		Duration:    toFloat64Ptr(d.Duration),
		Rating:      toFloat64Ptr(d.Rating),
		URL:         d.URL,
		Source:      source,
		ExternalID:  d.ExternalID,
	}
}

func (c *Client) Search(site, date, name string, limit int) ([]scene.Scene, error) {
	if c.apiKey == "" {
		return nil, nil
	}

	var parts []string
	if site != "" {
		parts = append(parts, rxNonAlphaNum.ReplaceAllString(strings.ToLower(unidecode.Unidecode(site)), ""))
	}
	if date != "" {
		parts = append(parts, date)
	}
	if name != "" {
		parts = append(parts, name)
	}

	parseStr := strings.Join(parts, ".")
	if parseStr == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/scenes?parse=%s&limit=%d", c.baseURL, url.QueryEscape(parseStr), limit)
	log.Infof("TPDB search: %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.getHeaders()

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TPDB search returned status %d", resp.StatusCode)
	}

	var res tpdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	normalized := make([]scene.Scene, 0, len(res.Data))
	for _, d := range res.Data {
		normalized = append(normalized, c.normalize(d, "tpdb"))
	}
	return normalized, nil
}

func (c *Client) SearchRaw(q string, limit int) ([]scene.Scene, error) {
	if c.apiKey == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/scenes?q=%s&per_page=%d", c.baseURL, url.QueryEscape(q), limit)
	log.Infof("TPDB raw search: %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.getHeaders()

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TPDB raw search returned status %d", resp.StatusCode)
	}

	var res tpdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	normalized := make([]scene.Scene, 0, len(res.Data))
	for _, d := range res.Data {
		normalized = append(normalized, c.normalize(d, "tpdb"))
	}
	return normalized, nil
}

func (c *Client) GetByID(id string) (*scene.Scene, error) {
	if c.apiKey == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/scenes/%s", c.baseURL, url.PathEscape(id))
	log.Infof("TPDB ID lookup: %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.getHeaders()

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TPDB ID lookup returned status %d", resp.StatusCode)
	}

	// Some endpoints might return a single scene object directly, or wrap it in a {"data": ...}
	// Let's decode to raw json.RawMessage first to handle both.
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	var singleRes tpdbSingleResponse
	if err := json.Unmarshal(raw, &singleRes); err == nil && singleRes.Data.Title != "" {
		norm := c.normalize(singleRes.Data, "tpdb")
		return &norm, nil
	}

	var direct tpdbScene
	if err := json.Unmarshal(raw, &direct); err == nil && direct.Title != "" {
		norm := c.normalize(direct, "tpdb")
		return &norm, nil
	}

	return nil, fmt.Errorf("failed to parse TPDB ID lookup response")
}

func (c *Client) JAVSearch(code string, limit int) ([]scene.Scene, error) {
	if c.apiKey == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/jav?parse=%s&limit=%d", c.baseURL, url.QueryEscape(code), limit)
	log.Infof("TPDB JAV search: %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.getHeaders()

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TPDB JAV search returned status %d", resp.StatusCode)
	}

	var res tpdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	normalized := make([]scene.Scene, 0, len(res.Data))
	for _, d := range res.Data {
		normalized = append(normalized, c.normalize(d, "tpdb_jav"))
	}
	return normalized, nil
}

func (c *Client) JAVGetByID(id string) (*scene.Scene, error) {
	if c.apiKey == "" {
		return nil, nil
	}

	u := fmt.Sprintf("%s/jav/%s", c.baseURL, url.PathEscape(id))
	log.Infof("TPDB JAV ID lookup: %s", u)

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.getHeaders()

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TPDB JAV ID lookup returned status %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	var singleRes tpdbSingleResponse
	if err := json.Unmarshal(raw, &singleRes); err == nil && singleRes.Data.Title != "" {
		norm := c.normalize(singleRes.Data, "tpdb_jav")
		return &norm, nil
	}

	var direct tpdbScene
	if err := json.Unmarshal(raw, &direct); err == nil && direct.Title != "" {
		norm := c.normalize(direct, "tpdb_jav")
		return &norm, nil
	}

	return nil, fmt.Errorf("failed to parse TPDB JAV ID lookup response")
}
