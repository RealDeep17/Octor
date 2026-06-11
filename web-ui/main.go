package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

func init() {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(network, "tcp") {
				network = "tcp4"
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "web-ui"
	app.Usage = "runs octor web ui v2"
	app.Version = "0.0.1"
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	configure(app)
	err := app.Run(os.Args)
	if err != nil {
		log.WithError(err).Fatal("failed to serve application")
	}
}
