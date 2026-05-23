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

	var movies []*models.Movie
	err = db.Model(&movies).
		Where("title ILIKE ?", "%kenzie%").
		Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d movie records:\n", len(movies))
	for _, m := range movies {
		fmt.Printf("MovieID: %v, ResourceID: %s, Title: %s, Path: %v\n",
			m.MovieID, m.ResourceID, m.Title, *m.Path)
	}

	var mis []*models.MediaInfo
	err = db.Model(&mis).
		Where("resource_id IN (SELECT resource_id FROM movie WHERE title ILIKE ?)", "%kenzie%").
		Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}
	fmt.Printf("Found %d media info records:\n", len(mis))
	for _, mi := range mis {
		fmt.Printf("ResourceID (Hash): %s, Status: %v\n", mi.ResourceID, mi.Status)
	}
}
