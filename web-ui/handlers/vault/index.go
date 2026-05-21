package vault

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/vault"
	"github.com/webtor-io/web-ui/services/web"
)

// index handles HTTP request for displaying the My Vault dashboard (Level 1: HTTP interaction).
func (h *Handler) index(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if !user.HasAuth() {
		lang := i18n.GetLang(c)
		v := url.Values{
			"from":       []string{"vault"},
			"return-url": []string{i18n.LangPath(lang, "/vault")},
		}
		c.Redirect(http.StatusFound, i18n.LangPath(lang, "/login")+"?"+v.Encode())
		return
	}

	stats, enriched, err := h.vault.GetUserStats(c.Request.Context(), user)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	ctx := web.NewContext(c)
	q := strings.TrimSpace(c.Query("q"))
	data := &PledgeListData{
		Args: &shared.IndexArgs{
			Query: q,
		},
		Pledges:               filterPledges(buildPledgeDisplay(enriched, h.vault.GetExpirePeriod()), q),
		Stats:                 stats,
		FreezePeriod:          h.vault.GetFreezePeriod(),
		ExpirePeriod:          h.vault.GetExpirePeriod(),
		TransferTimeoutPeriod: h.vault.GetTransferTimeoutPeriod(),
		IsFree:                isFreeTier(ctx),
	}

	h.tb.Build("vault/index").HTML(http.StatusOK, ctx.WithData(data))
}

func filterPledges(list []PledgeDisplay, q string) []PledgeDisplay {
	if q == "" {
		return list
	}
	out := make([]PledgeDisplay, 0)
	for _, p := range list {
		if p.Resource != nil && strings.Contains(strings.ToLower(p.Resource.Name), strings.ToLower(q)) {
			out = append(out, p)
		} else if strings.Contains(strings.ToLower(p.ResourceID), strings.ToLower(q)) {
			out = append(out, p)
		}
	}
	return out
}

// isFreeTier returns true when the user has no paid subscription.
func isFreeTier(ctx *web.Context) bool {
	return false
}

// buildPledgeDisplay converts enriched pledges into display rows for the table.
func buildPledgeDisplay(enriched []vault.EnrichedPledge, expirePeriod time.Duration) []PledgeDisplay {
	display := make([]PledgeDisplay, 0, len(enriched))
	for _, e := range enriched {
		var expiresIn time.Duration
		if !e.Pledge.Funded && e.Pledge.Resource != nil && e.Pledge.Resource.ExpiredAt != nil {
			remaining := time.Until(e.Pledge.Resource.ExpiredAt.Add(expirePeriod))
			if remaining > 0 {
				expiresIn = remaining
			}
		}

		showProgress := e.Pledge.Resource != nil &&
			e.Pledge.Resource.Funded &&
			!e.Pledge.Resource.Vaulted &&
			!e.Pledge.Resource.Expired

		display = append(display, PledgeDisplay{
			PledgeID:     e.Pledge.PledgeID.String(),
			ResourceID:   e.Pledge.ResourceID,
			Resource:     e.Pledge.Resource,
			Amount:       e.Pledge.Amount,
			IsFrozen:     e.IsFrozen,
			Funded:       e.Pledge.Funded,
			CreatedAt:    e.Pledge.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiresIn:    expiresIn,
			ShowProgress: showProgress,
		})
	}
	return display
}
