package config

import (
	"os"
	"strconv"
)

type Config struct {
	StashDBAPIKey            string
	StashDBEndpoint          string
	ThePornDBAPIKey          string
	TPDBBase                 string
	SidecarEnrichmentEnabled bool
	SidecarDataDir           string
	Port                     int
}

func Load() *Config {
	portVal := 8000
	if pStr := os.Getenv("OMDB_API_PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			portVal = p
		}
	} else if pStr := os.Getenv("PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			portVal = p
		}
	}

	sidecarEnrich := true
	if val := os.Getenv("SIDECAR_ENRICHMENT_ENABLED"); val != "" {
		sidecarEnrich = val != "false" && val != "0"
	}

	stashdbEndpoint := os.Getenv("STASHDB_ENDPOINT")
	if stashdbEndpoint == "" {
		stashdbEndpoint = "https://stashdb.org/graphql"
	}

	tpdbBase := os.Getenv("TPDB_BASE")
	if tpdbBase == "" {
		tpdbBase = "https://api.theporndb.net"
	}

	tpdbKey := os.Getenv("THEPORNDB_API_KEY")
	if tpdbKey == "" {
		tpdbKey = os.Getenv("TPDB_API_KEY")
	}

	dataDir := os.Getenv("SIDECAR_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	return &Config{
		StashDBAPIKey:            os.Getenv("STASHDB_API_KEY"),
		StashDBEndpoint:          stashdbEndpoint,
		ThePornDBAPIKey:          tpdbKey,
		TPDBBase:                 tpdbBase,
		SidecarEnrichmentEnabled: sidecarEnrich,
		SidecarDataDir:           dataDir,
		Port:                     portVal,
	}
}
