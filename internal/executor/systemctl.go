package executor

import (
	"context"
	"fmt"
	"time"
)

// SystemctlExecutor menjalankan `systemctl restart <service>`.
//
// Service harus ada di allowlist (selain juga divalidasi policy engine).
// Tidak memakai shell: argumen dibangun sebagai slice.
type SystemctlExecutor struct {
	base
	runner          CommandRunner
	allowedServices map[string]struct{}
}

// SystemctlOption konfigurasi SystemctlExecutor.
type SystemctlOption func(*SystemctlExecutor)

// WithSystemctlRunner mengganti CommandRunner (untuk pengujian).
func WithSystemctlRunner(r CommandRunner) SystemctlOption {
	return func(e *SystemctlExecutor) {
		if r != nil {
			e.runner = r
		}
	}
}

// WithSystemctlPolicy memasang policy checker.
func WithSystemctlPolicy(p Policy) SystemctlOption {
	return func(e *SystemctlExecutor) { e.base.policy = p }
}

// WithSystemctlJournal memasang journal.
func WithSystemctlJournal(j Journal) SystemctlOption {
	return func(e *SystemctlExecutor) { e.base.journal = j }
}

// WithSystemctlTimeout mengatur timeout (default 30s).
func WithSystemctlTimeout(d time.Duration) SystemctlOption {
	return func(e *SystemctlExecutor) {
		if d > 0 {
			e.base.timeout = d
		}
	}
}

// NewSystemctlExecutor membuat executor systemctl.
// allowedServices kosong berarti menolak semua (fail closed).
func NewSystemctlExecutor(allowedServices []string, opts ...SystemctlOption) *SystemctlExecutor {
	e := &SystemctlExecutor{
		base: base{
			typ:     "systemctl",
			risk:    "medium",
			timeout: 30 * time.Second,
		},
		runner:          execRunner{},
		allowedServices: make(map[string]struct{}, len(allowedServices)),
	}
	for _, s := range allowedServices {
		e.allowedServices[s] = struct{}{}
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute menjalankan systemctl restart untuk service yang diizinkan.
func (e *SystemctlExecutor) Execute(ctx context.Context, a Action) (Result, error) {
	return e.base.guarded(ctx, a, func(runCtx context.Context) (Result, error) {
		if _, ok := e.allowedServices[a.Target]; !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrServiceNotAllowed, a.Target)
		}
		out, err := e.runner.Run(runCtx, "systemctl", "restart", a.Target)
		if err != nil {
			return Result{Output: string(out)}, fmt.Errorf("systemctl restart %s: %w", a.Target, err)
		}
		return Result{Output: string(out)}, nil
	})
}
