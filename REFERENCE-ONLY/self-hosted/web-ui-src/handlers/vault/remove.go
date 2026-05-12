package vault

import (
	"context"

	"os"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// removePledge handles HTTP request for removing a pledge
func (h *Handler) removePledge(c *gin.Context) {
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)

	err := h.deletePledge(c.Request.Context(), resourceID, user)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Redirect back to whichever page the remove was triggered from.
	// async.js sends X-Return-Url = window.location so this stays on the content page.
	web.RedirectWithSuccess(c)
}

// deletePledge contains the core business logic for pledge removal
func (h *Handler) deletePledge(ctx context.Context, resourceID string, user *auth.User) error {
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	resource, err := h.vault.GetResource(ctx, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to get vault resource")
	}
	if resource == nil {
		return errors.New("resource not found")
	}

	pledge, err := h.vault.GetPledge(ctx, user, resource)
	if err != nil {
		return errors.Wrap(err, "failed to get user pledge")
	}
	if pledge == nil {
		return errors.New("pledge not found")
	}

	isFrozen, err := h.vault.IsPledgeFrozen(ctx, pledge)
	if err != nil {
		return errors.Wrap(err, "failed to check pledge frozen status")
	}
	if isFrozen {
		return errors.New("pledge is frozen and cannot be removed")
	}

	err = h.vault.RemovePledge(ctx, pledge)
	if err != nil {
		return errors.Wrap(err, "failed to remove pledge")
	}

	// 0. Reset vaulted status in DB so the UI reflects the change immediately
	if h.pg != nil {
		if db := h.pg.Get(); db != nil {
			errReset := vaultModels.ResetResourceVaulted(ctx, db, resourceID)
			if errReset != nil {
				log.WithError(errReset).WithField("resource_id", resourceID).Error("vault: failed to reset vaulted status in DB")
			}
		}
	}

	// 1. Cancel any active background downloads for this resource
	if cancel, ok := h.activeDownloads.Load(resourceID); ok {
		if cancelFunc, ok := cancel.(context.CancelFunc); ok {
			cancelFunc()
			h.activeDownloads.Delete(resourceID)
			log.WithField("resource_id", resourceID).Info("vault: cancelled active download")
		}
	}

	// 2. Physical storage deletion (purge files but keep Library entry)
	dataDir := "/data"
	targetDir := dataDir + "/" + resourceID
	touchFile := dataDir + "/" + resourceID + ".touch"

	log.WithField("resource_id", resourceID).Info("vault: purging physical storage")

	if err := os.RemoveAll(targetDir); err != nil {
		log.WithError(err).WithField("path", targetDir).Warn("vault: failed to delete data directory")
	}
	if err := os.Remove(touchFile); err != nil && !os.IsNotExist(err) {
		log.WithError(err).WithField("path", touchFile).Warn("vault: failed to delete touch file")
	}

	return nil
}
