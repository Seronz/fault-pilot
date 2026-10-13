package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status merepresentasikan keadaan sebuah permohonan approval.
type Status string

const (
	// StatusPending menunggu keputusan.
	StatusPending Status = "pending"
	// StatusApproved disetujui (belum tentu dipakai).
	StatusApproved Status = "approved"
	// StatusRejected ditolak.
	StatusRejected Status = "rejected"
	// StatusExpired kadaluarsa sebelum diputuskan/dipakai.
	StatusExpired Status = "expired"
)

// DefaultTTL adalah masa berlaku default sebuah approval.
const DefaultTTL = 15 * time.Minute

// Error yang dikembalikan oleh Gate.
var (
	ErrNotFound    = errors.New("approval: request tidak ditemukan")
	ErrInvalid     = errors.New("approval: request tidak valid")
	ErrForbidden   = errors.New("approval: tidak berwenang")
	ErrNotPending  = errors.New("approval: request sudah diputuskan")
	ErrSelfApprove = errors.New("approval: pemohon tidak boleh menyetujui sendiri")
)

// Request adalah satu permohonan approval manusia.
type Request struct {
	ID          string    `json:"id"`
	IncidentID  string    `json:"incident_id"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Risk        string    `json:"risk"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	Reason      string    `json:"reason,omitempty"`
	Status      Status    `json:"status"`
	DecidedBy   string    `json:"decided_by,omitempty"`
	DecidedAt   time.Time `json:"decided_at,omitempty"`
	Note        string    `json:"note,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	Consumed    bool      `json:"consumed"`
	ConsumedAt  time.Time `json:"consumed_at,omitempty"`
}

// Observer menerima notifikasi saat sebuah approval diputuskan.
type Observer interface {
	ApprovalDecided(status string)
}

// Store menyimpan Request. Implementasi harus aman untuk akses konkuren.
type Store interface {
	Save(ctx context.Context, r Request) error
	Get(ctx context.Context, id string) (Request, bool, error)
	List(ctx context.Context) ([]Request, error)
}

// MemoryStore adalah Store in-memory (aman untuk akses konkuren).
type MemoryStore struct {
	mu    sync.RWMutex
	m     map[string]Request
	order []string
}

// NewMemoryStore membuat MemoryStore kosong.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{m: make(map[string]Request)}
}

// Save menyimpan/mengganti sebuah Request.
func (s *MemoryStore) Save(_ context.Context, r Request) error {
	if r.ID == "" {
		return fmt.Errorf("%w: id kosong", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[r.ID]; !ok {
		s.order = append(s.order, r.ID)
	}
	s.m[r.ID] = r
	return nil
}

// Get mengambil Request berdasarkan ID.
func (s *MemoryStore) Get(_ context.Context, id string) (Request, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	return r, ok, nil
}

// List mengembalikan salinan semua Request sesuai urutan pembuatan.
func (s *MemoryStore) List(context.Context) ([]Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Request, 0, len(s.order))
	for _, id := range s.order {
		if r, ok := s.m[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// Gate adalah Human Approval Gate: membuat, memutuskan, dan memeriksa approval.
type Gate struct {
	rbac      *RBAC
	store     Store
	ttl       time.Duration
	now       func() time.Time
	genID     func() string
	allowSelf bool
	observer  Observer

	mu sync.Mutex
}

// GateOption mengonfigurasi Gate.
type GateOption func(*Gate)

// WithTTL mengatur masa berlaku approval (default 15 menit).
func WithTTL(d time.Duration) GateOption {
	return func(g *Gate) {
		if d > 0 {
			g.ttl = d
		}
	}
}

// WithStore mengganti penyimpanan (default MemoryStore).
func WithStore(s Store) GateOption {
	return func(g *Gate) {
		if s != nil {
			g.store = s
		}
	}
}

// WithClock mengganti sumber waktu (untuk pengujian).
func WithClock(f func() time.Time) GateOption {
	return func(g *Gate) {
		if f != nil {
			g.now = f
		}
	}
}

// WithIDGenerator mengganti generator ID (untuk pengujian).
func WithIDGenerator(f func() string) GateOption {
	return func(g *Gate) {
		if f != nil {
			g.genID = f
		}
	}
}

// WithAllowSelfApproval mengizinkan pemohon menyetujui permohonannya sendiri.
// Default: tidak diizinkan (prinsip four-eyes).
func WithAllowSelfApproval() GateOption { return func(g *Gate) { g.allowSelf = true } }

// WithObserver memasang observer keputusan.
func WithObserver(o Observer) GateOption { return func(g *Gate) { g.observer = o } }

// NewGate membuat Gate. Bila rbac nil, semua user ditolak (fail closed).
func NewGate(rbac *RBAC, opts ...GateOption) *Gate {
	g := &Gate{
		rbac:      rbac,
		store:     NewMemoryStore(),
		ttl:       DefaultTTL,
		now:       time.Now,
		genID:     func() string { return uuid.NewString() },
		allowSelf: false,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Request membuat permohonan approval baru (status pending). Pemohon wajib
// memiliki permission "read". Risk harus dikenal.
func (g *Gate) Request(ctx context.Context, r Request) (Request, error) {
	if r.RequestedBy == "" || !g.rbac.CanRead(r.RequestedBy) {
		return Request{}, ErrForbidden
	}
	if r.IncidentID == "" || r.Action == "" || r.Target == "" {
		return Request{}, fmt.Errorf("%w: field wajib kosong", ErrInvalid)
	}
	if !KnownRisk(r.Risk) {
		return Request{}, fmt.Errorf("%w: risk %q tidak dikenal", ErrInvalid, r.Risk)
	}

	now := g.now()
	r.ID = g.genID()
	r.RequestedAt = now
	r.ExpiresAt = now.Add(g.ttl)
	r.Status = StatusPending
	r.DecidedBy = ""
	r.DecidedAt = time.Time{}
	r.Note = ""
	r.Consumed = false
	r.ConsumedAt = time.Time{}

	if err := g.store.Save(ctx, r); err != nil {
		return Request{}, err
	}
	return r, nil
}

// Approve menyetujui sebuah request. Approver wajib punya permission
// "approve:<risk>" dan (default) bukan pemohon sendiri.
func (g *Gate) Approve(ctx context.Context, id, approver, note string) (Request, error) {
	return g.decide(ctx, id, approver, StatusApproved, note)
}

// Reject menolak sebuah request.
func (g *Gate) Reject(ctx context.Context, id, approver, note string) (Request, error) {
	return g.decide(ctx, id, approver, StatusRejected, note)
}

func (g *Gate) decide(ctx context.Context, id, actor string, status Status, note string) (Request, error) {
	if actor == "" {
		return Request{}, ErrForbidden
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	r, ok, err := g.store.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if !ok {
		return Request{}, ErrNotFound
	}

	now := g.now()
	if r.Status == StatusPending && now.After(r.ExpiresAt) {
		r.Status = StatusExpired
		_ = g.store.Save(ctx, r)
		return Request{}, ErrNotPending
	}
	if r.Status != StatusPending {
		return Request{}, ErrNotPending
	}
	if !g.rbac.CanApprove(actor, r.Risk) {
		return Request{}, ErrForbidden
	}
	if !g.allowSelf && actor == r.RequestedBy {
		return Request{}, ErrSelfApprove
	}

	r.Status = status
	r.DecidedBy = actor
	r.DecidedAt = now
	r.Note = note
	if err := g.store.Save(ctx, r); err != nil {
		return Request{}, err
	}
	if g.observer != nil {
		g.observer.ApprovalDecided(string(status))
	}
	return r, nil
}

// Check dipakai oleh policy engine: mengembalikan true bila ada approval yang
// disetujui, cocok (incident/action/target/risk), belum dipakai, dan belum
// kadaluarsa. Approval yang cocok langsung ditandai "consumed" (sekali pakai).
func (g *Gate) Check(ctx context.Context, incidentID, action, target, risk string) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	reqs, err := g.store.List(ctx)
	if err != nil {
		return false, "approval store error"
	}
	now := g.now()
	for i := range reqs {
		r := reqs[i]
		if r.IncidentID != incidentID || r.Action != action || r.Target != target || r.Risk != risk {
			continue
		}
		switch r.Status {
		case StatusApproved:
			if r.Consumed {
				continue
			}
			if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
				r.Status = StatusExpired
				_ = g.store.Save(ctx, r)
				continue
			}
			r.Consumed = true
			r.ConsumedAt = now
			if err := g.store.Save(ctx, r); err != nil {
				return false, "approval store error"
			}
			return true, "approved by " + r.DecidedBy
		case StatusPending:
			if now.After(r.ExpiresAt) {
				r.Status = StatusExpired
				_ = g.store.Save(ctx, r)
			}
		}
	}
	return false, "human approval required"
}

// Get mengembalikan satu request.
func (g *Gate) Get(ctx context.Context, id string) (Request, bool, error) {
	return g.store.Get(ctx, id)
}

// List mengembalikan seluruh request (urut waktu dibuat).
func (g *Gate) List(ctx context.Context) ([]Request, error) {
	return g.store.List(ctx)
}

// Pending mengembalikan request yang masih menunggu keputusan.
func (g *Gate) Pending(ctx context.Context) ([]Request, error) {
	all, err := g.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Request, 0, len(all))
	for _, r := range all {
		if r.Status == StatusPending {
			out = append(out, r)
		}
	}
	return out, nil
}
