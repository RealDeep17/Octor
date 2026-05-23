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
	err = db.Model(&movies).Select()
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Printf("Found %d movies in the database:\n", len(movies))
	for _, m := range movies {
		fmt.Printf("ResourceID: %s, Title: %s\n", m.ResourceID, m.Title)
	}
}
