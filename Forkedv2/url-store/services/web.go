package services

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/url-store/models"
)

const (
	webHostFlag = "host"
	webPortFlag = "port"
)

type Web struct {
	host string
	port int
	db   *cs.PG
	acp  *AbuseCheckPool
	ln   net.Listener
}

func NewWeb(c *cli.Context, db *cs.PG, acp *AbuseCheckPool) *Web {
	return &Web{
		host: c.String(webHostFlag),
		port: c.Int(webPortFlag),
		db:   db,
		acp:  acp,
	}
}

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   webHostFlag,
			Usage:  "listening host",
			Value:  "",
			EnvVar: "WEB_HOST",
		},
		cli.IntFlag{
			Name:   webPortFlag,
			Usage:  "http listening port",
			Value:  8080,
			EnvVar: "WEB_PORT",
		},
	)
}

func (s *Web) process(w http.ResponseWriter, r *http.Request) error {
	h := strings.TrimLeft(strings.TrimRight(r.URL.Path, "/"), "/")
	data, err := hex.DecodeString(h)
	if err != nil {
		return errors.Wrapf(err, "failed parse path=%v", h)
	}
	if len(data) != 20 {
		return errors.Errorf("sha1 must contain 20 bytes %v given", len(data))
	}
	ok, err := s.acp.Check(h)
	if err != nil {
		return errors.Wrapf(err, "failed to check abuse hash=%v", h)
	}
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("url forbidden"))
		return nil
	}
	db := s.db.Get()
	m := new(models.URL)
	err = db.Model(m).Where("digest(url, 'sha1') = ?", data).Select()
	if err == pg.ErrNoRows {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("url not found"))
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "failed to find url by hash=%v", h)
	}
	go func() {
		db.Model(m).Set("accessed_at = now()").WherePK().Update()
	}()
	w.Header().Set("Last-Modified", m.CreatedAt.Format(http.TimeFormat))
	w.Header().Set("Etag", h)
	w.Write([]byte(m.URL))
	return nil
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	s.ln = ln
	if err != nil {
		return errors.Wrap(err, "failed to web listen to tcp connection")
	}
	m := http.NewServeMux()
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := s.process(w, r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		}
	})
	log.Infof("serving Web at %v", addr)
	srv := &http.Server{
		Handler:        m,
		MaxHeaderBytes: 50 << 20,
	}
	return srv.Serve(ln)
}

func (s *Web) Close() {
	log.Info("closing Web")
	defer func() {
		log.Info("Web closed")
	}()
	if s.ln != nil {
		s.ln.Close()
	}
}
