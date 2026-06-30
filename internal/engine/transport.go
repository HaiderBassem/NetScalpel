package engine

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"time"
)

// NewOptimizedClient returns an http.Client configured for high-throughput concurrency.
func NewOptimizedClient(poolSize int) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        poolSize,
			MaxIdleConnsPerHost: poolSize,
			DisableCompression:  true,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
}

// ExecuteRangeRequest performs a byte-range HTTP GET request to saturate the link.
func ExecuteRangeRequest(ctx context.Context, client *http.Client, targetURL string, start, end int64, state *ProbeState) error {
	var currentIP string

	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			currentIP, _, _ = net.SplitHostPort(addr)
		},
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	buf := make([]byte, 128*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			state.AddBytes(n, currentIP)
		}
		if err != nil {
			return err
		}
	}
}
