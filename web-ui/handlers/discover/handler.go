package discover

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/adultposter"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/enrich"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/javguru"
	"github.com/webtor-io/web-ui/services/prowlarr"
	"github.com/webtor-io/web-ui/services/stashdb"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/tpdb"
	"github.com/webtor-io/web-ui/services/web"
)

// addonView is the per-addon shape we serialize into the page bootstrap
// (window._addons). Carries the manifest snapshot we captured at add
// time so the JS client can render names + capabilities in the
// AddonHealthChip and CatalogSelector before manifests are fetched. The
// JS client lazily refreshes the snapshot via /stremio/addon-url/:id/
// refresh-snapshot when it sees a fresh manifest from an addon whose
// snapshot is missing or older than 7 days.
type addonView struct {
	ID         string     `json:"id"`
	URL        string     `json:"url"`
	Name       string     `json:"name,omitempty"`
	Logo       string     `json:"logo,omitempty"`
	ManifestID string     `json:"manifestId,omitempty"`
	Version    string     `json:"version,omitempty"`
	Resources  []string   `json:"resources,omitempty"`
	Types      []string   `json:"types,omitempty"`
	FetchedAt  *time.Time `json:"fetchedAt,omitempty"`
}

type indexData struct {
	Addons          []addonView
	StremioSettings *models.StremioSettingsData
	IsAdmin         bool
}

type Handler struct {
	tb    template.Builder[*web.Context]
	pg    *cs.PG
	api   *api.Api
	admin *admin.Admin
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, api *api.Api, tpdbSvc *tpdb.Service, adminSvc *admin.Admin, redis *cs.RedisClient, javGuruSvc *javguru.Service, stashdbSvc *stashdb.Service, prowlarrSvc *prowlarr.Service, posterSvc *adultposter.Service) {
	h := &Handler{
		tb:    tm.MustRegisterViews("discover/*").WithLayout("main"),
		pg:    pg,
		api:   api,
		admin: adminSvc,
	}
	r.GET("/discover", h.index)
	r.GET("/discover/search", h.search)
	RegisterAdultRoutes(r, tpdbSvc, adminSvc, redis, javGuruSvc, stashdbSvc, prowlarrSvc, posterSvc)
}

func (h *Handler) search(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=14400")
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query q"})
		return
	}

	cl := api.GetClaimsFromContext(c)
	res, err := h.api.SearchSFW(c.Request.Context(), cl, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// SFW guard: filter out adult/NSFW results from search output
	var items []map[string]interface{}
	if err := json.Unmarshal(res, &items); err == nil {
		var filtered []map[string]interface{}
		for _, item := range items {
			id, _ := item["id"].(string)
			if id != "" {
				idLower := strings.ToLower(id)
				if strings.HasPrefix(idLower, "tpdb") || strings.HasPrefix(idLower, "stash") {
					continue
				}
			}
			title, _ := item["title"].(string)
			if title != "" {
				isAdult, _ := enrich.IsAdultPath(title)
				if isAdult {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		if filteredRes, errMarshal := json.Marshal(filtered); errMarshal == nil {
			res = filteredRes
		}
	}

	c.Data(http.StatusOK, "application/json", res)
}

func (h *Handler) index(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		// Preserve the deep-link query (?id=ttXXXX&type=movie) and lang prefix
		// so a guest landing on /ru/discover?id=… ends up back on the same
		// title in the same language after signing in. The login page renders
		// a contextual info card driven by from=discover.
		lang := i18n.GetLang(c)
		returnURL := i18n.LangPath(lang, "/discover")
		if rq := c.Request.URL.RawQuery; rq != "" {
			returnURL += "?" + rq
		}
		v := url.Values{
			"from":       []string{"discover"},
			"return-url": []string{returnURL},
		}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}

	db := h.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	addons, err := models.GetUserStremioAddonUrls(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get addon urls"))
		return
	}
	stremioSettings, err := models.GetUserStremioSettingsData(c.Request.Context(), db, u.ID)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get stremio settings"))
		return
	}

	views := make([]addonView, len(addons))
	for i, a := range addons {
		views[i] = addonView{
			ID:         a.ID.String(),
			URL:        a.Url,
			Name:       derefStr(a.Name),
			Logo:       derefStr(a.ManifestLogo),
			ManifestID: derefStr(a.ManifestID),
			Version:    derefStr(a.ManifestVersion),
			Resources:  a.ManifestResources,
			Types:      a.ManifestTypes,
			FetchedAt:  a.ManifestFetchedAt,
		}
	}

	wctx := web.NewContext(c)
	h.tb.Build("discover/index").HTML(http.StatusOK, wctx.WithData(&indexData{
		Addons:          views,
		StremioSettings: stremioSettings,
		IsAdmin:         wctx.IsAdmin || h.admin.HasAdmin(c),
	}))
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
