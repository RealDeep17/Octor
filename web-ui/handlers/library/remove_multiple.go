package library

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (s *Handler) removeMultiple(c *gin.Context) {
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
	db := s.pg.Get()
	if db == nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	for _, rID := range req.ResourceIDs {
		rID = strings.TrimSpace(rID)
		if rID == "" {
			continue
		}
		_ = models.RemoveFromLibrary(ctx, db, u.ID, rID)
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}
