// Package healthcheck implements the -healthcheck flag. Distroless images have
// no shell, curl or wget, so the binary probes its own running instance and
// the container runtime looks at the exit code.
package healthcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// URL returns a URL that reaches a server listening on listenAddr from the
// same host. Wildcard hosts (":8080", "0.0.0.0:8080", "[::]:8080") mean "all
// interfaces" and are not dialable, so they are replaced with 127.0.0.1.
func URL(listenAddr, path string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("parse listen address %q: %w", listenAddr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: path}
	return u.String(), nil
}

// Check sends GET rawURL and succeeds only on 200 OK. The caller bounds the
// probe with a ctx deadline.
func Check(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	// A dedicated client: the probe targets the local host directly and must
	// never go through a proxy configured via HTTP_PROXY.
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d", rawURL, resp.StatusCode)
	}
	return nil
}
