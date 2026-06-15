package embed

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/webtor-io/web-ui/services/web"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type GetData struct {
	CheckScript string
	CheckHash   string
	ID          string
}

func (s *Handler) get(c *gin.Context) {
	id := c.Query("id")
	code := uuid.New().String()
	h := sha1.New()
	h.Write([]byte(id + code))
	gd := GetData{
		CheckHash:   hex.EncodeToString(h.Sum(nil)),
		CheckScript: s.generateCheckScript(code, id),
		ID:          id,
	}
	s.tb.Build("embed/get").HTML(http.StatusOK, web.NewContext(c).WithData(gd))
}

func (s *Handler) generateCheckScript(code string, id string) string {
	codeJSON, _ := json.Marshal(code)
	idJSON, _ := json.Marshal(id)
	return fmt.Sprintf(`
		var found = false;
		var scripts = document.getElementsByTagName('script');
			for (var i = scripts.length; i--;) {
				if (
					scripts[i].src.includes('https://cdn.jsdelivr.net/npm/@octor/') ||
					scripts[i].src.includes('http://localhost:9009/')
				) {
					found = %s;
				}
			}
		var f = window.frames['octor-' + %s];
		f.contentWindow.postMessage({id: %s, name: 'check', data: found}, '*');
	`, string(codeJSON), string(idJSON), string(idJSON))
}
