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

	var sms []*models.SeriesMetadata
	err = db.Model(&sms).
		Where("poster_horizontal_url IS NOT NULL AND poster_horizontal_url != ?", "").
		Limit(10).
		Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d series metadata records with horizontal posters:\n", len(sms))
	for _, sm := range sms {
		fmt.Printf("VideoID: %s, Title: %s, PosterURL: %s, PosterHURL: %s\n",
			sm.VideoID, sm.Title, sm.PosterURL, sm.PosterHorizontalURL)
	}
}
