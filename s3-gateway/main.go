package main

import (
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
)

const (
	storageDir = "/srv/octor/drive-mount"
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

func main() {
	if err := os.MkdirAll(storageDir, 0777); err != nil {
		log.Fatalf("failed to create storage dir: %v", err)
	}

	http.HandleFunc("/", handleS3)

	log.Printf("Starting Custom Zero-Leak S3 Gateway on port %s...", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

func handleS3(w http.ResponseWriter, r *http.Request) {
	// Parse bucket and key
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
		// Ensure metadata dir for this upload exists
		metaDir := filepath.Join(bucketDir, ".uploads", key, upID)
		if err := os.MkdirAll(metaDir, 0777); err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <UploadId>%s</UploadId>
</InitiateMultipartUploadResult>`, bucket, key, upID)
		log.Printf("[S3] Initiated Multipart Upload: Bucket=%s, Key=%s, UploadId=%s", bucket, key, upID)
		return
	}

	// 2. Upload Part
	if uploadID != "" && partNumberStr != "" && r.Method == http.MethodPut {
		partNum, err := strconv.Atoi(partNumberStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "InvalidArgument", "Invalid part number", r.URL.Path)
			return
		}

		metaDir := filepath.Join(bucketDir, ".uploads", key, uploadID)
		partPath := filepath.Join(metaDir, fmt.Sprintf("part-%d", partNum))

		partFile, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}
		defer partFile.Close()

		written, err := io.Copy(partFile, r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}

		w.Header().Set("ETag", fmt.Sprintf("\"part-%d-etag\"", partNum))
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Uploaded Part %d: Bucket=%s, Key=%s, Size=%d bytes", partNum, bucket, key, written)
		return
	}

	// 3. Complete Multipart Upload
	if uploadID != "" && r.Method == http.MethodPost {
		metaDir := filepath.Join(bucketDir, ".uploads", key, uploadID)
		finalPath := filepath.Join(bucketDir, key)

		// Parse the part list to know exactly what parts to concatenate
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

		// Open final destination file
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

		var totalSize int64
		for _, p := range cmp.Parts {
			partPath := filepath.Join(metaDir, fmt.Sprintf("part-%d", p.PartNumber))
			srcFile, err := os.Open(partPath)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", fmt.Sprintf("Missing part %d: %v", p.PartNumber, err), r.URL.Path)
				return
			}
			written, err := io.Copy(destFile, srcFile)
			srcFile.Close()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
				return
			}
			totalSize += written
		}

		// Clean up part files
		_ = os.RemoveAll(metaDir)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
  <Location>http://localhost:9000/%s/%s</Location>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <ETag>"completed-etag"</ETag>
</CompleteMultipartUploadResult>`, bucket, key, bucket, key)
		log.Printf("[S3] Completed Multipart Upload: Bucket=%s, Key=%s, TotalSize=%d bytes", bucket, key, totalSize)
		return
	}

	// 4. Abort Multipart Upload
	if uploadID != "" && r.Method == http.MethodDelete {
		metaDir := filepath.Join(bucketDir, ".uploads", key, uploadID)
		_ = os.RemoveAll(metaDir)
		w.WriteHeader(http.StatusNoContent)
		log.Printf("[S3] Aborted Multipart Upload: Bucket=%s, Key=%s, UploadId=%s", bucket, key, uploadID)
		return
	}

	// 5. List Parts (Resume support)
	if uploadID != "" && r.Method == http.MethodGet {
		metaDir := filepath.Join(bucketDir, ".uploads", key, uploadID)
		files, _ := os.ReadDir(metaDir)

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListPartsResult>
  <Bucket>%s</Bucket>
  <Key>%s</Key>
  <UploadId>%s</UploadId>
  <IsTruncated>false</IsTruncated>`, bucket, key, uploadID)

		for _, f := range files {
			if strings.HasPrefix(f.Name(), "part-") {
				partNumStr := strings.TrimPrefix(f.Name(), "part-")
				partNum, _ := strconv.Atoi(partNumStr)
				info, err := f.Info()
				if err == nil {
					_, _ = fmt.Fprintf(w, `
  <Part>
    <PartNumber>%d</PartNumber>
    <ETag>"part-%d-etag"</ETag>
    <Size>%d</Size>
  </Part>`, partNum, partNum, info.Size())
				}
			}
		}
		_, _ = w.Write([]byte("\n</ListPartsResult>"))
		return
	}

	// 6. Head Object (Check if file exists)
	if r.Method == http.MethodHead {
		finalPath := filepath.Join(bucketDir, key)
		info, err := os.Stat(finalPath)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
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

		written, err := io.Copy(destFile, r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path)
			return
		}

		w.Header().Set("ETag", "\"completed-etag\"")
		w.WriteHeader(http.StatusOK)
		log.Printf("[S3] Put Object: Bucket=%s, Key=%s, Size=%d bytes", bucket, key, written)
		return
	}

	writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Method not allowed", r.URL.Path)
}
