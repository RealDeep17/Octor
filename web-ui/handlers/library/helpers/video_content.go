package helpers

import (
	"fmt"
	"strings"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/enrich"
)

type VideoContentHelper struct{}

func NewVideoContentHelper() *VideoContentHelper {
	return &VideoContentHelper{}
}

func (s *VideoContentHelper) GetTitle(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().Title
	}
	return m.GetContent().Title
}

func (s *VideoContentHelper) HasYear(m models.VideoContentWithMetadata) bool {
	return s.GetYear(m) != 0
}

func (s *VideoContentHelper) GetYear(m models.VideoContentWithMetadata) int {
	if m.GetMetadata() != nil && m.GetMetadata().Year != nil {
		return int(*m.GetMetadata().Year)
	}
	if m.GetContent().Year != nil {
		return int(*m.GetContent().Year)
	}
	return 0
}

func (s *VideoContentHelper) HasRating(m models.VideoContentWithMetadata) bool {
	return s.GetRating(m) > 0
}

func (s *VideoContentHelper) GetRating(m models.VideoContentWithMetadata) float64 {
	if m.GetMetadata() != nil && m.GetMetadata().Rating != nil {
		return *m.GetMetadata().Rating
	}
	return 0
}

func (s *VideoContentHelper) HasPoster(m models.VideoContentWithMetadata) bool {
	return s.GetOriginalPoster(m) != ""
}

func (s *VideoContentHelper) GetOriginalPoster(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().PosterURL
	}
	return ""
}

func (s *VideoContentHelper) HasPosterHorizontal(m models.VideoContentWithMetadata) bool {
	if m.GetMetadata() == nil {
		return false
	}
	posterURL := m.GetMetadata().PosterURL
	videoID := m.GetMetadata().VideoID
	return m.GetMetadata().PosterHorizontalURL != "" ||
		strings.Contains(posterURL, "theporndb.net") ||
		strings.Contains(posterURL, "stashdb.org") ||
		strings.HasPrefix(videoID, "tpdb:") ||
		strings.HasPrefix(videoID, "tpdb=") ||
		strings.HasPrefix(videoID, "tpdb_jav:") ||
		strings.HasPrefix(videoID, "tpdb_jav=") ||
		strings.HasPrefix(videoID, "stash:") ||
		strings.HasPrefix(videoID, "stash=")
}

func (s *VideoContentHelper) GetPosterLayout(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() == nil {
		return "vertical"
	}
	if !s.HasPosterHorizontal(m) {
		return "vertical"
	}
	layout := m.GetUserPosterLayout()
	if layout != "" {
		return layout
	}
	// For adult content/scenes, default to horizontal layout to prevent vertical cropping of horizontal posters
	posterURL := m.GetMetadata().PosterURL
	videoID := m.GetMetadata().VideoID
	if strings.Contains(posterURL, "theporndb.net") ||
		strings.Contains(posterURL, "stashdb.org") ||
		strings.HasPrefix(videoID, "tpdb:") ||
		strings.HasPrefix(videoID, "tpdb=") ||
		strings.HasPrefix(videoID, "tpdb_jav:") ||
		strings.HasPrefix(videoID, "tpdb_jav=") ||
		strings.HasPrefix(videoID, "stash:") ||
		strings.HasPrefix(videoID, "stash=") {
		return "horizontal"
	}
	return "vertical"
}

func (s *VideoContentHelper) GetOriginalPosterHorizontal(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().PosterHorizontalURL
	}
	return ""
}

func (s *VideoContentHelper) HasVideoID(m models.VideoContentWithMetadata) bool {
	return m.GetMetadata() != nil && m.GetMetadata().VideoID != ""
}

func (s *VideoContentHelper) GetVideoID(m models.VideoContentWithMetadata) string {
	if m.GetMetadata() != nil {
		return m.GetMetadata().VideoID
	}
	return ""
}

func (s *VideoContentHelper) GetResourceID(m models.VideoContentWithMetadata) string {
	if m == nil || m.GetContent() == nil {
		return ""
	}
	return m.GetContent().ResourceID
}

func (s *VideoContentHelper) GetUserWatched(m models.VideoContentWithMetadata) bool {
	if m == nil {
		return false
	}
	switch v := m.(type) {
	case *models.Movie:
		return v.UserWatched
	case *models.Series:
		return v.UserWatched
	}
	return false
}

func (s *VideoContentHelper) GetUserRating(m models.VideoContentWithMetadata) *int16 {
	if m == nil {
		return nil
	}
	switch v := m.(type) {
	case *models.Movie:
		return v.UserRating
	case *models.Series:
		return v.UserRating
	}
	return nil
}

func (s *VideoContentHelper) GetVideoType(m models.VideoContentWithMetadata) string {
	return string(m.GetContentType())
}

func (s *VideoContentHelper) GetCachedPoster240(m models.VideoContentWithMetadata) string {
	return fmt.Sprintf("/lib/%v/poster/%v/240.jpg", m.GetContentType(), m.GetMetadata().VideoID)
}

func (s *VideoContentHelper) GetCachedPosterHorizontal500(m models.VideoContentWithMetadata) string {
	return fmt.Sprintf("/lib/%v/poster-h/%v/500.jpg", m.GetContentType(), m.GetMetadata().VideoID)
}

func (s *VideoContentHelper) GetCachedPosterHorizontal480(m models.VideoContentWithMetadata) string {
	return fmt.Sprintf("/lib/%v/poster-h/%v/480.jpg", m.GetContentType(), m.GetMetadata().VideoID)
}

func (s *VideoContentHelper) GetCachedPosterHorizontal720(m models.VideoContentWithMetadata) string {
	return fmt.Sprintf("/lib/%v/poster-h/%v/720.jpg", m.GetContentType(), m.GetMetadata().VideoID)
}

func (s *VideoContentHelper) HasEpisodeStill(ep *models.Episode) bool {
	return ep.EpisodeMetadata != nil && ep.EpisodeMetadata.StillURL != nil && *ep.EpisodeMetadata.StillURL != ""
}

func (s *VideoContentHelper) GetCachedEpisodeStill(ep *models.Episode, width int) string {
	if ep.EpisodeMetadata == nil || ep.Season == nil || ep.Episode == nil {
		return ""
	}
	return fmt.Sprintf("/lib/episode/still/%v/%v/%v/%v.jpg", ep.EpisodeMetadata.VideoID, *ep.Season, *ep.Episode, width)
}

func (s *VideoContentHelper) GetEpisodeTitle(ep *models.Episode) string {
	if ep.EpisodeMetadata != nil && ep.EpisodeMetadata.Title != nil && *ep.EpisodeMetadata.Title != "" {
		return *ep.EpisodeMetadata.Title
	}
	if ep.Title != nil {
		return *ep.Title
	}
	if ep.Episode != nil {
		return fmt.Sprintf("Episode %d", *ep.Episode)
	}
	return ""
}

func (s *VideoContentHelper) GetEpisodePlot(ep *models.Episode) string {
	if ep.EpisodeMetadata != nil && ep.EpisodeMetadata.Plot != nil {
		return *ep.EpisodeMetadata.Plot
	}
	return ""
}

func (s *VideoContentHelper) GetStudio(m models.VideoContentWithMetadata) string {
	if m == nil {
		return ""
	}
	studio := ""
	if m.GetContent() != nil && m.GetContent().Metadata != nil {
		if dir, ok := m.GetContent().Metadata["Director"].(string); ok && dir != "" && dir != "N/A" {
			studio = dir
		}
	}
	if studio == "" && m.GetPath() != nil {
		if _, s := enrich.IsAdultPath(*m.GetPath()); s != "" {
			studio = s
		}
	}
	return studio
}

// GetEpisodeSummary returns a short human-readable string of which episodes
// are available in the library for this series item, e.g.:
//   "S1E3 — Ozymandias"   (single episode with title)
//   "S2 · 6 Episodes"     (multiple episodes, same season)
//   "3 Seasons · 24 Episodes"
func (s *VideoContentHelper) GetEpisodeSummary(m models.VideoContentWithMetadata) string {
	ser, ok := m.(*models.Series)
	if !ok || len(ser.Episodes) == 0 {
		return ""
	}
	eps := ser.Episodes
	if len(eps) == 1 {
		ep := eps[0]
		var sea, epNum int16
		if ep.Season != nil {
			sea = *ep.Season
		}
		if ep.Episode != nil {
			epNum = *ep.Episode
		}
		title := ""
		if ep.EpisodeMetadata != nil && ep.EpisodeMetadata.Title != nil && *ep.EpisodeMetadata.Title != "" {
			title = *ep.EpisodeMetadata.Title
			runes := []rune(title)
			if len(runes) > 32 {
				title = string(runes[:32]) + "…"
			}
		}
		if title != "" {
			return fmt.Sprintf("S%dE%d — %s", sea, epNum, title)
		}
		return fmt.Sprintf("S%dE%d", sea, epNum)
	}
	// Multiple episodes — group by season.
	seasons := map[int16]int{}
	for _, ep := range eps {
		if ep.Season != nil {
			seasons[*ep.Season]++
		}
	}
	if len(seasons) == 1 {
		var sea int16
		for k := range seasons {
			sea = k
		}
		if len(eps) == 1 {
			return fmt.Sprintf("S%d · 1 Episode", sea)
		}
		return fmt.Sprintf("S%d · %d Episodes", sea, len(eps))
	}
	return fmt.Sprintf("%d Seasons · %d Episodes", len(seasons), len(eps))
}

// GetCardHref returns the correct href for a library card.
// Series with a known VideoID go to /series/<video_id> (the episode selector page).
// Everything else goes to /<resource_id> (the torrent file browser).
func (s *VideoContentHelper) GetCardHref(m models.VideoContentWithMetadata) string {
	if ser, ok := m.(*models.Series); ok {
		if ser.SeriesMetadata != nil && ser.SeriesMetadata.VideoMetadata != nil {
			if ser.SeriesMetadata.VideoMetadata.VideoID != "" {
				return "/series/" + ser.SeriesMetadata.VideoMetadata.VideoID
			}
		}
	}
	return "/" + m.GetContent().ResourceID
}

func (s *VideoContentHelper) IsAdultJAV(m models.VideoContentWithMetadata) bool {
	if m == nil || m.GetContent() == nil {
		return false
	}
	if m.GetMetadata() != nil {
		videoID := m.GetMetadata().VideoID
		if strings.HasPrefix(videoID, "tpdb_jav:") || strings.HasPrefix(videoID, "tpdb_jav=") {
			return true
		}
	}
	if m.GetPath() != nil {
		if strings.Contains(strings.ToLower(*m.GetPath()), "jav") {
			return true
		}
	}
	return false
}

func (s *VideoContentHelper) IsSeriesAnime(m models.VideoContentWithMetadata) bool {
	if m == nil {
		return false
	}
	if ser, ok := m.(*models.Series); ok {
		return ser.IsAnime
	}
	return false
}
