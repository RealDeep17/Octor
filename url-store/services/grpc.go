package services

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/url-store/models"
	pb "github.com/webtor-io/url-store/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	grpcHostFlag = "grpc-host"
	grpcPortFlag = "grpc-port"
)

func RegisterGRPCFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   grpcHostFlag,
			Usage:  "grpc listening host",
			Value:  "",
			EnvVar: "GRPC_HOST",
		},
		cli.IntFlag{
			Name:   grpcPortFlag,
			Usage:  "grpc listening port",
			Value:  50051,
			EnvVar: "GRPC_PORT",
		},
	)
}

type GRPC struct {
	pb.UnimplementedUrlStoreServer
	host string
	port int
	db   *cs.PG
	ln   net.Listener
	acp  *AbuseCheckPool
}

func NewGRPC(c *cli.Context, db *cs.PG, acp *AbuseCheckPool) *GRPC {
	return &GRPC{
		host: c.String(grpcHostFlag),
		port: c.Int(grpcPortFlag),
		db:   db,
		acp:  acp,
	}
}

func (s *GRPC) Push(c context.Context, r *pb.PushRequest) (*pb.PushReply, error) {
	db := s.db.Get()
	u := r.GetUrl()
	_, err := url.Parse(u)
	if err != nil {
		return nil, errors.Errorf("failed to parse url=%v", u)
	}

	hb := sha1.Sum([]byte(u))
	h := hex.EncodeToString(hb[:])

	ok, err := s.acp.Check(h)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check abuse hash=%v", h)
	}

	if !ok {
		return nil, status.Errorf(codes.PermissionDenied, "restricted by the rightholder hash=%v", h)
	}

	m := &models.URL{URL: u}

	_, err = db.Model(m).
		OnConflict("(url) DO UPDATE").
		Set("accessed_at = now()").
		Insert()

	if err != nil {
		return nil, err
	}

	return &pb.PushReply{Hash: h}, nil
}

func (s *GRPC) Check(c context.Context, r *pb.CheckRequest) (*pb.CheckReply, error) {
	db := s.db.Get()
	h := r.GetHash()

	data, err := hex.DecodeString(h)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse hash=%v", data)
	}

	ok, err := s.acp.Check(h)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check abuse hash=%v", h)
	}

	if !ok {
		return nil, status.Errorf(codes.PermissionDenied, "restricted by the rightholder hash=%v", h)
	}

	m := new(models.URL)

	err = db.Model(m).Where("digest(url, 'sha1') = ?", data).Select()
	if err == pg.ErrNoRows {
		return &pb.CheckReply{Exists: false}, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find url by hash=%v", h)
	}
	go func() {
		db.Model(m).Set("accessed_at = now()").WherePK().Update()
	}()
	return &pb.CheckReply{Exists: true}, nil
}

func (s *GRPC) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen to tcp connection")
	}
	s.ln = ln
	var opts []grpc.ServerOption
	gs := grpc.NewServer(opts...)
	pb.RegisterUrlStoreServer(gs, s)
	log.Infof("serving GRPC at %v", addr)
	return gs.Serve(ln)
}

func (s *GRPC) Close() {
	log.Info("closing GRPC")
	defer func() {
		log.Info("GRPC closed")
	}()
	if s.ln != nil {
		s.ln.Close()
	}
}
