package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	csrf "github.com/utrack/gin-csrf"
	ra "github.com/webtor-io/rest-api/services"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/i18n"
	vault "github.com/webtor-io/web-ui/services/vault"

	vaultModels "github.com/webtor-io/web-ui/models/vault"
)

// TorrentStatus represents the current combined status of a torrent.
type TorrentStatus struct {
	State          string  `json:"state"`    // idle, caching, cached, vaulting, vaulted
	Progress       float64 `json:"progress"` // 0-100 for caching/vaulting
	Seeders        int     `json:"seeders"`  // seed count for caching
	Label          string  `json:"label"`    // translated state label
	SpeedBytes     int64   `json:"speed_bytes"`
	RemainingBytes int64   `json:"remaining_bytes"`
	ETASeconds     int64   `json:"eta_seconds"`
	TotalStr       string  `json:"total_str,omitempty"`
	CompletedStr   string  `json:"completed_str,omitempty"`
	RemainingStr   string  `json:"remaining_str,omitempty"`
	Detail         string  `json:"detail,omitempty"`
	Selective      bool    `json:"selective"`
}

// TorrentStatsData holds the relevant fields from a torrent stats event.
type TorrentStatsData struct {
	Total          int64
	Completed      int64
	Seeders        int
	SpeedBytes     int64
	RemainingBytes int64
	ETASeconds     int64
	Cached         bool
}

// resolveStatus is a pure function that determines the combined torrent status
// from vault DB state, vault API state, and torrent seeding stats.
// Priority: vaulted > vaulting > cached > caching > idle.
func resolveStatus(dbResource *vaultModels.Resource, apiResource *vault.Resource, stats *TorrentStatsData, vaultSpeedBytes int64) *TorrentStatus {
	vaultState := resolveVaultState(dbResource, apiResource)
	cachingState := resolveCachingState(stats)

	if vaultState.State == "vaulted" {
		return vaultState
	}
	if vaultState.State == "vaulting" || vaultState.State == "waiting" {
		if stats != nil {
			vaultState.Seeders = stats.Seeders
		}
		if apiResource != nil && apiResource.Status == vault.StatusFailed {
			vaultState.SpeedBytes = 0
			vaultState.ETASeconds = 0
			if apiResource.Error != "" {
				vaultState.Detail = fmt.Sprintf("Error: %s (retrying...)", apiResource.Error)
			} else {
				vaultState.Detail = "Error: storage failed (retrying...)"
			}
		} else {
			vaultState.SpeedBytes = vaultSpeedBytes
			if vaultSpeedBytes > 0 && vaultState.RemainingBytes > 0 {
				vaultState.ETASeconds = int64(math.Ceil(float64(vaultState.RemainingBytes) / float64(vaultSpeedBytes)))
			} else {
				vaultState.ETASeconds = 0
			}
			vaultState.Detail = buildStatusDetail(&TorrentStatsData{
				SpeedBytes:     vaultState.SpeedBytes,
				RemainingBytes: vaultState.RemainingBytes,
				ETASeconds:     vaultState.ETASeconds,
			})
		}
		return vaultState
	}
	if cachingState.State == "cached" {
		return cachingState
	}
	if cachingState.State == "caching" {
		return cachingState
	}
	return &TorrentStatus{State: "idle"}
}

func resolveVaultState(dbResource *vaultModels.Resource, apiResource *vault.Resource) *TorrentStatus {
	if dbResource == nil {
		return &TorrentStatus{State: "idle"}
	}
	selective := len(dbResource.SelectedFiles) > 0
	if dbResource.Vaulted {
		return &TorrentStatus{State: "vaulted", Selective: selective}
	}
	if !dbResource.Funded {
		return &TorrentStatus{State: "idle"}
	}
	// Funded but not vaulted — check API for progress
	if apiResource == nil {
		return &TorrentStatus{State: "vaulting", Progress: 0, Selective: selective}
	}
	status := &TorrentStatus{
		State:     "vaulting",
		Progress:  apiResource.GetProgress(),
		Selective: selective,
	}
	if apiResource.TotalSize > 0 {
		status.TotalStr = formatBytes(apiResource.TotalSize)
		status.CompletedStr = formatBytes(apiResource.StoredSize)
		status.RemainingBytes = apiResource.TotalSize - apiResource.StoredSize
		if status.RemainingBytes < 0 {
			status.RemainingBytes = 0
		}
		status.RemainingStr = formatBytes(status.RemainingBytes)
		if status.RemainingBytes > 0 {
			status.Detail = fmt.Sprintf("%s left", status.RemainingStr)
		}
	}
	switch apiResource.Status {
	case vault.StatusProcessing:
		if apiResource.ClaimExpiresAt != nil && apiResource.ClaimExpiresAt.Before(time.Now()) {
			status.State = "waiting"
			status.Progress = apiResource.GetProgress()
			return status
		}
		return status
	case vault.StatusCompleted:
		return &TorrentStatus{State: "vaulted", Selective: selective}
	case vault.StatusQueued:
		status.State = "waiting"
		status.Progress = 0
		return status
	case vault.StatusFailed:
		if apiResource.Error != "" {
			status.Detail = fmt.Sprintf("Error: %s (retrying...)", apiResource.Error)
		} else {
			status.Detail = "Error: storage failed (retrying...)"
		}
		return status
	default:
		// Failed or unknown — still funded, system will retry
		return status
	}
}

func resolveCachingState(stats *TorrentStatsData) *TorrentStatus {
	if stats == nil {
		return &TorrentStatus{State: "idle"}
	}
	if stats.Cached {
		status := &TorrentStatus{State: "cached", Progress: 100, Seeders: stats.Seeders}
		if stats.Total > 0 {
			status.TotalStr = formatBytes(stats.Total)
			status.CompletedStr = formatBytes(stats.Total)
		}
		return status
	}
	if stats.Total <= 0 {
		return &TorrentStatus{State: "idle"}
	}
	// If nothing has been downloaded yet, treat as caching 0% with details
	if stats.Completed <= 0 {
		return &TorrentStatus{
			State:    "caching",
			Progress: 0,
			Seeders:  stats.Seeders,
			Detail:   buildStatusDetail(stats),
		}
	}
	progress := float64(stats.Completed) / float64(stats.Total) * 100
	if progress >= 100 {
		return &TorrentStatus{State: "cached", Progress: 100, Seeders: stats.Seeders}
	}
	return &TorrentStatus{
		State:          "caching",
		Progress:       progress,
		Seeders:        stats.Seeders,
		SpeedBytes:     stats.SpeedBytes,
		RemainingBytes: stats.RemainingBytes,
		ETASeconds:     stats.ETASeconds,
		TotalStr:       formatBytes(stats.Total),
		CompletedStr:   formatBytes(stats.Completed),
		RemainingStr:   formatBytes(stats.RemainingBytes),
		Detail:         buildStatusDetail(stats),
	}
}

func buildStatusDetail(stats *TorrentStatsData) string {
	if stats == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if stats.SpeedBytes > 0 {
		parts = append(parts, fmt.Sprintf("%s/s", formatBytes(stats.SpeedBytes)))
	}
	if stats.RemainingBytes > 0 {
		parts = append(parts, fmt.Sprintf("%s left", formatBytes(stats.RemainingBytes)))
	}
	if stats.ETASeconds > 0 {
		parts = append(parts, fmt.Sprintf("ETA %s", formatETA(stats.ETASeconds)))
	}
	return strings.Join(parts, " • ")
}

func formatBytes(v int64) string {
	if v <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(v)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", int64(value), units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func formatETA(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	d := time.Duration(seconds) * time.Second
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", int(d.Hours()/24), int(d.Hours())%24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// prepareInitialStatus computes the initial status for SSR (vault DB only, no SSE connection).
func (s *Handler) prepareInitialStatus(ctx context.Context, resourceID string) *TorrentStatus {
	if s.vault == nil {
		return &TorrentStatus{State: "idle"}
	}
	dbResource, err := s.vault.GetResource(ctx, resourceID)
	if err != nil {
		log.WithError(err).Warn("failed to get vault resource for initial status")
		return &TorrentStatus{State: "idle"}
	}
	return resolveStatus(dbResource, nil, nil, 0)
}

// status is the SSE endpoint handler for real-time torrent status updates.
// Uses c.Stream() + c.SSEvent() like the job handler for proper proxy compatibility.
// All computation happens in a background goroutine; the callback only reads from a channel.
func (s *Handler) status(c *gin.Context) {
	// Validate CSRF token from query parameter (EventSource doesn't support custom headers).
	token := c.Query("_csrf")
	if token == "" || token != csrf.GetToken(c) {
		c.String(http.StatusForbidden, "CSRF token mismatch")
		return
	}
	// Strip _csrf from the URL so it is not persisted in access logs,
	// proxy logs, or browser history. We've already consumed it above.
	q := c.Request.URL.Query()
	q.Del("_csrf")
	c.Request.URL.RawQuery = q.Encode()

	resourceID := c.Param("resource_id")
	active := c.Query("active") == "1"
	itemID := c.Query("item_id")
	claims := api.GetClaimsFromContext(c)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache,no-store,no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// Channel for status updates from background goroutine
	statusCh := make(chan *TorrentStatus, 10)

	go s.statusLoop(ctx, claims, resourceID, itemID, active, statusCh)

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(1 * time.Second)
		select {
		case <-ctx.Done():
			ticker.Stop()
			return false
		case <-ticker.C:
			c.SSEvent("ping", "")
			return true
		case status, ok := <-statusCh:
			if !ok {
				return false
			}
			stateKey := status.State
			if status.Selective && (status.State == "vaulted" || status.State == "vaulting") {
				stateKey = status.State + "Selective"
			}
			status.Label = i18n.TranslateWithLocalizer(i18n.GetLocalizer(c), "resource.status."+stateKey)
			c.SSEvent("message", status)
			return status.State != "vaulted"
		}
	})
}

type SpeedWindow struct {
	samples []int64
	maxSize int
}

func NewSpeedWindow(maxSize int) *SpeedWindow {
	return &SpeedWindow{
		samples: make([]int64, 0, maxSize),
		maxSize: maxSize,
	}
}

func (w *SpeedWindow) Add(sample int64) {
	if len(w.samples) >= w.maxSize {
		w.samples = w.samples[1:]
	}
	w.samples = append(w.samples, sample)
}

func (w *SpeedWindow) Average() int64 {
	if len(w.samples) == 0 {
		return 0
	}
	var sum int64
	for _, s := range w.samples {
		sum += s
	}
	return sum / int64(len(w.samples))
}

// statusLoop runs in a background goroutine, computing status updates and sending them to the channel.
func (s *Handler) statusLoop(ctx context.Context, claims *api.Claims, resourceID string, itemID string, active bool, out chan<- *TorrentStatus) {
	defer close(out)

	var statsCh <-chan api.EventData
	var lastStats *TorrentStatsData
	var passiveStats *TorrentStatsData
	var lastEventAt time.Time
	var lastPassiveCheck time.Time
	var lastJSON string
	var lastDBResource *vaultModels.Resource
	var lastAPIResource *vault.Resource
	var lastVaultStored int64
	var lastVaultAt time.Time
	var lastVaultSpeedBytes int64

	downloadSpeedWindow := NewSpeedWindow(15)
	uploadSpeedWindow := NewSpeedWindow(15)
	completedPieces := make(map[int]bool)
	var maxCompletedSeen int64
	var lastMonotonicCompleted int64

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	type statsResult struct {
		ch    <-chan api.EventData
		msg   string
		stats *TorrentStatsData
	}
	statsChResult := make(chan statsResult, 1)

	if active {
		go func() {
			ch, msg, stats := s.tryConnectStats(ctx, claims, resourceID, itemID)
			statsChResult <- statsResult{ch: ch, msg: msg, stats: stats}
		}()
	}

	var lastVaultCheck time.Time
	loadVaultState := func(force bool) {
		if s.vault == nil {
			return
		}
		if !force && time.Since(lastVaultCheck) < 2*time.Second {
			return
		}
		lastVaultCheck = time.Now()
		var err error
		dbRes, err := s.vault.GetResource(ctx, resourceID)
		if err != nil {
			if ctx.Err() == nil {
				log.WithError(err).Warn("failed to get vault resource for status")
			}
			lastDBResource = nil
			lastAPIResource = nil
			return
		}
		lastDBResource = dbRes
		lastAPIResource = nil
		if lastDBResource != nil && lastDBResource.Funded && !lastDBResource.Vaulted {
			apiRes, err := s.vault.GetVaultAPIResource(ctx, resourceID)
			if err != nil {
				if ctx.Err() == nil {
					log.WithError(err).Warn("failed to get vault api resource for status")
				}
			} else {
				lastAPIResource = apiRes
				if lastAPIResource != nil && lastAPIResource.TotalSize > 0 {
					now := time.Now()
					if lastVaultAt.IsZero() || lastAPIResource.StoredSize < lastVaultStored {
						lastVaultStored = lastAPIResource.StoredSize
						lastVaultAt = now
						lastVaultSpeedBytes = 0
						uploadSpeedWindow = NewSpeedWindow(15)
					} else {
						deltaSeconds := now.Sub(lastVaultAt).Seconds()
						if deltaSeconds > 0 {
							deltaBytes := lastAPIResource.StoredSize - lastVaultStored
							if deltaBytes < 0 {
								deltaBytes = 0
							}
							instant := int64(math.Round(float64(deltaBytes) / deltaSeconds))
							uploadSpeedWindow.Add(instant)
							lastVaultSpeedBytes = uploadSpeedWindow.Average()
							lastVaultStored = lastAPIResource.StoredSize
							lastVaultAt = now
						}
					}
				}
			}
		}
	}

	loadPassiveCacheState := func(force bool) {
		if !force && time.Since(lastPassiveCheck) < 2*time.Second {
			return
		}
		lastPassiveCheck = time.Now()
		stats, err := s.getPassiveCacheStats(ctx, claims, resourceID, itemID)
		if err != nil {
			if ctx.Err() == nil {
				log.WithError(err).WithField("resourceID", resourceID).Warn("failed to get passive cache status")
			}
			return
		}
		passiveStats = stats
	}

	sendStatus := func() bool {
		stats := passiveStats
		if lastStats != nil {
			stats = lastStats
		}
		status := resolveStatus(lastDBResource, lastAPIResource, stats, lastVaultSpeedBytes)
		data, _ := json.Marshal(status)
		jsonStr := string(data)
		if jsonStr == lastJSON {
			return true
		}
		lastJSON = jsonStr
		select {
		case out <- status:
		case <-ctx.Done():
			return false
		}
		return status.State != "vaulted"
	}

	loadVaultState(true)
	loadPassiveCacheState(true)
	if !sendStatus() {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case res := <-statsChResult:
			statsCh = res.ch
			log.WithField("resourceID", resourceID).WithField("connected", res.ch != nil).WithField("msg", res.msg).Info("status: stats connection result")
			if res.stats != nil {
				lastStats = res.stats
				maxCompletedSeen = lastStats.Completed
				lastMonotonicCompleted = lastStats.Completed
			}
			if !sendStatus() {
				return
			}

		case ev, ok := <-statsCh:
			if ok {
				completed := int64(ev.Completed)
				if len(ev.Pieces) > 0 {
					for _, p := range ev.Pieces {
						if p.Complete {
							completedPieces[int(p.Position)] = true
						}
					}
					ratio := float64(len(completedPieces)) / float64(len(ev.Pieces))
					completed = int64(math.Round(ratio * float64(ev.Total)))
				}
				if completed < maxCompletedSeen {
					completed = maxCompletedSeen
				} else {
					maxCompletedSeen = completed
				}

				now := time.Now()
				speedBytes := int64(0)
				if lastStats != nil && !lastEventAt.IsZero() {
					deltaSeconds := now.Sub(lastEventAt).Seconds()
					if deltaSeconds > 0 {
						deltaBytes := completed - lastMonotonicCompleted
						if deltaBytes < 0 {
							deltaBytes = 0
						}
						instant := int64(math.Round(float64(deltaBytes) / deltaSeconds))
						downloadSpeedWindow.Add(instant)
						speedBytes = downloadSpeedWindow.Average()
						lastEventAt = now
					} else {
						speedBytes = downloadSpeedWindow.Average()
					}
				} else {
					lastEventAt = now
				}
				lastMonotonicCompleted = completed

				remainingBytes := ev.Total - completed
				if remainingBytes < 0 {
					remainingBytes = 0
				}
				etaSeconds := int64(0)
				if speedBytes > 0 && remainingBytes > 0 {
					etaSeconds = int64(math.Ceil(float64(remainingBytes) / float64(speedBytes)))
				}
				seeders := ev.Seeders
				if seeders <= 0 {
					seeders = ev.Peers
				}
				lastStats = &TorrentStatsData{
					Total:          ev.Total,
					Completed:      completed,
					Seeders:        seeders,
					SpeedBytes:     speedBytes,
					RemainingBytes: remainingBytes,
					ETASeconds:     etaSeconds,
				}
				log.WithField("resourceID", resourceID).WithField("completed", completed).WithField("total", ev.Total).WithField("peers", ev.Peers).Info("status: got stats event")
			} else {
				log.WithField("resourceID", resourceID).Warn("status: stats channel closed")
				lastStats = nil
				statsCh = nil
			}
			if !sendStatus() {
				return
			}

		case <-ticker.C:
			loadVaultState(false)
			loadPassiveCacheState(false)
			if !sendStatus() {
				return
			}
		}
	}
}

func cachedStatsFromExport(exportResp *ra.ExportResponse) *TorrentStatsData {
	if exportResp == nil {
		return &TorrentStatsData{Cached: true}
	}
	size := exportResp.Source.Size
	return &TorrentStatsData{
		Total:     size,
		Completed: size,
		Cached:    true,
	}
}

// getPassiveCacheStats checks cache/vault availability via rest-api export metadata.
// This follows the rest-api `done=true` path and must not open torrent stats.
func (s *Handler) resolveStatusItemID(ctx context.Context, claims *api.Claims, resourceID string, itemID string) (string, error) {
	if itemID != "" {
		return itemID, nil
	}
	list, err := s.api.ListResourceContentCached(ctx, claims, resourceID, &api.ListResourceContentArgs{
		Output: api.OutputList,
		Limit:  1,
	})
	if err != nil {
		return "", err
	}
	if list == nil || list.ID == "" {
		return "", nil
	}
	return list.ID, nil
}

func (s *Handler) getPassiveCacheStats(ctx context.Context, claims *api.Claims, resourceID string, itemID string) (*TorrentStatsData, error) {
	connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	statusItemID, err := s.resolveStatusItemID(connCtx, claims, resourceID, itemID)
	if err != nil {
		return nil, err
	}
	if statusItemID == "" {
		return nil, nil
	}
	exportResp, err := s.api.ExportResourceContent(connCtx, claims, resourceID, statusItemID, "")
	if err != nil {
		return nil, err
	}
	statItem, ok := exportResp.ExportItems["torrent_client_stat"]
	if !ok || statItem.URL == "" {
		return cachedStatsFromExport(exportResp), nil
	}
	return nil, nil
}

// tryConnectStats establishes an active SSE connection to torrent-http-proxy
// for real-time torrent stats. It is only called after an explicit user action
// asks the status endpoint for active cache status.
func (s *Handler) tryConnectStats(ctx context.Context, claims *api.Claims, resourceID string, itemID string) (<-chan api.EventData, string, *TorrentStatsData) {
	connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var statusItemID string
	var err error
	for i := 0; i < 3; i++ {
		statusItemID, err = s.resolveStatusItemID(connCtx, claims, resourceID, itemID)
		if err == nil && statusItemID != "" {
			break
		}
		if i < 2 {
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		msg := fmt.Sprintf("list failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg, nil
	}

	if statusItemID == "" {
		return nil, "empty item ID", nil
	}

	exportResp, err := s.api.ExportResourceContent(connCtx, claims, resourceID, statusItemID, "")
	if err != nil {
		msg := fmt.Sprintf("export failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg, nil
	}

	statItem, ok := exportResp.ExportItems["torrent_client_stat"]
	if !ok || statItem.URL == "" {
		return nil, "cached", cachedStatsFromExport(exportResp)
	}

	log.WithField("resourceID", resourceID).WithField("url", statItem.URL[:min(len(statItem.URL), 80)]).Info("status: connecting to active stats SSE")

	ch, err := s.api.Stats(ctx, statItem.URL)
	if err != nil {
		if err.Error() == "cached" {
			return nil, "cached", cachedStatsFromExport(exportResp)
		}
		msg := fmt.Sprintf("stats SSE failed: %v", err)
		log.WithError(err).WithField("resourceID", resourceID).Warn("status: " + msg)
		return nil, msg, nil
	}

	log.WithField("resourceID", resourceID).Info("status: connected to active torrent stats SSE")
	return ch, "connected", nil
}
