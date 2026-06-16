package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"

	"github.com/gin-gonic/gin"

	"github.com/webtor-io/web-ui/services/template"
)

type LoginData struct {
	Instruction        string
	Card               *LoginCard
	InviteCodeRequired bool
	SMTPConfigured     bool
}

// LoginCard carries i18n keys for the contextual info card on /login. The
// concrete copy is owned by the locales (e.g. vault.signInCard.intro), the
// handler only decides which set of keys applies to the current `from` value.
type LoginCard struct {
	NoteKey     string
	IntroKey    string
	FeatureKeys []string
}

// loginCardFor returns the card descriptor for a given `from` value or nil if
// the `from` doesn't drive a contextual card. Keep the allowed set explicit so
// typos in the URL don't silently render an empty card.
func loginCardFor(from string) *LoginCard {
	switch from {
	case "vault":
		return &LoginCard{
			NoteKey:  "auth.login.vaultNote",
			IntroKey: "vault.signInCard.intro",
			FeatureKeys: []string{
				"vault.signInCard.feature1",
				"vault.signInCard.feature2",
				"vault.signInCard.feature3",
				"vault.signInCard.feature4",
			},
		}
	case "library":
		return &LoginCard{
			NoteKey:  "auth.login.libraryNote",
			IntroKey: "library.signInCard.intro",
			FeatureKeys: []string{
				"library.signInCard.feature1",
				"library.signInCard.feature2",
				"library.signInCard.feature3",
				"library.signInCard.feature4",
			},
		}
	case "discover":
		return &LoginCard{
			NoteKey:  "auth.login.discoverNote",
			IntroKey: "discover.signInCard.intro",
			FeatureKeys: []string{
				"discover.signInCard.feature1",
				"discover.signInCard.feature2",
				"discover.signInCard.feature3",
				"discover.signInCard.feature4",
			},
		}
	}
	return nil
}

type LogoutData struct{}

type ProcessAuthData struct {
	ReturnURL string
}

type Handler struct {
	tb template.Builder[*web.Context]
	a  *auth.Auth
}

func RegisterHandler(r *gin.Engine, tm *template.Manager[*web.Context], a *auth.Auth) {
	h := &Handler{
		tb: tm.MustRegisterViews("auth/*").WithLayout("main"),
		a:  a,
	}

	r.Use(func(c *gin.Context) {
		u := auth.GetUserFromContext(c)
		if u != nil && u.Expired {
			h.refresh(c)
			c.Abort()
			return
		}
	})

	r.GET("/login", h.login)
	r.GET("/refresh", h.refresh)
	r.GET("/logout", h.logout)
	r.GET("/auth/verify", h.verify)
	r.GET("/auth/callback/google", h.callback)
	r.POST("/auth/invite-code", h.setInviteCode)
}

type InviteCodeRequest struct {
	Email      string `json:"email"`
	InviteCode string `json:"inviteCode"`
}

func (s *Handler) setInviteCode(c *gin.Context) {
	var req InviteCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	if s.a.IsInviteCodeRequired() {
		isNew := true
		if req.Email != "" {
			var err error
			isNew, err = s.a.IsNewUser(c.Request.Context(), req.Email)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error checking user existence"})
				return
			}
		}

		if isNew {
			if req.InviteCode == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invite code is required to sign up"})
				return
			}
			if !s.a.IsInviteCodeValid(req.InviteCode) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid invite code"})
				return
			}
		}
	}

	session := sessions.Default(c)
	session.Set("invite-code", req.InviteCode)
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (s *Handler) refresh(c *gin.Context) {
	s.tb.Build("auth/refresh").HTML(http.StatusOK, web.NewContext(c))
}

func (s *Handler) login(c *gin.Context) {
	instruction := "default"
	if c.Query("from") != "" {
		instruction = c.Query("from")
	}
	ld := LoginData{
		Instruction:        instruction,
		Card:               loginCardFor(instruction),
		InviteCodeRequired: s.a.IsInviteCodeRequired(),
		SMTPConfigured:     s.a.IsSMTPConfigured(),
	}
	if c.Query("return-url") != "" {
		session := sessions.Default(c)
		session.Set("return-url", c.Query("return-url"))
		_ = session.Save()
	}
	s.tb.Build("auth/login").HTML(http.StatusOK, web.NewContext(c).WithData(ld))
}

func (s *Handler) logout(c *gin.Context) {
	s.tb.Build("auth/logout").HTML(http.StatusOK, web.NewContext(c).WithData(LogoutData{}))
}

func (s *Handler) verify(c *gin.Context) {
	s.processAuth(c, "auth/verify")
}

func (s *Handler) callback(c *gin.Context) {
	s.processAuth(c, "auth/callback")
}

func (s *Handler) processAuth(c *gin.Context, t string) {
	session := sessions.Default(c)
	var returnURL string
	if session.Get("return-url") != nil {
		returnURL = session.Get("return-url").(string)
		session.Delete("return-url")
		_ = session.Save()
	}
	s.tb.Build(t).HTML(http.StatusOK, web.NewContext(c).WithData(&ProcessAuthData{
		ReturnURL: returnURL,
	}))
}
