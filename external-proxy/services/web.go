package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/vincent-petithory/dataurl"
)

type Web struct {
	host   string
	port   int
	ln     net.Listener
	server *http.Server
}

const (
	WEB_HOST_FLAG = "host"
	WEB_PORT_FLAG = "port"
)

func NewWeb(c *cli.Context) *Web {
	return &Web{host: c.String(WEB_HOST_FLAG), port: c.Int(WEB_PORT_FLAG)}
}

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   WEB_HOST_FLAG,
			Usage:  "listening host",
			Value:  "",
			EnvVar: "WEB_HOST",
		},
		cli.IntFlag{
			Name:   WEB_PORT_FLAG,
			Usage:  "http listening port",
			Value:  8080,
			EnvVar: "WEB_PORT",
		},
	)
}

func (s *Web) ServeRemote(data string) (io.ReadCloser, string, error) {
	u, err := url.Parse(data)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to parse url")
	}
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to get url")
	}
	t := resp.Header.Get("Content-Type")
	return resp.Body, t, nil
}

func (s *Web) ServeData(data string) (io.ReadCloser, string, error) {
	dataURL, err := dataurl.DecodeString(data)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to parse data url")
	}
	return ioutil.NopCloser(bytes.NewReader(dataURL.Data)), dataURL.ContentType(), nil
}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to web listen to tcp connection")
	}
	s.ln = ln
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 3 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			log.WithError(err).Error("failed to decode")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		data := string(decoded)
		ct := ""
		var rc io.ReadCloser
		if strings.HasPrefix(data, "http") {
			rc, ct, err = s.ServeRemote(data)
		} else if strings.HasPrefix(string(decoded), "data") {
			rc, ct, err = s.ServeData(data)
		}
		if err != nil {
			log.WithError(err).Error("failed to process data")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer rc.Close()
		if ct != "" {
			w.Header().Add("Content-Type", ct)
		}
		_, err = io.Copy(w, rc)
		if err != nil {
			log.WithError(err).Error("failed to write response")
			w.WriteHeader(http.StatusNotFound)
			return
		}
	})
	log.Infof("Serving Web at %v", addr)
	s.server = &http.Server{
		Handler: mux,
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
