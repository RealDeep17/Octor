package adultposter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/services/tpdb"
)

type Service struct {
	client   *http.Client
	cacheDir string
}

func New(client *http.Client, cacheDir string) *Service {
	return &Service{
		client:   client,
		cacheDir: cacheDir,
	}
}

var wpThumbnailRegex = regexp.MustCompile(`-\d+x\d+(\.[a-zA-Z0-9]+)$`)

func (s *Service) CleanOriginalImageURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	cleaned := urlStr
	if strings.Contains(cleaned, "/wp-content/uploads/") {
		cleaned = strings.ReplaceAll(cleaned, "/thumbs/", "/")
	}
	cleaned = wpThumbnailRegex.ReplaceAllString(cleaned, "$1")
	if strings.Contains(cleaned, "pics.dmm.co.jp") {
		cleaned = strings.ReplaceAll(cleaned, "ps.jpg", "pl.jpg")
	}
	return cleaned
}

func (s *Service) GetCacheFilePath(urlStr, shape string) string {
	hash := sha256.Sum256([]byte(urlStr + "::" + shape))
	hexHash := hex.EncodeToString(hash[:])
	return filepath.Join(s.cacheDir, hexHash+".jpg")
}

func (s *Service) GetDMMFallbackURLs(urlStr string) []string {
	parts := strings.Split(urlStr, "/")
	if len(parts) == 0 {
		return nil
	}
	filename := strings.ToLower(parts[len(parts)-1])

	rx := regexp.MustCompile(`(?:^|[^a-z0-9])([a-z]{2,8})[^a-z0-9]*(\d{3,6})`)
	m := rx.FindStringSubmatch(filename)

	if len(m) < 3 {
		m = rx.FindStringSubmatch(strings.ToLower(urlStr))
	}

	if len(m) < 3 {
		return nil
	}

	prefix := m[1]
	numStr := m[2]

	var num int
	fmt.Sscanf(numStr, "%d", &num)

	cids := []string{
		fmt.Sprintf("%s%d", prefix, num),
		fmt.Sprintf("%s%03d", prefix, num),
		fmt.Sprintf("%s%05d", prefix, num),
		fmt.Sprintf("1%s%05d", prefix, num),
		fmt.Sprintf("30%s%05d", prefix, num),
		fmt.Sprintf("h_1143%s%05d", prefix, num),
		fmt.Sprintf("h_1143%s%03d", prefix, num),
	}

	var urls []string
	seen := make(map[string]bool)
	for _, cid := range cids {
		if seen[cid] {
			continue
		}
		seen[cid] = true
		urls = append(urls,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/movie/adult/%s/%spl.jpg", cid, cid),
			fmt.Sprintf("https://pics.dmm.co.jp/digital/video/%s/%spl.jpg", cid, cid),
		)
	}
	return urls
}

func (s *Service) WarmSinglePoster(urlStr, destPath, shape string, isJav bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resp *http.Response
	var err error

	cl := *s.client
	cl.Timeout = 10 * time.Second
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	if isJav {
		fallbacks := s.GetDMMFallbackURLs(urlStr)
		for _, fallbackURL := range fallbacks {
			var req *http.Request
			req, err = http.NewRequestWithContext(ctx, "GET", fallbackURL, nil)
			if err == nil {
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
				resp, err = cl.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					break
				}
				if resp != nil {
					resp.Body.Close()
					resp = nil
				}
			}
		}
	}

	if (resp == nil || resp.StatusCode != http.StatusOK) && isJav {
		ddgURL := fmt.Sprintf("https://external-content.duckduckgo.com/iu/?u=%s", url.QueryEscape(urlStr))
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "GET", ddgURL, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
			resp, err = cl.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				// Success via DDG proxy!
			} else if resp != nil {
				resp.Body.Close()
				resp = nil
			}
		}
	}

	if resp == nil || resp.StatusCode != http.StatusOK {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", "https://jav.guru/")
		resp, err = cl.Do(req)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download original image, status: %d", resp.StatusCode)
	}

	srcImg, err := imaging.Decode(resp.Body)
	if err != nil {
		return err
	}

	bounds := srcImg.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var processed image.Image
	if isJav && shape == "portrait" {
		if w > h {
			urlLower := strings.ToLower(urlStr)
			isJavJacket := strings.Contains(urlLower, "dmm.co.jp") ||
				strings.Contains(urlLower, "jav.guru") ||
				strings.Contains(urlLower, "javmiku") ||
				strings.Contains(urlLower, "javnorth") ||
				strings.Contains(urlLower, "pl.jpg")

			if isJavJacket {
				processed = imaging.Crop(srcImg, image.Rect(w/2, 0, w, h))
				processed = imaging.Resize(processed, 500, 0, imaging.Linear)
			} else {
				processed = imaging.Fill(srcImg, 500, 750, imaging.Center, imaging.Linear)
			}
		} else {
			processed = imaging.Resize(srcImg, 500, 0, imaging.Linear)
		}
	} else {
		if shape == "portrait" {
			if w > h {
				processed = imaging.Fill(srcImg, 500, 750, imaging.Center, imaging.Linear)
			} else {
				processed = imaging.Resize(srcImg, 500, 0, imaging.Linear)
			}
		} else {
			processed = imaging.Resize(srcImg, 900, 0, imaging.Linear)
		}
	}

	_ = os.MkdirAll(s.cacheDir, 0755)
	tempPath := destPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
		_ = os.Remove(tempPath)
	}()

	err = imaging.Encode(out, processed, imaging.JPEG, imaging.JPEGQuality(85))
	if err != nil {
		return err
	}

	_ = out.Close()
	return os.Rename(tempPath, destPath)
}

func (s *Service) PreWarmPosters(scenes []tpdb.TpdbScene, isJav bool) {
	_ = os.MkdirAll(s.cacheDir, 0755)

	for _, scene := range scenes {
		originalPoster := s.CleanOriginalImageURL(scene.Poster)
		if originalPoster == "" {
			originalPoster = s.CleanOriginalImageURL(scene.PosterImage)
		}
		if originalPoster == "" {
			originalPoster = s.CleanOriginalImageURL(scene.Image)
		}
		if originalPoster == "" {
			continue
		}

		filePath := s.GetCacheFilePath(originalPoster, "portrait")

		if _, err := os.Stat(filePath); err == nil {
			continue
		}

		log.Infof("Pre-warming adult discovery poster for scene: %s", scene.Title)
		err := s.WarmSinglePoster(originalPoster, filePath, "portrait", isJav)
		if err != nil {
			log.WithError(err).Warnf("failed to pre-warm adult poster for: %s", originalPoster)
		}
	}
}

func (s *Service) PruneAdultPosters(days int) error {
	if days <= 0 {
		days = 7
	}
	thresholdDuration := time.Duration(days) * 24 * time.Hour
	cutoff := time.Now().Add(-thresholdDuration)

	dirEntries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	prunedCount := 0
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(s.cacheDir, entry.Name())
			err = os.Remove(filePath)
			if err == nil {
				prunedCount++
			}
		}
	}

	if prunedCount > 0 {
		log.Infof("Pruned %d expired adult discovery posters from local disk cache", prunedCount)
	}
	return nil
}
