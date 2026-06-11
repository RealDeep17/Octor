package web

import (
	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/geo"
	"github.com/webtor-io/web-ui/handlers/session"
	"github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/geoip"
	"github.com/webtor-io/web-ui/services/i18n"
)

type Context struct {
	Data        any
	CSRF        string
	SessionID   string
	ErrKey      string
	User        *auth.User
	Claims      *claims.Data
	TierUpdated bool
	Geo         *geoip.Data
	ApiClaims   *api.Claims
	Lang        string
	Path        string
	IsAdmin     bool
	ginCtx      *gin.Context
}

func (c *Context) WithData(obj any) *Context {
	nc := *c
	nc.Data = obj
	return &nc
}

// WithErrKey sets an i18n error key directly (e.g. from query params).
func (c *Context) WithErrKey(key string) *Context {
	nc := *c
	nc.ErrKey = key
	return &nc
}

// WithErr classifies the error into an i18n key and logs the original.
func (c *Context) WithErr(err error) *Context {
	nc := *c
	nc.ErrKey = ClassifyError(err)
	return &nc
}

func (s *Context) GetGinContext() *gin.Context {
	return s.ginCtx
}

func NewContext(c *gin.Context) *Context {
	user := auth.GetUserFromContext(c)
	cl := claims.GetFromContext(c)
	sess := session.GetFromContext(c)
	geoData := geo.GetFromContext(c)
	aCl := api.GetClaimsFromContext(c)
	tu := claims.GetTierUpdateFromContext(c)
	lang := i18n.GetLang(c)

	var isAdmin bool
	if adminSvcVal, exists := c.Get("admin_svc"); exists {
		if adminSvc, ok := adminSvcVal.(*admin.Admin); ok {
			isAdmin = adminSvc.IsAdminUser(user)
		}
	}

	var csrf, sessionID string
	if sess != nil {
		csrf = sess.CSRF
		sessionID = sess.ID
	}

	return &Context{
		CSRF:        csrf,
		User:        user,
		Claims:      cl,
		ApiClaims:   aCl,
		SessionID:   sessionID,
		Geo:         geoData,
		TierUpdated: tu,
		Lang:        lang,
		Path:        c.Request.URL.Path,
		IsAdmin:     isAdmin,
		ginCtx:      c,
	}
}
