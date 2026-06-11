package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/webtor-io/web-ui/services/web"
)

func getInfraDataPath(subpath string) string {
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

var settingsFilePath = getInfraDataPath("settings.json")

// OctorSettings holds all runtime-configurable settings persisted to disk.
// The rest-api TransmissionService reads this same file for autoVaultEnabled().
type OctorSettings struct {
	AutoVault *bool `json:"auto_vault"`
}

type SettingsData struct {
	AutoVaultEnabled bool
}

func LoadSettings() (*OctorSettings, error) {
	data, err := os.ReadFile(settingsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &OctorSettings{}, nil
		}
		return nil, errors.Wrap(err, "failed to read settings file")
	}
	var s OctorSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, errors.Wrap(err, "failed to parse settings file")
	}
	return &s, nil
}

func SaveSettings(s *OctorSettings) error {
	if err := os.MkdirAll(getInfraDataPath(""), 0755); err != nil {
		return errors.Wrap(err, "failed to create infra-data directory")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal settings")
	}
	return os.WriteFile(settingsFilePath, data, 0644)
}

func (h *Handler) settingsIndex(c *gin.Context) {
	s, err := LoadSettings()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	autoVault := false
	if s.AutoVault != nil {
		autoVault = *s.AutoVault
	}
	h.tb.Build("admin/settings").HTML(http.StatusOK, web.NewContext(c).WithData(&SettingsData{
		AutoVaultEnabled: autoVault,
	}))
}

func (h *Handler) settingsSave(c *gin.Context) {
	s, err := LoadSettings()
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	autoVault := c.PostForm("auto_vault") == "1"
	s.AutoVault = &autoVault
	if err := SaveSettings(s); err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	web.RedirectWithSuccessAndMessage(c, "toast.settingsSaved")
}
