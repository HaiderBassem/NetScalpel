package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"netscalpel/internal/config"
	"netscalpel/internal/engine"
	"netscalpel/internal/mikrotik"
	"netscalpel/internal/probes"
)

// ──────────────────────────────────────────────
// Result types
// ──────────────────────────────────────────────

// ConnectionInfo captures what happened when we tried to bring up the PPPoE link.
type ConnectionInfo struct {
	Status            string  `json:"status"`              // "CONNECTED" | "FAILED"
	TimeToConnectSecs float64 `json:"time_to_connect_seconds,omitempty"`
	AssignedIP        string  `json:"assigned_ip,omitempty"`
	Error             string  `json:"error,omitempty"`
}

// PackageReport is the full test result for one ISP package / PPPoE interface.
type PackageReport struct {
	PackageName         string                        `json:"package_name"`
	InterfaceName       string                        `json:"interface_name"`
	Order               int                           `json:"order"`
	AdvertisedSpeedMbps int                           `json:"advertised_speed_mbps"`
	Connection          ConnectionInfo                `json:"connection"`
	Tests               map[string]engine.ProbeResult `json:"tests"`
}

// MasterReport is the top-level JSON written to disk when all packages are done.
type MasterReport struct {
	Timestamp            string          `json:"timestamp"`
	RouterIdentity       string          `json:"router_identity"`
	TotalDurationSeconds float64         `json:"total_duration_seconds"`
	Packages             []PackageReport `json:"packages"`
}

// ──────────────────────────────────────────────
// Probe task helper
// ──────────────────────────────────────────────

// probeTask is a single speed-test probe waiting to be executed.
type probeTask struct {
	Name     string
	Priority int
	Execute  func() engine.ProbeResult
}

// buildProbeTasks creates the ordered list of probes based on config.
func buildProbeTasks(cfg *config.Config) []probeTask {
	var tasks []probeTask

	if cfg.Probes.YouTube.Enabled {
		tasks = append(tasks, probeTask{
			Name:     "youtube",
			Priority: cfg.Probes.YouTube.Priority,
			Execute:  func() engine.ProbeResult { return probes.RunYouTube(cfg.Probes.YouTube) },
		})
	}
	if cfg.Probes.Facebook.Enabled {
		tasks = append(tasks, probeTask{
			Name:     "facebook",
			Priority: cfg.Probes.Facebook.Priority,
			Execute:  func() engine.ProbeResult { return probes.RunFacebook(cfg.Probes.Facebook) },
		})
	}
	if cfg.Probes.Fast.Enabled {
		tasks = append(tasks, probeTask{
			Name:     "fast",
			Priority: cfg.Probes.Fast.Priority,
			Execute:  func() engine.ProbeResult { return probes.RunFast(cfg.Probes.Fast) },
		})
	}
	if cfg.Probes.Download.Enabled {
		tasks = append(tasks, probeTask{
			Name:     "download",
			Priority: cfg.Probes.Download.Priority,
			Execute:  func() engine.ProbeResult { return probes.RunFileDownload(cfg.Probes.Download) },
		})
	}
	if cfg.Probes.Speedtest.Enabled {
		tasks = append(tasks, probeTask{
			Name:     "speedtest",
			Priority: cfg.Probes.Speedtest.Priority,
			Execute:  func() engine.ProbeResult { return probes.RunSpeedtest(cfg.Probes.Speedtest) },
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Priority < tasks[j].Priority
	})
	return tasks
}

// runAllProbes executes every probe task and returns the results map.
func runAllProbes(tasks []probeTask) map[string]engine.ProbeResult {
	results := make(map[string]engine.ProbeResult, len(tasks))
	for _, task := range tasks {
		fmt.Printf("\n  [PROBE] Starting → %s (priority %d)\n", task.Name, task.Priority)
		results[task.Name] = task.Execute()
		fmt.Printf("  [PROBE] Finished → %s\n", task.Name)
	}
	return results
}

// skippedProbeMap returns all probes as SKIPPED with the given reason.
func skippedProbeMap(tasks []probeTask, reason string) map[string]engine.ProbeResult {
	results := make(map[string]engine.ProbeResult, len(tasks))
	for _, t := range tasks {
		results[t.Name] = engine.SkippedResult(reason)
	}
	return results
}

// ──────────────────────────────────────────────
// Main
// ──────────────────────────────────────────────

func main() {
	dryRun := flag.Bool("dry-run", false,
		"Print what would happen without executing anything on the MikroTik")
	configPath := flag.String("config", "config.json", "Path to the JSON configuration file")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║    NetScalpel  —  Multi-Package Engine    ║")
	fmt.Println("╚══════════════════════════════════════════╝")

	if *dryRun {
		fmt.Println("[DRY-RUN] Mode active — no changes will be made to the MikroTik.")
	}

	// ── 1. Load configuration ────────────────────────────────────────────────
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatalf("Configuration error: %v", err)
	}

	// Sort packages by their configured order.
	sort.Slice(cfg.Packages, func(i, j int) bool {
		return cfg.Packages[i].Order < cfg.Packages[j].Order
	})

	// Build the list of enabled packages only.
	var enabledPackages []config.PackageEntry
	for _, p := range cfg.Packages {
		if p.Enabled {
			enabledPackages = append(enabledPackages, p)
		}
	}

	if len(enabledPackages) == 0 {
		fatalf("No enabled packages found in configuration. Set at least one package's 'enabled' to true.")
	}

	// Collect all interface names (enabled or not) for the initial/final disable.
	allInterfaceNames := make([]string, len(cfg.Packages))
	for i, p := range cfg.Packages {
		allInterfaceNames[i] = p.InterfaceName
	}

	fmt.Printf("\n[INFO] %d package(s) enabled out of %d configured.\n",
		len(enabledPackages), len(cfg.Packages))
	fmt.Printf("[INFO] Probe suite: YouTube=%v Facebook=%v Fast=%v Download=%v Speedtest=%v\n",
		cfg.Probes.YouTube.Enabled, cfg.Probes.Facebook.Enabled,
		cfg.Probes.Fast.Enabled, cfg.Probes.Download.Enabled,
		cfg.Probes.Speedtest.Enabled)

	// ── 2. Connect to MikroTik ───────────────────────────────────────────────
	mt := mikrotik.New(
		cfg.MikroTik.Address,
		cfg.MikroTik.Port,
		cfg.MikroTik.Username,
		cfg.MikroTik.Password,
		time.Duration(cfg.MikroTik.ConnectTimeoutSeconds)*time.Second,
	)

	routerIdentity := "unknown"

	if !*dryRun {
		fmt.Printf("\n[SSH] Connecting to MikroTik at %s:%d...\n",
			cfg.MikroTik.Address, cfg.MikroTik.Port)
		if err := mt.Connect(); err != nil {
			fatalf("MikroTik SSH connection failed: %v", err)
		}
		defer mt.Close()

		routerIdentity, err = mt.GetRouterIdentity()
		if err != nil {
			fmt.Printf("[WARN] Could not read router identity: %v (continuing anyway)\n", err)
		}
		fmt.Printf("[SSH] Connected — Router Identity: %s\n", routerIdentity)
	} else {
		routerIdentity = "DRY-RUN"
	}

	// ── 3. Disable ALL packages (clean slate) ────────────────────────────────
	fmt.Println("\n[SETUP] Disabling all PPPoE interfaces (clean slate)...")
	if !*dryRun {
		if err := mt.DisableAll(allInterfaceNames); err != nil {
			// Non-fatal: log and continue — some interfaces may already be disabled.
			fmt.Printf("[WARN] DisableAll returned error(s): %v\n", err)
		} else {
			fmt.Println("[SETUP] All interfaces disabled successfully.")
		}
	} else {
		for _, n := range allInterfaceNames {
			fmt.Printf("[DRY-RUN] Would disable: %s\n", n)
		}
	}

	// ── 4. Build probe task list ─────────────────────────────────────────────
	probeTasks := buildProbeTasks(cfg)
	if len(probeTasks) == 0 {
		fatalf("No probes are enabled. Enable at least one probe in the 'probes' section.")
	}

	// ── 5. Run each package ──────────────────────────────────────────────────
	globalStart := time.Now()
	var packageReports []PackageReport
	cooldown := time.Duration(cfg.MikroTik.CooldownBetweenPackagesSeconds) * time.Second

	for idx, pkg := range enabledPackages {
		fmt.Printf("\n══════════════════════════════════════════════\n")
		fmt.Printf("  Package %d/%d  →  %s  (%s)\n",
			idx+1, len(enabledPackages), pkg.Name, pkg.InterfaceName)
		fmt.Printf("  Advertised speed: %d Mbps\n", pkg.AdvertisedSpeedMbps)
		fmt.Printf("══════════════════════════════════════════════\n")

		report := PackageReport{
			PackageName:         pkg.Name,
			InterfaceName:       pkg.InterfaceName,
			Order:               pkg.Order,
			AdvertisedSpeedMbps: pkg.AdvertisedSpeedMbps,
		}

		if *dryRun {
			report.Connection = ConnectionInfo{Status: "DRY-RUN"}
			report.Tests = skippedProbeMap(probeTasks, "dry-run mode")
			packageReports = append(packageReports, report)
			continue
		}

		// ── 5a. Enable the interface ─────────────────────────────────────────
		fmt.Printf("[PKG] Enabling interface %q...\n", pkg.InterfaceName)
		if err := mt.EnablePackage(pkg.InterfaceName); err != nil {
			errMsg := fmt.Sprintf("failed to enable interface: %v", err)
			fmt.Printf("[ERROR] %s\n", errMsg)
			report.Connection = ConnectionInfo{Status: "FAILED", Error: errMsg}
			report.Tests = skippedProbeMap(probeTasks, errMsg)
			packageReports = append(packageReports, report)
			continue
		}

		// ── 5b. Wait for PPPoE to connect ────────────────────────────────────
		connResult := mt.WaitForConnection(
			pkg.InterfaceName,
			120*time.Second,
		)

		if !connResult.Connected {
			fmt.Printf("[PKG] %s — connection FAILED: %s\n",
				pkg.InterfaceName, connResult.Error)
			report.Connection = ConnectionInfo{
				Status:            "FAILED",
				TimeToConnectSecs: connResult.TimeToConnectSecs,
				Error:             connResult.Error,
			}
			report.Tests = skippedProbeMap(probeTasks, connResult.Error)
		} else {
			fmt.Printf("[PKG] %s — CONNECTED in %.1fs  (IP: %s)\n",
				pkg.InterfaceName, connResult.TimeToConnectSecs, connResult.AssignedIP)
			report.Connection = ConnectionInfo{
				Status:            "CONNECTED",
				TimeToConnectSecs: connResult.TimeToConnectSecs,
				AssignedIP:        connResult.AssignedIP,
			}

			// ── 5c. Run all probes ────────────────────────────────────────────
			report.Tests = runAllProbes(probeTasks)
		}

		// ── 5d. Disable the interface ─────────────────────────────────────────
		fmt.Printf("\n[PKG] Disabling interface %q...\n", pkg.InterfaceName)
		if err := mt.DisablePackage(pkg.InterfaceName); err != nil {
			fmt.Printf("[WARN] Could not disable %q: %v (continuing)\n",
				pkg.InterfaceName, err)
		} else {
			fmt.Printf("[PKG] Interface %q disabled.\n", pkg.InterfaceName)
		}

		packageReports = append(packageReports, report)

		// ── 5e. Cooldown between packages ─────────────────────────────────────
		if cooldown > 0 && idx < len(enabledPackages)-1 {
			fmt.Printf("[PKG] Cooldown: waiting %s before next package...\n", cooldown)
			time.Sleep(cooldown)
		}
	}

	// ── 6. Final cleanup — disable everything ────────────────────────────────
	if !*dryRun {
		fmt.Println("\n[CLEANUP] Final pass — disabling all PPPoE interfaces...")
		if err := mt.DisableAll(allInterfaceNames); err != nil {
			fmt.Printf("[WARN] Final DisableAll error(s): %v\n", err)
		} else {
			fmt.Println("[CLEANUP] All interfaces disabled.")
		}
	}

	// ── 7. Write results ─────────────────────────────────────────────────────
	totalDuration := time.Since(globalStart).Seconds()
	masterReport := MasterReport{
		Timestamp:            time.Now().Format(time.RFC3339),
		RouterIdentity:       routerIdentity,
		TotalDurationSeconds: totalDuration,
		Packages:             packageReports,
	}

	if err := writeJSON(cfg.OutputFile, masterReport); err != nil {
		fatalf("Failed to write output file %q: %v", cfg.OutputFile, err)
	}

	fmt.Printf("\n╔══════════════════════════════════════════╗\n")
	fmt.Printf("║  All done — %.1f seconds total            \n", totalDuration)
	fmt.Printf("║  Results saved to: %s\n", cfg.OutputFile)
	fmt.Printf("╚══════════════════════════════════════════╝\n")
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Printf("\n[FATAL] "+format+"\n", args...)
	os.Exit(1)
}
