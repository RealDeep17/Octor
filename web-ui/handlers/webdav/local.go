package webdav

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/webdav"
)

type LocalDirectory struct {
	BaseDirectory
	Root string
}

func (s *LocalDirectory) resolveSafePath(path string) (string, error) {
	cleanRoot := filepath.Clean(s.Root)
	fullPath := filepath.Clean(filepath.Join(cleanRoot, path))

	// Ensure fullPath starts with cleanRoot
	if !strings.HasPrefix(fullPath, cleanRoot) {
		return "", errors.New("permission denied: path traversal detected")
	}

	// Ensure boundary check
	if len(fullPath) > len(cleanRoot) && fullPath[len(cleanRoot)] != filepath.Separator {
		return "", errors.New("permission denied: path traversal detected")
	}

	return fullPath, nil
}

func (s *LocalDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, err
	}
	return f, nil, nil
}

func (s *LocalDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}
	var fis []webdav.FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fis = append(fis, webdav.FileInfo{
			Path:     filepath.Join(path, entry.Name()),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			IsDir:    entry.IsDir(),
			MIMEType: "",
		})
	}
	return fis, nil
}

func (s *LocalDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, webdav.NewHTTPError(404, errors.New("file not found"))
		}
		return nil, err
	}
	return &webdav.FileInfo{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

func (s *LocalDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return nil, false, err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	written, err := io.Copy(f, body)
	if err != nil {
		return nil, false, err
	}
	info, _ := f.Stat()
	return &webdav.FileInfo{
		Path:    path,
		Size:    written,
		ModTime: info.ModTime(),
		IsDir:   false,
	}, true, nil
}

func (s *LocalDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(fullPath)
}

func (s *LocalDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	oldPath, err := s.resolveSafePath(path)
	if err != nil {
		return false, err
	}
	newPath, err := s.resolveSafePath(dest)
	if err != nil {
		return false, err
	}
	err = os.Rename(oldPath, newPath)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *LocalDirectory) Mkdir(ctx context.Context, path string) error {
	fullPath, err := s.resolveSafePath(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0755)
}

var _ webdav.FileSystem = (*LocalDirectory)(nil)
