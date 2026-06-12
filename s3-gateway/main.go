package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	cs "github.com/webtor-io/common-services"
)

func getInfraDataPath(subpath string) string {
	return cs.GetInfraDataPath(subpath)
}

var (
	storageDir     = getInfraDataPath("drive-mount-vfs")
	indexDriveDir  = getInfraDataPath("drive1-index")
	tempUploadsDir = "/tmp/octor-s3-uploads"
	port           = ":9000"
	uploadPartSize = int64(32 * 1024 * 1024) // Default to 32MB multipart size
	humanReadable  = false                  // Default to original hash-based storage
)


// S3 Error response
type s3Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

func writeError(w http.ResponseWriter, status int, code, message, resource string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	errObj := s3Error{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestID: "1234567890",
	}
	_ = xml.NewEncoder(w).Encode(errObj)
}

func generateUploadID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type partInfo struct {
	partNumber int
	size       int64
	etag       string
}

type activeUpload struct {
	uploadID      string
	bucket        string
	key           string
	finalPath     string
	humanPath     string
	destFile      *os.File
	writer        *bufio.Writer
	tempDir       string

	mu            sync.Mutex
	cond          *sync.Cond
	partsReceived map[int]string // partNum -> tempFilePath
	expectedParts []int          // List of parts to flush
	completed     bool
	aborted       bool
	doneFlushing  chan struct{}
	flushErr      error
	nextPart      int
	totalBytes    int64
	totalFlushed  int64
	partsMetadata []partInfo // For ListParts
	partSize      int64
}

var (
	mu              sync.Mutex
	activeUploads   = make(map[string]*activeUpload)
	writeBufferSize = 16 * 1024 * 1024       // Default to 16MB sequential write buffer
	maxStagingBytes = int64(512 * 1024 * 1024) // 512MB default max staging buffer
)

func createLink(finalPath string, humanPath string) {
	// Links are disabled due to FUSE mount restrictions. 
	// We handle this by uploading directly to the human path in handleS3.
}

func main() {
	_ = mime.AddExtensionType(".mkv", "video/x-matroska")

	if envStorageDir := os.Getenv("S3_GATEWAY_STORAGE_DIR"); envStorageDir != "" {
		storageDir = envStorageDir
		log.Printf("Using S3_GATEWAY_STORAGE_DIR from environment: %s", storageDir)
	} else {
		log.Printf("S3_GATEWAY_STORAGE_DIR not set, defaulting to %s", storageDir)
	}

	if err := os.MkdirAll(storageDir, 0777); err != nil {
		log.Fatalf("failed to create storage dir: %v", err)
	}

	if envTempDir := os.Getenv("S3_GATEWAY_TEMP_UPLOADS_DIR"); envTempDir != "" {
		tempUploadsDir = envTempDir
		log.Printf("Using S3_GATEWAY_TEMP_UPLOADS_DIR from environment: %s", tempUploadsDir)
	} else {
		log.Printf("S3_GATEWAY_TEMP_UPLOADS_DIR not set, defaulting to %s", tempUploadsDir)
	}

	// Wipe any pre-existing files/directories in tempUploadsDir on startup
	if err := os.RemoveAll(tempUploadsDir); err != nil {
		log.Printf("Warning: failed to clean up temp uploads directory %s on startup: %v", tempUploadsDir, err)
	}

	if err := os.MkdirAll(tempUploadsDir, 0777); err != nil {
		log.Fatalf("failed to create temp uploads dir: %v", err)
	}

	if envMaxStaging := os.Getenv("S3_GATEWAY_MAX_STAGING_SIZE"); envMaxStaging != "" {
		if val, err := strconv.ParseInt(envMaxStaging, 10, 64); err == nil {
			maxStagingBytes = val
			log.Printf("Using S3_GATEWAY_MAX_STAGING_SIZE from environment: %d bytes", maxStagingBytes)
		} else {
			log.Printf("Invalid S3_GATEWAY_MAX_STAGING_SIZE '%s', defaulting to %d bytes", envMaxStaging, maxStagingBytes)
		}
	}

	if os.Getenv("S3_GATEWAY_HUMAN_READABLE") == "true" {
		humanReadable = true
		log.Printf("[S3] HUMAN_READABLE mode ENABLED: Files will be stored with torrent names.")
	} else {
		log.Printf("[S3] HUMAN_READABLE mode DISABLED: Reverting to default hash-based storage.")
	}

	if envBufSize := os.Getenv("S3_GATEWAY_WRITE_BUFFER_SIZE"); envBufSize != "" {
		if val, err := strconv.Atoi(envBufSize); err == nil {
			writeBufferSize = val
			log.Printf("Using S3_GATEWAY_WRITE_BUFFER_SIZE from environment: %d bytes", writeBufferSize)
		} else {
			log.Printf("Invalid S3_GATEWAY_WRITE_BUFFER_SIZE '%s', defaulting to %d bytes", envBufSize, writeBufferSize)
		}
	} else {
		log.Printf("S3_GATEWAY_WRITE_BUFFER_SIZE not set, defaulting to %d bytes", writeBufferSize)
	}

	if envPartSize := os.Getenv("AWS_UPLOAD_PART_SIZE"); envPartSize != "" {
		if val, err := strconv.ParseInt(envPartSize, 10, 64); err == nil {
			uploadPartSize = val
			log.Printf("Using AWS_UPLOAD_PART_SIZE from environment: %d bytes", uploadPartSize)
		} else {
			log.Printf("Invalid AWS_UPLOAD_PART_SIZE '%s', defaulting to %d bytes", envPartSize, uploadPartSize)
		}
	} else {
		log.Printf("AWS_UPLOAD_PART_SIZE not set, defaulting to %d bytes", uploadPartSize)
	}

	if envPort := os.Getenv("PORT"); envPort != "" {
		if strings.HasPrefix(envPort, ":") {
			port = envPort
		} else {
			port = ":" + envPort
		}
		log.Printf("Using PORT from environment: %s", port)
	}

	http.HandleFunc("/", handleS3)

	log.Printf("Starting Zero-Buffer Direct-Stream S3 Gateway on port %s...", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func (u *activeUpload) flushLoop() {
	defer close(u.doneFlushing)

	for {
		u.mu.Lock()
		for {
			if u.aborted {
				u.mu.Unlock()
				return
			}
			if u.flushErr != nil {
				u.mu.Unlock()
				return
			}

			// If completed and we have flushed all expected parts, we are done
			if u.completed {
				allFlushed := true
				for _, partNum := range u.expectedParts {
					if partNum >= u.nextPart {
						allFlushed = false
						break
					}
				}
				if allFlushed {
					u.mu.Unlock()
					return
				}
			}

			// Check if the next part has been received
			_, exists := u.partsReceived[u.nextPart]
			if exists {
				break
			}

			// Wait for a new part or completion signal
			u.cond.Wait()
		}

		partNum := u.nextPart
		partPath := u.partsReceived[partNum]
		u.mu.Unlock()

		// Perform the flush without holding the lock
		err := u.flushPart(partNum, partPath)

		u.mu.Lock()
		if err != nil {
			u.flushErr = err
			u.cond.Broadcast() // Wake up blocked writers
			u.mu.Unlock()
			return
		}
		u.nextPart++
		u.cond.Broadcast() // Wake up CompleteMultipartUpload and blocked writers
		u.mu.Unlock()
	}
}

func (u *activeUpload) flushPart(partNum int, partPath string) error {
	srcFile, err := os.Open(partPath)
	if err != nil {
		return fmt.Errorf("failed to open temp part %d: %v", partNum, err)
	}
	defer srcFile.Close()

	var written int64
	if u.writer != nil {
		written, err = io.Copy(u.writer, srcFile)
	} else {
		written, err = io.Copy(u.destFile, srcFile)
	}
	if err != nil {
		return fmt.Errorf("failed to write part %d to Google Drive: %v", partNum, err)
	}

	srcFile.Close()
	_ = os.Remove(partPath)

	// Update size and etag in activeUpload metadata
	u.mu.Lock()
	u.totalFlushed += written
	etag := fmt.Sprintf("\"part-%d-etag\"", partNum)
	u.partsMetadata = append(u.partsMetadata, partInfo{
		partNumber: partNum,
		size:       written,
		etag:       etag,
	})
	u.mu.Unlock()

	log.Printf("[S3] Flushed Part %d to Google Drive: Size=%d bytes", partNum, written)
	return nil
}

func checkParallelMode() bool {
	if os.Getenv("RCLONE_VFS_CACHE_MODE") != "writes" {
		return false
	}
	// Chunker overlay does not support random-access WriteAt out-of-order writes (throws illegal seek).
	storageDir := os.Getenv("S3_GATEWAY_STORAGE_DIR")
	if strings.Contains(storageDir, "drive-mount") && !strings.Contains(storageDir, "drive-mount-vfs") {
		return false
	}
	return true
}

func writeAt(f *os.File, r io.Reader, off int64) (int64, error) {
	var total int64
	buf := make([]byte, 32*1024) // 32KB copy buffer
	for {
		n, rErr := r.Read(buf)
		if n > 0 {
			wN, wErr := f.WriteAt(buf[:n], off+total)
			total += int64(wN)
			if wErr != nil {
				return total, wErr
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			return total, rErr
		}
	}
	return total, nil
}

func handleS3(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 && parts[0] != "" {
		bucket := parts[0]
		if bucket == "favicon.ico" || bucket == "robots.txt" || strings.HasPrefix(bucket, "apple-touch-icon") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	if len(parts) == 0 || parts[0] == "" {
		// List buckets mock
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
  <Buckets>
    <Bucket><Name>vault</Name></Bucket>
    <Bucket><Name>torrent-store</Name></Bucket>
    <Bucket><Name>poster-cache</Name></Bucket>
    <Bucket><Name>storage</Name></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
		return
	}

	bucket := parts[0]
	var key string
	if len(parts) == 2 {
		key = parts[1]
	}

	// New Organized Mapping Logic
	var bucketDir string
	switch bucket {
	case "vault":
		bucketDir = filepath.Join(storageDir, "vault")
	case "torrent-store":
		bucketDir = filepath.Join(storageDir, "data", "torrents_system")
	case "poster-cache":
		bucketDir = filepath.Join(filepath.Dir(storageDir), "postercache")
	case "storage":
		bucketDir = filepath.Join(storageDir, "recovery")
	default:
		bucketDir = filepath.Join(storageDir, bucket)
	}

	if err := os.MkdirAll(bucketDir, 0777); err != nil {
		writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
		return
	}

	query := r.URL.Query()
	_, isUploads := query["uploads"]
	uploadID := query.Get("uploadId")
	partNumberStr := query.Get("partNumber")

	// 1. Create Multipart Upload
	if isUploads && r.Method == http.MethodPost {
		upID := generateUploadID()
		
		// If human path is provided, use it as the actual filename on the cloud drive
		humanPath := r.Header.Get("X-Amz-Meta-Human-Path")
		finalPath := filepath.Join(bucketDir, key)
		if humanReadable && humanPath != "" {
			finalPath = filepath.Join(storageDir, humanPath)
			log.Printf("[S3] Direct Human Upload: Mapping %s/%s -> %s", bucket, key, finalPath)
		}

		if err := os.MkdirAll(filepath.Dir(finalPath), 0777); err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}
		destFile, err := os.OpenFile(finalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}

		var writer *bufio.Writer
		if writeBufferSize > 0 && !checkParallelMode() {
			writer = bufio.NewWriterSize(destFile, writeBufferSize)
			log.Printf("[S3] Wrapping upload with sequential RAM write buffer: size=%d bytes", writeBufferSize)
		}

		var tempDir string
		if !checkParallelMode() {
			tempDir = filepath.Join(tempUploadsDir, upID)
			if err := os.MkdirAll(tempDir, 0777); err != nil {
				destFile.Close()
				writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Failed to create temp uploads dir: %v", err), r.URL.Path)
				return
			}
		}

		u := &activeUpload{
			uploadID:      upID,
			bucket:        bucket,
			key:           key,
			finalPath:     finalPath,
			humanPath:     r.Header.Get("X-Amz-Meta-Human-Path"),
			destFile:      destFile,
			writer:        writer,
			tempDir:       tempDir,
			partsReceived: make(map[int]string),
			doneFlushing:  make(chan struct{}),
			nextPart:      1,
			partSize:      uploadPartSize,
		}
		u.cond = sync.NewCond(&u.mu)

		mu.Lock()
		activeUploads[upID] = u
		mu.Unlock()

		if !checkParallelMode() {
			go u.flushLoop()
		} else {
			close(u.doneFlushing)
		}

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, bucket, key, upID)
		log.Printf("[S3] Initiated Async Multipart Stream: Bucket=%s, Key=%s, UploadId=%s", bucket, key, upID)
		return
	}

	// 2. Upload Part
	if uploadID != "" && partNumberStr != "" && r.Method == http.MethodPut {
		partNum, err := strconv.Atoi(partNumberStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidArgument", "Invalid part number", r.URL.Path)
			return
		}

		mu.Lock()
		u, ok := activeUploads[uploadID]
		mu.Unlock()

		if !ok {
			writeError(w, http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist.", r.URL.Path)
			return
		}

		if checkParallelMode() {
			u.mu.Lock()
			if r.ContentLength > 0 {
				if partNum == 1 {
					u.partSize = r.ContentLength
				} else if u.partSize == uploadPartSize && r.ContentLength > 5*1024*1024 {
					u.partSize = r.ContentLength
				}
			}
			partSizeToUse := u.partSize
			u.mu.Unlock()

			offset := int64(partNum - 1) * partSizeToUse
			written, err := writeAt(u.destFile, r.Body, offset)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("failed to write parallel part data: %v", err), r.URL.Path)
				return
			}

			u.mu.Lock()
			u.partsReceived[partNum] = "" // Mark as received
			u.totalBytes += written
			u.totalFlushed += written
			etag := fmt.Sprintf("\"part-%d-etag\"", partNum)
			u.partsMetadata = append(u.partsMetadata, partInfo{
				partNumber: partNum,
				size:       written,
				etag:       etag,
			})
			u.cond.Broadcast() // Wake up CompleteMultipartUpload
			u.mu.Unlock()

			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusOK)
			log.Printf("[S3] Direct parallel write Part %d: Size=%d bytes", partNum, written)
			return
		}

		u.mu.Lock()
		if u.flushErr != nil {
			flushErr := u.flushErr
			u.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Background flusher failed: %v", flushErr), r.URL.Path)
			return
		}
		if u.aborted {
			u.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "InternalError", "Upload aborted", r.URL.Path)
			return
		}

		// Apply backpressure if pending bytes exceed maxStagingBytes
		for u.totalBytes-u.totalFlushed > maxStagingBytes {
			if u.aborted || u.flushErr != nil {
				break
			}
			log.Printf("[S3] Staging buffer full (%d bytes pending), applying backpressure...", u.totalBytes-u.totalFlushed)
			u.cond.Wait()
		}

		if u.flushErr != nil {
			flushErr := u.flushErr
			u.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Background flusher failed: %v", flushErr), r.URL.Path)
			return
		}
		if u.aborted {
			u.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "InternalError", "Upload aborted", r.URL.Path)
			return
		}
		u.mu.Unlock()

		partPath := filepath.Join(u.tempDir, fmt.Sprintf("part-%d", partNum))
		partFile, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("failed to create temp part file: %v", err), r.URL.Path)
			return
		}

		written, err := io.Copy(partFile, r.Body)
		partFile.Close()
		if err != nil {
			_ = os.Remove(partPath)
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("failed to write temp part data: %v", err), r.URL.Path)
			return
		}

		u.mu.Lock()
		u.partsReceived[partNum] = partPath
		u.totalBytes += written
		u.cond.Broadcast() // Wake up flushLoop and any blocked writers
		u.mu.Unlock()

		etag := fmt.Sprintf("\"part-%d-etag\"", partNum)
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Staged Part %d in RAM: Size=%d bytes", partNum, written)
		return
	}

	// 3. Complete Multipart Upload
	if uploadID != "" && r.Method == http.MethodPost {
		mu.Lock()
		u, ok := activeUploads[uploadID]
		mu.Unlock()

		if !ok {
			writeError(w, http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist.", r.URL.Path)
			return
		}

		// Parse the part list to know exactly what parts to expect
		type CompleteMultipartUpload struct {
			Parts []struct {
				PartNumber int    `xml:"PartNumber"`
				ETag       string `xml:"ETag"`
			} `xml:"Part"`
		}
		var cmp CompleteMultipartUpload
		if err := xml.NewDecoder(r.Body).Decode(&cmp); err != nil {
			writeError(w, http.StatusBadRequest, "MalformedXML", err.Error(), r.URL.Path)
			return
		}

		u.mu.Lock()
		if !checkParallelMode() && u.flushErr != nil {
			flushErr := u.flushErr
			u.mu.Unlock()
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Background flusher failed: %v", flushErr), r.URL.Path)
			return
		}

		// Extract expected parts in sorted order
		u.expectedParts = make([]int, len(cmp.Parts))
		for i, p := range cmp.Parts {
			u.expectedParts[i] = p.PartNumber
		}
		u.completed = true
		u.cond.Broadcast() // Wake up flusher if sequential

		if checkParallelMode() {
			// Wait for all expected parts to be received
			for {
				allReceived := true
				for _, partNum := range u.expectedParts {
					if _, exists := u.partsReceived[partNum]; !exists {
						allReceived = false
						break
					}
				}
				if allReceived {
					break
				}
				log.Printf("[S3] Waiting for all parallel parts to be received...")
				u.cond.Wait()
			}
			u.mu.Unlock()
		} else {
			u.mu.Unlock()
			// Wait for the background flusher to finish
			log.Printf("[S3] Waiting for background flusher to finish writing remaining parts to Google Drive...")
			<-u.doneFlushing

			u.mu.Lock()
			if u.flushErr != nil {
				flushErr := u.flushErr
				u.mu.Unlock()
				writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Background flusher failed: %v", flushErr), r.URL.Path)
				return
			}
			u.mu.Unlock()
		}

		u.mu.Lock()
		totalSize := u.totalBytes
		u.mu.Unlock()

		// Remove from active uploads map
		mu.Lock()
		delete(activeUploads, uploadID)
		mu.Unlock()

		// Flush and close the final file on Google Drive
		if u.writer != nil {
			if err := u.writer.Flush(); err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Failed to flush file buffers: %v", err), r.URL.Path)
				return
			}
		}
		err := u.destFile.Close()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Failed to finalize file on Google Drive: %v", err), r.URL.Path)
			return
		}

		// Clean up staging directory
		_ = os.RemoveAll(u.tempDir)

		createLink(u.finalPath, u.humanPath)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
  <Location>http://localhost:9000/%s/%s</Location>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <ETag>"completed-etag"</ETag>
</CompleteMultipartUploadResult>`, bucket, key, bucket, key)
		log.Printf("[S3] Completed Async Multipart Stream: Bucket=%s, Key=%s, TotalSize=%d bytes", bucket, key, totalSize)
		return
	}

	// 4. Abort Multipart Upload
	if uploadID != "" && r.Method == http.MethodDelete {
		mu.Lock()
		u, ok := activeUploads[uploadID]
		if ok {
			delete(activeUploads, uploadID)
			u.aborted = true
			u.cond.Broadcast()
			_ = u.destFile.Close()
			_ = os.Remove(u.finalPath)
			_ = os.RemoveAll(u.tempDir)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		log.Printf("[S3] Aborted Multipart Stream: Bucket=%s, Key=%s, UploadId=%s", bucket, key, uploadID)
		return
	}

	// 5. List Parts (Resume support)
	if uploadID != "" && r.Method == http.MethodGet {
		mu.Lock()
		u, ok := activeUploads[uploadID]
		if !ok {
			mu.Unlock()
			writeError(w, http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist.", r.URL.Path)
			return
		}
		partsCopy := make([]partInfo, len(u.partsMetadata))
		copy(partsCopy, u.partsMetadata)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListPartsResult>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <UploadId>%s</UploadId>
  <IsTruncated>false</IsTruncated>`, bucket, key, uploadID)

		for _, p := range partsCopy {
			_, _ = fmt.Fprintf(w, `
  <Part>
    <PartNumber>%d</PartNumber>
    <ETag>%s</ETag>
    <Size>%d</Size>
  </Part>`, p.partNumber, p.etag, p.size)
		}
		_, _ = w.Write([]byte("\n</ListPartsResult>"))
		return
	}

	// 6. Head Object (Check if file exists)
	if r.Method == http.MethodHead {
		finalPath := filepath.Join(bucketDir, key)
		info, err := os.Stat(finalPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", r.URL.Path)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
		w.Header().Set("ETag", "\"completed-etag\"")

		// Force Content-Type based on extension for HEAD requests
		ext := filepath.Ext(key)
		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)

		w.WriteHeader(http.StatusOK)
		return
	}

	// 7. Delete Object
	if r.Method == http.MethodDelete {
		finalPath := filepath.Join(bucketDir, key)
		_ = os.RemoveAll(finalPath)
		w.WriteHeader(http.StatusNoContent)
		log.Printf("[S3] Deleted Object: Bucket=%s, Key=%s", bucket, key)
		return
	}

	// 9. Get Object
	if r.Method == http.MethodGet {
		finalPath := filepath.Join(bucketDir, key)
		file, err := os.Open(finalPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", r.URL.Path)
			return
		}
		defer file.Close()
		info, _ := file.Stat()
		w.Header().Set("ETag", "\"completed-etag\"")

		// Force explicit content headers if requested (prevents MKV->WebM sniffing)
		if ct := r.URL.Query().Get("response-content-type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if cd := r.URL.Query().Get("response-content-disposition"); cd != "" {
			w.Header().Set("Content-Disposition", cd)
		}

		http.ServeContent(w, r, key, info.ModTime(), file)
		return
	}

	// 8. Put Object (Single part upload fallback) or Copy Object
	if r.Method == http.MethodPut {
		copySource := r.Header.Get("x-amz-copy-source")
		humanPath := r.Header.Get("X-Amz-Meta-Human-Path")
		finalPath := filepath.Join(bucketDir, key)
		if humanReadable && humanPath != "" {
			finalPath = filepath.Join(storageDir, humanPath)
		}

		if err := os.MkdirAll(filepath.Dir(finalPath), 0777); err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}
		destFile, err := os.OpenFile(finalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}
		defer destFile.Close()

		var written int64
		if copySource != "" {
			// Handle S3 CopyObject request
			copySource = strings.TrimPrefix(copySource, "/")
			decodedSource, err := url.PathUnescape(copySource)
			if err != nil {
				writeError(w, http.StatusBadRequest, "InvalidURI", err.Error(), r.URL.Path)
				return
			}
			parts := strings.SplitN(decodedSource, "/", 2)
			if len(parts) != 2 {
				writeError(w, http.StatusBadRequest, "InvalidArgument", "Invalid x-amz-copy-source format. Must be /bucket/key", r.URL.Path)
				return
			}
			srcBucket := parts[0]
			srcKey := parts[1]

			var srcBucketDir string
			switch srcBucket {
			case "vault":
				srcBucketDir = filepath.Join(storageDir, "vault")
			case "torrent-store":
				srcBucketDir = filepath.Join(storageDir, "data", "torrents_system")
			case "poster-cache":
				srcBucketDir = filepath.Join(filepath.Dir(storageDir), "postercache")
			case "storage":
				srcBucketDir = filepath.Join(storageDir, "recovery")
			default:
				srcBucketDir = filepath.Join(storageDir, srcBucket)
			}
			srcFilePath := filepath.Join(srcBucketDir, srcKey)

			srcFile, err := os.Open(srcFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					writeError(w, http.StatusNotFound, "NoSuchKey", "The specified copy source key does not exist.", r.URL.Path)
				} else {
					writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
				}
				return
			}
			defer srcFile.Close()

			if writeBufferSize > 0 {
				bw := bufio.NewWriterSize(destFile, writeBufferSize)
				written, err = io.Copy(bw, srcFile)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
					return
				}
				if err := bw.Flush(); err != nil {
					writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
					return
				}
			} else {
				written, err = io.Copy(destFile, srcFile)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
					return
				}
			}

			createLink(finalPath, r.Header.Get("X-Amz-Meta-Human-Path"))

			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("ETag", "\"completed-etag\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<CopyObjectResult>
  <ETag>"completed-etag"</ETag>
</CopyObjectResult>`))
			log.Printf("[S3] Copy Object: Source=%s/%s -> DestBucket=%s, Key=%s, Size=%d bytes", srcBucket, srcKey, bucket, key, written)
			return
		}

		// Regular Put Object (Single part upload fallback)
		if writeBufferSize > 0 {
			bw := bufio.NewWriterSize(destFile, writeBufferSize)
			written, err = io.Copy(bw, r.Body)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
				return
			}
			if err := bw.Flush(); err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
				return
			}
		} else {
			written, err = io.Copy(destFile, r.Body)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
				return
			}
		}

		createLink(finalPath, r.Header.Get("X-Amz-Meta-Human-Path"))

		w.Header().Set("ETag", "\"completed-etag\"")
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Put Object: Bucket=%s, Key=%s, Size=%d bytes", bucket, key, written)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Method not allowed", r.URL.Path)
}
