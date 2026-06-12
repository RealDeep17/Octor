package admin

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/web"
)

func (h *Handler) runEnrichPool(ctx context.Context, ids []string, force bool, label string) {
	sem := make(chan struct{}, h.enricher.Concurrency)
	var wg sync.WaitGroup

	db, err := h.db()
	if err != nil {
		log.WithError(err).Errorf("%s: failed to get DB", label)
		return
	}
	_ = db

	total := len(ids)
	log.Infof("%s: processing %d resources with %d concurrent workers", label, total, h.enricher.Concurrency)

	for i, id := range ids {
		id := id
		i := i
		wg.Add(1)
		select {
		case <-ctx.Done():
			wg.Done()
			log.Infof("%s: context cancelled, stopping queue insertion", label)
			goto loopExit
		case sem <- struct{}{}:
		}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			log.Infof("%s [%d/%d]: enriching %s", label, i+1, total, id)
			if err := h.enricher.Enrich(ctx, id, &api.Claims{}, force, ""); err != nil {
				log.WithError(err).Warnf("%s: failed on resource %s", label, id)
			}
		}()
	}
loopExit:

	wg.Wait()
	log.Infof("%s: completed", label)
}

func (h *Handler) refreshEnrichment(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer cancel()

		ids, err := models.GetStaleOrMissingMetadataResourceIDs(bgCtx, db, 7*24*time.Hour)
		if err != nil {
			log.WithError(err).Error("Smart Refresh: failed to query stale resources")
			return
		}
		h.runEnrichPool(bgCtx, ids, false, "Smart Refresh")
	}()

	web.RedirectWithSuccessAndMessage(c, "toast.enrichmentRefreshStarted")
}

func (h *Handler) forceAllEnrichment(c *gin.Context) {
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()

		db, err := h.db()
		if err != nil {
			log.WithError(err).Error("Force All: failed to get DB")
			return
		}

		resources, err := models.GetActiveResources(bgCtx, db)
		if err != nil {
			log.WithError(err).Error("Force All: failed to query active resources")
			return
		}

		ids := make([]string, len(resources))
		for i, r := range resources {
			ids[i] = r.ResourceID
		}
		h.runEnrichPool(bgCtx, ids, true, "Force All")
	}()

	web.RedirectWithSuccessAndMessage(c, "toast.forceAllEnrichmentStarted")
}

func (h *Handler) forceEverythingEnrichment(c *gin.Context) {
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()

		db, err := h.db()
		if err != nil {
			log.WithError(err).Error("Force Everything: failed to get DB")
			return
		}

		resources, err := models.GetAllResources(bgCtx, db)
		if err != nil {
			log.WithError(err).Error("Force Everything: failed to query all resources")
			return
		}

		ids := make([]string, len(resources))
		for i, r := range resources {
			ids[i] = r.ResourceID
		}
		h.runEnrichPool(bgCtx, ids, true, "Force Everything")
	}()

	web.RedirectWithSuccessAndMessage(c, "toast.forceEverythingEnrichmentStarted")
}
