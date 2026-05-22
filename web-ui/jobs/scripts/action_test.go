package scripts

import (
	"math"
	"testing"
	"time"

	claimsproto "github.com/webtor-io/claims-provider/proto"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/web"
)

func ctxWith(rate, tier string) *web.Context {
	c := &web.Context{}
	if rate != "" {
		c.ApiClaims = &api.Claims{Rate: rate}
	}
	if tier != "" {
		c.Claims = &claims.Data{Context: &claimsproto.Context{Tier: &claimsproto.Tier{Name: tier}}}
	}
	return c
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestDetachDirectVideoFallback(t *testing.T) {
	tag := &ra.ExportTag{
		Sources: []ra.ExportSource{
			{Src: "https://media.example/movie~vod/hls/abc/index.m3u8", Type: "application/vnd.apple.mpegurl"},
			{Src: "https://media.example/movie.mp4?redirect=true", Type: "video/mp4"},
		},
	}

	fallbackURL, fallbackType := detachDirectVideoFallback(tag)
	if fallbackURL != "https://media.example/movie.mp4?redirect=true" {
		t.Fatalf("fallbackURL = %q", fallbackURL)
	}
	if fallbackType != "video/mp4" {
		t.Fatalf("fallbackType = %q", fallbackType)
	}
	if len(tag.Sources) != 1 {
		t.Fatalf("len(tag.Sources) = %d, want 1", len(tag.Sources))
	}
	if tag.Sources[0].Type != "application/vnd.apple.mpegurl" {
		t.Fatalf("remaining source type = %q", tag.Sources[0].Type)
	}
}

func TestDetachDirectVideoFallbackKeepsSingleDirectSource(t *testing.T) {
	tag := &ra.ExportTag{
		Sources: []ra.ExportSource{
			{Src: "https://media.example/movie.mp4?redirect=true", Type: "video/mp4"},
		},
	}

	fallbackURL, fallbackType := detachDirectVideoFallback(tag)
	if fallbackURL != "" || fallbackType != "" {
		t.Fatalf("fallback = (%q, %q), want empty", fallbackURL, fallbackType)
	}
	if len(tag.Sources) != 1 {
		t.Fatalf("len(tag.Sources) = %d, want 1", len(tag.Sources))
	}
}

func TestGetVideoBitrateForItemFallsBackToSizeDuration(t *testing.T) {
	mp := &api.MediaProbe{}
	mp.Format.Duration = "10.0"
	item := &ra.ListItem{Size: 25_000_000}

	got := getVideoBitrateForItem(mp, item)
	if got != 20_000_000 {
		t.Fatalf("bitrate = %d, want 20000000", got)
	}
}

func TestIsMP4Video(t *testing.T) {
	if !isMP4Video(&ra.ListItem{MediaFormat: ra.Video, Ext: "MP4"}) {
		t.Fatal("MP4 video should be direct playable")
	}
	if isMP4Video(&ra.ListItem{MediaFormat: ra.Video, Ext: "mkv"}) {
		t.Fatal("MKV should not be classified as MP4")
	}
	if isMP4Video(&ra.ListItem{MediaFormat: ra.Audio, Ext: "mp4"}) {
		t.Fatal("audio item should not be classified as MP4 video")
	}
}

func TestShouldUseSessionHLSForMP4Audio(t *testing.T) {
	item := &ra.ListItem{MediaFormat: ra.Video, Ext: "mp4"}
	mp := &api.MediaProbe{}
	mp.Streams = append(mp.Streams,
		struct {
			CodecName string `json:"codec_name"`
			CodecType string `json:"codec_type"`
			Width     int    `json:"width,omitempty"`
			Height    int    `json:"height,omitempty"`
			BitRate   string `json:"bit_rate"`
			Duration  string `json:"duration"`
			Tags      struct {
				CreationTime time.Time `json:"creation_time"`
				HandlerName  string    `json:"handler_name"`
				Language     string    `json:"language"`
				VendorId     string    `json:"vendor_id"`
				Title        string    `json:"title"`
			} `json:"tags"`
			Index         int    `json:"index,omitempty"`
			Channels      int    `json:"channels,omitempty"`
			ChannelLayout string `json:"channel_layout,omitempty"`
			SampleRate    string `json:"sample_rate,omitempty"`
		}{CodecName: "h264", CodecType: "video"},
		struct {
			CodecName string `json:"codec_name"`
			CodecType string `json:"codec_type"`
			Width     int    `json:"width,omitempty"`
			Height    int    `json:"height,omitempty"`
			BitRate   string `json:"bit_rate"`
			Duration  string `json:"duration"`
			Tags      struct {
				CreationTime time.Time `json:"creation_time"`
				HandlerName  string    `json:"handler_name"`
				Language     string    `json:"language"`
				VendorId     string    `json:"vendor_id"`
				Title        string    `json:"title"`
			} `json:"tags"`
			Index         int    `json:"index,omitempty"`
			Channels      int    `json:"channels,omitempty"`
			ChannelLayout string `json:"channel_layout,omitempty"`
			SampleRate    string `json:"sample_rate,omitempty"`
		}{CodecName: "truehd", CodecType: "audio"},
	)

	if !shouldUseSessionHLSForMP4Audio(item, mp) {
		t.Fatal("MP4 with copyable video and TrueHD audio should use session HLS")
	}
}

func TestParseRateLimit(t *testing.T) {
	cases := map[string]int64{
		"":    0,
		"10M": 10_000_000,
		"1M":  1_000_000,
		"10":  0,
		"10K": 0,
		"xM":  0,
	}
	for in, want := range cases {
		if got := parseRateLimit(in); got != want {
			t.Errorf("parseRateLimit(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestIsRateLimited(t *testing.T) {
	// cap=10Mbit/s => 1_250_000 B/s
	cap := int64(10_000_000)
	if !isRateLimited(1_200_000, cap) {
		t.Error("1.2MB/s against 10Mbit/s cap should count as rate-limited (≥90%)")
	}
	if isRateLimited(1_000_000, cap) {
		t.Error("1.0MB/s (80% of cap) should not count as rate-limited")
	}
}

func TestBuildSlowDownloadData_RateLimited(t *testing.T) {
	c := ctxWith("10M", "free")
	// measured speed = 1.2 MB/s ≈ 9.6 Mbit/s (saturates 10Mbit cap)
	sdd := buildSlowDownloadData(c, 1_200_000, 8_000_000)
	if !sdd.IsRateLimited {
		t.Fatal("expected IsRateLimited=true")
	}
	if !almostEqual(sdd.RateLimitMbps, 10) {
		t.Errorf("RateLimitMbps = %v, want 10", sdd.RateLimitMbps)
	}
	if !almostEqual(sdd.MeasuredSpeedMbps, 9.6) {
		t.Errorf("MeasuredSpeedMbps = %v, want 9.6", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 8) {
		t.Errorf("RequiredSpeedMbps = %v, want 8 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestBuildSlowDownloadData_SlowNotCappedFreeFallback(t *testing.T) {
	c := &web.Context{}
	// No claims at all: slow peers, no cap. IsRateLimited must be false, tier defaults to "free".
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("no claims should not flag IsRateLimited")
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free fallback", sdd.TierName)
	}
	if sdd.RateLimitMbps != 0 {
		t.Errorf("RateLimitMbps = %v, want 0", sdd.RateLimitMbps)
	}
}

func TestBuildSlowDownloadData_CapPresentButNotSaturated(t *testing.T) {
	// Cap=20M, measured 500KB/s (4Mbit/s) — slow for other reasons, not rate limit.
	c := ctxWith("20M", "free")
	sdd := buildSlowDownloadData(c, 500_000, 8_000_000)
	if sdd.IsRateLimited {
		t.Error("measured speed far below cap should not be classified as rate-limited")
	}
}

func TestCheckCachedRateLimit_NoCap(t *testing.T) {
	c := &web.Context{}
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("no ApiClaims => not limited")
	}
	c = ctxWith("", "premium")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("empty Rate => not limited")
	}
}

func TestCheckCachedRateLimit_CapSufficient(t *testing.T) {
	// Cap=20M, bitrate=8M. Cap > bitrate => not limited.
	c := ctxWith("20M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap above bitrate should not raise warning")
	}
}

func TestCheckCachedRateLimit_CapInsufficient(t *testing.T) {
	// Cap=5M, bitrate=8M. Cap < bitrate => warn.
	c := ctxWith("5M", "free")
	sdd, limited := checkCachedRateLimit(c, 8_000_000)
	if !limited {
		t.Fatal("cap below bitrate should raise warning")
	}
	if !sdd.IsRateLimited {
		t.Error("SlowDownloadData.IsRateLimited should be true on cached path")
	}
	if !almostEqual(sdd.RateLimitMbps, 5) {
		t.Errorf("RateLimitMbps = %v, want 5", sdd.RateLimitMbps)
	}
	// For cached path the "measured" speed equals the cap.
	if !almostEqual(sdd.MeasuredSpeedMbps, 5) {
		t.Errorf("MeasuredSpeedMbps = %v, want 5 (== cap)", sdd.MeasuredSpeedMbps)
	}
	if !almostEqual(sdd.RequiredSpeedMbps, 8) {
		t.Errorf("RequiredSpeedMbps = %v, want 8 (= bitrate)", sdd.RequiredSpeedMbps)
	}
	if sdd.TierName != "free" {
		t.Errorf("TierName = %q, want free", sdd.TierName)
	}
}

func TestCheckCachedRateLimit_CapEqualsRequirement(t *testing.T) {
	// Boundary: cap exactly at bitrate — treat as sufficient.
	c := ctxWith("8M", "basic")
	if _, limited := checkCachedRateLimit(c, 8_000_000); limited {
		t.Error("cap == bitrate should not raise warning")
	}
}
