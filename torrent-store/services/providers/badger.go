package providers

import (
	"context"
	"os"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	badger "github.com/dgraph-io/badger/v3"
	"github.com/urfave/cli"
	ss "github.com/webtor-io/torrent-store/services"
)

const (
	BadgerExpireFlag = "badger-expire"
	BadgerPathFlag   = "badger-path"
)

func RegisterBadgerFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   BadgerExpireFlag,
			Usage:  "badger expire (sec); set 0 to disable ttl",
			Value:  3600,
			EnvVar: "BADGER_EXPIRE",
		},
		cli.StringFlag{
			Name:   BadgerPathFlag,
			Usage:  "badger database path",
			Value:  "badger_data",
			EnvVar: "BADGER_PATH",
		},
	)
}

type Badger struct {
	exp time.Duration
	db  *badger.DB
}

func NewBadger(c *cli.Context) *Badger {
	path := c.String(BadgerPathFlag)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(path, 0755)
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

func (s *Badger) entry(h string, val []byte) *badger.Entry {
	e := badger.NewEntry([]byte(h), val)
	if s.exp > 0 {
		e = e.WithTTL(s.exp)
	}
	return e
}

func (s *Badger) Touch(_ context.Context, h string) (ok bool, err error) {
	err = s.db.Update(func(txn *badger.Txn) error {
		i, err := txn.Get([]byte(h))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ss.ErrNotFound
		} else {
			err = i.Value(func(val []byte) error {
				return txn.SetEntry(s.entry(h, val))
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
		return txn.SetEntry(s.entry(h, torrent))
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
