package main

import (
	"context"
	"net/http"

	"github.com/go-pg/migrations/v8"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	services "github.com/webtor-io/common-services"
	ra "github.com/webtor-io/rest-api/services"
	m "github.com/webtor-io/web-ui/migrations"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/migration"
)

func makePGMigrationCMD() cli.Command {
	migrateCmd := cli.Command{
		Name:    "migrate",
		Aliases: []string{"m"},
		Usage:   "Migrates database",
	}
	configurePGMigration(&migrateCmd)
	return migrateCmd
}

func configurePGMigration(c *cli.Command) {
	upCmd := cli.Command{
		Name:    "up",
		Usage:   "Runs all available migrations",
		Aliases: []string{"u"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "up")
		},
	}
	downCmd := cli.Command{
		Name:    "down",
		Usage:   "Reverts last migration",
		Aliases: []string{"d"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "down")
		},
	}
	resetCmd := cli.Command{
		Name:    "reset",
		Usage:   "Reverts all migrations",
		Aliases: []string{"r"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "reset")
		},
	}
	versionCmd := cli.Command{
		Name:    "version",
		Usage:   "Prints current db version",
		Aliases: []string{"v"},
		Action: func(c *cli.Context) error {
			return pgMigrate(c, "version")
		},
	}
	backfillCmd := cli.Command{
		Name:    "backfill-sizes",
		Usage:   "Backfills file sizes into episode metadata",
		Action: func(c *cli.Context) error {
			return pgBackfillSizes(c)
		},
	}
	c.Subcommands = []cli.Command{upCmd, downCmd, resetCmd, versionCmd, backfillCmd}
	for k, _ := range c.Subcommands {
		configureSubPGMigration(&c.Subcommands[k])
	}
}
func configureSubPGMigration(c *cli.Command) {
	c.Flags = services.RegisterPGFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
}

func pgMigrate(c *cli.Context, a ...string) error {
	// Setting DB
	db := services.NewPG(c)
	defer db.Close()

	// Setting PGMigrations
	col := migrations.NewCollection()
	mgr := migration.NewPGMigration(db, col)

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Api
	sapi := api.New(c, cl)

	// Setting Claims Client
	cpCl := claims.NewClient(c)
	if cpCl != nil {
		defer cpCl.Close()
	}

	// Setting custom migrations
	m.PopulateTorrentSizeBytes(col, sapi)
	m.PopulateUserTiers(col, cpCl)

	// Run
	return mgr.Run(a...)
}

func pgBackfillSizes(c *cli.Context) error {
	// 1. Connect DB
	pgConn := services.NewPG(c)
	defer pgConn.Close()
	db := pgConn.Get()

	// 2. Setting HTTP Client
	cl := http.DefaultClient

	// 3. Setting Api
	sapi := api.New(c, cl)

	// 4. Query all episodes where metadata does not have "size" or size is 0
	var episodes []*models.Episode
	ctx := context.Background()
	err := db.Model(&episodes).
		Where("metadata->>'size' IS NULL OR metadata->>'size' = '0'").
		Select()
	if err != nil {
		return err
	}

	log.Infof("Found %d episodes needing size backfill", len(episodes))

	// Cache file sizes per resourceID to avoid duplicate API requests
	fileSizes := map[string]map[string]int64{}

	claims := &api.Claims{}

	for _, ep := range episodes {
		if ep.Path == nil || *ep.Path == "" {
			continue
		}
		path := *ep.Path
		resourceID := ep.ResourceID

		sizes, ok := fileSizes[resourceID]
		if !ok {
			log.Infof("Fetching file list for resource %s", resourceID)
			resp, err := sapi.ListResourceContent(ctx, claims, resourceID, &api.ListResourceContentArgs{
				Limit:  1000,
				Path:   "/",
				Output: api.OutputList,
			})
			if err != nil {
				log.Errorf("Failed to list resource %s content: %v", resourceID, err)
				continue
			}
			sizes = map[string]int64{}
			if resp != nil {
				for _, item := range resp.Items {
					if item.Type == ra.ListTypeFile {
						sizes[item.PathStr] = item.Size
					}
				}
			}
			fileSizes[resourceID] = sizes
		}

		if size, found := sizes[path]; found {
			if ep.Metadata == nil {
				ep.Metadata = map[string]any{}
			}
			ep.Metadata["size"] = size
			_, err = db.Model(ep).WherePK().Column("metadata").Update()
			if err != nil {
				log.Errorf("Failed to update episode %s: %v", ep.EpisodeID, err)
				return err
			}
			log.Infof("Updated episode %s (%s) size: %d bytes", ep.EpisodeID, path, size)
		} else {
			log.Warnf("Could not find file size for path %s in resource %s", path, resourceID)
		}
	}
	log.Info("Backfill complete!")
	return nil
}
