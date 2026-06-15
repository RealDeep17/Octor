package library

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (h *Handler) layoutMultiple(c *gin.Context) {
	user := auth.GetUserFromContext(c)
	if user == nil || !user.HasAuth() {
		c.Status(http.StatusUnauthorized)
		return
	}

	var req struct {
		VideoIDs   []string `form:"video_ids[]"`
		VideoTypes []string `form:"video_types[]"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	ctx := c.Request.Context()

	var cleanIDs []string
	var cleanTypes []string
	for i, vid := range req.VideoIDs {
		if i < len(req.VideoTypes) && vid != "" && req.VideoTypes[i] != "" {
			cleanIDs = append(cleanIDs, vid)
			cleanTypes = append(cleanTypes, req.VideoTypes[i])
		}
	}

	if len(cleanIDs) > 0 {
		statusMap, err := h.uvs.FilterUserStatus(ctx, user.ID, cleanIDs)
		if err != nil {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		for i, vid := range cleanIDs {
			vtype := cleanTypes[i]
			currentLayout := "vertical"
			if st, ok := statusMap[vid]; ok && st.Layout != "" {
				currentLayout = st.Layout
			}

			newLayout := "horizontal"
			if currentLayout == "horizontal" {
				newLayout = "vertical"
			}

			if vtype == "movie" || vtype == "adult" {
				_ = h.uvs.SetMoviePosterLayout(ctx, user.ID, vid, newLayout)
			} else if vtype == "series" {
				_ = h.uvs.SetSeriesPosterLayout(ctx, user.ID, vid, newLayout)
			}
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
}
