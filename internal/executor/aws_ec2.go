package executor

import (
	"context"
	"strings"
	"time"
)

// EC2API adalah bagian kecil dari AWS EC2 API yang dibutuhkan.
//
// Dengan interface ini executor dapat diuji tanpa AWS/kredensial nyata. Di
// produksi, adopsi klien aws-sdk-go-v2 yang menyediakan method RebootInstances.
type EC2API interface {
	RebootInstances(ctx context.Context, instanceIDs []string) error
}

// EC2Executor me-reboot instance EC2. Risiko tinggi (high).
type EC2Executor struct {
	base
	api       EC2API
	allowedID map[string]struct{}
}

// EC2Option konfigurasi EC2Executor.
type EC2Option func(*EC2Executor)

// WithEC2Policy memasang policy checker.
func WithEC2Policy(p Policy) EC2Option { return func(e *EC2Executor) { e.base.policy = p } }

// WithEC2Journal memasang journal.
func WithEC2Journal(j Journal) EC2Option { return func(e *EC2Executor) { e.base.journal = j } }

// WithEC2Timeout mengatur timeout.
func WithEC2Timeout(d time.Duration) EC2Option {
	return func(e *EC2Executor) {
		if d > 0 {
			e.base.timeout = d
		}
	}
}

// NewEC2Executor membuat executor EC2 dengan EC2API (boleh nil, akan gagal
// fail-closed saat Execute). allowedInstances adalah allowlist instance ID.
func NewEC2Executor(api EC2API, allowedInstances []string, opts ...EC2Option) *EC2Executor {
	e := &EC2Executor{
		base: base{
			typ:     "aws_ec2",
			risk:    "high",
			timeout: 60 * time.Second,
		},
		api:       api,
		allowedID: make(map[string]struct{}),
	}
	for _, id := range allowedInstances {
		e.allowedID[id] = struct{}{}
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute me-reboot instance pada allowlist.
func (e *EC2Executor) Execute(ctx context.Context, a Action) (Result, error) {
	return e.base.guarded(ctx, a, func(runCtx context.Context) (Result, error) {
		if e.api == nil {
			return Result{}, ErrNoAPI
		}

		ids := targetIDs(a)
		if len(ids) == 0 {
			return Result{}, ErrInvalidAction
		}
		for _, id := range ids {
			if _, ok := e.allowedID[id]; !ok {
				return Result{}, ErrServiceNotAllowed
			}
		}

		if err := e.api.RebootInstances(runCtx, ids); err != nil {
			return Result{}, err
		}
		return Result{Output: "reboot requested: " + strings.Join(ids, ",")}, nil
	})
}

// targetIDs menggabungkan Target dan Params["instance_ids"].
func targetIDs(a Action) []string {
	var ids []string
	if a.Target != "" {
		ids = append(ids, a.Target)
	}
	if a.Params != nil {
		for _, id := range strings.Split(a.Params["instance_ids"], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
