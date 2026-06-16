package services

import (
	"context"
	"fmt"
	"github.com/urfave/cli"
	"io"
	"net"
	"net/http"
	"time"

	logrusmiddleware "github.com/bakins/logrus-middleware"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Web struct {
	pool   *SRT2VTT
	host   string
	port   int
	ln     net.Listener
	server *http.Server
}

const (
	webHostFlag = "host"
	webPortFlag = "port"
)

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

func NewWeb(c *cli.Context, pool *SRT2VTT) *Web {
	return &Web{
		pool: pool,
		host: c.String(webHostFlag),
		port: c.Int(webPortFlag),
	}
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		url := r.Header.Get("X-Source-Url")
		if url == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, err := s.pool.Get(r.Context(), url)
		if err != nil {
			log.WithError(err).Errorf("failed to process request with url=%s", url)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, data)
	})
	logger := log.New()
	logger.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	l := logrusmiddleware.Middleware{
		Logger: logger,
	}
	log.Infof("serving Web at %v", addr)
	s.server = &http.Server{
		Handler: l.Handler(mux, ""),
		// ReadTimeout:    5 * time.Minute,
		// WriteTimeout:   5 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}
	if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Web) Close() {
	log.Info("closing Web")
	defer func() {
		log.Info("Web closed")
	}()
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			log.Errorf("Web graceful shutdown failed: %v", err)
			_ = s.server.Close()
		}
	} else if s.ln != nil {
		_ = s.ln.Close()
	}
}
