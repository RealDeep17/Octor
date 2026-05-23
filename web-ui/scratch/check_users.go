package main

import (
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



	var users []struct {
		ID    string `pg:"id"`
		Email string `pg:"email"`
	}
	err = db.Model().Table("user").Column("id", "email").Select(&users)
	if err != nil {
		log.Printf("Failed to select users: %v", err)
	} else {
		fmt.Println("--- Users in DB ---")
		for _, u := range users {
			fmt.Printf("UserID: %s, Email: %s\n\n", u.ID, u.Email)
		}
	}

	var statuses []struct {
		UserID       string `pg:"user_id"`
		VideoID      string `pg:"video_id"`
		PosterLayout string `pg:"poster_layout"`
	}
	err = db.Model().Table("movie_status").Column("user_id", "video_id", "poster_layout").Select(&statuses)
	if err != nil {
		log.Printf("Failed to select statuses: %v", err)
	} else {
		fmt.Println("--- movie_status records ---")
		for _, s := range statuses {
			fmt.Printf("UserID: %s, VideoID: %s, PosterLayout: %s\n", s.UserID, s.VideoID, s.PosterLayout)
		}
	}
}
