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
	err = db.Model(&mms).Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d total movie metadata records:\n", len(mms))
	for _, mm := range mms {
		if mm.VideoMetadata != nil {
			fmt.Printf("VideoID: %s, Title: %s, PosterURL: %s, PosterHURL: %s\n",
				mm.VideoID, mm.Title, mm.PosterURL, mm.PosterHorizontalURL)
		} else {
			fmt.Printf("VideoID: %s, Title: %s (VideoMetadata is nil)\n", mm.VideoID, mm.Title)
		}
	}

	var sms []*models.SeriesMetadata
	err = db.Model(&sms).Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d total series metadata records:\n", len(sms))
	for _, sm := range sms {
		if sm.VideoMetadata != nil {
			fmt.Printf("VideoID: %s, Title: %s, PosterURL: %s, PosterHURL: %s\n",
				sm.VideoID, sm.Title, sm.PosterURL, sm.PosterHorizontalURL)
		} else {
			fmt.Printf("VideoID: %s, Title: %s (VideoMetadata is nil)\n", sm.VideoID, sm.Title)
		}
	}
}
