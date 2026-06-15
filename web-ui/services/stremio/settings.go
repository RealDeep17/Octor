package stremio

import (
	"context"

	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
)

type StremioDynamicSettingsKey struct{}

// GetUserStremioSettings returns Stremio settings for a specific user
func GetUserSettingsDataByClaims(ctx context.Context, db *pg.DB, userID uuid.UUID) (*models.StremioSettingsData, error) {
	s, err := models.GetUserStremioSettingsData(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	if dyn, ok := ctx.Value(StremioDynamicSettingsKey{}).(*models.StremioSettingsData); ok && dyn != nil {
		if dyn.PreferredLanguage != "" {
			s.PreferredLanguage = dyn.PreferredLanguage
		}
		s.DiscoverOnly = dyn.DiscoverOnly
		if len(dyn.PreferredResolutions) > 0 {
			s.PreferredResolutions = dyn.PreferredResolutions
		}
	}
	return s, nil
}
