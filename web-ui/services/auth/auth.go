package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	sv "github.com/webtor-io/web-ui/services/common"

	defaultErrors "errors"
	"sync"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/supertokens/supertokens-golang/ingredients/emaildelivery"
	"github.com/supertokens/supertokens-golang/recipe/dashboard"
	"github.com/supertokens/supertokens-golang/recipe/passwordless"
	"github.com/supertokens/supertokens-golang/recipe/passwordless/plessmodels"
	"github.com/supertokens/supertokens-golang/recipe/session"
	"github.com/supertokens/supertokens-golang/recipe/session/errors"
	"github.com/supertokens/supertokens-golang/recipe/session/sessmodels"
	"github.com/supertokens/supertokens-golang/recipe/thirdparty"
	"github.com/supertokens/supertokens-golang/recipe/thirdparty/tpmodels"
	"github.com/supertokens/supertokens-golang/recipe/usermetadata"
	"github.com/supertokens/supertokens-golang/recipe/userroles"
	"github.com/supertokens/supertokens-golang/supertokens"
	"github.com/urfave/cli"
)

const (
	SupertokensHostFlag    = "supertokens-host"
	SupertokensPortFlag    = "supertokens-port"
	googleClientIDFlag     = "google-client-id"
	googleClientSecretFlag = "google-client-secret"
	overrideUserEmail      = "override-user-email"
	InviteCodeRequiredFlag = "invite-code-required"
	InviteCodesFlag        = "invite-codes"
)

const userEmailCacheTTL = 5 * time.Minute

func RegisterFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   SupertokensHostFlag,
			Usage:  "supertokens host",
			Value:  "",
			EnvVar: "SUPERTOKENS_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   SupertokensPortFlag,
			Usage:  "supertokens port",
			EnvVar: "SUPERTOKENS_SERVICE_PORT",
		},
		cli.StringFlag{
			Name:   googleClientIDFlag,
			Usage:  "google oauth client id",
			EnvVar: "GOOGLE_CLIENT_ID",
		},
		cli.StringFlag{
			Name:   googleClientSecretFlag,
			Usage:  "google oauth client secret",
			EnvVar: "GOOGLE_CLIENT_SECRET",
		},
		cli.StringFlag{
			Name:   overrideUserEmail,
			Usage:  "override user email",
			EnvVar: "OVERRIDE_USER_EMAIL",
		},
		cli.BoolFlag{
			Name:   InviteCodeRequiredFlag,
			Usage:  "require invite code for signup",
			EnvVar: "INVITE_CODE_REQUIRED",
		},
		cli.StringFlag{
			Name:   InviteCodesFlag,
			Usage:  "comma-separated allowed invite codes",
			EnvVar: "INVITE_CODES",
		},
	)
}

type cachedUserEmail struct {
	Email     string
	ExpiresAt time.Time
}

type Auth struct {
	url                string
	smtpUser           string
	smtpPass           string
	smtpSecure         bool
	smtpHost           string
	smtpPort           int
	domain             string
	cl                 *http.Client
	pg                 *cs.PG
	googleClientID     string
	googleClientSecret string
	hasSupetokens      bool
	overrideUserEmail  string
	inviteCodeRequired bool
	inviteCodes        []string
	userEmailCache     sync.Map // userID → email, avoids repeated SuperTokens round-trips
}

func parseInviteCodes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var codes []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			codes = append(codes, trimmed)
		}
	}
	return codes
}

func New(c *cli.Context, cl *http.Client, pg *cs.PG) *Auth {
	return &Auth{
		url:                c.String(SupertokensHostFlag) + ":" + c.String(SupertokensPortFlag),
		hasSupetokens:      c.String(SupertokensHostFlag) != "" && c.String(SupertokensPortFlag) != "",
		smtpUser:           c.String(sv.SMTPUserFlag),
		smtpPass:           c.String(sv.SMTPPassFlag),
		smtpHost:           c.String(sv.SMTPHostFlag),
		smtpSecure:         c.BoolT(sv.SMTPSecureFlag),
		smtpPort:           c.Int(sv.SMTPPortFlag),
		domain:             c.String(sv.DomainFlag),
		cl:                 cl,
		pg:                 pg,
		googleClientID:     c.String(googleClientIDFlag),
		googleClientSecret: c.String(googleClientSecretFlag),
		overrideUserEmail:  c.String(overrideUserEmail),
		inviteCodeRequired: c.Bool(InviteCodeRequiredFlag),
		inviteCodes:        parseInviteCodes(c.String(InviteCodesFlag)),
	}
}

func (s *Auth) IsInviteCodeRequired() bool {
	return s.inviteCodeRequired
}

func (s *Auth) IsInviteCodeValid(code string) bool {
	for _, c := range s.inviteCodes {
		if c == code {
			return true
		}
	}
	return false
}

func (s *Auth) IsNewUser(ctx context.Context, email string) (bool, error) {
	db := s.pg.Get()
	if db == nil {
		return false, fmt.Errorf("db is nil")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var user models.User
	err := db.Model(&user).
		Context(ctx).
		Where("email = ?", email).
		Limit(1).
		Select()
	if err == nil {
		return false, nil // user exists
	}
	if defaultErrors.Is(err, pg.ErrNoRows) {
		return true, nil // user does not exist
	}
	return false, err // db error
}

func (s *Auth) Init() error {
	if !s.hasSupetokens {
		return nil
	}
	smtpSettings := emaildelivery.SMTPSettings{
		Host: s.smtpHost,
		From: emaildelivery.SMTPFrom{
			Name:  "Octor",
			Email: s.smtpUser,
		},
		Username: &s.smtpUser,
		Port:     s.smtpPort,
		Password: s.smtpPass,
		Secure:   s.smtpSecure,
		TLSConfig: &tls.Config{
			ServerName: s.smtpHost,
		},
	}
	apiBasePath := "/auth"
	websiteBasePath := "/auth"
	return supertokens.Init(supertokens.TypeInput{
		// Debug: true,
		Supertokens: &supertokens.ConnectionInfo{
			// https://try.supertokens.com is for demo purposes. Replace this with the address of your core instance (sign up on supertokens.com), or self host a core.
			ConnectionURI: s.url,
			// APIKey: <API_KEY(if configured)>,
		},
		AppInfo: supertokens.AppInfo{
			AppName:         "octor",
			APIDomain:       s.domain,
			WebsiteDomain:   s.domain,
			APIBasePath:     &apiBasePath,
			WebsiteBasePath: &websiteBasePath,
		},
		RecipeList: []supertokens.Recipe{
			passwordless.Init(plessmodels.TypeInput{
				FlowType: "MAGIC_LINK",
				ContactMethodEmail: plessmodels.ContactMethodEmailConfig{
					Enabled: true,
				},
				EmailDelivery: &emaildelivery.TypeInput{
					Service: passwordless.MakeSMTPService(emaildelivery.SMTPServiceConfig{
						Settings: smtpSettings,
						Override: func(originalImplementation emaildelivery.SMTPInterface) emaildelivery.SMTPInterface {
							*originalImplementation.GetContent = func(input emaildelivery.EmailType, userContext supertokens.UserContext) (emaildelivery.EmailContent, error) {

								email := input.PasswordlessLogin.Email

								// magic link
								urlWithLinkCode := *input.PasswordlessLogin.UrlWithLinkCode
								body := fmt.Sprintf("<a href=\"%v\">Login to your account!</a>", urlWithLinkCode)

								// send some custom email content
								return emaildelivery.EmailContent{
									Body:    body,
									IsHtml:  true,
									Subject: "Login to your account!",
									ToEmail: email,
								}, nil

							}

							return originalImplementation
						},
					}),
				},
			}),
			thirdparty.Init(&tpmodels.TypeInput{
				SignInAndUpFeature: tpmodels.TypeInputSignInAndUp{
					Providers: []tpmodels.ProviderInput{
						{
							Config: tpmodels.ProviderConfig{
								ThirdPartyId: "google",
								Clients: []tpmodels.ProviderClientConfig{
									{
										ClientID:     s.googleClientID,
										ClientSecret: s.googleClientSecret,
									},
								},
							},
						},
					},
				},
			}),
			session.Init(nil), // initializes session features
			dashboard.Init(nil),
			usermetadata.Init(nil),
			userroles.Init(nil),
		},
	})
}

type User struct {
	ID                      uuid.UUID
	Email                   string
	Expired                 bool
	IsNew                   bool
	Tier                    string
	Skin                    string
	GridDensity             string
	VaultAutoDeleteUnseeded bool
}

func (s *User) HasAuth() bool {
	return s.ID != uuid.Nil
}

func makeUserFromContext(c *gin.Context) *User {
	u := &User{}
	uc := c.Request.Context().Value(UserContext{})
	su, ok := uc.(*models.User)
	if ok && su != nil {
		u.ID = su.UserID
		u.Email = su.Email
		u.Tier = su.Tier
		u.Skin = su.Skin
		u.GridDensity = su.GridDensity
		u.VaultAutoDeleteUnseeded = su.VaultAutoDeleteUnseeded
	}
	inc := c.Request.Context().Value(IsNewContext{})
	isNew, ok := inc.(bool)
	if ok {
		u.IsNew = isNew
	}
	return u
}

func GetUserFromContext(c *gin.Context) *User {
	if IsAdmin(c) {
		return makeUserFromContext(c)
	}
	if c.Query(sv.AccessTokenParamName) != "" {
		return makeUserFromContext(c)
	}
	if sessionContainer := session.GetSessionFromRequestContext(c.Request.Context()); sessionContainer != nil {
		return makeUserFromContext(c)
	}
	u := &User{}
	if err := c.Request.Context().Value(ErrorContext{}); err != nil {
		if defaultErrors.As(err.(error), &errors.TryRefreshTokenError{}) {
			u.Expired = true
		}
	}
	return u
}

type ErrorContext struct{}

type UserContext struct{}
type IsNewContext struct{}

func (s *Auth) myVerifySession(options *sessmodels.VerifySessionOptions, otherHandler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, err := session.GetSession(r, w, options)
		//err = errors.TryRefreshTokenError{}
		if err != nil {
			ctx := context.WithValue(r.Context(), ErrorContext{}, err)
			r := r.WithContext(ctx)
			if defaultErrors.As(err, &errors.TryRefreshTokenError{}) {
				if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
					otherHandler(w, r)
					return
				}
				// This means that the session exists, but the access token
				// has expired.

				// You can handle this in a custom way by sending a 401.
				// Or you can call the errorHandler middleware as shown below
			} else if defaultErrors.As(err, &errors.UnauthorizedError{}) {
				otherHandler(w, r)
				return
				// This means that the session does not exist anymore.

				// You can handle this in a custom way by sending a 401.
				// Or you can call the errorHandler middleware as shown below
			} else if defaultErrors.As(err, &errors.InvalidClaimError{}) {
				otherHandler(w, r)
				return
				// The user is missing some required claim.
				// You can pass the missing claims to the frontend and handle it there
			}

			// OR you can use this errorHandler which will
			// handle all of the above errors in the default way
			err = supertokens.ErrorHandler(err, r, w)
			if err != nil {
				log.WithError(err).Error("failed to handle error")
				w.WriteHeader(500)
			}
			return
		}
		if sess != nil {
			ctx := context.WithValue(r.Context(), sessmodels.SessionContext, sess)
			u, isNew, err := s.createUser(r.Context(), sess)
			if err != nil {
				log.WithError(err).Error("failed to create user")
				w.WriteHeader(500)
			} else {
				ctx = context.WithValue(ctx, UserContext{}, u)
				ctx = context.WithValue(ctx, IsNewContext{}, isNew)
			}

			otherHandler(w, r.WithContext(ctx))
		} else {
			otherHandler(w, r)
		}
	}
}

func (s *Auth) createUser(ctx context.Context, sess sessmodels.SessionContainer) (u *models.User, isNew bool, err error) {
	db := s.pg.Get()
	if db == nil {
		log.Error("createUser: db is nil")
		return
	}
	userID := sess.GetUserID()

	var email string
	if s.overrideUserEmail != "" {
		email = s.overrideUserEmail
	} else {
		if cached, ok := s.userEmailCache.Load(userID); ok {
			entry, ok := cached.(cachedUserEmail)
			if ok && time.Now().Before(entry.ExpiresAt) {
				email = entry.Email
			}
		}
		if email == "" {
			userInfo, plErr := passwordless.GetUserByID(userID)
			if plErr == nil && userInfo != nil && userInfo.Email != nil {
				email = *userInfo.Email
			}
		}
		if email == "" {
			tpUserInfo, tpErr := thirdparty.GetUserByID(userID)
			if tpErr == nil && tpUserInfo != nil && tpUserInfo.Email != "" {
				email = tpUserInfo.Email
			}
		}
	}

	if email != "" && s.inviteCodeRequired {
		isNewUser, checkErr := s.IsNewUser(ctx, email)
		if checkErr == nil && isNewUser {
			inviteCodeVal := ctx.Value("invite-code")
			inviteCodeStr, _ := inviteCodeVal.(string)

			if !s.IsInviteCodeValid(inviteCodeStr) {
				log.Warnf("createUser: registration blocked for email=%s, invalid/missing invite code: '%s'", email, inviteCodeStr)
				_ = supertokens.DeleteUser(userID)
				return nil, false, fmt.Errorf("invalid invite code")
			}
		}
	}

	if s.overrideUserEmail != "" {
		return models.GetOrCreateUser(ctx, db, s.overrideUserEmail)
	}

	// Fast path: in-process cache avoids ~1s SuperTokens network call on every request.
	// Keep it time-bound so email changes in SuperTokens eventually converge.
	if cached, ok := s.userEmailCache.Load(userID); ok {
		entry, ok := cached.(cachedUserEmail)
		if ok && time.Now().Before(entry.ExpiresAt) {
			return models.GetOrCreateUser(ctx, db, entry.Email)
		}
		s.userEmailCache.Delete(userID)
	}

	// Try passwordless recipe first
	userInfo, plErr := passwordless.GetUserByID(userID)
	if plErr == nil && userInfo != nil && userInfo.Email != nil {
		log.Infof("createUser: found passwordless user email=%s", *userInfo.Email)
		s.userEmailCache.Store(userID, cachedUserEmail{Email: *userInfo.Email, ExpiresAt: time.Now().Add(userEmailCacheTTL)})
		return models.GetOrCreateUser(ctx, db, *userInfo.Email)
	} else if plErr != nil {
		log.Infof("createUser: passwordless GetUserByID error: %v", plErr)
	}

	// Try third-party recipe
	tpUserInfo, tpErr := thirdparty.GetUserByID(userID)
	if tpErr == nil && tpUserInfo != nil && tpUserInfo.Email != "" {
		log.Infof("createUser: found thirdparty user email=%s", tpUserInfo.Email)
		s.userEmailCache.Store(userID, cachedUserEmail{Email: tpUserInfo.Email, ExpiresAt: time.Now().Add(userEmailCacheTTL)})
		return models.GetOrCreateUser(ctx, db, tpUserInfo.Email)
	} else if tpErr != nil {
		log.Errorf("createUser: thirdparty GetUserByID error: %v", tpErr)
	}

	log.Warnf("createUser: failed to identify userID=%s", userID)
	return
}

func (s *Auth) verifySession(options *sessmodels.VerifySessionOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.Host, "localhost:") || strings.HasPrefix(c.Request.Host, "127.0.0.1:") || c.Request.Host == "localhost" || c.Request.Host == "127.0.0.1" {
			s.registerAdminUser(c)
			c.Next()
			return
		}
		s.myVerifySession(options, func(rw http.ResponseWriter, r *http.Request) {
			c.Request = c.Request.WithContext(r.Context())
			c.Next()
		})(c.Writer, c.Request)
		// we call Abort so that the next handler in the chain is not called, unless we call Next explicitly
		c.Abort()
	}
}

func (s *Auth) RegisterHandler(r *gin.Engine) {
	if !s.hasSupetokens {
		r.Use(func(c *gin.Context) {
			s.registerAdminUser(c)

			c.Next()
		})
		return
	}
	// CORS
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "OPTIONS"},
		AllowHeaders:     append([]string{"content-type"}, supertokens.GetAllCORSHeaders()...),
		MaxAge:           1 * time.Minute,
		AllowCredentials: true,
	}))

	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.Host, "localhost:") || strings.HasPrefix(c.Request.Host, "127.0.0.1:") || c.Request.Host == "localhost" || c.Request.Host == "127.0.0.1" {
			s.registerAdminUser(c)
			c.Next()
			return
		}

		// Inject invite-code from session into request Context so createUser can access it
		session := sessions.Default(c)
		inviteCodeVal := session.Get("invite-code")
		if inviteCodeVal != nil {
			if inviteCodeStr, ok := inviteCodeVal.(string); ok && inviteCodeStr != "" {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), "invite-code", inviteCodeStr))
			}
		}

		supertokens.Middleware(http.HandlerFunc(
			func(rw http.ResponseWriter, r *http.Request) {
				c.Request = c.Request.WithContext(r.Context())
				c.Next()
			})).ServeHTTP(c.Writer, c.Request)
		// we call Abort so that the next handler in the chain is not called, unless we call Next explicitly
		c.Abort()
	})
	sessionRequired := false
	r.Use(s.verifySession(&sessmodels.VerifySessionOptions{
		SessionRequired: &sessionRequired,
	}))
}

type IsAdminContext struct{}

func (s *Auth) registerAdminUser(c *gin.Context) {
	db := s.pg.Get()
	if db == nil {
		return
	}
	u, isNew, err := models.GetOrCreateUser(c.Request.Context(), db, "admin")
	if err != nil {
		log.WithError(err).Error("failed to create admin user")
		return
	}
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, UserContext{}, u)
	ctx = context.WithValue(ctx, IsNewContext{}, isNew)
	ctx = context.WithValue(ctx, IsAdminContext{}, true)
	c.Request = c.Request.WithContext(ctx)
}

func IsAdmin(c *gin.Context) bool {
	v := c.Request.Context().Value(IsAdminContext{})
	isAdmin, ok := v.(bool)
	if !ok {
		return false
	}
	return isAdmin
}

func HasAuth(c *gin.Context) {
	u := GetUserFromContext(c)
	if !u.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Next()
}
