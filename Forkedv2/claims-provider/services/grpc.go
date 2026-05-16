package services

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/webtor-io/claims-provider/models"
	pb "github.com/webtor-io/claims-provider/proto"
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
	pb.UnimplementedClaimsProviderServer
	host  string
	port  int
	ln    net.Listener
	srv   *grpc.Server
	store *Store
}

// NewGRPC returns a new GRPC server
func NewGRPC(c *cli.Context, s *Store) *GRPC {
	// Build and return a new GRPC server
	return &GRPC{
		// Host and port to listen on
		host: c.String(grpcHostFlag),
		port: c.Int(grpcPortFlag),
		// Store to get claims from
		store: s,
	}
}

// Serve starts the gRPC server
func (s *GRPC) Serve() error {
	// Build address
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	// Listen to TCP connection
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.Wrap(err, "failed to listen to tcp connection")
	}

	// Save the listener
	s.ln = listener

	// Create a new gRPC server with a logger interceptor
	serverOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			start := time.Now()
			fields := log.Fields{
				"method": info.FullMethod,
			}
			if r, ok := req.(*pb.GetRequest); ok {
				fields["email"] = r.Email
			}
			resp, err := handler(ctx, req)
			fields["took"] = time.Since(start)
			if err != nil {
				log.WithFields(fields).WithError(err).Error("grpc unary call failed")
			} else {
				log.WithFields(fields).Info("grpc unary call succeeded")
			}
			return resp, err
		}),
	}
	gs := grpc.NewServer(serverOpts...)
	// store server pointer for graceful shutdown
	s.srv = gs

	// Register the service
	pb.RegisterClaimsProviderServer(gs, s)

	// Log the start message
	log.Infof("serving GRPC at %v", addr)

	// Start the server
	return gs.Serve(listener)
}

// Close stops the gRPC server and releases the listener
func (s *GRPC) Close() error {
	// Attempt graceful stop if server exists
	if s.srv != nil {
		s.srv.GracefulStop()
	}
	// Close the listener if it's open
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// Get gets claims for a user
func (s *GRPC) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	email := ""
	if req != nil {
		email = req.Email
	}
	var (
		c          *models.Claims
		cs         []*models.Claims
		lastErr    error
		anySuccess bool
	)
	{
		c, err := s.store.GetByEmail(ctx, email)
		if err != nil {
			lastErr = err
			log.WithField("email", email).WithError(err).Warn("failed to get claims by email")
		} else {
			anySuccess = true
		}
		if c != nil {
			cs = append(cs, c)
		}
	}
	if !anySuccess {
		log.WithFields(log.Fields{
			"email": email,
		}).WithError(lastErr).Error("failed to get claims from store")
		return nil, status.Error(codes.Internal, "failed to get claims")
	}
	if len(cs) == 0 {
		log.WithFields(log.Fields{
			"email": email,
		}).Error("no claims found")
		return nil, status.Error(codes.NotFound, "no claims found")
	}

	// Sort claims by tier ID in descending order
	sort.Slice(cs, func(i, j int) bool {
		return cs[i].TierID > cs[j].TierID
	})

	c = cs[0]

	// Hardcode Pro tier and unlimited points for self-hosted de-monetization
	tierID := uint32(1000)
	tierName := "Pro"
	vaultPoints := uint64(999999999999999999)

	// Build GRPC response
	return &pb.GetResponse{
		Context: &pb.Context{
			Tier: &pb.Tier{
				Id:   tierID,
				Name: tierName,
			},
		},
		Claims: &pb.Claims{
			Connection: &pb.Connection{
				Rate: c.DownloadRate,
			},
			Embed: &pb.Embed{
				NoAds: true, // Always no ads
			},
			Site: &pb.Site{
				NoAds: true, // Always no ads
			},
			Vault: &pb.Vault{
				Points: &vaultPoints,
			},
		},
	}, nil
}
