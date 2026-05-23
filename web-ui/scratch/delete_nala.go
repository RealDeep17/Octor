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

	// 1. Delete movie records for Nala Brooks
	var movies []*models.Movie
	err = db.Model(&movies).Where("title ILIKE ?", "%nala%").Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}
	for _, m := range movies {
		// Delete media_info
		res, err := db.Model((*models.MediaInfo)(nil)).Where("resource_id = ?", m.ResourceID).Delete()
		if err != nil {
			log.Fatalf("Failed to delete media_info for %s: %v", m.ResourceID, err)
		}
		fmt.Printf("Deleted %d media_info records for resource %s\n", res.RowsAffected(), m.ResourceID)

		// Delete movie
		res, err = db.Model((*models.Movie)(nil)).Where("movie_id = ?", m.MovieID).Delete()
		if err != nil {
			log.Fatalf("Failed to delete movie %v: %v", m.MovieID, err)
		}
		fmt.Printf("Deleted %d movie records for %s\n", res.RowsAffected(), m.Title)
	}

	// 2. Delete movie_metadata record
	res, err := db.Model((*models.MovieMetadata)(nil)).
		Where("video_id = ?", "tpdb:1016276").
		Delete()
	if err != nil {
		log.Fatalf("Failed to delete movie_metadata: %v", err)
	}
	fmt.Printf("Deleted %d movie_metadata records\n", res.RowsAffected())
}
