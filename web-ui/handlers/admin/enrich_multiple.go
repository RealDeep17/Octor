package admin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

// enrichMultiple handles POST /admin/library/enrich-multiple
// Force re-enriches multiple library resources as an admin.
func (h *Handler) enrichMultiple(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		ResourceIDs []string `form:"resource_ids[]"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if h.enricher == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	for _, rID := range req.ResourceIDs {
		rID = strings.TrimSpace(rID)
		if rID == "" {
			continue
		}
		// Admin re-enrichment uses empty claims (system authority)
		err := h.enricher.Enrich(ctx, rID, &api.Claims{}, true, "")
		if err != nil {
			log.WithError(err).Errorf("admin bulk enrich failed for resource %s", rID)
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.enrichmentStarted")
}
