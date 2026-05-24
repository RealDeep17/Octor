package library

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/web"
)

// enrichResource handles POST /lib/:id/enrich
// Forces re-enrichment of a single library resource (admin only).
// This is the Jellyfin-style "Refresh Metadata" per-item action —
// it always runs (force=true), bypassing the 24h lock, and resets retry_count.
func (s *Handler) enrichResource(c *gin.Context) {
	ctx := c.Request.Context()

	wc, ok := c.MustGet("web-context").(*web.Context)
	if !ok || wc == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Admin only — same gate as sidecar enrichment.
	if !wc.IsAdmin {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	resourceID := c.Param("id")
	if resourceID == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if s.enricher == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	// Run synchronously so the UI gets immediate feedback.
	// force=true: always runs, steals any existing Processing lock, resets retry_count.
	err := s.enricher.Enrich(ctx, resourceID, wc.ApiClaims, true, "")
	if err != nil {
		log.WithError(err).Errorf("per-item enrich failed for resource %s", resourceID)
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.Infof("per-item enrich completed for resource %s", resourceID)

	// HTMX partial: redirect back to the library button area so the UI refreshes.
	web.RedirectWithSuccessAndMessage(c, "toast.enrichmentStarted")
}
