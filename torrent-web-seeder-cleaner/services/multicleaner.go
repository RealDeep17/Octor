package services

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
)

type MultiCleaner struct {
	p        string
	keep     string
	free     string
	maxAge   time.Duration
	interval time.Duration
	cleaners []*Cleaner
}

func NewMultiCleaner(c *cli.Context) *MultiCleaner {
	return &MultiCleaner{
		p:        c.String(DataDirFlag),
		keep:     c.String(CleanerKeepFreeFlag),
		free:     c.String(CleanerFreeFlag),
		maxAge:   c.Duration(CleanerMaxAgeFlag),
		interval: c.Duration(CleanerIntervalFlag),
		cleaners: []*Cleaner{},
	}
}

func (s *MultiCleaner) Serve() error {
	if strings.HasSuffix(s.p, "*") {
		prefix := strings.TrimSuffix(s.p, "*")
		dir, lp := path.Split(prefix)
		if dir == "" {
			dir = "."
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var dirs []string
		for _, f := range files {
			if f.IsDir() && strings.HasPrefix(f.Name(), lp) {
				dirs = append(dirs, f.Name())
			}
		}
		for _, d := range dirs {
			s.cleaners = append(s.cleaners, NewCleaner(dir+"/"+d, s.keep, s.free, s.maxAge, s.interval))
		}
	} else {
		s.cleaners = append(s.cleaners, NewCleaner(s.p, s.keep, s.free, s.maxAge, s.interval))
	}
	if len(s.cleaners) == 0 {
		return errors.Errorf("no cleaners for %v", s.p)
	}
	var sv []cs.Servable
	for _, c := range s.cleaners {
		sv = append(sv, c)
	}
	serve := cs.NewServe(sv...)
	err := serve.Serve()
	if err != nil {
		log.WithError(err).Error("got server error")
	}
	return err
}

func (s *MultiCleaner) Close() {
	for _, c := range s.cleaners {
		c.Close()
	}
}
