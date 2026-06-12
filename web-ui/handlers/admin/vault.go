package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type AdminVaultStats struct {
	TotalVaultedGB  float64
	TotalGDriveGB   float64
	SavedCount      int
	ProcessingCount int
}

type VaultData struct {
	Args         *shared.IndexArgs
	Users        []UserOption
	SelectedUser string
	Pledges      []AdminPledgeDisplay
	Stats        AdminVaultStats
}

type AdminPledgeDisplay struct {
	vaultModels.Pledge
	WorkerStatus *vault.Resource
	SeedCount    int
}

func (a AdminPledgeDisplay) ShowProgress() bool {
	return a.Resource != nil &&
		a.Resource.Funded &&
		!a.Resource.Vaulted &&
		!a.Resource.Expired
}

func (h *Handler) vaultIndex(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	userID, selected, err := selectedUserID(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	var pledges []vaultModels.Pledge
	query := db.Model(&pledges).
		Context(ctx).
		Relation("Resource").
		Relation("User").
		Order("pledge.created_at DESC")
	if userID != nil {
		query.Where("pledge.user_id = ?", *userID)
	}
	if q != "" {
		query.Where("resource.name ILIKE ?", "%"+q+"%")
	}
	if err := query.Select(); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to load admin vault"))
		return
	}

	enrichedPledges := make([]AdminPledgeDisplay, 0, len(pledges))
	for _, p := range pledges {
		item := AdminPledgeDisplay{Pledge: p}
		if p.Resource != nil && p.Resource.Funded && !p.Resource.Vaulted && !p.Resource.Expired {
			status, err := h.vault.GetVaultAPIResource(ctx, p.ResourceID)
			if err == nil && status != nil {
				// We map vault.Resource to any or wait, in vault.go the package is "github.com/webtor-io/web-ui/services/vault"
				// So we can use type assertion or import vault
				item.WorkerStatus = status
			}
			item.SeedCount = h.getLiveSeeds(ctx, c, p.ResourceID)
		}
		enrichedPledges = append(enrichedPledges, item)
	}

	users, err := h.loadUsers(ctx, db, selected)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	var stats AdminVaultStats
	if userID != nil {
		var dbStats struct {
			TotalVaultedGB  float64 `pg:"total_vaulted_gb"`
			SavedCount      int     `pg:"saved_count"`
			ProcessingCount int     `pg:"processing_count"`
		}
		_, _ = db.QueryOneContext(ctx, &dbStats, `
			SELECT 
				COALESCE(SUM(CASE WHEN r.vaulted = true AND r.expired = false THEN r.required_vp ELSE 0 END), 0) as total_vaulted_gb,
				COUNT(CASE WHEN r.vaulted = true AND r.expired = false THEN 1 END) as saved_count,
				COUNT(CASE WHEN r.vaulted = false AND r.expired = false THEN 1 END) as vaulting_count
			FROM vault.pledge p
			JOIN vault.resource r ON p.resource_id = r.resource_id
			WHERE p.user_id = ?
		`, *userID)
		stats.TotalVaultedGB = dbStats.TotalVaultedGB
		stats.SavedCount = dbStats.SavedCount
		stats.ProcessingCount = dbStats.ProcessingCount
	} else {
		var dbStats struct {
			TotalVaultedGB  float64 `pg:"total_vaulted_gb"`
			SavedCount      int     `pg:"saved_count"`
			ProcessingCount int     `pg:"processing_count"`
		}
		_, _ = db.QueryOneContext(ctx, &dbStats, `
			SELECT 
				COALESCE(SUM(CASE WHEN vaulted = true AND expired = false THEN required_vp ELSE 0 END), 0) as total_vaulted_gb,
				COUNT(CASE WHEN vaulted = true AND expired = false THEN 1 END) as saved_count,
				COUNT(CASE WHEN vaulted = false AND expired = false THEN 1 END) as processing_count
			FROM vault.resource
		`)
		stats.TotalVaultedGB = dbStats.TotalVaultedGB
		stats.SavedCount = dbStats.SavedCount
		stats.ProcessingCount = dbStats.ProcessingCount
	}

	if h.vault != nil {
		stats.TotalGDriveGB = h.vault.GetTotalSpaceGB()
	}

	h.tb.Build("admin/vault").HTML(http.StatusOK, web.NewContext(c).WithData(&VaultData{
		Args: &shared.IndexArgs{
			Query: q,
		},
		Users:        users,
		SelectedUser: selected,
		Pledges:      enrichedPledges,
		Stats:        stats,
	}))
}

func (h *Handler) removePledge(c *gin.Context) {
	uIDRaw := c.PostForm("user_id")
	rID := c.PostForm("resource_id")
	alsoLibrary := c.PostForm("also_library") == "true"
	allUsers := c.PostForm("all_users") == "true"

	if rID == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	if allUsers {
		pledges, err := vaultModels.GetResourcePledges(c.Request.Context(), db, rID)
		if err == nil {
			for _, p := range pledges {
				pCopy := p
				_ = h.vault.RemovePledge(c.Request.Context(), &pCopy)
			}
		}
		if alsoLibrary {
			_, _ = db.Model((*models.Library)(nil)).
				Context(c.Request.Context()).
				Where("resource_id = ?", rID).
				Delete()
		}
		web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
		return
	}

	if uIDRaw == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	uID, err := uuid.FromString(uIDRaw)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid user id"))
		return
	}

	resource, err := h.vault.GetResource(c.Request.Context(), rID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get resource"))
		return
	}

	if resource == nil {
		c.Status(http.StatusNotFound)
		return
	}

	pledge, err := h.vault.GetPledge(c.Request.Context(), &auth.User{ID: uID}, resource)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get pledge"))
		return
	}

	if pledge == nil {
		c.Status(http.StatusNotFound)
		return
	}

	if err := h.vault.RemovePledge(c.Request.Context(), pledge); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to remove pledge"))
		return
	}

	if alsoLibrary {
		_ = models.RemoveFromLibrary(c.Request.Context(), db, uID, rID)
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}

func (h *Handler) removeMultiplePledge(c *gin.Context) {
	var req struct {
		UserIDs     []string `form:"user_ids[]"`
		ResourceIDs []string `form:"resource_ids[]"`
		AllUsers    string   `form:"all_users"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	allUsers := req.AllUsers == "true"
	alsoLibrary := c.PostForm("also_library") == "true"

	ctx := c.Request.Context()
	db, err := h.db()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	for i, rIDRaw := range req.ResourceIDs {
		rID := strings.TrimSpace(rIDRaw)
		if rID == "" {
			continue
		}

		if allUsers {
			pledges, err := vaultModels.GetResourcePledges(ctx, db, rID)
			if err == nil {
				for _, p := range pledges {
					pCopy := p
					_ = h.vault.RemovePledge(ctx, &pCopy)
				}
			}
			if alsoLibrary {
				_, _ = db.Model((*models.Library)(nil)).
					Context(ctx).
					Where("resource_id = ?", rID).
					Delete()
			}
		} else {
			if i >= len(req.UserIDs) {
				continue
			}
			uIDRaw := strings.TrimSpace(req.UserIDs[i])
			if uIDRaw == "" {
				continue
			}

			uID, err := uuid.FromString(uIDRaw)
			if err != nil {
				continue
			}

			resource, err := h.vault.GetResource(ctx, rID)
			if err != nil || resource == nil {
				continue
			}

			user := &auth.User{ID: uID}
			pledge, err := h.vault.GetPledge(ctx, user, resource)
			if err != nil || pledge == nil {
				continue
			}

			if err := h.vault.RemovePledge(ctx, pledge); err != nil {
				log.WithError(err).WithField("resource_id", rID).Warn("admin failed to bulk delete pledge")
				continue
			}
		}

		if h.api != nil {
			claims := api.GetClaimsFromContext(c)
			purgeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if purgeErr := h.api.PurgeResourceCache(purgeCtx, claims, rID); purgeErr != nil {
				log.WithError(purgeErr).WithField("resource_id", rID).Warn("failed to purge seeder cache after admin bulk vault removal")
			}
			cancel()
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.removedFromVault")
}

func (h *Handler) retryMultiplePledge(c *gin.Context) {
	var req struct {
		UserIDs     []string `form:"user_ids[]"`
		ResourceIDs []string `form:"resource_ids[]"`
	}

	if err := c.ShouldBind(&req); err != nil || len(req.UserIDs) != len(req.ResourceIDs) {
		c.Status(http.StatusBadRequest)
		return
	}

	ctx := c.Request.Context()

	for i, rIDRaw := range req.ResourceIDs {
		rID := strings.TrimSpace(rIDRaw)
		uIDRaw := strings.TrimSpace(req.UserIDs[i])

		if rID == "" || uIDRaw == "" {
			continue
		}

		uID, err := uuid.FromString(uIDRaw)
		if err != nil {
			continue
		}

		resource, err := h.vault.GetResource(ctx, rID)
		if err != nil || resource == nil {
			continue
		}

		user := &auth.User{ID: uID}
		pledge, err := h.vault.GetPledge(ctx, user, resource)
		if err != nil || pledge == nil {
			continue
		}

		_, err = h.vault.PutResource(ctx, rID)
		if err != nil {
			log.WithError(err).WithField("resource_id", rID).Warn("admin failed to bulk retry pledge")
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.vaultRetrying")
}

func (h *Handler) getLiveSeeds(ctx context.Context, c *gin.Context, resourceID string) int {
	wcc := web.NewContext(c)
	er, err := h.api.ExportResourceContent(ctx, wcc.ApiClaims, resourceID, resourceID, "")
	if err != nil {
		return 0
	}
	statsURL, ok := er.ExportItems["stats"]
	if !ok || statsURL.URL == "" {
		return 0
	}

	shortCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	ch, err := h.api.Stats(shortCtx, statsURL.URL)
	if err != nil {
		return 0
	}

	select {
	case event, ok := <-ch:
		if ok {
			return event.Peers
		}
	case <-shortCtx.Done():
		return 0
	}
	return 0
}
