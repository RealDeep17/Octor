package ziphttp_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/webtor-io/torrent-archiver/ziphttp"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

var (
	data = []string{"abra", "cadabra"}
	l    = int64(239)
)

func getLen(s *httptest.Server, begin int64, end int64, data []string) int64 {
	return l
	// var buf bytes.Buffer
	// zw := zip.NewWriter(&buf, begin, end, s.Client())
	// var l int64
	// for _, d := range data {
	// 	zw.CreateHeader(&zip.FileHeader{
	// 		Name: d,
	// 	})
	// 	l += int64(len(d))
	// }
	// zw.Close()
	// l += int64(buf.Len())
	// return l
}

func getBytes(s *httptest.Server, begin int64, end int64, data []string) []byte {
	var buf bytes.Buffer
	zw := ziphttp.NewWriter(&buf, begin, end, s.Client())
	for _, d := range data {
		header := &ziphttp.FileHeader{
			Name:               d,
			URL:                s.URL + "/" + d,
			UncompressedSize64: uint64(len(d)),
		}
		header.SetMode(os.FileMode(int(0644)))
		zw.CreateHeader(context.Background(), header)
	}
	zw.Close()
	return buf.Bytes()
}

func testReadData(t *testing.T, b []byte, data []string) {
	testRead(t, b, []byte(strings.Join(data, "")))
}

func testRead(t *testing.T, b []byte, data []byte) {
	var wb bytes.Buffer
	r, err := ziphttp.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}
		io.Copy(&wb, rc)
	}
	if wb.String() != string(data) {
		t.Fatalf("Expected %s got %s", data, wb.Bytes())
	}
}

func testOffset(t *testing.T, s *httptest.Server, i int64, data []string, l int64) {
	b1 := getBytes(s, 0, i, data)
	if i+1 != int64(len(b1)) {
		t.Fatalf("Expected %d got %d", i+1, len(b1))
	}
	b2 := getBytes(s, i+1, -1, data)
	if l-i-1 != int64(len(b2)) {
		t.Fatalf("Expected %d got %d", l-i-1, len(b2))
	}
	b := append(b1, b2...)
	if l != int64(len(b)) {
		t.Fatalf("Expected %d got %d", l, len(b))
	}
	testReadData(t, b, data)
}

func runServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		data := []byte(strings.TrimPrefix(req.URL.String(), "/"))
		if req.Header.Get("Range") != "" {
			parts := strings.Split(strings.TrimPrefix(req.Header.Get("Range"), "bytes="), "-")
			begin, _ := strconv.Atoi(parts[0])
			end, _ := strconv.Atoi(parts[1])
			rw.Write(data[begin : end+1])
		} else {
			rw.Write(data)
		}
	}))
}

func runGBServer(size int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		sizeBytes := int64(size) * 1024 * 1024 * 1024
		begin, end := int64(0), sizeBytes-1
		if req.Header.Get("Range") != "" {
			parts := strings.Split(strings.TrimPrefix(req.Header.Get("Range"), "bytes="), "-")
			begin, _ = strconv.ParseInt(parts[0], 10, 64)
			if parts[1] != "" {
				end, _ = strconv.ParseInt(parts[1], 10, 64)
			}
			if end >= sizeBytes {
				end = sizeBytes - 1
			}
			if begin > end {
				rw.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
		}
		chunk := make([]byte, 1024*1024)
		for i := 0; i < len(chunk); i++ {
			chunk[i] = 1
		}
		remaining := end - begin + 1
		for remaining > 0 {
			n := int64(len(chunk))
			if remaining < n {
				n = remaining
			}
			_, _ = rw.Write(chunk[:n])
			remaining -= n
		}
	}))
}

func TestWrite(t *testing.T) {
	s := runServer()
	defer s.Close()
	l := getLen(s, 0, -1, data)
	b := getBytes(s, 0, -1, data)
	if l != int64(len(b)) {
		t.Fatalf("Expected %d got %d", l, len(b))
	}
	testReadData(t, b, data)
}

func TestWriteWithOffsets(t *testing.T) {
	s := runServer()
	defer s.Close()
	l := getLen(s, 0, -1, data)
	for i := int64(0); i < l; i++ {
		testOffset(t, s, i, data, l)
	}
}

func TestWriteWithOffset33(t *testing.T) {
	s := runServer()
	defer s.Close()
	l := getLen(s, 0, -1, data)
	testOffset(t, s, 33, data, l)
}

func TestWriteWithOffset34(t *testing.T) {
	s := runServer()
	defer s.Close()
	l := getLen(s, 0, -1, data)
	testOffset(t, s, 34, data, l)
}

func Test5GBZipping(t *testing.T) {
	size := 5
	sizeBytes := int64(size) * 1024 * 1024 * 1024
	var buf bytes.Buffer
	s := runGBServer(size)
	defer s.Close()
	zw := ziphttp.NewWriter(&buf, sizeBytes+1, -1, s.Client())
	header := &ziphttp.FileHeader{
		Name:               fmt.Sprintf("%vGB", size),
		URL:                s.URL,
		UncompressedSize64: uint64(sizeBytes),
	}
	header.SetMode(os.FileMode(int(0644)))
	if err := zw.CreateHeader(context.Background(), header); err != nil {
		t.Fatalf("CreateHeader() error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	b := buf.Bytes()
	if len(b) == 0 {
		t.Fatal("expected tail range to include zip64 records")
	}
	for _, sig := range [][]byte{[]byte("PK\x06\x06"), []byte("PK\x06\x07"), []byte("PK\x05\x06")} {
		if !bytes.Contains(b, sig) {
			t.Fatalf("expected zip tail to contain signature % x", sig)
		}
	}
}
