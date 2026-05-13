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

func TestGRPCGet_ByPatreonID(t *testing.T) {
	st := &Store{LazyMap: lazymap.New[*models.Claims](&lazymap.Config{Concurrency: 1, Expire: time.Second, ErrorExpire: time.Second, Capacity: 10})}
	calledWith := "<unset>"
	expected := &models.Claims{PatreonUserID: "pat_123", TierID: 1, TierName: "bronze", DownloadRate: u64(100), EmbedNoAds: false, SiteNoAds: false}
	st.fetchByPatreonID = func(ctx context.Context, patreonID string) (*models.Claims, error) {
		calledWith = patreonID
		return expected, nil
	}
	// Also mock the fetch function to return nil (no claims found for email)
	st.fetch = func(ctx context.Context, email string) (*models.Claims, error) {
		return nil, nil
	}
	g := &GRPC{store: st}

	resp, err := g.Get(context.Background(), &pb.GetRequest{PatreonUserId: expected.PatreonUserID})
	if err != nil {
		t.Fatalf("unexpected error for patreon id: %v", err)
	}
	if calledWith != expected.PatreonUserID {
		t.Fatalf("store was not called with patreon id, got %q", calledWith)
	}
	if resp.Context == nil || resp.Context.PatreonUserId != expected.PatreonUserID {
		t.Fatalf("patreon id not propagated to response context: %+v", resp.Context)
	}
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

func TestGRPCGet_TwoResultsDifferentTiers(t *testing.T) {
	st := &Store{LazyMap: lazymap.New[*models.Claims](&lazymap.Config{Concurrency: 1, Expire: time.Second, ErrorExpire: time.Second, Capacity: 10})}

	// Lower tier claim from Patreon ID
	patreonClaim := &models.Claims{
		PatreonUserID: "pat_123",
		TierID:        2,
		TierName:      "silver",
		DownloadRate:  u64(200),
		EmbedNoAds:    true,
		SiteNoAds:     false,
	}

	// Higher tier claim from email
	emailClaim := &models.Claims{
		Email:        "user@example.com",
		TierID:       5,
		TierName:     "platinum",
		DownloadRate: u64(500),
		EmbedNoAds:   true,
		SiteNoAds:    true,
	}

	st.fetchByPatreonID = func(ctx context.Context, patreonID string) (*models.Claims, error) {
		return patreonClaim, nil
	}
	st.fetch = func(ctx context.Context, email string) (*models.Claims, error) {
		return emailClaim, nil
	}

	g := &GRPC{store: st}

	resp, err := g.Get(context.Background(), &pb.GetRequest{
		PatreonUserId: patreonClaim.PatreonUserID,
		Email:         emailClaim.Email,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == nil || resp.Context == nil || resp.Context.Tier == nil || resp.Claims == nil {
		t.Fatalf("response has unexpected nil parts: %+v", resp)
	}

	// Should return the higher tier claim (email claim with tier 5)
	if resp.Context.Tier.Id != emailClaim.TierID {
		t.Errorf("expected higher tier id %d, got %d", emailClaim.TierID, resp.Context.Tier.Id)
	}
	if resp.Context.Tier.Name != emailClaim.TierName {
		t.Errorf("expected higher tier name %q, got %q", emailClaim.TierName, resp.Context.Tier.Name)
	}
	if val64(resp.Claims.Connection.Rate) != val64(emailClaim.DownloadRate) {
		t.Errorf("expected higher tier download rate %d, got %d", val64(emailClaim.DownloadRate), val64(resp.Claims.Connection.Rate))
	}
	if resp.Claims.Embed.NoAds != emailClaim.EmbedNoAds {
		t.Errorf("expected higher tier embed no_ads %v, got %v", emailClaim.EmbedNoAds, resp.Claims.Embed.NoAds)
	}
	if resp.Claims.Site.NoAds != emailClaim.SiteNoAds {
		t.Errorf("expected higher tier site no_ads %v, got %v", emailClaim.SiteNoAds, resp.Claims.Site.NoAds)
	}
}
