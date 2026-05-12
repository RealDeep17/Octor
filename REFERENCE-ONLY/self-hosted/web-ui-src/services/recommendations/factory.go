package recommendations

import (
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
)

// resolverConcurrency caps how many TMDB lookups run in parallel during a
// recommendation request. Sized for Gemini's 6-10 candidates per call: 10
// resolves the typical batch in a single wave instead of two awkward
// halves. Comfortably under TMDB's 40-req/10s burst limit even with
// 3 HTTP calls per item.
const resolverConcurrency = 10

// New constructs the production Service with all collaborators wired from
// a *cli.Context. It is the single entry point used by serve.go; tests
// should call NewGeminiService directly with mocks for each collaborator.
//
// Returns interface-nil when the feature flag is off or
// GEMINI_API_KEY is empty — call sites should treat a nil Service as
// "feature disabled" and skip handler registration entirely.
func New(c *cli.Context, pg *cs.PG, redis *cs.RedisClient, lookup MetadataLookup, localizer ContentLocalizer) Service {
	cfg := ConfigFromCLI(c)

	if !cfg.Enabled {
		log.Info("ai_rec: feature flag off — recommendations service not started")
		return nil
	}
	if cfg.GeminiAPIKey == "" {
		log.Warning("ai_rec: enabled but GEMINI_API_KEY is empty — service disabled")
		return nil
	}

	historyLoader := NewDBUserHistoryLoader(pg)
	contextBuilder := NewUserContextBuilder(historyLoader, cfg.HistoryLimit)
	resolver := NewResolver(lookup, localizer, resolverConcurrency)
	quota := NewRedisQuota(redis.Get(), cfg)
	chipsCache := NewRedisChipsCache(redis.Get())
	freshReleases := NewDBFreshReleasesLoader(pg, int16(cfg.FreshReleasesMinYear), cfg.FreshReleasesLimit, cfg.FreshReleasesCacheTTL)

	svc := NewGeminiService(cfg, contextBuilder, resolver, quota, chipsCache, freshReleases)
	if svc == nil {
		// Explicit interface-nil so callers can do `if svc != nil`.
		// Without this, returning a typed (*GeminiService)(nil) would
		// wrap into a non-nil interface — classic Go gotcha.
		return nil
	}
	log.Info("ai_rec: using Gemini for recommendations")
	return svc
}
