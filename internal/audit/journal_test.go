package audit_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seronz/fault-pilot/internal/audit"
	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/store"
)

// pastikan store.Store memenuhi audit.JournalStore
var _ audit.JournalStore = (*store.Store)(nil)

// memJournalStore adalah JournalStore in-memory untuk pengujian.
type memJournalStore struct {
	entries  map[string]domain.JournalEntry
	failSave bool
}

func newMemJournalStore() *memJournalStore {
	return &memJournalStore{entries: map[string]domain.JournalEntry{}}
}

func (m *memJournalStore) SaveJournalEntry(ctx context.Context, e domain.JournalEntry) error {
	if m.failSave {
		return errors.New("db down")
	}
	m.entries[e.ActionID] = e
	return nil
}

func (m *memJournalStore) JournalEntry(ctx context.Context, actionID string) (domain.JournalEntry, bool, error) {
	e, ok := m.entries[actionID]
	return e, ok, nil
}

func (m *memJournalStore) JournalByKey(ctx context.Context, key string) (domain.JournalEntry, bool, error) {
	for _, e := range m.entries {
		if e.IdempotencyKey == key {
			return e, true, nil
		}
	}
	return domain.JournalEntry{}, false, nil
}

func (m *memJournalStore) JournalByState(ctx context.Context, states ...domain.JournalState) ([]domain.JournalEntry, error) {
	set := make(map[domain.JournalState]bool, len(states))
	for _, s := range states {
		set[s] = true
	}
	var out []domain.JournalEntry
	for _, e := range m.entries {
		if set[e.State] {
			out = append(out, e)
		}
	}
	return out, nil
}

type failingSink struct{ err error }

func (f failingSink) Append(ctx context.Context, e domain.JournalEntry) error { return f.err }

func testAction() audit.Action {
	return audit.Action{
		IncidentID:     "inc-1",
		Action:         "restart_simulated_service",
		Target:         "checkout",
		IdempotencyKey: "key-1",
	}
}

// T10: proses mati setelah exec mulai -> state dipulihkan, tidak eksekusi ganda.
func TestT10_RecoverAfterCrashNoDoubleExec(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")

	// Instance pertama: mulai aksi lalu "mati" (tanpa Succeed/Fail).
	st1, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}
	j1 := audit.NewJournal(st1, nil)
	entry, err := j1.Start(ctx, testAction())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if entry.State != domain.JournalExecuting {
		t.Fatalf("state awal harus executing, dapat %s", entry.State)
	}
	st1.Close() // simulasi proses mati

	// Restart: buka DB yang sama, lalu reconcile.
	st2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	defer st2.Close()

	j2 := audit.NewJournal(st2, nil)
	recovered, err := j2.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("harus memulihkan 1 entri, dapat %d", len(recovered))
	}
	if recovered[0].State != domain.JournalUnknown {
		t.Fatalf("state hasil pemulihan harus unknown, dapat %s", recovered[0].State)
	}

	executing, err := st2.JournalByState(ctx, domain.JournalExecuting)
	if err != nil {
		t.Fatalf("by state: %v", err)
	}
	if len(executing) != 0 {
		t.Fatalf("tidak boleh ada entri executing, dapat %d", len(executing))
	}

	// Percobaan eksekusi ulang: harus ditolak, fungsi exec tidak dijalankan.
	executed := 0
	if _, err := j2.Start(ctx, testAction()); err == nil {
		executed++
	} else if !errors.Is(err, audit.ErrAlreadyExists) {
		t.Fatalf("harus ErrAlreadyExists, dapat %v", err)
	}
	if executed != 0 {
		t.Fatal("aksi tidak boleh dieksekusi dua kali setelah restart")
	}
}

// T11: kegagalan tulis audit -> journal ditahan, aksi baru ditolak.
func TestT11_AuditFailureHoldsNewActions(t *testing.T) {
	ctx := context.Background()
	j := audit.NewJournal(newMemJournalStore(), failingSink{err: errors.New("audit disk full")})

	if _, err := j.Start(ctx, testAction()); err == nil {
		t.Fatal("Start harus error saat audit gagal")
	}
	if !j.Held() {
		t.Fatal("journal harus ditahan setelah kegagalan tulis")
	}

	if _, err := j.Start(ctx, audit.Action{IdempotencyKey: "key-2"}); !errors.Is(err, audit.ErrHeld) {
		t.Fatalf("aksi baru harus ditahan (ErrHeld), dapat %v", err)
	}
}

// T11 (varian): kegagalan tulis DB juga menahan aksi baru.
func TestT11_DBFailureHoldsNewActions(t *testing.T) {
	ctx := context.Background()
	fake := newMemJournalStore()
	fake.failSave = true
	j := audit.NewJournal(fake, nil)

	if _, err := j.Start(ctx, testAction()); err == nil {
		t.Fatal("Start harus error saat DB gagal")
	}
	if !j.Held() {
		t.Fatal("journal harus ditahan setelah kegagalan DB")
	}
	if _, err := j.Start(ctx, audit.Action{IdempotencyKey: "key-2"}); !errors.Is(err, audit.ErrHeld) {
		t.Fatalf("aksi baru harus ditahan (ErrHeld), dapat %v", err)
	}
}

// Alur sukses: pending -> executing -> succeeded, audit JSONL bertambah.
func TestJournalHappyPathAndJSONL(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	sink := audit.NewJSONLWriter(&buf)

	j := audit.NewJournal(newMemJournalStore(), sink)
	entry, err := j.Start(ctx, testAction())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if entry.ActionID == "" {
		t.Fatal("ActionID (UUID) tidak boleh kosong")
	}
	if entry.State != domain.JournalExecuting {
		t.Fatalf("state harus executing, dapat %s", entry.State)
	}

	done, err := j.Succeed(ctx, entry.ActionID, "selesai")
	if err != nil {
		t.Fatalf("succeed: %v", err)
	}
	if done.State != domain.JournalSucceeded {
		t.Fatalf("state harus succeeded, dapat %s", done.State)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("audit JSONL harus 3 baris (pending, executing, succeeded), dapat %d", len(lines))
	}
}

// IdempotencyKey yang sama tidak boleh membuat dua entri.
func TestStartIsIdempotent(t *testing.T) {
	ctx := context.Background()
	j := audit.NewJournal(newMemJournalStore(), nil)

	if _, err := j.Start(ctx, testAction()); err != nil {
		t.Fatalf("start 1: %v", err)
	}
	if _, err := j.Start(ctx, testAction()); !errors.Is(err, audit.ErrAlreadyExists) {
		t.Fatalf("start 2 harus ErrAlreadyExists, dapat %v", err)
	}
}

// Reconcile pada DB bersih tidak memulihkan apa pun.
func TestReconcileClean(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "clean.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	j := audit.NewJournal(st, nil)
	recovered, err := j.Reconcile(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("harus 0 entri, dapat %d", len(recovered))
	}
}
