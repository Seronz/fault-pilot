package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ControlPayload sesuai dengan API Fault Injector.
type ControlPayload struct {
	Mode      string  `json:"mode,omitempty"`
	LatencyMS int     `json:"latency_ms,omitempty"`
	ErrorRate float64 `json:"error_rate,omitempty"`
}

// LabExecutor mengeksekusi aksi dengan mengirim POST /control ke Fault Injector
// (dummy lab). Ini executor default dan TIDAK BOLEH dihapus: dipakai untuk
// pengujian lokal tanpa menyentuh host/cluster.
type LabExecutor struct {
	base
	client *http.Client
	fiURL  string
	deps   map[string]chan struct{}
	mu     sync.Mutex
}

// LabOption konfigurasi LabExecutor.
type LabOption func(*LabExecutor)

// WithHTTPClient mengganti http client.
func WithHTTPClient(c *http.Client) LabOption {
	return func(e *LabExecutor) {
		if c != nil {
			e.client = c
		}
	}
}

// WithFaultInjectorURL mengatur endpoint /control.
func WithFaultInjectorURL(url string) LabOption { return func(e *LabExecutor) { e.fiURL = url } }

// WithDeadline mengatur timeout default eksekusi.
func WithDeadline(d time.Duration) LabOption {
	return func(e *LabExecutor) {
		if d > 0 {
			e.base.timeout = d
		}
	}
}

// WithMaxConcurrencyPerDependency membatasi konkurensi per dependency.
func WithMaxConcurrencyPerDependency(dep string, max int) LabOption {
	return func(e *LabExecutor) {
		if max <= 0 {
			max = 1
		}
		ch := make(chan struct{}, max)
		e.mu.Lock()
		e.deps[dep] = ch
		e.mu.Unlock()
	}
}

// WithLabPolicy memasang policy checker.
func WithLabPolicy(p Policy) LabOption { return func(e *LabExecutor) { e.base.policy = p } }

// WithLabJournal memasang journal.
func WithLabJournal(j Journal) LabOption { return func(e *LabExecutor) { e.base.journal = j } }

// New membuat LabExecutor baru (nama lama dipertahankan untuk kompatibilitas).
func New(opts ...LabOption) *LabExecutor {
	e := &LabExecutor{
		base: base{
			typ:     "lab",
			risk:    "low",
			timeout: 5 * time.Second,
		},
		client: &http.Client{Timeout: 10 * time.Second},
		fiURL:  "http://localhost:8081/control",
		deps:   make(map[string]chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NewLabExecutor alias New.
func NewLabExecutor(opts ...LabOption) *LabExecutor { return New(opts...) }

func (e *LabExecutor) acquire(ctx context.Context, dep string) (func(), error) {
	e.mu.Lock()
	ch, ok := e.deps[dep]
	if !ok {
		ch = make(chan struct{}, 1)
		e.deps[dep] = ch
	}
	e.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ErrDependencyBusy
	}
}

// Execute mengirim POST /control ke Fault Injector. Tidak pernah menyentuh host.
func (e *LabExecutor) Execute(ctx context.Context, a Action) (Result, error) {
	return e.base.guarded(ctx, a, func(runCtx context.Context) (Result, error) {
		release, err := e.acquire(runCtx, a.Target)
		if err != nil {
			return Result{}, err
		}
		defer release()

		payload := ControlPayload{Mode: a.Type}
		body, err := json.Marshal(payload)
		if err != nil {
			return Result{}, err
		}

		req, err := http.NewRequestWithContext(runCtx, http.MethodPost, e.fiURL, bytes.NewReader(body))
		if err != nil {
			return Result{}, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := e.client.Do(req)
		if err != nil {
			return Result{}, ErrExecutionFailed
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return Result{}, ErrExecutionFailed
		}
		return Result{Output: "control accepted"}, nil
	})
}

// ErrDependencyBusy dikembalikan bila dependency sedang dieksekusi aksi lain.
var ErrDependencyBusy = errors.New("executor: dependency busy")

// ErrExecutionFailed dikembalikan bila eksekusi gagal.
var ErrExecutionFailed = errors.New("executor: execution failed")
