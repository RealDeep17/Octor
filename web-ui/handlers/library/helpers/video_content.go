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
