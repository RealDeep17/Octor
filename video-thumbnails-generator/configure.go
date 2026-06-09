package main

import (
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	s "github.com/webtor-io/video-thumbnails-generator/services"
)

func configure(app *cli.App) {
	app.Flags = []cli.Flag{}
	app.Flags = cs.RegisterProbeFlags(app.Flags)
	app.Flags = cs.RegisterS3ClientFlags(app.Flags)
	s.RegisterWebFlags(app)
	s.RegisterS3StorageFlags(app)
	app.Action = run
}

func run(c *cli.Context) error {
	// Setting S3Client
	s3cl := cs.NewS3Client(c, &http.Client{
		Timeout: time.Second * 60,
	})

	// Setting S3Storage
	s3st := s.NewS3Storage(c, s3cl)

	// Setting GeneratorPool
	p := s.NewGeneratorPool(s3st)

	// Setting ProbeService
	probe := cs.NewProbe(c)
	defer probe.Close()

	// Setting WebService
	web := s.NewWeb(c, p)
	defer web.Close()

	// Setting ServeService
	serve := cs.NewServe(web, probe)

	// And SERVE!
	err := serve.Serve()
	if err != nil {
		log.WithError(err).Error("Got server error")
	}
	return err
}
