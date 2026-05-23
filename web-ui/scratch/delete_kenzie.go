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

	hash := "4828f66fd2b84e8ba83930515f47b753ab59e2af"

	// 1. Delete movie records
	res, err := db.Model((*models.Movie)(nil)).
		Where("resource_id = ?", hash).
		Delete()
	if err != nil {
		log.Fatalf("Failed to delete movies: %v", err)
	}
	fmt.Printf("Deleted %d movie records\n", res.RowsAffected())

	// 2. Delete media_info record
	res, err = db.Model((*models.MediaInfo)(nil)).
		Where("resource_id = ?", hash).
		Delete()
	if err != nil {
		log.Fatalf("Failed to delete media_info: %v", err)
	}
	fmt.Printf("Deleted %d media_info records\n", res.RowsAffected())

	// 3. Delete incorrect movie_metadata records
	res, err = db.Model((*models.MovieMetadata)(nil)).
		Where("video_id = ?", "tpdb:1305862").
		Delete()
	if err != nil {
		log.Fatalf("Failed to delete movie_metadata: %v", err)
	}
	fmt.Printf("Deleted %d movie_metadata records\n", res.RowsAffected())
}
