// Package config memuat konfigurasi Fault Pilot dari file YAML.
//
// Contoh config.yaml:
//
//	executor:
//	  type: lab            # lab | systemctl | kubectl | aws_ec2 | ansible
//	  allowlist:
//	    targets: [checkout]
//	    runbooks: [runbooks/restart.yml]
//	  fault_injector_url: http://localhost:8081/control
//
// Bila file tidak ada, Default() dipakai (type=lab, dummy tetap default).
package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// Allowlist membatasi target/runbook yang boleh dieksekusi.
type Allowlist struct {
	Targets  []string `yaml:"targets"`
	Runbooks []string `yaml:"runbooks"`
}

// ExecutorConfig adalah konfigurasi executor.
type ExecutorConfig struct {
	Type             string    `yaml:"type"`
	Allowlist        Allowlist `yaml:"allowlist"`
	FaultInjectorURL string    `yaml:"fault_injector_url"`
}

// Config adalah konfigurasi tingkat atas.
type Config struct {
	Executor ExecutorConfig `yaml:"executor"`
}

// Default mengembalikan konfigurasi default (executor lab).
func Default() Config {
	return Config{Executor: ExecutorConfig{Type: "lab"}}
}

// Load membaca config.yaml. Bila file tidak ada, Default() dikembalikan.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Executor.Type == "" {
		cfg.Executor.Type = "lab"
	}
	return cfg, nil
}
