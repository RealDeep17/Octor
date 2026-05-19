package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	storageDir = "/srv/octor/infra-data/drive-mount"
	port       = ":9000"
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
	uploadID   string
	bucket     string
	key        string
	finalPath  string
	destFile   *os.File
	writer     *bufio.Writer
	nextPart   int
	parts      []partInfo
	totalBytes int64
}

var (
	mu              sync.Mutex
	activeUploads   = make(map[string]*activeUpload)
	writeBufferSize = 16 * 1024 * 1024 // Default to 16MB sequential write buffer
)

func main() {
	if err := os.MkdirAll(storageDir, 0777); err != nil {
		log.Fatalf("failed to create storage dir: %v", err)
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

	http.HandleFunc("/", handleS3)

	log.Printf("Starting Zero-Buffer Direct-Stream S3 Gateway on port %s...", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func handleS3(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		// List buckets mock
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
  <Buckets>
    <Bucket><Name>vault</Name></Bucket>
    <Bucket><Name>torrent-store</Name></Bucket>
    <Bucket><Name>poster-cache</Name></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
		return
	}

	bucket := parts[0]
	var key string
	if len(parts) == 2 {
		key = parts[1]
	}

	bucketDir := filepath.Join(storageDir, bucket)
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
		finalPath := filepath.Join(bucketDir, key)
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
		if writeBufferSize > 0 {
			writer = bufio.NewWriterSize(destFile, writeBufferSize)
			log.Printf("[S3] Wrapping upload with sequential RAM write buffer: size=%d bytes", writeBufferSize)
		}

		mu.Lock()
		activeUploads[upID] = &activeUpload{
			uploadID:  upID,
			bucket:    bucket,
			key:       key,
			finalPath: finalPath,
			destFile:  destFile,
			writer:    writer,
			nextPart:  1,
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, bucket, key, upID)
		log.Printf("[S3] Initiated Zero-Buffer Multipart Stream: Bucket=%s, Key=%s, UploadId=%s", bucket, key, upID)
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

		mu.Lock()
		if partNum != u.nextPart {
			mu.Unlock()
			writeError(w, http.StatusBadRequest, "InvalidPart", fmt.Sprintf("Expected sequential part %d but got %d", u.nextPart, partNum), r.URL.Path)
			return
		}
		mu.Unlock()

		var written int64
		if u.writer != nil {
			written, err = io.Copy(u.writer, r.Body)
		} else {
			written, err = io.Copy(u.destFile, r.Body)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}

		etag := fmt.Sprintf("\"part-%d-etag\"", partNum)

		mu.Lock()
		u.parts = append(u.parts, partInfo{
			partNumber: partNum,
			size:       written,
			etag:       etag,
		})
		u.totalBytes += written
		u.nextPart++
		mu.Unlock()

		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Streamed Part %d directly to Google Drive: Bucket=%s, Key=%s, Size=%d bytes", partNum, bucket, key, written)
		return
	}

	// 3. Complete Multipart Upload
	if uploadID != "" && r.Method == http.MethodPost {
		mu.Lock()
		u, ok := activeUploads[uploadID]
		if !ok {
			mu.Unlock()
			writeError(w, http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist.", r.URL.Path)
			return
		}
		delete(activeUploads, uploadID)
		mu.Unlock()

		totalSize := u.totalBytes
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

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
  <Location>http://localhost:9000/%s/%s</Location>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <ETag>"completed-etag"</ETag>
</CompleteMultipartUploadResult>`, bucket, key, bucket, key)
		log.Printf("[S3] Completed Zero-Buffer Multipart Stream: Bucket=%s, Key=%s, TotalSize=%d bytes", bucket, key, totalSize)
		return
	}

	// 4. Abort Multipart Upload
	if uploadID != "" && r.Method == http.MethodDelete {
		mu.Lock()
		u, ok := activeUploads[uploadID]
		if ok {
			delete(activeUploads, uploadID)
			_ = u.destFile.Close()
			_ = os.Remove(u.finalPath)
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
		partsCopy := make([]partInfo, len(u.parts))
		copy(partsCopy, u.parts)
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
		w.WriteHeader(http.StatusOK)
		return
	}

	// 7. Delete Object
	if r.Method == http.MethodDelete {
		finalPath := filepath.Join(bucketDir, key)
		_ = os.Remove(finalPath)
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
		http.ServeContent(w, r, key, info.ModTime(), file)
		return
	}

	// 8. Put Object (Single part upload fallback)
	if r.Method == http.MethodPut {
		finalPath := filepath.Join(bucketDir, key)
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

		w.Header().Set("ETag", "\"completed-etag\"")
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Put Object: Bucket=%s, Key=%s, Size=%d bytes", bucket, key, written)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Method not allowed", r.URL.Path)
}
