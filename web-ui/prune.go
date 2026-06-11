package main

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	services "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
)

func makePruneCMD() cli.Command {
	pruneCMD := cli.Command{
		Name:   "prune",
		Usage:  "Prunes inactive one-timer resources from the database",
		Action: pruneOneTimers,
	}
	pruneCMD.Flags = []cli.Flag{
		cli.IntFlag{
			Name:   "days",
			Usage:  "number of days after which inactive resources are considered expired",
			Value:  30,
			EnvVar: "DB_PRUNE_DAYS",
		},
	}
	pruneCMD.Flags = services.RegisterPGFlags(pruneCMD.Flags)
	return pruneCMD
}

func pruneOneTimers(c *cli.Context) error {
	days := c.Int("days")
	if days <= 0 {
		days = 30
	}
	olderThan := time.Duration(days) * 24 * time.Hour

	// Setting DB
	pg := services.NewPG(c)
	defer pg.Close()

	db := pg.Get()
	if db == nil {
		log.Warn("database connection not available for prune")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	log.Infof("running database cleanup for inactive resources older than %d days...", days)
	prunedCount, err := models.PruneOneTimers(ctx, db, olderThan)
	if err != nil {
		log.WithError(err).Error("database pruning failed")
		return err
	}

	log.Infof("database pruning completed. Removed %d rows.", prunedCount)
	return nil
}
