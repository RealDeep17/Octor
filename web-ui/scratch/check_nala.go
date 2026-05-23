package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/webtor-io/web-ui/models"
)

func main() {
	opt, err := pg.ParseURL("postgres://octor:octor@localhost:5433/octor?sslmode=disable")
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	db := pg.Connect(opt)
	defer db.Close()

	ctx := context.Background()

	var movies []*models.Movie
	err = db.Model(&movies).
		Context(ctx).
		Relation("MovieMetadata").
		Select()
	if err != nil {
		log.Fatalf("Failed to query movies: %v", err)
	}

	for _, m := range movies {
		if m.Title == "Nala Brooks" || (m.Path != nil && (contains(*m.Path, "Nala") || contains(*m.Path, "LegalPorno"))) {
			fmt.Printf("MovieID: %s, Title: %s, Path: %s, ResourceID: %s\n", m.MovieID, m.Title, deref(m.Path), m.ResourceID)
			if m.MovieMetadata != nil {
				fmt.Printf("  Metadata: VideoID: %s, Title: %s, Year: %d, PosterURL: %s\n",
					m.MovieMetadata.VideoID, m.MovieMetadata.Title, derefInt16(m.MovieMetadata.Year), m.MovieMetadata.PosterURL)
			} else {
				fmt.Println("  Metadata: nil")
			}
		}
	}
}

func deref(p *string) string {
	if p == nil {
		return "nil"
	}
	return *p
}

func derefInt16(p *int16) int16 {
	if p == nil {
		return 0
	}
	return *p
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && strings.Contains(s, substr)))
}
