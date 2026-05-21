package webdav

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/webdav"
)

type LocalDirectory struct {
	BaseDirectory
	Root string
}

func (s *LocalDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	fullPath := filepath.Join(s.Root, path)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, err
	}
	return f, nil, nil
}

func (s *LocalDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	fullPath := filepath.Join(s.Root, path)
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
	fullPath := filepath.Join(s.Root, path)
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
	fullPath := filepath.Join(s.Root, path)
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
	fullPath := filepath.Join(s.Root, path)
	return os.RemoveAll(fullPath)
}

func (s *LocalDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	oldPath := filepath.Join(s.Root, path)
	newPath := filepath.Join(s.Root, dest)
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *LocalDirectory) Mkdir(ctx context.Context, path string) error {
	fullPath := filepath.Join(s.Root, path)
	return os.MkdirAll(fullPath, 0755)
}

var _ webdav.FileSystem = (*LocalDirectory)(nil)
