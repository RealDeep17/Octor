package main

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	s "github.com/webtor-io/claims-provider/services"
	cs "github.com/webtor-io/common-services"
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
	c.Flags = cs.RegisterProbeFlags(c.Flags)
	c.Flags = s.RegisterGRPCFlags(c.Flags)
	c.Flags = s.RegisterStoreFlags(c.Flags)
	c.Flags = cs.RegisterPGFlags(c.Flags)
}

func serve(c *cli.Context) error {
	// Setting DB
	pg := cs.NewPG(c)
	defer pg.Close()

	var servers []cs.Servable

	// Setting Probe
	probe := cs.NewProbe(c)
	servers = append(servers, probe)
	defer probe.Close()

	// Setting Store
	store := s.NewStore(c, pg)

	// Setting GRPC
	grpc := s.NewGRPC(c, store)
	servers = append(servers, grpc)
	defer func(grpc *s.GRPC) {
		_ = grpc.Close()
	}(grpc)

	// Setting Serve
	serve := cs.NewServe(servers...)

	// And SERVE!
	err := serve.Serve()
	if err != nil {
		log.WithError(err).Error("got server error")
	}
	return err
}
