package donate

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type Handler struct {
}

func RegisterHandler(r *gin.Engine) {
	h := &Handler{}
	r.GET("/donate", h.get)
}

func (h *Handler) get(c *gin.Context) {
	c.Redirect(http.StatusTemporaryRedirect, "/")
}
