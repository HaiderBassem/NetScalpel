package probes

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"sync"
	"time"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"
)

func RunFacebook(cfg config.FacebookConfig) engine.ProbeResult {
	fmt.Println("[INFO] Initiating Facebook CDN Saturation Test...")

	cmd := exec.Command("yt-dlp", "-g", "-f", "bestvideo", cfg.VideoURL)
	out, err := cmd.Output()
	if err != nil {
		return engine.ProbeResult{Status: "FAILED", Error: fmt.Sprintf("yt-dlp execution failed: %v", err)}
	}

	streamURL := strings.TrimSpace(string(out))
	if streamURL == "" {
		return engine.ProbeResult{Status: "FAILED", Error: "stream URL is empty or expired"}
	}

	duration := time.Duration(cfg.DurationSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	state := engine.NewProbeState()
	httpClient := engine.NewOptimizedClient(200)
	var wg sync.WaitGroup

	go state.MonitorThroughput(ctx, "FB Edge")

	for i := 0; i < cfg.Connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					startByte := rand.Int63n(50 * 1024 * 1024)
					endByte := startByte + (5 * 1024 * 1024)
					_ = engine.ExecuteRangeRequest(ctx, httpClient, streamURL, startByte, endByte, state)
				}
			}
		}()
	}

	wg.Wait()
	fmt.Println("\n[INFO] Facebook test complete.")
	return state.CompileResult()
}
