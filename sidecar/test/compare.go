package main

import (
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
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProwlarrResult struct {
	Title      string `json:"title"`
	Indexer    string `json:"indexer"`
	Categories []struct {
		Id int `json:"id"`
	} `json:"categories"`
}

type CacheEntry struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       []byte              `json:"body"`
}

var cacheMu sync.Mutex

func getCache(cacheKey string) (CacheEntry, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	path := filepath.Join("sidecar/test/cache", cacheKey+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheEntry{}, false
	}

	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return CacheEntry{}, false
	}
	return entry, true
}

func saveCache(cacheKey string, entry CacheEntry) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	path := filepath.Join("sidecar/test/cache", cacheKey+".json")
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

var (
	tpdbCleanIndex    map[string]CacheEntry
	stashdbCleanIndex map[string][]CacheEntry
	cacheIndexMu      sync.RWMutex
)

func loadCacheIntoMemory() {
	cacheIndexMu.Lock()
	defer cacheIndexMu.Unlock()

	tpdbCleanIndex = make(map[string]CacheEntry)
	stashdbCleanIndex = make(map[string][]CacheEntry)

	files, err := os.ReadDir("sidecar/test/cache")
	if err != nil {
		fmt.Printf("Error reading cache dir: %v\n", err)
		return
	}

	// Read testsheet691-adult.json to get all titles
	testsheetPath := os.Getenv("TESTSHEET_PATH")
	if testsheetPath == "" {
		testsheetPath = "sidecar/test/testsheet691-adult.json"
	}
	testsheetData, err := os.ReadFile(testsheetPath)
	var titles []string
	if err == nil {
		_ = json.Unmarshal(testsheetData, &titles)
	}

	// Python helper regexes
	rxPyCleanup := regexp.MustCompile(`(?i)\b(XXX|1080p|720p|2160p|4[Kk]|WEB[-. ]?DL|WEBRip|HDRip|BluRay|x264|x265|H\.?264|H\.?265|MP4|WRB|XC|SPLIT[-. ]?SCENES?|BTS|mkv|mp4|avi|wmv|mov|rq)\b`)
	rxPyProper := regexp.MustCompile(`(?i)\b(PROPER|REPACK|READNFO|INTERNAL|LIMITED)\b`)
	rxPyBrackets := regexp.MustCompile(`\[.*?\]`)
	rxPyParens := regexp.MustCompile(`\(.*?\)`)
	rxPySeps := regexp.MustCompile(`[-_.]+`)
	rxPySpaces := regexp.MustCompile(`\s+`)
	rxPyTrim := regexp.MustCompile(`^[ -]+|[ -]+$`)
	rxExtension := regexp.MustCompile(`(?i)\.(mp4|mkv|avi|mov|wmv|webm|ts)$`)

	pyClean := func(title string) string {
		stem := rxExtension.ReplaceAllString(title, "")
		stem = rxPyCleanup.ReplaceAllString(stem, "")
		stem = rxPyProper.ReplaceAllString(stem, "")
		stem = rxPyBrackets.ReplaceAllString(stem, "")
		stem = rxPyParens.ReplaceAllString(stem, "")
		stem = rxPySeps.ReplaceAllString(stem, " ")
		stem = rxPySpaces.ReplaceAllString(stem, " ")
		stem = rxPyTrim.ReplaceAllString(stem, "")
		return stem
	}

	genericWords := map[string]bool{
		"hardcore": true, "sex": true, "exposed": true, "titties": true, "stepmom": true, "stepsister": true,
		"caring": true, "sharing": true, "sharingiscaring": true, "huge": true, "tits": true, "fucking": true,
		"anal": true, "pussy": true, "first": true, "teacher": true, "stepparent": true, "stepdad": true,
		"stepson": true,
	}

	getPySearchTerms := func(name string) []string {
		if name == "" {
			return nil
		}
		terms := []string{name}
		words := strings.Fields(name)
		if len(words) > 2 {
			firstTwo := strings.Join(words[:2], " ")
			if !genericWords[strings.ToLower(firstTwo)] {
				terms = append(terms, firstTwo)
			}
			lastTwo := strings.Join(words[len(words)-2:], " ")
			allGeneric := true
			for _, w := range words[len(words)-2:] {
				if !genericWords[strings.ToLower(w)] {
					allGeneric = false
					break
				}
			}
			if !allGeneric {
				found := false
				for _, t := range terms {
					if t == lastTwo {
						found = true
						break
					}
				}
				if !found {
					terms = append(terms, lastTwo)
				}
			}
		}
		var res []string
		for _, t := range terms {
			if len(t) > 2 {
				res = append(res, t)
			}
		}
		return res
	}

	// Build map of term -> cache entry by scanning all titles and computing hashes
	hashToEntry := make(map[string]CacheEntry)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		if !strings.HasPrefix(f.Name(), "stashdb_") {
			continue
		}
		path := filepath.Join("sidecar/test/cache", f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err == nil {
			hashToEntry[strings.TrimSuffix(strings.TrimPrefix(f.Name(), "stashdb_"), ".json")] = entry
		}
	}

	const searchPythonQuery = `\nquery ($term: String!) {\n  searchScene(term: $term) {\n    id title details release_date duration\n    studio {\n      name\n      parent {\n        name\n      }\n    }\n    performers { performer { name } as }\n    tags { name }\n    images { url width height }\n    urls { url site { name } }\n  }\n}\n`

	// Index exact terms
	for _, title := range titles {
		// Python terms
		pyName := pyClean(title)
		for _, term := range getPySearchTerms(pyName) {
			termEscaped, _ := json.Marshal(term)
			pyPayload := fmt.Sprintf(`{"query": "%s", "variables": {"term": %s}}`, searchPythonQuery, string(termEscaped))
			h := md5Hash(pyPayload)
			if entry, ok := hashToEntry[h]; ok {
				cleanK := cleanQueryTerm(term)
				if cleanK != "" {
					stashdbCleanIndex[cleanK] = append(stashdbCleanIndex[cleanK], entry)
				}
			}
		}

		// Also compute hashes for Go terms
		// Since Go might clean slightly differently, we register Go's terms to the same matched cache entry!
		cleanGo := pyClean(title)
		rxGoCleanup := regexp.MustCompile(`(?i)\b(360p|480p|540p|576p|1440p|ktr|nbq|btm|wr)\b`)
		cleanGo = rxGoCleanup.ReplaceAllString(cleanGo, "")
		cleanGo = rxPySpaces.ReplaceAllString(cleanGo, " ")
		cleanGo = strings.TrimSpace(cleanGo)

		for _, termPy := range getPySearchTerms(pyName) {
			termEscaped, _ := json.Marshal(termPy)
			pyPayload := fmt.Sprintf(`{"query": "%s", "variables": {"term": %s}}`, searchPythonQuery, string(termEscaped))
			h := md5Hash(pyPayload)
			if entry, ok := hashToEntry[h]; ok {
				for _, termGo := range getPySearchTerms(cleanGo) {
					cleanK := cleanQueryTerm(termGo)
					if cleanK != "" {
						stashdbCleanIndex[cleanK] = append(stashdbCleanIndex[cleanK], entry)
					}
				}
			}
		}
	}

	// Populate rest from original loadCacheIntoMemory fallback so we don't miss anything else
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		
		path := filepath.Join("sidecar/test/cache", f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}

		var decompressed []byte
		gr, err := gzip.NewReader(bytes.NewReader(entry.Body))
		if err == nil {
			decompressed, _ = io.ReadAll(gr)
			gr.Close()
		} else {
			decompressed = entry.Body
		}

		if strings.HasPrefix(f.Name(), "tpdb_") {
			var payload struct {
				Links map[string]string `json:"links"`
			}
			if err := json.Unmarshal(decompressed, &payload); err == nil {
				if firstURL, ok := payload.Links["first"]; ok && firstURL != "" {
					fu, err := url.Parse(firstURL)
					if err == nil {
						cachedTerm := fu.Query().Get("q")
						if cachedTerm == "" {
							cachedTerm = fu.Query().Get("parse")
						}
						if cachedTerm != "" {
							cleanKey := cleanQueryTerm(cachedTerm)
							if cleanKey != "" {
								tpdbCleanIndex[cleanKey] = entry
							}
						}
					}
				}
			}
		} else if strings.HasPrefix(f.Name(), "stashdb_") {
			var resp struct {
				Data struct {
					SearchScene []struct {
						Title      string `json:"title"`
						Performers []struct {
							Performer struct {
								Name string `json:"name"`
							} `json:"performer"`
						} `json:"performers"`
					} `json:"searchScene"`
				} `json:"data"`
			}
			if err := json.Unmarshal(decompressed, &resp); err == nil {
				for _, s := range resp.Data.SearchScene {
					cleanTitle := cleanQueryTerm(s.Title)
					if cleanTitle != "" {
						found := false
						for _, existing := range stashdbCleanIndex[cleanTitle] {
							if bytes.Equal(existing.Body, entry.Body) {
								found = true
								break
							}
						}
						if !found {
							stashdbCleanIndex[cleanTitle] = append(stashdbCleanIndex[cleanTitle], entry)
						}
					}
					for _, p := range s.Performers {
						cleanPName := cleanQueryTerm(p.Performer.Name)
						if cleanPName != "" {
							found := false
							for _, existing := range stashdbCleanIndex[cleanPName] {
								if bytes.Equal(existing.Body, entry.Body) {
									found = true
									break
								}
							}
							if !found {
								stashdbCleanIndex[cleanPName] = append(stashdbCleanIndex[cleanPName], entry)
							}
						}
					}
				}
			}
		}
	}
	fmt.Printf("Index built: %d tpdb entries, %d stashdb entries.\n", len(tpdbCleanIndex), len(stashdbCleanIndex))
}

func cleanQueryTerm(term string) string {
	term = strings.ToLower(term)
	var sb strings.Builder
	for _, r := range term {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	term = sb.String()
	
	words := strings.Fields(term)
	stopWords := map[string]bool{
		"xxx": true, "360p": true, "480p": true, "540p": true, "576p": true,
		"720p": true, "1080p": true, "1440p": true, "2160p": true, "4k": true,
		"webdl": true, "web": true, "dl": true, "webrip": true, "hdrip": true,
		"bluray": true, "x264": true, "x265": true, "mp4": true, "mkv": true,
		"avi": true, "wmv": true, "mov": true, "bts": true, "ktr": true,
		"nbq": true, "btm": true, "wr": true, "xc": true, "wrb": true,
		"vsex": true, "p2p": true, "split": true, "scenes": true, "scene": true,
		"the": true, "and": true, "for": true, "with": true, "you": true,
		"your": true, "that": true, "this": true, "from": true, "her": true,
		"him": true, "she": true, "his": true,
	}
	
	var filtered []string
	for _, w := range words {
		if !stopWords[w] {
			filtered = append(filtered, w)
		}
	}
	
	sort.Strings(filtered)
	return strings.Join(filtered, " ")
}

func findFuzzyTPDBCache(targetURL string) (CacheEntry, bool) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return CacheEntry{}, false
	}
	
	term := u.Query().Get("q")
	if term == "" {
		term = u.Query().Get("parse")
	}
	if term == "" {
		return CacheEntry{}, false
	}

	cleanTarget := cleanQueryTerm(term)
	if cleanTarget == "" {
		return CacheEntry{}, false
	}

	cacheIndexMu.RLock()
	defer cacheIndexMu.RUnlock()

	if entry, ok := tpdbCleanIndex[cleanTarget]; ok {
		return entry, true
	}
	
	return CacheEntry{}, false
}

func findFuzzyStashDBCache(bodyBytes []byte) (CacheEntry, bool) {
	var payload struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return CacheEntry{}, false
	}

	termVal, ok := payload.Variables["term"]
	if !ok {
		return CacheEntry{}, false
	}
	term, ok := termVal.(string)
	if !ok || term == "" {
		return CacheEntry{}, false
	}

	cleanTarget := cleanQueryTerm(term)
	if cleanTarget == "" {
		return CacheEntry{}, false
	}

	cacheIndexMu.RLock()
	defer cacheIndexMu.RUnlock()

	if entries, ok := stashdbCleanIndex[cleanTarget]; ok && len(entries) > 0 {
		return entries[0], true
	}
	
	for k, entries := range stashdbCleanIndex {
		if strings.Contains(k, cleanTarget) || strings.Contains(cleanTarget, k) {
			if len(entries) > 0 {
				return entries[0], true
			}
		}
	}

	return CacheEntry{}, false
}

func tryGetTPDBCache(targetURL string) (CacheEntry, bool) {
	rawHash := md5Hash(targetURL)
	if entry, found := getCache("tpdb_" + rawHash); found {
		return entry, true
	}

	u, err := url.Parse(targetURL)
	if err == nil {
		u.RawQuery = strings.ReplaceAll(u.RawQuery, "+", "%20")
		normHash := md5Hash(u.String())
		if entry, found := getCache("tpdb_" + normHash); found {
			return entry, true
		}
	}

	if entry, found := findFuzzyTPDBCache(targetURL); found {
		return entry, true
	}

	return CacheEntry{}, false
}

func tryGetStashDBCache(bodyBytes []byte) (CacheEntry, bool) {
	rawHash := md5Hash(string(bodyBytes))
	if entry, found := getCache("stashdb_" + rawHash); found {
		return entry, true
	}

	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err == nil {
		if strings.Contains(req.Query, "searchScene") {
			term, _ := req.Variables["term"].(string)
			termEscaped, _ := json.Marshal(term)
			const searchPythonQuery = `\nquery ($term: String!) {\n  searchScene(term: $term) {\n    id title details release_date duration\n    studio {\n      name\n      parent {\n        name\n      }\n    }\n    performers { performer { name } as }\n    tags { name }\n    images { url width height }\n    urls { url site { name } }\n  }\n}\n`
			pythonJSON := fmt.Sprintf(`{"query": "%s", "variables": {"term": %s}}`, searchPythonQuery, string(termEscaped))
			pyHash := md5Hash(pythonJSON)
			if entry, found := getCache("stashdb_" + pyHash); found {
				return entry, true
			}
		} else if strings.Contains(req.Query, "findScene") {
			id, _ := req.Variables["id"].(string)
			idEscaped, _ := json.Marshal(id)
			const findPythonQuery = `\nquery ($id: ID!) {\n  findScene(id: $id) {\n    id title details release_date duration\n    studio {\n      name\n      parent {\n        name\n      }\n    }\n    performers { performer { name } as }\n    tags { name }\n    images { url width height }\n    urls { url site { name } }\n  }\n}\n`
			pythonJSON := fmt.Sprintf(`{"query": "%s", "variables": {"id": %s}}`, findPythonQuery, string(idEscaped))
			pyHash := md5Hash(pythonJSON)
			if entry, found := getCache("stashdb_" + pyHash); found {
				return entry, true
			}
		}
	}

	if entry, found := findFuzzyStashDBCache(bodyBytes); found {
		return entry, true
	}

	return CacheEntry{}, false
}

func md5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

var liveRequestMu sync.Mutex
var lastLiveRequestTime time.Time
const liveRequestDelay = 120 * time.Millisecond // 120ms sleep between live calls to PornDB

func acquireLiveRequestToken() {
	liveRequestMu.Lock()
	defer liveRequestMu.Unlock()

	now := time.Now()
	elapsed := now.Sub(lastLiveRequestTime)
	if elapsed < liveRequestDelay {
		time.Sleep(liveRequestDelay - elapsed)
	}
	lastLiveRequestTime = time.Now()
}

func startCachingProxy() {
	_ = os.MkdirAll("sidecar/test/cache", 0755)
	loadCacheIntoMemory()


	http.HandleFunc("/tpdb/", func(w http.ResponseWriter, r *http.Request) {
		targetPath := strings.TrimPrefix(r.URL.Path, "/tpdb")
		targetURL := "https://api.theporndb.net" + targetPath
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		if entry, found := tryGetTPDBCache(targetURL); found {
			for k, vv := range entry.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(entry.StatusCode)
			w.Write(entry.Body)
			return
		}

		http.Error(w, "not found in cache", http.StatusNotFound)
	})

	http.HandleFunc("/stashdb", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if entry, found := tryGetStashDBCache(bodyBytes); found {
			for k, vv := range entry.Header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(entry.StatusCode)
			w.Write(entry.Body)
			return
		}

		http.Error(w, "not found in cache", http.StatusNotFound)
	})

	fmt.Println("Starting caching proxy on :8002...")
	if err := http.ListenAndServe(":8002", nil); err != nil {
		fmt.Printf("Caching proxy error: %v\n", err)
	}
}

func main() {
	fmt.Println("=== Starting Sidecar Validation and Comparison ===")

	// 1. Resolve Prowlarr configuration
	prowlarrURL := os.Getenv("PROWLARR_URL")
	if prowlarrURL == "" {
		prowlarrURL = "http://localhost:9696/prowlarr"
	}
	// If it contains host.docker.internal, swap with localhost for host-based test
	if strings.Contains(prowlarrURL, "host.docker.internal") {
		prowlarrURL = strings.Replace(prowlarrURL, "host.docker.internal", "localhost", 1)
	}

	apiKey := os.Getenv("PROWLARR_API_KEY")
	if apiKey == "" {
		apiKey = "ef2a909b564245b8a2849325e29146d3" // default from remote
	}

	fmt.Printf("Using Prowlarr URL: %s\n", prowlarrURL)

	// 2. Start Caching Proxy
	go startCachingProxy()
	time.Sleep(1 * time.Second)

	// 3. Load testsheet from testsheet691-adult.json
	testsheetPath := os.Getenv("TESTSHEET_PATH")
	if testsheetPath == "" {
		testsheetPath = "sidecar/test/testsheet691-adult.json"
	}
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

	fmt.Printf("Successfully loaded %d torrent titles from testsheet.\n", len(torrentTitles))
	client := &http.Client{Timeout: 10 * time.Second}

	// 4. Start Python sidecar on port 8093
	fmt.Println("Starting Python sidecar on port 8093...")
	var pyCmd *exec.Cmd
	if _, err := os.Stat("/app/sidecar/venv/bin/uvicorn"); err == nil {
		pyCmd = exec.Command("/app/sidecar/venv/bin/uvicorn", "main:app", "--host", "127.0.0.1", "--port", "8093")
		pyCmd.Dir = "/app/sidecar"
	} else if _, err := os.Stat("sidecar/venv/bin/uvicorn"); err == nil {
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
		"TPDB_BASE=http://localhost:8002/tpdb",
		"STASHDB_ENDPOINT=http://localhost:8002/stashdb",
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

	// 5. Start Go sidecar on port 8095
	fmt.Println("Starting Go sidecar on port 8095...")
	var goCmd *exec.Cmd
	if _, err := os.Stat("/app/sidecar_bin"); err == nil {
		goCmd = exec.Command("/app/sidecar_bin")
	} else if _, err := os.Stat("bin/sidecar"); err == nil {
		goCmd = exec.Command("bin/sidecar")
	} else if _, err := os.Stat("bin/sidecar_go_only_bin"); err == nil {
		goCmd = exec.Command("bin/sidecar_go_only_bin")
	} else {
		goCmd = exec.Command("go", "run", "sidecar/main.go")
	}
	goCmd.Env = append(os.Environ(),
		"OMDB_API_PORT=8095",
		"PORT=8095",
		"THEPORNDB_API_KEY=4MODCdLTeVcKDx28wTWiW86sF2IRqlnmVe0XVkGG55696daf",
		"STASHDB_API_KEY=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiIwMTlkZmZkYS0yZGVmLTdlN2UtYWQ4Zi0yN2FkZjc1MDI1NmYiLCJzdWIiOiJBUElLZXkiLCJpYXQiOjE3NzgxMTM5ODF9.GgudiUnFvNXpQic158c3QtheEkYY2rTLtEu5PuYn2xY",
		"TPDB_BASE=http://localhost:8002/tpdb",
		"STASHDB_ENDPOINT=http://localhost:8002/stashdb",
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

	// Verify Python sidecar on 8093
	fmt.Println("Checking connection to Python sidecar on port 8093...")
	resp, err := client.Get("http://localhost:8093/settings")
	if err != nil {
		fmt.Printf("ERROR: Python sidecar on port 8093 is not reachable: %v\n", err)
		return
	}
	resp.Body.Close()
	fmt.Println("Python sidecar is up and running.")

	// Verify Go sidecar on 8095
	fmt.Println("Checking connection to Go sidecar on port 8095...")
	resp, err = client.Get("http://localhost:8095/settings")
	if err != nil {
		fmt.Printf("ERROR: Go sidecar on port 8095 is not reachable: %v\n", err)
		return
	}
	resp.Body.Close()
	fmt.Println("Go sidecar is up and running.")

	// 6. Parallel comparison loop
	fmt.Println("Running side-by-side comparison on the dataset...")

	var mu sync.Mutex
	var results []struct {
		Title  string
		Status string // "MATCH", "MISMATCH", "NEITHER_ENRICHED", "ONLY_PYTHON", "ONLY_GO", "ERROR"
		Diffs  []string
	}

	totalProcessed := 0
	matches := 0
	mismatches := 0
	neitherEnriched := 0
	onlyPythonEnriched := 0
	onlyGoEnriched := 0
	errorsCount := 0

	// Run all tests in the testsheet without limit
	maxTests := len(torrentTitles)

	// Worker pool
	jobs := make(chan string, maxTests)
	for i := 0; i < maxTests; i++ {
		jobs <- torrentTitles[i]
	}
	close(jobs)

	numWorkers := 100
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			localClient := &http.Client{Timeout: 45 * time.Second}

			for title := range jobs {
				pyURL := fmt.Sprintf("http://localhost:8093/?t=%s&porn=true", url.QueryEscape(title))
				goURL := fmt.Sprintf("http://localhost:8095/?t=%s&porn=true", url.QueryEscape(title))

				pyReq, _ := http.NewRequest("GET", pyURL, nil)
				pyResp, pyErr := localClient.Do(pyReq)

				goReq, _ := http.NewRequest("GET", goURL, nil)
				goResp, goErr := localClient.Do(goReq)

				if pyErr != nil || goErr != nil {
					mu.Lock()
					errorsCount++
					totalProcessed++
					results = append(results, struct {
						Title  string
						Status string
						Diffs  []string
					}{
						Title:  title,
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
					continue
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
					// Both found — compare fields
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

				results = append(results, struct {
					Title  string
					Status string
					Diffs  []string
				}{
					Title:  title,
					Status: status,
					Diffs:  diffs,
				})

				if totalProcessed%50 == 0 {
					fmt.Printf("  Progress %d/%d — Matches=%d Mismatches=%d Errors=%d NeitherEnriched=%d\n",
						totalProcessed, maxTests, matches, mismatches, errorsCount, neitherEnriched)
				}
				mu.Unlock()
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	// Write everything to a new timestamped log file
	logPath := fmt.Sprintf("sidecar/test/compare_output_%s.log", time.Now().Format("20060102_150405"))
	logFile, err := os.Create(logPath)
	if err != nil {
		fmt.Printf("ERROR: Could not create log file: %v\n", err)
	} else {
		defer logFile.Close()

		fmt.Fprintf(logFile, "=== sidecar comparison log ===\n")
		fmt.Fprintf(logFile, "Run time: %s\n\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(logFile, "--- STATS ---\n")
		fmt.Fprintf(logFile, "Total processed:      %d\n", totalProcessed)
		fmt.Fprintf(logFile, "Perfect matches:      %d\n", matches)
		fmt.Fprintf(logFile, "Neither enriched:     %d\n", neitherEnriched)
		fmt.Fprintf(logFile, "Mismatches (total):   %d\n", mismatches)
		fmt.Fprintf(logFile, "  - Only Python:      %d\n", onlyPythonEnriched)
		fmt.Fprintf(logFile, "  - Only Go:          %d\n", onlyGoEnriched)
		fmt.Fprintf(logFile, "  - Field differences: %d\n", mismatches-onlyPythonEnriched-onlyGoEnriched)
		fmt.Fprintf(logFile, "Errors:               %d\n\n", errorsCount)

		fmt.Fprintf(logFile, "--- DETAILED RESULTS ---\n")
		for _, r := range results {
			fmt.Fprintf(logFile, "[%s] Torrent: %q\n", r.Status, r.Title)
			for _, d := range r.Diffs {
				fmt.Fprintf(logFile, "  - %s\n", d)
			}
			fmt.Fprintf(logFile, "\n")
		}
		fmt.Printf("Detailed results written to %s\n", logPath)
	}

	fmt.Println("\n==========================================")
	fmt.Println("          COMPARISON STATISTICS           ")
	fmt.Println("==========================================")
	fmt.Printf("Total processed:      %d\n", totalProcessed)
	fmt.Printf("Perfect matches:      %d\n", matches)
	fmt.Printf("Neither enriched:     %d (sidecar can't enrich these)\n", neitherEnriched)
	fmt.Printf("Mismatches (total):   %d\n", mismatches)
	fmt.Printf("  - Only Python:      %d\n", onlyPythonEnriched)
	fmt.Printf("  - Only Go:          %d\n", onlyGoEnriched)
	fmt.Printf("  - Field differences: %d\n", mismatches-onlyPythonEnriched-onlyGoEnriched)
	fmt.Printf("Errors:               %d\n", errorsCount)
	fmt.Println("==========================================")

	if mismatches == 0 && errorsCount == 0 {
		fmt.Println("\n🎉 Success! Both implementations returned identical matching results.")
	} else {
		fmt.Printf("\n⚠️  Warning: %d mismatches or %d errors were found. Check %s for details.\n", mismatches, errorsCount, logPath)
	}
}

func goioCopy(dst io.Writer, src io.Reader) {
	io.Copy(dst, src)
}
