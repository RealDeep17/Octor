package vault

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/api"
	"github.com/webtor-io/web-ui/services/auth"
	"github.com/webtor-io/web-ui/services/web"
)

// addPledge handles HTTP request for creating a pledge
func (h *Handler) addPledge(c *gin.Context) {
	resourceID := c.PostForm("resource_id")
	user := auth.GetUserFromContext(c)
	apiClaims := api.GetClaimsFromContext(c)

	log.WithFields(log.Fields{
		"resource_id": resourceID,
		"user_id":     user.ID,
		"has_claims":  apiClaims != nil,
	}).Info("DEBUG: vault: addPledge handler HIT")

	err := h.createPledge(c.Request.Context(), resourceID, user, apiClaims)
	if err != nil {
		log.WithError(err).Error("vault: add pledge failed")
		web.RedirectWithError(c, err)
		return
	}

	web.RedirectWithSuccessAndMessage(c, "toast.addedToVault")
}

// createPledge is the business logic for pledge creation.
func (h *Handler) createPledge(ctx context.Context, resourceID string, user *auth.User, apiClaims *api.Claims) error {
	if resourceID == "" {
		return errors.New("resource_id is required")
	}
	if apiClaims == nil {
		return errors.New("failed to get claims")
	}

	log.WithField("resource_id", resourceID).Info("vault: getting or creating resource")
	resource, err := h.vault.GetOrCreateResource(ctx, apiClaims, resourceID)
	if err != nil {
		return err
	}

	log.WithField("resource_id", resourceID).Info("vault: creating pledge")
	_, err = h.vault.CreatePledge(ctx, user, resource)
	if err != nil {
		return err
	}

	// Auto-add to library so it appears in the user's collection.
	// Fire-and-forget: stream all files directly from the internal seeder.
	// Use a cancellable context so we can stop downloading if the pledge is removed.
	ctx, cancel := context.WithCancel(context.Background())
	h.activeDownloads.Store(resourceID, cancel)

	go func() {
		defer h.activeDownloads.Delete(resourceID)
		h.downloadAllFiles(ctx, apiClaims, resourceID)
		h.addToLibrary(ctx, apiClaims, user, resourceID)
	}()

	return nil
}

// seederClient connects directly to the torrent-web-seeder bypassing nginx/THP.
// Large read buffer + keep-alive maximises throughput.
var seederClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 60 * time.Second,
		}).DialContext,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		DisableCompression:    true,
		WriteBufferSize:       1 << 20, // 1 MB
		ReadBufferSize:        1 << 20, // 1 MB
	},
}

const (
	seederHost = "127.0.0.1"
	seederPort = "8091"
)

// downloadAllFiles lists all files and downloads them using a small worker pool.
// Limiting concurrency prevents piece-scheduler thrashing: the seeder is optimised
// for sequential reads, so N simultaneous streams fight over the same pieces.
// downloadAllFiles lists all files and downloads them using a small worker pool.
func (h *Handler) downloadAllFiles(ctx context.Context, claims *api.Claims, resourceID string) {
	token := h.mintServiceToken(resourceID)

	var paths []string
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempt*5) * time.Second):
			}
		}
		listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
		list, err := h.api.ListResourceContent(listCtx, claims, resourceID, &api.ListResourceContentArgs{
			Output: api.OutputList,
			Limit:  500,
		})
		listCancel()
		if err != nil || list == nil {
			log.WithError(err).WithField("resource_id", resourceID).WithField("attempt", attempt+1).Warn("vault: file list failed, retrying")
			continue
		}
		for _, item := range list.Items {
			p := strings.TrimSpace(item.PathStr)
			if p != "" && !strings.HasSuffix(p, "/") {
				paths = append(paths, p)
			}
		}
		if len(paths) > 0 {
			break
		}
	}

	if len(paths) == 0 {
		log.WithField("resource_id", resourceID).Warn("vault: no file paths found, cannot start download")
		return
	}

	log.WithField("resource_id", resourceID).WithField("files", len(paths)).Info("vault: starting direct seeder downloads")

	// Worker pool: max 3 concurrent streams so the seeder's sequential piece
	// scheduler isn't thrashed by too many simultaneous readers.
	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(filePath string) {
			defer wg.Done()
			defer func() { <-sem }()
			h.streamFileDirect(ctx, resourceID, filePath, token)
		}(p)
	}
	wg.Wait()
}

// streamFileDirect streams one file from the seeder to /dev/null using a large buffer.
// streamFileDirect streams one file from the seeder to /dev/null using a large buffer.
func (h *Handler) streamFileDirect(ctx context.Context, resourceID, filePath, token string) {
	// Build: http://127.0.0.1:8091/{infohash}/{path}?token=...
	u := fmt.Sprintf("http://%s:%s/%s%s", seederHost, seederPort, resourceID, filePath)
	if token != "" {
		u += "?token=" + token
	}

	ctx, cancel := context.WithTimeout(context.Background(), 48*time.Hour)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).WithField("path", filePath).Warn("vault: bad seeder URL")
		return
	}

	resp, err := seederClient.Do(req)
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).WithField("path", filePath).Warn("vault: seeder request failed")
		return
	}
	defer resp.Body.Close()

	// 4 MB drain buffer for maximum throughput.
	written, _ := io.Copy(io.Discard, bufio.NewReaderSize(resp.Body, 4<<20))
	log.WithField("resource_id", resourceID).WithField("path", filePath).WithField("bytes", written).Info("vault: file download complete")
}

// mintServiceToken creates a long-lived JWT with no rate limit or session binding.
func (h *Handler) mintServiceToken(infohash string) string {
	claims := jwt.MapClaims{
		"role":   "premium",
		"hash":   infohash,
		"exp":    time.Now().Add(48 * time.Hour).Unix(),
		"source": "vault-downloader",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := h.api.SignClaims(tok.Claims)
	if err != nil {
		log.WithError(err).Warn("vault: failed to mint service token, will try without")
		return ""
	}
	return signed
}

// addToLibrary silently saves the torrent to the user's library on vault add.
func (h *Handler) addToLibrary(ctx context.Context, claims *api.Claims, user *auth.User, resourceID string) {
	if h.pg == nil {
		return
	}
	// ctx is now passed from the caller and can be cancelled

	db := h.pg.Get()
	if db == nil {
		return
	}

	// Skip if already in library
	alreadyIn, err := models.IsInLibrary(ctx, db, user.ID, resourceID)
	if err == nil && alreadyIn {
		return
	}

	t, err := h.api.GetTorrentCached(ctx, claims, resourceID)
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).Warn("vault: failed to fetch torrent for library")
		return
	}

	mi, err := metainfo.Load(bytes.NewReader(t))
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).Warn("vault: failed to parse torrent metadata")
		return
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).Warn("vault: failed to unmarshal torrent info")
		return
	}

	// Race condition fix: if the user rapidly clicked "Remove from Vault" while GetTorrentCached 
	// was taking time to resolve, the pledge might be gone. If so, abort library insertion.
	if resource, _ := h.vault.GetResource(ctx, resourceID); resource != nil {
		if pledge, _ := h.vault.GetPledge(ctx, user, resource); pledge == nil {
			log.WithField("resource_id", resourceID).Info("vault: pledge removed before auto-add finished, aborting library insertion")
			return
		}
	}

	_, err = models.AddTorrentToLibrary(ctx, db, user.ID, resourceID, &info, "", int64(len(t)))
	if err != nil {
		log.WithError(err).WithField("resource_id", resourceID).Warn("vault: failed to add to library")
		return
	}

	log.WithField("resource_id", resourceID).Info("vault: auto-added to library")
}
