package collector

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/domain"
)

// TestSubmitRejectedMarksFailed memastikan event yang gagal masuk worker pool
// (Submit mengembalikan false) ditandai failed, bukan dibiarkan seolah selesai.
func TestSubmitRejectedMarksFailed(t *testing.T) {
	block := make(chan struct{})
	proc := func(ctx context.Context, e domain.Event) error {
		<-block // tahan slot sampai test selesai mengecek
		return nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	c := New(2, 1, proc, logger)
	defer func() {
		close(block)
		_ = c.Close()
	}()

	a := domain.Event{Type: "alert", Service: "svc", Message: "a"}
	b := domain.Event{Type: "alert", Service: "svc", Message: "b"}

	if !c.Push(a) {
		t.Fatal("push a should be accepted")
	}
	waitForInternal(t, func() bool {
		st, ok := c.Status(a)
		return ok && st == EventRunning
	})

	if !c.Push(b) {
		t.Fatal("push b should be accepted")
	}
	// Tunggu sampai consumer mengambil b dari kanal (kanal kosong), lalu
	// consumer akan memblokir di Submit karena slot ditahan oleh a.
	waitForInternal(t, func() bool { return len(c.events) == 0 })

	// Batalkan context agar Submit untuk b ditolak.
	c.cancel()

	waitForInternal(t, func() bool {
		st, ok := c.Status(b)
		return ok && st == EventFailed
	})

	if got := c.Failed(); got != 1 {
		t.Fatalf("want 1 failed event, got %d", got)
	}
	if got := c.Completed(); got != 0 {
		t.Fatalf("want 0 completed event, got %d", got)
	}
}

func waitForInternal(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
