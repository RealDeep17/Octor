package services

import (
	"os"
	"path/filepath"
)

// GetInfraDataPath resolves workspace data directory path dynamically.
func GetInfraDataPath(subpath string) string {
	if root := os.Getenv("OCTOR_ROOT"); root != "" {
		return filepath.Join(root, "infra-data", subpath)
	}
	if root := os.Getenv("PROJECT_ROOT"); root != "" {
		return filepath.Join(root, "infra-data", subpath)
	}
	if _, err := os.Stat("/srv/octor"); err == nil {
		return filepath.Join("/srv/octor/infra-data", subpath)
	}
	return filepath.Join("./infra-data", subpath)
}
