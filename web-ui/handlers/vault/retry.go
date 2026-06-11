package vault

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// retryResource handles HTTP POST request to force retry/reset a vault resource (Level 1: HTTP interaction)
func (h *Handler) retryResource(c *gin.Context) {
	// Extract parameters from form
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)

	// Call business logic
	err := h.processRetry(c.Request.Context(), resourceID, user)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Redirect with success
	web.RedirectWithSuccessAndMessage(c, "toast.vaultRetryQueued")
}

// processRetry contains the core business logic for resource force retry (Level 2: Business logic)
func (h *Handler) processRetry(ctx context.Context, resourceID string, user *auth.User) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Get vault resource
	resource, err := h.vault.GetResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to get vault resource")
	}

	// If resource doesn't exist, nothing to retry
	if resource == nil {
		return errors.New("resource not found")
	}

	// Get user's pledge for this resource to ensure authorization (security guard boundary)
	pledge, err := h.vault.GetPledge(ctx, user, resource)
	if err != nil {
		return errors.Wrap(err, "failed to get user pledge")
	}

	// If user does not have a pledge for this resource, deny retry for security
	if pledge == nil {
		return errors.New("unauthorized: pledge not found for this resource")
	}

	// Trigger PutResource to reset status/error in the vault API client
	_, err = h.vault.PutResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to queue resource retry in vault api")
	}

	return nil
}
