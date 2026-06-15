package stremio

import (
	"context"
	"fmt"
	"os"

	"github.com/webtor-io/web-ui/services/auth"
)

const catalogID = "Octor"

type Manifest struct {
	domain string
	u      *auth.User
	ht     bool
}

func NewManifest(domain string, u *auth.User, hasToken bool) *Manifest {
	return &Manifest{
		domain: domain,
		u:      u,
		ht:     hasToken,
	}
}

func (s *Manifest) GetManifest(c context.Context) (*ManifestResponse, error) {
	m := &ManifestResponse{
		Id:          "org.stremio.octor",
		Version:     "0.0.2",
		Name:        "Octor",
		Description: "Stream your personal torrent library from Octor directly in Stremio. Add torrents to your Octor account and watch them instantly — no downloading, no setup, just click and play.",
		Types:       []string{"movie", "series"},
		Catalogs: []CatalogItem{
			{"movie", catalogID},
			{"series", catalogID},
		},
		Resources:    []string{"stream", "catalog", "meta"},
		Logo:         fmt.Sprintf("%v/assets/night/android-chrome-256x256.png", s.domain),
		ContactEmail: "support@octor",
		AddonsConfig: &AddonsConfig{
			Issuer:    "https://stremio-addons.net",
			Signature: os.Getenv("STREMIO_ADDONS_SIGNATURE"),
		},
	}
	if s.u == nil || !s.ht {
		m.BehaviorHints = &BehaviorHints{
			Configurable:          true,
			ConfigurationRequired: true,
		}

	}
	return m, nil
}

var _ ManifestService = (*Manifest)(nil)
