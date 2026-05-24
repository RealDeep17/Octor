package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-pg/migrations/v8"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	ac "github.com/webtor-io/web-ui/services/anthropic_client"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/migration"
)

func makeEnrichCMD() cli.Command {
	enrichCMD := cli.Command{
		Name:    "enrich",
		Aliases: []string{"e"},
		Usage:   "Enriches content with metadata",
	}
	configureEnrich(&enrichCMD)
	return enrichCMD
}

func configureEnrich(c *cli.Command) {
	runCmd := cli.Command{
		Name:   "run",
		Usage:  "Enriches specific torrent resources with metadata",
		Action: enrich,
	}
	runCmd.Flags = append(runCmd.Flags,
		cli.BoolFlag{
			Name:  "force",
			Usage: "force enrichment",
		},
		cli.BoolFlag{
			Name:  "force-error",
			Usage: "force error enrichment",
		},
		cli.StringFlag{
			Name:  "id",
			Usage: "id for enrichment",
		},
	)
	runCmd.Flags = cs.RegisterPGFlags(runCmd.Flags)
	runCmd.Flags = api.RegisterFlags(runCmd.Flags)
	runCmd.Flags = ac.RegisterFlags(runCmd.Flags)
	runCmd.Flags = configureEnricher(runCmd.Flags)

	popularCmd := cli.Command{
		Name:   "popular",
		Usage:  "Fetches popular recent films from metadata providers into the DB cache",
		Action: enrichPopular,
	}
	popularCmd.Flags = cs.RegisterPGFlags(popularCmd.Flags)
	popularCmd.Flags = ac.RegisterFlags(popularCmd.Flags)
	popularCmd.Flags = configureEnricher(popularCmd.Flags)
	popularCmd.Flags = append(popularCmd.Flags,
		cli.StringFlag{
			Name:   "release-date-gte",
			Usage:  "minimum release date (YYYY-MM-DD)",
			Value:  "2025-01-01",
			EnvVar: "ENRICH_POPULAR_RELEASE_DATE_GTE",
		},
		cli.IntFlag{
			Name:   "limit",
			Usage:  "max number of films to fetch",
			Value:  300,
			EnvVar: "ENRICH_POPULAR_LIMIT",
		},
		cli.BoolFlag{
			Name:  "force",
			Usage: "re-fetch and update all films even if already cached (useful after adding new metadata fields like credits)",
		},
	)

	c.Subcommands = []cli.Command{runCmd, popularCmd}
	
	refreshCmd := cli.Command{
		Name:   "refresh",
		Usage:  "Refreshes stale or missing metadata for previously enriched torrent resources",
		Action: refreshEnrich,
	}
	refreshCmd.Flags = cs.RegisterPGFlags(refreshCmd.Flags)
	refreshCmd.Flags = api.RegisterFlags(refreshCmd.Flags)
	refreshCmd.Flags = ac.RegisterFlags(refreshCmd.Flags)
	refreshCmd.Flags = configureEnricher(refreshCmd.Flags)
	refreshCmd.Flags = append(refreshCmd.Flags,
		cli.IntFlag{
			Name:   "days",
			Usage:  "number of days after which metadata is considered stale",
			Value:  7,
			EnvVar: "ENRICH_REFRESH_DAYS",
		},
	)

	c.Subcommands = []cli.Command{runCmd, popularCmd, refreshCmd}
}

func enrichPopular(c *cli.Context) error {
	releaseDateGte := c.String("release-date-gte")
	limit := c.Int("limit")
	force := c.Bool("force")

	pg := cs.NewPG(c)
	defer pg.Close()

	col := migrations.NewCollection()
	m := migration.NewPGMigration(pg, col)
	if err := m.Run(); err != nil {
		return err
	}

	cl := http.DefaultClient
	// api.Api is not needed for popular — only the enricher's metadata
	// mappers, which are wired through makeEnricher. We pass a nil api
	// since popular flow never hits the REST API. The AI resolver also
	// stays disabled here: popular cron deals with already-known TMDB
	// ids by definition.
	en := makeEnricher(c, cl, pg, nil, ac.New(c))

	log.WithFields(log.Fields{
		"release_date_gte": releaseDateGte,
		"limit":            limit,
		"force":            force,
	}).Info("starting enrich popular")

	ctx := context.Background()
	if err := en.RefreshPopular(ctx, releaseDateGte, limit, force); err != nil {
		return errors.Wrap(err, "enrich popular failed")
	}

	log.Info("enrich popular completed")
	return nil
}

func enrich(c *cli.Context) error {
	force := c.Bool("force")
	forceError := c.Bool("force-error")
	if forceError {
		force = true
	}
	id := c.String("id")
	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	// Setting Migrations
	col := migrations.NewCollection()
	m := migration.NewPGMigration(pg, col)
	err := m.Run()
	if err != nil {
		return err
	}
	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Octor API
	sapi := api.New(c, cl)

	// Setting Enricher (with optional AI fallback wired from --ai-enrich-* flags)
	en := makeEnricher(c, cl, pg, sapi, ac.New(c))

	var resources []*models.TorrentResource
	ctx := context.Background()
	if id != "" {
		r, err := models.GetResourceByID(ctx, db, id)
		if err != nil {
			return err
		}
		// torrent_resource is only populated when a resource lands in
		// Library; resources that were only streamed have no row.
		// Enrichment itself only needs the hash, so synthesize a
		// minimal TorrentResource and continue.
		if r == nil {
			log.WithField("id", id).Info("no torrent_resource row, enriching by hash directly")
			r = &models.TorrentResource{ResourceID: id}
		}
		resources = append(resources, r)
	} else {
		if forceError {
			resources, err = models.GetErrorResources(ctx, db)
		} else if force {
			resources, err = models.GetAllResources(ctx, db)
		} else {
			resources, err = models.GetResourcesWithoutMediaInfo(ctx, db)
		}
	}
	if err != nil {
		return err
	}

	log.Infof("enrich: processing %d resources with %d concurrent workers", len(resources), en.Concurrency)

	sem := make(chan struct{}, en.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, resource := range resources {
		resource := resource
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			err := en.Enrich(ctx, resource.ResourceID, &api.Claims{}, force, "")
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func refreshEnrich(c *cli.Context) error {
	days := c.Int("days")
	if days <= 0 {
		days = 7
	}
	staleThreshold := time.Duration(days) * 24 * time.Hour

	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	// Setting Migrations
	col := migrations.NewCollection()
	m := migration.NewPGMigration(pg, col)
	err := m.Run()
	if err != nil {
		return err
	}
	db := pg.Get()
	if db == nil {
		return errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Octor API
	sapi := api.New(c, cl)

	// Setting Enricher
	en := makeEnricher(c, cl, pg, sapi, ac.New(c))

	ctx := context.Background()
	ids, err := models.GetStaleOrMissingMetadataResourceIDs(ctx, db, staleThreshold)
	if err != nil {
		return err
	}

	total := len(ids)
	log.Infof("found %d resources with stale, missing, or error metadata to refresh. Running with %d concurrent workers", total, en.Concurrency)

	sem := make(chan struct{}, en.Concurrency)
	var wg sync.WaitGroup

	for i, id := range ids {
		id := id
		i := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			log.Infof("[%d/%d] refreshing metadata for resource %s", i+1, total, id)
			err := en.Enrich(ctx, id, &api.Claims{}, true, "")
			if err != nil {
				log.WithError(err).Warnf("failed to refresh metadata for resource %s", id)
			}
		}()
	}
	wg.Wait()

	return nil
}
