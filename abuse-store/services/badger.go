package services

import (
	"errors"
	"time"

	"github.com/dgraph-io/badger/v3"
	log "github.com/sirupsen/logrus"
)

func NewBadger() *badger.DB {
	opt := badger.DefaultOptions("/tmp/badger")
	db, _ := badger.Open(opt)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := db.RunValueLogGC(0.7); err != nil {
				if !errors.Is(err, badger.ErrNoRewrite) {
					log.WithError(err).Error("badger GC error")
				}
				continue
			}
		}
	}()
	return db
}
