package main

import (
	"fmt"
	"log"

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

	var mms []*models.MovieMetadata
	err = db.Model(&mms).
		Where("poster_horizontal_url IS NOT NULL AND poster_horizontal_url != ?", "").
		Limit(10).
		Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d movie metadata records with horizontal posters:\n", len(mms))
	for _, mm := range mms {
		fmt.Printf("VideoID: %s, Title: %s, PosterURL: %s, PosterHURL: %s\n",
			mm.VideoID, mm.Title, mm.PosterURL, mm.PosterHorizontalURL)
	}
}
