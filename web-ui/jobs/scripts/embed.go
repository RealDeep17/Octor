package scripts

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net/http"
	"time"

	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/embed"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/job"
	"github.com/webtor-io/web-ui/services/template"
	"github.com/webtor-io/web-ui/services/web"
)

type EmbedScript struct {
	api             *api.Api
	i18n            *i18n.Service
	settings        *models.EmbedSettings
	resourceID      string
	file            string
	tb              template.Builder[*web.Context]
	c               *web.Context
	cl              *http.Client
	dsd             *embed.DomainSettingsData
	warmup          WarmupSettings
	grace           GraceSettings
	forceDirectPlay bool
	debug           string
}

type EmbedAdsData struct {
	DomainSettings *embed.DomainSettingsData
}

func NewEmbedScript(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, settings *models.EmbedSettings, resourceID string, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings, grace GraceSettings, forceDirectPlay bool, debug string) *EmbedScript {
	return &EmbedScript{
		c:               c,
		api:             api,
		i18n:            i18nSvc,
		settings:        settings,
		resourceID:      resourceID,
		file:            file,
		tb:              tb,
		cl:              cl,
		dsd:             dsd,
		warmup:          warmup,
		grace:           grace,
		forceDirectPlay: forceDirectPlay,
		debug:           debug,
	}
}

func (s *EmbedScript) Run(ctx context.Context, j *job.Job) (err error) {
	j.InProgress(i18n.TranslateWithLocalizer(s.i18n.Localizer(s.c.Lang), "job.retrievingData"))
	resCtx, resCancel := context.WithTimeout(ctx, 30*time.Second)
	defer resCancel()

	// Step 1: Find the item ID by path
	lr, err := s.api.ListResourceContent(resCtx, s.c.ApiClaims, s.resourceID, &api.ListResourceContentArgs{
		Path:   s.file,
		Limit:  1,
		Output: api.OutputList,
	})
	if err != nil {
		return err
	}
	if lr == nil || len(lr.Items) == 0 {
		return fmt.Errorf("file not found: %s", s.file)
	}
	itemID := lr.Items[0].ID

	j.Done()

	action := "stream"
	id := s.resourceID

	vsud := &models.VideoStreamUserData{}

	// Pass nil for user-subtitles: the embed flow intentionally omits the
	// My Subtitles tab (no account context on third-party sites) so the
	// script never needs the service.
	as, _ := Action(s.tb, s.api, s.i18n, nil, s.c, id, itemID, action, &s.settings.StreamSettings, s.dsd, vsud, s.warmup, s.grace, false, s.forceDirectPlay, s.debug)
	err = as.Run(ctx, j)
	if err != nil {
		return err
	}
	return
}

func Embed(tb template.Builder[*web.Context], cl *http.Client, c *web.Context, api *api.Api, i18nSvc *i18n.Service, settings *models.EmbedSettings, resourceID string, file string, dsd *embed.DomainSettingsData, warmup WarmupSettings, grace GraceSettings, forceDirectPlay bool, debug string) (r job.Runnable, hash string, err error) {
	geoHash := ""
	if c.Geo != nil {
		geoHash = c.Geo.Country
	}
	fdpKey := ""
	if forceDirectPlay {
		fdpKey = "fdp"
	}
	debugKey := ""
	if debug != "" {
		debugKey = "dbg-" + debug
	}
	hourKey := time.Now().UTC().Format("2006010215")
	hash = fmt.Sprintf("%x", sha1.Sum([]byte(geoHash+"/"+fmt.Sprintf("%+v", dsd)+"/"+c.ApiClaims.Role+"/"+fmt.Sprintf("%+v", settings)+"/"+resourceID+"/"+hourKey+"/"+c.Lang+"/"+fdpKey+"/"+debugKey)))
	r = NewEmbedScript(tb, cl, c, api, i18nSvc, settings, resourceID, file, dsd, warmup, grace, forceDirectPlay, debug)
	return
}
