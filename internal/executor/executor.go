// Package executor mengeksekusi proposal remediation.
//
// Executor hanya berinteraksi dengan Fault Injector (HTTP POST /control).
// Tidak pernah mengakses host/container langsung (fail closed, tidak menyentuh host).
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

var (
	ErrDependencyBusy  = errors.New("executor: dependency busy")
	ErrExecutionFailed = errors.New("executor: execution failed")
)

// ControlPayload sesuai dengan API Fault Injector.
type ControlPayload struct {
	Mode      string  `json:"mode,omitempty"`
	LatencyMS int     `json:"latency_ms,omitempty"`
	ErrorRate float64 `json:"error_rate,omitempty"`
}

// Executor menjalankan aksi terhadap target.
type Executor struct {
	client   *http.Client
	fiURL    string // URL Fault Injector, misal http://localhost:8081/control
	deps     map[string]chan struct{}
	mu       sync.Mutex
	deadline time.Duration
}

// Option konfigurasi executor.
type Option func(*Executor)

// WithHTTPClient mengganti http client.
func WithHTTPClient(c *http.Client) Option {
	return func(e *Executor) {
		if c != nil {
			e.client = c
		}
	}
}

// WithFaultInjectorURL mengatur endpoint /control.
func WithFaultInjectorURL(url string) Option { return func(e *Executor) { e.fiURL = url } }

// WithDeadline mengatur timeout default eksekusi.
func WithDeadline(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.deadline = d
		}
	}
}

// WithMaxConcurrencyPerDependency membatasi konkurensi per dependency.
func WithMaxConcurrencyPerDependency(dep string, max int) Option {
	return func(e *Executor) {
		if max <= 0 {
			max = 1
		}
		ch := make(chan struct{}, max)
		e.mu.Lock()
		e.deps[dep] = ch
		e.mu.Unlock()
	}
}

// New membuat Executor baru.
func New(opts ...Option) *Executor {
	e := &Executor{
		client: &http.Client{Timeout: 10 * time.Second},
		fiURL:  "http://localhost:8081/control",
		deps:   make(map[string]chan struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.deadline <= 0 {
		e.deadline = 5 * time.Second
	}
	return e
}

func (e *Executor) acquire(ctx context.Context, dep string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

// Execute menjalankan aksi. Hanya mengirim POST ke Fault Injector.
// Tidak pernah menyentuh host.
func (e *Executor) Execute(ctx context.Context, action, target, incidentID string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	execCtx, cancel := context.WithTimeout(ctx, e.deadline)
	defer cancel()

	release, err := e.acquire(execCtx, target)
	if err != nil {
		return err
	}
	defer release()

	payload := ControlPayload{Mode: action}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(execCtx, http.MethodPost, e.fiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrExecutionFailed
		}
		return ErrExecutionFailed
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrExecutionFailed
	}
	return nil
}
