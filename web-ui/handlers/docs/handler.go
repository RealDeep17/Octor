package docs

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb template.Builder[*web.Context]
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context]) {
	h := &Handler{
		tb: tm.MustRegisterViews("docs").WithLayout("main"),
	}

	r.GET("/docs", h.get)
}

func (h *Handler) get(c *gin.Context) {
	h.tb.Build("docs").HTML(http.StatusOK, web.NewContext(c))
}
