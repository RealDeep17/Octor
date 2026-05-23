package main

import (
	"context"
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

	ctx := context.Background()

	mm, err := models.GetMovieMetadataByVideoID(ctx, db, "tt0458352")
	if err != nil {
		log.Fatalf("Failed to get metadata: %v", err)
	}

	if mm == nil {
		fmt.Println("GetMovieMetadataByVideoID returned nil!")
		return
	}

	fmt.Printf("Success! MovieMetadataID: %s\n", mm.MovieMetadataID)
	if mm.VideoMetadata == nil {
		fmt.Println("VideoMetadata is nil!")
	} else {
		fmt.Printf("VideoID: %s, Title: %s, PosterURL: %s, PosterHURL: %s\n",
			mm.VideoID, mm.Title, mm.PosterURL, mm.PosterHorizontalURL)
	}
}
