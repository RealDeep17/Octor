package stremio

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/urfave/cli"
)

func NewClient(c *cli.Context) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			if ip := net.ParseIP(host); ip != nil {
				if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					return nil, fmt.Errorf("SSRF prevention: routing to private/loopback/link-local IP address %s is rejected", ip.String())
				}
				return dialer.DialContext(ctx, network, addr)
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			var allowedIP net.IP
			for _, ip := range ips {
				if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
					return nil, fmt.Errorf("SSRF prevention: routing to private/loopback/link-local IP address %s is rejected", ip.String())
				}
				if allowedIP == nil {
					allowedIP = ip
				}
			}

			if allowedIP == nil {
				return nil, fmt.Errorf("SSRF prevention: no allowed IP addresses resolved for host %s", host)
			}

			return dialer.DialContext(ctx, network, net.JoinHostPort(allowedIP.String(), port))
		},
	}

	// Configure proxy if provided
	proxyURL := c.String(StremioProxyFlag)
	if proxyURL != "" {
		if parsedURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsedURL)
		}
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	return client
}

