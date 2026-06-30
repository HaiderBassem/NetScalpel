package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"
)

func RunFast(cfg config.FastConfig) engine.ProbeResult {
	fmt.Println("[INFO] Initiating Fast.com (Netflix CDN) Native Emulator...")

	duration := time.Duration(cfg.DurationSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration+10*time.Second)
	defer cancel()

	targetURLs, err := fetchFastURLs(ctx)
	if err != nil {
		return engine.ProbeResult{Status: "FAILED", Error: fmt.Sprintf("API negotiation failed: %v", err)}
	}

	if len(targetURLs) == 0 {
		return engine.ProbeResult{Status: "FAILED", Error: "API returned no valid CDN targets"}
	}

	fmt.Printf("[INFO] Discovered %d Netflix OCA endpoints. Commencing test phase.\n", len(targetURLs))

	execCtx, execCancel := context.WithTimeout(context.Background(), duration)
	defer execCancel()

	state := engine.NewProbeState()
	var wg sync.WaitGroup

	httpClient := engine.NewOptimizedClient(cfg.Connections)

	go state.MonitorThroughput(execCtx, "Netflix OCA")

	for i := 0; i < cfg.Connections; i++ {
		target := targetURLs[i%len(targetURLs)]
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			for {
				select {
				case <-execCtx.Done():
					return
				default:
					startByte := rand.Int63n(100 * 1024 * 1024)
					endByte := startByte + (25 * 1024 * 1024) // 25MB chunks
					_ = engine.ExecuteRangeRequest(execCtx, httpClient, url, startByte, endByte, state)
				}
			}
		}(target)
	}

	wg.Wait()
	fmt.Println("\n[INFO] Fast.com test complete.")
	return state.CompileResult()
}

func fetchFastURLs(ctx context.Context) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fast.com", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	scriptRegex := regexp.MustCompile(`src="(/app-[^"]+\.js)"`)
	matches := scriptRegex.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		return nil, fmt.Errorf("application script not found")
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://fast.com"+matches[1], nil)
	if err != nil {
		return nil, err
	}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	jsBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	tokenRegex := regexp.MustCompile(`token:"([^"]+)"`)
	tokenMatches := tokenRegex.FindStringSubmatch(string(jsBody))
	if len(tokenMatches) < 2 {
		return nil, fmt.Errorf("authorization token extraction failed")
	}
	token := tokenMatches[1]

	apiURL := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=5", token)
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	apiBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var apiResponse struct {
		Targets []struct {
			URL string `json:"url"`
		} `json:"targets"`
	}

	if err := json.Unmarshal(apiBody, &apiResponse); err != nil {
		return nil, err
	}

	var urls []string
	for _, target := range apiResponse.Targets {
		if target.URL != "" {
			urls = append(urls, target.URL)
		}
	}

	return urls, nil
}
