package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"
)

func main() {
	fmt.Println("=== Starting Live Sidecar Validation and Comparison ===")

	testsheetPath := "sidecar/test/testsheet447-mismatches.json"
	fmt.Printf("Loading testsheet from %s...\n", testsheetPath)
	
	file, err := os.Open(testsheetPath)
	if err != nil {
		fmt.Printf("ERROR: Failed to open testsheet: %v\n", err)
		return
	}
	
	var torrentTitles []string
	if err := json.NewDecoder(file).Decode(&torrentTitles); err != nil {
		file.Close()
		fmt.Printf("ERROR: Failed to decode testsheet JSON: %v\n", err)
		return
	}
	file.Close()


	fmt.Printf("Successfully loaded %d torrent titles for live test.\n", len(torrentTitles))
	client := &http.Client{Timeout: 15 * time.Second}

	// Start Python sidecar on port 8093 targeting live APIs
	fmt.Println("Starting Python sidecar on port 8093 (live)...")
	var pyCmd *exec.Cmd
	if _, err := os.Stat("sidecar/venv/bin/uvicorn"); err == nil {
		pyCmd = exec.Command("./venv/bin/uvicorn", "main:app", "--host", "127.0.0.1", "--port", "8093")
		pyCmd.Dir = "sidecar"
	} else {
		pyCmd = exec.Command("uvicorn", "main:app", "--host", "127.0.0.1", "--port", "8093")
		pyCmd.Dir = "sidecar"
	}
	pyCmd.Env = append(os.Environ(),
		"PORT=8093",
		"OMDB_API_PORT=8093",
		"THEPORNDB_API_KEY=4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf",
		"STASHDB_API_KEY=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY",
		"TPDB_BASE=https://api.theporndb.net",
		"STASHDB_ENDPOINT=https://stashdb.org/graphql",
	)
	pyStdout, _ := pyCmd.StdoutPipe()
	pyStderr, _ := pyCmd.StderrPipe()
	go goioCopy(os.Stdout, pyStdout)
	go goioCopy(os.Stderr, pyStderr)

	if err := pyCmd.Start(); err != nil {
		fmt.Printf("Failed to start Python sidecar: %v\n", err)
		return
	}
	defer func() {
		fmt.Println("Stopping Python sidecar...")
		pyCmd.Process.Kill()
	}()

	// Start Go sidecar on port 8095 targeting live APIs
	fmt.Println("Starting Go sidecar on port 8095 (live)...")
	goCmd := exec.Command("go", "run", "sidecar/main.go")
	goCmd.Env = append(os.Environ(),
		"PORT=8095",
		"OMDB_API_PORT=8095",
		"THEPORNDB_API_KEY=4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf",
		"STASHDB_API_KEY=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY",
		"TPDB_BASE=https://api.theporndb.net",
		"STASHDB_ENDPOINT=https://stashdb.org/graphql",
	)
	goStdout, _ := goCmd.StdoutPipe()
	goStderr, _ := goCmd.StderrPipe()
	go goioCopy(os.Stdout, goStdout)
	go goioCopy(os.Stderr, goStderr)

	if err := goCmd.Start(); err != nil {
		fmt.Printf("Failed to start Go sidecar: %v\n", err)
		return
	}
	defer func() {
		fmt.Println("Stopping Go sidecar...")
		goCmd.Process.Kill()
	}()

	// Wait for sidecars to start
	time.Sleep(5 * time.Second)

	// Verify Python sidecar
	resp, err := client.Get("http://localhost:8093/settings")
	if err != nil {
		fmt.Printf("ERROR: Python sidecar is not reachable: %v\n", err)
		return
	}
	resp.Body.Close()

	// Verify Go sidecar
	resp, err = client.Get("http://localhost:8095/settings")
	if err != nil {
		fmt.Printf("ERROR: Go sidecar is not reachable: %v\n", err)
		return
	}
	resp.Body.Close()

	fmt.Println("Both sidecars are up. Starting comparison queries...")

	var mu sync.Mutex
	var wg sync.WaitGroup
	
	type resultEntry struct {
		Title  string
		Status string
		Diffs  []string
	}
	var results []resultEntry
	matches := 0
	mismatches := 0
	neitherEnriched := 0
	onlyPythonEnriched := 0
	onlyGoEnriched := 0
	errorsCount := 0
	totalProcessed := 0

	// Use a worker pool of 2 to avoid aggressive rate limiting on live APIs
	sem := make(chan struct{}, 2)

	for _, title := range torrentTitles {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			pyURL := fmt.Sprintf("http://localhost:8093/?t=%s&porn=true", url.QueryEscape(t))
			goURL := fmt.Sprintf("http://localhost:8095/?t=%s&porn=true", url.QueryEscape(t))

			pyReq, _ := http.NewRequest("GET", pyURL, nil)
			pyResp, pyErr := client.Do(pyReq)

			goReq, _ := http.NewRequest("GET", goURL, nil)
			goResp, goErr := client.Do(goReq)

			if pyErr != nil || goErr != nil {
				mu.Lock()
				errorsCount++
				totalProcessed++
				results = append(results, resultEntry{
					Title:  t,
					Status: "ERROR",
					Diffs:  []string{fmt.Sprintf("REQUEST_ERROR: py=%v go=%v", pyErr, goErr)},
				})
				mu.Unlock()
				if pyResp != nil {
					pyResp.Body.Close()
				}
				if goResp != nil {
					goResp.Body.Close()
				}
				return
			}

			var pyData map[string]any
			var goData map[string]any

			json.NewDecoder(pyResp.Body).Decode(&pyData)
			json.NewDecoder(goResp.Body).Decode(&goData)
			pyResp.Body.Close()
			goResp.Body.Close()

			pyResponseStatus := pyData["Response"]
			goResponseStatus := goData["Response"]

			var status string
			var diffs []string

			if pyResponseStatus == "False" && goResponseStatus == "False" {
				status = "NEITHER_ENRICHED"
			} else if pyResponseStatus == "True" && goResponseStatus == "False" {
				status = "ONLY_PYTHON"
				diffs = append(diffs, "Python found result, Go returned 404/False")
			} else if pyResponseStatus == "False" && goResponseStatus == "True" {
				status = "ONLY_GO"
				diffs = append(diffs, "Go found result, Python returned 404/False")
			} else {
				fieldsToCompare := []string{
					"Title", "Year", "Rated", "Released", "Runtime", "Genre",
					"Actors", "Plot", "Poster", "ImdbRating", "ImdbID", "Production", "Website",
				}
				for _, f := range fieldsToCompare {
					pyVal := pyData[f]
					goVal := goData[f]
					if !reflect.DeepEqual(pyVal, goVal) {
						diffs = append(diffs, fmt.Sprintf("Field %s mismatch: Python=%q, Go=%q", f, pyVal, goVal))
					}
				}
				if len(diffs) > 0 {
					status = "MISMATCH"
				} else {
					status = "MATCH"
				}
			}

			mu.Lock()
			totalProcessed++
			switch status {
			case "MATCH":
				matches++
			case "NEITHER_ENRICHED":
				neitherEnriched++
			case "ONLY_PYTHON":
				onlyPythonEnriched++
				mismatches++
			case "ONLY_GO":
				onlyGoEnriched++
				mismatches++
			case "MISMATCH":
				mismatches++
			}

			results = append(results, resultEntry{
				Title:  t,
				Status: status,
				Diffs:  diffs,
			})

			fmt.Printf("  Progress %d/%d — Matches=%d Mismatches=%d (OnlyPy=%d OnlyGo=%d) Errors=%d\n",
				totalProcessed, len(torrentTitles), matches, mismatches, onlyPythonEnriched, onlyGoEnriched, errorsCount)
			mu.Unlock()

			// Sleep to avoid rate limiting
			time.Sleep(500 * time.Millisecond)
		}(title)
	}
	wg.Wait()

	fmt.Println("\n==========================================")
	fmt.Println("        LIVE COMPARISON STATISTICS        ")
	fmt.Println("==========================================")
	fmt.Printf("Total processed:      %d\n", totalProcessed)
	fmt.Printf("Perfect matches:      %d\n", matches)
	fmt.Printf("Neither enriched:     %d\n", neitherEnriched)
	fmt.Printf("Mismatches (total):   %d\n", mismatches)
	fmt.Printf("  - Only Python:      %d\n", onlyPythonEnriched)
	fmt.Printf("  - Only Go:          %d\n", onlyGoEnriched)
	fmt.Printf("Errors:               %d\n", errorsCount)
	fmt.Println("==========================================")

	// Save log
	logPath := "sidecar/test/compare_live_output.log"
	logFile, _ := os.Create(logPath)
	if logFile != nil {
		defer logFile.Close()
		fmt.Fprintf(logFile, "=== Live sidecar comparison log ===\n")
		for _, r := range results {
			fmt.Fprintf(logFile, "[%s] Torrent: %q\n", r.Status, r.Title)
			for _, d := range r.Diffs {
				fmt.Fprintf(logFile, "  - %s\n", d)
			}
			fmt.Fprintf(logFile, "\n")
		}
	}
}

func goioCopy(dst io.Writer, src io.Reader) {
	io.Copy(dst, src)
}
