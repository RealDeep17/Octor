package stremio

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	"github.com/webtor-io/web-ui/models"
	at "github.com/webtor-io/web-ui/services/access_token"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/claims"
	sv "github.com/webtor-io/web-ui/services/common"
	lr "github.com/webtor-io/web-ui/services/link_resolver"
	"github.com/webtor-io/web-ui/services/stremio"
	"github.com/webtor-io/web-ui/services/web"
)

type Handler struct {
	at     *at.AccessToken
	b      *stremio.Builder
	pg     *cs.PG
	lr     *lr.LinkResolver
	secret string
}

func RegisterHandler(c *cli.Context, r *gin.Engine, at *at.AccessToken, b *stremio.Builder, pg *cs.PG, lr *lr.LinkResolver) {
	h := &Handler{
		at:     at,
		b:      b,
		pg:     pg,
		lr:     lr,
		secret: c.String(sv.SessionSecretFlag),
	}

	gr := r.Group("/stremio")
	gr.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST"},
	}))
	gr.GET("/manifest.json", h.manifest)
	// Public configure endpoint to satisfy stremio-addons directory requirements
	gr.GET("/configure", h.configure)
	gra := gr.Group("")
	gra.Use(auth.HasAuth)
	gra.POST("/url/generate", h.generateUrl)
	grapi := gra.Group("")
	grapi.Use(at.HasScope("stremio:read"))
	grapi.GET("/catalog/:type/*id", h.catalog)
	grapi.GET("/stream/:type/*id", h.stream)
	grapi.GET("/meta/:type/*id", h.meta)
	grapi.GET("/resolve/*data", h.resolve)

	// Route group for parametric configuration (e.g. /stremio/token=abc,resolution=1080p/...)
	grc := gr.Group("/:config")
	grc.Use(h.withConfig)
	grc.GET("/manifest.json", h.manifest)
	grc.GET("/catalog/:type/*id", h.catalog)
	grc.GET("/stream/:type/*id", h.stream)
	grc.GET("/meta/:type/*id", h.meta)
}

func (s *Handler) generateUrl(c *gin.Context) {
	_, err := s.at.Generate(c, "stremio", []string{"stremio:read"})
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to generate stremio addon url"))
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.addonUrlGenerated")
}

func (s *Handler) manifest(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	hasToken := c.Query(sv.AccessTokenParamName) != ""
	mas, err := s.b.BuildManifestService(user, hasToken)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build manifest service"))
		return
	}
	resp, err := mas.GetManifest(c.Request.Context())
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get manifest response"))
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) catalog(c *gin.Context) {
	ct := c.Param("type")
	user := auth.GetUserFromContext(c)
	cas, err := s.b.BuildCatalogService(user)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build catalog service"))
		return
	}
	resp, err := cas.GetCatalog(c.Request.Context(), ct)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get catalog response"))
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) meta(c *gin.Context) {
	ct := c.Param("type")
	id := s.cleanResourceID(c.Param("id"))
	user := auth.GetUserFromContext(c)
	mes, err := s.b.BuildMetaService(user)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build meta service"))
		return
	}
	resp, err := mes.GetMeta(c.Request.Context(), ct, id)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get meta response"))
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) stream(c *gin.Context) {
	ct := c.Param("type")
	id := s.cleanResourceID(c.Param("id"))
	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)
	cla := claims.GetFromContext(c)
	sts, err := s.b.BuildStreamsService(c.Request.Context(), user, s.lr, apiClaims, cla, c.Query(sv.AccessTokenParamName))
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to build streams service"))
		return
	}
	resp, err := sts.GetStreams(c.Request.Context(), ct, id)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get streams response"))
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (s *Handler) cleanResourceID(rawID string) string {
	return strings.TrimPrefix(strings.TrimSuffix(rawID, ".json"), "/")
}

func (s *Handler) configure(c *gin.Context) {
	u := auth.GetUserFromContext(c)
	if !u.HasAuth() {
		v := url.Values{
			"from":       []string{"stremio-configure"},
			"return-url": []string{"/stremio/configure"},
		}
		c.Redirect(http.StatusFound, "/login?"+v.Encode())
		return
	}
	// For now, redirect authenticated users to their profile where the personalized addon URL and install link are shown
	c.Redirect(http.StatusFound, "/profile")
}

func (s *Handler) resolve(c *gin.Context) {
	// Step 1: Extract JWT token from URL path
	data := strings.TrimPrefix(c.Param("data"), "/")
	if data == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Step 2: Parse and validate JWT token
	token, err := jwt.Parse(data, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.secret), nil
	})

	if err != nil {
		log.WithError(err).Warn("failed to parse JWT token")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Step 3: Extract claims
	jwtClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		log.Warn("invalid JWT token claims")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Step 4: Extract claims. JWT shape: {hash, idx, exp}. Path resolution
	// (when needed by user backends) and resource registration are handled
	// inside LinkResolver / Octor backend.
	hash, ok := jwtClaims["hash"].(string)
	if !ok || hash == "" {
		log.Warn("missing or invalid hash in JWT claims")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	fileIdxF, _ := jwtClaims["idx"].(float64)
	fileIdx := int(fileIdxF)

	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)
	userClaims := claims.GetFromContext(c)

	// Step 5: Resolve link using LinkResolver
	linkResult, err := s.lr.ResolveLink(c.Request.Context(), user.ID, apiClaims, userClaims, hash, fileIdx, true)
	if err != nil {
		log.WithError(err).
			WithField("hash", hash).
			WithField("file_idx", fileIdx).
			Error("failed to resolve link")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Step 7: Check if URL was generated
	if linkResult == nil || linkResult.URL == "" {
		log.WithField("hash", hash).
			WithField("file_idx", fileIdx).
			Warn("no URL generated for resolve")
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Step 8: Redirect to destination URL
	c.Redirect(http.StatusFound, linkResult.URL)
}

func parseConfigSegment(segment string) map[string]string {
	config := make(map[string]string)
	if segment == "" {
		return config
	}
	unescaped, err := url.QueryUnescape(segment)
	if err == nil {
		segment = unescaped
	}
	// Split by comma or semicolon
	parts := strings.Split(segment, ",")
	if len(parts) == 1 && strings.Contains(segment, ";") {
		parts = strings.Split(segment, ";")
	}
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			config[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return config
}

func buildDynamicSettings(config map[string]string) *models.StremioSettingsData {
	dyn := &models.StremioSettingsData{}

	// 1. Resolution
	if res, ok := config["resolution"]; ok && res != "" && res != "All" {
		dyn.PreferredResolutions = []models.ResolutionSetting{
			{Resolution: res, Enabled: true},
		}
	}

	// 2. Language
	if lang, ok := config["language"]; ok && lang != "" {
		dyn.PreferredLanguage = lang
	}

	// 3. DiscoverOnly
	if do, ok := config["discoverOnly"]; ok {
		dyn.DiscoverOnly = (do == "true")
	}

	return dyn
}

func (s *Handler) withConfig(c *gin.Context) {
	configSeg := c.Param("config")
	configMap := parseConfigSegment(configSeg)

	tokenStr := configMap["token"]
	isManifest := strings.HasSuffix(c.Request.URL.Path, "/manifest.json")

	if !isManifest {
		if tokenStr == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		atRecord, err := s.at.GetToken(c.Request.Context(), tokenStr)
		if err != nil || atRecord == nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		// Verify scope
		hasScope := false
		for _, sc := range atRecord.Scope {
			if sc == "stremio:read" {
				hasScope = true
				break
			}
		}
		if !hasScope {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// Set token in query parameters
		q := c.Request.URL.Query()
		q.Set(sv.AccessTokenParamName, tokenStr)
		c.Request.URL.RawQuery = q.Encode()

		// Inject authenticated user and scope into context
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, atRecord.User))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), at.TokenScope{}, atRecord.Scope))
	} else {
		// For manifest, if token is valid, inject it
		if tokenStr != "" {
			atRecord, err := s.at.GetToken(c.Request.Context(), tokenStr)
			if err == nil && atRecord != nil {
				// Set token in query parameters
				q := c.Request.URL.Query()
				q.Set(sv.AccessTokenParamName, tokenStr)
				c.Request.URL.RawQuery = q.Encode()

				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), auth.UserContext{}, atRecord.User))
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), at.TokenScope{}, atRecord.Scope))
			}
		}
	}

	// Build and inject dynamic settings
	dyn := buildDynamicSettings(configMap)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), stremio.StremioDynamicSettingsKey{}, dyn))

	c.Next()
}

