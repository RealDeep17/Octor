package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

type ManagementUser struct {
	ID            string
	Email         string
	Tier          string
	CreatedAt     time.Time
	VaultedCount  int
	VaultingCount int
	LibraryCount  int
}

type ManagementData struct {
	Users []ManagementUser
}

func (h *Handler) management(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// Fetch all users
	var users []models.User
	if err := db.Model(&users).Context(ctx).Order("email ASC").Select(); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to load users"))
		return
	}

	// Fetch library counts per user
	type LibStat struct {
		UserID uuid.UUID `pg:"user_id"`
		Count  int       `pg:"count"`
	}
	var libStats []LibStat
	_, _ = db.QueryContext(ctx, &libStats, `
		SELECT user_id, COUNT(*) as count FROM library GROUP BY user_id
	`)
	libMap := make(map[uuid.UUID]int)
	for _, s := range libStats {
		libMap[s.UserID] = s.Count
	}

	// Fetch vault counts per user
	type VaultStat struct {
		UserID        uuid.UUID `pg:"user_id"`
		VaultedCount  int       `pg:"vaulted_count"`
		VaultingCount int       `pg:"vaulting_count"`
	}
	var vaultStats []VaultStat
	_, _ = db.QueryContext(ctx, &vaultStats, `
		SELECT 
			p.user_id,
			COUNT(CASE WHEN r.vaulted = true AND r.expired = false THEN 1 END) as vaulted_count,
			COUNT(CASE WHEN r.vaulted = false AND r.expired = false THEN 1 END) as vaulting_count
		FROM vault.pledge p
		JOIN vault.resource r ON p.resource_id = r.resource_id
		GROUP BY p.user_id
	`)
	vaultMap := make(map[uuid.UUID]VaultStat)
	for _, s := range vaultStats {
		vaultMap[s.UserID] = s
	}

	var mUsers []ManagementUser
	for _, u := range users {
		id := u.UserID.String()
		tier := u.Tier
		if tier == "paid" {
			tier = "Paid"
		} else if tier == "free" || tier == "" {
			tier = "Free"
		} else if len(tier) > 0 {
			tier = strings.ToUpper(tier[:1]) + strings.ToLower(tier[1:])
		}

		libCount := libMap[u.UserID]
		vStats := vaultMap[u.UserID]

		mUsers = append(mUsers, ManagementUser{
			ID:            id,
			Email:         u.Email,
			Tier:          tier,
			CreatedAt:     u.CreatedAt,
			VaultedCount:  vStats.VaultedCount,
			VaultingCount: vStats.VaultingCount,
			LibraryCount:  libCount,
		})
	}

	h.tb.Build("admin/management").HTML(http.StatusOK, web.NewContext(c).WithData(&ManagementData{
		Users: mUsers,
	}))
}

func (h *Handler) deleteUser(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	rawUserID := c.PostForm("user_id")
	if rawUserID == "" {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("missing user_id"))
		return
	}
	userID, err := uuid.FromString(rawUserID)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid user_id"))
		return
	}

	currentUser := auth.GetUserFromContext(c)
	if currentUser != nil && currentUser.ID == userID {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("cannot delete your own active profile"))
		return
	}

	// Deleting the user from the "user" table will automatically clear up all database entries across
	// all 16 related tables due to ON DELETE CASCADE foreign key constraints.
	_, err = db.Model((*models.User)(nil)).
		Context(ctx).
		Where("user_id = ?", userID).
		Delete()
	if err != nil {
		log.WithError(err).WithField("user_id", rawUserID).Error("failed to delete user")
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to delete user"))
		return
	}

	web.RedirectWithSuccessAndMessage(c, "toast.userDeleted")
}

func (h *Handler) migrateUser(c *gin.Context) {
	db, err := h.db()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	fromUserIDRaw := c.PostForm("from_user_id")
	toUserIDRaw := c.PostForm("to_user_id")

	if fromUserIDRaw == "" || toUserIDRaw == "" {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("missing from_user_id or to_user_id"))
		return
	}

	if fromUserIDRaw == toUserIDRaw {
		_ = c.AbortWithError(http.StatusBadRequest, errors.New("source and target user must be different"))
		return
	}

	fromUserID, err := uuid.FromString(fromUserIDRaw)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid from_user_id"))
		return
	}

	toUserID, err := uuid.FromString(toUserIDRaw)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "invalid to_user_id"))
		return
	}

	// Execute migration inside a transaction
	err = db.RunInTransaction(ctx, func(tx *pg.Tx) error {
		// Sort the participating user UUIDs deterministically to prevent deadlocks
		firstUserID, secondUserID := fromUserID, toUserID
		if strings.Compare(fromUserID.String(), toUserID.String()) > 0 {
			firstUserID, secondUserID = toUserID, fromUserID
		}

		// Lock participating user rows at the start of the transaction
		_, err = tx.ExecContext(ctx, `
			SELECT 1 FROM "user" WHERE user_id IN (?, ?) FOR UPDATE
		`, firstUserID, secondUserID)
		if err != nil {
			return errors.Wrap(err, "failed to lock participating users")
		}

		// 1. Library
		_, err = tx.ExecContext(ctx, `
			INSERT INTO library (user_id, resource_id, created_at, name)
			SELECT ?, resource_id, created_at, name FROM library WHERE user_id = ?
			ON CONFLICT (user_id, resource_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate library entries")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM library WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to clear migrated library entries")
		}

		// 2. Vault Pledges (merge duplicate resource pledges and transfer non-duplicates)
		_, err = tx.ExecContext(ctx, `
			UPDATE vault.pledge p
			SET amount = p.amount + f.amount, updated_at = NOW()
			FROM vault.pledge f
			WHERE p.resource_id = f.resource_id AND p.user_id = ? AND f.user_id = ?
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to merge duplicate pledges")
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE vault.pledge
			SET user_id = ?, updated_at = NOW()
			WHERE user_id = ? AND resource_id NOT IN (
				SELECT resource_id FROM vault.pledge WHERE user_id = ?
			)
		`, toUserID, fromUserID, toUserID)
		if err != nil {
			return errors.Wrap(err, "failed to transfer non-duplicate pledges")
		}

		_, err = tx.ExecContext(ctx, `DELETE FROM vault.pledge WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to clean up remaining source pledges")
		}

		// 3. Vault Transaction Logs
		_, err = tx.ExecContext(ctx, `
			UPDATE vault.tx_log SET user_id = ?, updated_at = NOW() WHERE user_id = ?
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate transaction logs")
		}

		// 4. Vault Points (user_vp)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO vault.user_vp (user_id, total, created_at, updated_at)
			VALUES (?, 0, NOW(), NOW())
			ON CONFLICT (user_id) DO NOTHING
		`, toUserID)
		if err != nil {
			return errors.Wrap(err, "failed to initialize target user_vp")
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE vault.user_vp t
			SET total = COALESCE(t.total, 0) + COALESCE(s.total, 0), updated_at = NOW()
			FROM vault.user_vp s
			WHERE t.user_id = ? AND s.user_id = ?
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to merge user_vp balances")
		}

		_, err = tx.ExecContext(ctx, `DELETE FROM vault.user_vp WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source user_vp")
		}

		// 5. Watch History
		_, err = tx.ExecContext(ctx, `
			INSERT INTO watch_history (user_id, resource_id, path, position, duration, watched, created_at, updated_at)
			SELECT ?, resource_id, path, position, duration, watched, created_at, updated_at
			FROM watch_history WHERE user_id = ?
			ON CONFLICT (user_id, resource_id, path) DO UPDATE
			SET position = EXCLUDED.position, duration = EXCLUDED.duration, watched = EXCLUDED.watched, updated_at = EXCLUDED.updated_at
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate watch history")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM watch_history WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source watch history")
		}

		// 6. Watchlists (Movie / Series)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO movie_watchlist (user_id, video_id, source, created_at)
			SELECT ?, video_id, source, created_at FROM movie_watchlist WHERE user_id = ?
			ON CONFLICT (user_id, video_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate movie watchlist")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM movie_watchlist WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source movie watchlist")
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO series_watchlist (user_id, video_id, source, created_at)
			SELECT ?, video_id, source, created_at FROM series_watchlist WHERE user_id = ?
			ON CONFLICT (user_id, video_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate series watchlist")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM series_watchlist WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source series watchlist")
		}

		// 7. Playback Statuses (Movie / Series / Episode)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO movie_status (user_id, video_id, watched, rating, source, watched_at, created_at, updated_at, poster_layout)
			SELECT ?, video_id, watched, rating, source, watched_at, created_at, updated_at, poster_layout FROM movie_status WHERE user_id = ?
			ON CONFLICT (user_id, video_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate movie status")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM movie_status WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source movie status")
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO series_status (user_id, video_id, watched, rating, source, watched_at, created_at, updated_at, poster_layout)
			SELECT ?, video_id, watched, rating, source, watched_at, created_at, updated_at, poster_layout FROM series_status WHERE user_id = ?
			ON CONFLICT (user_id, video_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate series status")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM series_status WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source series status")
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO episode_status (user_id, video_id, season, episode, watched, rating, source, watched_at, created_at, updated_at)
			SELECT ?, video_id, season, episode, watched, rating, source, watched_at, created_at, updated_at FROM episode_status WHERE user_id = ?
			ON CONFLICT (user_id, video_id, season, episode) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate episode status")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM episode_status WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source episode status")
		}

		// 8. User Subtitles
		_, err = tx.ExecContext(ctx, `
			UPDATE user_subtitle SET user_id = ?, updated_at = NOW() WHERE user_id = ?
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate user subtitles")
		}

		// 9. Stremio Settings & Addon URLs
		_, err = tx.ExecContext(ctx, `
			INSERT INTO stremio_settings (user_id, auto_feed, auto_gdrive_feed, stream_format, stream_type, subtitle_lang, audio_lang, skin, created_at, updated_at)
			SELECT ?, auto_feed, auto_gdrive_feed, stream_format, stream_type, subtitle_lang, audio_lang, skin, created_at, updated_at FROM stremio_settings WHERE user_id = ?
			ON CONFLICT (user_id) DO NOTHING
		`, toUserID, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to migrate stremio settings")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM stremio_settings WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to delete source stremio settings")
		}

		_, err = tx.ExecContext(ctx, `
			UPDATE stremio_addon_url
			SET user_id = ?, updated_at = NOW()
			WHERE user_id = ? AND url NOT IN (
				SELECT url FROM stremio_addon_url WHERE user_id = ?
			)
		`, toUserID, fromUserID, toUserID)
		if err != nil {
			return errors.Wrap(err, "failed to transfer non-duplicate stremio addon URLs")
		}

		_, err = tx.ExecContext(ctx, `DELETE FROM stremio_addon_url WHERE user_id = ?`, fromUserID)
		if err != nil {
			return errors.Wrap(err, "failed to clean up duplicate stremio addon URLs")
		}

		return nil
	})

	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"from_user_id": fromUserIDRaw,
			"to_user_id":   toUserIDRaw,
		}).Error("failed to migrate user data")
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to migrate user data"))
		return
	}

	web.RedirectWithSuccessAndMessage(c, "toast.userMigrated")
}
