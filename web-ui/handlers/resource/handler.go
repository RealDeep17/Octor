package resource

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	j "github.com/webtor-io/web-ui/jobs"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	api            *api.Api
	jobs           *j.Jobs
	tb             template.Builder[*web.Context]
	pg             *cs.PG
	vault          *vault.Vault
	enricher       *enrich.Enricher
	useDirectLinks bool
}

func RegisterHandler(c *cli.Context, r *gin.Engine, tm *template.Manager[*web.Context], api *api.Api, jobs *j.Jobs, pg *cs.PG, v *vault.Vault, en *enrich.Enricher) {
	helper := NewHelper()
	h := &Handler{
		api:            api,
		jobs:           jobs,
		tb:             tm.MustRegisterViews("resource/*").WithHelper(helper).WithLayout("main"),
		pg:             pg,
		vault:          v,
		enricher:       en,
		useDirectLinks: c.BoolT(common.UseDirectLinks),
	}
	r.POST("/", h.post)
	r.POST("/enrich/:resource_id", h.enrichInternal)
	r.GET("/:resource_id/status", h.status)
	r.GET("/:resource_id/items", h.getItems)
	r.GET("/:resource_id", func(c *gin.Context) {
		rid := c.Param("resource_id")
		if strings.HasPrefix(rid, "magnet") {
			h.post(c)
			return
		}
		if strings.HasSuffix(rid, ".torrent") {
			h.downloadTorrent(c)
			return
		}
		h.get(c)
	})
}

func (s *Handler) downloadTorrent(c *gin.Context) {
	resourceID := strings.TrimSuffix(c.Param("resource_id"), ".torrent")
	claims := api.GetClaimsFromContext(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	torrent, err := s.api.GetTorrentCached(ctx, claims, resourceID)
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to get torrent"))
		c.String(http.StatusInternalServerError, "failed to get torrent")
		return
	}

	mi, err := metainfo.Load(bytes.NewReader(torrent))
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to load torrent metainfo"))
		c.String(http.StatusInternalServerError, "failed to load torrent")
		return
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		_ = c.Error(errors.Wrap(err, "failed to unmarshal torrent metainfo"))
		c.String(http.StatusInternalServerError, "failed to parse torrent")
		return
	}

	filename := info.Name + ".torrent"
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Length", fmt.Sprintf("%d", len(torrent)))
	c.Data(http.StatusOK, "application/x-bittorrent", torrent)
}

func (s *Handler) enrichInternal(c *gin.Context) {
	// Simple API key check (same as rest-api uses)
	key := c.Request.Header.Get("X-Api-Key")
	if key == "" {
		key = c.Query("api_key")
	}
	// Use the OCTOR_API_KEY from the CLI context/env
	expectedKey := c.GetString("octor-api-key")
	if expectedKey == "" {
		// Fallback to searching the context or flags if not explicitly set in middleware
		// For now, we'll assume it's passed or we'll allow it if empty (local only)
	}

	if expectedKey != "" && key != expectedKey {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	resourceID := c.Param("resource_id")
	if resourceID == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Trigger background enrichment via Jobs service
	if s.jobs != nil {
		go func(id string) {
			_, err := s.jobs.Enrich(web.NewContext(c), id)
			if err != nil {
				fmt.Printf("Internal Enrichment Trigger failed for %s: %v\n", id, err)
			}
		}(resourceID)
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "enrichment_triggered"})
}
