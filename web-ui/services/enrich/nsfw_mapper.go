package enrich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	om "github.com/webtor-io/web-ui/models/omdb"
	"github.com/webtor-io/web-ui/services/tpdb"
)

type NSFWMapper struct {
	pg      *cs.PG
	tpdbSvc *tpdb.Service
}

func NewNSFWMapper(pg *cs.PG, tpdbSvc *tpdb.Service) *NSFWMapper {
	return &NSFWMapper{
		pg:      pg,
		tpdbSvc: tpdbSvc,
	}
}

func (s *NSFWMapper) GetName() string {
	return "NSFW"
}

func (s *NSFWMapper) Map(ctx context.Context, vc *models.VideoContent, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	return nil, nil
}

func (s *NSFWMapper) MapByID(ctx context.Context, videoID string, ct models.ContentType, force bool) (*models.VideoMetadata, error) {
	if strings.HasPrefix(videoID, "tpdb=") {
		videoID = "tpdb:" + strings.TrimPrefix(videoID, "tpdb=")
	} else if strings.HasPrefix(videoID, "tpdb_jav=") {
		videoID = "tpdb_jav:" + strings.TrimPrefix(videoID, "tpdb_jav=")
	} else if strings.HasPrefix(videoID, "stash=") {
		videoID = "stash:" + strings.TrimPrefix(videoID, "stash=")
	}

	if !strings.HasPrefix(videoID, "tpdb:") && !strings.HasPrefix(videoID, "tpdb_jav:") && !strings.HasPrefix(videoID, "stash:") {
		return nil, nil
	}

	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("db is nil")
	}

	// 1. Check local cache (models.GetMovieMetadataByVideoID)
	if !force {
		meta, err := models.GetMovieMetadataByVideoID(ctx, db, videoID)
		if err == nil && meta != nil {
			return meta.VideoMetadata, nil
		}
	}

	// 2. Fetch from local sidecar service
	host := os.Getenv("OMDB_API_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("OMDB_API_PORT")
	if port == "" {
		port = "8000"
	}
	key := os.Getenv("OMDB_API_KEY")
	if key == "" {
		key = "1799aaa7"
	}
	secure := os.Getenv("OMDB_API_SECURE") == "true"
	proto := "http"
	if secure {
		proto = "https"
	}

	u := fmt.Sprintf("%s://%s:%s/?apikey=%s&i=%s&porn=true", proto, host, port, key, url.QueryEscape(videoID))
	log.Infof("NSFWMapper: querying sidecar for metadata: %s", u)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar status: %d", resp.StatusCode)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	if r, ok := raw["Response"].(string); !ok || r != "True" {
		return nil, nil
	}

	title, _ := raw["Title"].(string)
	yearStr, _ := raw["Year"].(string)
	plot, _ := raw["Plot"].(string)
	posterURL, _ := raw["Poster"].(string)
	ratingStr, _ := raw["imdbRating"].(string)

	var yearVal *int16
	if yearStr != "" && yearStr != "N/A" {
		parts := strings.Split(yearStr, "-")
		if len(parts) > 0 {
			if y, err := strconv.Atoi(parts[0]); err == nil {
				y16 := int16(y)
				yearVal = &y16
			}
		}
	}

	var ratingVal *float64
	if ratingStr != "" && ratingStr != "N/A" {
		if r, err := strconv.ParseFloat(ratingStr, 64); err == nil {
			ratingVal = &r
		}
	}

	md := &models.VideoMetadata{
		VideoID:             videoID,
		Title:               title,
		Year:                yearVal,
		Plot:                plot,
		PosterURL:           posterURL,
		PosterHorizontalURL: posterURL,
		Rating:              ratingVal,
	}

	// Cache the result in omdb_info raw JSON to allow simple local lookups in the future
	otype := om.OmdbTypeMovie
	_, _ = om.UpsertInfo(ctx, db, videoID, otype, raw)

	log.Infof("NSFWMapper sidecar map success for %s", videoID)

	return md, nil
}

var _ MetadataMapper = (*NSFWMapper)(nil)
var _ DirectMapper = (*NSFWMapper)(nil)
