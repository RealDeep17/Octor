package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	pb "github.com/webtor-io/claims-provider/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		host       string
		port       int
		email      string
		patreonID  string
		timeoutSec int
	)

	// Defaults can be overridden by environment variables GRPC_HOST and GRPC_PORT
	hostDefault := envOrDefault("GRPC_HOST", "127.0.0.1")
	portDefault := 50051
	if v := os.Getenv("GRPC_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			portDefault = p
		}
	}

	flag.StringVar(&host, "grpc-host", hostDefault, "gRPC server host (can use GRPC_HOST env)")
	flag.IntVar(&port, "grpc-port", portDefault, "gRPC server port (can use GRPC_PORT env)")
	flag.StringVar(&email, "email", "", "email to query claims by")
	flag.StringVar(&patreonID, "patreon-id", "", "Patreon ID to query claims by")
	flag.IntVar(&timeoutSec, "timeout", 10, "dial/call timeout in seconds")
	flag.Parse()

	if email == "" && patreonID == "" {
		fail(errors.New("either --email or --patreon-id must be provided"))
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fail(fmt.Errorf("failed to create client connection for %s: %w", addr, err))
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			_, _ = fmt.Fprintln(os.Stderr, "warn: closing connection:", cerr)
		}
	}()

	client := pb.NewClaimsProviderClient(conn)

	// Server prefers patreon_id when both are present; we emulate same behavior.
	req := &pb.GetRequest{
		Email:         email,
		PatreonUserId: patreonID,
	}

	resp, err := client.Get(ctx, req)
	if err != nil {
		fail(fmt.Errorf("Get call failed: %w", err))
	}

	printResponse(resp)
}

func fail(err error) {
	if _, werr := fmt.Fprintln(os.Stderr, "error:", err); werr != nil {
		// Best-effort fallback if stderr write fails
		_ = os.Stderr.Sync()
	}
	os.Exit(1)
}

func printResponse(r *pb.GetResponse) {
	if r == nil {
		fmt.Println("no response")
		return
	}

	fmt.Println("Context:")
	if r.Context != nil {
		if r.Context.Tier != nil {
			fmt.Printf("  Tier: id=%d name=%s\n", r.Context.Tier.Id, r.Context.Tier.Name)
		}
		fmt.Printf("  Patreon ID: %s\n", r.Context.PatreonUserId)
	}

	fmt.Println("Claims:")
	if r.Claims != nil {
		if r.Claims.Connection != nil {
			fmt.Printf("  Connection: rate=%d\n", r.Claims.Connection.Rate)
		}
		if r.Claims.Embed != nil {
			fmt.Printf("  Embed: no_ads=%t\n", r.Claims.Embed.NoAds)
		}
		if r.Claims.Site != nil {
			fmt.Printf("  Site: no_ads=%t\n", r.Claims.Site.NoAds)
		}
	}
}
