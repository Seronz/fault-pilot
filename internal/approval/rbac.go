// Package approval menyediakan RBAC dan Human Approval Gate.
//
// Latar belakang: di lingkungan produksi, aksi berisiko tinggi tidak boleh
// dieksekusi otomatis. Setiap aksi dengan risk tertentu wajib disetujui manusia
// yang berwenang (lihat config/rbac.yaml). Prinsipnya fail closed: user tanpa
// role/permission, role tidak dikenal, atau approval kadaluarsa => DITOLAK.
package approval

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Permission adalah izin bertindak, mis. "read" atau "approve:high".
type Permission string

const (
	// PermRead mengizinkan membaca insiden/daftar approval.
	PermRead Permission = "read"
	// PermApproveLow mengizinkan menyetujui aksi berisiko rendah.
	PermApproveLow Permission = "approve:low"
	// PermApproveMedium mengizinkan menyetujui aksi berisiko sedang.
	PermApproveMedium Permission = "approve:medium"
	// PermApproveHigh mengizinkan menyetujui aksi berisiko tinggi.
	PermApproveHigh Permission = "approve:high"
)

// Risk level aksi. Nilai ini konsisten dengan executor.RiskLevel.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// ApprovePermission mengembalikan permission untuk menyetujui risk tertentu.
func ApprovePermission(risk string) Permission { return Permission("approve:" + risk) }

// KnownRisk melaporkan apakah risk termasuk yang dikenal.
func KnownRisk(risk string) bool {
	switch risk {
	case RiskLow, RiskMedium, RiskHigh:
		return true
	default:
		return false
	}
}

// ErrRBAC menandai kesalahan konfigurasi RBAC.
var ErrRBAC = errors.New("approval: rbac error")

// RBACConfig adalah bentuk mentah config/rbac.yaml.
type RBACConfig struct {
	Roles map[string][]string `yaml:"roles"`
	Users map[string]string   `yaml:"users"`
}

// RBAC memetakan user -> role -> kumpulan permission.
type RBAC struct {
	roles map[string]map[Permission]struct{}
	users map[string]string
}

// NewRBAC membangun RBAC dari konfigurasi. User yang memakai role tidak dikenal
// ditolak (fail closed) dengan mengembalikan error.
func NewRBAC(cfg RBACConfig) (*RBAC, error) {
	r := &RBAC{
		roles: make(map[string]map[Permission]struct{}, len(cfg.Roles)),
		users: make(map[string]string, len(cfg.Users)),
	}
	for role, perms := range cfg.Roles {
		if role == "" {
			return nil, fmt.Errorf("%w: nama role kosong", ErrRBAC)
		}
		set := make(map[Permission]struct{}, len(perms))
		for _, p := range perms {
			if p == "" {
				continue
			}
			set[Permission(p)] = struct{}{}
		}
		r.roles[role] = set
	}
	for user, role := range cfg.Users {
		if _, ok := r.roles[role]; !ok {
			return nil, fmt.Errorf("%w: user %q memakai role tidak dikenal %q", ErrRBAC, user, role)
		}
		r.users[user] = role
	}
	return r, nil
}

// LoadRBAC membaca file RBAC YAML. Bila file tidak ada, RBAC kosong dikembalikan
// (semua orang ditolak => fail closed), bukan error.
func LoadRBAC(path string) (*RBAC, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewRBAC(RBACConfig{})
		}
		return nil, err
	}
	var cfg RBACConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRBAC, err)
	}
	return NewRBAC(cfg)
}

// Role mengembalikan role user (bila ada).
func (r *RBAC) Role(user string) (string, bool) {
	if r == nil {
		return "", false
	}
	role, ok := r.users[user]
	return role, ok
}

// Has melaporkan apakah user memiliki permission tertentu.
func (r *RBAC) Has(user string, perm Permission) bool {
	if r == nil {
		return false
	}
	role, ok := r.users[user]
	if !ok {
		return false
	}
	perms, ok := r.roles[role]
	if !ok {
		return false
	}
	_, ok = perms[perm]
	return ok
}

// CanRead melaporkan apakah user boleh membaca.
func (r *RBAC) CanRead(user string) bool { return r.Has(user, PermRead) }

// CanApprove melaporkan apakah user boleh menyetujui aksi pada risk tertentu.
func (r *RBAC) CanApprove(user, risk string) bool {
	if !KnownRisk(risk) {
		return false
	}
	return r.Has(user, ApprovePermission(risk))
}
