package services

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	m2tp "github.com/webtor-io/magnet2torrent/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEffectiveWantedIndices(t *testing.T) {
	t.Run("all wanted when no selection", func(t *testing.T) {
		wanted, all := effectiveWantedIndices(4, nil, nil)
		assert.True(t, all)
		assert.Nil(t, wanted)
	})

	t.Run("unwanted only selects everything else", func(t *testing.T) {
		wanted, all := effectiveWantedIndices(4, nil, []int{1, 3})
		assert.False(t, all)
		assert.Equal(t, []int{0, 2}, wanted)
	})

	t.Run("wanted and unwanted are combined", func(t *testing.T) {
		wanted, all := effectiveWantedIndices(5, []int{4, 2, 1}, []int{2})
		assert.False(t, all)
		assert.Equal(t, []int{1, 4}, wanted)
	})

	t.Run("out of range wanted indices are ignored", func(t *testing.T) {
		wanted, all := effectiveWantedIndices(2, []int{-1, 0, 3}, nil)
		assert.False(t, all)
		assert.Equal(t, []int{0}, wanted)
	})
}

func TestParseDownloadClientUsernames(t *testing.T) {
	raw := []byte(`{"username":"first@example.com"}
{"username":" second@example.com "}
{"host":"transmission","username":"FIRST@example.com"}`)

	assert.Equal(t, []string{"first@example.com", "second@example.com"}, parseDownloadClientUsernames(raw))
}

func TestArrRequestClassificationMatchesAnyConfiguredTransmissionClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/transmission/rpc", nil)
	c.Request.SetBasicAuth("second@example.com", "secret")

	assert.True(t, isWhisparrRequest(c, []string{"first@example.com", "SECOND@example.com"}))
	assert.True(t, isRadarrOrSonarrRequest(c, []string{"radarr@example.com"}, []string{"second@example.com"}))
	assert.False(t, isRadarrOrSonarrRequest(c, []string{"radarr@example.com"}, []string{"sonarr@example.com"}))
}

func TestTransmissionServiceSaveTorrentsUsesPersistFileDir(t *testing.T) {
	tempDir := t.TempDir()
	persistFile := filepath.Join(tempDir, "nested", "transmission_torrents.json")
	s := &TransmissionService{
		persistFile: persistFile,
		trackedTorrent: map[string]TrackedTorrent{
			"hash": {InfoHash: "hash", Name: "Name"},
		},
	}

	require.NoError(t, s.saveTorrents())
	_, err := os.Stat(persistFile)
	require.NoError(t, err)
}

func TestTransmissionService_HandleRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set up temporary persistence dir
	tempDir, err := os.MkdirTemp("", "transmission-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Mock Vault Server
	mockVaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate status = 2 (Completed)
		_, _ = w.Write([]byte(`{"status": 2, "stored_size": 129368064, "total_size": 129368064}`))
	}))
	defer mockVaultServer.Close()

	// Mock Torrent Server
	mockTorrentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sintel.torrent", r.URL.Path)
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(loadSintel(t))
	}))
	defer mockTorrentServer.Close()

	vaultURL, err := url.Parse(mockVaultServer.URL)
	assert.NoError(t, err)
	vHost, vPortStr, err := net.SplitHostPort(vaultURL.Host)
	assert.NoError(t, err)
	vPort, err := strconv.Atoi(vPortStr)
	assert.NoError(t, err)

	// Initialize Services
	rm := NewTestResourceMap()
	tsclm, _ := rm.ts.Get()
	tsclmm := tsclm.(*TorrentStoreClientMock)
	m2tclm, _ := rm.m2t.Get()
	m2tclmm := m2tclm.(*Magnet2TorrentClientMock)

	apiKey := "super-secure-key"
	s := &TransmissionService{
		rm:             rm,
		apiKey:         apiKey,
		vaultHost:      vHost,
		vaultPort:      vPort,
		persistFile:    filepath.Join(tempDir, "transmission_torrents.json"),
		httpClient:     mockVaultServer.Client(),
		trackedTorrent: make(map[string]TrackedTorrent),
	}

	// 1. CSRF Verification
	t.Run("CSRF Handshake Fail", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", nil)
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, "octor-transmission-session-id", w.Header().Get("X-Transmission-Session-Id"))
	})

	// 2. Authentication Verification
	t.Run("Auth Fail Missing API Key", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", nil)
		req.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, `Basic realm="Transmission"`, w.Header().Get("WWW-Authenticate"))
	})

	t.Run("Auth Fail Wrong Basic Auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", nil)
		req.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		req.SetBasicAuth("", "wrong-key")
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Auth Success with Basic Auth", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody, _ := json.Marshal(TransmissionRPCReq{
			Method: "session-get",
		})
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBody))
		req.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		req.SetBasicAuth("", apiKey)
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp TransmissionRPCResp
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "success", resp.Result)
		assert.Equal(t, "4.0.0", resp.Arguments["version"])
	})

	t.Run("Auth Success with X-Api-Key Header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		reqBody, _ := json.Marshal(TransmissionRPCReq{
			Method: "session-get",
		})
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBody))
		req.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		req.Header.Set("X-Api-Key", apiKey)
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	// 3. RPC Methods Verification
	t.Run("torrent-add, torrent-get, torrent-remove lifecycle", func(t *testing.T) {
		// Mock resource map behaviors
		tsclmm.On("Touch", mock.Anything, mock.Anything, mock.Anything).Return(nil, status.Error(codes.NotFound, "not found"))
		tsclmm.On("Push", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		m2tclmm.On("Magnet2Torrent", mock.Anything, mock.Anything, mock.Anything).Return(&m2tp.Magnet2TorrentReply{
			Torrent: loadSintel(t),
		}, nil)

		// A. torrent-add
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		addArgs := map[string]interface{}{
			"filename": sintelMagnet,
		}
		reqBody, _ := json.Marshal(TransmissionRPCReq{
			Method:    "torrent-add",
			Arguments: addArgs,
		})
		req, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBody))
		req.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		req.Header.Set("X-Api-Key", apiKey)
		c.Request = req

		s.HandleRPC(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var addResp TransmissionRPCResp
		err = json.Unmarshal(w.Body.Bytes(), &addResp)
		assert.NoError(t, err)
		require.Equal(t, "success", addResp.Result)

		added, ok := addResp.Arguments["torrent-added"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "Sintel", added["name"])
		assert.Equal(t, "08ada5a7a6183aae1e09d831df6748d566095a10", added["hashString"])

		// B. torrent-get
		wGet := httptest.NewRecorder()
		cGet, _ := gin.CreateTestContext(wGet)

		reqBodyGet, _ := json.Marshal(TransmissionRPCReq{
			Method: "torrent-get",
		})
		reqGet, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBodyGet))
		reqGet.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		reqGet.Header.Set("X-Api-Key", apiKey)
		cGet.Request = reqGet

		s.HandleRPC(cGet)

		assert.Equal(t, http.StatusOK, wGet.Code)

		var getResp TransmissionRPCResp
		err = json.Unmarshal(wGet.Body.Bytes(), &getResp)
		assert.NoError(t, err)
		assert.Equal(t, "success", getResp.Result)

		torrents, ok := getResp.Arguments["torrents"].([]interface{})
		assert.True(t, ok)
		assert.Len(t, torrents, 1)

		torrent0 := torrents[0].(map[string]interface{})
		assert.Equal(t, "Sintel", torrent0["name"])
		assert.Equal(t, "08ada5a7a6183aae1e09d831df6748d566095a10", torrent0["hashString"])
		assert.Equal(t, float64(1.0), torrent0["percentDone"])
		assert.Equal(t, float64(6), torrent0["status"]) // Seeding status

		// A2. torrent-add via HTTP URL
		wUrl := httptest.NewRecorder()
		cUrl, _ := gin.CreateTestContext(wUrl)

		addArgsUrl := map[string]interface{}{
			"filename": mockTorrentServer.URL + "/sintel.torrent",
		}
		reqBodyUrl, _ := json.Marshal(TransmissionRPCReq{
			Method:    "torrent-add",
			Arguments: addArgsUrl,
		})
		reqUrl, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBodyUrl))
		reqUrl.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		reqUrl.Header.Set("X-Api-Key", apiKey)
		cUrl.Request = reqUrl

		s.HandleRPC(cUrl)

		assert.Equal(t, http.StatusOK, wUrl.Code)

		var addRespUrl TransmissionRPCResp
		err = json.Unmarshal(wUrl.Body.Bytes(), &addRespUrl)
		assert.NoError(t, err)
		require.Equal(t, "success", addRespUrl.Result)

		// C. torrent-remove
		wRemove := httptest.NewRecorder()
		cRemove, _ := gin.CreateTestContext(wRemove)

		removeArgs := map[string]interface{}{
			"ids": []interface{}{added["id"]},
		}
		reqBodyRemove, _ := json.Marshal(TransmissionRPCReq{
			Method:    "torrent-remove",
			Arguments: removeArgs,
		})
		reqRemove, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBodyRemove))
		reqRemove.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		reqRemove.Header.Set("X-Api-Key", apiKey)
		cRemove.Request = reqRemove

		s.HandleRPC(cRemove)

		assert.Equal(t, http.StatusOK, wRemove.Code)

		// Verify empty lists on subsequent get
		wGetEmpty := httptest.NewRecorder()
		cGetEmpty, _ := gin.CreateTestContext(wGetEmpty)

		reqGetEmpty, _ := http.NewRequest(http.MethodPost, "/transmission/rpc", bytes.NewBuffer(reqBodyGet))
		reqGetEmpty.Header.Set("X-Transmission-Session-Id", "octor-transmission-session-id")
		reqGetEmpty.Header.Set("X-Api-Key", apiKey)
		cGetEmpty.Request = reqGetEmpty

		s.HandleRPC(cGetEmpty)

		var getRespEmpty TransmissionRPCResp
		_ = json.Unmarshal(wGetEmpty.Body.Bytes(), &getRespEmpty)
		torrentsEmptyRaw := getRespEmpty.Arguments["torrents"]
		if torrentsEmptyRaw != nil {
			torrentsEmpty, ok := torrentsEmptyRaw.([]interface{})
			assert.True(t, ok)
			assert.Len(t, torrentsEmpty, 0)
		}
	})
}

func TestWeb_WebhookAndSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Mock Torrent Server
	mockTorrentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sintel.torrent", r.URL.Path)
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(loadSintel(t))
	}))
	defer mockTorrentServer.Close()

	// Mock Prowlarr Server
	mockProwlarrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/indexer" {
			assert.Equal(t, "test-prowlarr-key", r.URL.Query().Get("apikey"))
			_, _ = w.Write([]byte(`[
				{
					"id": 1,
					"name": "mock-indexer",
					"enable": true,
					"priority": 25
				}
			]`))
			return
		}

		assert.Equal(t, "/api/v1/search", r.URL.Path)
		assert.Equal(t, "test-prowlarr-key", r.URL.Query().Get("apikey"))
		assert.Equal(t, "ubuntu", r.URL.Query().Get("query"))

		_, _ = w.Write([]byte(`[
			{
				"title": "Ubuntu Linux ISO",
				"infoHash": "08ada5a7a6183aae1e09d831df6748d566095a10",
				"size": 129368064,
				"seeders": 10,
				"leechers": 5
			}
		]`))
	}))
	defer mockProwlarrServer.Close()

	// Initialize Services
	rm := NewTestResourceMap()
	tsclm, _ := rm.ts.Get()
	tsclmm := tsclm.(*TorrentStoreClientMock)
	m2tclm, _ := rm.m2t.Get()
	m2tclmm := m2tclm.(*Magnet2TorrentClientMock)

	apiKey := "super-secure-key"
	transmission := &TransmissionService{
		rm:     rm,
		apiKey: apiKey,
	}

	prowlarr := &ProwlarrClient{
		url:         mockProwlarrServer.URL,
		apiKey:      "test-prowlarr-key",
		client:      mockProwlarrServer.Client(),
		searchCache: make(map[string]CachedSearch),
	}

	web := &Web{
		rm:           rm,
		prowlarr:     prowlarr,
		transmission: transmission,
	}

	router := gin.New()
	router.POST("/webhook/ingest", web.postWebhookIngest)
	router.GET("/search", web.getSearch)

	t.Run("Webhook Ingest blocked without API key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhook/ingest", bytes.NewBuffer([]byte(`{"url": "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10"}`)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Webhook Ingest success with API key in header", func(t *testing.T) {
		tsclmm.On("Touch", mock.Anything, mock.Anything, mock.Anything).Return(nil, status.Error(codes.NotFound, "not found"))
		tsclmm.On("Push", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		m2tclmm.On("Magnet2Torrent", mock.Anything, mock.Anything, mock.Anything).Return(&m2tp.Magnet2TorrentReply{
			Torrent: loadSintel(t),
		}, nil)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhook/ingest", bytes.NewBuffer([]byte(`{"url": "`+sintelMagnet+`"}`)))
		req.Header.Set("X-Api-Key", apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			MagnetURI string `json:"magnet_uri"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "08ada5a7a6183aae1e09d831df6748d566095a10", resp.ID)
		assert.Equal(t, "Sintel", resp.Name)
	})

	t.Run("Webhook Ingest success with API key in query", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhook/ingest?api_key="+apiKey, bytes.NewBuffer([]byte(`{"url": "`+sintelMagnet+`"}`)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Webhook Ingest success with torrent URL", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhook/ingest?api_key="+apiKey, bytes.NewBuffer([]byte(`{"url": "`+mockTorrentServer.URL+`/sintel.torrent"}`)))
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Search blocked without API key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/search?q=ubuntu", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Search success with API key and query proxying", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/search?q=ubuntu", nil)
		req.Header.Set("X-Api-Key", apiKey)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var results []ProwlarrResultItem
		err := json.Unmarshal(w.Body.Bytes(), &results)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "Ubuntu Linux ISO", results[0].Title)
		assert.Equal(t, "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Ubuntu+Linux+ISO", results[0].MagnetURL)
	})
}
