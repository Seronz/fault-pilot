package approval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seronz/fault-pilot/internal/approval"
)

func sampleRBAC(t *testing.T) *approval.RBAC {
	t.Helper()
	r, err := approval.NewRBAC(approval.RBACConfig{
		Roles: map[string][]string{
			"viewer":   {"read"},
			"sre":      {"read", "approve:low", "approve:medium"},
			"sre_lead": {"read", "approve:low", "approve:medium", "approve:high"},
		},
		Users: map[string]string{
			"ronz@company.com":   "sre_lead",
			"sre@company.com":    "sre",
			"viewer@company.com": "viewer",
		},
	})
	if err != nil {
		t.Fatalf("NewRBAC: %v", err)
	}
	return r
}

func TestRBAC_Permissions(t *testing.T) {
	r := sampleRBAC(t)

	if !r.CanRead("sre@company.com") {
		t.Fatal("sre harus bisa read")
	}
	if r.CanApprove("sre@company.com", approval.RiskHigh) {
		t.Fatal("sre TIDAK boleh approve high")
	}
	if !r.CanApprove("sre@company.com", approval.RiskMedium) {
		t.Fatal("sre harus bisa approve medium")
	}
	if !r.CanApprove("ronz@company.com", approval.RiskHigh) {
		t.Fatal("sre_lead harus bisa approve high")
	}
	if !r.CanRead("viewer@company.com") {
		t.Fatal("viewer harus bisa read")
	}
	if r.CanApprove("viewer@company.com", approval.RiskLow) {
		t.Fatal("viewer TIDAK boleh approve")
	}
}

func TestRBAC_UnknownUserAndRisk(t *testing.T) {
	r := sampleRBAC(t)
	if _, ok := r.Role("ghost@company.com"); ok {
		t.Fatal("user tak dikenal tidak boleh punya role")
	}
	if r.Has("ghost@company.com", approval.PermRead) {
		t.Fatal("user tak dikenal harus ditolak")
	}
	if r.CanApprove("ronz@company.com", "critical") {
		t.Fatal("risk tak dikenal harus ditolak")
	}
}

func TestRBAC_UnknownRoleIsError(t *testing.T) {
	_, err := approval.NewRBAC(approval.RBACConfig{
		Roles: map[string][]string{"viewer": {"read"}},
		Users: map[string]string{"a@company.com": "sre_lead"},
	})
	if err == nil {
		t.Fatal("user dengan role tak dikenal harus error (fail closed)")
	}
}

func TestRBAC_NilSafe(t *testing.T) {
	var r *approval.RBAC
	if r.CanRead("x") || r.CanApprove("x", approval.RiskLow) {
		t.Fatal("RBAC nil harus menolak semua")
	}
}

func TestLoadRBAC_MissingFileDeniesAll(t *testing.T) {
	r, err := approval.LoadRBAC(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("LoadRBAC: %v", err)
	}
	if r.CanRead("ronz@company.com") {
		t.Fatal("RBAC kosong harus menolak semua")
	}
}

func TestLoadRBAC_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rbac.yaml")
	content := "roles:\n  sre_lead: [read, approve:high]\nusers:\n  ronz@company.com: sre_lead\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := approval.LoadRBAC(path)
	if err != nil {
		t.Fatalf("LoadRBAC: %v", err)
	}
	if !r.CanApprove("ronz@company.com", approval.RiskHigh) {
		t.Fatal("ronz harus bisa approve high")
	}
}
