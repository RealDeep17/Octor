package library

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/web"
)

// enrichMultiple handles POST /lib/enrich-multiple
// Force re-enriches multiple library resources for the logged-in user.
func (s *Handler) enrichMultiple(c *gin.Context) {
	ctx := c.Request.Context()
	wc := web.NewContext(c)
	if wc == nil || wc.User == nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	var req struct {
		ResourceIDs []string `form:"resource_ids[]"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if s.enricher == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	for _, rID := range req.ResourceIDs {
		rID = strings.TrimSpace(rID)
		if rID == "" {
			continue
		}
		// force=true: bypasses locks and re-runs enrichment
		err := s.enricher.Enrich(ctx, rID, wc.ApiClaims, true, "")
		if err != nil {
			log.WithError(err).Errorf("bulk enrich failed for resource %s", rID)
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.enrichmentStarted")
}
