package services

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
	"github.com/webtor-io/claims-provider/models"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/lazymap"
)

const (
	storeCacheConcurrencyFlag = "store-cache-concurrency"
	storeCacheExpireFlag      = "store-cache-expire"
	storeCacheErrorExpireFlag = "store-cache-error-expire"
	storeCacheCapacityFlag    = "store-cache-capacity"
	storeDBTimeoutFlag        = "store-db-timeout"
)

// RegisterStoreFlags registers CLI flags/env vars for tuning the store/cache behavior.
func RegisterStoreFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.IntFlag{
			Name:   storeCacheConcurrencyFlag,
			Usage:  "maximum concurrent cache builders",
			Value:  10,
			EnvVar: "STORE_CACHE_CONCURRENCY",
		},
		cli.DurationFlag{
			Name:   storeCacheExpireFlag,
			Usage:  "cache expiration for successful entries (e.g. 60s, 5m)",
			Value:  60 * time.Second,
			EnvVar: "STORE_CACHE_EXPIRE",
		},
		cli.DurationFlag{
			Name:   storeCacheErrorExpireFlag,
			Usage:  "cache expiration for error entries (e.g. 10s)",
			Value:  10 * time.Second,
			EnvVar: "STORE_CACHE_ERROR_EXPIRE",
		},
		cli.IntFlag{
			Name:   storeCacheCapacityFlag,
			Usage:  "cache capacity (max entries)",
			Value:  1000,
			EnvVar: "STORE_CACHE_CAPACITY",
		},
		cli.DurationFlag{
			Name:   storeDBTimeoutFlag,
			Usage:  "database query timeout (e.g. 5s)",
			Value:  5 * time.Second,
			EnvVar: "STORE_DB_TIMEOUT",
		},
	)
}

type Store struct {
	lazymap.LazyMap[*models.Claims]
	pg               *cs.PG
	dbTimeout        time.Duration
	fetch            func(ctx context.Context, email string) (*models.Claims, error)
	fetchByPatreonID func(ctx context.Context, patreonID string) (*models.Claims, error)
}

func NewStore(c *cli.Context, pg *cs.PG) *Store {
	return &Store{
		pg: pg,
		LazyMap: lazymap.New[*models.Claims](&lazymap.Config{
			Concurrency: c.Int(storeCacheConcurrencyFlag),
			Expire:      c.Duration(storeCacheExpireFlag),
			ErrorExpire: c.Duration(storeCacheErrorExpireFlag),
			Capacity:    c.Int(storeCacheCapacityFlag),
		}),
		dbTimeout: c.Duration(storeDBTimeoutFlag),
	}
}

func (s *Store) get(ctx context.Context, email string) (claims *models.Claims, err error) {
	claims = &models.Claims{}
	if s.pg == nil {
		return nil, errors.New("database is not initialized")
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, s.dbTimeout)
	defer cancel()
	_, err = db.QueryOneContext(ctx, claims, `select * from public.get_member_claims_by_email(?)`, email)
	if err != nil {
		return nil, err
	}
	return
}

func (s *Store) getByPatreonID(ctx context.Context, patreonID string) (claims *models.Claims, err error) {
	claims = &models.Claims{}
	if s.pg == nil {
		return nil, errors.New("database is not initialized")
	}
	db := s.pg.Get()
	if db == nil {
		return nil, errors.New("database connection is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, s.dbTimeout)
	defer cancel()
	_, err = db.QueryOneContext(ctx, claims, `select * from public.get_member_claims_by_patreon_id(?)`, patreonID)
	if err != nil {
		return nil, err
	}
	return
}

func (s *Store) GetByEmail(ctx context.Context, email string) (claims *models.Claims, err error) {
	builder := func() (*models.Claims, error) { return s.get(ctx, email) }
	if s.fetch != nil {
		builder = func() (*models.Claims, error) { return s.fetch(ctx, email) }
	}
	return s.LazyMap.Get("email:"+email, builder)
}

func (s *Store) GetByPatreonID(ctx context.Context, patreonID string) (claims *models.Claims, err error) {
	builder := func() (*models.Claims, error) { return s.getByPatreonID(ctx, patreonID) }
	if s.fetchByPatreonID != nil {
		builder = func() (*models.Claims, error) { return s.fetchByPatreonID(ctx, patreonID) }
	}
	return s.LazyMap.Get("patreon:"+patreonID, builder)
}
