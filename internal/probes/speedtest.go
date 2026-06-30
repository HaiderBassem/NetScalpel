package probes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"
)

type speedtestJSON struct {
	Download struct {
		Bandwidth int `json:"bandwidth"` // bytes/s in official CLI
	} `json:"download"`
	Upload struct {
		Bandwidth int `json:"bandwidth"`
	} `json:"upload"`
	Server struct {
		Name    string `json:"name"`
		ID      int    `json:"id"`
		Host    string `json:"host"`
		Country string `json:"country"`
	} `json:"server"`
	Interface struct {
		ExternalIP string `json:"externalIp"`
	} `json:"interface"`
	Result struct {
		URL string `json:"url"`
	} `json:"result"`
}

// RunSpeedtest executes the Speedtest CLI and parses its JSON output.
// If cfg.ServerID is set (e.g. "62149"), the test is pinned to that server.
// Otherwise the CLI auto-selects the closest server.
func RunSpeedtest(cfg config.SpeedtestConfig) engine.ProbeResult {
	fmt.Println("[INFO] Initiating Speedtest CLI Test...")

	args := []string{"--format=json", "--accept-license", "--accept-gdpr"}
	if cfg.ServerID != "" {
		args = append(args, "--server-id="+cfg.ServerID)
		fmt.Printf("[INFO] Pinning to server ID: %s\n", cfg.ServerID)
	} else {
		fmt.Println("[INFO] No server ID set — auto-selecting nearest server.")
	}

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	cmdPath := filepath.Join(exeDir, "speedtest")
	cmd := exec.Command(cmdPath, args...)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errorMsg := fmt.Sprintf("speedtest failed: %v", err)
		if stderr.Len() > 0 {
			errorMsg += fmt.Sprintf(" | stderr: %s", stderr.String())
		}
		return engine.ProbeResult{
			Status: "FAILED",
			Error:  errorMsg,
		}
	}

	var data speedtestJSON
	if err := json.Unmarshal(out.Bytes(), &data); err != nil {
		return engine.ProbeResult{
			Status: "FAILED",
			Error:  "failed to parse speedtest JSON: " + err.Error(),
		}
	}

	// Official Speedtest CLI reports bandwidth in bytes/s — convert to Mbps.
	downloadMbps := float64(data.Download.Bandwidth) * 8 / 1_000_000
	uploadMbps := float64(data.Upload.Bandwidth) * 8 / 1_000_000

	serverLabel := data.Server.Name
	if data.Server.Country != "" {
		serverLabel += " (" + data.Server.Country + ")"
	}

	fmt.Printf("[INFO] Speedtest done — ↓ %.2f Mbps  ↑ %.2f Mbps  server: %s\n",
		downloadMbps, uploadMbps, serverLabel)

	return engine.ProbeResult{
		Status:     "SUCCESS",
		AvgMbps:    downloadMbps,
		PeakMbps:   downloadMbps,
		UploadMbps: uploadMbps,
		ServerName: serverLabel,
		ServerIP:   data.Server.Host,
		ClientIP:   data.Interface.ExternalIP,
	}
}
