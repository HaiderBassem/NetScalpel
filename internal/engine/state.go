package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type NodeStat struct {
	IP    string `json:"IP"`
	Bytes int64  `json:"Bytes"`
}

type ProbeResult struct {
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	PeakMbps   float64    `json:"peak_mbps,omitempty"`
	AvgMbps    float64    `json:"avg_mbps,omitempty"`
	UploadMbps float64    `json:"upload_mbps,omitempty"`
	TopIPs     []NodeStat `json:"top_ips,omitempty"`
	ServerName string     `json:"server_name,omitempty"`
	Sponsor    string     `json:"sponsor,omitempty"`
	ServerIP   string     `json:"server_ip,omitempty"`
	ClientIP   string     `json:"client_ip,omitempty"`
}

type ProbeState struct {
	totalBytes int64
	maxSpeed   float64
	sumSpeed   float64
	ticks      int64
	ipMap      sync.Map
}

func NewProbeState() *ProbeState {
	return &ProbeState{}
}

func (s *ProbeState) AddBytes(n int, ip string) {
	atomic.AddInt64(&s.totalBytes, int64(n))
	if ip != "" {
		val, _ := s.ipMap.LoadOrStore(ip, new(int64))
		atomic.AddInt64(val.(*int64), int64(n))
	}
}

// MonitorThroughput calculates and logs the throughput every second.
func (s *ProbeState) MonitorThroughput(ctx context.Context, label string) {
	var lastBytes int64
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := atomic.LoadInt64(&s.totalBytes)
			mbps := float64(current-lastBytes) * 8 / 1000000
			lastBytes = current

			if mbps > s.maxSpeed {
				s.maxSpeed = mbps
			}
			if mbps > 0 {
				s.sumSpeed += mbps
				s.ticks++
			}

			top := s.GetTopNodes(1)
			ipStr := "Detecting..."
			if len(top) > 0 {
				ipStr = top[0].IP
			}
			fmt.Printf("\r[RUNNING] Speed: %8.2f Mbps | Top Node: %-20s", mbps, ipStr)
		}
	}
}

func (s *ProbeState) GetTopNodes(limit int) []NodeStat {
	var stats []NodeStat
	s.ipMap.Range(func(k, v interface{}) bool {
		stats = append(stats, NodeStat{
			IP:    k.(string),
			Bytes: atomic.LoadInt64(v.(*int64)),
		})
		return true
	})

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Bytes > stats[j].Bytes
	})

	if len(stats) > limit {
		return stats[:limit]
	}
	return stats
}

func (s *ProbeState) CompileResult() ProbeResult {
	avg := 0.0
	if s.ticks > 0 {
		avg = s.sumSpeed / float64(s.ticks)
	}

	return ProbeResult{
		Status:   "SUCCESS",
		PeakMbps: s.maxSpeed,
		AvgMbps:  avg,
		TopIPs:   s.GetTopNodes(3),
	}
}

// SkippedResult returns a zeroed ProbeResult used when a package failed to
// connect and the probe could not be executed.
func SkippedResult(reason string) ProbeResult {
	return ProbeResult{
		Status:   "SKIPPED",
		Error:    reason,
		PeakMbps: 0,
		AvgMbps:  0,
	}
}
