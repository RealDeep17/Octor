package stremio

import (
	"flag"
	"net/http"
	"strings"
	"testing"

	"github.com/urfave/cli"
)

func TestNewClient_SSRFPrevention(t *testing.T) {
	app := cli.NewApp()
	flagSet := flag.NewFlagSet("test", flag.ContinueOnError)
	flagSet.String(StremioUserAgentFlag, "", "")
	flagSet.String(StremioProxyFlag, "", "")
	c := cli.NewContext(app, flagSet, nil)

	client := NewClient(c)

	testURLs := []string{
		"http://127.0.0.1:8080/manifest.json",
		"http://localhost:8080/manifest.json",
		"http://10.0.0.1/manifest.json",
		"http://192.168.1.1/manifest.json",
		"http://169.254.169.254/latest/meta-data/",
	}

	for _, u := range testURLs {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			t.Fatalf("failed to create request for %s: %v", u, err)
		}
		_, err = client.Do(req)
		if err == nil {
			t.Errorf("expected request to %s to fail due to SSRF prevention, but it succeeded", u)
			continue
		}

		if !strings.Contains(err.Error(), "SSRF prevention") {
			t.Errorf("expected SSRF prevention error for %s, but got: %v", u, err)
		}
	}
}
