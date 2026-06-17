package admin

import (
	"sync"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	cs "github.com/webtor-io/common-services"
	libHelpers "github.com/webtor-io/web-ui/handlers/library/helpers"
	"github.com/webtor-io/web-ui/models"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/vault"
	uvs "github.com/webtor-io/web-ui/services/user_video_status"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb             template.Builder[*web.Context]
	pg             *cs.PG
	vault          *vault.Vault
	enricher       *enrich.Enricher
	admin          *adminsvc.Admin
	api            *api.Api
	enrichmentLock sync.Mutex
	uvs            *uvs.Service
}

type UserOption struct {
	ID            string
	Email         string
	Selected      bool
	Tier          string
	VaultedCount  int
	VaultingCount int
}

type OwnerSummary struct {
	ResourceID    string    `pg:"resource_id"`
	UserCount     int       `pg:"user_count"`
	OwnerEmails   string    `pg:"owner_emails"`
	OwnerIDs      string    `pg:"owner_ids"`
	PrimaryEmail  string    `pg:"primary_email"`
	PrimaryUserID uuid.UUID `pg:"primary_user_id"`
}

type AdminVideoItem struct {
	Content       models.VideoContentWithMetadata
	ResourceID    string
	CreatedAt     time.Time
	OwnerCount    int
	OwnerEmails   []string
	OwnerIDs      []uuid.UUID
	PrimaryEmail  string
	PrimaryUserID uuid.UUID
}

func (i AdminVideoItem) OwnerLabel() string {
	if i.OwnerCount <= 0 {
		return "No users"
	}
	if i.OwnerCount == 1 {
		if i.PrimaryEmail != "" {
			return i.PrimaryEmail
		}
		if len(i.OwnerEmails) > 0 {
			return i.OwnerEmails[0]
		}
		return "1 user"
	}
	return fmt.Sprintf("%d users", i.OwnerCount)
}

func (i AdminVideoItem) OwnerTitle() string {
	if len(i.OwnerEmails) == 0 {
		return i.OwnerLabel()
	}
	return strings.Join(i.OwnerEmails, ", ")
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, v *vault.Vault, en *enrich.Enricher, admin *adminsvc.Admin, sapi *api.Api) {
	h := &Handler{
		tb:             tm.MustRegisterViews("admin/*").WithHelper(libHelpers.NewVideoContentHelper()).WithLayout("main"),
		pg:             pg,
		vault:          v,
		enricher:       en,
		admin:          admin,
		api:            sapi,
		uvs:            uvs.New(pg.Get()),
	}

	// Start System I/O Stats Collector background thread
	go StartStatsCollector(pg.Get())

	gr := r.Group("/admin")
	gr.Use(admin.Require())
	gr.GET("", func(c *gin.Context) { c.Redirect(http.StatusFound, i18n.LangPath(i18n.GetLang(c), "/admin/library")) })
	
	lg := gr.Group("/library")
	lg.GET("", h.library)
	lg.GET("/:type", h.library)
	lg.POST("/remove", h.remove)
	lg.POST("/remove-multiple", h.removeMultiple)
	lg.POST("/enrich-multiple", h.enrichMultiple)
	lg.POST("/layout-multiple", h.layoutMultiple)
	lg.POST("/toggle-multiple", h.toggleMultiple)

	gr.GET("/vault", h.vaultIndex)
	gr.POST("/vault/remove", h.removePledge)
	gr.POST("/vault/remove-multiple", h.removeMultiplePledge)
	gr.POST("/vault/retry", h.retryPledge)
	gr.POST("/vault/retry-multiple", h.retryMultiplePledge)
	gr.GET("/status", h.status)
	gr.POST("/status/action", h.statusAction)
	gr.GET("/auth-check", h.authCheck)
	gr.GET("/drive", h.driveIndex)
	gr.GET("/drive/*path", h.driveIndex)
	gr.POST("/enrichment/refresh", h.refreshEnrichment)
	gr.POST("/enrichment/force-all", h.forceAllEnrichment)
	gr.POST("/enrichment/force-everything", h.forceEverythingEnrichment)
	gr.GET("/management", h.management)
	gr.POST("/management/delete", h.deleteUser)
	gr.POST("/management/move", h.migrateUser)
	gr.GET("/settings", h.settingsIndex)
	gr.POST("/settings", h.settingsSave)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (h *Handler) db() (*pg.DB, error) {
	db := h.pg.Get()
	if db == nil {
		return nil, errors.New("no db")
	}
	return db, nil
}

func (h *Handler) authCheck(c *gin.Context) {
	c.Status(http.StatusOK)
}
