package tpdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
)

const (
	ApiKeyFlag = "tpdb-api-key"
	BaseURL    = "https://api.theporndb.net"
)

type Service struct {
	apiKey string
	client *http.Client
	pg     *cs.PG
}

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.StringFlag{
		Name:   ApiKeyFlag,
		Usage:  "ThePornDB API key",
		EnvVar: "TPDB_API_KEY",
	})
}

func New(c *cli.Context, cl *http.Client, pg *cs.PG) *Service {
	key := c.String(ApiKeyFlag)
	if key == "" {
		return nil
	}
	return &Service{
		apiKey: key,
		client: cl,
		pg:     pg,
	}
}

type tpdbResponse struct {
	Data []struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	} `json:"data"`
}

func (s *Service) FetchStudioPoster(ctx context.Context, name string) (string, error) {
	if name == "" || name == "N/A" {
		return "", nil
	}
	db := s.pg.Get()
	cached, err := models.GetTpdbStudio(ctx, db, name)
	if err == nil && cached != nil && time.Since(cached.UpdatedAt) < 30*24*time.Hour {
		return cached.PosterURL, nil
	}

	u := fmt.Sprintf("%s/studios?q=%s", BaseURL, url.QueryEscape(name))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tpdb api error: %d", resp.StatusCode)
	}

	var tr tpdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}

	posterURL := ""
	if len(tr.Data) > 0 {
		// Try exact match first
		for _, d := range tr.Data {
			if strings.EqualFold(d.Name, name) {
				posterURL = d.Image
				break
			}
		}
		if posterURL == "" {
			posterURL = tr.Data[0].Image
		}
	}

	_ = models.UpsertTpdbStudio(ctx, db, &models.TpdbStudio{
		Name:      name,
		PosterURL: posterURL,
	})

	return posterURL, nil
}

func (s *Service) FetchPerformerPoster(ctx context.Context, name string) (string, error) {
	if name == "" || name == "N/A" {
		return "", nil
	}
	db := s.pg.Get()
	cached, err := models.GetTpdbPerformer(ctx, db, name)
	if err == nil && cached != nil && time.Since(cached.UpdatedAt) < 30*24*time.Hour {
		return cached.PosterURL, nil
	}

	u := fmt.Sprintf("%s/performers?q=%s", BaseURL, url.QueryEscape(name))
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tpdb api error: %d", resp.StatusCode)
	}

	var tr tpdbResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}

	posterURL := ""
	if len(tr.Data) > 0 {
		// Try exact match first
		for _, d := range tr.Data {
			if strings.EqualFold(d.Name, name) {
				posterURL = d.Image
				break
			}
		}
		if posterURL == "" {
			posterURL = tr.Data[0].Image
		}
	}

	_ = models.UpsertTpdbPerformer(ctx, db, &models.TpdbPerformer{
		Name:      name,
		PosterURL: posterURL,
	})

	return posterURL, nil
}
