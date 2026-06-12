package library

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
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

	var cleanIDs []string
	for _, rID := range req.ResourceIDs {
		rID = strings.TrimSpace(rID)
		if rID != "" {
			cleanIDs = append(cleanIDs, rID)
		}
	}
	if len(cleanIDs) > 0 {
		if err := models.RemoveMultipleFromLibrary(ctx, db, u.ID, cleanIDs); err != nil {
			logrus.WithError(err).Warn("failed to remove multiple from library")
		}
	}

	if s.nats != nil && s.nats.Get() != nil {
		_ = s.nats.Get().Publish(fmt.Sprintf("user.%s.update", u.ID.String()), []byte(`{"type": "library"}`))
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}
