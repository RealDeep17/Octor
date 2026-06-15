package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

func (h *Handler) toggleMultiple(c *gin.Context) {
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
			isWatched := false
			if st, ok := statusMap[vid]; ok {
				isWatched = st.Watched
			}

			if isWatched {
				if vtype == "movie" || vtype == "adult" {
					_ = h.uvs.UnmarkMovie(ctx, user.ID, vid)
				} else if vtype == "series" {
					_ = h.uvs.UnmarkSeries(ctx, user.ID, vid)
				}
			} else {
				if vtype == "movie" || vtype == "adult" {
					_ = h.uvs.MarkMovieWatched(ctx, user.ID, vid, models.UserVideoSourceManual)
				} else if vtype == "series" {
					_ = h.uvs.MarkSeriesWatched(ctx, user.ID, vid, models.UserVideoSourceManual)
				}
			}
		}
	}

	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
}
