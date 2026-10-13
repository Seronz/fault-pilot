package executor

import (
	"context"
	"fmt"
	"time"
)

// KubectlExecutor menjalankan `kubectl rollout restart deployment/<name> -n <ns>`.
//
// Namespace diambil dari Params["namespace"] (default "default"). Mendukung
// dry-run lewat Params["dry_run"]="true" (menambahkan `--dry-run=server`).
// Memakai kubeconfig default dari environment (argv bukan shell).
type KubectlExecutor struct {
	base
	runner      CommandRunner
	allowedDeps map[string]struct{}
}

// KubectlOption konfigurasi KubectlExecutor.
type KubectlOption func(*KubectlExecutor)

// WithKubectlRunner mengganti CommandRunner (untuk pengujian).
func WithKubectlRunner(r CommandRunner) KubectlOption {
	return func(e *KubectlExecutor) {
		if r != nil {
			e.runner = r
		}
	}
}

// WithKubectlPolicy memasang policy checker.
func WithKubectlPolicy(p Policy) KubectlOption {
	return func(e *KubectlExecutor) { e.base.policy = p }
}

// WithKubectlJournal memasang journal.
func WithKubectlJournal(j Journal) KubectlOption {
	return func(e *KubectlExecutor) { e.base.journal = j }
}

// WithKubectlTimeout mengatur timeout.
func WithKubectlTimeout(d time.Duration) KubectlOption {
	return func(e *KubectlExecutor) {
		if d > 0 {
			e.base.timeout = d
		}
	}
}

// NewKubectlExecutor membuat executor kubectl. allowedDeployments adalah
// allowlist nama deployment (tanpa prefix "deployment/").
func NewKubectlExecutor(allowedDeployments []string, opts ...KubectlOption) *KubectlExecutor {
	e := &KubectlExecutor{
		base: base{
			typ:     "kubectl",
			risk:    "medium",
			timeout: 30 * time.Second,
		},
		runner:      execRunner{},
		allowedDeps: make(map[string]struct{}),
	}
	for _, d := range allowedDeployments {
		e.allowedDeps[d] = struct{}{}
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute menjalankan kubectl rollout restart.
func (e *KubectlExecutor) Execute(ctx context.Context, a Action) (Result, error) {
	return e.base.guarded(ctx, a, func(runCtx context.Context) (Result, error) {
		if _, ok := e.allowedDeps[a.Target]; !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrServiceNotAllowed, a.Target)
		}

		ns := "default"
		if a.Params != nil && a.Params["namespace"] != "" {
			ns = a.Params["namespace"]
		}

		args := []string{"rollout", "restart", "deployment/" + a.Target, "-n", ns}
		dryRun := a.Params != nil && a.Params["dry_run"] == "true"
		if dryRun {
			args = append(args, "--dry-run=server")
		}

		out, err := e.runner.Run(runCtx, "kubectl", args...)
		if err != nil {
			return Result{Output: string(out), DryRun: dryRun}, fmt.Errorf("kubectl rollout restart %s: %w", a.Target, err)
		}
		return Result{Output: string(out), DryRun: dryRun}, nil
	})
}
