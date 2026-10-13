package executor_test

import (
	"context"
	"sync"

	"github.com/seronz/fault-pilot/internal/audit"
	"github.com/seronz/fault-pilot/internal/domain"
)

// fakePolicy adalah policy checker yang dapat diatur untuk pengujian.
type fakePolicy struct {
	mu     sync.Mutex
	allow  bool
	reason string
	calls  int
}

func (p *fakePolicy) Validate(_ context.Context, _ domain.Proposal) (bool, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.allow {
		return true, "ok"
	}
	if p.reason == "" {
		return false, "denied"
	}
	return false, p.reason
}

func (p *fakePolicy) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// fakeJournal mencatat siklus hidup aksi.
type fakeJournal struct {
	mu        sync.Mutex
	started   int
	succeeded int
	failed    int
}

func (j *fakeJournal) Start(_ context.Context, _ audit.Action) (domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.started++
	return domain.JournalEntry{ActionID: "act-1"}, nil
}

func (j *fakeJournal) Succeed(_ context.Context, _, _ string) (domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.succeeded++
	return domain.JournalEntry{}, nil
}

func (j *fakeJournal) Fail(_ context.Context, _, _ string) (domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.failed++
	return domain.JournalEntry{}, nil
}

// fakeRunner merekam pemanggilan perintah tanpa menjalankannya.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	out   []byte
	err   error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.out, r.err
}

func (r *fakeRunner) lastCall() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return nil
	}
	return r.calls[len(r.calls)-1]
}

// fakeEC2 adalah EC2API palsu.
type fakeEC2 struct {
	ids []string
	err error
}

func (f *fakeEC2) RebootInstances(_ context.Context, ids []string) error {
	f.ids = append(f.ids, ids...)
	return f.err
}
