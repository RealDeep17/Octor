package library

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (s *Handler) remove(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusForbidden)
		return
	}
	ctx := c.Request.Context()
	rID, _ := c.GetPostForm("resource_id")
	alsoVault := c.PostForm("also_vault") == "true"

	err := s.removeFromLibrary(ctx, c, u)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to remove from library"))
		return
	}

	if alsoVault && s.vault != nil && rID != "" {
		resource, err := s.vault.GetResource(ctx, rID)
		if err == nil && resource != nil {
			pledge, err := s.vault.GetPledge(ctx, u, resource)
			if err == nil && pledge != nil {
				_ = s.vault.RemovePledge(ctx, pledge)
				if s.api != nil {
					claims := api.GetClaimsFromContext(c)
					purgeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					_ = s.api.PurgeResourceCache(purgeCtx, claims, rID)
				}
			}
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromLibrary")
}

func (s *Handler) removeFromLibrary(ctx context.Context, c *gin.Context, u *auth.User) (err error) {
	rID, _ := c.GetPostForm("resource_id")
	db := s.pg.Get()
	if db == nil {
		return errors.New("no db")
	}
	return models.RemoveFromLibrary(ctx, db, u.ID, rID)
}
