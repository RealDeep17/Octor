package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/sidecar/internal/cache"
	"github.com/webtor-io/sidecar/internal/config"
	"github.com/webtor-io/sidecar/internal/detect"
	"github.com/webtor-io/sidecar/internal/enrich"
	"github.com/webtor-io/sidecar/internal/scene"
	"github.com/webtor-io/sidecar/internal/settings"
	"github.com/webtor-io/sidecar/internal/stashdb"
	"github.com/webtor-io/sidecar/internal/tpdb"
)

type Handler struct {
	cfg       *config.Config
	tpdbCl    *tpdb.Client
	stashdbCl *stashdb.Client
}

var rxAlphanumeric = regexp.MustCompile(`[^a-z0-9]`)

func NewHandler(cfg *config.Config, tpdbCl *tpdb.Client, stashdbCl *stashdb.Client) *Handler {
	return &Handler{
		cfg:       cfg,
		tpdbCl:    tpdbCl,
		stashdbCl: stashdbCl,
	}
}

func (h *Handler) Metadata(c *gin.Context) {
	t := c.Query("t")
	i := c.Query("i")
	sidecarEnrichmentEnabled := c.Query("sidecar_enrichment_enabled")
	porn := c.Query("porn")

	var duration *float64
	if durStr := c.Query("duration"); durStr != "" {
		if d, err := strconv.ParseFloat(durStr, 64); err == nil {
			duration = &d
		}
	}

	if t == "" && i == "" {
		c.JSON(http.StatusBadRequest, gin.H{"Response": "False", "Error": "No title or ID"})
		return
	}

	// 1. Resolve settings
	s, err := settings.Load(h.cfg.SidecarDataDir, h.cfg.SidecarEnrichmentEnabled)
	sidecarEnabled := true
	if err == nil {
		sidecarEnabled = s.SidecarEnrichmentEnabled
	}
	if sidecarEnrichmentEnabled != "" {
		sidecarEnabled = strings.ToLower(sidecarEnrichmentEnabled) == "true"
	}

	if !sidecarEnabled {
		log.Infof("Sidecar enrichment disabled, skipping lookup")
		c.JSON(http.StatusNotFound, gin.H{"Response": "False", "Error": "Movie not found!"})
		return
	}

	// 2. Cache check
	cacheKey := fmt.Sprintf("%s|%s|", t, i)
	if duration != nil {
		cacheKey += strconv.FormatFloat(*duration, 'f', -1, 64)
	}
	if cachedVal, found := cache.Cache.Get(cacheKey); found {
		log.Infof("Cache hit for %q", cacheKey)
		c.JSON(http.StatusOK, cachedVal)
		return
	}

	// 3. ID Lookup Path
	if i != "" {
		normalizedID := i
		if strings.HasPrefix(normalizedID, "tpdb=") {
			normalizedID = "tpdb:" + normalizedID[5:]
		} else if strings.HasPrefix(normalizedID, "tpdb_jav=") {
			normalizedID = "tpdb_jav:" + normalizedID[9:]
		} else if strings.HasPrefix(normalizedID, "stash=") {
			normalizedID = "stash:" + normalizedID[6:]
		}

		if strings.HasPrefix(normalizedID, "tpdb:") {
			sceneID := normalizedID[5:]
			res, err := h.tpdbCl.GetByID(sceneID)
			if err == nil && res != nil {
				omdbResp := ToOmdb(*res)
				cache.Cache.Add(cacheKey, &omdbResp)
				c.JSON(http.StatusOK, omdbResp)
				return
			}
		} else if strings.HasPrefix(normalizedID, "tpdb_jav:") {
			sceneID := normalizedID[9:]
			var res *scene.Scene
			if strings.HasPrefix(sceneID, "fallback_") {
				cleanCode := sceneID[9:]
				results, err := h.tpdbCl.JAVSearch(cleanCode, 5)
				if err == nil && len(results) > 0 {
					res = &results[0]
				}
			} else {
				res, err = h.tpdbCl.JAVGetByID(sceneID)
				if err != nil || res == nil {
					results, err := h.tpdbCl.JAVSearch(sceneID, 5)
					if err == nil && len(results) > 0 {
						res = &results[0]
					}
				}
			}
			if res != nil {
				omdbResp := ToOmdb(*res)
				cache.Cache.Add(cacheKey, &omdbResp)
				c.JSON(http.StatusOK, omdbResp)
				return
			}
		} else if strings.HasPrefix(normalizedID, "stash:") {
			sceneID := normalizedID[6:]
			res, err := h.stashdbCl.GetByID(sceneID)
			if err == nil && res != nil {
				omdbResp := ToOmdb(*res)
				cache.Cache.Add(cacheKey, &omdbResp)
				c.JSON(http.StatusOK, omdbResp)
				return
			}
		}
	}

	// 4. JAV fast-path by title
	javCode, isJav := detect.ExtractJAVCode(t)
	if t != "" && isJav {
		log.Infof("JAV title fast-path for code: %s", javCode)
		results, err := h.tpdbCl.JAVSearch(javCode, 5)
		if err == nil && len(results) > 0 {
			var best *scene.Scene
			codeClean := rxAlphanumeric.ReplaceAllString(strings.ToLower(javCode), "")
			for _, r := range results {
				extClean := rxAlphanumeric.ReplaceAllString(strings.ToLower(r.ExternalID), "")
				if extClean != "" && (extClean == codeClean || strings.Contains(codeClean, extClean) || strings.Contains(extClean, codeClean)) {
					best = &r
					break
				}
			}
			if best == nil {
				best = &results[0]
			}
			omdbResp := ToOmdb(*best)
			cache.Cache.Add(cacheKey, &omdbResp)
			c.JSON(http.StatusOK, omdbResp)
			return
		}
		log.Warnf("JAV lookup missed for %s, falling through to scene search", javCode)
	}

	// 5. Western adult scene path
	isAdult := strings.ToLower(porn) == "true" || (t != "" && (detect.IsAdultContent(t) || detect.IsJAV(t)))
	if t != "" && isAdult {
		log.Infof("Adult content detected, attempting enrichment for: %s", t)
		res, score := enrich.AdultEnrichmentLookup(c.Request.Context(), t, duration, h.tpdbCl, h.stashdbCl)
		if res != nil && score >= 100.0 {
			log.Infof("Best match overall: %q score=%.1f (source: %s)", res.Title, score, res.Source)
			omdbResp := ToOmdb(*res)
			cache.Cache.Add(cacheKey, &omdbResp)
			c.JSON(http.StatusOK, omdbResp)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"Response": "False", "Error": "Movie not found!"})
}

func (h *Handler) GetSettings(c *gin.Context) {
	s, err := settings.Load(h.cfg.SidecarDataDir, h.cfg.SidecarEnrichmentEnabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *Handler) PostSettings(c *gin.Context) {
	enabledStr := c.PostForm("sidecar_enrichment_enabled")
	if enabledStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Missing sidecar_enrichment_enabled form parameter"})
		return
	}

	enabled := strings.ToLower(enabledStr) == "true"
	s := &settings.Settings{SidecarEnrichmentEnabled: enabled}

	if err := settings.Save(h.cfg.SidecarDataDir, s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "sidecar_enrichment_enabled": enabled})
}
