package admin

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/handlers/library/shared"
	"github.com/webtor-io/web-ui/services/web"
)

type DriveItem struct {
	Name        string
	Path        string
	EscapedPath string
	Size        int64
	IsDir       bool
	ModTime     time.Time
}

type DriveBreadcrumb struct {
	Name        string
	Path        string
	EscapedPath string
}

type DriveData struct {
	Args              *shared.IndexArgs
	Path              string
	ParentPath        string
	EscapedParentPath string
	Items             []DriveItem
	Breadcrumbs       []DriveBreadcrumb
	RcloneUsed        int64
	RcloneTotal       int64
}

func (i DriveItem) IsPlayable() bool {
	if i.IsDir {
		return false
	}
	ext := strings.ToLower(filepath.Ext(i.Name))
	switch ext {
	case ".mp4", ".mkv", ".webm", ".avi", ".mov", ".m4v", ".mp3", ".wav":
		return true
	}
	return false
}

func (h *Handler) driveIndex(c *gin.Context) {
	root := filepath.Clean(getInfraDataPath("drive-mount-vfs"))
	evalRoot, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = evalRoot
	}
	path := strings.TrimPrefix(c.Param("path"), "/")
	fullPath := filepath.Join(root, path)

	resolvedFullPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		resolvedFullPath = filepath.Clean(fullPath)
	}

	rel, err := filepath.Rel(root, resolvedFullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		c.Status(http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
		} else {
			_ = c.AbortWithError(http.StatusInternalServerError, err)
		}
		return
	}

	if !info.IsDir() {
		c.File(fullPath)
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	var items []DriveItem
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		itemPath := filepath.Join(path, entry.Name())
		items = append(items, DriveItem{
			Name:        entry.Name(),
			Path:        itemPath,
			EscapedPath: escapePath(itemPath),
			Size:        info.Size(),
			IsDir:       entry.IsDir(),
			ModTime:     info.ModTime(),
		})
	}

	var bc []DriveBreadcrumb
	bc = append(bc, DriveBreadcrumb{Name: "Root", Path: "", EscapedPath: ""})

	parts := strings.Split(strings.Trim(path, "/"), "/")
	curr := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		curr = filepath.Join(curr, p)
		bc = append(bc, DriveBreadcrumb{Name: p, Path: curr, EscapedPath: escapePath(curr)})
	}

	parent := ""
	if path != "" {
		parent = filepath.Dir(strings.TrimSuffix(path, "/"))
		if parent == "." {
			parent = ""
		}
	}

	var rcloneUsedBytes, rcloneTotalBytes int64
	if h.vault != nil {
		rcloneUsedBytes = int64(h.vault.GetUsedSpaceGB() * 1024 * 1024 * 1024)
		rcloneTotalBytes = int64(h.vault.GetTotalSpaceGB() * 1024 * 1024 * 1024)
	}

	h.tb.Build("admin/drive").HTML(http.StatusOK, web.NewContext(c).WithData(&DriveData{
		Path:              path,
		ParentPath:        parent,
		EscapedParentPath: escapePath(parent),
		Items:             items,
		Breadcrumbs:       bc,
		RcloneUsed:        rcloneUsedBytes,
		RcloneTotal:       rcloneTotalBytes,
	}))
}
