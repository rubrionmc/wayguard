package main

import (
	"errors"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type DurationMs time.Duration

func (d *DurationMs) UnmarshalText(text []byte) error {
	str := string(text)

	parsed, err := time.ParseDuration(str)
	if err != nil {
		return err
	}

	*d = DurationMs(parsed.Milliseconds())
	return nil
}

type Config struct {
	Server    ServerConfig    `toml:"server"`
	Discovery DiscoveryConfig `toml:"discovery"`
	Timings   TimingConfig    `toml:"timings"`
	Backends  BackendsConfig  `toml:"backends"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
}

type DiscoveryConfig struct {
	Enabled   bool   `toml:"enabled"`
	Namespace string `toml:"namespace"`
}

type TimingConfig struct {
	BackendDial          DurationMs `toml:"backend_dial"`
	DiscoveryInterval    DurationMs `toml:"discovery_interval"`
	HealthcheckDial      DurationMs `toml:"healthcheck_dial"`
	HealthcheckInterval  DurationMs `toml:"healthcheck_interval"`
	LogRateLimitInterval DurationMs `toml:"log_rate_limit_interval"`
}

type BackendsConfig struct {
	Primary  BackendConfig `toml:"primary"`
	Fallback BackendConfig `toml:"fallback"`
}

type BackendConfig struct {
	Type string `toml:"type"`
	Port int    `toml:"port"`
}

func LoadConfigFromFile(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return LoadConfigFromData(data)
}

func LoadConfigFromData(tomlData []byte) (*Config, error) {
	tomlString := string(tomlData)
	if tomlString == "" {
		return nil, errors.New("config string is empty")
	}

	var cfg Config
	if _, err := toml.Decode(tomlString, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
