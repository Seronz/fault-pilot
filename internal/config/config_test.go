package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seronz/fault-pilot/internal/config"
)

func TestLoad_MissingFileUsesDefault(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Executor.Type != "lab" {
		t.Fatalf("default type = %q, want lab", cfg.Executor.Type)
	}
}

func TestLoad_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "executor:\n  type: systemctl\n  allowlist:\n    targets: [checkout, payments]\n  fault_injector_url: http://localhost:8081/control\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Executor.Type != "systemctl" {
		t.Fatalf("type = %q", cfg.Executor.Type)
	}
	if len(cfg.Executor.Allowlist.Targets) != 2 || cfg.Executor.Allowlist.Targets[0] != "checkout" {
		t.Fatalf("targets = %v", cfg.Executor.Allowlist.Targets)
	}
	if cfg.Executor.FaultInjectorURL != "http://localhost:8081/control" {
		t.Fatalf("url = %q", cfg.Executor.FaultInjectorURL)
	}
}
