package series

import (
	"github.com/gin-gonic/gin"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	tb    template.Builder[*web.Context]
	pg    *cs.PG
	admin *admin.Admin
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], pg *cs.PG, adminSvc *admin.Admin) {
	h := &Handler{
		tb:    tm.MustRegisterViews("series/*").WithLayout("main"),
		pg:    pg,
		admin: adminSvc,
	}
	r.GET("/series/:video_id", h.get)
}
