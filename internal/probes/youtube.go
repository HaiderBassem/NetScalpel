package probes

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"

	"github.com/kkdai/youtube/v2"
)

func RunYouTube(cfg config.YouTubeConfig) engine.ProbeResult {
	fmt.Println("[INFO] Initiating YouTube GGC Test...")

	client := youtube.Client{}
	video, err := client.GetVideo(cfg.VideoID)
	if err != nil {
		return engine.ProbeResult{Status: "FAILED", Error: fmt.Sprintf("metadata fetch failed: %v", err)}
	}

	var format *youtube.Format
	for i := range video.Formats {
		if strings.Contains(video.Formats[i].QualityLabel, "2160p") {
			format = &video.Formats[i]
			break
		}
	}

	if format == nil {
		return engine.ProbeResult{Status: "FAILED", Error: "2160p format unavailable"}
	}

	streamURL, err := client.GetStreamURL(video, format)
	if err != nil {
		return engine.ProbeResult{Status: "FAILED", Error: "stream resolution failed"}
	}

	duration := time.Duration(cfg.DurationSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	state := engine.NewProbeState()
	httpClient := engine.NewOptimizedClient(200)
	var wg sync.WaitGroup

	go state.MonitorThroughput(ctx, "GGC")

	for i := 0; i < cfg.Connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					start := rand.Int63n(200 * 1024 * 1024)
					end := start + (15 * 1024 * 1024)
					_ = engine.ExecuteRangeRequest(ctx, httpClient, streamURL, start, end, state)
				}
			}
		}()
	}

	wg.Wait()
	fmt.Println("\n[INFO] YouTube test complete.")
	return state.CompileResult()
}
