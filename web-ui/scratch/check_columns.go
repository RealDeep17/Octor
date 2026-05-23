package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-pg/pg/v10"
)

func main() {
	opt, err := pg.ParseURL("postgres://octor:octor@localhost:5433/octor?sslmode=disable")
	if err != nil {
		log.Fatalf("Parse error: %v", err)
	}
	db := pg.Connect(opt)
	defer db.Close()

	ctx := context.Background()

	var cols []struct {
		ColumnName string `pg:"column_name"`
		DataType   string `pg:"data_type"`
	}
	_, err = db.QueryContext(ctx, &cols, `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = 'movie_status'
	`)
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}

	fmt.Println("--- movie_status columns ---")
	for _, c := range cols {
		fmt.Printf("%s (%s)\n", c.ColumnName, c.DataType)
	}

	var statuses []struct {
		VideoID      string `pg:"video_id"`
		PosterLayout string `pg:"poster_layout"`
	}
	err = db.Model().Table("movie_status").Column("video_id", "poster_layout").Select(&statuses)
	if err != nil {
		log.Printf("Failed to select statuses: %v", err)
	} else {
		fmt.Println("--- movie_status records ---")
		for _, s := range statuses {
			fmt.Printf("VideoID: %s, PosterLayout: %s\n", s.VideoID, s.PosterLayout)
		}
	}
}
