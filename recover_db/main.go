package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-pg/pg/v10"
)

// fileMeta matches the JSON structure saved by the vault worker
type fileMeta struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type ownershipMeta struct {
	ResourceID  string     `json:"resource_id"`
	TorrentName string     `json:"torrent_name"`
	SessionID   string     `json:"session_id"`
	VaultedAt   string     `json:"vaulted_at"`
	Files       []fileMeta `json:"files"`
}

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
	dryRun := flag.Bool("dry-run", true, "Run without making database changes (safe mode)")
	flag.Parse()

	// Load custom.env if present
	loadEnv("custom.env")
	loadEnv("../custom.env")
	if homeDir, err := os.UserHomeDir(); err == nil {
		loadEnv(filepath.Join(homeDir, "octor/custom.env"))
	}

	log.Println(strings.Repeat("=", 60))
	log.Printf("Starting Octor Database Recovery")
	log.Printf("Mode: DRY-RUN = %v", *dryRun)
	log.Println(strings.Repeat("=", 60))

	// 1. Setup S3
	bucket := os.Getenv("AWS_BUCKET")
	if bucket == "" {
		// Fallback to vault specific env var
		bucket = os.Getenv("VAULT_AWS_BUCKET")
	}
	if bucket == "" {
		log.Fatal("Error: AWS_BUCKET or VAULT_AWS_BUCKET environment variable is required")
	}

	awsConfig := &aws.Config{}
	if endpoint := os.Getenv("AWS_ENDPOINT"); endpoint != "" {
		awsConfig.Endpoint = aws.String(endpoint)
		awsConfig.S3ForcePathStyle = aws.Bool(true)
	}
	if region := os.Getenv("AWS_REGION"); region != "" {
		awsConfig.Region = aws.String(region)
	}

	sess := session.Must(session.NewSession(awsConfig))
	svc := s3.New(sess)

	// 2. Setup Postgres connections (Vault DB and Web-UI DB)
	pgUser := os.Getenv("PG_USER")
	pgPass := os.Getenv("PG_PASSWORD")
	pgHost := os.Getenv("PG_HOST")
	pgPort := os.Getenv("PG_PORT")
	
	if pgHost == "" { pgHost = "localhost" }
	if pgPort == "" { pgPort = "5432" }
	if pgUser == "" { pgUser = "octor" }

	addr := pgHost + ":" + pgPort
	
	// Connect to Vault DB
	vaultDb := pg.Connect(&pg.Options{
		Addr:     addr,
		User:     pgUser,
		Password: pgPass,
		Database: "vault",
	})
	defer vaultDb.Close()
	if _, err := vaultDb.Exec("SELECT 1"); err != nil {
		log.Fatalf("Failed to connect to Vault DB: %v", err)
	}

	// Connect to Web UI (Octor) DB
	octorDb := pg.Connect(&pg.Options{
		Addr:     addr,
		User:     pgUser,
		Password: pgPass,
		Database: "octor",
	})
	defer octorDb.Close()
	if _, err := octorDb.Exec("SELECT 1"); err != nil {
		log.Fatalf("Failed to connect to Octor DB: %v", err)
	}

	// Build user session map (SessionID -> UserID)
	// The metadata stores SessionID which is a SHA1 hash of the User UUID
	userMap := buildUserSessionMap(octorDb)
	log.Printf("Found %d users in Octor database", len(userMap))

	// If there is exactly one user, use it as fallback for session UUID mismatch
	var fallbackUserID string
	if len(userMap) == 1 {
		for _, uid := range userMap {
			fallbackUserID = uid
			break
		}
		log.Printf("Single user detected. Using User ID %s as fallback for all recovered library mappings.", fallbackUserID)
	}

	// Load the admin user ID from DB as the fallback recipient for unmapped files
	var adminUserID string
	_, err := octorDb.QueryOne(pg.Scan(&adminUserID), `SELECT user_id FROM "user" WHERE email = 'admin' LIMIT 1`)
	if err != nil {
		log.Printf("Warning: Failed to fetch admin user ID: %v", err)
	} else {
		log.Printf("Found admin user ID %s to use as fallback for orphaned session metadata", adminUserID)
	}

	// 3. Scan S3 and Rebuild
	log.Printf("Scanning S3 Bucket: %s for torrents...", bucket)

	var recoveredCount int

	// First try local directory scanning using VAULT_STORAGE_PATH or S3_GATEWAY_STORAGE_DIR
	vaultStoragePath := os.Getenv("VAULT_STORAGE_PATH")
	if vaultStoragePath == "" {
		vaultStoragePath = os.Getenv("S3_GATEWAY_STORAGE_DIR")
	}
	if vaultStoragePath == "" {
		vaultStoragePath = "./infra-data/drive-mount-vfs"
	}

	localTorrentsDir := filepath.Join(vaultStoragePath, "recovery", "torrents")

	log.Printf("Checking local torrents directory: %s", localTorrentsDir)
	files, err := os.ReadDir(localTorrentsDir)
	if err == nil {
		log.Printf("Found %d files in local torrents directory, processing concurrently with 30 workers...", len(files))
		
		// Set up worker pool to handle rclone FUSE mount latency in parallel
		numWorkers := 30
		jobs := make(chan string, len(files))
		results := make(chan bool, len(files))
		var wg sync.WaitGroup

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for key := range jobs {
					res := processTorrent(svc, vaultDb, octorDb, userMap, bucket, key, *dryRun, vaultStoragePath, fallbackUserID, adminUserID)
					results <- res
				}
			}()
		}

		// Feed jobs
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			name := file.Name()
			if strings.Contains(name, ".archived/") {
				continue
			}
			if !strings.HasSuffix(name, ".torrent") {
				continue
			}
			jobs <- "torrents/" + name
		}
		close(jobs)

		// Wait for workers in background
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results
		for res := range results {
			if res {
				recoveredCount++
			}
		}
	} else {
		log.Printf("Local scanning failed/unavailable (%v), falling back to S3 ListObjects...", err)
		err = svc.ListObjectsV2Pages(&s3.ListObjectsV2Input{
			Bucket: aws.String("storage"),
			Prefix: aws.String("torrents/"),
		}, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
			for _, obj := range page.Contents {
				key := *obj.Key
				
				// Ignore archived torrents (these were deleted by users)
				if strings.Contains(key, ".archived/") {
					continue
				}
				if !strings.HasSuffix(key, ".torrent") {
					continue
				}
				
				if processTorrent(svc, vaultDb, octorDb, userMap, bucket, key, *dryRun, "", fallbackUserID, adminUserID) {
					recoveredCount++
				}
			}
			return !lastPage
		})

		if err != nil {
			log.Fatalf("Failed to list S3 objects: %v", err)
		}
	}

	log.Println(strings.Repeat("-", 60))
	if *dryRun {
		log.Printf("DRY-RUN COMPLETE. %d resources would be recovered.", recoveredCount)
		log.Printf("To actually apply changes, run:")
		log.Printf("go run scripts/recover_db.go --dry-run=false")
	} else {
		log.Printf("RECOVERY COMPLETE. %d resources successfully restored to database.", recoveredCount)
	}
}

// buildUserSessionMap pre-calculates SHA1 hashes for all users to match against metadata
func buildUserSessionMap(db *pg.DB) map[string]string {
	userMap := make(map[string]string)
	
	type User struct {
		tableName struct{} `pg:"user"`
		UserID    string   `pg:"user_id"`
	}
	var users []User
	
	err := db.Model(&users).Column("user_id").Select()
	if err != nil {
		log.Printf("Warning: Failed to fetch users: %v", err)
		return userMap
	}

	for _, u := range users {
		h := sha1.New()
		h.Write([]byte(u.UserID))
		hash := hex.EncodeToString(h.Sum(nil))
		userMap[hash] = u.UserID
	}
	
	return userMap
}

func processTorrent(svc *s3.S3, vaultDb *pg.DB, octorDb *pg.DB, userMap map[string]string, bucket, key string, dryRun bool, vaultStoragePath string, fallbackUserID string, adminUserID string) bool {
	var raw []byte
	var err error
	localRead := false

	// 1. Load .torrent
	if vaultStoragePath != "" {
		localTorrentPath := filepath.Join(vaultStoragePath, "recovery", key)
		raw, err = os.ReadFile(localTorrentPath)
		if err == nil {
			localRead = true
		}
	}

	if !localRead {
		out, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: aws.String("storage"),
			Key:    aws.String(key),
		})
		if err != nil {
			log.Printf("  [ERROR] Failed to download %s: %v", key, err)
			return false
		}
		defer out.Body.Close()
		raw, _ = io.ReadAll(out.Body)
	}

	// Parse metainfo
	mi, err := metainfo.Load(bytes.NewReader(raw))
	if err != nil {
		log.Printf("  [ERROR] Failed to parse %s: %v", key, err)
		return false
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		log.Printf("  [ERROR] Failed to unmarshal info for %s: %v", key, err)
		return false
	}

	infohash := mi.HashInfoBytes().HexString()
	torrentName := info.Name
	if info.NameUtf8 != "" {
		torrentName = info.NameUtf8
	}
	totalSize := info.TotalLength()
	fileCount := len(info.UpvertedFiles())

	// 2. Fetch Ownership Metadata (Crucial for full recovery)
	var meta ownershipMeta
	hasMetadata := false

	if vaultStoragePath != "" {
		localMetaPath := filepath.Join(vaultStoragePath, "recovery", "metadata", "resources", infohash+".json")
		metaRaw, err := os.ReadFile(localMetaPath)
		if err == nil {
			if err := json.Unmarshal(metaRaw, &meta); err == nil {
				hasMetadata = true
			}
		}
	}

	if !hasMetadata {
		metaKey := fmt.Sprintf("metadata/resources/%s.json", infohash)
		metaOut, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: aws.String("storage"),
			Key:    aws.String(metaKey),
		})
		if err == nil {
			defer metaOut.Body.Close()
			if err := json.NewDecoder(metaOut.Body).Decode(&meta); err == nil {
				hasMetadata = true
			}
		}
	}

	if !hasMetadata {
		log.Printf("  [WARN] Skipping %s: No metadata JSON found (cannot recover hashes or ownership)", infohash)
		return false
	}

	// Determine if the resource existed in the original database
	var inLibrary bool
	if _, err := octorDb.QueryOne(pg.Scan(&inLibrary), `SELECT EXISTS(SELECT 1 FROM library WHERE resource_id = ?)`, infohash); err != nil {
		log.Printf("  [ERROR] Failed to query library existence for %s: %v", infohash, err)
	}
	var inVault bool
	if _, err := vaultDb.QueryOne(pg.Scan(&inVault), `SELECT EXISTS(SELECT 1 FROM resource WHERE resource_id = ?)`, infohash); err != nil {
		log.Printf("  [ERROR] Failed to query vault resource existence for %s: %v", infohash, err)
	}

	// Determine User ID from Session ID (used if we need to fall back or verify new active uploads)
	userID := userMap[meta.SessionID]
	if userID == "" && fallbackUserID != "" {
		userID = fallbackUserID
	}

	if !inLibrary && !inVault {
		// If it has a valid session, it is a newly added active torrent. Otherwise, it is a deleted leaked orphan.
		if userID == "" {
			log.Printf("  [INFO] Skipping %s: Not present in library or vault backup and no active session (leaked orphan)", infohash)
			return false
		}
	}

	if inLibrary {
		// It is already in the library, so we don't need to insert another library entry.
		// (The PG dump has already restored the user's library mapping cleanly)
		userID = ""
	} else if inVault {
		// It was vaulted-only (inVault is true, inLibrary is false).
		// We do NOT map it to any user's library, maintaining its vaulted-only state.
		userID = ""
	} else if userID == "" && adminUserID != "" {
		// Fallback to the admin user so it gets listed in a user's library and isn't orphaned
		userID = adminUserID
	}

	// If it's not in the library or vault table, check if it belongs to an active session
	if userID == "" && !inVault && !inLibrary {
		log.Printf("  [INFO] Skipping %s: Not present in library or vault and no active session (leaked orphan)", infohash)
		return false
	}

	// VERIFICATION: Ensure all files for this torrent actually exist in the vault directory!
	if vaultStoragePath != "" {
		allExist := true
		for _, f := range meta.Files {
			if f.Hash == "" {
				continue
			}
			localFilePath := filepath.Join(vaultStoragePath, "vault", f.Hash, f.Hash)
			if _, err := os.Stat(localFilePath); err != nil {
				allExist = false
				break
			}
		}
		if !allExist {
			log.Printf("  [INFO] Skipping %s: One or more files are missing from the vault (likely deleted)", infohash)
			return false
		}
	}

	log.Printf("Recovering: %s (%s)", infohash, torrentName)
	if userID != "" {
		log.Printf("  -> Assigning to User ID: %s", userID)
	}

	if dryRun {
		return true
	}

	// --- VAULT DATABASE RECOVERY ---

	// Insert into vault.resource
	_, err = vaultDb.Exec(`
		INSERT INTO resource (resource_id, status, total_size, stored_size, created_at, updated_at) 
		VALUES (?, 2, ?, ?, now(), now()) ON CONFLICT DO NOTHING`,
		infohash, totalSize, totalSize)
	if err != nil {
		log.Printf("  [ERROR] Vault DB: Failed to insert resource %s: %v", infohash, err)
		return false
	}

	// Insert into vault.file and vault.resource_file
	for _, f := range meta.Files {
		if f.Hash == "" { continue }
		
		// Insert file record
		_, err = vaultDb.Exec(`
			INSERT INTO file (hash, status, total_size, stored_size, created_at, updated_at)
			VALUES (?, 2, 0, 0, now(), now()) ON CONFLICT DO NOTHING`,
			f.Hash)
		if err != nil {
			log.Printf("  [ERROR] Vault DB: Failed to insert file %s: %v", f.Hash, err)
		}

		// Insert mapping - this supports BOTH legacy hashes and new human paths
		_, err = vaultDb.Exec(`
			INSERT INTO resource_file (resource_id, file_hash, path)
			VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			infohash, f.Hash, f.Path)
		if err != nil {
			log.Printf("  [ERROR] Vault DB: Failed to insert resource_file mapping: %v", err)
		}
	}

	// --- OCTOR (WEB-UI) DATABASE RECOVERY ---

	// Insert into octor.torrent_resource
	_, err = octorDb.Exec(`
		INSERT INTO torrent_resource (resource_id, name, file_count, size_bytes, created_at, torrent_size_bytes) 
		VALUES (?, ?, ?, ?, now(), ?) ON CONFLICT DO NOTHING`,
		infohash, torrentName, fileCount, totalSize, len(raw))
	if err != nil {
		log.Printf("  [ERROR] Octor DB: Failed to insert torrent_resource %s: %v", infohash, err)
		return false
	}

	// Insert into octor.library (Assign to User)
	if userID != "" {
		_, err = octorDb.Exec(`
			INSERT INTO library (user_id, resource_id, created_at, name)
			VALUES (?, ?, now(), ?) ON CONFLICT DO NOTHING`,
			userID, infohash, torrentName)
		if err != nil {
			log.Printf("  [ERROR] Octor DB: Failed to add to user library: %v", err)
		}
	}

	return true
}
