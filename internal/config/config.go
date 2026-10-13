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

// ApprovalConfig mengatur RBAC & Human Approval Gate.
type ApprovalConfig struct {
	Enabled      bool     `yaml:"enabled"`       // aktifkan gate? default true
	Addr         string   `yaml:"addr"`          // alamat HTTP API approval, default :8082
	RBACPath     string   `yaml:"rbac_path"`     // path config/rbac.yaml
	RequireRisks []string `yaml:"require_risks"` // risk wajib approval, default [high]
	TTL          string   `yaml:"ttl"`           // masa berlaku approval, default 15m
}

// Config adalah konfigurasi tingkat atas.
type Config struct {
	Executor      ExecutorConfig      `yaml:"executor"`
	Observability ObservabilityConfig `yaml:"observability"`
	Approval      ApprovalConfig      `yaml:"approval"`
}

// ObservabilityConfig mengatur endpoint metrics/health dan store.
type ObservabilityConfig struct {
	MetricsAddr string `yaml:"metrics_addr"`  // alamat HTTP /metrics,/healthz,/readyz
	StorePath   string `yaml:"store_path"`    // path SQLite untuk health check
	LastPollMax string `yaml:"last_poll_max"` // durasi maksimum sejak poll terakhir
}

// Default mengembalikan konfigurasi default (executor lab).
func Default() Config {
	return Config{
		Executor: ExecutorConfig{Type: "lab"},
		Observability: ObservabilityConfig{
			MetricsAddr: ":9090",
			StorePath:   "faultpilot.db",
			LastPollMax: "2m",
		},
		Approval: ApprovalConfig{
			Enabled:      true,
			Addr:         ":8082",
			RBACPath:     "config/rbac.yaml",
			RequireRisks: []string{"high"},
			TTL:          "15m",
		},
	}
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
	if cfg.Observability.MetricsAddr == "" {
		cfg.Observability.MetricsAddr = ":9090"
	}
	if cfg.Observability.StorePath == "" {
		cfg.Observability.StorePath = "faultpilot.db"
	}
	if cfg.Observability.LastPollMax == "" {
		cfg.Observability.LastPollMax = "2m"
	}
	if cfg.Approval.RBACPath == "" {
		cfg.Approval.RBACPath = "config/rbac.yaml"
	}
	if cfg.Approval.Addr == "" {
		cfg.Approval.Addr = ":8082"
	}
	if cfg.Approval.RequireRisks == nil {
		cfg.Approval.RequireRisks = []string{"high"}
	}
	if cfg.Approval.TTL == "" {
		cfg.Approval.TTL = "15m"
	}
	return cfg, nil
}
