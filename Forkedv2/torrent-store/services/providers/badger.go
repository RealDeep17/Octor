package providers

import (
	"context"
	"os"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"time"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/urfave/cli"
	ss "github.com/webtor-io/torrent-store/services"
)

const (
	BadgerExpireFlag = "badger-expire"
)

func RegisterBadgerFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   BadgerExpireFlag,
			Usage:  "badger expire (sec)",
			Value:  3600,
			EnvVar: "BADGER_EXPIRE",
		},
	)
}

type Badger struct {
	exp time.Duration
	db  *badger.DB
}

func NewBadger(c *cli.Context) *Badger {
	path := "badger_data"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.Mkdir(path, 0755)
	}
	opt := badger.DefaultOptions(path)
	db, err := badger.Open(opt)
	if err != nil {
		log.WithError(err).Fatal("failed to open badger")
	}
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
	return &Badger{
		exp: time.Duration(c.Int(BadgerExpireFlag)) * time.Second,
		db:  db,
	}
}

func (s *Badger) Name() string {
	return "badger"
}

func (s *Badger) Touch(_ context.Context, h string) (ok bool, err error) {
	err = s.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte(h))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ss.ErrNotFound
		} else {
			err = i.Value(func(val []byte) error {
				e := badger.NewEntry([]byte(h), val).WithTTL(s.exp)
				return txn.SetEntry(e)
			})
			return err
		}
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Badger) Push(_ context.Context, h string, torrent []byte) (ok bool, err error) {
	err = s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(h), torrent).WithTTL(s.exp)
		return txn.SetEntry(e)
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Badger) Pull(_ context.Context, h string) (torrent []byte, err error) {
	err = s.db.View(func(txn *badger.Txn) (err error) {
		i, err := txn.Get([]byte(h))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ss.ErrNotFound
		} else {
			err = i.Value(func(val []byte) error {
				torrent = val
				return nil
			})
			return
		}
	})
	return
}

func (s *Badger) Close() {
	_ = s.db.Close()
}

var _ ss.StoreProvider = (*Badger)(nil)
