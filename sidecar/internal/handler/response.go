package handler

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/webtor-io/sidecar/internal/scene"
)

var rxYear = regexp.MustCompile(`(19|20)\d{2}`)

func yearFromDate(val string) string {
	if val == "" {
		return "N/A"
	}
	if m := rxYear.FindString(val); m != "" {
		return m
	}
	return "N/A"
}

func formatRuntime(val *float64) string {
	if val == nil {
		return "N/A"
	}
	seconds := *val
	minutes := int(math.Round(seconds / 60.0))
	if minutes < 1 {
		minutes = 1
	}
	return fmt.Sprintf("%d min", minutes)
}

func ToOmdb(s scene.Scene) scene.OmdbResponse {
	var performers []string
	for _, p := range s.Performers {
		if p.Name != "" {
			performers = append(performers, p.Name)
		}
	}

	actors := "N/A"
	if len(performers) > 0 {
		limit := len(performers)
		if limit > 8 {
			limit = 8
		}
		actors = strings.Join(performers[:limit], ", ")
	}

	genre := "Adult"
	if len(s.Tags) > 0 {
		limit := len(s.Tags)
		if limit > 8 {
			limit = 8
		}
		genre = strings.Join(s.Tags[:limit], ", ")
	}

	plot := "N/A"
	if s.Description != "" {
		plot = s.Description
	}

	poster := "N/A"
	if s.Poster != "" {
		poster = s.Poster
	}

	ratingStr := "N/A"
	if s.Rating != nil {
		ratingStr = fmt.Sprintf("%.1f", *s.Rating)
	}

	imdbID := s.Source + ":" + s.ID
	if s.ID == "" {
		imdbID = s.Source + ":" + s.Title
	}

	production := "N/A"
	if s.Site != "" {
		production = s.Site
	}

	website := "N/A"
	if s.URL != "" {
		website = s.URL
	}

	released := "N/A"
	if s.Date != "" {
		released = s.Date
	}

	return scene.OmdbResponse{
		Title:      s.Title,
		Year:       yearFromDate(s.Date),
		Rated:      "XXX",
		Released:   released,
		Runtime:    formatRuntime(s.Duration),
		Genre:      genre,
		Director:   "N/A",
		Actors:     actors,
		Plot:       plot,
		Language:   "N/A",
		Country:    "N/A",
		Awards:     "N/A",
		Poster:     poster,
		Ratings:    make([]any, 0),
		Metascore:  "N/A",
		ImdbRating: ratingStr,
		ImdbVotes:  "N/A",
		ImdbID:     imdbID,
		Type:       "movie",
		DVD:        "N/A",
		BoxOffice:  "N/A",
		Production: production,
		Website:    website,
		Response:   "True",
	}
}
