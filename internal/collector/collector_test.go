package collector_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/collector"
	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// T05: 100 identical events must collapse to a single incident.
func TestT05_HundredSameEventsYieldOneIncident(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t05.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	proc := func(ctx context.Context, e domain.Event) error {
		return st.CreateIncident(ctx, domain.Incident{
			ID:          "inc-" + e.ID,
			Status:      "open",
			EvidenceIDs: []string{e.ID},
		})
	}

	c := collector.New(collector.DefaultBufferSize, collector.DefaultMaxConcurrent, proc, discardLogger())
	defer c.Close()

	same := domain.Event{Type: "alert", Service: "checkout", Message: "high error rate"}
	for i := 0; i < 100; i++ {
		c.Push(same)
	}

	waitFor(t, func() bool {
		n, err := st.CountIncidents(ctx)
		return err == nil && n >= 1
	})

	n, err := st.CountIncidents(ctx)
	if err != nil {
		t.Fatalf("count incidents: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly 1 incident from 100 identical events, got %d", n)
	}
	if got := c.Accepted(); got != 1 {
		t.Fatalf("want 1 accepted event, got %d", got)
	}
}

// T06: 1000 concurrent pushes stay bounded; Push never blocks.
func TestT06_ThousandConcurrentPushesBounded(t *testing.T) {
	release := make(chan struct{})
	proc := func(ctx context.Context, e domain.Event) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}

	c := collector.New(collector.DefaultBufferSize, collector.DefaultMaxConcurrent, proc, discardLogger())

	const n = 1000
	var accepted int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := domain.Event{Type: "alert", Service: "svc", Message: fmt.Sprintf("m-%d", i)}
			if c.Push(e) {
				atomic.AddInt64(&accepted, 1)
			}
		}(i)
	}
	wg.Wait()

	bound := int64(collector.DefaultBufferSize + collector.DefaultMaxConcurrent + 1)
	if accepted > bound {
		t.Fatalf("accepted %d exceeds bound %d", accepted, bound)
	}
	if accepted == 0 {
		t.Fatalf("expected at least one accepted event")
	}
	if c.Len() > c.Cap() {
		t.Fatalf("channel len %d exceeds cap %d", c.Len(), c.Cap())
	}

	close(release)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// T12: Close returns promptly even when the buffer is full and workers are busy.
func TestT12_ShutdownWhileFull(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	proc := func(ctx context.Context, e domain.Event) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}

	c := collector.New(collector.DefaultBufferSize, collector.DefaultMaxConcurrent, proc, discardLogger())

	for i := 0; i < 10000; i++ {
		e := domain.Event{Type: "alert", Service: "svc", Message: fmt.Sprintf("m-%d", i)}
		if !c.Push(e) {
			break
		}
	}
	if c.Len() == 0 {
		t.Fatalf("expected the buffer to be non-empty before shutdown")
	}

	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked while channel was full")
	}

	if c.Push(domain.Event{Type: "alert", Service: "svc", Message: "after-close"}) {
		t.Fatal("Push after Close must return false")
	}
}
