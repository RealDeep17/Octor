package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	models "github.com/webtor-io/web-ui/models"
	vaultModels "github.com/webtor-io/web-ui/models/vault"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/claims"
	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/notification"
	"github.com/webtor-io/web-ui/services/vault"
)

// Interfaces for testability
type reaperVault interface {
	RemovePledge(ctx context.Context, pledge *vaultModels.Pledge) error
	RemoveResource(ctx context.Context, resourceID string) error
	GetVaultAPIResource(ctx context.Context, resourceID string) (*vault.Resource, error)
}

type reaperNotification interface {
	SendTransferTimeout(to string, r *vaultModels.Resource) error
	SendExpired(to string, r *vaultModels.Resource) error
}

type reaperStore interface {
	GetExpiredResources(ctx context.Context, expirePeriod time.Duration, abandonedExpirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error)
	GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error)
	GetGhostResources(ctx context.Context) ([]vaultModels.Resource, error)
	RemoveFromLibrary(ctx context.Context, userID uuid.UUID, resourceID string) error
}

// pgReaperStore wraps *pg.DB to implement reaperStore
type pgReaperStore struct {
	db *pg.DB
}

func (s *pgReaperStore) GetExpiredResources(ctx context.Context, expirePeriod time.Duration, abandonedExpirePeriod time.Duration, transferTimeoutPeriod time.Duration) ([]vaultModels.Resource, error) {
	return vaultModels.GetExpiredResources(ctx, s.db, expirePeriod, abandonedExpirePeriod, transferTimeoutPeriod)
}

func (s *pgReaperStore) GetResourcePledgesWithUsers(ctx context.Context, resourceID string) ([]vaultModels.Pledge, error) {
	return vaultModels.GetResourcePledgesWithUsers(ctx, s.db, resourceID)
}

func (s *pgReaperStore) GetGhostResources(ctx context.Context) ([]vaultModels.Resource, error) {
	return vaultModels.GetGhostResources(ctx, s.db)
}

func (s *pgReaperStore) RemoveFromLibrary(ctx context.Context, userID uuid.UUID, resourceID string) error {
	return models.RemoveFromLibrary(ctx, s.db, userID, resourceID)
}

type reaper struct {
	store                  reaperStore
	vault                  reaperVault
	notification           reaperNotification
	expirePeriod           time.Duration
	abandonedExpirePeriod  time.Duration
	transferTimeoutPeriod  time.Duration
	pg                     *cs.PG
	cpCl                   *claims.Client
	httpClient             *http.Client
}

func makeVaultCMD() cli.Command {
	vaultCMD := cli.Command{
		Name:    "vault",
		Aliases: []string{"v"},
		Usage:   "Vault management commands",
	}
	configureVault(&vaultCMD)
	return vaultCMD
}

func configureVault(c *cli.Command) {
	reapCmd := cli.Command{
		Name:    "reap",
		Aliases: []string{"r"},
		Usage:   "Removes expired vault resources and their pledges",
		Action:  reap,
	}
	configureVaultReap(&reapCmd)
	c.Subcommands = []cli.Command{reapCmd}
}

func configureVaultReap(c *cli.Command) {
	c.Flags = cs.RegisterPGFlags(c.Flags)
	c.Flags = api.RegisterFlags(c.Flags)
	c.Flags = claims.RegisterClientFlags(c.Flags)
	c.Flags = vault.RegisterApiFlags(c.Flags)
	c.Flags = vault.RegisterFlags(c.Flags)
	c.Flags = common.RegisterFlags(c.Flags)
}

func reap(c *cli.Context) error {
	ctx := context.Background()

	// Initialize services
	r, err := initializeReaper(c)
	if err != nil {
		return err
	}
	defer r.Close()

	log.WithField("expire_period", r.expirePeriod).
		WithField("abandoned_expire_period", r.abandonedExpirePeriod).
		WithField("transfer_timeout_period", r.transferTimeoutPeriod).
		Info("starting vault reap process")

	r.run(ctx)

	log.Info("vault reap process completed")
	return nil
}

func initializeReaper(c *cli.Context) (*reaper, error) {
	// Setting DB
	pg := cs.NewPG(c)

	// Setting Migrations (migrations are handled by the main serve command; skipping here to avoid conflicts)
	// m := cs.NewPGMigration(pg)
	// err := m.Run()
	// if err != nil {
	// 	pg.Close()
	// 	return nil, errors.Wrap(err, "failed to run migrations")
	// }

	db := pg.Get()
	if db == nil {
		pg.Close()
		return nil, errors.New("db is nil")
	}

	// Setting HTTP Client
	cl := http.DefaultClient

	// Setting Octor API
	sapi := api.New(c, cl)

	// Setting Claims Client
	cpCl := claims.NewClient(c)

	// Setting Claims
	claimsService := claims.New(c, cpCl, pg)

	// Setting Vault API
	vaultApi := vault.NewApi(c, cl)

	// Setting Vault
	vaultService := vault.New(c, vaultApi, claimsService, cl, pg, sapi)
	if vaultService == nil {
		pg.Close()
		if cpCl != nil {
			cpCl.Close()
		}
		return nil, errors.New("vault service is not configured (missing VAULT_SERVICE_HOST)")
	}

	// Setting Notification Service
	notificationService := notification.New(c, db)

	r := &reaper{
		store:                  &pgReaperStore{db: db},
		vault:                  vaultService,
		notification:           notificationService,
		expirePeriod:           c.Duration(vault.VaultResourceExpirePeriodFlag),
		abandonedExpirePeriod:  c.Duration(vault.VaultResourceAbandonedExpirePeriodFlag),
		transferTimeoutPeriod:  c.Duration(vault.VaultResourceTransferTimeoutPeriodFlag),
		pg:                     pg,
		cpCl:                   cpCl,
		httpClient:             http.DefaultClient,
	}

	return r, nil
}

func (r *reaper) Close() {
	r.pg.Close()
	if r.cpCl != nil {
		r.cpCl.Close()
	}
}

func (r *reaper) run(ctx context.Context) {
	resources, err := r.store.GetExpiredResources(ctx, r.expirePeriod, r.abandonedExpirePeriod, r.transferTimeoutPeriod)
	if err != nil {
		log.WithError(err).Warn("failed to get expired resources")
		return
	}

	log.WithField("count", len(resources)).Info("found expired resources")

	for _, resource := range resources {
		r.processResource(ctx, resource)
	}

	// Clean up ghost resources — funded_vp > 0 but no funded pledges
	// (caused by user account deletion cascading pledges but not updating resource)
	r.reapGhostResources(ctx)
}

func (r *reaper) reapGhostResources(ctx context.Context) {
	resources, err := r.store.GetGhostResources(ctx)
	if err != nil {
		log.WithError(err).Warn("failed to get ghost resources")
		return
	}

	if len(resources) == 0 {
		return
	}

	log.WithField("count", len(resources)).Info("found ghost resources (funded but no pledges)")

	for _, resource := range resources {
		log.WithField("resource_id", resource.ResourceID).
			WithField("name", resource.Name).
			WithField("funded_vp", resource.FundedVP).
			Info("removing ghost resource")

		err := r.vault.RemoveResource(ctx, resource.ResourceID)
		if err != nil {
			log.WithError(err).
				WithField("resource_id", resource.ResourceID).
				Warn("failed to remove ghost resource")
			continue
		}

		log.WithField("resource_id", resource.ResourceID).Info("removed ghost resource")
	}
}

func (r *reaper) processResource(ctx context.Context, resource vaultModels.Resource) {
	isTransferTimeout := resource.ExpiredAt == nil

	log.WithField("resource_id", resource.ResourceID).
		WithField("is_transfer_timeout", isTransferTimeout).
		Info("processing resource")

	// Get all pledges for this resource with user information
	pledges, err := r.store.GetResourcePledgesWithUsers(ctx, resource.ResourceID)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			Warn("failed to get resource pledges, skipping resource")
		return
	}

	log.WithField("resource_id", resource.ResourceID).
		WithField("pledge_count", len(pledges)).
		Info("found pledges for resource")

	if isTransferTimeout {
		// Check global environment toggle
		if os.Getenv("VAULT_AUTO_DELETE_UNSEEDED") == "false" {
			log.WithField("resource_id", resource.ResourceID).Info("global vault auto delete is disabled, skipping")
			return
		}

		// Check if any user who pledged to this resource has disabled auto-delete
		for _, pledge := range pledges {
			if pledge.User != nil && !pledge.User.VaultAutoDeleteUnseeded {
				log.WithField("resource_id", resource.ResourceID).
					WithField("user_id", pledge.UserID).
					Info("user has disabled vault auto delete, skipping resource deletion")
				return
			}
		}

		// Check last activity/update time from vault service (representing download progress)
		lastActivityAt := resource.CreatedAt
		if resource.FundedAt != nil {
			lastActivityAt = *resource.FundedAt
		}
		if vr, err := r.vault.GetVaultAPIResource(ctx, resource.ResourceID); err == nil && vr != nil {
			lastActivityAt = vr.UpdatedAt
		}
		if time.Since(lastActivityAt) < r.transferTimeoutPeriod {
			log.WithField("resource_id", resource.ResourceID).
				WithField("last_activity_at", lastActivityAt).
				Info("resource has recent seeding/downloading activity, skipping deletion")
			return
		}
		// Check if this was added via Transmission RPC
		type rpcTorrent struct {
			InfoHash string `json:"infoHash"`
			AddedBy  string `json:"addedBy"`
		}
		var tracked []rpcTorrent
		if data, err := os.ReadFile(cs.GetInfraDataPath("transmission_torrents.json")); err == nil {
			_ = json.Unmarshal(data, &tracked)
		}
		var isRPC bool
		for _, t := range tracked {
			if strings.ToLower(t.InfoHash) == strings.ToLower(resource.ResourceID) {
				isRPC = true
				break
			}
		}

		if isRPC {
			log.WithField("resource_id", resource.ResourceID).Info("stuck torrent was added via Transmission RPC, attempting to remove and blocklist in *arrs")
			if r.removeAndBlocklistInArrs(ctx, resource.ResourceID) {
				log.WithField("resource_id", resource.ResourceID).Info("successfully requested *arr to remove and blocklist, skipping local cleanup (will be handled by *arr's RPC client)")
				return
			}
			log.WithField("resource_id", resource.ResourceID).Warn("failed to remove from *arrs, falling back to local cleanup")
		}
	}

	// Remove all pledges, clean up library, and send notifications
	for _, pledge := range pledges {
		// Clean up from library if exists
		err = r.store.RemoveFromLibrary(ctx, pledge.UserID, resource.ResourceID)
		if err != nil {
			log.WithError(err).
				WithField("resource_id", resource.ResourceID).
				WithField("user_id", pledge.UserID).
				Warn("failed to remove from library")
		}
		r.removePledgeAndNotify(ctx, pledge, resource, isTransferTimeout)
	}

	// Delete the resource
	err = r.vault.RemoveResource(ctx, resource.ResourceID)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			Warn("failed to delete resource")
		return
	}

	log.WithField("resource_id", resource.ResourceID).Info("deleted resource")
}

func (r *reaper) removeAndBlocklistInArrs(ctx context.Context, infoHash string) bool {
	radarrKey := os.Getenv("RADARR_API_KEY")
	sonarrKey := os.Getenv("SONARR_API_KEY")
	whisparrKey := os.Getenv("WHISPARR_API_KEY")

	type clientConfig struct {
		name string
		port int
		key  string
		path string
	}

	clients := []clientConfig{
		{name: "radarr", port: 7878, key: radarrKey, path: "/radarr"},
		{name: "sonarr", port: 8989, key: sonarrKey, path: "/sonarr"},
		{name: "whisparr", port: 6969, key: whisparrKey, path: "/whisparr"},
	}

	removedAny := false

	type arrQueueItem struct {
		ID         int    `json:"id"`
		DownloadID string `json:"downloadId"`
	}
	type arrQueueResponse struct {
		Records []arrQueueItem `json:"records"`
	}

	for _, client := range clients {
		if client.key == "" {
			continue
		}
		// 1. Fetch the queue
		resp, err := r.makeArrRequest(ctx, client.name, client.port, client.key, "GET", client.path+"/api/v3/queue")
		if err != nil {
			log.WithError(err).Warnf("failed to fetch queue from %s", client.name)
			continue
		}
		defer resp.Body.Close()

		var queueResp arrQueueResponse
		bodyBytes, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(bodyBytes, &queueResp); err != nil {
			log.WithError(err).Warnf("failed to parse queue response from %s", client.name)
			continue
		}

		for _, item := range queueResp.Records {
			if strings.ToLower(item.DownloadID) == strings.ToLower(infoHash) {
				log.Infof("found stuck torrent %s in %s queue, removing and blocklisting", infoHash, client.name)
				// 2. Delete and blocklist
				deletePath := fmt.Sprintf("%s/api/v3/queue/%d?removeFromClient=true&blocklist=true", client.path, item.ID)
				delResp, delErr := r.makeArrRequest(ctx, client.name, client.port, client.key, "DELETE", deletePath)
				if delErr != nil {
					log.WithError(delErr).Warnf("failed to delete queue item %d from %s", item.ID, client.name)
				} else {
					delResp.Body.Close()
					log.Infof("successfully requested removal and blocklist of stuck torrent %s from %s", infoHash, client.name)
					removedAny = true
				}
			}
		}
	}

	return removedAny
}

func (r *reaper) makeArrRequest(ctx context.Context, serviceName string, port int, apiKey string, method string, path string) (*http.Response, error) {
	// Try host.docker.internal first, then localhost, then 127.0.0.1
	hosts := []string{"host.docker.internal", "localhost", "127.0.0.1"}
	var lastErr error
	for _, host := range hosts {
		url := fmt.Sprintf("http://%s:%d%s", host, port, path)
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.httpClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted {
				return resp, nil
			}
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (r *reaper) removePledgeAndNotify(ctx context.Context, pledge vaultModels.Pledge, resource vaultModels.Resource, isTransferTimeout bool) {
	// Remove pledge
	err := r.vault.RemovePledge(ctx, &pledge)
	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("pledge_id", pledge.PledgeID).
			Warn("failed to remove pledge, skipping")
		return
	}

	log.WithField("resource_id", resource.ResourceID).
		WithField("pledge_id", pledge.PledgeID).
		WithField("user_id", pledge.UserID).
		WithField("amount", pledge.Amount).
		Info("removed pledge")

	// Send notification to user if user data is available
	if pledge.User == nil || pledge.User.Email == "" {
		return
	}

	r.sendNotification(pledge.User.Email, resource, isTransferTimeout)
}

func (r *reaper) sendNotification(email string, resource vaultModels.Resource, isTransferTimeout bool) {
	var err error
	var action string
	if isTransferTimeout {
		err = r.notification.SendTransferTimeout(email, &resource)
		action = "transfer timeout"
	} else {
		err = r.notification.SendExpired(email, &resource)
		action = "expiration"
	}

	if err != nil {
		log.WithError(err).
			WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Warn("failed to send " + action + " notification")
	} else {
		log.WithField("resource_id", resource.ResourceID).
			WithField("user_email", email).
			Info("sent " + action + " notification")
	}
}
