package vault

import (
	"bytes"
	"context"
	"io"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// addPledge handles HTTP request for creating a pledge (Level 1: HTTP interaction)
func (h *Handler) addPledge(c *gin.Context) {
	// Extract parameters from form
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)

	// Call business logic
	err := h.createPledge(c.Request.Context(), resourceID, user, apiClaims)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	if err = h.addTorrentToLibrary(c, resourceID, user, apiClaims); err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "failed to add torrent to library"))
		return
	}

	// Redirect with success
	web.RedirectWithSuccessAndMessage(c, "toast.addedToVault")
}

// createPledge contains the core business logic for pledge creation (Level 2: Business logic)
func (h *Handler) createPledge(ctx context.Context, resourceID string, user *auth.User, apiClaims *api.Claims) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Validate claims
	if apiClaims == nil {
		return errors.New("failed to get claims")
	}

	// Get or create resource
	resource, err := h.vault.GetOrCreateResource(ctx, apiClaims, resourceID)
	if err != nil {
		return err
	}

	// Create pledge
	_, err = h.vault.CreatePledge(ctx, user, resource)
	if err != nil {
		return err
	}

	return nil
}

func (h *Handler) addTorrentToLibrary(c *gin.Context, resourceID string, user *auth.User, apiClaims *api.Claims) error {
	db := h.pg.Get()
	if db == nil {
		return errors.New("database connection is not available")
	}
	if h.api == nil {
		return errors.New("api service is not available")
	}
	if apiClaims == nil {
		return errors.New("failed to get claims")
	}

	torrentBytes, err := h.api.GetTorrentCached(c.Request.Context(), apiClaims, resourceID)
	if err != nil {
		return errors.Wrap(err, "failed to get torrent")
	}

	body := io.NopCloser(bytes.NewReader(torrentBytes))
	defer func() {
		_ = body.Close()
	}()

	mi, err := metainfo.Load(body)
	if err != nil {
		return errors.Wrap(err, "failed to load torrent metainfo")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal torrent info")
	}

	_, err = models.AddTorrentToLibrary(c.Request.Context(), db, user.ID, resourceID, &info, "", int64(len(torrentBytes)))
	if err != nil {
		return err
	}
	if h.jobs != nil {
		_, _ = h.jobs.Enrich(web.NewContext(c), resourceID)
	}

	return nil
}
