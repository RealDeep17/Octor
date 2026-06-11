package services

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	awss3 "github.com/aws/aws-sdk-go/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/pkg/errors"
)

func init() {
	_ = mime.AddExtensionType(".mkv", "video/x-matroska")
}

const presignTTL = 1 * time.Hour

// WebSeed handler — GET/HEAD /webseed/{id}/{path}
// @Summary      Webseed proxy
// @Description  Redirects to a presigned S3 URL for the stored file. Root path requires the whole resource to be stored; per-file paths are served as soon as the individual file is stored, even if the resource as a whole is still uploading.
// @Tags         webseed
// @Param        id    path      string  true  "Resource ID"
// @Param        path  path      string  true  "Path inside resource"
// @Success      302
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /webseed/{id}/{path} [get]
// @Router       /webseed/{id}/{path} [head]
func (s *Web) webSeed(c *gin.Context) {
	if !s.validateWebSeedDependencies(c) {
		return
	}
	id := c.Param("id")
	p := c.Param("path")

	db := s.pg.Get()
	res, err := ResourceGetByID(c.Request.Context(), db, id)

	if err != nil {
		_ = c.Error(err)
		return
	}
	if res == nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Root: signals "whole torrent is in vault" — used by clients to
	// short-circuit availability checks. Stays gated on resource status.
	if p == "" || p == "/" {
		if res.Status != StatusStored {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusOK)
		return
	}

	// Per-file: serve as soon as this specific file finished uploading,
	// independent of the resource's aggregate status. Lets partially
	// uploaded torrents (e.g. a TV series mid-store) be streamed for
	// already-stored episodes.
	hash, ok, err := s.lookupStoredFileHash(c.Request.Context(), db, id, p)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}

	if c.Request.Method == http.MethodHead {
		s.handleHeadRequest(c, hash)
	} else {
		// Calculate overrides to prevent browser sniffing issues (e.g. MKV as WebM)
		ext := filepath.Ext(p)
		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		fileName := filepath.Base(p)
		contentDisposition := fmt.Sprintf("inline; filename=\"%s\"", fileName)
		if c.Query("download") == "true" {
			contentDisposition = fmt.Sprintf("attachment; filename=\"%s\"", fileName)
		}

		presignedURL, err := s.presignGetObject(c.Request.Context(), hash, contentType, contentDisposition)
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.Redirect(http.StatusFound, presignedURL)
	}
}

func (s *Web) handleHeadRequest(c *gin.Context, hash string) {
	s3cl := s.s3.Get()
	out, err := s3cl.HeadObjectWithContext(c.Request.Context(), &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s3Key(hash)),
	})
	if err != nil {
		out, err = s3cl.HeadObjectWithContext(c.Request.Context(), &awss3.HeadObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(hash),
		})
	}
	if err != nil {
		_ = c.Error(err)
		return
	}
	if out.ContentLength != nil {
		c.Header("Content-Length", fmt.Sprintf("%d", *out.ContentLength))
	}
	if out.ContentType != nil {
		c.Header("Content-Type", *out.ContentType)
	}
	c.Header("Accept-Ranges", "bytes")
	c.Status(http.StatusOK)
}

func (s *Web) validateWebSeedDependencies(c *gin.Context) bool {
	if s.pg.Get() == nil {
		_ = c.Error(errors.New("DB not configured"))
		return false
	}
	if s.s3 == nil {
		_ = c.Error(errors.New("S3 not configured"))
		return false
	}
	if s.bucket == "" {
		_ = c.Error(errors.New("aws-bucket is not configured"))
		return false
	}
	return true
}

func (s *Web) lookupStoredFileHash(ctx context.Context, db *pg.DB, id, path string) (string, bool, error) {
	rf := &ResourceFile{}
	err := db.Model(rf).
		Context(ctx).
		Relation("File").
		Where("resource_file.resource_id = ?", id).
		Where("resource_file.path = ?", path).
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if rf.File == nil || rf.File.Status != StatusStored {
		return "", false, nil
	}
	return rf.FileHash, true, nil
}

func (s *Web) presignGetObject(ctx context.Context, hash string, contentType string, contentDisposition string) (string, error) {
	s3cl := s.s3.Get()
	key := s3Key(hash)
	_, err := s3cl.HeadObjectWithContext(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		key = hash
	}
	req, _ := s3cl.GetObjectRequest(&awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		ResponseContentType:        aws.String(contentType),
		ResponseContentDisposition: aws.String(contentDisposition),
	})
	return req.Presign(presignTTL)
}
