package vault

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (h *Handler) retryMultiple(c *gin.Context) {
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
		_ = h.processRetry(ctx, rID, u)
	}

	web.RedirectWithSuccessAndMessage(c, "toast.vaultRetrying")
}
