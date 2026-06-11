package main

import (
	"bufio"
	"flag"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-pg/pg/v10"
)

// loadEnv reads a simple KEY=VALUE env file like custom.env
func loadEnv(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return // Ignore if not found, rely on system env
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			// Only set if not already set in environment
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func main() {
	// Setup flags
	dryRun := flag.Bool("dry-run", true, "Set to 'false' to actually delete files and database rows")
	flag.Parse()

	// Load custom env files
	loadEnv("custom.env")
	loadEnv("../custom.env")
	loadEnv("/home/ubuntu/octor/custom.env")

	log.Println(strings.Repeat("=", 60))
	log.Printf("Starting Octor Storage Garbage Collection (Go Version)")
	log.Printf("Mode: DRY-RUN = %v", *dryRun)
	log.Println(strings.Repeat("=", 60))

	// Get DB credentials
	pgUser := os.Getenv("PG_USER")
	pgPass := os.Getenv("PG_PASSWORD")
	pgHost := os.Getenv("PG_HOST")
	pgPort := os.Getenv("PG_PORT")

	if pgHost == "" { pgHost = "localhost" }
	if pgPort == "" { pgPort = "5432" }
	if pgUser == "" { pgUser = "octor" }

	addr := pgHost + ":" + pgPort

	// Connect to vault database
	vaultDb := pg.Connect(&pg.Options{
		Addr:     addr,
		User:     pgUser,
		Password: pgPass,
		Database: "vault",
	})
	defer vaultDb.Close()

	if _, err := vaultDb.Exec("SELECT 1"); err != nil {
		log.Fatalf("Error: Failed to connect to vault database: %v", err)
	}

	// 1. If live run, prune orphaned DB rows first
	if !*dryRun {
		log.Println("Pruning orphaned rows from database 'file' table...")
		res, err := vaultDb.Exec(`
			DELETE FROM file 
			WHERE hash NOT IN (SELECT DISTINCT file_hash FROM resource_file) 
			  AND updated_at < NOW() - INTERVAL '4 hours'
		`)
		if err != nil {
			log.Printf("Warning: Failed to prune orphaned DB rows: %v", err)
		} else {
			log.Printf("Pruned %d orphaned database rows from file table.", res.RowsAffected())
		}
	}

	// 2. Query active resource IDs
	var activeResources []struct {
		ResourceId string
	}
	_, err := vaultDb.Query(&activeResources, "SELECT resource_id FROM resource;")
	if err != nil {
		log.Fatalf("Error: Failed to query active resources: %v", err)
	}

	activeResourcesSet := make(map[string]bool)
	for _, r := range activeResources {
		activeResourcesSet[strings.ToLower(r.ResourceId)] = true
	}

	// 3. Query active file hashes
	var activeFileHashes []struct {
		Hash string
	}
	_, err = vaultDb.Query(&activeFileHashes, `
		SELECT DISTINCT file_hash AS hash FROM resource_file
		UNION
		SELECT hash FROM file WHERE updated_at >= NOW() - INTERVAL '4 hours';
	`)
	if err != nil {
		log.Fatalf("Error: Failed to query active file hashes: %v", err)
	}

	activeHashesSet := make(map[string]bool)
	for _, h := range activeFileHashes {
		activeHashesSet[strings.ToLower(h.Hash)] = true
	}

	log.Printf("Active Resources in DB: %d", len(activeResourcesSet))
	log.Printf("Active File Hashes in DB (including 4h grace window): %d", len(activeHashesSet))
	log.Println(strings.Repeat("=", 60))

	// Get storage path
	storagePath := os.Getenv("VAULT_STORAGE_PATH")
	if storagePath == "" {
		storagePath = os.Getenv("S3_GATEWAY_STORAGE_DIR")
	}
	if storagePath == "" {
		storagePath = "./infra-data/drive-mount-vfs"
	}

	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		log.Fatalf("Error: Storage path does not exist: %s", storagePath)
	}

	var orphanedMetadataFiles []string
	var orphanedTorrentFiles []string
	var orphanedVaultDirs []string

	// 4. Check recovery metadata (metadata/resources/<id>.json)
	metaDir := filepath.Join(storagePath, "recovery", "metadata", "resources")
	if entries, err := os.ReadDir(metaDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				infohash := strings.ToLower(strings.TrimSuffix(entry.Name(), ".json"))
				if len(infohash) == 40 && !activeResourcesSet[infohash] {
					orphanedMetadataFiles = append(orphanedMetadataFiles, filepath.Join(metaDir, entry.Name()))
				}
			}
		}
	}

	// 5. Check recovery torrents (recovery/torrents/<name> [<id>].torrent)
	torrentDir := filepath.Join(storagePath, "recovery", "torrents")
	hashRe := regexp.MustCompile(`\[([a-fA-F0-9]{40})\]\.torrent$`)
	if entries, err := os.ReadDir(torrentDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".torrent") && !strings.HasPrefix(entry.Name(), ".") {
				match := hashRe.FindStringSubmatch(entry.Name())
				if len(match) == 2 {
					infohash := strings.ToLower(match[1])
					if !activeResourcesSet[infohash] {
						orphanedTorrentFiles = append(orphanedTorrentFiles, filepath.Join(torrentDir, entry.Name()))
					}
				}
			}
		}
	}

	// 6. Check vault files (vault/<hash>/<hash>)
	vaultDir := filepath.Join(storagePath, "vault")
	if entries, err := os.ReadDir(vaultDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && len(entry.Name()) == 40 {
				fileHash := strings.ToLower(entry.Name())
				if !activeHashesSet[fileHash] {
					orphanedVaultDirs = append(orphanedVaultDirs, filepath.Join(vaultDir, entry.Name()))
				}
			}
		}
	}

	// Print summary
	log.Printf("Found %d orphaned metadata files", len(orphanedMetadataFiles))
	log.Printf("Found %d orphaned torrent files", len(orphanedTorrentFiles))
	log.Printf("Found %d orphaned vault directories", len(orphanedVaultDirs))
	log.Println(strings.Repeat("-", 60))

	// Perform cleanup or reporting
	var totalFreedBytes int64

	if len(orphanedMetadataFiles) > 0 {
		log.Println("\nOrphaned Metadata Files:")
		for _, path := range orphanedMetadataFiles {
			log.Printf("  - %s", filepath.Base(path))
			if !*dryRun {
				if err := os.Remove(path); err != nil {
					log.Printf("    [Error deleting]: %v", err)
				}
			}
		}
	}

	if len(orphanedTorrentFiles) > 0 {
		log.Println("\nOrphaned Torrent Files:")
		for _, path := range orphanedTorrentFiles {
			log.Printf("  - %s", filepath.Base(path))
			if !*dryRun {
				if err := os.Remove(path); err != nil {
					log.Printf("    [Error deleting]: %v", err)
				}
			}
		}
	}

	if len(orphanedVaultDirs) > 0 {
		log.Println("\nOrphaned Vault Directories:")
		for _, path := range orphanedVaultDirs {
			var dirSize int64
			_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					dirSize += info.Size()
				}
				return nil
			})
			totalFreedBytes += dirSize

			log.Printf("  - %s (%.2f MB)", filepath.Base(path), float64(dirSize)/(1024*1024))
			if !*dryRun {
				if err := os.RemoveAll(path); err != nil {
					log.Printf("    [Error deleting]: %v", err)
				}
			}
		}
	}

	log.Println("\n" + strings.Repeat("=", 60))
	if *dryRun {
		log.Printf("DRY-RUN COMPLETE. Would delete %d files and %d directories.", len(orphanedMetadataFiles)+len(orphanedTorrentFiles), len(orphanedVaultDirs))
		log.Printf("Estimated space freed: %.2f GB", float64(totalFreedBytes)/(1024*1024*1024))
		log.Println("To apply changes, run with: --dry-run=false")
	} else {
		log.Printf("CLEANUP COMPLETE. Deleted %d files and %d directories.", len(orphanedMetadataFiles)+len(orphanedTorrentFiles), len(orphanedVaultDirs))
		log.Printf("Freed: %.2f GB", float64(totalFreedBytes)/(1024*1024*1024))
	}
	log.Println(strings.Repeat("=", 60))
}
