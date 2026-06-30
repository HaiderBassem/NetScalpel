package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the root configuration loaded from config.json.
type Config struct {
	OutputFile string       `json:"output_file"`
	MikroTik   MikroTikConfig `json:"mikrotik"`
	Packages   []PackageEntry `json:"packages"`
	Probes     ProbeConfig  `json:"probes"`
}

// MikroTikConfig holds SSH connection parameters for the RouterOS device.
type MikroTikConfig struct {
	Address                       string `json:"address"`
	Port                          int    `json:"port"`
	Username                      string `json:"username"`
	Password                      string `json:"password"`
	ConnectTimeoutSeconds         int    `json:"connect_timeout_seconds"`
	CooldownBetweenPackagesSeconds int   `json:"cooldown_between_packages_seconds"`
}

// PackageEntry represents a single PPPoE interface (ISP package) to be tested.
type PackageEntry struct {
	Name                string `json:"name"`
	InterfaceName       string `json:"interface_name"`
	Order               int    `json:"order"`
	AdvertisedSpeedMbps int    `json:"advertised_speed_mbps"`
	Enabled             bool   `json:"enabled"`
}

// ProbeConfig groups all probe-specific settings.
type ProbeConfig struct {
	YouTube   YouTubeConfig   `json:"youtube"`
	Facebook  FacebookConfig  `json:"facebook"`
	Fast      FastConfig      `json:"fast"`
	Download  DownloadConfig  `json:"download"`
	Speedtest SpeedtestConfig `json:"speedtest"`
}

type YouTubeConfig struct {
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority"`
	VideoID         string `json:"video_id"`
	Connections     int    `json:"connections"`
	DurationSeconds int    `json:"duration_seconds"`
}

type FacebookConfig struct {
	Enabled         bool   `json:"enabled"`
	Priority        int    `json:"priority"`
	VideoURL        string `json:"video_url"`
	Connections     int    `json:"connections"`
	DurationSeconds int    `json:"duration_seconds"`
}

type FastConfig struct {
	Enabled         bool `json:"enabled"`
	Priority        int  `json:"priority"`
	Connections     int  `json:"connections"`
	DurationSeconds int  `json:"duration_seconds"`
}

type DownloadConfig struct {
	Enabled         bool     `json:"enabled"`
	Priority        int      `json:"priority"`
	TargetURLs      []string `json:"target_urls"`
	Connections     int      `json:"connections"`
	DurationSeconds int      `json:"duration_seconds"`
}

type SpeedtestConfig struct {
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
	// ServerID pins the test to a specific Speedtest.net server.
	// Leave empty ("") to let the CLI auto-select the nearest server.
	ServerID string `json:"server_id,omitempty"`
}

// Load reads and parses the JSON configuration file at the given path.
// Returns a descriptive error if the file cannot be opened or parsed.
func Load(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open config file %q: %w", path, err)
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// validate checks that required fields are present and values are in range.
func (c *Config) validate() error {
	if c.OutputFile == "" {
		return fmt.Errorf("output_file must not be empty")
	}
	if c.MikroTik.Address == "" {
		return fmt.Errorf("mikrotik.address must not be empty")
	}
	if c.MikroTik.Port <= 0 || c.MikroTik.Port > 65535 {
		return fmt.Errorf("mikrotik.port must be between 1 and 65535, got %d", c.MikroTik.Port)
	}
	if c.MikroTik.Username == "" {
		return fmt.Errorf("mikrotik.username must not be empty")
	}
	if c.MikroTik.ConnectTimeoutSeconds <= 0 {
		c.MikroTik.ConnectTimeoutSeconds = 10
	}
	if c.MikroTik.CooldownBetweenPackagesSeconds < 0 {
		c.MikroTik.CooldownBetweenPackagesSeconds = 0
	}
	if len(c.Packages) == 0 {
		return fmt.Errorf("packages list must not be empty")
	}
	for i, p := range c.Packages {
		if p.InterfaceName == "" {
			return fmt.Errorf("packages[%d] (%q): interface_name must not be empty", i, p.Name)
		}
		if p.Order <= 0 {
			return fmt.Errorf("packages[%d] (%q): order must be > 0, got %d", i, p.Name, p.Order)
		}
	}
	return nil
}
