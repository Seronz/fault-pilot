// Package executor mengeksekusi aksi remediation.
//
// Ada beberapa jenis executor dengan tingkat risiko berbeda:
//   - lab      : hanya HTTP POST /control ke Fault Injector (default, dummy tetap ada)
//   - systemctl: `systemctl restart <service>` (host lokal)
//   - kubectl  : `kubectl rollout restart deployment/<name> -n <ns>`
//   - aws_ec2  : reboot instance lewat EC2API (mockable)
//   - ansible  : `ansible-playbook <runbook> -l <target>`
//
// Semua executor WAJIB: cek policy (allowlist service/target) dulu, catat ke
// journal SEBELUM dan SESUDAH eksekusi, fail closed, timeout, dan tidak pernah
// memakai shell (`bash -c`). Argumen dibangun sebagai slice dan dijalankan
// lewat exec.CommandContext agar tidak bisa disuntik shell.
package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/seronz/fault-pilot/internal/audit"
	"github.com/seronz/fault-pilot/internal/domain"
)

var (
	// ErrDenied dikembalikan bila policy menolak aksi.
	ErrDenied = errors.New("executor: action denied by policy")
	// ErrInvalidAction dikembalikan bila aksi tidak lengkap/tidak valid.
	ErrInvalidAction = errors.New("executor: invalid action")
	// ErrRunbookNotAllowed dikembalikan bila runbook di luar allowlist.
	ErrRunbookNotAllowed = errors.New("executor: runbook not allowed")
	// ErrServiceNotAllowed dikembalikan bila service di luar allowlist.
	ErrServiceNotAllowed = errors.New("executor: service not allowed")
	// ErrNoAPI dikembalikan bila EC2API belum di-set.
	ErrNoAPI = errors.New("executor: no EC2 API configured")
)

// Action adalah aksi remediation yang akan dieksekusi.
type Action struct {
	IncidentID  string            `json:"incident_id"`
	EvidenceIDs []string          `json:"evidence_ids,omitempty"`
	Type        string            `json:"type"`   // mis. "restart_service"
	Target      string            `json:"target"` // mis. nama service/instance
	Params      map[string]string `json:"params,omitempty"`
}

// Result adalah hasil eksekusi yang berhasil.
type Result struct {
	Executor string        `json:"executor"`
	Output   string        `json:"output,omitempty"`
	DryRun   bool          `json:"dry_run,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Executor menjalankan satu aksi remediation.
type Executor interface {
	Type() string      // "lab" | "systemctl" | "kubectl" | "aws_ec2" | "ansible"
	RiskLevel() string // "low" | "medium" | "high"
	Execute(ctx context.Context, a Action) (Result, error)
}

// Policy memvalidasi aksi sebelum eksekusi (fail closed). Implementasi
// biasanya adalah *policy.Engine.
type Policy interface {
	Validate(ctx context.Context, p domain.Proposal) (bool, string)
}

// Journal mencatat siklus hidup aksi. Implementasi biasanya *audit.Journal.
type Journal interface {
	Start(ctx context.Context, a audit.Action) (domain.JournalEntry, error)
	Succeed(ctx context.Context, actionID, msg string) (domain.JournalEntry, error)
	Fail(ctx context.Context, actionID, msg string) (domain.JournalEntry, error)
}

// base adalah bagian bersama semua executor: metadata, policy, journal, timeout.
type base struct {
	typ     string
	risk    string
	timeout time.Duration
	policy  Policy
	journal Journal
}

func (b *base) Type() string      { return b.typ }
func (b *base) RiskLevel() string { return b.risk }

// guarded menjalankan alur aman: policy -> journal (before) -> fn -> journal (after).
//
// Bila policy menolak, aksi tidak dijalankan. Bila journal gagal dicatat,
// aksi tidak dijalankan (fail closed).
func (b *base) guarded(ctx context.Context, a Action, fn func(ctx context.Context) (Result, error)) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.Type == "" || a.Target == "" {
		return Result{}, ErrInvalidAction
	}

	if b.policy != nil {
		ok, reason := b.policy.Validate(ctx, domain.Proposal{
			IncidentID:  a.IncidentID,
			EvidenceIDs: a.EvidenceIDs,
			Action:      a.Type,
			Target:      a.Target,
			Reason:      a.Params["reason"],
		})
		if !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrDenied, reason)
		}
	}

	// Catat SEBELUM eksekusi.
	var entry domain.JournalEntry
	if b.journal != nil {
		e, err := b.journal.Start(ctx, audit.Action{
			IncidentID: a.IncidentID,
			Action:     a.Type,
			Target:     a.Target,
		})
		if err != nil {
			return Result{}, err
		}
		entry = e
	}

	runCtx := ctx
	if b.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}

	start := time.Now()
	res, err := fn(runCtx)
	res.Executor = b.typ
	res.Duration = time.Since(start)

	// Catat SESUDAH eksekusi.
	if b.journal != nil {
		if err != nil {
			if _, jerr := b.journal.Fail(ctx, entry.ActionID, err.Error()); jerr != nil {
				return res, jerr
			}
		} else {
			if _, jerr := b.journal.Succeed(ctx, entry.ActionID, "ok"); jerr != nil {
				return res, jerr
			}
		}
	}
	return res, err
}
