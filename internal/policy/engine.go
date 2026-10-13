// Package policy memvalidasi Proposal sebelum dieksekusi.
//
// Alur singkat: proposal masuk -> cek allowlist aksi & target -> cek insiden
// masih aktif -> cek bukti benar milik insiden dan ada di store -> cek aturan
// state (sedang berjalan, cooldown, limit) -> bila semua lolos, insiden
// "diklaim" agar tidak dieksekusi ganda.
//
// Prinsipnya fail closed: apa pun yang meragukan (JSON tidak valid, target
// asing, bukti hilang, error store) langsung DITOLAK.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/seronz/fault-pilot/internal/domain"
)

const (
	// ActionRestartSimulatedService adalah satu-satunya aksi yang diizinkan.
	ActionRestartSimulatedService = "restart_simulated_service"
	// TargetCheckout adalah satu-satunya target yang diizinkan.
	TargetCheckout = "checkout"
	// ActiveStatus adalah status insiden yang dianggap masih aktif.
	ActiveStatus = "open"
)

const (
	DefaultCooldown              = 30 * time.Second
	DefaultMaxActionsPerIncident = 1
)

// Store adalah bagian store yang dibutuhkan policy engine.
type Store interface {
	Incident(ctx context.Context, id string) (domain.Incident, bool, error)
	EventExists(ctx context.Context, id string) (bool, error)
}

// Config mengatur allowlist dan aturan state. Field kosong memakai default.
type Config struct {
	AllowedActions        []string
	AllowedTargets        []string
	Cooldown              time.Duration
	MaxActionsPerIncident int
	Now                   func() time.Time // injectable untuk pengujian
}

// Engine memvalidasi proposal dan melacak aksi yang sedang berjalan.
type Engine struct {
	st             Store
	allowedActions map[string]struct{}
	allowedTargets map[string]struct{}
	cooldown       time.Duration
	maxPerIncident int
	now            func() time.Time

	mu          sync.Mutex
	running     map[string]struct{}  // insiden yang sedang dieksekusi
	lastAction  map[string]time.Time // waktu aksi terakhir per insiden
	actionCount map[string]int       // jumlah aksi per insiden
}

// New membuat Engine dengan konfigurasi default.
func New(st Store) *Engine { return NewWithConfig(st, Config{}) }

// NewWithConfig membuat Engine dengan konfigurasi tertentu.
func NewWithConfig(st Store, cfg Config) *Engine {
	if len(cfg.AllowedActions) == 0 {
		cfg.AllowedActions = []string{ActionRestartSimulatedService}
	}
	if len(cfg.AllowedTargets) == 0 {
		cfg.AllowedTargets = []string{TargetCheckout}
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = DefaultCooldown
	}
	if cfg.MaxActionsPerIncident <= 0 {
		cfg.MaxActionsPerIncident = DefaultMaxActionsPerIncident
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	e := &Engine{
		st:             st,
		allowedActions: make(map[string]struct{}, len(cfg.AllowedActions)),
		allowedTargets: make(map[string]struct{}, len(cfg.AllowedTargets)),
		cooldown:       cfg.Cooldown,
		maxPerIncident: cfg.MaxActionsPerIncident,
		now:            cfg.Now,
		running:        make(map[string]struct{}),
		lastAction:     make(map[string]time.Time),
		actionCount:    make(map[string]int),
	}
	for _, a := range cfg.AllowedActions {
		e.allowedActions[a] = struct{}{}
	}
	for _, t := range cfg.AllowedTargets {
		e.allowedTargets[t] = struct{}{}
	}
	return e
}

// ValidateJSON adalah pembungkus Validate untuk input JSON mentah.
// JSON tidak valid langsung ditolak (fail closed).
func (e *Engine) ValidateJSON(ctx context.Context, raw []byte) (bool, string) {
	var p domain.Proposal
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return false, "invalid json"
	}
	return e.Validate(ctx, p)
}

// Validate memeriksa proposal dan, bila lolos, mengklaim insiden agar tidak
// dieksekusi ganda. Mengembalikan (true, "ok") atau (false, alasan).
func (e *Engine) Validate(ctx context.Context, p domain.Proposal) (bool, string) {
	if e.st == nil {
		return false, "no store"
	}
	if p.IncidentID == "" {
		return false, "missing incident id"
	}
	if _, ok := e.allowedActions[p.Action]; !ok {
		return false, "action not allowed"
	}
	if _, ok := e.allowedTargets[p.Target]; !ok {
		return false, "target not allowed"
	}
	if len(p.EvidenceIDs) == 0 {
		return false, "missing evidence"
	}

	// Insiden harus ada dan masih aktif.
	inc, ok, err := e.st.Incident(ctx, p.IncidentID)
	if err != nil {
		return false, "store error"
	}
	if !ok {
		return false, "incident not found"
	}
	if inc.Status != ActiveStatus {
		return false, "incident not active"
	}

	// Setiap bukti harus milik insiden dan ada di store.
	belongs := make(map[string]struct{}, len(inc.EvidenceIDs))
	for _, id := range inc.EvidenceIDs {
		belongs[id] = struct{}{}
	}
	for _, id := range p.EvidenceIDs {
		if _, ok := belongs[id]; !ok {
			return false, "evidence not part of incident"
		}
		exists, err := e.st.EventExists(ctx, id)
		if err != nil {
			return false, "store error"
		}
		if !exists {
			return false, "evidence not found"
		}
	}

	// Aturan state diperiksa atomik supaya dua worker tidak lolos bersamaan.
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, busy := e.running[p.IncidentID]; busy {
		return false, "action already running"
	}
	if last, ok := e.lastAction[p.IncidentID]; ok {
		if e.now().Sub(last) < e.cooldown {
			return false, "cooldown active"
		}
	}
	if e.actionCount[p.IncidentID] >= e.maxPerIncident {
		return false, "action limit reached"
	}

	e.running[p.IncidentID] = struct{}{}
	return true, "ok"
}

// Done menandai aksi selesai: melepas klaim dan memperbarui cooldown/limit.
func (e *Engine) Done(incidentID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.running[incidentID]; !ok {
		return
	}
	delete(e.running, incidentID)
	e.lastAction[incidentID] = e.now()
	e.actionCount[incidentID]++
}

// Abort melepas klaim tanpa menghitung aksi (mis. eksekusi gagal).
func (e *Engine) Abort(incidentID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.running, incidentID)
}
