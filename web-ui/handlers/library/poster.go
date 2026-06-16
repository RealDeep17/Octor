package library

import (
	"bytes"
	"context"
	"time"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/webtor-io/web-ui/models"
)

type PosterFormat string

const (
	PosterFormatJPEG PosterFormat = "jpg"
)

const (
	PosterJPEGQuality = 85
)

// resizeSemaphore restricts the number of concurrent image resizing operations.
var resizeSemaphore = make(chan struct{}, 8)

// errPosterNotFound is returned when no poster URL can be resolved for the
// requested video — distinct from real internal errors (db down, S3 cache
// failure, image decode failure). The HTTP layer maps it to 404 so absent
// posters don't pollute the 5xx error budget.
var errPosterNotFound = errors.New("poster not found")

type PosterArgs struct {
	t          models.ContentType
	imdbID     string
	width      int
	format     PosterFormat
	horizontal bool
}

func (s *Handler) bindPosterArgs(c *gin.Context, horizontal bool) (*PosterArgs, error) {
	t := models.ContentType(c.Param("type"))
	if t != models.ContentTypeSeries && t != models.ContentTypeMovie {
		return nil, errors.Errorf("wrong video type %v", t)
	}
	file := c.Param("file")
	fileParts := strings.Split(file, ".")
	if len(fileParts) != 2 {
		return nil, errors.Errorf("wrong file format %v", file)
	}
	width, err := strconv.Atoi(fileParts[0])
	if err != nil {
		return nil, errors.Errorf("wrong width %v", width)
	}
	f := PosterFormat(fileParts[1])
	if f != PosterFormatJPEG {
		return nil, errors.Errorf("wrong format %v", f)
	}
	return &PosterArgs{
		t:          t,
		imdbID:     c.Param("imdb_id"),
		width:      width,
		format:     f,
		horizontal: horizontal,
	}, nil
}

func (s *Handler) poster(c *gin.Context) {
	s.handlePoster(c, false)
}

func (s *Handler) posterHorizontal(c *gin.Context) {
	s.handlePoster(c, true)
}

func (s *Handler) handlePoster(c *gin.Context, horizontal bool) {

	pa, err := s.bindPosterArgs(c, horizontal)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "failed to bind poster args"))
		return
	}

	ctx := c.Request.Context()

	db := s.pg.Get()
	if db == nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.New("no db"))
		return
	}

	var b *bytes.Buffer

	// 1. Try S3 cache if configured
	if s.s3Cl != nil && s.posterCacheS3Bucket != "" {
		cl := s.s3Cl.Get()
		cacheCtx, cacheCancel := context.WithTimeout(ctx, 1*time.Second)
		b, err = s.getPosterFromCache(cacheCtx, cl, pa)
		cacheCancel()
		if err != nil {
			log.WithError(err).Warn("poster: S3 cache get failed, falling through")
		} else if b != nil {
			// Verify aspect ratio of the cached image to prevent vertical/horizontal mismatch
			cfg, _, decodeErr := image.DecodeConfig(bytes.NewReader(b.Bytes()))
			if decodeErr == nil {
				isCachedHorizontal := cfg.Width > cfg.Height
				var hasOnlyHorizontal bool
				md, _ := s.getPosterMetadata(ctx, db, pa.t, pa.imdbID)
				if md != nil {
					isAdult := strings.Contains(md.PosterURL, "theporndb.net") ||
						strings.Contains(md.PosterURL, "stashdb.org") ||
						strings.HasPrefix(md.VideoID, "tpdb:") ||
						strings.HasPrefix(md.VideoID, "tpdb=") ||
						isJavVideoID(md.VideoID) ||
						strings.HasPrefix(md.VideoID, "stash:") ||
						strings.HasPrefix(md.VideoID, "stash=")
					isJav := isJavVideoID(md.VideoID)
					if (isAdult && !isJav) || (md.PosterURL == "" && md.PosterHorizontalURL != "") {
						hasOnlyHorizontal = true
					}
				}
				if pa.horizontal && !isCachedHorizontal {
					log.Warnf("poster: Cached poster for %s is vertical but horizontal was requested, bypassing cache", pa.imdbID)
					b = nil
				} else if !pa.horizontal && isCachedHorizontal && !hasOnlyHorizontal {
					log.Warnf("poster: Cached poster for %s is horizontal but vertical was requested, bypassing cache", pa.imdbID)
					b = nil
				}
			}
		}
	}

	// 2. Cache Hit -> Serve immediately
	if b != nil {
		etag := s.generateETag(b.Bytes())

		if match := c.Request.Header.Get("If-None-Match"); match != "" && match == etag {
			c.Status(http.StatusNotModified)
			return
		}
		c.Header("Content-Type", "image/jpeg")
		c.Header("Content-Length", strconv.Itoa(b.Len()))
		c.Header("ETag", etag)
		c.Header("Cache-Control", "public, max-age=86400")
		c.Status(http.StatusOK)

		_, _ = io.Copy(c.Writer, b)
		return
	}

	// 3. Cache Miss -> Lookup the original poster URL
	posterURL, err := s.getOriginalPosterURL(ctx, db, pa)
	if err != nil {
		if errors.Is(err, errPosterNotFound) {
			_ = c.AbortWithError(http.StatusNotFound, err)
			return
		}
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get original poster URL"))
		return
	}

	// 4. Trigger synchronous download, crop, and S3-caching if it's a JAV scene
	isJav := isJavVideoID(pa.imdbID)
	if isJav {
		resizedBuf, resizeErr := s.getResizedJPEGPoster(ctx, db, pa)
		if resizeErr != nil {
			log.WithError(resizeErr).Warnf("poster: synchronous JAV resize failed for %s", pa.imdbID)
			c.Redirect(http.StatusTemporaryRedirect, posterURL)
			return
		}

		if s.s3Cl != nil && s.posterCacheS3Bucket != "" {
			imgData := make([]byte, resizedBuf.Len())
			copy(imgData, resizedBuf.Bytes())
			go func(pa *PosterArgs, data []byte) {
				detachedCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cl := s.s3Cl.Get()
				if putErr := s.putPosterToCache(detachedCtx, cl, pa, bytes.NewBuffer(data)); putErr != nil {
					log.WithError(putErr).Warn("poster: S3 background cache put failed for JAV")
				} else {
					log.Infof("poster: successfully cached JAV poster in background for %s", pa.imdbID)
				}
			}(pa, imgData)
		}

		c.Header("Content-Type", "image/jpeg")
		c.Header("Content-Length", strconv.Itoa(resizedBuf.Len()))
		c.Header("ETag", s.generateETag(resizedBuf.Bytes()))
		c.Header("Cache-Control", "public, max-age=86400")
		c.Status(http.StatusOK)
		_, _ = io.Copy(c.Writer, resizedBuf)
		return
	}

	// 4. Trigger background download, resize, and S3-caching (if S3 is configured) for standard scenes
	if s.s3Cl != nil && s.posterCacheS3Bucket != "" {
		go func(pa *PosterArgs) {
			select {
			case resizeSemaphore <- struct{}{}:
				defer func() { <-resizeSemaphore }()
			case <-time.After(60 * time.Second):
				return
			}

			detachedCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			resizedBuf, resizeErr := s.getResizedJPEGPoster(detachedCtx, s.pg.Get(), pa)
			if resizeErr != nil {
				log.WithError(resizeErr).Warnf("poster: background resize failed for %s", pa.imdbID)
				return
			}
			cl := s.s3Cl.Get()
			if putErr := s.putPosterToCache(detachedCtx, cl, pa, resizedBuf); putErr != nil {
				log.WithError(putErr).Warn("poster: S3 background cache put failed")
			} else {
				log.Infof("poster: successfully cached resized poster in background for %s", pa.imdbID)
			}
		}(pa)
	}

	// 5. Instantly redirect browser to original CDN URL
	c.Redirect(http.StatusTemporaryRedirect, posterURL)
}

func (s *Handler) generateETag(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf(`"%x"`, sum[:])
}

func (s *Handler) getOriginalPosterURL(ctx context.Context, db *pg.DB, args *PosterArgs) (string, error) {
	md, err := s.getPosterMetadata(ctx, db, args.t, args.imdbID)
	if err != nil {
		return "", err
	}
	if md == nil {
		return "", errors.Wrapf(errPosterNotFound, "%s %s", args.t, args.imdbID)
	}

	var posterURL string
	if args.horizontal {
		posterURL = md.PosterHorizontalURL
		if posterURL == "" {
			posterURL = md.PosterURL
		}
		if strings.Contains(posterURL, "theporndb.net") {
			// Extract raw original background image from CDN without signature restrictions
			var sceneSuffix string
			if idx := strings.Index(posterURL, "/scene/"); idx != -1 {
				sceneSuffix = posterURL[idx:]
			} else if idx := strings.Index(posterURL, "/scene%2F"); idx != -1 {
				sceneSuffix = posterURL[idx:]
			} else if idx := strings.Index(posterURL, "/scene%2f"); idx != -1 {
				sceneSuffix = posterURL[idx:]
			}
			if sceneSuffix != "" {
				sceneSuffix = strings.ReplaceAll(sceneSuffix, "%2F", "/")
				sceneSuffix = strings.ReplaceAll(sceneSuffix, "%2f", "/")
				posterURL = "https://cdn.theporndb.net" + sceneSuffix
			}
		}
	} else {
		posterURL = md.PosterURL
		if posterURL == "" {
			posterURL = md.PosterHorizontalURL
		}
	}

	if posterURL == "" {
		return "", errors.Wrapf(errPosterNotFound, "%s %s", args.t, args.imdbID)
	}
	if strings.Contains(posterURL, "pics.dmm.co.jp") {
		posterURL = strings.ReplaceAll(posterURL, "ps.jpg", "pl.jpg")
	}
	return posterURL, nil
}

func (s *Handler) getResizedPoster(ctx context.Context, db *pg.DB, args *PosterArgs) (*image.NRGBA, error) {
	posterURL, err := s.getOriginalPosterURL(ctx, db, args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", posterURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download poster from %s, status: %d", posterURL, resp.StatusCode)
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	srcImg, err := imaging.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	bounds := srcImg.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var processed image.Image
	isJav := isJavVideoID(args.imdbID)

	if isJav {
		if !args.horizontal {
			// Portrait poster requested
			if w > h {
				// Determine if it is a full DVD jacket (front cover on right) or a generic landscape backdrop
				urlLower := strings.ToLower(posterURL)
				isJavJacket := strings.Contains(urlLower, "dmm.co.jp") ||
					strings.Contains(urlLower, "jav.guru") ||
					strings.Contains(urlLower, "javmiku") ||
					strings.Contains(urlLower, "javnorth") ||
					strings.Contains(urlLower, "pl.jpg")

				if isJavJacket {
					// If source is horizontal jacket cover, crop the right 50% (front cover)
					processed = imaging.Crop(srcImg, image.Rect(w/2, 0, w, h))
					processed = imaging.Resize(processed, args.width, 0, imaging.Lanczos)
				} else {
					// Generic scene screenshot / backdrop: crop center to 2:3 aspect ratio
					processed = imaging.Fill(srcImg, args.width, int(float64(args.width)*1.5), imaging.Center, imaging.Lanczos)
				}
			} else {
				// Already vertical, do not crop
				processed = srcImg
				processed = imaging.Resize(processed, args.width, 0, imaging.Lanczos)
			}
		} else {
			// Landscape (horizontals) requested: do not crop! Just resize
			processed = imaging.Resize(srcImg, args.width, 0, imaging.Lanczos)
		}
	} else {
		if !args.horizontal && w > h {
			// Center crop to 2:3 aspect ratio
			processed = imaging.Fill(srcImg, args.width, int(float64(args.width)*1.5), imaging.Center, imaging.Lanczos)
		} else {
			processed = imaging.Resize(srcImg, args.width, 0, imaging.Lanczos)
		}
	}

	return imaging.Clone(processed), nil
}

func (s *Handler) getResizedJPEGPoster(ctx context.Context, db *pg.DB, args *PosterArgs) (*bytes.Buffer, error) {
	r, err := s.getResizedPoster(ctx, db, args)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, r, &jpeg.Options{Quality: PosterJPEGQuality})
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) getPosterMetadata(ctx context.Context, db *pg.DB, t models.ContentType, videoID string) (md *models.VideoMetadata, err error) {
	// First, try the persisted enrichment record. If a torrent has been
	// enriched, the poster URL is already in series_metadata/movie_metadata
	// and we can return it directly. This keeps the served poster from the
	// same source that produced the video_id (an invariant — see migration
	// 51 and the Kinopoisk Unofficial mapper for the historical reason).
	var dbMd *models.VideoMetadata
	switch t {
	case models.ContentTypeMovie:
		mm, mmErr := models.GetMovieMetadataByVideoID(ctx, db, videoID)
		if mmErr == nil && mm != nil && mm.VideoMetadata != nil && (mm.PosterURL != "" || mm.PosterHorizontalURL != "") {
			dbMd = mm.VideoMetadata
		}
	case models.ContentTypeSeries:
		sm, smErr := models.GetSeriesMetadataByVideoID(ctx, db, videoID)
		if smErr == nil && sm != nil && sm.VideoMetadata != nil && (sm.PosterURL != "" || sm.PosterHorizontalURL != "") {
			dbMd = sm.VideoMetadata
		}
	}

	// If we found database metadata, but it is missing the horizontal poster url,
	// try to look it up in the mapper-specific caches (like tmdb.info) which might
	// contain the backdrop_path. This acts as a graceful fallback for previously
	// enriched items before migration 55.
	if dbMd != nil {
		if dbMd.PosterHorizontalURL != "" {
			return dbMd, nil
		}
		if s.enricher != nil {
			fallbackMd, err := s.enricher.LookupByVideoID(ctx, videoID, t)
			if err == nil && fallbackMd != nil && fallbackMd.PosterHorizontalURL != "" {
				dbMd.PosterHorizontalURL = fallbackMd.PosterHorizontalURL
				return dbMd, nil
			}
		}
		return dbMd, nil
	}

	// Fallback: AI/discover writes only into mapper-specific caches
	// (tmdb.info, kpu.info) without ever populating series_metadata /
	// movie_metadata. The mapper chain resolves those.
	if s.enricher != nil {
		md, err = s.enricher.LookupByVideoID(ctx, videoID, t)
		if err == nil && md != nil && (md.PosterURL != "" || md.PosterHorizontalURL != "") {
			return md, nil
		}
	}
	return nil, nil
}

func (s *PosterArgs) Key() string {
	if s.horizontal {
		return fmt.Sprintf("horizontal/%v/%v/%v.%v", s.t, s.imdbID, s.width, s.format)
	}
	return fmt.Sprintf("%v/%v/%v.%v", s.t, s.imdbID, s.width, s.format)
}

func (s *Handler) getPosterFromCache(ctx context.Context, s3Cl *s3.S3, pa *PosterArgs) (*bytes.Buffer, error) {
	r, err := s3Cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.posterCacheS3Bucket),
		Key:    aws.String(pa.Key()),
	})
	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == s3.ErrCodeNoSuchKey {
			return nil, nil
		}
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(r.Body)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r.Body)
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) makeAWSMD5(b []byte) *string {
	h := md5.Sum(b)
	m := base64.StdEncoding.EncodeToString(h[:])
	return aws.String(m)
}

func (s *Handler) putPosterToCache(ctx context.Context, s3Cl *s3.S3, pa *PosterArgs, b *bytes.Buffer) (err error) {
	data := b.Bytes()
	_, err = s3Cl.PutObjectWithContext(ctx,
		&s3.PutObjectInput{
			Bucket:     aws.String(s.posterCacheS3Bucket),
			Key:        aws.String(pa.Key()),
			Body:       bytes.NewReader(data),
			ContentMD5: s.makeAWSMD5(data),
		})
	return
}

func isJavVideoID(videoID string) bool {
	idLower := strings.ToLower(videoID)
	return strings.HasPrefix(idLower, "tpdb_jav:") || strings.HasPrefix(idLower, "tpdb_jav=") || strings.Contains(idLower, "jav.guru")
}
