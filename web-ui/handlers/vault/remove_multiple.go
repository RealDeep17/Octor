package vault

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (h *Handler) removeMultiple(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}

	var req struct {
		ResourceIDs []string `form:"resource_ids[]"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	ctx := c.Request.Context()

	for _, rID := range req.ResourceIDs {
		rID = strings.TrimSpace(rID)
		if rID == "" {
			continue
		}
		
		err := h.deletePledge(ctx, rID, u)
		if err != nil {
			logrus.WithError(err).WithField("resource_id", rID).Warn("failed to delete pledge in bulk")
			continue
		}

		if h.api != nil {
			claims := api.GetClaimsFromContext(c)
			purgeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if purgeErr := h.api.PurgeResourceCache(purgeCtx, claims, rID); purgeErr != nil {
				logrus.WithError(purgeErr).WithField("resource_id", rID).Warn("failed to purge seeder cache after bulk vault removal")
			}
			cancel()
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}
