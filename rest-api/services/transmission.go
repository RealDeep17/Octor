package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
)

type TransmissionService struct {
	rm             *ResourceMap
	db             *pg.DB
	apiKey         string
	vaultHost      string
	vaultPort      int
	autoVault      bool
	persistFile    string
	httpClient     *http.Client
	torrentsLock   sync.RWMutex
	trackedTorrent map[string]TrackedTorrent // Key: infoHash
}

type TrackedTorrent struct {
	InfoHash  string    `json:"infoHash"`
	Name      string    `json:"name"`
	AddedAt   time.Time `json:"addedAt"`
	TotalSize int64     `json:"totalSize"`
}

type TransmissionRPCReq struct {
	Method    string                 `json:"method"`
	Arguments map[string]interface{} `json:"arguments"`
	Tag       *int                   `json:"tag,omitempty"`
}

type TransmissionRPCResp struct {
	Result    string                 `json:"result"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
	Tag       *int                   `json:"tag,omitempty"`
}

const (
	automationAPIKeyFlag   = "automation-apikey"
	automationAutoVaultFlag = "automation-auto-vault"
	transmissionSessionID  = "octor-transmission-session-id"
	transmissionPersistDir = "/srv/octor/infra-data"
	transmissionSettingsFile = "/srv/octor/infra-data/settings.json"
)

func RegisterTransmissionFlags(f []cli.Flag) []cli.Flag {
	f = cs.RegisterPGFlags(f)
	return append(f,
		cli.StringFlag{
			Name:   automationAPIKeyFlag,
			Usage:  "Automation API Key to protect webhook and RPC endpoints",
			Value:  "",
			EnvVar: "AUTOMATION_API_KEY",
		},
		cli.BoolFlag{
			Name:   automationAutoVaultFlag,
			Usage:  "Automatically vault torrents ingested via Transmission RPC (can be overridden at runtime via settings.json)",
			EnvVar: "AUTOMATION_AUTO_VAULT",
		},
		cli.StringFlag{
			Name:   "vault-host-rpc",
			Usage:  "Vault service host",
			Value:  "localhost",
			EnvVar: "VAULT_SERVICE_HOST",
		},
		cli.IntFlag{
			Name:   "vault-port-rpc",
			Usage:  "Vault service port",
			Value:  8086,
			EnvVar: "VAULT_SERVICE_PORT",
		},
	)
}

func NewTransmissionService(c *cli.Context, rm *ResourceMap, db *pg.DB) *TransmissionService {
	s := &TransmissionService{
		rm:             rm,
		db:             db,
		apiKey:         c.String(automationAPIKeyFlag),
		autoVault:      c.Bool(automationAutoVaultFlag),
		vaultHost:      c.String("vault-host-rpc"),
		vaultPort:      c.Int("vault-port-rpc"),
		persistFile:    filepath.Join(transmissionPersistDir, "transmission_torrents.json"),
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		trackedTorrent: make(map[string]TrackedTorrent),
	}

	if err := s.loadTorrents(); err != nil {
		log.WithError(err).Warn("Failed to load tracked Transmission torrents")
	}

	return s
}

func (s *TransmissionService) loadTorrents() error {
	s.torrentsLock.Lock()
	defer s.torrentsLock.Unlock()

	if _, err := os.Stat(s.persistFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(s.persistFile)
	if err != nil {
		return errors.Wrap(err, "failed to read torrents file")
	}

	var list []TrackedTorrent
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.Wrap(err, "failed to unmarshal torrents file")
	}

	for _, t := range list {
		s.trackedTorrent[t.InfoHash] = t
	}

	log.Infof("Loaded %d tracked torrents for Transmission RPC", len(list))
	return nil
}

func (s *TransmissionService) saveTorrents() error {
	s.torrentsLock.RLock()
	var list []TrackedTorrent
	for _, t := range s.trackedTorrent {
		list = append(list, t)
	}
	s.torrentsLock.RUnlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal torrents list")
	}

	if err := os.MkdirAll(transmissionPersistDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create persist directory")
	}

	if err := os.WriteFile(s.persistFile, data, 0644); err != nil {
		return errors.Wrap(err, "failed to write torrents file")
	}

	return nil
}

func stringToIntID(s string) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() & 0x7fffffff)
}

// autoVaultEnabled checks the runtime settings file first, then falls back to the
// startup env flag. This lets admins toggle vaulting without a service restart.
func (s *TransmissionService) autoVaultEnabled() bool {
	type settings struct {
		AutoVault *bool `json:"auto_vault"`
	}
	data, err := os.ReadFile(transmissionSettingsFile)
	if err == nil {
		var cfg settings
		if json.Unmarshal(data, &cfg) == nil && cfg.AutoVault != nil {
			return *cfg.AutoVault
		}
	}
	return s.autoVault
}

// triggerAutoVault calls the vault worker's PUT /resource/:id endpoint to queue
// the torrent for long-term storage. This is a best-effort fire-and-forget;
// failure is logged but does NOT fail the torrent-add response.
func (s *TransmissionService) triggerAutoVault(ctx context.Context, infoHash string) {
	if s.vaultHost == "" {
		log.Warn("auto-vault: VAULT_SERVICE_HOST not configured, skipping")
		return
	}
	u := fmt.Sprintf("http://%s:%d/resource/%s", s.vaultHost, s.vaultPort, infoHash)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
	if err != nil {
		log.WithError(err).Errorf("auto-vault: failed to build PUT request for %s", infoHash)
		return
	}
	cl := s.httpClient
	if cl == nil {
		cl = http.DefaultClient
	}
	resp, err := cl.Do(req)
	if err != nil {
		log.WithError(err).Errorf("auto-vault: PUT request failed for %s", infoHash)
		return
	}
	defer resp.Body.Close()
	log.Infof("auto-vault: queued %s for vaulting (status=%d)", infoHash, resp.StatusCode)
}

func (s *TransmissionService) getVaultStatus(ctx context.Context, hash string) (stored int64, total int64, completed bool) {
	url := fmt.Sprintf("http://%s:%d/resource/%s", s.vaultHost, s.vaultPort, hash)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, false
	}
	cl := s.httpClient
	if cl == nil {
		cl = http.DefaultClient
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var status struct {
			Status     int   `json:"status"`
			StoredSize int64 `json:"stored_size"`
			TotalSize  int64 `json:"total_size"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
			return status.StoredSize, status.TotalSize, status.Status == 2 // StatusCompleted = 2
		}
	}
	return 0, 0, false
}

func (s *TransmissionService) HandleRPC(g *gin.Context) {
	// 1. CSRF Token Validation
	csrfHeader := g.Request.Header.Get("X-Transmission-Session-Id")
	if csrfHeader != transmissionSessionID {
		g.Header("X-Transmission-Session-Id", transmissionSessionID)
		g.Data(http.StatusConflict, "text/html", []byte("X-Transmission-Session-Id header is missing or invalid."))
		return
	}

	// 2. Authentication check and target user extraction
	var targetEmails []string
	username, password, ok := g.Request.BasicAuth()
	log.Debugf("TransmissionRPC: BasicAuth ok=%v, username=%s", ok, username)

	if s.apiKey != "" {
		if !ok || password != s.apiKey {
			// Also check standard Header
			headerKey := g.Request.Header.Get("X-Api-Key")
			if headerKey != s.apiKey {
				log.Warnf("TransmissionRPC: unauthorized request from %s", g.ClientIP())
				g.Header("WWW-Authenticate", `Basic realm="Transmission"`)
				g.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
	}

	if username != "" {
		// Extract emails from username field
		for _, email := range strings.Split(username, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				targetEmails = append(targetEmails, email)
			}
		}
		log.Debugf("TransmissionRPC: target emails resolved from auth: %v", targetEmails)
	}

	// 3. Parse JSON Body
	var rpcReq TransmissionRPCReq
	if err := g.BindJSON(&rpcReq); err != nil {
		g.JSON(http.StatusBadRequest, TransmissionRPCResp{Result: "invalid JSON"})
		return
	}
	log.Debugf("TransmissionRPC: method=%s, targetEmails=%v", rpcReq.Method, targetEmails)

	respArgs := make(map[string]interface{})
	result := "success"

	switch rpcReq.Method {
	case "session-get":
		respArgs["version"] = "4.0.0"
		respArgs["rpc-version-minimum"] = 1
		respArgs["rpc-version"] = 17
		respArgs["download-dir"] = "/srv/Big ARRS/downloads"
		respArgs["download-dir-free-space"] = int64(1099511627776) // 1 TB fake space

	case "session-close":
		// Session close is a no-op

	case "torrent-add":
		filename, _ := rpcReq.Arguments["filename"].(string)
		metainfoStr, _ := rpcReq.Arguments["metainfo"].(string)

		var payload []byte
		var err error

		if metainfoStr != "" {
			payload, err = base64.StdEncoding.DecodeString(metainfoStr)
			if err != nil {
				result = "invalid base64 metainfo"
				break
			}
		} else if filename != "" {
			if strings.HasPrefix(filename, "http://") || strings.HasPrefix(filename, "https://") {
				payload, err = s.DownloadTorrentURL(g.Request.Context(), filename)
				if err != nil {
					log.WithError(err).Errorf("Failed to download torrent URL: %s", filename)
					result = fmt.Sprintf("failed to download torrent URL: %v", err)
					break
				}
			} else {
				payload = []byte(filename)
			}
		} else {
			result = "missing filename or metainfo"
			break
		}

		// Import the torrent into Octor
		res, err := s.rm.Get(context.Background(), payload)
		if err != nil {
			log.WithError(err).Errorf("Failed to add resource to Octor")
			result = err.Error()
			break
		}

		s.torrentsLock.Lock()
		_, exists := s.trackedTorrent[res.ID]
		tracked := TrackedTorrent{
			InfoHash:  res.ID,
			Name:      res.Name,
			AddedAt:   time.Now(),
			TotalSize: res.Size,
		}
		s.trackedTorrent[res.ID] = tracked
		s.torrentsLock.Unlock()

		_ = s.saveTorrents()

		if !exists {
			// Octor-Native Library Ingestion
			if len(targetEmails) > 0 && s.db != nil {
				go s.HandleLibraryIngest(context.Background(), res, targetEmails)
			}

			// Auto-vault: fire-and-forget, non-blocking
			if s.autoVaultEnabled() {
				go s.triggerAutoVault(context.Background(), res.ID)
			}
		}

		addedTorrent := map[string]interface{}{
			"id":         stringToIntID(res.ID),
			"name":       res.Name,
			"hashString": res.ID,
		}

		if exists {
			respArgs["torrent-duplicate"] = addedTorrent
		} else {
			respArgs["torrent-added"] = addedTorrent
		}

	case "torrent-get":
		s.torrentsLock.RLock()
		torrentsList := []interface{}{}
		for _, tracked := range s.trackedTorrent {
			id := stringToIntID(tracked.InfoHash)
			stored, total, completed := s.getVaultStatus(g.Request.Context(), tracked.InfoHash)

			percentDone := 0.0
			if completed {
				percentDone = 1.0
				_ = s.ensureDummyFiles(g.Request.Context(), tracked.InfoHash)
			} else if total > 0 {
				percentDone = float64(stored) / float64(total)
			} else if s.autoVaultEnabled() {
				percentDone = 0.0
			} else if tracked.TotalSize > 0 {
				percentDone = 1.0
			}

			status := 4 // Downloading
			if completed {
				status = 6 // Seeding
			}

			leftUntilDone := int64(0)
			if total > 0 {
				leftUntilDone = total - stored
			} else if !completed {
				leftUntilDone = tracked.TotalSize
			}

			tSize := total
			if tSize == 0 {
				tSize = tracked.TotalSize
			}

			filesList := []interface{}{}
			if res, err := s.rm.Get(g.Request.Context(), []byte(tracked.InfoHash)); err == nil && res != nil {
				for _, f := range res.Files {
					fPath := strings.Join(f.Path, "/")
					bytesCompleted := int64(0)
					if completed {
						bytesCompleted = f.Size
					} else if total > 0 {
						bytesCompleted = int64(float64(f.Size) * (float64(stored) / float64(total)))
					}
					filesList = append(filesList, map[string]interface{}{
						"bytesCompleted": bytesCompleted,
						"length":         f.Size,
						"name":           fPath,
					})
				}
			}

			torrentInfo := map[string]interface{}{
				"id":            id,
				"name":          tracked.Name,
				"hashString":    tracked.InfoHash,
				"percentDone":   percentDone,
				"status":        status,
				"totalSize":     tSize,
				"leftUntilDone": leftUntilDone,
				"rateDownload":  0,
				"rateUpload":    0,
				"eta":           -1,
				"error":         0,
				"errorString":    "",
				"isFinished":    completed,
				"downloadDir":   "/srv/Big ARRS/downloads",
				"files":         filesList,
			}
			torrentsList = append(torrentsList, torrentInfo)
		}
		s.torrentsLock.RUnlock()
		respArgs["torrents"] = torrentsList

	case "torrent-remove":
		// Get IDs of torrents to remove
		idsArg, ok := rpcReq.Arguments["ids"]
		if ok {
			var hashStringsToRemove []string
			s.torrentsLock.Lock()

			switch val := idsArg.(type) {
			case []interface{}:
				for _, idVal := range val {
					if idStr, ok := idVal.(string); ok {
						// Checked if passed hash string
						hashStringsToRemove = append(hashStringsToRemove, idStr)
					} else if idNum, ok := idVal.(float64); ok {
						// Look up by integer ID
						targetID := int(idNum)
						for hash := range s.trackedTorrent {
							if stringToIntID(hash) == targetID {
								hashStringsToRemove = append(hashStringsToRemove, hash)
							}
						}
					}
				}
			case string:
				hashStringsToRemove = append(hashStringsToRemove, val)
			case float64:
				targetID := int(val)
				for hash := range s.trackedTorrent {
					if stringToIntID(hash) == targetID {
						hashStringsToRemove = append(hashStringsToRemove, hash)
					}
				}
			}

			for _, hash := range hashStringsToRemove {
				go s.removeDummyFiles(context.Background(), hash)
				delete(s.trackedTorrent, hash)
			}
			s.torrentsLock.Unlock()

			_ = s.saveTorrents()

			// Perform library and vault cleanup for target users
			if len(targetEmails) > 0 && s.db != nil {
				go func(hashes []string, emails []string) {
					ctx := context.Background()
					// 1. Resolve users
					var userRows []struct {
						UserID string `pg:"user_id"`
					}
					err := s.db.Model().Table("user").
						Column("user_id").
						Where("email IN (?)", pg.In(emails)).
						Select(&userRows)
					if err != nil {
						log.WithError(err).Errorf("torrent-remove: failed to resolve user emails: %v", emails)
						return
					}
					if len(userRows) == 0 {
						return
					}
					var userIDs []string
					for _, r := range userRows {
						userIDs = append(userIDs, r.UserID)
					}

					for _, hash := range hashes {
						// 2. Delete library entries for these users
						_, err := s.db.ExecContext(ctx, "DELETE FROM library WHERE resource_id = ? AND user_id IN (?)", hash, pg.In(userIDs))
						if err != nil {
							log.WithError(err).Errorf("torrent-remove: failed to delete library entry for %s", hash)
						}

						// 3. For vault pledges, we need to subtract the pledge amounts from vault.resource.funded_vp
						var pledges []struct {
							Amount float64 `pg:"amount"`
						}
						err = s.db.Model().Table("vault.pledge").
							Column("amount").
							Where("resource_id = ? AND user_id IN (?)", hash, pg.In(userIDs)).
							Select(&pledges)
						if err != nil && !errors.Is(err, pg.ErrNoRows) {
							log.WithError(err).Errorf("torrent-remove: failed to select pledges for %s", hash)
						}

						deletedPledgeSum := float64(0)
						for _, p := range pledges {
							deletedPledgeSum += p.Amount
						}

						// Delete the pledges
						if len(pledges) > 0 {
							_, err = s.db.ExecContext(ctx, "DELETE FROM vault.pledge WHERE resource_id = ? AND user_id IN (?)", hash, pg.In(userIDs))
							if err != nil {
								log.WithError(err).Errorf("torrent-remove: failed to delete pledges for %s", hash)
							}
						}

						// 4. Update vault.resource funded_vp and check if we should delete
						var vaultRes struct {
							RequiredVP float64 `pg:"required_vp"`
							FundedVP   float64 `pg:"funded_vp"`
						}
						err = s.db.Model().Table("vault.resource").
							Column("required_vp", "funded_vp").
							Where("resource_id = ?", hash).
							Select(&vaultRes)
						
						if err == nil {
							newFundedVP := vaultRes.FundedVP - deletedPledgeSum
							if newFundedVP < 0 {
								newFundedVP = 0
							}

							// Check if there are any other users left in library for this resource
							var libCount int
							_, err = s.db.QueryOneContext(ctx, pg.Scan(&libCount), "SELECT COUNT(*) FROM library WHERE resource_id = ?", hash)
							if err != nil {
								log.WithError(err).Errorf("torrent-remove: failed to count library entries for %s", hash)
							}

							if libCount == 0 {
								// No users left! Delete everything and queue vault deletion
								_, _ = s.db.ExecContext(ctx, "DELETE FROM vault.resource WHERE resource_id = ?", hash)
								_, _ = s.db.ExecContext(ctx, "DELETE FROM torrent_resource WHERE resource_id = ?", hash)

								// Trigger vault microservice DELETE API
								go func(id string) {
									log.Infof("torrent-remove: queueing vault deletion for %s", id)
									u := fmt.Sprintf("http://%s:%d/resource/%s", s.vaultHost, s.vaultPort, id)
									req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, u, nil)
									if err != nil {
										return
									}
									resp, err := s.httpClient.Do(req)
									if err == nil {
										resp.Body.Close()
									}
								}(hash)
							} else {
								// Users still left. Just update funding
								funded := newFundedVP >= vaultRes.RequiredVP && vaultRes.RequiredVP > 0
								_, err = s.db.ExecContext(ctx, "UPDATE vault.resource SET funded_vp = ?, funded = ? WHERE resource_id = ?", newFundedVP, funded, hash)
								if err != nil {
									log.WithError(err).Errorf("torrent-remove: failed to update vault.resource for %s", hash)
								}

								// If no longer funded, queue vault deletion
								if !funded {
									go func(id string) {
										log.Infof("torrent-remove: resource %s is no longer funded; queueing vault deletion", id)
										u := fmt.Sprintf("http://%s:%d/resource/%s", s.vaultHost, s.vaultPort, id)
										req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, u, nil)
										if err != nil {
											return
										}
										resp, err := s.httpClient.Do(req)
										if err == nil {
											resp.Body.Close()
										}
									}(hash)
								}
							}
						}
					}
				}(hashStringsToRemove, targetEmails)
			}
		}

	default:
		result = "unsupported method"
	}

	g.JSON(http.StatusOK, TransmissionRPCResp{
		Result:    result,
		Arguments: respArgs,
		Tag:       rpcReq.Tag,
	})
}

func (s *TransmissionService) DownloadTorrentURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	cl := s.httpClient
	if cl == nil {
		cl = http.DefaultClient
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	// Limit reader to 10MB to prevent Denial of Service
	limitReader := io.LimitReader(resp.Body, 10*1024*1024)
	return io.ReadAll(limitReader)
}

func (s *TransmissionService) HandleLibraryIngest(ctx context.Context, res *Resource, emails []string) {
	if s.db == nil {
		return
	}

	// 1. Resolve users
	var userRows []struct {
		UserID string `pg:"user_id"`
	}
	err := s.db.Model().Table("user").
		Column("user_id").
		Where("email IN (?)", pg.In(emails)).
		Select(&userRows)
	if err != nil {
		log.WithError(err).Errorf("LibraryIngest: failed to resolve user emails: %v", emails)
		return
	}

	if len(userRows) == 0 {
		log.Warnf("LibraryIngest: no Octor users found for emails: %v", emails)
		return
	}

	// 2. Insert Torrent Resource
	torrentRes := map[string]interface{}{
		"resource_id":        res.ID,
		"name":               res.Name,
		"file_count":         len(res.Files),
		"size_bytes":         res.Size,
		"torrent_size_bytes": int64(len(res.Torrent)),
		"created_at":         time.Now(),
	}
	_, err = s.db.Model(&torrentRes).Table("torrent_resource").OnConflict("DO NOTHING").Insert()
	if err != nil {
		log.WithError(err).Errorf("LibraryIngest: failed to insert torrent_resource for %s", res.ID)
	}

	// 3. Insert Library entries for each user
	for _, row := range userRows {
		libEntry := map[string]interface{}{
			"user_id":     row.UserID,
			"resource_id": res.ID,
			"name":        res.Name,
			"created_at":  time.Now(),
		}
		_, err = s.db.Model(&libEntry).Table("library").OnConflict("DO NOTHING").Insert()
		if err != nil {
			log.WithError(err).Errorf("LibraryIngest: failed to insert library entry for user %s, resource %s", row.UserID, res.ID)
		}
	}

	// 4. Trigger Metadata Enrichment
	// We call the running Web UI service via an internal HTTP request.
	// This removes the dependency on local shell scripts.
	go func(id string, apiKey string) {
		log.Infof("LibraryIngest: triggering metadata enrichment for %s via internal API", id)
		url := fmt.Sprintf("http://127.0.0.1:8082/enrich/%s", id)
		if apiKey != "" {
			url += "?api_key=" + apiKey
		}
		req, _ := http.NewRequest("POST", url, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.WithError(err).Errorf("LibraryIngest: internal enrichment trigger failed for %s", id)
		} else {
			defer resp.Body.Close()
			log.Infof("LibraryIngest: internal enrichment trigger accepted for %s (status=%d)", id, resp.StatusCode)
		}
	}(res.ID, s.apiKey)

	// 5. Vault Logic (if enabled)
	if s.autoVaultEnabled() {
		requiredVP := float64(res.Size) / (1024 * 1024 * 1024)
		
		// Ensure vault.resource exists
		vaultRes := map[string]interface{}{
			"resource_id": res.ID,
			"required_vp": requiredVP,
			"funded_vp":   requiredVP,
			"funded":      true,
			"funded_at":   time.Now(),
			"vaulted":     false,
			"name":        res.Name,
			"created_at":  time.Now(),
			"updated_at":  time.Now(),
		}
		_, err = s.db.Model(&vaultRes).Table("vault.resource").
			OnConflict("(resource_id) DO UPDATE SET funded_vp = EXCLUDED.funded_vp, funded = EXCLUDED.funded, funded_at = EXCLUDED.funded_at").
			Insert()
		if err != nil {
			log.WithError(err).Errorf("LibraryIngest: failed to ensure vault.resource for %s", res.ID)
		}

		// Create pledges for each user
		for _, row := range userRows {
			pledge := map[string]interface{}{
				"resource_id": res.ID,
				"user_id":     row.UserID,
				"amount":      requiredVP,
				"funded":      true,
				"frozen_at":   time.Now(),
				"created_at":  time.Now(),
				"updated_at":  time.Now(),
			}
			_, err = s.db.Model(&pledge).Table("vault.pledge").OnConflict("DO NOTHING").Insert()
			if err != nil {
				log.WithError(err).Errorf("LibraryIngest: failed to create vault.pledge for user %s, resource %s", row.UserID, res.ID)
			}
		}
		log.Infof("LibraryIngest: successfully added %s to library and vault for %d users", res.ID, len(userRows))
	} else {
		log.Infof("LibraryIngest: successfully added %s to library for %d users", res.ID, len(userRows))
	}
}

func (s *TransmissionService) ensureDummyFiles(ctx context.Context, infoHash string) error {
	markerPath := filepath.Join("/srv/Big ARRS/downloads", ".octor_dummy_"+infoHash)
	if _, err := os.Stat(markerPath); err == nil {
		return nil // already created
	}

	res, err := s.rm.Get(ctx, []byte(infoHash))
	if err != nil {
		return err
	}
	for _, f := range res.Files {
		fPath := strings.Join(f.Path, "/")
		fullPath := filepath.Join("/srv/Big ARRS/downloads", fPath)

		cleanPath := filepath.Clean(fullPath)
		if !strings.HasPrefix(cleanPath, "/srv/Big ARRS/downloads/") {
			log.Warnf("DummyFiles: path traversal attempt blocked: %s", fPath)
			continue
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(cleanPath), 0755); err != nil {
			log.WithError(err).Errorf("Failed to create dummy directory: %s", filepath.Dir(cleanPath))
			continue
		}

		// Check if file already exists
		if _, err := os.Stat(cleanPath); err == nil {
			continue // already exists
		}

		ext := strings.ToLower(filepath.Ext(cleanPath))
		isVideo := ext == ".mkv" || ext == ".mp4" || ext == ".avi" || ext == ".ts" || ext == ".m4v" || ext == ".mov" || ext == ".wmv"

		var createErr error
		if isVideo {
			// Copy dummy.mkv template
			createErr = copyFile("/srv/octor/infra-data/dummy.mkv", cleanPath)
			if createErr != nil {
				log.WithError(createErr).Errorf("Failed to copy dummy video template: %s", cleanPath)
				// Fallback to 0-byte file
				createErr = createEmptyFile(cleanPath)
			} else {
				log.Infof("Created dummy video file for Sonarr import: %s", cleanPath)
			}
		} else {
			createErr = createEmptyFile(cleanPath)
			if createErr == nil {
				log.Infof("Created empty dummy file for Sonarr import: %s", cleanPath)
			}
		}

		if createErr != nil {
			log.WithError(createErr).Errorf("Failed to create dummy file: %s", cleanPath)
			continue
		}
	}

	// Create marker file
	if file, err := os.Create(markerPath); err == nil {
		file.Close()
	}
	return nil
}

func (s *TransmissionService) removeDummyFiles(ctx context.Context, infoHash string) {
	markerPath := filepath.Join("/srv/Big ARRS/downloads", ".octor_dummy_"+infoHash)
	_ = os.Remove(markerPath)

	res, err := s.rm.Get(ctx, []byte(infoHash))
	if err != nil {
		return
	}
	for _, f := range res.Files {
		fPath := strings.Join(f.Path, "/")
		fullPath := filepath.Join("/srv/Big ARRS/downloads", fPath)

		cleanPath := filepath.Clean(fullPath)
		if !strings.HasPrefix(cleanPath, "/srv/Big ARRS/downloads/") {
			continue
		}

		// Delete file
		_ = os.Remove(cleanPath)

		// Clean up empty parent directories up to /srv/Big ARRS/downloads
		parent := filepath.Dir(cleanPath)
		for parent != "/srv/Big ARRS/downloads" && parent != "/" && parent != "." {
			// Try to remove, will fail if not empty
			if err := os.Remove(parent); err != nil {
				break
			}
			parent = filepath.Dir(parent)
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}

func createEmptyFile(dst string) error {
	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	file.Close()
	return nil
}

