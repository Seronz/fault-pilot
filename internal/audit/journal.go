// Package audit menyediakan ActionJournal (jurnal aksi yang persisten) dan
// audit trail JSONL.
//
// Alur: setiap aksi ditulis ke SQLite SEBELUM dieksekusi (state pending lalu
// executing) dan sesudahnya (succeeded/failed/unknown). IdempotencyKey
// mencegah aksi yang sama dieksekusi dua kali. Saat startup, Reconcile
// memulihkan entri yang masih executing/pending menjadi unknown.
//
// Fail closed: bila ada kegagalan tulis (DB atau audit), journal "ditahan"
// (hold) sehingga aksi baru tidak diizinkan sampai diperbaiki.
package audit

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/seronz/fault-pilot/internal/domain"
)

var (
	// ErrAlreadyExists dikembalikan bila idempotency key sudah pernah dicatat.
	ErrAlreadyExists = errors.New("action already journaled")
	// ErrHeld dikembalikan bila journal sedang menahan aksi baru.
	ErrHeld = errors.New("journal held: new actions blocked")
	// ErrNotFound dikembalikan bila entri jurnal tidak ditemukan.
	ErrNotFound = errors.New("journal entry not found")
)

// JournalStore adalah persistensi yang dibutuhkan ActionJournal.
type JournalStore interface {
	SaveJournalEntry(ctx context.Context, e domain.JournalEntry) error
	JournalEntry(ctx context.Context, actionID string) (domain.JournalEntry, bool, error)
	JournalByKey(ctx context.Context, key string) (domain.JournalEntry, bool, error)
	JournalByState(ctx context.Context, states ...domain.JournalState) ([]domain.JournalEntry, error)
}

// AuditSink menulis audit trail (mis. JSONL).
type AuditSink interface {
	Append(ctx context.Context, e domain.JournalEntry) error
}

// Action adalah aksi yang akan dijurnalkan.
type Action struct {
	IncidentID     string
	Action         string
	Target         string
	IdempotencyKey string // bila kosong, dibangkitkan otomatis
}

// Journal adalah ActionJournal yang mencatat siklus hidup aksi.
type Journal struct {
	store JournalStore
	audit AuditSink
	now   func() time.Time
	genID func() string

	mu   sync.Mutex
	held bool // true bila terjadi kegagalan tulis -> tolak aksi baru
}

// NewJournal membuat ActionJournal. audit boleh nil (audit trail dilewati).
func NewJournal(store JournalStore, audit AuditSink) *Journal {
	return &Journal{
		store: store,
		audit: audit,
		now:   time.Now,
		genID: func() string { return uuid.NewString() },
	}
}

// Held melaporkan apakah journal sedang menahan aksi baru.
func (j *Journal) Held() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.held
}

// Start mencatat aksi baru (pending lalu executing) SEBELUM eksekusi.
//
// Bila idempotency key sudah ada, aksi dianggap sudah pernah dicatat dan
// fungsi mengembalikan ErrAlreadyExists (pemanggil tidak boleh mengeksekusi).
func (j *Journal) Start(ctx context.Context, a Action) (domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.held {
		return domain.JournalEntry{}, ErrHeld
	}

	key := a.IdempotencyKey
	if key == "" {
		key = j.genID()
	}

	existing, ok, err := j.store.JournalByKey(ctx, key)
	if err != nil {
		return domain.JournalEntry{}, j.fail(err)
	}
	if ok {
		return existing, ErrAlreadyExists
	}

	entry := domain.JournalEntry{
		ActionID:       j.genID(),
		IdempotencyKey: key,
		IncidentID:     a.IncidentID,
		Action:         a.Action,
		Target:         a.Target,
		State:          domain.JournalPending,
		UpdatedAt:      j.now(),
	}
	if err := j.persist(ctx, entry); err != nil {
		return domain.JournalEntry{}, err
	}

	entry.State = domain.JournalExecuting
	entry.UpdatedAt = j.now()
	if err := j.persist(ctx, entry); err != nil {
		return domain.JournalEntry{}, err
	}
	return entry, nil
}

// Succeed menandai aksi berhasil.
func (j *Journal) Succeed(ctx context.Context, actionID, msg string) (domain.JournalEntry, error) {
	return j.transition(ctx, actionID, domain.JournalSucceeded, msg)
}

// Fail menandai aksi gagal.
func (j *Journal) Fail(ctx context.Context, actionID, msg string) (domain.JournalEntry, error) {
	return j.transition(ctx, actionID, domain.JournalFailed, msg)
}

// Unknown menandai hasil aksi tidak diketahui.
func (j *Journal) Unknown(ctx context.Context, actionID, msg string) (domain.JournalEntry, error) {
	return j.transition(ctx, actionID, domain.JournalUnknown, msg)
}

// transition mengganti state entri lalu menyimpannya.
func (j *Journal) transition(ctx context.Context, actionID string, st domain.JournalState, msg string) (domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.held {
		return domain.JournalEntry{}, ErrHeld
	}

	entry, ok, err := j.store.JournalEntry(ctx, actionID)
	if err != nil {
		return domain.JournalEntry{}, j.fail(err)
	}
	if !ok {
		return domain.JournalEntry{}, ErrNotFound
	}

	entry.State = st
	entry.Message = msg
	entry.UpdatedAt = j.now()
	if err := j.persist(ctx, entry); err != nil {
		return domain.JournalEntry{}, err
	}
	return entry, nil
}

// Reconcile dipanggil saat startup: entri yang masih pending/executing
// (proses sebelumnya mati) dipulihkan menjadi unknown.
func (j *Journal) Reconcile(ctx context.Context) ([]domain.JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	entries, err := j.store.JournalByState(ctx, domain.JournalPending, domain.JournalExecuting)
	if err != nil {
		return nil, j.fail(err)
	}

	var recovered []domain.JournalEntry
	for _, e := range entries {
		e.State = domain.JournalUnknown
		e.Message = "recovered after restart"
		e.UpdatedAt = j.now()
		if err := j.persist(ctx, e); err != nil {
			return recovered, err
		}
		recovered = append(recovered, e)
	}
	return recovered, nil
}

// persist menulis entry ke SQLite lalu ke audit trail. Kegagalan apa pun
// membuat journal ditahan (fail closed).
func (j *Journal) persist(ctx context.Context, e domain.JournalEntry) error {
	if err := j.store.SaveJournalEntry(ctx, e); err != nil {
		return j.fail(err)
	}
	if j.audit != nil {
		if err := j.audit.Append(ctx, e); err != nil {
			return j.fail(err)
		}
	}
	return nil
}

// fail menandai journal ditahan dan mengembalikan error asli.
func (j *Journal) fail(err error) error {
	j.held = true
	return err
}
