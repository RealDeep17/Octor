package vault

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// removePledge handles HTTP request for removing a pledge (Level 1: HTTP interaction)
func (h *Handler) removePledge(c *gin.Context) {
	// Extract parameters from form
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)

	// Call business logic
	err := h.deletePledge(c.Request.Context(), resourceID, user)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}
	if h.api != nil {
		claims := api.GetClaimsFromContext(c)
		purgeCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if purgeErr := h.api.PurgeResourceCache(purgeCtx, claims, resourceID); purgeErr != nil {
			logrus.WithError(purgeErr).WithField("resource_id", resourceID).Warn("failed to purge seeder cache after vault removal")
		}
	}

	// Redirect with success
	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}

// deletePledge contains the core business logic for pledge removal (Level 2: Business logic)
func (h *Handler) deletePledge(ctx context.Context, resourceID string, user *auth.User) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Get vault resource
	resource, err := h.vault.GetResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist, nothing to remove
	if resource == nil {
		return errors.New("resource not found")
	}

	// Get user's pledge for this resource
	pledge, err := h.vault.GetPledge(ctx, user, resource)
	if err != nil {
		return errors.Wrap(err, "failed to get user pledge")
	}

	// If pledge doesn't exist, nothing to remove
	if pledge == nil {
		return errors.New("pledge not found")
	}

	// Remove the pledge
	err = h.vault.RemovePledge(ctx, pledge)
	if err != nil {
		return errors.Wrap(err, "failed to remove pledge")
	}

	return nil
}
