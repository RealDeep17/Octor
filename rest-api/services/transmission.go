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
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

type TransmissionService struct {
	rm             *ResourceMap
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

func NewTransmissionService(c *cli.Context, rm *ResourceMap) *TransmissionService {
	s := &TransmissionService{
		rm:             rm,
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

	// 2. Authentication check
	if s.apiKey != "" {
		_, password, ok := g.Request.BasicAuth()
		if !ok || password != s.apiKey {
			// Also check standard Header
			headerKey := g.Request.Header.Get("X-Api-Key")
			if headerKey != s.apiKey {
				g.Header("WWW-Authenticate", `Basic realm="Transmission"`)
				g.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
	}

	// 3. Parse JSON Body
	var rpcReq TransmissionRPCReq
	if err := g.BindJSON(&rpcReq); err != nil {
		g.JSON(http.StatusBadRequest, TransmissionRPCResp{Result: "invalid JSON"})
		return
	}

	respArgs := make(map[string]interface{})
	result := "success"

	switch rpcReq.Method {
	case "session-get":
		respArgs["version"] = "4.0.0"
		respArgs["rpc-version-minimum"] = 1
		respArgs["rpc-version"] = 17
		respArgs["download-dir"] = "/srv/octor/infra-data/drive-mount"
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
		res, err := s.rm.Get(g.Request.Context(), payload)
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

		// Auto-vault: fire-and-forget, non-blocking
		if s.autoVaultEnabled() {
			go s.triggerAutoVault(context.Background(), res.ID)
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
			} else if total > 0 {
				percentDone = float64(stored) / float64(total)
			} else if tracked.TotalSize > 0 {
				// Fallback to local cache if size resolved
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
				delete(s.trackedTorrent, hash)
			}
			s.torrentsLock.Unlock()

			_ = s.saveTorrents()
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
