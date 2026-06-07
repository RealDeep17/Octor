package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func main() {
	query := "DLDSS-494"
	searchURL := fmt.Sprintf("https://jav.guru/?s=%s", url.QueryEscape(query))
	req, _ := http.NewRequestWithContext(context.Background(), "GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	htmlStr := string(body)

	// Extract all image URLs
	imgRx := regexp.MustCompile(`(?i)src=["'](https?://[^"']+\.(?:jpg|png|webp|jpeg))["']`)
	matches := imgRx.FindAllStringSubmatch(htmlStr, -1)
	fmt.Printf("Total image matches found: %d\n", len(matches))
	for i, m := range matches {
		if strings.Contains(m[1], "/wp-content/uploads/") {
			fmt.Printf("[%d]: %s\n", i+1, m[1])
		}
	}
}
