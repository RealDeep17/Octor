package services

import (
	"encoding/json"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	m "github.com/webtor-io/abuse-store/models"
	cs "github.com/webtor-io/common-services"
)

const (
	storeSyncIntervalFlag = "sync-interval"
)

var (
	ErrNotFound = errors.New("store: abuse not found")
)

func RegisterStoreFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   storeSyncIntervalFlag + ", si",
			Usage:  "sync interval in minutes",
			Value:  10,
			EnvVar: "STORE_SYNC_INTERVAL",
		},
	)
}

type Store struct {
	b  *badger.DB
	p  *cs.PG
	si time.Duration
}

func NewStore(c *cli.Context, b *badger.DB, p *cs.PG) *Store {
	return &Store{
		b:  b,
		p:  p,
		si: time.Duration(c.Int(storeSyncIntervalFlag)) * time.Minute,
	}
}

func (s *Store) Sync() error {
	pg := s.p.Get()
	if pg == nil {
		return errors.New("database not initialized")
	}
	log.Info("DB syncing started")
	err := pg.Model(&m.Abuse{}).ForEach(func(a *m.Abuse) error {
		err := s.pushToCache(a)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	log.Info("DB syncing finished")
	return nil
}

func (s *Store) Check(i string) error {
	return s.b.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(i))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrNotFound
		} else {
			return err
		}
	})
}

func (s *Store) Push(a *m.Abuse) error {
	pg := s.p.Get()
	_, err := pg.Model(a).Insert()
	if err != nil {
		return errors.Wrapf(err, "failed to push to db abuse=%+v", a)
	}
	err = s.pushToCache(a)
	if err != nil {
		return errors.Wrapf(err, "failed to push to cache abuse=%+v", a)
	}
	return nil
}

func (s *Store) pushToCache(a *m.Abuse) error {
	aa, err := json.Marshal(a)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal data abuse=%v", a)
	}
	return s.b.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(a.Infohash), aa)
		return txn.SetEntry(e)
	})
}

func (s *Store) Serve() error {
	ticker := time.NewTicker(s.si)
	for range ticker.C {
		err := s.Sync()
		if err != nil {
			log.WithError(err).Error("failed to sync db")
		}
	}
	return nil
}
