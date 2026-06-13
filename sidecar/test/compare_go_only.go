// compare_go_only.go
//
// Runs the Go sidecar against testsheet447-mismatches.json and compares results
// against previously recorded Python outputs in compare_live_output.log.
//
// Features:
//   - Caching proxy on :8002 — all TPDB and StashDB responses are saved to
//     sidecar/test/cache/ and replayed on subsequent runs (zero live API calls
//     once the cache is warm).
//   - 50 parallel workers.
//   - Classifies each title as IMPROVED / REGRESSED / STABLE_MATCH / etc.
//
// Usage (from octor root):
//   go run sidecar/test/compare_go_only.go
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Cache helpers ──────────────────────────────────────────────────────────

const cacheDir = "sidecar/test/cache"

type cacheEntry struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       []byte              `json:"body"`
}

var cacheMu sync.Mutex

func cacheKey(prefix, key string) string {
	h := md5.Sum([]byte(key))
	return filepath.Join(cacheDir, prefix+"_"+hex.EncodeToString(h[:])+".json")
}

// normTPDBURL normalizes a TPDB URL so that equivalent requests (same path +
// same query params, regardless of encoding or ordering) share a cache key.
func normTPDBURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	// Re-encode query: parse → sort → re-encode (handles + vs %20, etc.)
	q := u.Query()
	u.RawQuery = q.Encode()
	// Lower-case the host
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

// normStashBody normalises a StashDB JSON body for stable cache keying.
// It decodes the JSON, re-marshals it (which sorts no maps but removes
// whitespace differences), and normalises the "term" variable to lower-case.
func normStashBody(body []byte) string {
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	// Lower-case the search term so "Anal Maid" and "anal maid" share a key
	if vars, ok := v["variables"].(map[string]any); ok {
		if term, ok := vars["term"].(string); ok {
			vars["term"] = strings.ToLower(term)
			v["variables"] = vars
		}
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func loadCache(path string) (cacheEntry, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return cacheEntry{}, false
	}
	return e, true
}

func saveCache(path string, e cacheEntry) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	data, _ := json.Marshal(e)
	_ = os.WriteFile(path, data, 0644)
}

// decompress tries gzip; falls back to raw.
func decompress(b []byte) []byte {
	gr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return b
	}
	out, _ := io.ReadAll(gr)
	gr.Close()
	return out
}

// ── Caching proxy ──────────────────────────────────────────────────────────

// proxyClient is a dedicated HTTP client for the proxy's live upstream calls.
var proxyClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
	},
}

// liveSem limits concurrent live API requests to avoid rate-limit hammering.
// Using 3 so we stay well within TPDB's rate limit when cache is cold.
var liveSem = make(chan struct{}, 3)
var liveMu sync.Mutex
var lastLive time.Time

func acquireLive() {
	liveSem <- struct{}{}
	liveMu.Lock()
	since := time.Since(lastLive)
	if since < 300*time.Millisecond {
		time.Sleep(300*time.Millisecond - since)
	}
	lastLive = time.Now()
	liveMu.Unlock()
}

func releaseLive() { <-liveSem }

func startCachingProxy(tpdbKey, stashKey string) {
	_ = os.MkdirAll(cacheDir, 0755)

	// ── TPDB handler ──────────────────────────────────────────────────────
	http.HandleFunc("/tpdb/", func(w http.ResponseWriter, r *http.Request) {
		// Reconstruct the real TPDB URL
		targetPath := strings.TrimPrefix(r.URL.Path, "/tpdb")
		targetURL := "https://api.theporndb.net" + targetPath
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		ck := cacheKey("tpdb", normTPDBURL(targetURL))
		if e, ok := loadCache(ck); ok {
			writeEntry(w, e)
			return
		}

		// Live fetch
		acquireLive()
		req, _ := http.NewRequest(r.Method, targetURL, nil)
		req.Header = make(http.Header)
		req.Header.Set("Authorization", "Bearer "+tpdbKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "octor-sidecar/2")

		resp, err := proxyClient.Do(req)
		releaseLive()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		// Only cache successful or 404 responses (not rate-limit errors)
		if resp.StatusCode != http.StatusTooManyRequests {
			e := cacheEntry{
				StatusCode: resp.StatusCode,
				Header:     map[string][]string(resp.Header),
				Body:       body,
			}
			saveCache(ck, e)
			writeEntry(w, e)
		} else {
			// Pass 429 through but don't cache it
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})

	// ── StashDB handler ───────────────────────────────────────────────────
	http.HandleFunc("/stashdb", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		ck := cacheKey("stashdb", normStashBody(bodyBytes))
		if e, ok := loadCache(ck); ok {
			writeEntry(w, e)
			return
		}

		// Live fetch
		acquireLive()
		req, _ := http.NewRequest(http.MethodPost, "https://stashdb.org/graphql", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Apikey", stashKey)

		resp, err := proxyClient.Do(req)
		releaseLive()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusTooManyRequests {
			e := cacheEntry{
				StatusCode: resp.StatusCode,
				Header:     map[string][]string(resp.Header),
				Body:       body,
			}
			saveCache(ck, e)
			writeEntry(w, e)
		} else {
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})

	fmt.Println("Caching proxy started on :8002")
	srv := &http.Server{
		Addr:         ":8002",
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Printf("Proxy error: %v\n", err)
	}
}

func writeEntry(w http.ResponseWriter, e cacheEntry) {
	body := decompress(e.Body)
	for k, vv := range e.Header {
		// Don't forward encoding headers — body is already decompressed
		kl := strings.ToLower(k)
		if kl == "content-encoding" || kl == "transfer-encoding" {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(e.StatusCode)
	w.Write(body)
}

// ── Previous-log parser ────────────────────────────────────────────────────

type prevResult struct {
	Status string
	Diffs  []string
}

func parsePreviousLog(logPath string) map[string]prevResult {
	f, err := os.Open(logPath)
	if err != nil {
		fmt.Printf("WARNING: Could not open previous log %s: %v\n", logPath, err)
		return nil
	}
	defer f.Close()

	out := make(map[string]prevResult)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	var curTitle, curStatus string
	var curDiffs []string

	flush := func() {
		if curTitle != "" && curStatus != "" {
			out[curTitle] = prevResult{Status: curStatus, Diffs: curDiffs}
		}
		curTitle, curStatus, curDiffs = "", "", nil
	}

	statusPrefixes := []string{"[MATCH]", "[MISMATCH]", "[NEITHER_ENRICHED]", "[ONLY_PYTHON]", "[ONLY_GO]", "[ERROR]"}
	for scanner.Scan() {
		line := scanner.Text()
		matched := false
		for _, pfx := range statusPrefixes {
			if strings.HasPrefix(line, pfx) {
				flush()
				bracketEnd := strings.Index(line, "]")
				curStatus = line[1:bracketEnd]
				rest := line[bracketEnd+1:]
				idx := strings.Index(rest, "Torrent: \"")
				if idx < 0 {
					break
				}
				title := rest[idx+len("Torrent: \""):]
				if len(title) > 0 && title[len(title)-1] == '"' {
					title = title[:len(title)-1]
				}
				curTitle = title
				matched = true
				break
			}
		}
		if !matched && strings.HasPrefix(line, "  - ") && curTitle != "" {
			curDiffs = append(curDiffs, line[4:])
		}
	}
	flush()
	return out
}

// ── Main ───────────────────────────────────────────────────────────────────

func main() {
	fmt.Println("=== Go-Only Regression & Improvement Test (cached) ===")

	const (
		testsheetPath = "sidecar/test/testsheet447-mismatches.json"
		prevLogPath   = "sidecar/test/compare_live_output.log"
		tpdbAPIKey    = "4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf"
		stashAPIKey   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY"
		workers = 20
	)

	outputLogPath := fmt.Sprintf("sidecar/test/compare_output_%s.log", time.Now().Format("20060102_150405"))

	// Count existing cache files
	if files, err := os.ReadDir(cacheDir); err == nil {
		fmt.Printf("Cache: %d entries in %s\n", len(files), cacheDir)
	} else {
		fmt.Println("Cache directory empty/missing — will be populated from live APIs.")
		_ = os.MkdirAll(cacheDir, 0755)
	}

	// Load testsheet
	f, err := os.Open(testsheetPath)
	if err != nil {
		fmt.Printf("ERROR: Cannot open testsheet: %v\n", err)
		return
	}
	var titles []string
	if err := json.NewDecoder(f).Decode(&titles); err != nil {
		f.Close()
		fmt.Printf("ERROR: Cannot decode testsheet: %v\n", err)
		return
	}
	f.Close()
	fmt.Printf("Loaded %d titles from testsheet.\n", len(titles))

	// Load previous results
	prev := parsePreviousLog(prevLogPath)
	fmt.Printf("Loaded %d previous Python results from log.\n", len(prev))

	// Start caching proxy
	go startCachingProxy(tpdbAPIKey, stashAPIKey)
	time.Sleep(300 * time.Millisecond)

	// Build Go sidecar binary
	fmt.Println("Building Go sidecar...")
	binPath := "bin/sidecar_go_only_bin"
	buildOut, err := exec.Command("go", "build", "-o", binPath, "./sidecar").CombinedOutput()
	if err != nil {
		fmt.Printf("ERROR: Build failed: %v\n%s\n", err, buildOut)
		return
	}
	fmt.Println("Build OK.")

	// Start Go sidecar — point at our caching proxy
	goCmd := exec.Command(binPath)
	goCmd.Env = append(os.Environ(),
		"PORT=8095",
		"GIN_MODE=release",
		"THEPORNDB_API_KEY="+tpdbAPIKey,
		"STASHDB_API_KEY="+stashAPIKey,
		"TPDB_BASE=http://localhost:8002/tpdb",
		"STASHDB_ENDPOINT=http://localhost:8002/stashdb",
	)
	goCmd.Stdout = os.Stdout
	goCmd.Stderr = os.Stderr
	if err := goCmd.Start(); err != nil {
		fmt.Printf("ERROR: Failed to start Go sidecar: %v\n", err)
		return
	}
	defer func() {
		fmt.Println("Stopping Go sidecar...")
		goCmd.Process.Kill()
		os.Remove(binPath)
	}()

	// Wait for sidecar to be ready
	client := &http.Client{Timeout: 60 * time.Second}
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := client.Get("http://localhost:8095/settings"); err == nil {
			resp.Body.Close()
			fmt.Println("Go sidecar is up!")
			break
		}
		if i == 29 {
			fmt.Println("ERROR: Go sidecar did not start in time")
			return
		}
	}

	// ── Parallel query loop ────────────────────────────────────────────────

	type result struct {
		Title    string
		GoStatus string
		GoData   map[string]any
		Prev     prevResult
		Change   string
		Notes    []string
	}

	var (
		mu             sync.Mutex
		wg             sync.WaitGroup
		results        []result
		improved       int
		regressed      int
		stableMatch    int
		stableMismatch int
		newGoOnly      int
		totalDone      int
	)

	sem := make(chan struct{}, workers)

	for _, title := range titles {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			goURL := fmt.Sprintf("http://localhost:8095/?t=%s&porn=true", url.QueryEscape(t))
			resp, err := client.Get(goURL)

			var goData map[string]any
			goStatus := "NOT_ENRICHED"
			if err == nil {
				json.NewDecoder(resp.Body).Decode(&goData)
				resp.Body.Close()
				if s, _ := goData["Response"].(string); s == "True" {
					goStatus = "ENRICHED"
				}
			}

			p, hasPrev := prev[t]
			var change string
			var notes []string

			if !hasPrev {
				if goStatus == "ENRICHED" {
					change = "NEW_GO_ONLY"
				} else {
					change = "NEW_NEITHER"
				}
			} else {
				switch {
				// ── Regressions ──────────────────────────────────────────
				case (p.Status == "MATCH" || p.Status == "ONLY_GO") && goStatus == "NOT_ENRICHED":
					change = "REGRESSED"
					notes = append(notes, fmt.Sprintf("Was %s → now NOT_ENRICHED ⚠️", p.Status))

				case p.Status == "MISMATCH" && goStatus == "NOT_ENRICHED":
					change = "REGRESSED"
					notes = append(notes, "Was MISMATCH (enriched) → now NOT_ENRICHED ⚠️")

				// ── Improvements ─────────────────────────────────────────
				case p.Status == "ONLY_PYTHON" && goStatus == "ENRICHED":
					change = "IMPROVED"
					notes = append(notes, "Was ONLY_PYTHON → Go now enriches ✅")
					if title, ok := goData["Title"].(string); ok {
						notes = append(notes, "  Go Title: "+title)
					}

				// ── Stable good ───────────────────────────────────────────
				case p.Status == "MATCH" && goStatus == "ENRICHED":
					change = "STABLE_MATCH"

				case p.Status == "ONLY_GO" && goStatus == "ENRICHED":
					change = "STABLE_ONLY_GO"

				// ── Mismatch still enriched ───────────────────────────────
				case p.Status == "MISMATCH" && goStatus == "ENRICHED":
					change = "MISMATCH_ENRICHED"
					// Show what Go returns now vs. what it returned before
					if goTitle, ok := goData["Title"].(string); ok {
						notes = append(notes, "Go Title now: "+goTitle)
					}
					for _, d := range p.Diffs {
						if strings.HasPrefix(d, "Field Title") {
							notes = append(notes, "Prev: "+d)
							break
						}
					}

				// ── Still not enriched ────────────────────────────────────
				case p.Status == "ONLY_PYTHON" && goStatus == "NOT_ENRICHED":
					change = "STILL_ONLY_PYTHON"

				case p.Status == "NEITHER_ENRICHED" && goStatus == "NOT_ENRICHED":
					change = "STILL_NEITHER"

				case p.Status == "NEITHER_ENRICHED" && goStatus == "ENRICHED":
					change = "NEW_GO_ONLY" // go newly enriches something python also missed
					notes = append(notes, "Was NEITHER_ENRICHED → Go now enriches (bonus!)")
					if title, ok := goData["Title"].(string); ok {
						notes = append(notes, "  Go Title: "+title)
					}

				default:
					change = fmt.Sprintf("OTHER_%s->%s", p.Status, goStatus)
				}
			}

			mu.Lock()
			totalDone++
			switch change {
			case "IMPROVED", "NEW_GO_ONLY":
				if change == "IMPROVED" {
					improved++
				} else {
					newGoOnly++
				}
			case "REGRESSED":
				regressed++
			case "STABLE_MATCH":
				stableMatch++
			default:
				stableMismatch++
			}

			results = append(results, result{
				Title:    t,
				GoStatus: goStatus,
				GoData:   goData,
				Prev:     p,
				Change:   change,
				Notes:    notes,
			})

			// Live progress for regressions & improvements
			if change == "REGRESSED" || change == "IMPROVED" {
				icon := "✅"
				if change == "REGRESSED" {
					icon = "⚠️ "
				}
				fmt.Printf("  %s [%s] %s\n", icon, change, t)
				for _, n := range notes {
					fmt.Printf("       %s\n", n)
				}
			}

			if totalDone%50 == 0 || totalDone == len(titles) {
				fmt.Printf("  Progress %d/%d — improved=%d regressed=%d stableMatch=%d\n",
					totalDone, len(titles), improved, regressed, stableMatch)
			}
			mu.Unlock()
		}(title)
	}
	wg.Wait()

	// Count cache size after run
	var cacheCount int
	if files, err := os.ReadDir(cacheDir); err == nil {
		cacheCount = len(files)
	}

	// ── Summary ────────────────────────────────────────────────────────────
	fmt.Println("\n==========================================")
	fmt.Println("     GO-ONLY REGRESSION TEST RESULTS     ")
	fmt.Println("==========================================")
	fmt.Printf("Titles tested:         %d\n", len(titles))
	fmt.Printf("Stable Matches:        %d\n", stableMatch)
	fmt.Printf("IMPROVED:              %d  ✅ (now enriched, was ONLY_PYTHON or missed)\n", improved)
	fmt.Printf("NEW_GO_ONLY:           %d  ✅ (Go enriches, prev was NEITHER or unknown)\n", newGoOnly)
	fmt.Printf("REGRESSED:             %d  ⚠️  (was enriched/matched, now lost)\n", regressed)
	fmt.Printf("Other (mismatches/etc): %d\n", stableMismatch)
	fmt.Printf("Cache entries now:     %d (in %s)\n", cacheCount, cacheDir)
	fmt.Println("==========================================")

	if regressed > 0 {
		fmt.Printf("\n⚠️  REGRESSIONS (%d):\n", regressed)
		for _, r := range results {
			if r.Change == "REGRESSED" {
				fmt.Printf("  - %s (was %s)\n", r.Title, r.Prev.Status)
				for _, n := range r.Notes {
					fmt.Printf("    %s\n", n)
				}
			}
		}
	}

	if improved > 0 {
		fmt.Printf("\n✅ IMPROVEMENTS (%d):\n", improved)
		for _, r := range results {
			if r.Change == "IMPROVED" {
				fmt.Printf("  + %s\n", r.Title)
				for _, n := range r.Notes {
					fmt.Printf("    %s\n", n)
				}
			}
		}
	}

	// ── Save detailed log ──────────────────────────────────────────────────
	lf, _ := os.Create(outputLogPath)
	if lf != nil {
		defer lf.Close()
		fmt.Fprintf(lf, "=== Go-Only Regression Test — %s ===\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(lf, "Testsheet: %s\n", testsheetPath)
		fmt.Fprintf(lf, "Previous: %s\n\n", prevLogPath)
		for _, r := range results {
			fmt.Fprintf(lf, "[%s] %s\n", r.Change, r.Title)
			for _, n := range r.Notes {
				fmt.Fprintf(lf, "  %s\n", n)
			}
			if r.GoStatus == "ENRICHED" {
				if v, _ := r.GoData["Title"].(string); v != "" {
					fmt.Fprintf(lf, "  GoTitle:    %s\n", v)
				}
				if v, _ := r.GoData["Released"].(string); v != "" {
					fmt.Fprintf(lf, "  GoReleased: %s\n", v)
				}
				if v, _ := r.GoData["Actors"].(string); v != "" {
					fmt.Fprintf(lf, "  GoActors:   %s\n", v)
				}
				if v, _ := r.GoData["Production"].(string); v != "" {
					fmt.Fprintf(lf, "  GoProd:     %s\n", v)
				}
			}
			fmt.Fprintln(lf)
		}
		fmt.Fprintf(lf, "\n=== SUMMARY ===\n")
		fmt.Fprintf(lf, "IMPROVED:     %d\n", improved)
		fmt.Fprintf(lf, "NEW_GO_ONLY:  %d\n", newGoOnly)
		fmt.Fprintf(lf, "REGRESSED:    %d\n", regressed)
		fmt.Fprintf(lf, "STABLE_MATCH: %d\n", stableMatch)
		fmt.Printf("\nFull log saved → %s\n", outputLogPath)
	}
}
