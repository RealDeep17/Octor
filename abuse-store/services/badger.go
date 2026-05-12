package services

import (
	"time"

	"github.com/dgraph-io/badger/v3"
)

func NewBadger() *badger.DB {
	opt := badger.DefaultOptions("/tmp/badger")
	db, _ := badger.Open(opt)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := db.RunValueLogGC(0.7); err != nil {
				return
			}
		}
	}()
	return db
}
