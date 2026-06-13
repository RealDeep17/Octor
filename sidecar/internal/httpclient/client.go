package httpclient

import (
	"bytes"
	"io"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

var defaultClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
	},
	Timeout: 8 * time.Second,
}

func Do(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	var resp *http.Response
	var err error
	maxAttempts := 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if len(bodyBytes) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err = defaultClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
				log.Warnf("HTTP request to %s failed with status %d (attempt %d/%d)", req.URL.String(), resp.StatusCode, attempt, maxAttempts)
				resp.Body.Close()
				if attempt < maxAttempts {
					time.Sleep(time.Duration(attempt) * 1 * time.Second)
					continue
				}
			} else {
				return resp, nil
			}
		} else {
			log.Warnf("HTTP request to %s failed with error: %v (attempt %d/%d)", req.URL.String(), err, attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(time.Duration(attempt) * 1 * time.Second)
				continue
			}
		}
	}

	return resp, err
}
