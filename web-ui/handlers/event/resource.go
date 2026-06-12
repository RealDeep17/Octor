package event

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

type resourceVaultedMsg struct {
	ResourceID string `json:"resource_id"`
}

func (h *Handler) resourceVaulted(msg []byte) error {
	var m resourceVaultedMsg
	if err := json.Unmarshal(msg, &m); err != nil {
		return err
	}
	if m.ResourceID == "" {
		return nil
	}

	// Use a bounded timeout so a stalled DB/email server cannot block
	// the JetStream consumer goroutine indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := h.pg.Get()
	if err := vaultModels.UpdateResourceVaulted(ctx, db, m.ResourceID); err != nil {
		return err
	}
	log.WithField("resource_id", m.ResourceID).Info("resource vaulted status updated successfully")

	// Notify users
	r, err := vaultModels.GetResource(ctx, db, m.ResourceID)
	if err != nil {
		return err
	}
	if r == nil {
		log.WithField("resource_id", m.ResourceID).Warn("resource not found for notification")
		return nil
	}

	pledges, err := vaultModels.GetResourcePledges(ctx, db, m.ResourceID)
	if err != nil {
		return err
	}

	userIds := make(map[string]struct{})
	for _, p := range pledges {
		userIds[p.UserID.String()] = struct{}{}
	}

	// Notify each pledger independently — a failure for one user must not
	// prevent notifications for all remaining users.
	for idStr := range userIds {
		u := &models.User{}
		if err := db.Model(u).
			Context(ctx).
			Where("user_id = ?", idStr).
			Select(); err != nil {
			log.WithError(err).WithField("user_id", idStr).Warn("failed to fetch user for vaulted notification")
			continue
		}
		if u.Email != "" {
			if err := h.ns.SendVaulted(u.Email, r); err != nil {
				log.WithError(err).WithField("email", u.Email).Warn("failed to send vaulted email notification")
				// do not continue — still send the SSE notification below
			}
		}
		if h.nats != nil && h.nats.Get() != nil {
			_ = h.nats.Get().Publish(fmt.Sprintf("user.%s.update", idStr), []byte(`{"type": "vault"}`))
		}
	}

	log.WithField("resource_id", m.ResourceID).Info("resource vaulted successfully")

	return nil
}
