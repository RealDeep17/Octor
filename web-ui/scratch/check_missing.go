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

func getDMMFallbackURLs(urlStr string) []string {
	parts := strings.Split(urlStr, "/")
	if len(parts) == 0 {
		return nil
	}
	filename := strings.ToLower(parts[len(parts)-1])

	// Parse prefix and number from filename or JAV code
	rx := regexp.MustCompile(`(?:^|[^a-z0-9])([a-z]{2,8})[^a-z0-9]*(\d{3,6})`)
	m := rx.FindStringSubmatch(filename)
	
	// If it doesn't match the filename, try the whole URL
	if len(m) < 3 {
		m = rx.FindStringSubmatch(strings.ToLower(urlStr))
	}

	if len(m) < 3 {
		return nil
	}

	prefix := m[1]
	numStr := m[2]
	
	var num int
	fmt.Sscanf(numStr, "%d", &num)

	cids := []string{
		fmt.Sprintf("%s%d", prefix, num),
		fmt.Sprintf("%s%03d", prefix, num),
		fmt.Sprintf("%s%05d", prefix, num),
		fmt.Sprintf("1%s%05d", prefix, num),
		fmt.Sprintf("30%s%05d", prefix, num),
		fmt.Sprintf("h_1143%s%05d", prefix, num),
		fmt.Sprintf("h_1143%s%03d", prefix, num),
	}

	var urls []string
	seen := make(map[string]bool)
	for _, cid := range cids {
		if seen[cid] {
			continue
		}
		seen[cid] = true
		urls = append(urls,
			fmt.Sprintf("https://pics.dmm.co.jp/mono/movie/adult/%s/%spl.jpg", cid, cid),
			fmt.Sprintf("https://pics.dmm.co.jp/digital/video/%s/%spl.jpg", cid, cid),
		)
	}
	return urls
}

func testQuery(query string) {
	fmt.Printf("\n=== Querying: %s ===\n", query)
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
	foundAny := false
	for _, m := range matches {
		imgURL := m[1]
		if strings.Contains(imgURL, "/wp-content/uploads/") && !strings.Contains(imgURL, "logo") {
			fmt.Printf("Scraped Image URL: %s\n", imgURL)
			foundAny = true
			
			// Test parser outputs
			fallbacks := getDMMFallbackURLs(imgURL)
			fmt.Printf("Fallback DMM URLs generated (%d):\n", len(fallbacks))
			for i, f := range fallbacks {
				if i < 4 { // just print first few
					fmt.Printf("  - %s\n", f)
				}
			}
			
			// Check if DDG works
			ddgURL := fmt.Sprintf("https://external-content.duckduckgo.com/iu/?u=%s", url.QueryEscape(imgURL))
			dReq, _ := http.NewRequest("GET", ddgURL, nil)
			dReq.Header.Set("User-Agent", "Mozilla/5.0")
			dResp, dErr := cl.Do(dReq)
			if dErr == nil {
				fmt.Printf("DuckDuckGo Status: %d\n", dResp.StatusCode)
				dResp.Body.Close()
			} else {
				fmt.Printf("DuckDuckGo Error: %v\n", dErr)
			}
		}
	}
	if !foundAny {
		fmt.Println("No uploads images found in JAV Guru search results!")
	}
}

func main() {
	testQuery("URE-082")
	testQuery("URE-101")
	testQuery("DASS-943")
}
