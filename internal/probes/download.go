package probes

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"sync"
	"time"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"
)

func RunFileDownload(cfg config.DownloadConfig) engine.ProbeResult {
	fmt.Println("[INFO] Initiating Realistic Multi-File Download Test...")

	if len(cfg.TargetURLs) == 0 {
		return engine.ProbeResult{Status: "FAILED", Error: "no target URLs provided in configuration"}
	}

	duration := time.Duration(cfg.DurationSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	state := engine.NewProbeState()
	var wg sync.WaitGroup

	go state.MonitorThroughput(ctx, "Mirror Node")

	for i := 0; i < cfg.Connections; i++ {
		targetURL := cfg.TargetURLs[i%len(cfg.TargetURLs)]
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					tmpFile, err := os.CreateTemp("", "netscalpel-download-*.dat")
					if err != nil {
						time.Sleep(1 * time.Second)
						continue
					}

					tmpPath := tmpFile.Name()
					_ = executeFileDownload(ctx, url, tmpFile, state)

					_ = tmpFile.Close()
					_ = os.Remove(tmpPath)
				}
			}
		}(targetURL)
	}

	wg.Wait()
	fmt.Println("\n[INFO] File Download test complete.")
	return state.CompileResult()
}

func executeFileDownload(ctx context.Context, url string, dst *os.File, state *engine.ProbeState) error {
	var currentIP string

	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			addr := connInfo.Conn.RemoteAddr().String()
			currentIP, _, _ = net.SplitHostPort(addr)
		},
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       100,
			DisableCompression: true,
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).DialContext,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	writer := &monitoredWriter{
		file:  dst,
		ip:    currentIP,
		state: state,
		ctx:   ctx,
	}

	_, err = io.Copy(writer, resp.Body)
	return err
}

type monitoredWriter struct {
	file  *os.File
	ip    string
	state *engine.ProbeState
	ctx   context.Context
}

func (mw *monitoredWriter) Write(p []byte) (int, error) {
	if mw.ctx.Err() != nil {
		return 0, mw.ctx.Err()
	}

	n, err := mw.file.Write(p)
	if n > 0 {
		mw.state.AddBytes(n, mw.ip)
	}
	return n, err
}
