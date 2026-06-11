package index

import (
	"net/http"
	"strings"
	"time"

	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/handlers/common"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
)

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

type Data struct {
	Instruction      string
	Tool             *common.Tool
	ContinueWatching []*models.WatchHistory
	Addons           []addonView
	SearchQuery      string
}

type Handler struct {
	tb template.Builder[*web.Context]
	pg *cs.PG
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG) {
	h := &Handler{
		tb: tm.MustRegisterViews("*").WithLayout("main"),
		pg: pg,
	}
	r.GET("/", h.index)
	r.HEAD("/", h.index)
	for _, tool := range common.Tools {
		r.GET("/"+tool.Url, h.index)
		r.HEAD("/"+tool.Url, h.index)
	}
}

func (s *Handler) index(c *gin.Context) {
	instruction := strings.TrimPrefix(c.Request.URL.Path, "/")

	// Find the matching tool based on the current URL
	var currentTool *common.Tool
	for i := range common.Tools {
		if common.Tools[i].Url == instruction {
			currentTool = &common.Tools[i]
			break
		}
	}

	data := &Data{
		Instruction: instruction,
		Tool:        currentTool,
		SearchQuery: c.Query("q"),
	}

	// Fetch continue watching and addons for authenticated users
	user := auth.GetUserFromContext(c)
	if user.HasAuth() {
		if db := s.pg.Get(); db != nil {
			if currentTool == nil {
				data.ContinueWatching, _ = models.GetRecentlyWatched(c.Request.Context(), db, user.ID, 10)
			}
			addons, err := models.GetUserStremioAddonUrls(c.Request.Context(), db, user.ID)
			if err == nil {
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
				data.Addons = views
			}
		}
	}

	ctx := web.NewContext(c).WithData(data)

	if c.Query("status") == "error" && c.Query("err") != "" {
		ctx = ctx.WithErrKey(c.Query("err"))
	}

	s.tb.Build("index").HTML(http.StatusOK, ctx)
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
