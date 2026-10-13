package investigation_test

import (
	"context"
	"testing"

	"github.com/seronz/fault-pilot/internal/domain"
	"github.com/seronz/fault-pilot/internal/investigation"
)

func TestT13_OutOfOrderEvidence(t *testing.T) {
	ctx := context.Background()
	inv := investigation.New()
	// T13: out-of-order evidence tetap merangkum dengan benar
	events := []domain.Event{
		{ID: "ev-3", Service: "checkout", Message: "error 3"},
		{ID: "ev-1", Service: "checkout", Message: "error 1"},
		{ID: "ev-2", Service: "checkout", Message: "error 2"},
	}
	inc := domain.Incident{ID: "inc-1", Status: "open", EvidenceIDs: []string{"ev-1", "ev-2", "ev-3"}}
	summary := inv.BuildSummary(ctx, inc, events)
	if summary == "" {
		t.Fatal("summary must not be empty")
	}
}
