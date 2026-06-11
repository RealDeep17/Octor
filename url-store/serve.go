package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	s "github.com/webtor-io/url-store/services"
)

func makeServeCMD() cli.Command {
	serveCmd := cli.Command{
		Name:    "serve",
		Aliases: []string{"s"},
		Usage:   "Serves web server",
		Action:  serve,
	}
	configureServe(&serveCmd)
	return serveCmd
}

func configureServe(c *cli.Command) {
	c.Flags = s.RegisterWebFlags([]cli.Flag{})
	c.Flags = s.RegisterGRPCFlags(c.Flags)
	c.Flags = cs.RegisterProbeFlags(c.Flags)
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = s.RegisterAbuseClientStoreFlags(c.Flags)
}

func serve(c *cli.Context) error {
	// Setting DB
	db := cs.NewPG(c)
	defer db.Close()

	// Setting Migrations
	m := cs.NewPGMigration(db)
	err := m.Run()
	if err != nil {
		return err
	}

	// Setting Probe
	probe := cs.NewProbe(c)
	defer probe.Close()

	// Setting Abuse Store Client
	abuseStoreClient := s.NewAbuseStoreClient(c)

	// Setting Abuse Store Pool
	abuseCheckPool := s.NewAbuseCheckPool(abuseStoreClient)

	// Setting GRPC
	grpc := s.NewGRPC(c, db, abuseCheckPool)
	defer grpc.Close()

	// Setting Web
	web := s.NewWeb(c, db, abuseCheckPool)
	defer web.Close()

	// Setting ServeService
	serve := cs.NewServe(probe, web, grpc)

	// And SERVE!
	err = serve.Serve()
	if err != nil {
		log.WithError(err).Error("Got server error")
	}
	return err
}
