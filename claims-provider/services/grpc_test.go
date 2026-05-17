package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/webtor-io/claims-provider/models"
	pb "github.com/webtor-io/claims-provider/proto"
	"github.com/webtor-io/lazymap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// helpers
func u64(v uint64) *uint64 { return &v }
func val64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}


func TestGRPCGet_AllowsEmptyEmail(t *testing.T) {
	// Prepare GRPC with a store that will be called with empty email
	st := &Store{LazyMap: lazymap.New[*models.Claims](&lazymap.Config{Concurrency: 1, Expire: time.Second, ErrorExpire: time.Second, Capacity: 10})}
	calledWith := "<unset>"
	expected := &models.Claims{TierID: 2, TierName: "silver", DownloadRate: u64(777), EmbedNoAds: true, SiteNoAds: true}
	st.fetch = func(ctx context.Context, email string) (*models.Claims, error) {
		calledWith = email
		return expected, nil
	}
	g := &GRPC{store: st}

	// Call with empty email
	resp, err := g.Get(context.Background(), &pb.GetRequest{Email: ""})
	if err != nil {
		t.Fatalf("unexpected error for empty email: %v", err)
	}
	if calledWith != "" {
		t.Fatalf("store was not called with empty email, got %q", calledWith)
	}
	if resp == nil || resp.Context == nil || resp.Context.Tier == nil || resp.Claims == nil || resp.Claims.Connection == nil || resp.Claims.Embed == nil || resp.Claims.Site == nil {
		t.Fatalf("response has unexpected nil parts: %+v", resp)
	}
	if resp.Context.Tier.Id != expected.TierID {
		t.Errorf("tier id mismatch: got %d want %d", resp.Context.Tier.Id, expected.TierID)
	}
	if resp.Context.Tier.Name != expected.TierName {
		t.Errorf("tier name mismatch: got %q want %q", resp.Context.Tier.Name, expected.TierName)
	}
	if val64(resp.Claims.Connection.Rate) != val64(expected.DownloadRate) {
		t.Errorf("download rate mismatch: got %d want %d", val64(resp.Claims.Connection.Rate), val64(expected.DownloadRate))
	}
	if resp.Claims.Embed.NoAds != expected.EmbedNoAds {
		t.Errorf("embed no_ads mismatch: got %v want %v", resp.Claims.Embed.NoAds, expected.EmbedNoAds)
	}
	if resp.Claims.Site.NoAds != expected.SiteNoAds {
		t.Errorf("site no_ads mismatch: got %v want %v", resp.Claims.Site.NoAds, expected.SiteNoAds)
	}
}

func TestGRPCGet_SuccessMapping(t *testing.T) {
	expected := &models.Claims{
		Email:        "user@example.com",
		TierID:       3,
		TierName:     "gold",
		DownloadRate: u64(123456),
		EmbedNoAds:   true,
		SiteNoAds:    false,
	}
	st := &Store{LazyMap: lazymap.New[*models.Claims](&lazymap.Config{Concurrency: 1, Expire: time.Second, ErrorExpire: time.Second, Capacity: 10})}
	// Inject fetch to bypass DB and cache builder
	st.fetch = func(ctx context.Context, email string) (*models.Claims, error) { return expected, nil }
	g := &GRPC{store: st}

	resp, err := g.Get(context.Background(), &pb.GetRequest{Email: expected.Email})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == nil || resp.Context == nil || resp.Context.Tier == nil || resp.Claims == nil || resp.Claims.Connection == nil || resp.Claims.Embed == nil || resp.Claims.Site == nil {
		t.Fatalf("response has unexpected nil parts: %+v", resp)
	}

	if resp.Context.Tier.Id != expected.TierID {
		t.Errorf("tier id mismatch: got %d want %d", resp.Context.Tier.Id, expected.TierID)
	}
	if resp.Context.Tier.Name != expected.TierName {
		t.Errorf("tier name mismatch: got %q want %q", resp.Context.Tier.Name, expected.TierName)
	}
	if val64(resp.Claims.Connection.Rate) != val64(expected.DownloadRate) {
		t.Errorf("download rate mismatch: got %d want %d", val64(resp.Claims.Connection.Rate), val64(expected.DownloadRate))
	}
	if resp.Claims.Embed.NoAds != expected.EmbedNoAds {
		t.Errorf("embed no_ads mismatch: got %v want %v", resp.Claims.Embed.NoAds, expected.EmbedNoAds)
	}
	if resp.Claims.Site.NoAds != expected.SiteNoAds {
		t.Errorf("site no_ads mismatch: got %v want %v", resp.Claims.Site.NoAds, expected.SiteNoAds)
	}
}

func TestGRPCGet_StoreError(t *testing.T) {
	st := &Store{LazyMap: lazymap.New[*models.Claims](&lazymap.Config{Concurrency: 1, Expire: time.Second, ErrorExpire: time.Second, Capacity: 10})}
	st.fetch = func(ctx context.Context, email string) (*models.Claims, error) { return nil, errors.New("boom") }
	g := &GRPC{store: st}

	_, err := g.Get(context.Background(), &pb.GetRequest{Email: "user@example.com"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	stErr, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if stErr.Code() != codes.Internal {
		t.Fatalf("expected Internal, got: %v", stErr.Code())
	}
}

