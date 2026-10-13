package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// AnsibleExecutor menjalankan `ansible-playbook <runbook> -l <target>`.
//
// Runbook harus ada di allowlist (path dibersihkan dan dicocokkan). Tidak
// memakai shell: argumen dibangun sebagai slice, sehingga `;`, `|`, dan
// substitusi perintah tidak dieksekusi.
type AnsibleExecutor struct {
	base
	runner        CommandRunner
	allowedRunbks map[string]struct{}
}

// AnsibleOption konfigurasi AnsibleExecutor.
type AnsibleOption func(*AnsibleExecutor)

// WithAnsibleRunner mengganti CommandRunner (untuk pengujian).
func WithAnsibleRunner(r CommandRunner) AnsibleOption {
	return func(e *AnsibleExecutor) {
		if r != nil {
			e.runner = r
		}
	}
}

// WithAnsiblePolicy memasang policy checker.
func WithAnsiblePolicy(p Policy) AnsibleOption {
	return func(e *AnsibleExecutor) { e.base.policy = p }
}

// WithAnsibleJournal memasang journal.
func WithAnsibleJournal(j Journal) AnsibleOption {
	return func(e *AnsibleExecutor) { e.base.journal = j }
}

// WithAnsibleTimeout mengatur timeout.
func WithAnsibleTimeout(d time.Duration) AnsibleOption {
	return func(e *AnsibleExecutor) {
		if d > 0 {
			e.base.timeout = d
		}
	}
}

// NewAnsibleExecutor membuat executor ansible. allowedRunbooks adalah allowlist
// path runbook (harus persis setelah dibersihkan).
func NewAnsibleExecutor(allowedRunbooks []string, opts ...AnsibleOption) *AnsibleExecutor {
	e := &AnsibleExecutor{
		base: base{
			typ:     "ansible",
			risk:    "high",
			timeout: 120 * time.Second,
		},
		runner:        execRunner{},
		allowedRunbks: make(map[string]struct{}),
	}
	for _, rb := range allowedRunbooks {
		e.allowedRunbks[filepath.Clean(rb)] = struct{}{}
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute menjalankan ansible-playbook.
func (e *AnsibleExecutor) Execute(ctx context.Context, a Action) (Result, error) {
	return e.base.guarded(ctx, a, func(runCtx context.Context) (Result, error) {
		runbook := filepath.Clean(a.Target)
		if _, ok := e.allowedRunbks[runbook]; !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrRunbookNotAllowed, runbook)
		}

		args := []string{runbook, "-l", a.Target}
		if a.Params != nil && a.Params["extra_vars"] != "" {
			args = append(args, "--extra-vars", a.Params["extra_vars"])
		}

		out, err := e.runner.Run(runCtx, "ansible-playbook", args...)
		if err != nil {
			return Result{Output: string(out)}, fmt.Errorf("ansible-playbook %s: %w", runbook, err)
		}
		return Result{Output: string(out)}, nil
	})
}
