package services

import (
	"fmt"
	"sync"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	pb "github.com/webtor-io/abuse-store/abuse-store"
	"google.golang.org/grpc"
)

const (
	abuseStoreClientHostFlag = "abuse-store-client-host"
	abuseStoreClientPortFlag = "abuse-store-client-port"
)

func RegisterAbuseClientStoreFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   abuseStoreClientHostFlag,
			Usage:  "abuse store client listening host",
			EnvVar: "ABUSE_STORE_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   abuseStoreClientPortFlag,
			Usage:  "abuse store client listening port",
			EnvVar: "ABUSE_STORE_SERVICE_PORT",
		},
	)
}

type AbuseStoreClient struct {
	host   string
	port   int
	inited bool
	mux    sync.Mutex
	err    error
	cl     pb.AbuseStoreClient
	conn   *grpc.ClientConn
}

func NewAbuseStoreClient(c *cli.Context) *AbuseStoreClient {
	return &AbuseStoreClient{
		host: c.String(abuseStoreClientHostFlag),
		port: c.Int(abuseStoreClientPortFlag),
	}
}

func (s *AbuseStoreClient) get() (pb.AbuseStoreClient, error) {
	if s.host == "" && s.port == 0 {
		return nil, nil
	}
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial abuse store addr=%v", addr)
	}
	s.conn = conn
	log.Infof("connected to abuse store addr=%v", addr)
	return pb.NewAbuseStoreClient(conn), nil
}

func (s *AbuseStoreClient) Close() {
	log.Info("closing Abuse Store")
	defer func() {
		log.Info("Abuse Store closed")
	}()
	if s.conn != nil {
		s.conn.Close()
	}
}

func (s *AbuseStoreClient) Get() (pb.AbuseStoreClient, error) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.inited {
		return s.cl, s.err
	}
	s.cl, s.err = s.get()
	s.inited = true
	return s.cl, s.err
}
