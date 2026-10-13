package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/store"
)

func TestStoreUsesWALAndPersists(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "faultpilot.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	mode, err := st.JournalMode(ctx)
	if err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("want journal_mode wal, got %q", mode)
	}

	e := domain.Event{ID: "ev-000001", Timestamp: time.Now(), Type: "alert", Service: "checkout", Message: "boom"}
	if err := st.AppendEvent(ctx, e); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := st.AppendEvent(ctx, e); err != nil {
		t.Fatalf("append duplicate: %v", err)
	}
	if n, _ := st.CountEvents(ctx); n != 1 {
		t.Fatalf("want 1 event after duplicate insert, got %d", n)
	}

	inc := domain.Incident{ID: "inc-000001", Status: "open", EvidenceIDs: []string{"ev-000001"}}
	if err := st.CreateIncident(ctx, inc); err != nil {
		t.Fatalf("create incident: %v", err)
	}
	if n, _ := st.CountIncidents(ctx); n != 1 {
		t.Fatalf("want 1 incident, got %d", n)
	}
}
