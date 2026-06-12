package vault

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
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

	selectedFiles := c.PostFormArray("selected_files[]")
	if len(selectedFiles) == 0 {
		selectedFiles = c.PostFormArray("selected_files")
	}

	// Call business logic
	err := h.createPledge(c.Request.Context(), resourceID, user, apiClaims, selectedFiles)
	if err != nil {
		web.RedirectWithError(c, err)
		return
	}

	// Create pledge succeeded, so we publish the vault update event
	if h.nats != nil && h.nats.Get() != nil {
		_ = h.nats.Get().Publish(fmt.Sprintf("user.%s.update", user.ID.String()), []byte(`{"type": "vault"}`))
	}

	if err = h.addTorrentToLibrary(c, resourceID, user, apiClaims); err != nil {
		web.RedirectWithError(c, errors.Wrap(err, "failed to add torrent to library"))
		return
	}

	// If we successfully added to library, publish the library update event
	if h.nats != nil && h.nats.Get() != nil {
		_ = h.nats.Get().Publish(fmt.Sprintf("user.%s.update", user.ID.String()), []byte(`{"type": "library"}`))
	}

	// Redirect with success
	web.RedirectWithSuccessAndMessage(c, "toast.addedToVault")
}

// createPledge contains the core business logic for pledge creation (Level 2: Business logic)
func (h *Handler) createPledge(ctx context.Context, resourceID string, user *auth.User, apiClaims *api.Claims, selectedFiles []string) error {
	// Validate resource_id
	if resourceID == "" {
		return errors.New("resource_id is required")
	}

	// Validate claims
	if apiClaims == nil {
		return errors.New("failed to get claims")
	}

	// Calculate required VP
	var requiredVP float64
	if len(selectedFiles) > 0 {
		list, err := h.api.ListResourceContentCached(ctx, apiClaims, resourceID, &api.ListResourceContentArgs{
			Output: api.OutputList,
			Limit:  10000,
		})
		if err == nil {
			// If all files are selected, skip selective vaulting
			if len(selectedFiles) >= len(list.Items) {
				selectedFiles = nil
				requiredVP, err = h.vault.GetRequiredVP(ctx, apiClaims, resourceID)
				if err != nil {
					return err
				}
			} else {
				var selectedBytes int64
				selMap := make(map[string]bool, len(selectedFiles))
				for _, sel := range selectedFiles {
					selMap[sel] = true
				}
				for _, file := range list.Items {
					if selMap[file.PathStr] {
						selectedBytes += file.Size
					}
				}
				requiredVP = float64(selectedBytes) / (1024 * 1024 * 1024)
			}
		} else {
			reqVP, err := h.vault.GetRequiredVP(ctx, apiClaims, resourceID)
			if err != nil {
				return err
			}
			requiredVP = reqVP
		}
	} else {
		reqVP, err := h.vault.GetRequiredVP(ctx, apiClaims, resourceID)
		if err != nil {
			return err
		}
		requiredVP = reqVP
	}

	// Get or create resource
	resource, err := h.vault.GetOrCreateResource(ctx, apiClaims, resourceID)
	if err != nil {
		return err
	}

	// Update RequiredVP and SelectedFiles to match the current selection (entire resource or selective files)
	db := h.pg.Get()
	if db != nil {
		q := db.Model((*vaultModels.Resource)(nil)).
			Context(ctx).
			Set("required_vp = ?", requiredVP)

		if len(selectedFiles) > 0 {
			q = q.Set("selected_files = ?", pg.Array(selectedFiles))
		} else {
			q = q.Set("selected_files = NULL")
		}

		_, err := q.Where("resource_id = ?", resourceID).Update()
		if err != nil {
			return errors.Wrap(err, "failed to update resource required VP and selected files")
		}
		resource.RequiredVP = requiredVP
		resource.SelectedFiles = selectedFiles
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
