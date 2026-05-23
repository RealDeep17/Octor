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

	res, err := db.Exec("DELETE FROM movie WHERE resource_id IN (?, ?)", "bec87974c98a4716fda093f82b9b97305fd90871", "4828f66fd2b84e8ba83930515f47b753ab59e2af")
	if err != nil {
		log.Fatalf("Delete error: %v", err)
	}
	fmt.Printf("Raw deleted %d movie records\n", res.RowsAffected())
}
