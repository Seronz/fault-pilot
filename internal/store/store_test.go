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

func TestStoreJournalPersistAndLookup(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	entry := domain.JournalEntry{
		ActionID:       "act-1",
		IdempotencyKey: "key-1",
		IncidentID:     "inc-1",
		Action:         "restart_simulated_service",
		Target:         "checkout",
		State:          domain.JournalExecuting,
		UpdatedAt:      time.Now(),
	}
	if err := st.SaveJournalEntry(ctx, entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, ok, err := st.JournalByKey(ctx, "key-1")
	if err != nil || !ok {
		t.Fatalf("by key: ok=%v err=%v", ok, err)
	}
	if got.ActionID != "act-1" || got.State != domain.JournalExecuting {
		t.Fatalf("unexpected entry: %+v", got)
	}

	// Upsert: transisi state.
	entry.State = domain.JournalSucceeded
	if err := st.SaveJournalEntry(ctx, entry); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _, _ = st.JournalEntry(ctx, "act-1")
	if got.State != domain.JournalSucceeded {
		t.Fatalf("state harus succeeded, dapat %s", got.State)
	}

	byState, err := st.JournalByState(ctx, domain.JournalSucceeded)
	if err != nil || len(byState) != 1 {
		t.Fatalf("by state: len=%d err=%v", len(byState), err)
	}
}
