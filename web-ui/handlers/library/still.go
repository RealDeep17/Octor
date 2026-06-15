package library

import (
	"bytes"
	"context"
	"time"
	"fmt"
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

// stillResizeSemaphore restricts the number of concurrent still-image resizing operations.
var stillResizeSemaphore = make(chan struct{}, 8)

type StillArgs struct {
	videoID string
	season  int
	episode int
	width   int
	format  PosterFormat
}

func (s *StillArgs) Key() string {
	return fmt.Sprintf("episode/%v/s%de%d/%v.%v", s.videoID, s.season, s.episode, s.width, s.format)
}

func (s *Handler) bindStillArgs(c *gin.Context) (*StillArgs, error) {
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
	season, err := strconv.Atoi(c.Param("season"))
	if err != nil {
		return nil, errors.Errorf("wrong season %v", c.Param("season"))
	}
	episode, err := strconv.Atoi(c.Param("episode"))
	if err != nil {
		return nil, errors.Errorf("wrong episode %v", c.Param("episode"))
	}
	return &StillArgs{
		videoID: c.Param("video_id"),
		season:  season,
		episode: episode,
		width:   width,
		format:  f,
	}, nil
}

func (s *Handler) still(c *gin.Context) {
	sa, err := s.bindStillArgs(c)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, errors.Wrap(err, "failed to bind still args"))
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
		b, err = s.getStillFromCache(cacheCtx, cl, sa)
		cacheCancel()
		if err != nil {
			log.WithError(err).Warn("still: S3 cache get failed, falling through")
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

	// 3. Cache Miss -> Lookup the original still URL
	stillURL, err := s.getOriginalStillURL(ctx, db, sa)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, errors.Wrap(err, "failed to get original still URL"))
		return
	}

	// 4. Trigger background download, resize, and S3-caching (if S3 is configured)
	if s.s3Cl != nil && s.posterCacheS3Bucket != "" {
		reqCtx := c.Request.Context()
		go func(sa *StillArgs, rCtx context.Context) {
			select {
			case stillResizeSemaphore <- struct{}{}:
				defer func() { <-stillResizeSemaphore }()
			case <-rCtx.Done():
				return
			}

			detachedCtx, cancel := context.WithTimeout(rCtx, 60*time.Second)
			defer cancel()

			resizedBuf, resizeErr := s.getResizedJPEGStill(detachedCtx, s.pg.Get(), sa)
			if resizeErr != nil {
				log.WithError(resizeErr).Warnf("still: background resize failed for episode still %s S%dE%d", sa.videoID, sa.season, sa.episode)
				return
			}
			cl := s.s3Cl.Get()
			if putErr := s.putStillToCache(detachedCtx, cl, sa, resizedBuf); putErr != nil {
				log.WithError(putErr).Warn("still: S3 background cache put failed")
			} else {
				log.Infof("still: successfully cached resized still in background for episode %s S%dE%d", sa.videoID, sa.season, sa.episode)
			}
		}(sa, reqCtx)
	}

	// 5. Instantly redirect browser to original CDN URL
	c.Redirect(http.StatusTemporaryRedirect, stillURL)
}

func (s *Handler) getOriginalStillURL(ctx context.Context, db *pg.DB, args *StillArgs) (string, error) {
	emd, err := models.GetEpisodeMetadata(ctx, db, args.videoID, int16(args.season), int16(args.episode))
	if err != nil {
		return "", errors.Wrap(err, "failed to get episode metadata")
	}
	if emd == nil || emd.StillURL == nil || *emd.StillURL == "" {
		return "", errors.New("no still url for episode")
	}
	return *emd.StillURL, nil
}

func (s *Handler) getResizedJPEGStill(ctx context.Context, db *pg.DB, args *StillArgs) (*bytes.Buffer, error) {
	stillURL, err := s.getOriginalStillURL(ctx, db, args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", stillURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	srcImg, err := imaging.Decode(resp.Body)
	if err != nil {
		return nil, err
	}

	resized := imaging.Resize(srcImg, args.width, 0, imaging.Linear)

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, resized, &jpeg.Options{Quality: PosterJPEGQuality})
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (s *Handler) getStillFromCache(ctx context.Context, s3Cl *s3.S3, sa *StillArgs) (*bytes.Buffer, error) {
	r, err := s3Cl.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.posterCacheS3Bucket),
		Key:    aws.String(sa.Key()),
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

func (s *Handler) putStillToCache(ctx context.Context, s3Cl *s3.S3, sa *StillArgs, b *bytes.Buffer) (err error) {
	data := b.Bytes()
	_, err = s3Cl.PutObjectWithContext(ctx,
		&s3.PutObjectInput{
			Bucket:     aws.String(s.posterCacheS3Bucket),
			Key:        aws.String(sa.Key()),
			Body:       bytes.NewReader(data),
			ContentMD5: s.makeAWSMD5(data),
		})
	return
}
