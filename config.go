/*
 * This file is part of the Rubrion Group.
 *
 * Licensed under the Rubrion Public License (RPL), Version 1, 2026.
 * You may not use this file except in compliance with the License.
 *
 * License:
 * https://rubrionmc.github.io/.github/licensens/RUBRION_PUBLIC_LICENSE
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 */
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server      ServerConfig      `toml:"server"`
	Limitations LimitationsConfig `toml:"limitations"`
	Discovery   DiscoveryConfig   `toml:"discovery"`
	Timings     TimingConfig      `toml:"timings"`
	Backends    BackendsConfig    `toml:"backends"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
}

type LimitationsConfig struct {
	MaxConnections      int   `toml:"max_connections"`
	MaxPacketsPerSecond int32 `toml:"max_pks_per_sec"`
	MaxBytesPerSecond   int32 `toml:"max_bytes_per_sec"`
	MaxBytesPerPacket   int32 `toml:"max_bytes_per_pk"`
}

type DiscoveryConfig struct {
	Namespace        string `toml:"namespace"`
	K8sClusterDomain string `toml:"k8s_cluster_domain"`
}

type TimingConfig struct {
	BackendDial         time.Duration `toml:"backend_dial"`
	DiscoveryInterval   time.Duration `toml:"discovery_interval"`
	HealthcheckDial     time.Duration `toml:"healthcheck_dial"`
	HealthcheckInterval time.Duration `toml:"healthcheck_interval"`
	LogLimitInterval    time.Duration `toml:"log_limit_interval"`
}

type BackendsConfig struct {
	Primary  BackendConfig `toml:"primary"`
	Fallback BackendConfig `toml:"fallback"`
}

type BackendConfig struct {
	Name string `toml:"name"`
	Port int    `toml:"port"`
}

func LoadConfigFromFile(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is empty")
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
		return nil, fmt.Errorf("config string is empty")
	}

	var cfg Config
	if _, err := toml.Decode(tomlString, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
