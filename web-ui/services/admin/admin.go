package admin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/urfave/cli"
	"github.com/webtor-io/web-ui/services/auth"
)

const EmailsFlag = "admin-emails"

type Admin struct {
	emails map[string]struct{}
}

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f, cli.StringFlag{
		Name:   EmailsFlag,
		Usage:  "comma-separated email addresses with admin access",
		EnvVar: "ADMIN_EMAILS",
	})
}

func New(c *cli.Context) *Admin {
	a := &Admin{emails: map[string]struct{}{}}
	for _, email := range strings.Split(c.String(EmailsFlag), ",") {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		a.emails[email] = struct{}{}
	}
	return a
}

func (a *Admin) IsAdminEmail(email string) bool {
	if a == nil {
		return false
	}
	_, ok := a.emails[strings.ToLower(strings.TrimSpace(email))]
	return ok
}

func (a *Admin) IsAdminUser(u *auth.User) bool {
	return u != nil && u.HasAuth() && a.IsAdminEmail(u.Email)
}

func (a *Admin) HasAdmin(c *gin.Context) bool {
	return a.IsAdminUser(auth.GetUserFromContext(c))
}

func (a *Admin) Require() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.HasAdmin(c) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
