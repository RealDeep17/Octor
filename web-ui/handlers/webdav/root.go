package webdav

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	adminsvc "github.com/webtor-io/web-ui/services/admin"
	"github.com/webtor-io/web-ui/services/webdav"
)

type RootDirectory struct {
	BaseDirectory
	Admin    *adminsvc.Admin
	Children map[string]webdav.FileSystem
}

func (s *RootDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	if isRoot(path) {
		return nil, nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	c := s.getChild(ctx, path)
	if c == nil {
		return nil, nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path || c.Root == path+"/" {
		return nil, nil, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Open(ctx, c.NewPath)
}

func (s *RootDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	if isRoot(path) {
		var dirs []webdav.FileInfo
		for k := range s.Children {
			dirs = append(dirs, newDirectoryFileInfo(k))
		}
		return dirs, nil
	}
	c := s.getChild(ctx, path)
	if c == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	fis, err := c.Child.ReadDir(ctx, c.NewPath, recursive)
	if err != nil {
		return nil, err
	}
	return addPrefixes(fis, c.Root), nil
}

func (s *RootDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	if isRoot(path) {
		fi := newDirectoryFileInfo("/")
		return &fi, nil
	}
	c := s.getChild(ctx, path)
	if c == nil {
		return nil, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	// If the path exactly matches the child name (with or without trailing slash), return its info
	if c.Root == path || c.Root == path+"/" {
		fi := newDirectoryFileInfo(c.Name)
		return &fi, nil
	}
	fi, err := c.Child.Stat(ctx, c.NewPath)
	if err != nil {
		return nil, err
	}
	return addPrefix(fi, c.Root), nil
}

func (s *RootDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	if isRoot(path) {
		return webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	c := s.getChild(ctx, path)
	if c == nil {
		return webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path || c.Root == path+"/" {
		return webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.RemoveAll(ctx, c.NewPath, opts)
}

func (s *RootDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	c := s.getChild(ctx, path)
	if c == nil {
		return nil, false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	if c.Root == path || c.Root == path+"/" {
		return nil, false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	fi, ok, err := c.Child.Create(ctx, c.NewPath, body, opts)
	if err != nil {
		return nil, false, err
	}
	return addPrefix(fi, c.Root), ok, nil
}

func (s *RootDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	c := s.getChild(ctx, path)
	if c == nil {
		return false, webdav.NewHTTPError(404, errors.New("file not found"))
	}
	if c.Root == path || c.Root == path+"/" {
		return false, webdav.NewHTTPError(403, errors.New("operation not permitted"))
	}
	return c.Child.Move(ctx, c.NewPath, s.removePrefix(dest, c.Name), options)
}

type ChildResponse struct {
	Child   webdav.FileSystem
	Name    string
	NewPath string
	Root    string
}

func (s *RootDirectory) getChild(ctx context.Context, path string) *ChildResponse {
	// Ensure path starts with / for matching
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for name, d := range s.Children {
		prefix := "/" + name
		// Match /name/something or exact /name
		if strings.HasPrefix(path, prefix+"/") || path == prefix {
			return &ChildResponse{
				Child:   d,
				Name:    name,
				NewPath: s.removePrefix(path, name),
				Root:    prefix + "/",
			}
		}
	}
	return nil
}

func (s *RootDirectory) removePrefix(path string, name string) string {
	res := strings.TrimPrefix(path, "/"+name)
	if res == "" || !strings.HasPrefix(res, "/") {
		res = "/" + res
	}
	return res
}

func (s *RootDirectory) canUseAdmin(ctx context.Context) bool {
	if s.Admin == nil {
		return false
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return false
	}
	return s.Admin.IsAdminUser(wcc.User)
}

// DualRootDirectory presents AdminChildren to admin users and UserChildren to everyone else.
// This is evaluated per-request so the tree is access-controlled without needing to build
// separate filesystem trees per user at server startup.
type DualRootDirectory struct {
	Admin         *adminsvc.Admin
	AdminChildren map[string]webdav.FileSystem
	UserChildren  map[string]webdav.FileSystem
}

func (s *DualRootDirectory) effectiveChildren(ctx context.Context) map[string]webdav.FileSystem {
	if s.Admin == nil {
		return s.UserChildren
	}
	wcc, err := getWebContext(ctx)
	if err != nil {
		return s.UserChildren
	}
	if s.Admin.IsAdminUser(wcc.User) {
		return s.AdminChildren
	}
	return s.UserChildren
}

func (s *DualRootDirectory) toRoot(ctx context.Context) *RootDirectory {
	return &RootDirectory{Children: s.effectiveChildren(ctx)}
}

func (s *DualRootDirectory) Open(ctx context.Context, path string) (io.ReadCloser, *url.URL, error) {
	return s.toRoot(ctx).Open(ctx, path)
}

func (s *DualRootDirectory) ReadDir(ctx context.Context, path string, recursive bool) ([]webdav.FileInfo, error) {
	return s.toRoot(ctx).ReadDir(ctx, path, recursive)
}

func (s *DualRootDirectory) Stat(ctx context.Context, path string) (*webdav.FileInfo, error) {
	return s.toRoot(ctx).Stat(ctx, path)
}

func (s *DualRootDirectory) RemoveAll(ctx context.Context, path string, opts *webdav.RemoveAllOptions) error {
	return s.toRoot(ctx).RemoveAll(ctx, path, opts)
}

func (s *DualRootDirectory) Create(ctx context.Context, path string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	return s.toRoot(ctx).Create(ctx, path, body, opts)
}

func (s *DualRootDirectory) Move(ctx context.Context, path, dest string, options *webdav.MoveOptions) (bool, error) {
	return s.toRoot(ctx).Move(ctx, path, dest, options)
}

func (s *DualRootDirectory) Mkdir(ctx context.Context, path string) error {
	return s.toRoot(ctx).Mkdir(ctx, path)
}

func (s *DualRootDirectory) Copy(ctx context.Context, path, dest string, options *webdav.CopyOptions) (bool, error) {
	return s.toRoot(ctx).Copy(ctx, path, dest, options)
}

var _ webdav.FileSystem = (*DualRootDirectory)(nil)
