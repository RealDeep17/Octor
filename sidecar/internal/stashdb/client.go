package stashdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/webtor-io/sidecar/internal/httpclient"
	"github.com/webtor-io/sidecar/internal/scene"

	log "github.com/sirupsen/logrus"
)

type Client struct {
	apiKey   string
	endpoint string
}

func NewClient(apiKey, endpoint string) *Client {
	if endpoint == "" {
		endpoint = "https://stashdb.org/graphql"
	}
	return &Client{
		apiKey:   apiKey,
		endpoint: endpoint,
	}
}

type stashStudio struct {
	Name   string       `json:"name"`
	Parent *stashParent `json:"parent"`
}

type stashParent struct {
	Name string `json:"name"`
}

type stashPerformer struct {
	Performer stashPerformerInner `json:"performer"`
	As        string              `json:"as"`
}

type stashPerformerInner struct {
	Name string `json:"name"`
}

type stashTag struct {
	Name string `json:"name"`
}

type stashImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type stashURL struct {
	URL  string `json:"url"`
	Site struct {
		Name string `json:"name"`
	} `json:"site"`
}

type stashScene struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Details     string           `json:"details"`
	ReleaseDate string           `json:"release_date"`
	Duration    *float64         `json:"duration"`
	Studio      *stashStudio     `json:"studio"`
	Performers  []stashPerformer `json:"performers"`
	Tags        []stashTag       `json:"tags"`
	Images      []stashImage     `json:"images"`
	URLs        []stashURL       `json:"urls"`
}

type searchSceneResponse struct {
	Data struct {
		SearchScene []stashScene `json:"searchScene"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type findSceneResponse struct {
	Data struct {
		FindScene *stashScene `json:"findScene"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const stashSearchQuery = `
query ($term: String!) {
  searchScene(term: $term) {
    id title details release_date duration
    studio {
      name
      parent {
        name
      }
    }
    performers { performer { name } as }
    tags { name }
    images { url width height }
    urls { url site { name } }
  }
}
`

const stashFindQuery = `
query ($id: ID!) {
  findScene(id: $id) {
    id title details release_date duration
    studio {
      name
      parent {
        name
      }
    }
    performers { performer { name } as }
    tags { name }
    images { url width height }
    urls { url site { name } }
  }
}
`

func (c *Client) graphql(query string, vars map[string]any) ([]byte, error) {
	payload := map[string]any{
		"query":     query,
		"variables": vars,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Apikey", c.apiKey)

	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stashdb graphql returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) normalize(s stashScene) scene.Scene {
	poster := ""
	for _, img := range s.Images {
		if img.URL != "" && img.Height > img.Width {
			if !strings.Contains(img.URL, "digitaloceanspaces") {
				poster = img.URL
				break
			}
		}
	}
	if poster == "" {
		for _, img := range s.Images {
			if img.URL != "" && !strings.Contains(img.URL, "digitaloceanspaces") {
				poster = img.URL
				break
			}
		}
	}
	if poster == "" && len(s.Images) > 0 {
		poster = s.Images[0].URL
	}

	siteName := ""
	parentName := ""
	if s.Studio != nil {
		siteName = s.Studio.Name
		if s.Studio.Parent != nil {
			parentName = s.Studio.Parent.Name
		}
	}

	var performers []scene.Performer
	for _, p := range s.Performers {
		if p.Performer.Name != "" {
			performers = append(performers, scene.Performer{Name: p.Performer.Name})
		}
	}

	var tags []string
	for _, t := range s.Tags {
		if t.Name != "" {
			tags = append(tags, t.Name)
		}
	}


	return scene.Scene{
		ID:          s.ID,
		Title:       s.Title,
		Date:        s.ReleaseDate,
		Site:        siteName,
		Parent:      parentName,
		Description: s.Details,
		Performers:  performers,
		Tags:        tags,
		Poster:      poster,
		Duration:    s.Duration,
		Rating:      nil,
		URL:         "", // intentionally empty — cross-DB URL merging causes actor bloat
		Source:      "stashdb",
	}

}

func (c *Client) Search(term string) ([]scene.Scene, error) {
	log.Infof("StashDB search initiated for term: '%s' (key present: %v)", term, c.apiKey != "")
	if c.apiKey == "" || term == "" {
		return nil, nil
	}

	vars := map[string]any{"term": term}
	data, err := c.graphql(stashSearchQuery, vars)
	if err != nil {
		return nil, err
	}

	var res searchSceneResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}

	if len(res.Errors) > 0 {
		var msgs []string
		for _, e := range res.Errors {
			msgs = append(msgs, e.Message)
		}
		log.Warnf("StashDB GraphQL errors: %s", strings.Join(msgs, "; "))
		return nil, fmt.Errorf("StashDB GraphQL errors: %s", strings.Join(msgs, "; "))
	}

	normalized := make([]scene.Scene, 0, len(res.Data.SearchScene))
	for _, s := range res.Data.SearchScene {
		normalized = append(normalized, c.normalize(s))
	}
	return normalized, nil
}

func (c *Client) GetByID(id string) (*scene.Scene, error) {
	log.Infof("StashDB ID lookup: %s", id)
	if c.apiKey == "" || id == "" {
		return nil, nil
	}

	vars := map[string]any{"id": id}
	data, err := c.graphql(stashFindQuery, vars)
	if err != nil {
		return nil, err
	}

	var res findSceneResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}

	if len(res.Errors) > 0 {
		var msgs []string
		for _, e := range res.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("StashDB find errors: %s", strings.Join(msgs, "; "))
	}

	if res.Data.FindScene == nil {
		return nil, nil
	}

	norm := c.normalize(*res.Data.FindScene)
	return &norm, nil
}
