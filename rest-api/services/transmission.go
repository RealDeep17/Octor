package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	cs "github.com/webtor-io/common-services"
	_ "modernc.org/sqlite"
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
	InfoHash      string      `json:"infoHash"`
	Name          string      `json:"name"`
	AddedAt       time.Time   `json:"addedAt"`
	TotalSize     int64       `json:"totalSize"`
	WantedFiles   []int       `json:"wantedFiles,omitempty"`
	UnwantedFiles []int       `json:"unwantedFiles,omitempty"`
	AddedBy       string      `json:"addedBy,omitempty"`
	vaultTimer    *time.Timer `json:"-"` // not persisted; deferred auto-vault timer
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
	automationAPIKeyFlag     = "automation-apikey"
	automationAutoVaultFlag  = "automation-auto-vault"
	transmissionSessionID    = "octor-transmission-session-id"
)

func getInfraDataPath(subpath string) string {
	return cs.GetInfraDataPath(subpath)
}

var (
	transmissionPersistDir   = getInfraDataPath("")
	transmissionSettingsFile = getInfraDataPath("settings.json")
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
		httpClient:     &http.Client{Timeout: 30 * time.Second},
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

	if err := os.MkdirAll(filepath.Dir(s.persistFile), 0755); err != nil {
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

func parseIntSlice(v interface{}) []int {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]int, 0, len(arr))
	for _, item := range arr {
		if num, ok := item.(float64); ok {
			out = append(out, int(num))
		}
	}
	return out
}

func effectiveWantedIndexSet(fileCount int, wantedIndices, unwantedIndices []int) (map[int]struct{}, bool) {
	if len(wantedIndices) == 0 && len(unwantedIndices) == 0 {
		return nil, true
	}

	wanted := make(map[int]struct{}, fileCount)
	if len(wantedIndices) > 0 {
		for _, idx := range wantedIndices {
			if idx >= 0 && idx < fileCount {
				wanted[idx] = struct{}{}
			}
		}
	} else {
		for idx := 0; idx < fileCount; idx++ {
			wanted[idx] = struct{}{}
		}
	}

	for _, idx := range unwantedIndices {
		delete(wanted, idx)
	}

	return wanted, false
}

func effectiveWantedIndices(fileCount int, wantedIndices, unwantedIndices []int) ([]int, bool) {
	wantedSet, allWanted := effectiveWantedIndexSet(fileCount, wantedIndices, unwantedIndices)
	if allWanted {
		return nil, true
	}
	wanted := make([]int, 0, len(wantedSet))
	for idx := range wantedSet {
		wanted = append(wanted, idx)
	}
	sort.Ints(wanted)
	return wanted, false
}

func dummyTemplatePath(name string) (string, error) {
	candidates := []string{
		getInfraDataPath(name),
		filepath.Join("./rest-api/assets/dummy", name),
		filepath.Join("assets/dummy", name),
		filepath.Join("rest-api/assets/dummy", name),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("dummy template %s not found in any of %v", name, candidates)
}

func getDownloadsDir() string {
	if val := os.Getenv("OCTOR_DOWNLOAD_DIR"); val != "" {
		return val
	}
	return "/downloads"
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
// the torrent for long-term storage. When selectedFiles is non-empty, only those
// file paths are sent to the vault for selective downloading.
// This is a best-effort fire-and-forget; failure is logged but does NOT fail
// the torrent-add response.
func (s *TransmissionService) triggerAutoVault(ctx context.Context, infoHash string, selectedFiles []string) {
	if s.vaultHost == "" {
		log.Warn("auto-vault: VAULT_SERVICE_HOST not configured, skipping")
		return
	}
	u := fmt.Sprintf("http://%s:%d/resource/%s", s.vaultHost, s.vaultPort, infoHash)

	var payload []byte
	if len(selectedFiles) > 0 {
		var err error
		payload, err = json.Marshal(map[string]interface{}{
			"selected_files": selectedFiles,
		})
		if err != nil {
			log.WithError(err).Errorf("auto-vault: failed to marshal selected files for %s", infoHash)
			return
		}
		log.Infof("auto-vault: selective vaulting %d files for %s", len(selectedFiles), infoHash)
	}

	cl := s.httpClient
	if cl == nil {
		cl = http.DefaultClient
	}

	maxRetries := 3
	backoff := 2 * time.Second

	for i := 0; i <= maxRetries; i++ {
		if i > 0 {
			log.Infof("auto-vault: retrying PUT request for %s in %v (attempt %d/%d)", infoHash, backoff, i, maxRetries)
			select {
			case <-ctx.Done():
				log.Errorf("auto-vault: context cancelled during retry backoff for %s", infoHash)
				return
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		var bodyReader io.Reader
		if len(payload) > 0 {
			bodyReader = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bodyReader)
		if err != nil {
			log.WithError(err).Errorf("auto-vault: failed to build PUT request for %s", infoHash)
			return
		}
		if len(payload) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := cl.Do(req)
		if err != nil {
			log.WithError(err).Errorf("auto-vault: PUT request failed for %s on attempt %d", infoHash, i)
			continue
		}

		// Read and discard body, then close it immediately to prevent leaking connections
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			log.Warnf("auto-vault: received status %d from vault for %s on attempt %d", resp.StatusCode, infoHash, i)
			continue
		}

		log.Infof("auto-vault: queued %s for vaulting (status=%d, selective=%v)", infoHash, resp.StatusCode, len(selectedFiles) > 0)
		return
	}

	log.Errorf("auto-vault: failed to queue %s after %d attempts", infoHash, maxRetries+1)
}

// deferredAutoVault waits for a short delay to allow torrent-set (files-wanted)
// to arrive from the *arr app before triggering the vault. If the torrent is
// removed during the wait, the vault is skipped.
func (s *TransmissionService) deferredAutoVault(infoHash string, delay time.Duration) {
	// Add a small pseudo-random jitter (0 to 3 seconds) using FNV hash of infoHash + timestamp
	h := fnv.New32a()
	_, _ = h.Write([]byte(infoHash))
	_, _ = h.Write([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	jitter := time.Duration(h.Sum32()%3000) * time.Millisecond
	delay += jitter

	s.torrentsLock.Lock()
	tracked, exists := s.trackedTorrent[infoHash]
	if !exists {
		s.torrentsLock.Unlock()
		return
	}
	// Cancel any existing timer for this torrent (e.g. from a re-add)
	if tracked.vaultTimer != nil {
		tracked.vaultTimer.Stop()
	}
	tracked.vaultTimer = time.AfterFunc(delay, func() {
		// Safety: verify torrent still exists (could have been removed during the delay)
		s.torrentsLock.RLock()
		_, stillExists := s.trackedTorrent[infoHash]
		s.torrentsLock.RUnlock()
		if !stillExists {
			log.Infof("auto-vault: deferred trigger cancelled — torrent %s was removed during delay", infoHash)
			return
		}

		// Timer fired — resolve wanted files and trigger vault
		selectedFiles := s.resolveWantedFiles(infoHash)
		if len(selectedFiles) > 0 {
			log.Infof("auto-vault: deferred trigger for %s with %d selected files", infoHash, len(selectedFiles))
		} else {
			log.Infof("auto-vault: deferred trigger for %s (full torrent, no file selection received)", infoHash)
		}
		s.triggerAutoVault(context.Background(), infoHash, selectedFiles)
	})
	s.trackedTorrent[infoHash] = tracked
	s.torrentsLock.Unlock()
}

// resolveWantedFiles translates the integer file indices stored in
// TrackedTorrent.WantedFiles into the actual file path strings that the
// vault worker expects in its selected_files field.
func (s *TransmissionService) resolveWantedFiles(infoHash string) []string {
	s.torrentsLock.RLock()
	tracked, exists := s.trackedTorrent[infoHash]
	s.torrentsLock.RUnlock()

	if !exists {
		return nil
	}

	// Fetch the torrent's file list from the resource map
	res, err := s.rm.Get(context.Background(), []byte(infoHash))
	if err != nil || res == nil {
		log.WithError(err).Warnf("resolveWantedFiles: failed to get resource for %s", infoHash)
		return nil
	}

	// Build a set of wanted indices for O(1) lookup
	wantedSet := make(map[int]struct{}, len(tracked.WantedFiles))
	for _, idx := range tracked.WantedFiles {
		wantedSet[idx] = struct{}{}
	}

	// If all files are wanted, skip selective vaulting
	if len(wantedSet) >= len(res.Files) {
		return nil
	}

	var paths []string
	for i, f := range res.Files {
		if _, ok := wantedSet[i]; ok {
			paths = append(paths, strings.Join(f.Path, "/"))
		}
	}
	return paths
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
	log.Infof("TransmissionRPC: method=%s, arguments=%v, targetEmails=%v", rpcReq.Method, rpcReq.Arguments, targetEmails)

	respArgs := make(map[string]interface{})
	result := "success"

	switch rpcReq.Method {
	case "session-get":
		respArgs["version"] = "4.0.0"
		respArgs["rpc-version-minimum"] = 1
		respArgs["rpc-version"] = 17
		respArgs["download-dir"] = getDownloadsDir()
		respArgs["download-dir-free-space"] = int64(1099511627776) // 1 TB fake space

	case "session-close":
		// Session close is a no-op

	case "torrent-add":
		filename, _ := rpcReq.Arguments["filename"].(string)
		metainfoStr, _ := rpcReq.Arguments["metainfo"].(string)
		wanted := parseIntSlice(rpcReq.Arguments["files-wanted"])
		unwanted := parseIntSlice(rpcReq.Arguments["files-unwanted"])

		var payload []byte
		var err error

		if metainfoStr != "" {
			payload, err = base64.StdEncoding.DecodeString(metainfoStr)
			if err != nil {
				result = "invalid base64 metainfo"
				break
			}
		} else if filename != "" {
			payload = []byte(filename)
		} else {
			result = "missing filename or metainfo"
			break
		}

		wEmails, rEmails, sEmails := getArrEmails()
		var addedBy string
		if isWhisparrRequest(g, wEmails) {
			addedBy = "whisparr"
		} else if isRadarrOrSonarrRequest(g, rEmails, sEmails) {
			addedBy = "arr"
		}

		// Fast path: magnet URI — extract hash immediately without blocking on DHT.
		// Whisparr times out (~30s) waiting for torrent-add; rm.Get() on cold magnets
		// blocks 100s+, so Whisparr never records the DownloadId and marks the client
		// unavailable. We register now and do rm.Get() in the background.
		var fastHash, fastName string
		if filename != "" && strings.HasPrefix(filename, "magnet:") {
			fastHash, fastName = extractMagnetHash(filename)
		}

		if fastHash != "" {
			s.torrentsLock.Lock()
			existing, exists := s.trackedTorrent[fastHash]
			if exists {
				if fastName != "" {
					existing.Name = fastName
				}
				if len(wanted) > 0 {
					existing.WantedFiles = wanted
				}
				if len(unwanted) > 0 {
					existing.UnwantedFiles = unwanted
				}
				if addedBy != "" {
					existing.AddedBy = addedBy
				}
				s.trackedTorrent[fastHash] = existing
			} else {
				s.trackedTorrent[fastHash] = TrackedTorrent{
					InfoHash:      fastHash,
					Name:          fastName,
					AddedAt:       time.Now(),
					TotalSize:     0,
					WantedFiles:   wanted,
					UnwantedFiles: unwanted,
					AddedBy:       addedBy,
				}
			}
			s.torrentsLock.Unlock()
			_ = s.saveTorrents()

			addedTorrent := map[string]interface{}{
				"id":         stringToIntID(fastHash),
				"name":       fastName,
				"hashString": fastHash,
			}
			if exists {
				respArgs["torrent-duplicate"] = addedTorrent
			} else {
				respArgs["torrent-added"] = addedTorrent
			}

			// Background goroutine: full rm.Get() to populate real name/size,
			// then trigger library ingest + auto-vault.
			capturedExists := exists
			capturedPayload := payload
			capturedWanted := wanted
			capturedUnwanted := unwanted
			capturedTargetEmails := targetEmails
			capturedFastHash := fastHash
			go func() {
				log.Infof("torrent-add fast-path: resolving %s in background", capturedFastHash)
				res, err := s.rm.Get(context.Background(), capturedPayload)
				if err != nil {
					log.WithError(err).Warnf("torrent-add fast-path: rm.Get() failed for %s", capturedFastHash)
					return
				}
				s.torrentsLock.Lock()
				if t, ok := s.trackedTorrent[res.ID]; ok {
					t.Name = res.Name
					t.TotalSize = res.Size
					s.trackedTorrent[res.ID] = t
				}
				s.torrentsLock.Unlock()
				_ = s.saveTorrents()
				if !capturedExists {
					if len(capturedTargetEmails) > 0 && s.db != nil {
						s.HandleLibraryIngest(context.Background(), res, capturedTargetEmails)
					}
					if s.autoVaultEnabled() {
						delay := 30 * time.Second
						if len(capturedWanted) > 0 || len(capturedUnwanted) > 0 {
							delay = 2 * time.Second
							log.Infof("auto-vault fast-path: file selection present, deferring 2s for %s", res.ID)
						}
						s.deferredAutoVault(res.ID, delay)
					}
				} else if s.autoVaultEnabled() && (len(capturedWanted) > 0 || len(capturedUnwanted) > 0) {
					s.deferredAutoVault(res.ID, 2*time.Second)
				}
			}()

		} else {
			// Slow path: .torrent metainfo blob, or magnet with no parseable hash.
			res, err := s.rm.Get(g.Request.Context(), payload)
			if err != nil {
				log.WithError(err).Errorf("Failed to add resource to Octor")
				result = err.Error()
				break
			}

			s.torrentsLock.Lock()
			existing, exists := s.trackedTorrent[res.ID]
			if exists {
				// Preserve existing file selection state on re-add, updating if new selection provided
				existing.Name = res.Name
				existing.TotalSize = res.Size
				if len(wanted) > 0 {
					existing.WantedFiles = wanted
				}
				if len(unwanted) > 0 {
					existing.UnwantedFiles = unwanted
				}
				if addedBy != "" {
					existing.AddedBy = addedBy
				}
				s.trackedTorrent[res.ID] = existing
			} else {
				s.trackedTorrent[res.ID] = TrackedTorrent{
					InfoHash:      res.ID,
					Name:          res.Name,
					AddedAt:       time.Now(),
					TotalSize:     res.Size,
					WantedFiles:   wanted,
					UnwantedFiles: unwanted,
					AddedBy:       addedBy,
				}
			}
			s.torrentsLock.Unlock()
			_ = s.saveTorrents()

			if !exists {
				if len(targetEmails) > 0 && s.db != nil {
					go s.HandleLibraryIngest(context.Background(), res, targetEmails)
				}
				if s.autoVaultEnabled() {
					delay := 30 * time.Second
					if len(wanted) > 0 || len(unwanted) > 0 {
						delay = 2 * time.Second
						log.Infof("auto-vault: torrent-add already has file selection. Deferring trigger by 2s for %s", res.ID)
					}
					s.deferredAutoVault(res.ID, delay)
				}
			} else if s.autoVaultEnabled() && (len(wanted) > 0 || len(unwanted) > 0) {
				// Duplicate torrent-add can still carry a revised file selection from an ARR client.
				s.deferredAutoVault(res.ID, 2*time.Second)
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
		}

	case "torrent-get":
		s.torrentsLock.RLock()
		clonedList := make([]TrackedTorrent, 0, len(s.trackedTorrent))
		for _, tracked := range s.trackedTorrent {
			clonedList = append(clonedList, tracked)
		}
		s.torrentsLock.RUnlock()

		wEmails, rEmails, sEmails := getArrEmails()
		reqWhisparr := isWhisparrRequest(g, wEmails)
		reqRadarrSonarr := isRadarrOrSonarrRequest(g, rEmails, sEmails)

		torrentsList := []interface{}{}
		for _, tracked := range clonedList {
			isWhisparr := s.isWhisparrTorrent(g.Request.Context(), tracked.InfoHash, tracked.AddedBy)

			// Apply filtering:
			if reqRadarrSonarr && isWhisparr {
				// Radarr/Sonarr cannot see Whisparr torrents
				continue
			}
			if reqWhisparr && !isWhisparr {
				// Whisparr only sees Whisparr torrents
				continue
			}

			id := stringToIntID(tracked.InfoHash)
			stored, total, completed := s.getVaultStatus(g.Request.Context(), tracked.InfoHash)

			percentDone := 0.0
			if completed {
				percentDone = 1.0
				_ = s.ensureDummyFiles(g.Request.Context(), tracked.InfoHash, tracked.WantedFiles, tracked.UnwantedFiles)
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
			fileStatsList := []interface{}{}
			if res, err := s.rm.Get(g.Request.Context(), []byte(tracked.InfoHash)); err == nil && res != nil {
				wantedSet, allWanted := effectiveWantedIndexSet(len(res.Files), tracked.WantedFiles, tracked.UnwantedFiles)
				for i, f := range res.Files {
					fPath := strings.Join(f.Path, "/")
					_, isWanted := wantedSet[i]
					if allWanted {
						isWanted = true
					}
					bytesCompleted := int64(0)
					if completed && isWanted {
						bytesCompleted = f.Size
					} else if total > 0 && isWanted {
						bytesCompleted = int64(float64(f.Size) * (float64(stored) / float64(total)))
					}
					filesList = append(filesList, map[string]interface{}{
						"bytesCompleted": bytesCompleted,
						"length":         f.Size,
						"name":           fPath,
						"wanted":         isWanted,
					})
					fileStatsList = append(fileStatsList, map[string]interface{}{
						"bytesCompleted": bytesCompleted,
						"wanted":         isWanted,
						"priority":       0,
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
				"errorString":   "",
				"isFinished":    completed,
				"downloadDir":   getDownloadsDir(),
				"files":         filesList,
				"fileStats":     fileStatsList,
			}
			torrentsList = append(torrentsList, torrentInfo)
		}
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
				// Cancel any pending deferred vault timer to prevent ghost triggers
				if tracked, ok := s.trackedTorrent[hash]; ok && tracked.vaultTimer != nil {
					tracked.vaultTimer.Stop()
				}
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

	case "torrent-set":
		// Handle file selection from *arr apps.
		// Sonarr/Radarr/Whisparr send files-wanted/files-unwanted as arrays
		// of 0-based file indices to select which files to download.
		idsArg, _ := rpcReq.Arguments["ids"]
		filesWanted, _ := rpcReq.Arguments["files-wanted"]
		filesUnwanted, _ := rpcReq.Arguments["files-unwanted"]

		// Resolve target torrent hash(es)
		var targetHashes []string
		s.torrentsLock.RLock()
		if idsArg != nil {
			switch val := idsArg.(type) {
			case []interface{}:
				for _, idVal := range val {
					if idStr, ok := idVal.(string); ok {
						if _, exists := s.trackedTorrent[idStr]; exists {
							targetHashes = append(targetHashes, idStr)
						}
					} else if idNum, ok := idVal.(float64); ok {
						targetID := int(idNum)
						for hash := range s.trackedTorrent {
							if stringToIntID(hash) == targetID {
								targetHashes = append(targetHashes, hash)
							}
						}
					}
				}
			case string:
				if _, exists := s.trackedTorrent[val]; exists {
					targetHashes = append(targetHashes, val)
				}
			case float64:
				targetID := int(val)
				for hash := range s.trackedTorrent {
					if stringToIntID(hash) == targetID {
						targetHashes = append(targetHashes, hash)
					}
				}
			}
		}
		s.torrentsLock.RUnlock()

		wanted := parseIntSlice(filesWanted)
		unwanted := parseIntSlice(filesUnwanted)

		if len(targetHashes) > 0 && (len(wanted) > 0 || len(unwanted) > 0) {
			s.torrentsLock.Lock()
			for _, hash := range targetHashes {
				tracked, ok := s.trackedTorrent[hash]
				if !ok {
					continue
				}

				// Merge wanted files: add new wanted indices
				existingSet := make(map[int]struct{}, len(tracked.WantedFiles))
				for _, idx := range tracked.WantedFiles {
					existingSet[idx] = struct{}{}
				}
				for _, idx := range wanted {
					existingSet[idx] = struct{}{}
				}
				// Remove any unwanted from the set
				for _, idx := range unwanted {
					delete(existingSet, idx)
				}
				merged := make([]int, 0, len(existingSet))
				for idx := range existingSet {
					merged = append(merged, idx)
				}
				tracked.WantedFiles = merged

				tracked.UnwantedFiles = unwanted
				s.trackedTorrent[hash] = tracked

				log.Infof("torrent-set: %s files-wanted=%v files-unwanted=%v", hash, tracked.WantedFiles, unwanted)
			}
			s.torrentsLock.Unlock()
			_ = s.saveTorrents()

			// Re-trigger deferred vault for torrents that got file selection,
			// so the vault uses the updated selection.
			if s.autoVaultEnabled() {
				for _, hash := range targetHashes {
					s.deferredAutoVault(hash, 2*time.Second)
				}
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
	url = rewriteLocalDownloadURL(url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.HasPrefix(req.URL.String(), "magnet:") || req.URL.Scheme == "magnet" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if strings.HasPrefix(loc, "magnet:") {
			return []byte(loc), nil
		}
	}

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

func (s *TransmissionService) ensureDummyFiles(ctx context.Context, infoHash string, wantedIndices, unwantedIndices []int) error {
	res, err := s.rm.Get(ctx, []byte(infoHash))
	if err != nil {
		return err
	}

	selectedIndices, allWanted := effectiveWantedIndices(len(res.Files), wantedIndices, unwantedIndices)
	wantedSet := make(map[int]struct{}, len(selectedIndices))
	for _, idx := range selectedIndices {
		wantedSet[idx] = struct{}{}
	}

	// Build a selection-aware marker so dummies are re-created if file selection changes.
	// e.g. ".octor_dummy_<hash>" (all files) vs ".octor_dummy_<hash>_sel_3_7" (selective)
	markerSuffix := ""
	if !allWanted {
		parts := make([]string, len(selectedIndices))
		for i, idx := range selectedIndices {
			parts[i] = fmt.Sprintf("%d", idx)
		}
		markerSuffix = "_sel_" + strings.Join(parts, "_")
	}
	markerPath := filepath.Join(getDownloadsDir(), ".octor_dummy_"+infoHash+markerSuffix)
	if _, err := os.Stat(markerPath); err == nil {
		return nil // already created with this exact selection
	}

	for i, f := range res.Files {
		// Skip files not in the wanted set (selective mode)
		if !allWanted {
			if _, ok := wantedSet[i]; !ok {
				continue
			}
		}
		fPath := strings.Join(f.Path, "/")
		fullPath := filepath.Join(getDownloadsDir(), fPath)

		cleanPath := filepath.Clean(fullPath)
		if !strings.HasPrefix(cleanPath, filepath.Clean(getDownloadsDir())+string(filepath.Separator)) {
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
			// Detect resolution from torrent/file name and pick matching dummy template.
			// Uses explicit resolution tokens to avoid false positives (e.g. "hd" in "DTS-HD").
			// Covers common naming patterns used by Sonarr, Radarr and Whisparr release groups.
			name := strings.ToLower(res.Name + " " + fPath)
			is4K := strings.Contains(name, "2160p") || strings.Contains(name, "2160i") ||
				strings.Contains(name, ".4k.") || strings.Contains(name, " 4k ") ||
				strings.Contains(name, "-4k-") || strings.Contains(name, "_4k_") ||
				strings.HasSuffix(name, " 4k") || strings.HasPrefix(name, "4k ") ||
				strings.Contains(name, ".uhd.") || strings.Contains(name, " uhd ") ||
				strings.Contains(name, "-uhd-") || strings.Contains(name, "_uhd_") ||
				strings.Contains(name, "4kuhd") || strings.Contains(name, "uhd4k") ||
				strings.Contains(name, "ultrahd") || strings.Contains(name, "ultra.hd") ||
				strings.Contains(name, "ultra-hd") || strings.Contains(name, "ultra_hd")
			is1080 := strings.Contains(name, "1080p") || strings.Contains(name, "1080i") ||
				strings.Contains(name, "fhd") || strings.Contains(name, "fullhd") ||
				strings.Contains(name, "full.hd") || strings.Contains(name, "full-hd") ||
				strings.Contains(name, "full_hd")
			is720 := strings.Contains(name, "720p") || strings.Contains(name, "720i")
			is480 := strings.Contains(name, "480p") || strings.Contains(name, "480i") ||
				strings.Contains(name, "576p") || strings.Contains(name, "576i") ||
				strings.Contains(name, "dvdrip") || strings.Contains(name, "dvdscr") ||
				strings.Contains(name, ".dvd.") || strings.Contains(name, " dvd ") ||
				strings.Contains(name, "-dvd-")

			var templatePath string
			var width int
			switch {
			case is4K:
				templatePath, createErr = dummyTemplatePath("dummy_2160p.mkv")
				width = 3840
			case is1080:
				templatePath, createErr = dummyTemplatePath("dummy_1080p.mkv")
				width = 1920
			case is720:
				templatePath, createErr = dummyTemplatePath("dummy_720p.mkv")
				width = 1280
			case is480:
				templatePath, createErr = dummyTemplatePath("dummy_480p.mkv")
				width = 720
			default:
				templatePath, createErr = dummyTemplatePath("dummy_1080p.mkv")
				width = 1920 // safe fallback
			}

			if createErr == nil {
				createErr = copyFile(templatePath, cleanPath)
			}

			if createErr != nil {
				log.WithError(createErr).Errorf("Failed to copy dummy video template: %s", cleanPath)
				return createErr
			} else {
				log.Infof("Created dummy video file (%dx%d) for *arr import: %s", width, width*9/16, cleanPath)
			}
		} else {
			createErr = createEmptyFile(cleanPath)
			if createErr != nil {
				log.WithError(createErr).Errorf("Failed to create empty dummy file: %s", cleanPath)
				return createErr
			}
			log.Infof("Created empty dummy file for *arr import: %s", cleanPath)
		}
	}

	// Create marker file
	if file, err := os.Create(markerPath); err == nil {
		file.Close()
	}
	return nil
}

func (s *TransmissionService) removeDummyFiles(ctx context.Context, infoHash string) {
	m, _ := filepath.Glob(filepath.Join(getDownloadsDir(), ".octor_dummy_"+infoHash+"*"))
	for _, f := range m {
		_ = os.Remove(f)
	}

	res, err := s.rm.Get(ctx, []byte(infoHash))
	if err != nil {
		return
	}
	for _, f := range res.Files {
		fPath := strings.Join(f.Path, "/")
		fullPath := filepath.Join(getDownloadsDir(), fPath)

		cleanPath := filepath.Clean(fullPath)
		if !strings.HasPrefix(cleanPath, filepath.Clean(getDownloadsDir())+string(filepath.Separator)) {
			continue
		}

		// Delete file
		_ = os.Remove(cleanPath)

		// Clean up empty parent directories up to downloads dir
		parent := filepath.Dir(cleanPath)
		for parent != filepath.Clean(getDownloadsDir()) && parent != "/" && parent != "." {
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

var (
	arrEmailsLock  sync.Mutex
	whisparrEmails []string
	radarrEmails   []string
	sonarrEmails   []string
	lastArrRead    time.Time
)

func parseDownloadClientUsernames(out []byte) []string {
	seen := map[string]struct{}{}
	var usernames []string
	add := func(username string) {
		username = strings.TrimSpace(username)
		if username == "" {
			return
		}
		key := strings.ToLower(username)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		usernames = append(usernames, username)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var settings struct {
			Username string `json:"username"`
		}
		if err := json.Unmarshal([]byte(line), &settings); err == nil {
			add(settings.Username)
			continue
		}
	}

	re := regexp.MustCompile(`"username"\s*:\s*"([^"]+)"`)
	for _, matches := range re.FindAllStringSubmatch(string(out), -1) {
		if len(matches) > 1 {
			add(matches[1])
		}
	}
	return usernames
}

func getArrConfigDir() string {
	if val := os.Getenv("OCTOR_ARR_CONFIG_DIR"); val != "" {
		return val
	}
	// Try host-based relative location dynamically determined by Octor
	if projectRoot := os.Getenv("PROJECT_ROOT"); projectRoot != "" {
		hostPath := filepath.Join(projectRoot, "../Big ARRS/config")
		if _, err := os.Stat(hostPath); err == nil {
			return hostPath
		}
	}
	// Fallback to absolute paths
	for _, p := range []string{
		"/srv/Big ARRS/config",
		"/downloads/config", // container fallback
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "/srv/Big ARRS/config"
}

func getArrEmails() ([]string, []string, []string) {
	arrEmailsLock.Lock()
	defer arrEmailsLock.Unlock()

	if time.Since(lastArrRead) < 5*time.Minute && (len(whisparrEmails) > 0 || len(radarrEmails) > 0 || len(sonarrEmails) > 0) {
		return whisparrEmails, radarrEmails, sonarrEmails
	}

	extractUsernames := func(dbPath string) []string {
		if _, err := os.Stat(dbPath); err != nil {
			return nil
		}
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			log.Errorf("Failed to open SQLite database %s: %v", dbPath, err)
			return nil
		}
		defer db.Close()

		rows, err := db.Query("SELECT Settings FROM DownloadClients WHERE Implementation='Transmission';")
		if err != nil {
			log.Errorf("Failed to query SQLite database %s: %v", dbPath, err)
			return nil
		}
		defer rows.Close()

		var results []byte
		for rows.Next() {
			var settingsStr string
			if err := rows.Scan(&settingsStr); err == nil {
				results = append(results, []byte(settingsStr+"\n")...)
			}
		}
		return parseDownloadClientUsernames(results)
	}

	arrConfigDir := getArrConfigDir()
	sonarrEmails = extractUsernames(filepath.Join(arrConfigDir, "sonarr/sonarr.db"))
	radarrEmails = extractUsernames(filepath.Join(arrConfigDir, "radarr/radarr.db"))
	whisparrEmails = nil
	for _, dbName := range []string{"whisparr3.db", "whisparr2.db", "whisparr.db"} {
		path := filepath.Join(arrConfigDir, "whisparr", dbName)
		if emails := extractUsernames(path); len(emails) > 0 {
			whisparrEmails = emails
			break
		}
	}

	lastArrRead = time.Now()
	log.Debugf("Arr emails resolved from SQLite: Whisparr=%v, Radarr=%v, Sonarr=%v", whisparrEmails, radarrEmails, sonarrEmails)
	return whisparrEmails, radarrEmails, sonarrEmails
}

func containsUsername(usernames []string, username string) bool {
	username = strings.TrimSpace(username)
	if username == "" {
		return false
	}
	for _, candidate := range usernames {
		if strings.EqualFold(strings.TrimSpace(candidate), username) {
			return true
		}
	}
	return false
}

func isWhisparrRequest(g *gin.Context, wEmails []string) bool {
	ua := strings.ToLower(g.Request.UserAgent())
	if strings.Contains(ua, "whisparr") || strings.Contains(ua, "wishparr") {
		return true
	}
	username, _, _ := g.Request.BasicAuth()
	return containsUsername(wEmails, username)
}

func isRadarrOrSonarrRequest(g *gin.Context, rEmails, sEmails []string) bool {
	ua := strings.ToLower(g.Request.UserAgent())
	if strings.Contains(ua, "radarr") || strings.Contains(ua, "sonarr") {
		return true
	}
	username, _, _ := g.Request.BasicAuth()
	return containsUsername(rEmails, username) || containsUsername(sEmails, username)
}

func (s *TransmissionService) isWhisparrTorrent(ctx context.Context, infoHash string, addedBy string) bool {
	if addedBy == "whisparr" {
		return true
	}
	if addedBy == "arr" {
		return false
	}
	if s.db == nil {
		return false
	}

	wEmails, _, _ := getArrEmails()
	if len(wEmails) == 0 {
		return false
	}

	var exists bool
	_, err := s.db.QueryOneContext(ctx, pg.Scan(&exists), `
		SELECT EXISTS (
			SELECT 1 FROM library l
			JOIN "user" u ON l.user_id = u.user_id
			WHERE l.resource_id = ? AND u.email IN (?)
		)
	`, infoHash, pg.In(wEmails))
	if err == nil {
		return exists
	}
	return false
}

// extractMagnetHash parses a magnet URI and returns the info-hash and display name
// without any DHT or network calls. Supports 40-char hex and 32-char base32 hashes.
func extractMagnetHash(magnet string) (hash, name string) {
	u, err := url.Parse(magnet)
	if err != nil {
		return
	}
	q := u.Query()
	for _, xt := range q["xt"] {
		lower := strings.ToLower(xt)
		if strings.HasPrefix(lower, "urn:btih:") {
			h := xt[9:]
			if len(h) == 40 || len(h) == 32 {
				hash = strings.ToLower(h)
				break
			}
		}
	}
	if dn := q.Get("dn"); dn != "" {
		name = dn
	} else if hash != "" {
		name = hash
	}
	return
	}
