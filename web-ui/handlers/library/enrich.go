package library

import (
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/web"
)

// enrichResource handles POST /lib/:id/enrich
// Force re-enriches a single library resource for any logged-in user.
// force=true always runs: bypasses the 24h lock and resets retry_count.
func (s *Handler) enrichResource(c *gin.Context) {
	ctx := c.Request.Context()

	wc, ok := c.MustGet("web-context").(*web.Context)
	if !ok || wc == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Any authenticated user can trigger re-enrichment on their own library items.
	// The route group middleware already requires a valid session.
	if wc.User == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
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

	// force=true: steals any existing Processing lock, resets retry_count, unblocks Abandoned.
	err := s.enricher.Enrich(ctx, resourceID, wc.ApiClaims, true, "")
	if err != nil {
		log.WithError(err).Errorf("per-item enrich failed for resource %s", resourceID)
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	log.Infof("per-item enrich completed for resource %s", resourceID)
	web.RedirectWithSuccessAndMessage(c, "toast.enrichmentStarted")
}
