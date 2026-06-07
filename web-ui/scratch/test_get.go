package main

import (
	"fmt"
	"net/http"
	"time"
)

func testDownload(urlStr string, ua string) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}
	if ua != "DEFAULT" {
		req.Header.Set("User-Agent", ua)
	}

	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		fmt.Printf("Error on request (ua=%s): %v\n", ua, err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response (ua=%s): Status=%d, ContentLength=%d\n", ua, resp.StatusCode, resp.ContentLength)
}

func main() {
	urlStr := "https://cdn.javmiku.com/wp-content/uploads/2026/05/adn782pl.jpg"
	fmt.Printf("Testing GET request for: %s\n", urlStr)
	testDownload(urlStr, "DEFAULT")
	testDownload(urlStr, "curl/7.81.0")
	testDownload(urlStr, "Mozilla/5.0")
	testDownload(urlStr, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
}
